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

func TestIsLenovoSubject(t *testing.T) {
	cases := []struct {
		subject string
		want    bool
	}{
		{"CN=Lenovo, OU=G09, O=Lenovo, L=Morrisville, S=North Carolina, C=US", true},
		{"CN=Lenovo (Beijing) Limited, O=Lenovo (Beijing) Limited, C=CN", true},
		{"CN=Example Corp, O=Example, C=US", false},
		{"CN=lenovo pc, O=LENOVO", true},
		{"", false},
	}
	for _, tc := range cases {
		if got := isLenovoSubject(tc.subject); got != tc.want {
			t.Fatalf("isLenovoSubject(%q) = %v, want %v", tc.subject, got, tc.want)
		}
	}
}
