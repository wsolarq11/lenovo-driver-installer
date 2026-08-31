//go:build windows

package inventory

import "testing"

func TestDecodeUTF16(t *testing.T) {
	// "Lenovo" in UTF-16LE, terminated by NUL.
	b := []byte{
		0x4c, 0x00, 0x65, 0x00, 0x6e, 0x00, 0x6f, 0x00,
		0x76, 0x00, 0x6f, 0x00, 0x00, 0x00,
	}
	if got := decodeUTF16(b); got != "Lenovo" {
		t.Fatalf("decodeUTF16 = %q, want Lenovo", got)
	}
	if got := decodeUTF16(nil); got != "" {
		t.Fatalf("decodeUTF16(nil) = %q, want empty", got)
	}
	if got := decodeUTF16([]byte{0x41, 0x00}); got != "A" {
		t.Fatalf("decodeUTF16 single char = %q, want A", got)
	}
}
