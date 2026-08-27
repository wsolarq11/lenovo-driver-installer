package install

import (
	"os"
	"path/filepath"
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
