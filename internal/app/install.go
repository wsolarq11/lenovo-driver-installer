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
	opts *Options,
	selected []*model.Driver,
	dataSource string,
	categoryID string,
	sysID string,
	osList []model.OSListEntry,
) error {
	dlDir := opts.DownloadDir
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
		expectedSize := compare.ConvertToBytes(driver.FileSize)
		a.Log(ctx, "["+driver.DriverCode+"] Downloading "+driver.FileName, "INFO")
		downloadedSize, downloadErr := a.downloadVerified(ctx, opts, driver, outFile, expectedSize, categoryID, sysID, osList, dataSource)
		if downloadErr != nil {
			_ = os.Remove(outFile)
			_ = os.Remove(outFile + ".sha256")
			a.Log(ctx, "["+driver.DriverCode+"] Download failed: "+downloadErr.Error(), "ERROR")
			failed = append(failed, driver)
			continue
		}

		if opts.DownloadOnly {
			a.Log(ctx, "["+driver.DriverCode+"] Downloaded only.", "INFO")
			a.writeHistoryRecordChecked(ctx, driver, "Downloaded", fmt.Sprintf("size=%d bytes", downloadedSize), "", "")
			success = append(success, driver)
			continue
		}

		a.Log(ctx, "["+driver.DriverCode+"] Installing "+driver.FileName, "INFO")
		code, installErr := install.InstallDriverFile(outFile, driver, dlDir)
		if installErr == nil && code == 0 {
			a.Log(ctx, "["+driver.DriverCode+"] Install success.", "INFO")
			a.writeHistoryRecordChecked(ctx, driver, "Installed", "exit=0", "", "")
			success = append(success, driver)
		} else if installErr != nil {
			interactiveCode, interactiveErr := a.tryInteractiveExeFallback(driver, outFile, dlDir)
			if interactiveErr == nil {
				a.Log(ctx, "["+driver.DriverCode+"] Interactive installer succeeded.", "INFO")
				a.writeHistoryRecordChecked(ctx, driver, "Installed", "interactive exit=0", "", "")
				success = append(success, driver)
			} else {
				message := fmt.Sprintf("interactive installer exit %d: %s", interactiveCode, interactiveErr.Error())
				a.Log(ctx, "["+driver.DriverCode+"] Install failed: "+message, "ERROR")
				a.writeHistoryRecordChecked(ctx, driver, "Failed", message, "", "")
				failed = append(failed, driver)
			}
		} else {
			a.Log(ctx, fmt.Sprintf("[%s] Install exit code %d.", driver.DriverCode, code), "ERROR")
			a.writeHistoryRecordChecked(ctx, driver, "Failed", fmt.Sprintf("exit=%d", code), "", "")
			failed = append(failed, driver)
		}
	}

	if len(success) > 0 && !opts.DownloadOnly {
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
	answer := strings.ToLower(strings.TrimSpace(readLine(a.Stdin)))
	if answer != "r" {
		if answer == "" {
			a.Log(context.Background(), "["+driver.DriverCode+"] No interactive fallback answer was received; skipping.", "WARN")
		}
		return -2, fmt.Errorf("interactive fallback skipped")
	}
	result := install.RunProcessWithTimeout(filePath, nil, 0, workingDir)
	if result.ExitCode == 0 || result.ExitCode == 3010 || result.ExitCode == 1641 {
		return 0, nil
	}
	return result.ExitCode, fmt.Errorf("interactive installer exit %d", result.ExitCode)
}

func (a *App) downloadVerified(
	ctx context.Context,
	opts *Options,
	driver *model.Driver,
	outFile string,
	expectedSize int64,
	categoryID string,
	sysID string,
	osList []model.OSListEntry,
	dataSource string,
) (int64, error) {
	downloadAttempt := 0
	refreshAttempted := false
	for {
		downloadAttempt++
		needsDownload := true
		existingSize := int64(0)
		if info, err := os.Stat(outFile); err == nil && !info.IsDir() {
			existingSize = info.Size()
			if compare.TestFileSizeMatch(expectedSize, existingSize) {
				needsDownload = false
			} else {
				a.Log(ctx, "["+driver.DriverCode+"] Cached file size mismatch, redownloading.", "WARN")
				_ = os.Remove(outFile)
				_ = os.Remove(outFile + ".sha256")
			}
			if !needsDownload && !download.TestHashCompanion(outFile, opts.SkipHashCheck) {
				a.Log(ctx, "["+driver.DriverCode+"] Cached file hash missing or mismatch, redownloading.", "WARN")
				_ = os.Remove(outFile)
				_ = os.Remove(outFile + ".sha256")
				needsDownload = true
			}
			if !needsDownload && driver.OfficialMD5 != "" && !opts.SkipHashCheck {
				cachedMD5 := download.FileMD5(outFile)
				if cachedMD5 != driver.OfficialMD5 {
					a.Log(ctx, "["+driver.DriverCode+"] Cached file MD5 mismatch, redownloading.", "WARN")
					_ = os.Remove(outFile)
					_ = os.Remove(outFile + ".sha256")
					needsDownload = true
				}
			}
			if !needsDownload {
				a.Log(ctx, fmt.Sprintf("[%s] Using verified cached file (%d bytes).", driver.DriverCode, existingSize), "INFO")
			}
		}
		if needsDownload {
			err := a.Downloader.DownloadWithRetry(ctx, driver.FilePath, outFile, 3)
			if err != nil {
				if download.IsHTTPStatus(err, 403) && !refreshAttempted {
					refreshAttempted = true
					a.Log(ctx, "["+driver.DriverCode+"] Download URL returned 403; refreshing official URL.", "WARN")
					_ = os.Remove(outFile)
					_ = os.Remove(outFile + ".sha256")
					refreshed, refreshErr := a.APIClient.GetRefreshedDriverURL(ctx, driver, categoryID, sysID, osList, opts.LatestAcrossOS, dataSource == "QuickFix")
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
		downloadedSize := fileSize(outFile)
		if !compare.TestFileSizeMatch(expectedSize, downloadedSize) {
			return 0, fmt.Errorf("downloaded size mismatch: expected %d, got %d", expectedSize, downloadedSize)
		}
		if driver.OfficialMD5 != "" && !opts.SkipHashCheck {
			actualMD5 := download.FileMD5(outFile)
			if actualMD5 == "" || actualMD5 != driver.OfficialMD5 {
				return 0, fmt.Errorf("official MD5 mismatch: expected %s, got %s", driver.OfficialMD5, actualMD5)
			}
		}
		if needsDownload && !opts.SkipHashCheck {
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
}

func fileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
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
	for _, driver := range drivers {
		beforeLocal := driver.LocalVersion
		matched := compare.GetMatchingLocalDevices(driver, localDevices)
		var pnpIDs []string
		for _, dev := range matched {
			if dev.PnpDeviceID != "" {
				pnpIDs = append(pnpIDs, dev.PnpDeviceID)
			}
		}
		driverVersions, _ := inventory.GetDeviceDriverVersions(ctx, pnpIDs)
		afterLocal, _ := compare.ResolveLocalDriverVersion(driver, matched, driverVersions, &snapshot)
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
