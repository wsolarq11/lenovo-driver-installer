package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"lenovo-driver/internal/compare"
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

// InstallSelected downloads and installs the selected drivers.
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

		silence, interactive := silentInstallEvidence(driver, outFile)
		if interactive {
			a.Log(ctx, "["+driver.DriverCode+"] No proven silent mechanism in the package; using interactive install: "+silence, "WARN")
			a.handleInteractiveExe(ctx, ad, outFile, dlDir, &success, &failed)
			continue
		}

		a.Log(ctx, "["+driver.DriverCode+"] Installing "+driver.FileName, "INFO")
		code, installErr := install.InstallDriverFile(outFile, driver, dlDir)
		switch classifyInstallResult(code, installErr) {
		case outcomeRebootRequired:
			// Reboot-required installs are success, but every later install must
			// wait for that reboot. Stop the pass and defer the rest instead of
			// chaining installs across an uncommitted driver state.
			a.Log(ctx, fmt.Sprintf("[%s] Installed; reboot required (exit %d). Deferring remaining drivers.", driver.DriverCode, code), "WARN")
			a.writeHistoryRecordChecked(ctx, ad, "Installed", fmt.Sprintf("exit=%d reboot-required; silent via %s", code, silence), "", "")
			success = append(success, ad)
			deferred = append(deferred, selected[i+1:]...)
			break installLoop
		case outcomeSuccess:
			a.Log(ctx, "["+driver.DriverCode+"] Install success.", "INFO")
			a.writeHistoryRecordChecked(ctx, ad, "Installed", "exit=0; silent via "+silence, "", "")
			success = append(success, ad)
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

func fileSize(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, fmt.Errorf("could not stat downloaded file %s: %w", path, err)
	}
	return info.Size(), nil
}

// installOutcome is what one installer invocation amounted to. The zero value is
// Failed, matching every other closed set in this package: an outcome that was
// never assigned must not read as a success. classifyInstallResult covers every
// branch today, so the ordering is a guard rather than a fix.
type installOutcome int

const (
	outcomeFailed installOutcome = iota
	outcomeSuccess
	outcomeRebootRequired
)

// classifyInstallResult maps one InstallDriverFile result to the install-loop
// outcome. Kept pure so the reboot-deferral decision is testable offline
// without downloading or installing anything. EXE packages never reach here:
// they are routed to the interactive fallback before InstallDriverFile, because
// no .exe silent switch is verified.
func classifyInstallResult(code int, installErr error) installOutcome {
	if compare.TestRebootExitCode(code) {
		return outcomeRebootRequired
	}
	if installErr != nil {
		return outcomeFailed
	}
	if code == 0 {
		return outcomeSuccess
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
		// Both sides of the comparison must be measured versions. The display
		// string is for prose; passing it here worked only because "not detected"
		// happens to parse as nothing, which is a coincidence, not a rule.
		beforeLocal := ad.LocalEvidence.MeasuredVersion()
		matched := compare.GetMatchingLocalDevices(driver, localDevices)
		afterEvidence, _ := a.localDriverState(driver, matched, versionIndex, &snapshot)
		// The display string is derived once, so every message below names the
		// same fact the binding verdict was computed from. It is prose ("not
		// detected", "a, b") and therefore belongs in Message, never in a version
		// column: those two columns hold a measured version or nothing, and a
		// sentence there would read as a version claim to every later join.
		afterLocal := afterEvidence.Display()
		afterMeasured := afterEvidence.MeasuredVersion()
		projected := projectMatchedEvidence(matched, evidenceByID)
		afterProblem := model.DeviceProblemSummary(projected)
		afterInf := firstInfName(projected)
		beforeInf := ad.BeforeInfName
		beforeLabel := ad.LocalEvidence.Display()
		problemLabel := afterProblem
		if problemLabel == "" {
			problemLabel = "none"
		}
		binding := installBindingVerdict(beforeLocal, afterEvidence, driver.Version, ad.LocalVendor)
		if binding.confirmsEffect() {
			verified = append(verified, ad)
		} else {
			unverified = append(unverified, ad.Driver.DriverCode)
		}
		message := fmt.Sprintf("binding=%s; local=%s; before=%s; problem=%s", binding.String(), afterLocal, beforeLabel, problemLabel)
		// Causal gate: a rollback offer is only honest when the problem is NEW
		// (clean before install, problem after). A problem that pre-existed the
		// install cannot be attributed to it, so it is reported but not offered.
		basis := rollbackCausalBasis(ad.DeviceProblem, afterProblem)
		if basis == causalNew {
			message += "; attribution=" + basis.String()
			if afterInf != "" {
				rollback := manualRollbackGuidance(beforeInf)
				message += "; rollback=" + rollback
				a.Log(ctx, fmt.Sprintf("[%s] Rollback hint: %s", driver.DriverCode, rollback), "WARN")
			}
			if offer := a.buildRollbackOffer(ad, beforeLocal, afterMeasured, projected); len(offer.Devices) > 0 {
				offers = append(offers, offer)
			}
		} else if basis == causalPreExisting {
			message += "; attribution=" + basis.String()
			a.Log(ctx, fmt.Sprintf("[%s] Recheck: problem pre-existed (%s); not attributed to this install.", driver.DriverCode, ad.DeviceProblem), "WARN")
		}
		switch binding {
		case bindingBound:
			if afterProblem != "" {
				a.Log(ctx, fmt.Sprintf("[%s] Recheck: bound at %s; device problem persists (%s).", driver.DriverCode, afterLocal, afterProblem), "WARN")
			} else {
				a.Log(ctx, fmt.Sprintf("[%s] Recheck: bound at %s; no device problem.", driver.DriverCode, afterLocal), "INFO")
			}
		case bindingStaged:
			a.Log(ctx, fmt.Sprintf("[%s] Recheck: staged %s -> %s; package %s not yet bound (reboot may be needed).", driver.DriverCode, beforeLabel, afterLocal, driver.Version), "WARN")
		case bindingUnchangedSame:
			a.Log(ctx, fmt.Sprintf("[%s] Recheck: machine already ran %s; reinstall confirmed no-op.", driver.DriverCode, afterLocal), "INFO")
		case bindingUnchanged:
			packageVersion := compare.GetMatchingRemoteComponent(driver.Version, ad.LocalVendor)
			if packageVersion != nil && packageVersion.String() != afterEvidence.MeasuredVersion() {
				a.Log(ctx, fmt.Sprintf("[%s] Recheck: package installed but local driver unchanged (%s); package version %s did not replace it.", driver.DriverCode, afterLocal, packageVersion.String()), "WARN")
			} else {
				a.Log(ctx, fmt.Sprintf("[%s] Recheck: unchanged (%s); reboot may be needed.", driver.DriverCode, afterLocal), "WARN")
			}
		case bindingUndetected:
			a.Log(ctx, fmt.Sprintf("[%s] Recheck: no comparable local version (%s); this driver's version lives outside the device list, so the recheck cannot confirm the install.", driver.DriverCode, beforeLabel), "ERROR")
		}
		a.writeHistoryRecordChecked(ctx, ad, "Verified", message, afterMeasured, beforeLocal)
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
