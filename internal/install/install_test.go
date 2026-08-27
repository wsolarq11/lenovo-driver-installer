package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

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

func TestInstallEXEReturnsSilentExitWhenFallbackUnused(t *testing.T) {
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
	if err == nil || !strings.Contains(err.Error(), "silent install exit 1603") {
		t.Fatalf("unexpected silent install error: %v", err)
	}
	if code != 1603 {
		t.Fatalf("installEXE failure code = %d, want 1603", code)
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
