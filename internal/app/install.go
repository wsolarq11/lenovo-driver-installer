package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"lenovo-driver/internal/compare"
	"lenovo-driver/internal/download"
	"lenovo-driver/internal/install"
	"lenovo-driver/internal/inventory"
	"lenovo-driver/internal/model"
)

// InstallSelected downloads and installs the selected drivers, mirroring the PS1 main loop.
func (a *App) InstallSelected(
	ctx context.Context,
	vc *ViewContext,
	selected []*model.Driver,
	dataSource string,
	listOsID string,
) error {
	dlDir := vc.Opts.DownloadDir
	if dlDir == "" {
		dlDir = filepath.Join(os.TempDir(), "LenovoDrivers")
	}
	if err := os.MkdirAll(dlDir, 0o755); err != nil {
		return err
	}
	a.Log(ctx, "Download directory : "+dlDir, "INFO")

	var success []*model.Driver
	var failed []*model.Driver
	for _, driver := range selected {
		if driver == nil {
			continue
		}
		outFile := filepath.Join(dlDir, driver.DriverCode+"_"+driver.FileName)
		expectedSize, sizeErr := compare.ConvertToBytes(driver.FileSize)
		if sizeErr != nil {
			a.Log(ctx, "["+driver.DriverCode+"] Invalid driver file size: "+sizeErr.Error(), "ERROR")
			failed = append(failed, driver)
			continue
		}
		a.Log(ctx, "["+driver.DriverCode+"] Downloading "+driver.FileName, "INFO")
		downloadedSize, downloadErr := a.downloadVerified(ctx, vc, driver, outFile, expectedSize, listOsID, dataSource)
		if downloadErr != nil {
			_ = os.Remove(outFile)
			_ = os.Remove(outFile + ".sha256")
			a.Log(ctx, "["+driver.DriverCode+"] Download failed: "+downloadErr.Error(), "ERROR")
			failed = append(failed, driver)
			continue
		}

		if vc.Opts.DownloadOnly {
			a.Log(ctx, "["+driver.DriverCode+"] Downloaded only.", "INFO")
			a.writeHistoryRecordChecked(ctx, driver, "Downloaded", fmt.Sprintf("size=%d bytes", downloadedSize), "", "")
			success = append(success, driver)
			continue
		}

		a.Log(ctx, "["+driver.DriverCode+"] Installing "+driver.FileName, "INFO")
		code, installErr := install.InstallDriverFile(outFile, driver, dlDir)
		switch {
		case code == 0 && installErr == nil:
			a.Log(ctx, "["+driver.DriverCode+"] Install success.", "INFO")
			a.writeHistoryRecordChecked(ctx, driver, "Installed", "exit=0", "", "")
			success = append(success, driver)
		case installErr != nil:
			// Terminal failure with no usable exit code (timeout/unpack/empty INF).
			message := installErr.Error()
			a.Log(ctx, "["+driver.DriverCode+"] Install failed: "+message, "ERROR")
			a.writeHistoryRecordChecked(ctx, driver, "Failed", message, "", "")
			failed = append(failed, driver)
		case strings.EqualFold(filepath.Ext(outFile), ".exe"):
			// A silent EXE ran and exited non-zero; offer the user an interactive rerun.
			if interactiveCode, interactiveErr := a.tryInteractiveExeFallback(driver, outFile, dlDir); interactiveErr == nil {
				a.Log(ctx, "["+driver.DriverCode+"] Interactive installer succeeded.", "INFO")
				a.writeHistoryRecordChecked(ctx, driver, "Installed", "interactive exit=0", "", "")
				success = append(success, driver)
			} else {
				message := fmt.Sprintf("interactive installer exit %d: %s", interactiveCode, interactiveErr.Error())
				a.Log(ctx, "["+driver.DriverCode+"] Install failed: "+message, "ERROR")
				a.writeHistoryRecordChecked(ctx, driver, "Failed", message, "", "")
				failed = append(failed, driver)
			}
		default:
			// Concrete non-zero exit from a non-EXE package.
			a.Log(ctx, fmt.Sprintf("[%s] Install exit code %d.", driver.DriverCode, code), "ERROR")
			a.writeHistoryRecordChecked(ctx, driver, "Failed", fmt.Sprintf("exit=%d", code), "", "")
			failed = append(failed, driver)
		}
	}

	if len(success) > 0 && !vc.Opts.DownloadOnly {
		a.verifyInstalled(ctx, success)
	}

	elapsed := time.Since(a.startedAt).Minutes()
	a.Log(ctx, fmt.Sprintf("Finished: success=%d, failed=%d, elapsed=%.2f min", len(success), len(failed), elapsed), "INFO")
	a.Log(ctx, "Log file: "+a.LogPath, "INFO")
	if len(failed) > 0 {
		return fmt.Errorf("%d driver(s) failed", len(failed))
	}
	return nil
}

func (a *App) tryInteractiveExeFallback(driver *model.Driver, filePath, workingDir string) (int, error) {
	if !strings.EqualFold(filepath.Ext(filePath), ".exe") {
		return -2, fmt.Errorf("interactive fallback is only available for EXE packages")
	}
	fmt.Fprintf(a.Stdout, "Type r to run %s interactively, or s to skip: ", driver.FileName)
	line, err := readLine(a.Stdin)
	if err != nil {
		a.Log(context.Background(), "["+driver.DriverCode+"] No interactive fallback answer was received; skipping.", "WARN")
		return -2, fmt.Errorf("interactive fallback skipped")
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	if answer != "r" {
		if answer == "" {
			a.Log(context.Background(), "["+driver.DriverCode+"] No interactive fallback answer was received; skipping.", "WARN")
		}
		return -2, fmt.Errorf("interactive fallback skipped")
	}
	result := install.RunProcessWithTimeout(filePath, nil, 0, workingDir)
	if compare.InstallSucceeded(result.ExitCode) {
		return 0, nil
	}
	return result.ExitCode, fmt.Errorf("interactive installer exit %d", result.ExitCode)
}

func (a *App) downloadVerified(
	ctx context.Context,
	vc *ViewContext,
	driver *model.Driver,
	outFile string,
	expectedSize int64,
	listOsID string,
	dataSource string,
) (int64, error) {
	opts := vc.Opts
	// At most one URL refresh is attempted, so the loop is bounded to two
	// passes: an initial download attempt and one retry against a refreshed
	// URL after a 403. The pass index makes the bound explicit instead of an
	// unbounded "for" whose termination the reader must infer from returns.
	for pass := 0; pass < 2; pass++ {
		needsDownload := true
		usable, existingSize, rejectReason := inspectCachedFile(outFile, expectedSize, driver.OfficialMD5, opts.SkipHashCheck)
		if usable {
			needsDownload = false
			a.Log(ctx, fmt.Sprintf("[%s] Using verified cached file (%d bytes).", driver.DriverCode, existingSize), "INFO")
		} else if rejectReason != "" {
			a.Log(ctx, "["+driver.DriverCode+"] "+rejectReason+", redownloading.", "WARN")
			_ = os.Remove(outFile)
			_ = os.Remove(outFile + ".sha256")
		}
		if needsDownload {
			err := a.Downloader.DownloadWithRetry(ctx, driver.FilePath, outFile, 3)
			if err != nil {
				if download.IsHTTPStatus(err, 403) && pass == 0 {
					a.Log(ctx, "["+driver.DriverCode+"] Download URL returned 403; refreshing official URL.", "WARN")
					_ = os.Remove(outFile)
					_ = os.Remove(outFile + ".sha256")
					refreshed, refreshErr := a.refreshDriverURL(ctx, vc, driver, listOsID, dataSource)
					if refreshErr != nil {
						a.Log(ctx, "["+driver.DriverCode+"] URL refresh failed: "+refreshErr.Error(), "ERROR")
						return 0, err
					}
					driver.FilePath = refreshed
					a.Log(ctx, "["+driver.DriverCode+"] Refreshed URL; retrying download once.", "INFO")
					continue
				}
				return 0, err
			}
		}
		return verifyDownloadedFile(outFile, expectedSize, driver.OfficialMD5, opts.SkipHashCheck, needsDownload)
	}
	return 0, fmt.Errorf("download did not converge after 2 passes")
}

func inspectCachedFile(outFile string, expectedSize int64, officialMD5 string, skipHashCheck bool) (usable bool, size int64, rejectReason string) {
	info, err := os.Stat(outFile)
	if err != nil || info.IsDir() {
		return false, 0, ""
	}
	size = info.Size()
	if !compare.TestFileSizeMatch(expectedSize, size) {
		return false, size, "Cached file size mismatch"
	}
	if !download.TestHashCompanion(outFile, skipHashCheck) {
		return false, size, "Cached file hash missing or mismatch"
	}
	if officialMD5 != "" && !skipHashCheck && download.FileMD5(outFile) != officialMD5 {
		return false, size, "Cached file MD5 mismatch"
	}
	return true, size, ""
}

func verifyDownloadedFile(outFile string, expectedSize int64, officialMD5 string, skipHashCheck, createCompanion bool) (int64, error) {
	downloadedSize, err := fileSize(outFile)
	if err != nil {
		return 0, err
	}
	if !compare.TestFileSizeMatch(expectedSize, downloadedSize) {
		return 0, fmt.Errorf("downloaded size mismatch: expected %d, got %d", expectedSize, downloadedSize)
	}
	if officialMD5 != "" && !skipHashCheck {
		actualMD5 := download.FileMD5(outFile)
		if actualMD5 == "" || actualMD5 != officialMD5 {
			return 0, fmt.Errorf("official MD5 mismatch: expected %s, got %s", officialMD5, actualMD5)
		}
	}
	if createCompanion && !skipHashCheck {
		downloadedHash := download.FileSHA256(outFile)
		if downloadedHash == "" {
			return 0, fmt.Errorf("could not compute SHA-256 for downloaded file")
		}
		_ = os.Remove(outFile + ".sha256")
		if err := download.WriteHashCompanion(outFile, downloadedHash); err != nil {
			return 0, err
		}
	}
	return downloadedSize, nil
}

func (a *App) refreshDriverURL(ctx context.Context, vc *ViewContext, driver *model.Driver, listOsID, preferredSource string) (string, error) {
	osIDs := []string{listOsID}
	if vc.Opts.LatestAcrossOS {
		osIDs = append(osIDs, otherOSIDs(vc.OsList, listOsID)...)
	}
	// Fetch each candidate OS list in parallel (independent reads), then scan
	// in OSID order so the first match is selected deterministically.
	var lastErr error
	for _, load := range a.loadDriverListsForOSIDs(ctx, vc, osIDs, preferredSource) {
		if load.err != nil {
			lastErr = load.err
			continue
		}
		for _, candidate := range load.result.Drivers {
			if candidate.DriverCode == driver.DriverCode && candidate.FilePath != "" {
				return candidate.FilePath, nil
			}
		}
		lastErr = fmt.Errorf("driver %s has no refreshed URL for OSID %s", driver.DriverCode, load.osID)
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("driver %s has no refreshed URL", driver.DriverCode)
	}
	return "", lastErr
}

func fileSize(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, fmt.Errorf("could not stat downloaded file %s: %w", path, err)
	}
	return info.Size(), nil
}

func (a *App) verifyInstalled(ctx context.Context, drivers []*model.Driver) {
	a.Log(ctx, "Running post-install verification pass...", "INFO")
	localDevices, err := inventory.GetLocalDeviceSnapshot(ctx)
	if err != nil {
		a.Log(ctx, "Post-install verification failed: "+err.Error(), "ERROR")
		return
	}
	installedApps, err := inventory.GetInstalledApps(ctx)
	if err != nil {
		a.Log(ctx, "Post-install verification failed: "+err.Error(), "ERROR")
		return
	}
	snapshot, err := inventory.GetSoftwareSnapshot(ctx, installedApps)
	if err != nil {
		a.Log(ctx, "Post-install verification failed: "+err.Error(), "ERROR")
		return
	}
	versionIndex, err := a.deviceVersionIndex(ctx, localDevices, drivers)
	if err != nil {
		a.Log(ctx, "Post-install verification failed: could not read local driver versions: "+err.Error(), "ERROR")
		return
	}
	for _, driver := range drivers {
		beforeLocal := driver.LocalVersion
		matched := compare.GetMatchingLocalDevices(driver, localDevices)
		afterLocal, _ := a.localDriverState(driver, matched, versionIndex, &snapshot)
		if afterLocal == "" {
			a.Log(ctx, "["+driver.DriverCode+"] Recheck: version not detectable yet.", "WARN")
			continue
		}
		beforeLabel := beforeLocal
		if beforeLabel == "" {
			beforeLabel = "not detected"
		}
		if afterLocal == beforeLocal {
			packageVersion := compare.GetMatchingRemoteComponent(driver.Version, driver.LocalVendor)
			if packageVersion != nil && packageVersion.String() != afterLocal {
				a.Log(ctx, fmt.Sprintf("[%s] Recheck: package installed but local driver unchanged (%s); package version %s did not replace it.", driver.DriverCode, afterLocal, packageVersion.String()), "WARN")
			} else {
				a.Log(ctx, fmt.Sprintf("[%s] Recheck: unchanged (%s); reboot may be needed.", driver.DriverCode, afterLocal), "WARN")
			}
		} else {
			a.Log(ctx, fmt.Sprintf("[%s] Recheck: %s -> %s", driver.DriverCode, beforeLabel, afterLocal), "INFO")
		}
		a.writeHistoryRecordChecked(ctx, driver, "Verified", fmt.Sprintf("local=%s; before=%s", afterLocal, beforeLabel), afterLocal, beforeLocal)
	}
}
