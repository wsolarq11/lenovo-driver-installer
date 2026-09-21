package app

import (
	"os"
	"path/filepath"
	"testing"

	"lenovo-driver/internal/model"
)

func TestFirstInfPath(t *testing.T) {
	// A bare leaf is reconstructed under %SystemRoot%\INF.
	if got := firstInfPath([]model.Device{{InfName: "oem42.inf"}}); got != filepath.Join(infRoot(), "INF", "oem42.inf") {
		t.Fatalf("leaf path = %q", got)
	}
	// An already-qualified path passes through unchanged.
	if got := firstInfPath([]model.Device{{InfName: `C:\Windows\INF\oem42.inf`}}); got != `C:\Windows\INF\oem42.inf` {
		t.Fatalf("qualified path = %q", got)
	}
	// An empty INF is skipped.
	if got := firstInfPath([]model.Device{{}}); got != "" {
		t.Fatalf("empty path = %q", got)
	}
}

func infRoot() string {
	if r := os.Getenv("SystemRoot"); r != "" {
		return r
	}
	return `C:\Windows`
}
