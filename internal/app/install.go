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
	"lenovo-driver/internal/trust"
)

// verifyFileSignature is the process-wide Authenticode verification hook. It
// defaults to the Windows WinVerifyTrust implementation and is a package
// variable only so the offline gate can stub the real trust call for unsigned
// fixture files. Production code never overrides it.
var verifyFileSignature = trust.VerifyFileSignature

// InstallSelected downloads and installs the selected drivers, mirroring the PS1 main loop.
func (a *App) InstallSelected(
	ctx context.Context,
	vc *ViewContext,
	selected []*model.AssessedDriver,
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

	var success []*model.AssessedDriver
	var failed []*model.AssessedDriver
	var deferred []*model.AssessedDriver
installLoop:
	for i, ad := range selected {
		if ad == nil || ad.Driver == nil {
			continue
		}
		driver := ad.Driver
		outFile := filepath.Join(dlDir, driver.DriverCode+"_"+driver.FileName)
		expectedSize, sizeErr := compare.ConvertToBytes(driver.FileSize)
		if sizeErr != nil {
			a.Log(ctx, "["+driver.DriverCode+"] Invalid driver file size: "+sizeErr.Error(), "ERROR")
			failed = append(failed, ad)
			continue
		}
		a.Log(ctx, "["+driver.DriverCode+"] Downloading "+driver.FileName, "INFO")
		downloadedSize, downloadErr := a.downloadVerified(ctx, vc, ad, outFile, expectedSize, listOsID, dataSource)
		if downloadErr != nil {
			_ = os.Remove(outFile)
			_ = os.Remove(outFile + ".sha256")
			a.Log(ctx, "["+driver.DriverCode+"] Download failed: "+downloadErr.Error(), "ERROR")
			failed = append(failed, ad)
			continue
		}

		if vc.Opts.DownloadOnly {
			a.Log(ctx, "["+driver.DriverCode+"] Downloaded only.", "INFO")
			a.writeHistoryRecordChecked(ctx, ad, "Downloaded", fmt.Sprintf("size=%d bytes", downloadedSize), "", "")
			success = append(success, ad)
			continue
		}

		// Audit-first: record the operator's install intent before mutating
		// device state. Windows records the mechanism (a driver was installed);
		// this row records the decision and blocks the install when the ledger
		// cannot be written (invariant 5: audit failure never commits).
		if err := a.WriteHistoryRecord(ad, "Install", "install requested", "", ""); err != nil {
			a.Log(ctx, "["+driver.DriverCode+"] Install intent audit write failed; skipping install: "+err.Error(), "ERROR")
			failed = append(failed, ad)
			continue
		}

		isEXE := strings.EqualFold(filepath.Ext(outFile), ".exe")
		if isEXE && !install.HasSilentParameters(outFile, driver) {
			a.Log(ctx, "["+driver.DriverCode+"] No official silent install parameters; using interactive install.", "WARN")
			a.handleInteractiveExe(ctx, ad, outFile, dlDir, &success, &failed)
			continue
		}

		a.Log(ctx, "["+driver.DriverCode+"] Installing "+driver.FileName, "INFO")
		code, installErr := install.InstallDriverFile(outFile, driver, dlDir)
		switch classifyInstallResult(code, installErr, isEXE) {
		case outcomeRebootRequired:
			// Reboot-required installs are success, but every later install must
			// wait for that reboot. Stop the pass and defer the rest instead of
			// chaining installs across an uncommitted driver state.
			a.Log(ctx, fmt.Sprintf("[%s] Installed; reboot required (exit %d). Deferring remaining drivers.", driver.DriverCode, code), "WARN")
			a.writeHistoryRecordChecked(ctx, ad, "Installed", fmt.Sprintf("exit=%d reboot-required", code), "", "")
			success = append(success, ad)
			deferred = append(deferred, selected[i+1:]...)
			break installLoop
		case outcomeSuccess:
			a.Log(ctx, "["+driver.DriverCode+"] Install success.", "INFO")
			a.writeHistoryRecordChecked(ctx, ad, "Installed", "exit=0", "", "")
			success = append(success, ad)
		case outcomeEXERetry:
			a.handleInteractiveExe(ctx, ad, outFile, dlDir, &success, &failed)
		case outcomeFailed:
			if installErr != nil {
				message := installErr.Error()
				a.Log(ctx, "["+driver.DriverCode+"] Install failed: "+message, "ERROR")
				a.writeHistoryRecordChecked(ctx, ad, "Failed", message, "", "")
				failed = append(failed, ad)
			} else {
				// Concrete non-zero exit from a non-EXE package.
				a.Log(ctx, fmt.Sprintf("[%s] Install exit code %d.", driver.DriverCode, code), "ERROR")
				a.writeHistoryRecordChecked(ctx, ad, "Failed", fmt.Sprintf("exit=%d", code), "", "")
				failed = append(failed, ad)
			}
		}
	}

	for _, ad := range deferred {
		if ad == nil || ad.Driver == nil {
			continue
		}
		a.writeHistoryRecordChecked(ctx, ad, "Deferred", "reboot required by earlier install", "", "")
	}
	if len(deferred) > 0 {
		a.Log(ctx, fmt.Sprintf("Deferred %d driver(s) until after reboot.", len(deferred)), "WARN")
	}

	var unverified []string
	if len(success) > 0 && !vc.Opts.DownloadOnly {
		success, unverified = a.verifyInstalled(ctx, success)
		if len(unverified) > 0 {
			a.Log(ctx, fmt.Sprintf("Unverified %d driver(s); the installer exited successfully but the machine shows no comparable change: %s", len(unverified), strings.Join(unverified, ", ")), "ERROR")
		}
	}

	elapsed := time.Since(a.startedAt).Minutes()
	a.Log(ctx, fmt.Sprintf("Finished: success=%d, failed=%d, deferred=%d, unverified=%d, elapsed=%.2f min", len(success), len(failed), len(deferred), len(unverified), elapsed), "INFO")
	a.Log(ctx, "Log file: "+a.LogPath, "INFO")
	if len(failed) > 0 {
		return fmt.Errorf("%d driver(s) failed", len(failed))
	}
	return nil
}

func (a *App) tryInteractiveExeFallback(ad *model.AssessedDriver, filePath, workingDir string) (int, error) {
	if !strings.EqualFold(filepath.Ext(filePath), ".exe") {
		return -2, fmt.Errorf("interactive fallback is only available for EXE packages")
	}
	fmt.Fprintf(a.Stdout, "Type r to run %s interactively, or s to skip: ", ad.FileName)
	line, err := readLine(a.Stdin)
	if err != nil {
		a.Log(context.Background(), "["+ad.DriverCode+"] No interactive fallback answer was received; skipping.", "WARN")
		return -2, fmt.Errorf("interactive fallback skipped")
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	if answer != "r" {
		if answer == "" {
			a.Log(context.Background(), "["+ad.DriverCode+"] No interactive fallback answer was received; skipping.", "WARN")
		}
		return -2, fmt.Errorf("interactive fallback skipped")
	}
	result := install.RunProcessWithTimeout(filePath, nil, 0, workingDir)
	if compare.InstallSucceeded(result.ExitCode) {
		return 0, nil
	}
	return result.ExitCode, fmt.Errorf("interactive installer exit %d", result.ExitCode)
}

// handleInteractiveExe runs the interactive EXE fallback and records the result
// into the shared success/failed collections. It exists so the no-silent-
// parameters path and the silent-failure path share one outcome policy.
func (a *App) handleInteractiveExe(ctx context.Context, ad *model.AssessedDriver, outFile, dlDir string, success, failed *[]*model.AssessedDriver) {
	if interactiveCode, interactiveErr := a.tryInteractiveExeFallback(ad, outFile, dlDir); interactiveErr == nil {
		a.Log(ctx, "["+ad.DriverCode+"] Interactive installer succeeded.", "INFO")
		a.writeHistoryRecordChecked(ctx, ad, "Installed", "interactive exit=0", "", "")
		*success = append(*success, ad)
	} else {
		message := fmt.Sprintf("interactive installer exit %d: %s", interactiveCode, interactiveErr.Error())
		a.Log(ctx, "["+ad.DriverCode+"] Install failed: "+message, "ERROR")
		a.writeHistoryRecordChecked(ctx, ad, "Failed", message, "", "")
		*failed = append(*failed, ad)
	}
}

func (a *App) downloadVerified(
	ctx context.Context,
	vc *ViewContext,
	ad *model.AssessedDriver,
	outFile string,
	expectedSize int64,
	listOsID string,
	dataSource string,
) (int64, error) {
	opts := vc.Opts
	driver := ad.Driver
	// Record revocation-check degradation per download using this run's context;
	// the fallback itself happens inside trust.VerifyFileSignature.
	trust.RevocationFallback = func(path string) {
		a.Log(ctx, "Revocation check unavailable for "+path+"; fell back to chain + signer verification.", "WARN")
	}
	// At most one URL refresh is attempted, so the loop is bounded to two
	// passes: an initial download attempt and one retry against a refreshed
	// URL after a 403. The pass index makes the bound explicit instead of an
	// unbounded "for" whose termination the reader must infer from returns.
	for pass := 0; pass < 2; pass++ {
		needsDownload := true
		usable, existingSize, rejectReason := inspectCachedFile(outFile, expectedSize, driver.OfficialMD5, opts.SkipHashCheck, opts.SkipSignatureCheck)
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
					refreshed, refreshErr := a.refreshDriverURL(ctx, vc, ad, listOsID, dataSource)
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
		return verifyDownloadedFile(outFile, expectedSize, driver.OfficialMD5, opts.SkipHashCheck, opts.SkipSignatureCheck, needsDownload)
	}
	return 0, fmt.Errorf("download did not converge after 2 passes")
}

func (a *App) refreshDriverURL(ctx context.Context, vc *ViewContext, ad *model.AssessedDriver, listOsID, preferredSource string) (string, error) {
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
			if candidate.DriverCode == ad.DriverCode && candidate.FilePath != "" {
				return candidate.FilePath, nil
			}
		}
		lastErr = fmt.Errorf("driver %s has no refreshed URL for OSID %s", ad.DriverCode, load.osID)
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("driver %s has no refreshed URL", ad.DriverCode)
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

type installOutcome int

const (
	outcomeSuccess installOutcome = iota
	outcomeRebootRequired
	outcomeEXERetry
	outcomeFailed
)

// classifyInstallResult maps one InstallDriverFile result to the install-loop
// outcome. Kept pure so the reboot-deferral decision is testable offline
// without downloading or installing anything.
func classifyInstallResult(code int, installErr error, isEXE bool) installOutcome {
	if compare.TestRebootExitCode(code) {
		return outcomeRebootRequired
	}
	if installErr != nil {
		return outcomeFailed
	}
	if code == 0 {
		return outcomeSuccess
	}
	if isEXE {
		return outcomeEXERetry
	}
	return outcomeFailed
}

// verifyInstalled re-reads machine state after the install pass and returns the
// drivers whose binding actually confirms the package took effect. Drivers it
// cannot confirm are reported as unverified and must not enter the run summary
// as successes: an installer can exit 0 without having changed anything.
func (a *App) verifyInstalled(ctx context.Context, drivers []*model.AssessedDriver) (verified []*model.AssessedDriver, unverified []string) {
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
	_, union := matchedByDriver(drivers, localDevices)
	versionIndex, err := a.deviceVersionIndex(ctx, union)
	if err != nil {
		a.Log(ctx, "Post-install verification failed: could not read local driver versions: "+err.Error(), "ERROR")
		return
	}
	// Re-read enriched device evidence so the recheck can distinguish a bound
	// healthy device from one that still reports a Windows problem code. A
	// failed evidence pass degrades to version-only verification, mirroring the
	// compare pass behavior.
	evidenceByID, evErr := a.buildDeviceEvidenceMap(ctx, union)
	if evErr != nil {
		a.Log(ctx, "Post-install device evidence lookup failed: "+evErr.Error(), "WARN")
		evidenceByID = nil
	}
	var offers []rollbackOffer
	for _, ad := range drivers {
		if ad == nil || ad.Driver == nil {
			continue
		}
		driver := ad.Driver
		beforeLocal := ad.LocalVersion
		matched := compare.GetMatchingLocalDevices(driver, localDevices)
		afterLocal, _ := a.localDriverState(driver, matched, versionIndex, &snapshot)
		projected := projectMatchedEvidence(matched, evidenceByID)
		afterProblem := model.DeviceProblemSummary(projected)
		afterInf := firstInfName(projected)
		beforeInf := ad.BeforeInfName
		beforeLabel := beforeLocal
		if beforeLabel == "" {
			beforeLabel = "not detected"
		}
		problemLabel := afterProblem
		if problemLabel == "" {
			problemLabel = "none"
		}
		binding := installBindingLabel(beforeLocal, afterLocal, driver.Version)
		if installBindingConfirmsEffect(binding) {
			verified = append(verified, ad)
		} else {
			unverified = append(unverified, ad.Driver.DriverCode)
		}
		message := fmt.Sprintf("binding=%s; local=%s; before=%s; problem=%s", binding, afterLocal, beforeLabel, problemLabel)
		// Causal gate: a rollback offer is only honest when the problem is NEW
		// (clean before install, problem after). A problem that pre-existed the
		// install cannot be attributed to it, so it is reported but not offered.
		basis := rollbackCausalBasis(ad.DeviceProblem, afterProblem)
		if basis == "new" {
			message += "; attribution=new"
			if afterInf != "" {
				rollback := manualRollbackGuidance(beforeInf)
				message += "; rollback=" + rollback
				a.Log(ctx, fmt.Sprintf("[%s] Rollback hint: %s", driver.DriverCode, rollback), "WARN")
			}
			if offer := a.buildRollbackOffer(ad, beforeLocal, afterLocal, projected); len(offer.Devices) > 0 {
				offers = append(offers, offer)
			}
		} else if basis == "pre-existing" {
			message += "; attribution=pre-existing"
			a.Log(ctx, fmt.Sprintf("[%s] Recheck: problem pre-existed (%s); not attributed to this install.", driver.DriverCode, ad.DeviceProblem), "WARN")
		}
		switch binding {
		case "bound":
			if afterProblem != "" {
				a.Log(ctx, fmt.Sprintf("[%s] Recheck: bound at %s; device problem persists (%s).", driver.DriverCode, afterLocal, afterProblem), "WARN")
			} else {
				a.Log(ctx, fmt.Sprintf("[%s] Recheck: bound at %s; no device problem.", driver.DriverCode, afterLocal), "INFO")
			}
		case "staged":
			a.Log(ctx, fmt.Sprintf("[%s] Recheck: staged %s -> %s; package %s not yet bound (reboot may be needed).", driver.DriverCode, beforeLabel, afterLocal, driver.Version), "WARN")
		case "unchanged-same":
			a.Log(ctx, fmt.Sprintf("[%s] Recheck: machine already ran %s; reinstall confirmed no-op.", driver.DriverCode, afterLocal), "INFO")
		case "unchanged":
			packageVersion := compare.GetMatchingRemoteComponent(driver.Version, ad.LocalVendor)
			if packageVersion != nil && packageVersion.String() != afterLocal {
				a.Log(ctx, fmt.Sprintf("[%s] Recheck: package installed but local driver unchanged (%s); package version %s did not replace it.", driver.DriverCode, afterLocal, packageVersion.String()), "WARN")
			} else {
				a.Log(ctx, fmt.Sprintf("[%s] Recheck: unchanged (%s); reboot may be needed.", driver.DriverCode, afterLocal), "WARN")
			}
		case "undetected":
			a.Log(ctx, fmt.Sprintf("[%s] Recheck: no comparable local version (%s); this driver's version lives outside the device list, so the recheck cannot confirm the install.", driver.DriverCode, beforeLabel), "ERROR")
		default:
			a.Log(ctx, "["+driver.DriverCode+"] Recheck: version not detectable yet.", "WARN")
		}
		a.writeHistoryRecordChecked(ctx, ad, "Verified", message, afterLocal, beforeLocal)
	}
	for i := range offers {
		offer := &offers[i]
		a.writeHistoryRecordChecked(ctx, offer.assessedDriver(), "RollbackOffered", rollbackOfferedMessage(*offer), offer.BeforeVersion, offer.AfterVersion)
	}
	if err := a.writeRollbackOffers(offers); err != nil {
		a.Log(ctx, "Could not persist rollback offers: "+err.Error(), "WARN")
	} else if len(offers) > 0 {
		a.Log(ctx, "Rollback offers : "+a.rollbackOfferPath(), "WARN")
	}
	if len(verified) == 0 && len(unverified) > 0 {
		a.Log(ctx, "No installed driver could be confirmed from machine state.", "ERROR")
	}
	return verified, unverified
}

// manualRollbackGuidance builds the human instruction for a post-install device
// problem. Device rollback is a per-device operation, not package deletion:
// deleting the active package does not restore the previous driver and can
// leave the device without one, so the tool never runs pnputil /delete-driver
// as a rollback action.
func manualRollbackGuidance(previousInf string) string {
	if previousInf == "" {
		previousInf = "unknown"
	}
	return fmt.Sprintf("Device Manager -> device -> Driver -> Roll Back Driver (previous INF %s); do not run pnputil /delete-driver", previousInf)
}

func inspectCachedFile(outFile string, expectedSize int64, officialMD5 string, skipHashCheck, skipSignatureCheck bool) (usable bool, size int64, rejectReason string) {
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
	if !skipSignatureCheck {
		if err := verifyFileSignature(outFile); err != nil {
			return false, size, "Cached file signature invalid"
		}
	}
	return true, size, ""
}

func verifyDownloadedFile(outFile string, expectedSize int64, officialMD5 string, skipHashCheck, skipSignatureCheck, createCompanion bool) (int64, error) {
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
	if !skipSignatureCheck {
		if err := verifyFileSignature(outFile); err != nil {
			return 0, fmt.Errorf("file signature invalid: %w", err)
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
