package inventory

import "testing"

func TestNormalizeOSInfoSetsKindAndBits(t *testing.T) {
	cases := []struct {
		row    osInfoRow
		kind   string
		bits   string
		osName string
	}{
		{osInfoRow{Caption: "Microsoft Windows 11 Pro", OSArchitecture: "64-bit"}, "Windows 11", "64-bit", "Windows 11 64-bit"},
		{osInfoRow{Caption: "Microsoft Windows 10", OSArchitecture: "32-bit"}, "Windows 10", "32-bit", "Windows 10 32-bit"},
		{osInfoRow{Caption: "Windows 8.1", OSArchitecture: "64-bit"}, "Windows 8", "64-bit", "Windows 8 64-bit"},
		{osInfoRow{Caption: "Windows 7 Ultimate", OSArchitecture: "32-bit"}, "Windows 7", "32-bit", "Windows 7 32-bit"},
		{osInfoRow{Caption: "Some Unknown OS", OSArchitecture: "x86"}, "Windows", "32-bit", "Windows 32-bit"},
	}
	for _, tc := range cases {
		got := normalizeOSInfo(tc.row)
		if got.Kind != tc.kind || got.Arch != tc.bits || got.OSName != tc.osName {
			t.Fatalf("normalizeOSInfo(%#v) = %#v, want kind=%s bits=%s osname=%s", tc.row, got, tc.kind, tc.bits, tc.osName)
		}
	}
}
