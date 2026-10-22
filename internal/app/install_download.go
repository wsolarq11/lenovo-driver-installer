package app

// Download transport and integrity: fetching a package, refreshing an expired
// URL once, deciding whether a cached file is still trustworthy, and verifying a
// downloaded one. These are file-and-network effects, so they live apart from the
// install loop that decides what to do with the result.

import (
	"context"
	"fmt"
	"os"

	"lenovo-driver/internal/compare"
	"lenovo-driver/internal/download"
	"lenovo-driver/internal/model"
	"lenovo-driver/internal/trust"
)

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
	// An empty OfficialMD5 silently skips the vendor-hash comparison below. That
	// is correct for the webpage source, which never sends one, but it must not
	// look like a verified download: say which evidence is actually left.
	if driver.OfficialMD5 == "" && !opts.SkipHashCheck {
		a.Log(ctx, "["+driver.DriverCode+"] No official MD5 in the "+driver.SourceAPI+
			" row; integrity rests on the local SHA-256 companion and the Authenticode signature.", "WARN")
	}
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
