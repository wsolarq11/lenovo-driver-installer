package download

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestHashCompanionRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "driver.exe")
	if err := os.WriteFile(path, []byte("driver bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	hash := FileSHA256(path)
	if hash == "" {
		t.Fatal("SHA-256 should be computable")
	}
	if err := WriteHashCompanion(path, hash); err != nil {
		t.Fatal(err)
	}
	if !TestHashCompanion(path, false) {
		t.Fatal("valid companion should pass")
	}
	if TestHashCompanion(path, false) == false {
		t.Fatal("valid companion should pass")
	}
	if !TestHashCompanion(path, true) {
		t.Fatal("skip flag should bypass companion check")
	}
	_ = os.WriteFile(path, []byte("tampered"), 0o644)
	if TestHashCompanion(path, false) {
		t.Fatal("tampered file should fail companion check")
	}
}

func TestHTTPStatusError(t *testing.T) {
	httpErr := &HTTPStatusError{Status: 403, URL: "https://example.invalid/driver.exe"}
	if !IsHTTPStatus(httpErr, 403) {
		t.Fatal("403 HTTPStatusError should be recognized")
	}
	if IsHTTPStatus(errors.New("download returned HTTP 403"), 403) {
		t.Fatal("plain error should not be recognized")
	}
}

func TestFileMD5(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "driver.exe")
	if err := os.WriteFile(path, []byte("abc"), 0o644); err != nil {
		t.Fatal(err)
	}
	sum := FileMD5(path)
	if len(sum) != 32 {
		t.Fatalf("MD5 length = %d", len(sum))
	}
}

func TestTrustedHost(t *testing.T) {
	cases := []struct {
		host string
		want bool
	}{
		{"download.lenovo.com", true},
		{"download.lenovo.com.cn", true},
		{"lenovo.com", true},
		{"lenovo.com.cn", true},
		{"ptstpd.lenovo.com.cn", true},
		{"newdriverdl.lenovo.com.cn", true}, // verified live CDN host (2026-09)
		{"cdn.example.com", false},
		{"lenovo.com.evil.com", false},
		{"evillenovo.com", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := TrustedHost(tc.host); got != tc.want {
			t.Fatalf("TrustedHost(%q) = %v, want %v", tc.host, got, tc.want)
		}
	}
}
