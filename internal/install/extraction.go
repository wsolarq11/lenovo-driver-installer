package install

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"lenovo-driver/internal/compare"
	"lenovo-driver/internal/model"
)

// This file owns the extracted-package fallback used when a silent installer
// fails to finalize on its own (in practice the NVIDIA Display.Driver
// compatibility heuristic). Keeping it in one place bounds vendor-specific
// quirk growth to a single module instead of scattering it across the generic
// install dispatcher.

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
	rootCandidates = sortNewestFirst(rootCandidates)
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
	args, hasArgs := silentInstallerArgs(driver, "")
	if hasArgs {
		for _, name := range []string{"setup.exe", "nvsetup.exe"} {
			path := filepath.Join(dir, name)
			if !fileExists(path) {
				continue
			}
			result := RunProcessWithTimeout(path, args, 900, dir)
			if result.TimedOut {
				return -1, true
			}
			if compare.InstallSucceeded(result.ExitCode) {
				return result.ExitCode, true
			}
			return result.ExitCode, true
		}
	}
	infs, _ := collectINFs(dir)
	if len(infs) > 0 {
		code, err := installINFPaths(infs, workingDir)
		if err != nil {
			if strings.Contains(err.Error(), "timed out") {
				return -1, true
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

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// sortNewestFirst is retained for the explicit NVIDIA compatibility heuristic.
func sortNewestFirst(paths []string) []string {
	type dirInfo struct {
		path string
		mod  time.Time
	}
	infos := make([]dirInfo, 0, len(paths))
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		infos = append(infos, dirInfo{path: path, mod: info.ModTime()})
	}
	sort.SliceStable(infos, func(i, j int) bool {
		return infos[i].mod.After(infos[j].mod)
	})
	out := make([]string, 0, len(infos))
	for _, info := range infos {
		out = append(out, info.path)
	}
	return out
}
