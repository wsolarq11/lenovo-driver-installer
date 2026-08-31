//go:build windows

package inventory

import (
	"os"
	"testing"
)

func TestDecodeFirstMultiString(t *testing.T) {
	// UTF-16LE bytes for "one\0two\0\0"
	b := []byte{
		'o', 0, 'n', 0, 'e', 0, 0, 0,
		't', 0, 'w', 0, 'o', 0, 0, 0, 0, 0,
	}
	if got := decodeFirstMultiString(b); got != "one" {
		t.Fatalf("decodeFirstMultiString = %q, want one", got)
	}
	if got := decodeFirstMultiString(nil); got != "" {
		t.Fatalf("decodeFirstMultiString(nil) = %q, want empty", got)
	}
}

func TestExpandWindowsEnv(t *testing.T) {
	old := os.Getenv("TESTVAR")
	os.Setenv("TESTVAR", "abc")
	t.Cleanup(func() {
		os.Setenv("TESTVAR", old)
	})
	cases := map[string]string{
		`%TESTVAR%\x`:           `abc\x`,
		`%SystemRoot%\System32`: os.Getenv("SystemRoot") + `\System32`,
		`no-env`:                `no-env`,
		`%%`:                    ``,
	}
	for in, want := range cases {
		if got := expandWindowsEnv(in); got != want {
			t.Fatalf("expandWindowsEnv(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDecodeASCIIAndSMBIOSString(t *testing.T) {
	raw := []byte{
		'L', 'E', 'N', 'O', 'V', 'O', 0,
		'P', 'F', '2', 0, 0,
	}
	if got := smbiosString(raw, 0, 1); got != "LENOVO" {
		t.Fatalf("smbiosString[1] = %q, want LENOVO", got)
	}
	if got := smbiosString(raw, 0, 2); got != "PF2" {
		t.Fatalf("smbiosString[2] = %q, want PF2", got)
	}
	if got := smbiosString(raw, 0, 0); got != "" {
		t.Fatalf("smbiosString[0] = %q, want empty", got)
	}
	if got := decodeASCII([]byte{'A', 'B', 0, 'C'}); got != "AB" {
		t.Fatalf("decodeASCII = %q, want AB", got)
	}
}

func TestNativeReadStringRejectsNonStringType(t *testing.T) {
	// This tests the type check path without a real registry handle by
	// relying on an invalid handle: the function should return false.
	if got, ok := nativeReadString(0, "X"); ok || got != "" {
		t.Fatalf("nativeReadString(invalid) = %q,%v, want empty,false", got, ok)
	}
}
