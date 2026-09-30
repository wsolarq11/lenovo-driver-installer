package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"lenovo-driver/internal/api"
	"lenovo-driver/internal/compare"
	"lenovo-driver/internal/model"
)

const (
	chainAltOSID     = "248"
	chainAltListOSID = "248"
)

// loadRealQuickFixAlt reads the Windows 11 (OSID 248) response recorded from the
// same machine and the same endpoint, with download tokens redacted.
func loadRealQuickFixAlt(t *testing.T) *api.QuickFixResponse {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "quickfix_real_82jq_os248.json"))
	if err != nil {
		t.Fatalf("read OSID 248 fixture: %v", err)
	}
	var resp api.QuickFixResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("decode OSID 248 fixture: %v", err)
	}
	return &resp
}

// TestEvidenceChainAcrossEditions closes the seam the single-edition chain could
// not reach.
//
// SelectLatestDrivers is the identity when a list holds one entry per
// {PartID, DriverName}, which is why the single-edition chain recorded it as
// having no discriminating power. Merging the two real lists gives it teeth:
//
//   - 21 of the groups appear in both editions with different versions, so the
//     group-internal comparison decides which row survives.
//   - 2 groups exist only under OSID 248, so the hasCurrent guard must drop
//     them: a Windows 11-only package must not be offered on Windows 10 even
//     when it is the newest row in its group.
//
// The surviving rows are real decisions about which package lands on this
// machine, so both directions are asserted rather than the output count alone.
func TestEvidenceChainAcrossEditions(t *testing.T) {
	list42 := api.ParseQuickFix(loadRealQuickFix(t), chainOSID)
	list248 := api.ParseQuickFix(loadRealQuickFixAlt(t), chainAltOSID)

	// Mirror the CLI's cross-edition path: fetch both lists, concatenate, then
	// run the ordinary filter and selection over the combined set.
	merged := append(append([]*model.Driver{}, list42...), list248...)
	if len(merged) != 47 {
		t.Fatalf("merged drivers = %d, want 47 (24 from OSID 42 + 23 from OSID 248)", len(merged))
	}

	filtered := filterDriverRows(merged, false)
	// Only the TouchPad readme is dropped, and only once: it ships in both lists.
	if len(filtered) != 46 {
		t.Fatalf("after filterDriverRows = %d, want 46 (one TouchPadReadme.txt per list)", len(filtered))
	}

	selected := compare.SelectLatestDrivers(filtered, chainListOSID)
	// 23 groups survive: the OSID 42 list contributes 23 rows once the readme
	// is filtered, and the two OSID 248-only groups have no OSID 42 entry.
	if len(selected) != 23 {
		t.Fatalf("SelectLatestDrivers over the merged list = %d, want 23 groups that contain OSID 42",
			len(selected))
	}

	// Every selected row must belong to a group that actually offers this OS.
	groups := map[string][]*model.Driver{}
	for _, d := range filtered {
		key := d.PartID + "|" + d.DriverName
		groups[key] = append(groups[key], d)
	}
	altOnly := 0
	for _, d := range selected {
		group := groups[d.PartID+"|"+d.DriverName]
		hasCurrent := false
		for _, g := range group {
			if g.OSID == chainListOSID {
				hasCurrent = true
			}
			if g.OSID == chainAltOSID && g.DriverCode == d.DriverCode {
				altOnly++
			}
		}
		if !hasCurrent {
			t.Errorf("selected %s from a group with no OSID 42 entry: %s", d.DriverCode, d.DriverName)
		}
	}

	// The Windows 11-only packages must not survive: Monitor Driver
	// (DRV202109090051) and RealtekRTL8852AE Wlan (DRV202109090060) exist only
	// under 248, so hasCurrent has to reject both groups outright.
	for _, code := range []string{"DRV202109090051", "DRV202109090060"} {
		for _, d := range selected {
			if d.DriverCode == code {
				t.Errorf("%s ships only for OSID 248 and must not be selected for OSID 42", code)
			}
		}
	}

	// 19 groups differ in version between editions, and for those the newer row
	// wins even though it is filed under the other OS. This is the whole point of
	// merging: Lenovo publishes one package across editions.
	fromAlt := 0
	for _, d := range selected {
		if d.OSID == chainAltOSID {
			fromAlt++
		}
	}
	if fromAlt != 19 {
		t.Errorf("selected rows filed under OSID 248 = %d, want 19", fromAlt)
	}
	t.Logf("23 groups from the merged list; %d rows come from the OSID 248 list, "+
		"2 OSID 248-only groups correctly dropped", fromAlt)

	// Spot-check one concrete group end to end rather than trusting the tally.
	var fn *model.Driver
	for _, d := range selected {
		if d.DriverName == "Lenovo Energy Management 联想电源管理驱动" {
			fn = d
		}
	}
	if fn == nil {
		t.Fatal("Lenovo Energy Management is missing from the merged selection")
	}
	// 15.11.29.13 (OSID 42) versus 15.11.29.65 (OSID 248): the newer one wins.
	if fn.Version != "15.11.29.65" || fn.OSID != chainAltOSID {
		t.Errorf("selected %s version %q from OSID %s, want 15.11.29.65 from OSID 248",
			fn.DriverCode, fn.Version, fn.OSID)
	}
}

// TestEvidenceChainIncludeBiosIsANoOpOnRealLists records why the firmware
// branch cannot be closed on this machine rather than leaving the gap implied.
// Both recorded lists — Windows 10 and Windows 11 — carry no firmware package
// at all, so includeBios=true and includeBios=false select the same rows and the
// flag has nothing to discriminate. The branch's own behaviour is covered in
// compare, where the firmware pattern lives; what this pins is that this chain
// does not and cannot cover it, so the gap stays visible.
func TestEvidenceChainIncludeBiosIsANoOpOnRealLists(t *testing.T) {
	merged := append(
		api.ParseQuickFix(loadRealQuickFix(t), chainOSID),
		api.ParseQuickFix(loadRealQuickFixAlt(t), chainAltOSID)...,
	)
	withBios := compare.SelectLatestDrivers(filterDriverRows(merged, true), chainListOSID)
	withoutBios := compare.SelectLatestDrivers(filterDriverRows(merged, false), chainListOSID)
	if len(withBios) != len(withoutBios) {
		t.Fatalf("includeBios changed the selection from %d to %d rows on lists that contain no firmware",
			len(withoutBios), len(withBios))
	}
	for i := range withBios {
		if withBios[i].DriverCode != withoutBios[i].DriverCode {
			t.Fatalf("includeBios changed row %d from %s to %s on lists that contain no firmware",
				i, withoutBios[i].DriverCode, withBios[i].DriverCode)
		}
	}
	t.Logf("includeBios is a no-op on both recorded lists (%d rows either way); "+
		"the firmware branch itself is covered in compare, not by this chain", len(withoutBios))
}
