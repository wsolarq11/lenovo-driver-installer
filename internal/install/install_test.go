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
	driver := &model.Driver{DriverCode: "d1", DriverName: "Test Driver", InstallParameter: "/VERYSILENT"}
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
		&model.Driver{DriverCode: "d1", InstallParameter: "/VERYSILENT"},
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
		&model.Driver{DriverCode: "d1", InstallParameter: "/VERYSILENT"},
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
		&model.Driver{DriverCode: "d1", InstallParameter: "/VERYSILENT"},
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

func TestNormalizePnPUtilExitCode(t *testing.T) {
	for code, want := range map[int]int{
		0:    0,
		1:    errorSuccessRebootRequired,
		3010: 3010,
		-1:   -1,
	} {
		if got := normalizePnPUtilExitCode(code); got != want {
			t.Fatalf("normalizePnPUtilExitCode(%d) = %d, want %d", code, got, want)
		}
	}
}

func TestSilentInstallerArgsDoesNotGuessFamily(t *testing.T) {
	driver := &model.Driver{
		DriverCode:       "d1",
		InstallParameter: "/VERYSILENT /NORESTART",
	}
	args, ok := silentInstallerArgs(driver, `C:\tmp\d1.log`)
	if !ok {
		t.Fatal("official Inno parameters should be usable")
	}
	joined := strings.Join(args, " ")
	if strings.Contains(joined, "/SUPPRESSMSGBOXES") {
		t.Fatalf("Inno default should not be injected: %#v", args)
	}
	if !strings.Contains(joined, "/LOG=") {
		t.Fatalf("Inno log flag should be appended for /VERYSILENT: %#v", args)
	}
	if !strings.Contains(joined, "/VERYSILENT") || !strings.Contains(joined, "/NORESTART") {
		t.Fatalf("official parameters should be preserved: %#v", args)
	}
}

func TestSilentInstallerArgsRejectsUnknownFamily(t *testing.T) {
	args, ok := silentInstallerArgs(&model.Driver{DriverCode: "d1"}, "")
	if ok || len(args) != 0 {
		t.Fatalf("empty official parameters must not produce silent args: args=%#v ok=%v", args, ok)
	}
	_, ok = silentInstallerArgs(&model.Driver{DriverCode: "d1", InstallParameter: "/S"}, "")
	if !ok {
		t.Fatal("an NSIS /S parameter is official and should be used verbatim")
	}
}

func TestInstallEXESurfacesRebootRequired(t *testing.T) {
	code, err := installEXE(
		"d1.exe",
		&model.Driver{DriverCode: "d1", InstallParameter: "/VERYSILENT"},
		t.TempDir(),
		func(filePath string, args []string, timeoutSeconds int, workingDirectory string) ProcessResult {
			return ProcessResult{ExitCode: 3010}
		},
		func(driver *model.Driver, workingDir string) (int, bool) {
			return 0, false
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if code != 3010 {
		t.Fatalf("installEXE must preserve reboot-required code: got %d, want 3010", code)
	}
}

func TestInstallEXENoParamsIsTerminal(t *testing.T) {
	called := false
	code, err := installEXE(
		"d1.exe",
		&model.Driver{DriverCode: "d1", FileName: "d1.exe"},
		t.TempDir(),
		func(filePath string, args []string, timeoutSeconds int, workingDirectory string) ProcessResult {
			called = true
			return ProcessResult{ExitCode: 0}
		},
		func(driver *model.Driver, workingDir string) (int, bool) {
			return 0, false
		},
	)
	if err == nil || !strings.Contains(err.Error(), "no official silent install parameters") {
		t.Fatalf("missing silent parameters should be a terminal error: code=%d err=%v", code, err)
	}
	if called {
		t.Fatal("installEXE must not launch an EXE without official silent parameters")
	}
}
