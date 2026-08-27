package install

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"lenovo-driver/internal/model"
)

// ProcessResult mirrors the PowerShell process helper result.
type ProcessResult struct {
	ExitCode int
	TimedOut bool
	Stdout   string
	Stderr   string
}

type processRunner func(filePath string, args []string, timeoutSeconds int, workingDirectory string) ProcessResult

type exeFallback func(driver *model.Driver, workingDir string) (int, bool)

// RunProcessWithTimeout mirrors Invoke-ProcessWithTimeout.
func RunProcessWithTimeout(filePath string, args []string, timeoutSeconds int, workingDirectory string) ProcessResult {
	cmd := exec.Command(filePath, args...)
	if workingDirectory != "" {
		cmd.Dir = workingDirectory
	}
	if err := cmd.Start(); err != nil {
		return ProcessResult{ExitCode: -2}
	}
	if timeoutSeconds <= 0 {
		err := cmd.Wait()
		return ProcessResult{ExitCode: processExitCode(err)}
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return ProcessResult{ExitCode: processExitCode(err)}
	case <-time.After(time.Duration(timeoutSeconds) * time.Second):
		_ = killTree(cmd.Process.Pid)
		<-done
		return ProcessResult{ExitCode: -1, TimedOut: true}
	}
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

// RunPnPUtilWithTimeout mirrors Invoke-PnPUtilWithTimeout.
func RunPnPUtilWithTimeout(infPath, workingDir string) ProcessResult {
	args := []string{"/add-driver", infPath, "/install"}
	result := runWithOutput("pnputil.exe", args, 900*time.Second, workingDir)
	if result.ExitCode == 1 {
		// pnputil returns 1 when a reboot is required for an otherwise successful add.
		result.ExitCode = 0
	}
	return result
}

func runWithOutput(filePath string, args []string, timeout time.Duration, workingDir string) ProcessResult {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, filePath, args...)
	if workingDir != "" {
		cmd.Dir = workingDir
	}
	out, err := cmd.CombinedOutput()
	result := ProcessResult{Stdout: string(out)}
	if ctx.Err() == context.DeadlineExceeded {
		_ = killTree(cmd.Process.Pid)
		result.TimedOut = true
		result.ExitCode = -1
		return result
	}
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			result.ExitCode = -2
		}
		return result
	}
	result.ExitCode = 0
	return result
}

func killTree(pid int) error {
	cmd := exec.Command("taskkill.exe", "/PID", fmt.Sprintf("%d", pid), "/T", "/F")
	return cmd.Run()
}

// InstallDriverFile installs one downloaded driver package.
func InstallDriverFile(filePath string, driver *model.Driver, workingDir string) (int, error) {
	if driver == nil {
		return -2, fmt.Errorf("driver is nil")
	}
	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".msi":
		result := RunProcessWithTimeout("msiexec.exe", []string{"/i", filePath, "/qn", "/norestart"}, 1800, workingDir)
		if result.TimedOut {
			return -1, fmt.Errorf("MSI timed out")
		}
		if result.ExitCode == 3010 || result.ExitCode == 1641 {
			return 0, nil
		}
		return result.ExitCode, nil
	case ".inf":
		result := RunPnPUtilWithTimeout(filePath, workingDir)
		if result.TimedOut {
			return -1, fmt.Errorf("pnputil timed out")
		}
		return result.ExitCode, nil
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
		if result.TimedOut {
			return -1, fmt.Errorf("expand.exe timed out")
		}
		if result.ExitCode != 0 {
			return result.ExitCode, fmt.Errorf("expand.exe exited %d", result.ExitCode)
		}
		return installINFs(extract, workingDir)
	case ".exe":
		return installEXE(filePath, driver, workingDir, RunProcessWithTimeout, ExtractedDriverFallback)
	default:
		result := RunProcessWithTimeout(filePath, nil, 1800, workingDir)
		if result.TimedOut {
			return -1, fmt.Errorf("installer timed out")
		}
		return result.ExitCode, nil
	}
}

func installEXE(filePath string, driver *model.Driver, workingDir string, run processRunner, fallback exeFallback) (int, error) {
	logPath := filepath.Join(workingDir, driver.DriverCode+".log")
	args := silentInstallerArgs(driver, logPath)
	result := run(filePath, args, 900, workingDir)
	if result.TimedOut {
		return finishEXEFallback(driver, workingDir, fallback, "EXE timed out", -1)
	}
	if result.ExitCode == 3010 || result.ExitCode == 1641 || result.ExitCode == 0 {
		return 0, nil
	}
	return finishEXEFallback(driver, workingDir, fallback, fmt.Sprintf("silent install exit %d", result.ExitCode), result.ExitCode)
}

func finishEXEFallback(driver *model.Driver, workingDir string, fallback exeFallback, silentErr string, silentCode int) (int, error) {
	fallbackCode, used := fallback(driver, workingDir)
	if used && fallbackCode == 0 {
		return 0, nil
	}
	if used {
		return fallbackCode, fmt.Errorf("%s; extracted fallback exit %d", silentErr, fallbackCode)
	}
	return silentCode, fmt.Errorf("%s", silentErr)
}

func silentInstallerArgs(driver *model.Driver, logPath string) []string {
	args := []string{"/VERYSILENT", "/SUPPRESSMSGBOXES", "/NORESTART"}
	if logPath != "" {
		args = append(args, "/LOG="+logPath)
	}
	installCode := driver.InstallParameter
	if installCode == "" {
		installCode = driver.InstallCode
	}
	if installCode != "" && !strings.HasPrefix(strings.ToLower(installCode), "/add-driver") {
		args = append(args, strings.Fields(installCode)...)
	}
	return args
}

func installINFs(root, workingDir string) (int, error) {
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
	if err != nil {
		return -2, err
	}
	if len(infs) == 0 {
		return 2, fmt.Errorf("no .inf found inside archive")
	}
	exitCode := 0
	for _, inf := range infs {
		result := RunPnPUtilWithTimeout(inf, workingDir)
		if result.TimedOut {
			return -1, fmt.Errorf("pnputil timed out")
		}
		if result.ExitCode != 0 {
			exitCode = result.ExitCode
		}
	}
	return exitCode, nil
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

var reLogTempDir = regexp.MustCompile(`[A-Za-z]:\\[^"\r\n]*is-[A-Za-z0-9]+\.tmp`)

// ExtractedDriverFallback uses log evidence first, then NVIDIA Display.Driver only.
func ExtractedDriverFallback(driver *model.Driver, workingDir string) (int, bool) {
	candidates := extractedTempDirs(driver, workingDir)
	for _, dir := range candidates {
		if code, used := installExtractedDir(driver, dir, workingDir); used {
			return code, true
		}
	}
	return -1, false
}

func extractedTempDirs(driver *model.Driver, workingDir string) []string {
	logPath := filepath.Join(workingDir, driver.DriverCode+".log")
	logDir := tempDirFromLog(logPath)
	roots := uniqueStrings([]string{
		filepath.Join(os.Getenv("SystemRoot"), "TempInst"),
		filepath.Join(os.Getenv("TEMP"), "TempInst"),
		os.Getenv("TEMP"),
	})
	cutoff := time.Now().Add(-2 * time.Hour)
	var candidates []string
	if logDir != "" {
		if info, err := os.Stat(logDir); err == nil && info.IsDir() {
			candidates = append(candidates, logDir)
		}
	}
	var rootCandidates []string
	isNvidia := strings.Contains(strings.ToLower(driver.DriverName), "nvidia")
	if logDir == "" && isNvidia {
		for _, root := range roots {
			if root == "" {
				continue
			}
			entries, err := os.ReadDir(root)
			if err != nil {
				continue
			}
			for _, entry := range entries {
				if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "is-") || !strings.HasSuffix(entry.Name(), ".tmp") {
					continue
				}
				info, err := entry.Info()
				if err != nil || info.ModTime().Before(cutoff) {
					continue
				}
				dir := filepath.Join(root, entry.Name())
				displayInfo, displayErr := os.Stat(filepath.Join(dir, "Display.Driver"))
				if displayErr != nil || !displayInfo.IsDir() {
					continue
				}
				rootCandidates = append(rootCandidates, dir)
			}
		}
	}
	sortNewestFirst(uniqueStrings(rootCandidates))
	return uniqueStrings(append(candidates, rootCandidates...))
}

func tempDirFromLog(logPath string) string {
	data, err := os.ReadFile(logPath)
	if err != nil {
		return ""
	}
	matches := reLogTempDir.FindAllString(string(data), -1)
	if len(matches) == 0 {
		return ""
	}
	return strings.TrimRight(matches[len(matches)-1], `/\`)
}

func installExtractedDir(driver *model.Driver, dir, workingDir string) (int, bool) {
	if strings.EqualFold(filepath.Base(dir), "Display.Driver") {
		dir = filepath.Dir(dir)
	}
	for _, name := range []string{"setup.exe", "nvsetup.exe"} {
		path := filepath.Join(dir, name)
		if !fileExists(path) {
			continue
		}
		result := RunProcessWithTimeout(path, silentInstallerArgs(driver, ""), 900, dir)
		if result.TimedOut {
			return -1, true
		}
		if result.ExitCode == 0 || result.ExitCode == 3010 || result.ExitCode == 1641 {
			return 0, true
		}
		return result.ExitCode, true
	}
	var infs []string
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && strings.EqualFold(filepath.Ext(path), ".inf") {
			infs = append(infs, path)
		}
		return nil
	})
	if len(infs) > 0 {
		code := 0
		for _, inf := range infs {
			result := RunPnPUtilWithTimeout(inf, workingDir)
			if result.TimedOut {
				return -1, true
			}
			if result.ExitCode != 0 {
				code = result.ExitCode
			}
		}
		return code, true
	}
	return 0, false
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func sortNewestFirst(paths []string) {
	for i := 1; i < len(paths); i++ {
		for j := i; j > 0; j-- {
			ai, _ := os.Stat(paths[j-1])
			bi, _ := os.Stat(paths[j])
			if ai == nil || bi == nil || bi.ModTime().After(ai.ModTime()) {
				paths[j-1], paths[j] = paths[j], paths[j-1]
			} else {
				break
			}
		}
	}
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
