package gui

import "testing"

func TestDeeplinkOSID(t *testing.T) {
	cases := []struct {
		link string
		want string
	}{
		{"lenovo-driver://audit?osid=248", "248"},
		{"lenovo-driver://audit", ""},
		{"lenovo-driver:///audit?osid=42", "42"},
		{"lenovo-driver://audit?osid=", ""},
		{"lenovo-driver://other?osid=248", ""},
		{"https://example.com/audit?osid=248", ""},
		{"not a url", ""},
	}
	for _, c := range cases {
		if got := deeplinkOSID(c.link); got != c.want {
			t.Errorf("deeplinkOSID(%q) = %q, want %q", c.link, got, c.want)
		}
	}
}
