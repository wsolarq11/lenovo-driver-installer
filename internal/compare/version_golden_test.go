package compare

import "testing"

// TestParseVersionString82JQGolden locks the real local driver versions
// recorded in docs/records/evidence-82jq.md for the 82JQ machine. These are
// regression fixtures, not synthetic samples: if the parser starts rejecting
// one of them, the comparison silently degrades that driver to Unknown.
func TestParseVersionString82JQGolden(t *testing.T) {
	versions := []string{
		"23.100.0.4",      // Intel WLAN
		"22.160.0.4",      // Bluetooth
		"1.2.0.118",       // AMD Serial-IO
		"6.0.9363.1",      // Realtek Audio
		"10.50.511.2021",  // Realtek LAN
		"2.0.0.25",        // Lenovo Fn
		"30.0.14052.9003", // AMD VGA
		"31.0.15.4630",    // NVIDIA VGA
	}
	for _, version := range versions {
		parsed := ParseVersionString(version)
		if parsed == nil || parsed.String() != version {
			t.Fatalf("ParseVersionString(%q) = %v, want %q", version, parsed, version)
		}
	}
}
