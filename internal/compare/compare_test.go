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
		got := parseVersionString(tc.input)
		if got == nil || got.String() != tc.want {
			t.Fatalf("parseVersionString(%q) = %v, want %q", tc.input, got, tc.want)
		}
	}
	if got := parseVersionString("Intel/Realtek mixed"); got != nil {
		t.Fatalf("multi-vendor input should return nil, got %v", got)
	}
}

func TestVersionCompare(t *testing.T) {
	a := parseVersionString("1.0.0.1")
	b := parseVersionString("2.0.0.5")
	if a.Compare(b) >= 0 {
		t.Fatal("1.0.0.1 should be older than 2.0.0.5")
	}
	if b.Compare(a) <= 0 {
		t.Fatal("2.0.0.5 should be newer than 1.0.0.1")
	}
	if a.Compare(parseVersionString("1.0.0.1")) != 0 {
		t.Fatal("same version should compare equal")
	}
}

func TestCompareDriverStatus(t *testing.T) {
	measured := func(v string) model.LocalEvidence {
		return model.ObservedVersion(v)
	}
	cases := []struct {
		name           string
		remote, vendor string
		local          model.LocalEvidence
		want           model.CompareStatus
	}{
		{"behind", "2.0.0.5", "", measured("1.0.0.1"), "Update"},
		{"same", "2.0.0.5", "", measured("2.0.0.5"), "Up to date"},
		{"ahead", "1.0.0.1", "", measured("2.0.0.5"), "Local newer"},
		{"absent", "2.0.0.5", "", model.ObservedAbsence(), "Not installed"},
		{"composite remote under unknown vendor", "2.0.0.5/3.0.0.1", "Intel", measured("3.0.0.1"), "Unknown"},
	}
	for _, tc := range cases {
		if got := CompareDriverStatus(tc.remote, tc.local, tc.vendor); got != tc.want {
			t.Fatalf("%s: CompareDriverStatus(%q,%v,%q) = %q, want %q",
				tc.name, tc.remote, tc.local, tc.vendor, got, tc.want)
		}
	}
}

// TestUnobservedIsNotAbsence pins the distinction the evidence kinds exist for.
// A driver with no device on this machine measured nothing, so calling it Not
// installed would assert absence from silence — and Not installed is the one
// status the automatic install set acts on unattended.
func TestUnobservedIsNotAbsence(t *testing.T) {
	unobserved := model.NotObserved()
	got := CompareDriverStatus("2.0.0.5", unobserved, "")
	if got != model.StatusUnknown {
		t.Fatalf("unobserved local evidence = %q, want Unknown", got)
	}
	if model.InAutomaticInstallSet(got) {
		t.Fatal("an unobserved driver must not enter the automatic install set")
	}
	if model.StatusEvidenceBasis(got) != model.BasisUndetermined {
		t.Fatalf("unobserved status basis = %q, want undetermined", model.StatusEvidenceBasis(got))
	}
}

// TestConflictedEvidenceIsUnknown pins that a measured disagreement yields no
// direction. The conflict is a fact about the machine; the ordering is not
// decidable from it.
func TestConflictedEvidenceIsUnknown(t *testing.T) {
	conflicted := model.ObservedConflict([]string{"30.0.15026.8004", "31.0.14052.9003"})
	if got := CompareDriverStatus("27.20.15026.8004", conflicted, "AMD"); got != model.StatusUnknown {
		t.Fatalf("conflicted local evidence = %q, want Unknown", got)
	}
	if model.InAutomaticInstallSet(model.StatusUnknown) {
		t.Fatal("Unknown must stay out of the automatic install set")
	}
}

func TestHardwareMatchFunction(t *testing.T) {
	pnp := "PCI\\VEN_1002&DEV_1638&SUBSYS_380317AA&REV_C5"
	if !hardwareMatches("1002_1638", pnp, pnp) {
		t.Fatal("AMD hardware id should match")
	}
	if hardwareMatches("10EC_0257", pnp, pnp) {
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
	got, err := ConvertToBytes("1 MB")
	if err != nil || got != 1024*1024 {
		t.Fatalf("1 MB = %d, err=%v", got, err)
	}
	got, err = ConvertToBytes("100 MB")
	if err != nil || got != 100*1024*1024 {
		t.Fatalf("100 MB = %d, err=%v", got, err)
	}
}

func TestConvertToBytesRejectsMalformedSize(t *testing.T) {
	for _, input := range []string{"1,234 MB", "unknown 123", "12 MB/s"} {
		if _, err := ConvertToBytes(input); err == nil {
			t.Fatalf("ConvertToBytes(%q) should fail", input)
		}
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

func TestVendorLookupIsShared(t *testing.T) {
	if got := deviceVendorFor([]string{"Intel Wireless", "Realtek Ethernet"}); got != "Intel" {
		t.Fatalf("deviceVendorFor = %q", got)
	}
	if got := matchVendor("MediaTek Wi-Fi 6E"); got != "MediaTek" {
		t.Fatalf("matchVendor = %q", got)
	}
	if got := GetMatchingRemoteComponent("1.0.0.1/MediaTek Wi-Fi 6E 2.0.0.1", "MediaTek"); got == nil || got.String() != "2.0.0.1" {
		t.Fatalf("GetMatchingRemoteComponent = %v", got)
	}
}

func TestDriverApplicableBluetoothVendorSpecific(t *testing.T) {
	realtek := []model.Device{{Name: "Realtek Bluetooth Adapter", Class: "Bluetooth"}}
	intel := []model.Device{{Name: "Intel Wireless Bluetooth", Class: "Bluetooth"}}
	driver := &model.Driver{DriverName: "BlueTooth 8852AE"}
	if !TestDriverApplicable(driver, realtek) {
		t.Fatal("Realtek 8852AE driver should be applicable to Realtek Bluetooth")
	}
	if TestDriverApplicable(driver, intel) {
		t.Fatal("Realtek 8852AE driver should not be applicable to Intel Bluetooth")
	}
}

func TestConvertToBytesFractionalUsesExactScaling(t *testing.T) {
	got, err := ConvertToBytes("1.5 MB")
	if err != nil || got != 1572864 {
		t.Fatalf("1.5 MB = %d, err=%v", got, err)
	}
}

func TestSizeToleranceUsesIntegerPercent(t *testing.T) {
	if got := sizeTolerance(100 * 1024 * 1024); got != 2097152 {
		t.Fatalf("100 MB tolerance = %d, want 2097152", got)
	}
}

func TestInstallSucceeded(t *testing.T) {
	cases := []struct {
		name     string
		exitCode int
		want     bool
	}{
		{"clean zero", 0, true},
		{"reboot code 3010", 3010, true},
		{"reboot code 1641", 1641, true},
		{"generic failure", 1, false},
		{"msi generic failure", 1603, false},
		{"large failure", 9999, false},
	}
	for _, tc := range cases {
		if got := InstallSucceeded(tc.exitCode); got != tc.want {
			t.Fatalf("InstallSucceeded(%d) = %v, want %v", tc.exitCode, got, tc.want)
		}
	}
}

func TestDriverMatchRulesHaveSequentialPriority(t *testing.T) {
	for i, rule := range driverMatchRules {
		if rule.priority != i+1 {
			t.Fatalf("driverMatchRules[%d].priority = %d, want %d (slice order must match priority)",
				i, rule.priority, i+1)
		}
	}
}
