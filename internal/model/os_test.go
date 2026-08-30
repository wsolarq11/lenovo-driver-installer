package model

import "testing"

// TestOSNameByID locks the single OSID-to-name lookup shared by API
// normalization, GUI export, and the interactive OS switcher.
func TestOSNameByID(t *testing.T) {
	osList := []OSListEntry{
		{OSID: "42", OSName: "Windows 10 64-bit"},
		{OSID: "248", OSName: "Windows 11 64-bit"},
	}
	if got := OSNameByID(osList, "248"); got != "Windows 11 64-bit" {
		t.Fatalf("OSNameByID('248') = %q", got)
	}
	if got := OSNameByID(osList, "999"); got != "" {
		t.Fatalf("OSNameByID(unknown) = %q, want empty", got)
	}
	if got := OSNameByID(nil, "42"); got != "" {
		t.Fatalf("OSNameByID(nil) = %q, want empty", got)
	}
}
