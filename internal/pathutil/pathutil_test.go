package pathutil

import "testing"

func TestBaseAndParent(t *testing.T) {
	cases := []struct {
		path, base, parent string
	}{
		{`C:\Windows\System32\DriverStore\FileRepository\oem1.inf`, "oem1.inf", `C:\Windows\System32\DriverStore\FileRepository`},
		{`C:\temp\is-ABC.tmp\`, "is-ABC.tmp", `C:\temp`},
		{`/tmp/is-ABC.tmp`, "is-ABC.tmp", "/tmp"},
		{"oem1.inf", "oem1.inf", ""},
	}
	for _, tc := range cases {
		if got := Base(tc.path); got != tc.base {
			t.Fatalf("Base(%q) = %q, want %q", tc.path, got, tc.base)
		}
		if got := Parent(tc.path); got != tc.parent {
			t.Fatalf("Parent(%q) = %q, want %q", tc.path, got, tc.parent)
		}
	}
}
