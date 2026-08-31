//go:build windows

package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNativeInstallSmoke is an opt-in real-machine check that calls
// DiInstallDriverW with a missing INF and verifies the pnputil fallback path.
// It never installs a real driver.
func TestNativeInstallSmoke(t *testing.T) {
	if os.Getenv("LENOVO_NATIVE_INSTALL_SMOKE") != "1" {
		t.Skip("set LENOVO_NATIVE_INSTALL_SMOKE=1 to run native install API smoke")
	}
	missing := filepath.Join(t.TempDir(), "does-not-exist.inf")
	_, err := InstallNativeINF(missing)
	if err == nil {
		t.Fatal("InstallNativeINF should fail for a missing INF")
	}
	if !strings.Contains(err.Error(), "DiInstallDriver failed") {
		t.Fatalf("expected DiInstallDriver failure, got: %v", err)
	}
	code, fallbackErr := installNativeOrPnPUtil(missing, t.TempDir())
	if fallbackErr == nil && code == 0 {
		t.Fatal("expected pnputil fallback to fail for a missing INF")
	}
}
