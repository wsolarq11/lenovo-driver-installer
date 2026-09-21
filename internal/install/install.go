package install

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"lenovo-driver/internal/compare"
	"lenovo-driver/internal/model"
)

// errorSuccessRebootRequired is the Windows installer exit code (3010) for a
// successful install that requires a reboot. The native DiInstallDriverW path
// reports it via its reboot flag and pnputil's exit code 1 is normalized to
// it, so both paths report reboot-required installs identically.
// compare.InstallSucceeded treats it as success.
const errorSuccessRebootRequired = 3010

// ProcessResult is the outcome of running one installer sub-process. Callers
// consume the exit code, the timeout flag, StartErr, and KillErr. Captured
// sub-process output is deliberately discarded (it is not part of the public
// contract).
//
// StartErr is non-nil only when the sub-process could not be started at all
// (missing executable, access denied, bad path). A start failure has no usable
// exit code, so callers must treat it as a terminal error rather than as a
// concrete exit code. ExitCode remains the mirror of the command line supplied
// for callers that only inspect that legacy field.
//
// KillErr is non-nil only when a timed-out process could not be terminated
// (taskkill unavailable or lacking permission), i.e. the timeout was reached
// but the child tree may still be alive.
type ProcessResult struct {
	ExitCode int
	TimedOut bool
	StartErr error
	KillErr  error
}

type processRunner func(filePath string, args []string, timeoutSeconds int, workingDirectory string) ProcessResult

type exeFallback func(driver *model.Driver, workingDir string) (int, bool)

// timeoutErr builds the terminal timeout error for a ProcessResult that hit
// its deadline, appending the process-tree kill failure detail when the child
// could not be terminated (so a half-alive subprocess is never silently
// reported as cleanly timed out).
func timeoutErr(prefix string, result ProcessResult) error {
	if result.KillErr != nil {
		return fmt.Errorf("%s timed out (failed to terminate child tree: %v)", prefix, result.KillErr)
	}
	return fmt.Errorf("%s timed out", prefix)
}

// RunProcessWithTimeout mirrors Invoke-ProcessWithTimeout.
func RunProcessWithTimeout(filePath string, args []string, timeoutSeconds int, workingDirectory string) ProcessResult {
	return runProcess(context.Background(), filePath, args, time.Duration(timeoutSeconds)*time.Second, workingDirectory)
}

func processExitCode(err error) int {
	if err == nil {
		return 0
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		return exitErr.ExitCode()
	}
	return -2
}

func runProcess(ctx context.Context, filePath string, args []string, timeout time.Duration, workingDir string) ProcessResult {
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	cmd := exec.Command(filePath, args...)
	if workingDir != "" {
		cmd.Dir = workingDir
	}
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return ProcessResult{ExitCode: -2, StartErr: err}
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return ProcessResult{ExitCode: processExitCode(err)}
	case <-ctx.Done():
		killErr := killTree(cmd.Process.Pid)
		select {
		case <-done:
		case <-time.After(5 * time.Second):
		}
		return ProcessResult{ExitCode: -1, TimedOut: true, KillErr: killErr}
	}
}

// RunPnPUtilWithTimeout mirrors Invoke-PnPUtilWithTimeout.
func RunPnPUtilWithTimeout(infPath, workingDir string) ProcessResult {
	result := runProcess(context.Background(), "pnputil.exe", []string{"/add-driver", infPath, "/install"}, 900*time.Second, workingDir)
	if result.StartErr != nil {
		// A non-start is already a terminal failure; do not mask it as exit 1.
		return result
	}
	// pnputil returns 1 when a reboot is required for an otherwise successful
	// add; normalize it to the shared reboot code so the pnputil and native
	// paths report it identically.
	result.ExitCode = normalizePnPUtilExitCode(result.ExitCode)
	return result
}

// normalizePnPUtilExitCode maps pnputil's reboot-required exit code (1) to the
// shared success-with-reboot code (3010) used by the native DiInstallDriverW
// path. Kept pure so the mapping is testable offline.
func normalizePnPUtilExitCode(exitCode int) int {
	if exitCode == 1 {
		return errorSuccessRebootRequired
	}
	return exitCode
}

func killTree(pid int) error {
	cmd := exec.Command("taskkill.exe", "/PID", fmt.Sprintf("%d", pid), "/T", "/F")
	return cmd.Run()
}

// InstallDriverFile installs one downloaded driver package.
//
// The result contract is uniform across every package type:
//   - err == nil && exitCode == 0  -> clean success.
//   - err == nil && exitCode != 0  -> an installer executable ran and exited
//     non-zero (a concrete, surfaced exit code). The caller may choose to
//     recover these interactively, but they are never "unreasoned" failures.
//   - err != nil                   -> a terminal failure with no usable exit
//     code: nil driver, start failure, timeout, un/expansion error, or an
//     archive with no importable INF. These are not retryable as-is; a start
//     failure arrives through ProcessResult.StartErr and is surfaced here.
//
// Only the .exe branch may run *both* an automatic extracted fallback and a
// caller-initiated interactive rerun; every other package type is single-shot.
func InstallDriverFile(filePath string, driver *model.Driver, workingDir string) (int, error) {
	if driver == nil {
		return -2, fmt.Errorf("driver is nil")
	}
	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".msi":
		result := RunProcessWithTimeout("msiexec.exe", []string{"/i", filePath, "/qn", "/norestart"}, 1800, workingDir)
		if result.StartErr != nil {
			return result.ExitCode, result.StartErr
		}
		if result.TimedOut {
			return -1, timeoutErr("MSI", result)
		}
		if compare.InstallSucceeded(result.ExitCode) {
			return result.ExitCode, nil
		}
		return result.ExitCode, nil
	case ".inf":
		return installNativeOrPnPUtil(filePath, workingDir)
	case ".zip":
		extract := filepath.Join(workingDir, strings.TrimSuffix(filepath.Base(filePath), filepath.Ext(filePath)))
		if err := ExtractZip(filePath, extract); err != nil {
			return -2, err
		}
		return installINFs(extract, workingDir)
	case ".cab":
		extract := filepath.Join(workingDir, strings.TrimSuffix(filepath.Base(filePath), filepath.Ext(filePath)))
		if err := os.MkdirAll(extract, 0o755); err != nil {
			return -2, err
		}
		result := RunProcessWithTimeout("expand.exe", []string{filePath, "-F:*", extract}, 900, workingDir)
		if result.StartErr != nil {
			return result.ExitCode, result.StartErr
		}
		if result.TimedOut {
			return -1, timeoutErr("expand.exe", result)
		}
		if result.ExitCode != 0 {
			return result.ExitCode, fmt.Errorf("expand.exe exited %d", result.ExitCode)
		}
		return installINFs(extract, workingDir)
	case ".exe":
		return installEXE(filePath, driver, workingDir, RunProcessWithTimeout, ExtractedDriverFallback)
	default:
		result := RunProcessWithTimeout(filePath, nil, 1800, workingDir)
		if result.StartErr != nil {
			return result.ExitCode, result.StartErr
		}
		if result.TimedOut {
			return -1, timeoutErr("installer", result)
		}
		return result.ExitCode, nil
	}
}

func installEXE(filePath string, driver *model.Driver, workingDir string, run processRunner, fallback exeFallback) (int, error) {
	logPath := filepath.Join(workingDir, driver.DriverCode+".log")
	args, ok := silentInstallerArgs(driver, logPath)
	if !ok {
		return -2, fmt.Errorf("no official silent install parameters for %s", driver.FileName)
	}
	result := run(filePath, args, 900, workingDir)
	if result.TimedOut {
		// No exit code was produced; a used fallback may still recover, otherwise
		// this is a terminal timeout error.
		return finishEXEFallback(driver, workingDir, fallback, "EXE install timed out", -1)
	}
	if compare.InstallSucceeded(result.ExitCode) {
		return result.ExitCode, nil
	}
	// The silent installer ran and exited non-zero. Report the concrete exit
	// code (nil error) so the caller can decide on an interactive rerun, after
	// giving the automatic extracted fallback a chance first.
	return finishEXEFallback(driver, workingDir, fallback, fmt.Sprintf("silent install exit %d", result.ExitCode), result.ExitCode)
}

// finishEXEFallback gives the automatic extracted-package fallback one chance
// to recover a failed silent EXE install. Under the InstallDriverFile contract
// it returns (0, nil) on fallback success, (code, nil) when a concrete exit
// code is available (the caller may retry interactively), and (code, err) only
// when no exit code exists at all (timeout) and the fallback could not help.
func finishEXEFallback(driver *model.Driver, workingDir string, fallback exeFallback, silentErr string, silentCode int) (int, error) {
	fallbackCode, used := fallback(driver, workingDir)
	if used && fallbackCode == 0 {
		return 0, nil
	}
	if used {
		return fallbackCode, nil
	}
	if silentCode >= 0 {
		return silentCode, nil
	}
	return silentCode, fmt.Errorf("%s", silentErr)
}

// silentInstallerArgs builds the silent-EXE command line from the official
// package parameters only. It never injects a family-specific silent default.
// The only family signal honored is the Inno-only /VERYSILENT flag, whose
// presence is the evidence that justifies appending the Inno /LOG flag.
// ok=false means the package has no usable silent parameters, and the caller
// must not launch it as a silent install.
func silentInstallerArgs(driver *model.Driver, logPath string) ([]string, bool) {
	raw := driver.InstallParameter
	if raw == "" {
		raw = driver.InstallCode
	}
	if raw == "" || strings.HasPrefix(strings.ToLower(raw), "/add-driver") {
		return nil, false
	}
	args := strings.Fields(raw)
	if logPath != "" && hasInnoFlag(args) {
		args = append(args, "/LOG="+logPath)
	}
	return args, true
}

// hasInnoFlag reports whether the official parameters contain /VERYSILENT,
// which is an Inno-only switch. Its presence is the evidence that permits the
// Inno-specific /LOG flag; its absence means no family assumption is made.
func hasInnoFlag(args []string) bool {
	for _, arg := range args {
		if strings.EqualFold(arg, "/VERYSILENT") {
			return true
		}
	}
	return false
}

// HasSilentParameters reports whether a driver carries official silent
// installer parameters, so orchestration can choose between the silent path
// and an interactive fallback without guessing an installer family.
func HasSilentParameters(driver *model.Driver) bool {
	_, ok := silentInstallerArgs(driver, "")
	return ok
}

func installINFPaths(paths []string, workingDir string) (int, error) {
	if len(paths) == 0 {
		return 2, fmt.Errorf("no .inf found inside archive")
	}
	exitCode := 0
	for _, inf := range paths {
		code, err := installNativeOrPnPUtil(inf, workingDir)
		if err != nil {
			return code, err
		}
		if code != 0 {
			exitCode = code
		}
	}
	return exitCode, nil
}

// installNativeOrPnPUtil installs one INF through DiInstallDriverW and falls
// back to pnputil when the native call fails. Both paths normalize a
// reboot-required result to errorSuccessRebootRequired (3010).
func installNativeOrPnPUtil(infPath, workingDir string) (int, error) {
	reboot, err := InstallNativeINF(infPath)
	if err == nil {
		if reboot {
			return errorSuccessRebootRequired, nil
		}
		return 0, nil
	}
	result := RunPnPUtilWithTimeout(infPath, workingDir)
	if result.StartErr != nil {
		return result.ExitCode, result.StartErr
	}
	if result.TimedOut {
		return -1, timeoutErr("pnputil", result)
	}
	return result.ExitCode, nil
}

func collectINFs(root string) ([]string, error) {
	var infs []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.EqualFold(filepath.Ext(path), ".inf") {
			infs = append(infs, path)
		}
		return nil
	})
	return infs, err
}

func installINFs(root, workingDir string) (int, error) {
	infs, err := collectINFs(root)
	if err != nil {
		return -2, err
	}
	return installINFPaths(infs, workingDir)
}

// ExtractZip extracts a zip archive.
func ExtractZip(zipPath, destination string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()
	for _, file := range r.File {
		target := filepath.Join(destination, file.Name)
		if !strings.HasPrefix(target, filepath.Clean(destination)+string(os.PathSeparator)) {
			return fmt.Errorf("unsafe zip path: %s", file.Name)
		}
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		src, err := file.Open()
		if err != nil {
			return err
		}
		dst, err := os.Create(target)
		if err != nil {
			src.Close()
			return err
		}
		_, copyErr := io.Copy(dst, src)
		src.Close()
		closeErr := dst.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}
