package app

import (
	"testing"

	"lenovo-driver/internal/api"
	"lenovo-driver/internal/compare"
	"lenovo-driver/internal/model"
)

// TestFirmwareFilterKeepsRealListsIntact records what the firmware filter does
// on the two lists this machine actually returns. Neither the Windows 10 nor the
// Windows 11 list carries a row matching reFirmware, so includeBios has nothing
// to discriminate here — which is why the cross-edition chain cannot close that
// branch, and why this is asserted rather than left implied.
//
// The branch's own behaviour is exercised with constructed rows below, since
// constructing a firmware package is the only way to reach it: Lenovo does not
// publish one for this model in either edition.
func TestFirmwareFilterKeepsRealListsIntact(t *testing.T) {
	for _, tc := range []struct {
		name string
		load func(*testing.T) *api.QuickFixResponse
	}{
		{"OSID 42", loadRealQuickFix},
		{"OSID 248", loadRealQuickFixAlt},
	} {
		for _, d := range api.ParseQuickFix(tc.load(t), chainOSID) {
			if reFirmware.MatchString(d.DriverName) {
				t.Errorf("%s: %s %q unexpectedly matches the firmware pattern", tc.name, d.DriverCode, d.DriverName)
			}
		}
	}

	merged := append(
		api.ParseQuickFix(loadRealQuickFix(t), chainOSID),
		api.ParseQuickFix(loadRealQuickFixAlt(t), chainAltOSID)...,
	)
	withBios := compare.SelectLatestDrivers(filterDriverRows(merged, true), chainListOSID)
	withoutBios := compare.SelectLatestDrivers(filterDriverRows(merged, false), chainListOSID)
	if len(withBios) != len(withoutBios) {
		t.Fatalf("includeBios changed the row count from %d to %d on lists with no firmware package",
			len(withoutBios), len(withBios))
	}
	t.Logf("includeBios is a no-op on both recorded lists: %d rows either way", len(withoutBios))
}

// TestFirmwareRowsAreDroppedUnlessRequested is the branch the recorded lists
// cannot reach. Each name below is a category the pattern is meant to catch, and
// each row is dropped by default and kept when firmware is explicitly requested.
func TestFirmwareRowsAreDroppedUnlessRequested(t *testing.T) {
	names := []string{
		"BIOS Update 联想 BIOS",
		"UEFI Firmware 固件",
		"Embedded Controller EC 驱动",
		"Intel Management Engine 驱动",
		"TPM 固件",
		"Thunderbolt Controller 驱动",
	}
	var rows []*model.Driver
	for i, name := range names {
		rows = append(rows, &model.Driver{
			DriverCode: "FW" + string(rune('A'+i)), DriverName: name,
			PartID: "900", OSID: chainOSID, FileName: "fw.exe", FileType: "exe",
		})
	}
	// A normal driver alongside them must survive either way.
	rows = append(rows, &model.Driver{
		DriverCode: "OK1", DriverName: "Lenovo Fn and Function Keys", PartID: "1",
		OSID: chainOSID, FileName: "ok.exe", FileType: "exe",
	})

	kept := filterDriverRows(rows, false)
	if len(kept) != 1 || kept[0].DriverCode != "OK1" {
		var codes []string
		for _, d := range kept {
			codes = append(codes, d.DriverCode)
		}
		t.Fatalf("default filter kept %v, want only OK1", codes)
	}
	if all := filterDriverRows(rows, true); len(all) != len(rows) {
		t.Fatalf("includeBios kept %d of %d rows, want all", len(all), len(rows))
	}
}

// TestSelectionOrderIsDeterministic pins the fix for a real defect: PartID is
// not a total sort key. On this machine four PartIDs each cover several groups
// (249 alone holds five different WLAN vendors), so a PartID-only sort left the
// surviving rows in map-iteration order and three consecutive -DryRun exports
// disagreed on row order. Repeating the selection has to produce the same
// sequence every time.
func TestSelectionOrderIsDeterministic(t *testing.T) {
	for _, tc := range []struct {
		name  string
		build func(*testing.T) []*model.Driver
	}{
		{"OSID 42", func(t *testing.T) []*model.Driver { return api.ParseQuickFix(loadRealQuickFix(t), chainOSID) }},
		{"OSID 248", func(t *testing.T) []*model.Driver { return api.ParseQuickFix(loadRealQuickFixAlt(t), chainAltOSID) }},
		{"merged", func(t *testing.T) []*model.Driver {
			return append(
				api.ParseQuickFix(loadRealQuickFix(t), chainOSID),
				api.ParseQuickFix(loadRealQuickFixAlt(t), chainAltOSID)...,
			)
		}},
	} {
		rows := tc.build(t)
		want := codes(compare.SelectLatestDrivers(filterDriverRows(rows, false), chainListOSID))
		// Twenty runs: map iteration order varies per range, so a single repeat
		// could pass by luck.
		for i := 0; i < 20; i++ {
			if got := codes(compare.SelectLatestDrivers(filterDriverRows(rows, false), chainListOSID)); !equal(got, want) {
				t.Fatalf("%s: run %d produced a different order:\n got %v\nwant %v", tc.name, i, got, want)
			}
		}
		t.Logf("%s: %d rows in a stable order across 20 runs", tc.name, len(want))
	}
}

func codes(drivers []*model.Driver) []string {
	out := make([]string, 0, len(drivers))
	for _, d := range drivers {
		out = append(out, d.DriverCode)
	}
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
