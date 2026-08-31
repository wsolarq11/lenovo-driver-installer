//go:build windows

package inventory

import (
	"encoding/binary"
	"os"
	"testing"
)

func TestDecodeUTF16MultiString(t *testing.T) {
	// UTF-16LE bytes for "one\0two\0\0"; decodeUTF16 stops at the first NUL.
	b := []byte{
		'o', 0, 'n', 0, 'e', 0, 0, 0,
		't', 0, 'w', 0, 'o', 0, 0, 0, 0, 0,
	}
	if got := decodeUTF16(b); got != "one" {
		t.Fatalf("decodeUTF16(multi) = %q, want one", got)
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

func TestFormatFileTime(t *testing.T) {
	if got := formatFileTime([8]byte{}); got != "" {
		t.Fatalf("formatFileTime(zero) = %q, want empty", got)
	}
	var ft [8]byte
	// 2020-01-01 00:00:00 UTC as FILETIME ticks (100ns since 1601-01-01 UTC).
	binary.LittleEndian.PutUint64(ft[:], 132223104000000000)
	if got := formatFileTime(ft); got != "2020-01-01T00:00:00Z" {
		t.Fatalf("formatFileTime(2020-01-01) = %q, want 2020-01-01T00:00:00Z", got)
	}
}

func TestIsStringType(t *testing.T) {
	for typ, want := range map[uint32]bool{
		1: true, // REG_SZ
		2: true, // REG_EXPAND_SZ
		0: false,
		3: false, // REG_BINARY
		4: false, // REG_DWORD
		7: false, // REG_MULTI_SZ (handled by decodeUTF16's first-NUL stop)
	} {
		if got := isStringType(typ); got != want {
			t.Fatalf("isStringType(%d) = %v, want %v", typ, got, want)
		}
	}
}
