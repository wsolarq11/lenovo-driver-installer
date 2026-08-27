package compare

import (
	"testing"

	"lenovo-driver/internal/model"
)

func TestParseVersionString(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"31.0.15.4630", "31.0.15.4630"},
		{" 2.0.0.5 ", "2.0.0.5"},
	}
	for _, tc := range cases {
		got := ParseVersionString(tc.input)
		if got == nil || got.String() != tc.want {
			t.Fatalf("ParseVersionString(%q) = %v, want %q", tc.input, got, tc.want)
		}
	}
	if got := ParseVersionString("Intel/Realtek mixed"); got != nil {
		t.Fatalf("multi-vendor input should return nil, got %v", got)
	}
}

func TestVersionCompare(t *testing.T) {
	a := ParseVersionString("1.0.0.1")
	b := ParseVersionString("2.0.0.5")
	if a.Compare(b) >= 0 {
		t.Fatal("1.0.0.1 should be older than 2.0.0.5")
	}
	if b.Compare(a) <= 0 {
		t.Fatal("2.0.0.5 should be newer than 1.0.0.1")
	}
	if a.Compare(ParseVersionString("1.0.0.1")) != 0 {
		t.Fatal("same version should compare equal")
	}
}

func TestCompareDriverStatus(t *testing.T) {
	cases := []struct {
		remote, local, vendor, want string
	}{
		{"2.0.0.5", "1.0.0.1", "", "Update"},
		{"2.0.0.5", "2.0.0.5", "", "Up to date"},
		{"1.0.0.1", "2.0.0.5", "", "Local newer"},
		{"2.0.0.5", "", "", "Not installed"},
		{"2.0.0.5/3.0.0.1", "3.0.0.1", "Intel", "Unknown"},
	}
	for _, tc := range cases {
		if got := CompareDriverStatus(tc.remote, tc.local, tc.vendor); got != tc.want {
			t.Fatalf("CompareDriverStatus(%q,%q,%q) = %q, want %q", tc.remote, tc.local, tc.vendor, got, tc.want)
		}
	}
}

func TestHardwareMatchFunction(t *testing.T) {
	pnp := "PCI\\VEN_1002&DEV_1638&SUBSYS_380317AA&REV_C5"
	if !TestHardwareMatch("1002_1638", pnp, pnp) {
		t.Fatal("AMD hardware id should match")
	}
	if TestHardwareMatch("10EC_0257", pnp, pnp) {
		t.Fatal("Realtek hardware id should not match")
	}
}

func TestDriverApplicableFunction(t *testing.T) {
	devices := []model.Device{
		{Name: "AMD Radeon Graphics", Class: "Display", DeviceID: "PCI\\VEN_1002&DEV_1638", PnpDeviceID: "PCI\\VEN_1002&DEV_1638"},
	}
	amd := &model.Driver{HardwareID: "1002_1638", DriverName: "AMD VGA", Version: "30.0.14052.9003"}
	if !TestDriverApplicable(amd, devices) {
		t.Fatal("AMD driver should be applicable")
	}
	absent := &model.Driver{HardwareID: "", DriverName: "NVIDIA VGA", Version: "1.0"}
	if TestDriverApplicable(absent, devices) {
		t.Fatal("NVIDIA driver should not be applicable")
	}
}

func TestConvertToBytes(t *testing.T) {
	if got := ConvertToBytes("1 MB"); got != 1024*1024 {
		t.Fatalf("1 MB = %d", got)
	}
	if got := ConvertToBytes("100 MB"); got != 100*1024*1024 {
		t.Fatalf("100 MB = %d", got)
	}
}

func TestFileSizeMatchFunction(t *testing.T) {
	if !TestFileSizeMatch(100*1024*1024, 100*1024*1024+100*1024) {
		t.Fatal("size within tolerance should match")
	}
	if TestFileSizeMatch(100*1024*1024, 50*1024*1024) {
		t.Fatal("size outside tolerance should not match")
	}
}

func TestResolveTargetOsEntry(t *testing.T) {
	osList := []model.OSListEntry{{OSID: "42", OSName: "Windows 10 64-bit"}, {OSID: "248", OSName: "Windows 11 64-bit"}}
	if got := ResolveTargetOsEntry(osList, "42"); got == nil || got.OSID != "42" {
		t.Fatalf("OSID lookup failed: %v", got)
	}
	if got := ResolveTargetOsEntry(osList, "Windows 11"); got == nil || got.OSID != "248" {
		t.Fatalf("OS name lookup failed: %v", got)
	}
}

func TestSelectLatestDrivers(t *testing.T) {
	old := &model.Driver{PartID: "p1", DriverName: "Audio", OSID: "42", Version: "1.0.0.1", DriverEditionID: 1}
	new := &model.Driver{PartID: "p1", DriverName: "Audio", OSID: "42", Version: "2.0.0.1", DriverEditionID: 2}
	selected := SelectLatestDrivers([]*model.Driver{old, new}, "42")
	if len(selected) != 1 || selected[0] != new {
		t.Fatalf("latest selection failed: %v", selected)
	}
}

func TestGetVersionMatchKeys(t *testing.T) {
	keys := GetVersionMatchKeys("1.0.0.1/2.0.0.1")
	if len(keys) != 2 || keys[0] != "1.0.0.1" || keys[1] != "2.0.0.1" {
		t.Fatalf("unexpected keys: %v", keys)
	}
}
