package trust

import (
	"path/filepath"
	"testing"
)

func TestVerifyFileSignatureMissingFile(t *testing.T) {
	// A nonexistent file must fail verification on every platform: on Windows
	// WinVerifyTrust rejects it, and off Windows the stub returns an error.
	path := filepath.Join(t.TempDir(), "missing.exe")
	if err := VerifyFileSignature(path); err == nil {
		t.Fatal("VerifyFileSignature(missing file) should return an error")
	}
}
