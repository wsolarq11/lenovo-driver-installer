//go:build windows

package trust

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestVerifyLenovoPackageSmoke verifies a real Lenovo-signed package end to end
// through WinVerifyTrust (with revocation checking) and the signer whitelist.
// It is opt-in because it depends on a downloaded official package.
func TestVerifyLenovoPackageSmoke(t *testing.T) {
	if os.Getenv("LENOVO_TRUST_SMOKE") != "1" {
		t.Skip("set LENOVO_TRUST_SMOKE=1 to verify a real Lenovo-signed package")
	}
	path := os.Getenv("LENOVO_TRUST_SMOKE_FILE")
	if path == "" {
		path = filepath.Join(os.TempDir(), "LenovoDrivers", "DRV202009030023_FN-01LF02AFAR2W6JB0.exe")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("Lenovo package not found at %s; set LENOVO_TRUST_SMOKE_FILE to a real Lenovo-signed package: %v", path, err)
	}
	if err := VerifyFileSignature(path); err != nil {
		t.Fatalf("VerifyFileSignature(%s) = %v", path, err)
	}
}

// TestVerifyRejectsNonLenovoSignerSmoke verifies the interception face against
// a real non-Lenovo Authenticode-signed binary: a valid signature from a
// non-Lenovo publisher must be rejected even though the chain is valid. Point
// LENOVO_TRUST_SMOKE_FILE at one (a git or node executable); the default tries
// git.exe.
func TestVerifyRejectsNonLenovoSignerSmoke(t *testing.T) {
	if os.Getenv("LENOVO_TRUST_SMOKE") != "1" {
		t.Skip("set LENOVO_TRUST_SMOKE=1 to verify non-Lenovo rejection")
	}
	path := os.Getenv("LENOVO_TRUST_SMOKE_FILE")
	if path == "" {
		path = filepath.Join(os.Getenv("ProgramFiles"), "Git", "cmd", "git.exe")
	}
	if _, err := os.Stat(path); err != nil {
		t.Skipf("non-Lenovo sample not found at %s: %v", path, err)
	}
	err := VerifyFileSignature(path)
	if err == nil {
		t.Fatal("non-Lenovo signer must be rejected")
	}
	if !strings.Contains(err.Error(), "Lenovo") {
		t.Fatalf("expected a Lenovo-whitelist rejection, got %v", err)
	}
}
