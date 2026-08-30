package install

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"lenovo-driver/internal/model"
)

func TestExtractedDriverFallbackPrefersLogDir(t *testing.T) {
	work := t.TempDir()
	tempRoot := t.TempDir()
	t.Setenv("TEMP", tempRoot)
	extractDir := filepath.Join(tempRoot, "is-ABC123.tmp")
	if err := os.MkdirAll(extractDir, 0o755); err != nil {
		t.Fatal(err)
	}
	setupPath := filepath.Join(extractDir, "setup.exe")
	if err := os.WriteFile(setupPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(work, "d1.log")
	if err := os.WriteFile(logPath, []byte("Destination: "+extractDir+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	driver := &model.Driver{DriverCode: "d1", DriverName: "Test Driver"}
	_, used := ExtractedDriverFallback(driver, work)
	if !used {
		t.Fatal("fallback should use the log-pinned extraction directory")
	}
}

func TestTempDirFromLog(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "d1.log")
	content := "Destination: C:\\Windows\\TempInst\\is-ABC123.tmp\n"
	if err := os.WriteFile(logPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := tempDirFromLog(logPath); got != `C:\Windows\TempInst\is-ABC123.tmp` {
		t.Fatalf("tempDirFromLog = %q", got)
	}
}

func TestInstallEXETimeoutAttemptsFallback(t *testing.T) {
	fallbackCalled := false
	code, err := installEXE(
		"d1.exe",
		&model.Driver{DriverCode: "d1"},
		t.TempDir(),
		func(filePath string, args []string, timeoutSeconds int, workingDirectory string) ProcessResult {
			return ProcessResult{ExitCode: -1, TimedOut: true}
		},
		func(driver *model.Driver, workingDir string) (int, bool) {
			fallbackCalled = true
			return 0, true
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 {
		t.Fatalf("installEXE timeout code = %d, want 0", code)
	}
	if !fallbackCalled {
		t.Fatal("EXE timeout did not attempt extracted fallback")
	}
}

func TestInstallEXESurfacesCleanExitWhenFallbackUnused(t *testing.T) {
	code, err := installEXE(
		"d1.exe",
		&model.Driver{DriverCode: "d1"},
		t.TempDir(),
		func(filePath string, args []string, timeoutSeconds int, workingDirectory string) ProcessResult {
			return ProcessResult{ExitCode: 1603}
		},
		func(driver *model.Driver, workingDir string) (int, bool) {
			return 0, false
		},
	)
	if err != nil {
		t.Fatalf("unexpected error for clean non-zero exit: %v", err)
	}
	if code != 1603 {
		t.Fatalf("installEXE failure code = %d, want 1603", code)
	}
}

func TestInstallEXETimeoutWithoutFallbackIsHardError(t *testing.T) {
	code, err := installEXE(
		"d1.exe",
		&model.Driver{DriverCode: "d1"},
		t.TempDir(),
		func(filePath string, args []string, timeoutSeconds int, workingDirectory string) ProcessResult {
			return ProcessResult{ExitCode: -1, TimedOut: true}
		},
		func(driver *model.Driver, workingDir string) (int, bool) {
			return 0, false
		},
	)
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("timeout with no usable fallback should be a terminal error: %v", err)
	}
	if code != -1 {
		t.Fatalf("installEXE timeout code = %d, want -1", code)
	}
}

func TestExtractedTempDirsScansRecentRootsOnlyForNvidia(t *testing.T) {
	tempRoot := t.TempDir()
	t.Setenv("TEMP", tempRoot)
	t.Setenv("SystemRoot", t.TempDir())
	recentDir := filepath.Join(tempRoot, "is-RECENT.tmp")
	if err := os.MkdirAll(recentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()

	if got := extractedTempDirs(&model.Driver{DriverCode: "d1", DriverName: "Audio"}, work); len(got) != 0 {
		t.Fatalf("non-NVIDIA fallback scanned recent temp roots: %#v", got)
	}

	nvidiaDir := filepath.Join(recentDir, "Display.Driver")
	if err := os.MkdirAll(nvidiaDir, 0o755); err != nil {
		t.Fatal(err)
	}
	got := extractedTempDirs(&model.Driver{DriverCode: "d2", DriverName: "NVIDIA Graphics"}, work)
	if len(got) != 1 || filepath.Clean(got[0]) != filepath.Clean(recentDir) {
		t.Fatalf("NVIDIA fallback candidates = %#v, want %q", got, recentDir)
	}
}

func TestRunProcessTimeoutKillsTree(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("kill-tree path uses taskkill.exe and requires Windows")
	}
	start := time.Now()
	result := runProcess(context.Background(), "powershell.exe",
		[]string{"-NoProfile", "-Command", "Start-Sleep -Seconds 30"},
		500*time.Millisecond, "")
	if !result.TimedOut {
		t.Fatalf("expected timeout, got %#v", result)
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("kill after timeout took too long: %v", elapsed)
	}
}

// TestInstallDriverFileStartFailureIsTerminal guards the InstallDriverFile
// contract: a sub-process that cannot be started at all has no usable exit
// code, so it must surface as err != nil (not as a concrete "exit code -2").
func TestInstallDriverFileStartFailureIsTerminal(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("start-failure path exercises Windows process-start semantics")
	}
	missing := filepath.Join(t.TempDir(), "launcher.bin")
	code, err := InstallDriverFile(missing, &model.Driver{DriverCode: "d1"}, t.TempDir())
	if err == nil {
		t.Fatalf("start failure must surface a terminal error, got code=%d err=nil", code)
	}
}

// TestRunProcessStartFailureCarriesError checks that runProcess records the
// start error instead of collapsing it into a bare "-2".
func TestRunProcessStartFailureCarriesError(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("start-failure path exercises Windows process-start semantics")
	}
	missing := filepath.Join(t.TempDir(), "missing.exe")
	result := runProcess(context.Background(), missing, nil, 0, "")
	if result.StartErr == nil {
		t.Fatal("runProcess should surface a start error for a missing executable")
	}
}
