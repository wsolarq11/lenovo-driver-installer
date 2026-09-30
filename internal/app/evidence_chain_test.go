package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"lenovo-driver/internal/api"
	"lenovo-driver/internal/compare"
	"lenovo-driver/internal/model"
)

// The chain: official response -> parse -> latest-per-part -> view filter ->
// hardware match -> version compare -> automatic install set -> ledger row ->
// post-install diff -> attribution.
//
// Every input is real 82JQ data captured by the tool's own collectors
// (see internal/inventory/evidence_fixture_dump_test.go and the redacted
// QuickFix response), not a hand-written minimal sample. The numbers asserted
// here are the ones a live -DryRun printed and exported on the same machine.
// That is what makes this a chain rather than a parse test: the moment the
// offline path and the real run disagree, a count mismatches and the test goes
// red. "The evidence agrees" stays a checkable claim instead of a remembered one.
const (
	chainOSID     = "42"
	chainListOSID = "42"
	// The AMD graphics version the official list still ships, older than what
	// this machine already runs. It is the downgrade the automatic set must
	// never re-admit.
	chainLocalSize = "27.20.15026.8004"
)

type chainDevice struct {
	PnpDeviceID   string
	DeviceID      string
	Name          string
	Class         string
	DriverVersion string
	InfName       string
	ProviderName  string
	ProblemNumber int
}

func loadRealQuickFix(t *testing.T) *api.QuickFixResponse {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "quickfix_real_82jq.json"))
	if err != nil {
		t.Fatalf("read real QuickFix fixture: %v", err)
	}
	var resp api.QuickFixResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("decode real QuickFix fixture: %v", err)
	}
	return &resp
}

func loadRealDevices(t *testing.T) []model.Device {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "local_devices_82jq.json"))
	if err != nil {
		t.Fatalf("read device fixture: %v", err)
	}
	var raw []chainDevice
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("decode device fixture: %v", err)
	}
	devices := make([]model.Device, 0, len(raw))
	for _, r := range raw {
		devices = append(devices, model.Device{
			Name: r.Name, Class: r.Class, DeviceID: r.DeviceID,
			PnpDeviceID: r.PnpDeviceID, DriverVersion: r.DriverVersion,
			InfName: r.InfName, ProviderName: r.ProviderName,
			ProblemNumber: r.ProblemNumber,
		})
	}
	return devices
}

func loadRealSoftware(t *testing.T) *model.SoftwareSnapshot {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "local_software_82jq.json"))
	if err != nil {
		t.Fatalf("read software fixture: %v", err)
	}
	var snap model.SoftwareSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		t.Fatalf("decode software fixture: %v", err)
	}
	if snap.LenovoFnServiceVersion == "" || snap.ProvisionedAmdPower == "" {
		t.Fatalf("software fixture lost the two service versions the compare rules read: %+v", snap)
	}
	return &snap
}

// chainAssess runs the same four steps the live view runs, in order.
func chainAssess(t *testing.T) []*model.AssessedDriver {
	t.Helper()
	devices := loadRealDevices(t)
	snapshot := loadRealSoftware(t)
	if len(devices) != 169 {
		t.Fatalf("device fixture shrank: got %d, want 169", len(devices))
	}
	// A fixture that lost its version column would quietly push every compare to
	// "undetermined" and still satisfy a structural assertion.
	withVersion := 0
	for _, d := range devices {
		if d.DriverVersion != "" {
			withVersion++
		}
	}
	if withVersion < 160 {
		t.Fatalf("device fixture lost versions: %d/%d carry one", withVersion, len(devices))
	}

	all := api.ParseQuickFix(loadRealQuickFix(t), chainOSID)
	if len(all) == 0 {
		t.Fatal("real QuickFix response parsed to zero drivers")
	}
	// Order matters and mirrors internal/app/view.go: filter first (247), then
	// collapse each part to its newest entry (248). Reversing the two happens to
	// give the same answer on this machine, which is exactly why the order has
	// to be read off the source rather than assumed.
	filtered := filterDriverRows(all, false)
	if len(filtered) == 0 {
		t.Fatal("view filter removed every driver")
	}
	drivers := compare.SelectLatestDrivers(filtered, chainListOSID)
	if len(drivers) == 0 {
		t.Fatal("latest-driver selection produced nothing")
	}
	t.Logf("parsed %d -> %d installable -> %d latest; %d local devices, %d local apps",
		len(all), len(filtered), len(drivers), len(devices), len(snapshot.InstalledApps))

	assessed := make([]*model.AssessedDriver, 0, len(drivers))
	for _, d := range drivers {
		ad := &model.AssessedDriver{Driver: d}
		if !compare.TestDriverApplicable(d, devices) {
			ad.CompareStatus = model.StatusNotApplicable
			ad.NonMatchReason = compare.DiagnoseDriverNonMatch(d, devices)
			assessed = append(assessed, ad)
			continue
		}
		matched := compare.GetMatchingLocalDevices(d, devices)
		versions := make([]string, 0, len(matched))
		for _, m := range matched {
			versions = append(versions, m.DriverVersion)
		}
		local, vendor := compare.ResolveLocalDriverVersion(d, matched, versions, snapshot)
		ad.LocalVersion = local
		ad.LocalVendor = vendor
		ad.CompareStatus = compare.CompareDriverStatus(d.Version, local, vendor)
		ad.MatchedDeviceIDs = pnpIDsOf(matched)
		assessed = append(assessed, ad)
	}
	return assessed
}

func TestEvidenceChainReal82JQ(t *testing.T) {
	assessed := chainAssess(t)

	counts := map[model.CompareStatus]int{}
	for _, ad := range assessed {
		counts[ad.CompareStatus]++
	}
	t.Logf("status counts: update=%d upToDate=%d notInstalled=%d localNewer=%d notApplicable=%d",
		counts[model.StatusUpdate], counts[model.StatusUpToDate],
		counts[model.StatusNotInstalled], counts[model.StatusLocalNewer],
		counts[model.StatusNotApplicable])

	// Asserted one by one: any step that drifts shows up as a count mismatch.
	// Not installed is 0 and Local newer is 10 because a software-versioned
	// driver now falls back to the device's measured version. Before that fix
	// DRV201907160015 read as "not installed" — and therefore sat in the
	// automatic set — while ACPI\VPC2004 was running Lenovo's oem90.inf at
	// 15.11.29.65, newer than the 15.11.29.13 the list offers.
	wantStatus := map[model.CompareStatus]int{
		model.StatusUpdate:        0,
		model.StatusUpToDate:      1,
		model.StatusNotInstalled:  0,
		model.StatusLocalNewer:    10,
		model.StatusNotApplicable: 12,
	}
	for status, want := range wantStatus {
		if counts[status] != want {
			t.Errorf("status %q = %d, want %d", status, counts[status], want)
		}
	}

	applicable, updates := partitionViewDrivers(assessed)
	// Empty, and that is the correct outcome: this machine has no driver the
	// official list would improve on, so the evidence chain concludes "act on
	// nothing" rather than "act on something we are unsure about".
	if len(applicable) != 0 {
		t.Fatalf("automatic install set = %d, want 0 (live dry-run reports 0)", len(applicable))
	}
	if len(updates) != 0 {
		t.Fatalf("update-only set = %d, want 0", len(updates))
	}
	t.Logf("automatic install set is empty: no driver on 82JQ is measurably behind the list")
}

// TestEvidenceChainLatestSelectionIsIdentityOnThisFixture records why the
// latest-per-part step has no discriminating power on the default path, instead
// of letting a "no-op" pass as if it were verified.
//
// The default view compares against a single OS list, and Lenovo publishes one
// entry per part for OSID 42, so collapsing to the newest is the identity here.
// Cross-edition selection only has teeth under -LatestAcrossOS, which is an
// explicit experimental flag, not the default. If a future response ever ships
// two rows for one part on the current OS, this goes red and the step starts
// mattering — which is exactly when the chain should be re-examined.
func TestEvidenceChainLatestSelectionIsIdentityOnThisFixture(t *testing.T) {
	all := api.ParseQuickFix(loadRealQuickFix(t), chainOSID)
	filtered := filterDriverRows(all, false)

	// The grouping key is {PartID, DriverName}, not PartID alone: Lenovo ships
	// several distinct components under one part number, and collapsing them into
	// one row would silently drop drivers.
	type groupKey struct{ part, name string }
	perGroup := map[groupKey][]string{}
	for _, d := range filtered {
		k := groupKey{d.PartID, d.DriverName}
		perGroup[k] = append(perGroup[k], d.DriverCode)
	}
	multi := 0
	for g, codes := range perGroup {
		if len(codes) > 1 {
			multi++
			t.Logf("group part=%q name=%q has %d rows: %v", g.part, g.name, len(codes), codes)
		}
	}
	selected := compare.SelectLatestDrivers(filtered, chainListOSID)
	if len(selected) != len(filtered) {
		t.Fatalf("selection changed the set: %d -> %d", len(filtered), len(selected))
	}
	if multi > 0 {
		t.Fatalf("%d group(s) carried more than one row yet selection kept every row; "+
			"latest-per-part is no longer collapsing, so the chain must be re-checked", multi)
	}
	t.Logf("%d installable rows across %d groups, one row each: selection is the identity on this fixture",
		len(filtered), len(perGroup))
}

// TestEvidenceChainReachesLedger closes the loop: the device identity the chain
// captured during comparison is the same identity the ledger records and the
// post-install diff attributes. The seam is only real if those ids come from
// the chain, so this reuses the assessed drivers rather than inventing device
// ids the way an isolated ledger test has to.
//
// The automatic set is empty on this machine, so the driver exercised here is
// the one that was wrongly in it before the fallback fix: Local newer is
// excluded from unattended install but stays reachable through the manual "s"
// selection, which is exactly the path whose audit trail has to hold up.
func TestEvidenceChainReachesLedger(t *testing.T) {
	var target *model.AssessedDriver
	for _, ad := range chainAssess(t) {
		if ad.CompareStatus == model.StatusLocalNewer && len(ad.MatchedDeviceIDs) > 0 {
			target = ad
			break
		}
	}
	if target == nil {
		t.Fatal("no Local newer driver matched a real device; the manual path has no evidence to audit")
	}
	ids := target.MatchedDeviceIDs
	t.Logf("%s (%s) targets %d device(s): %v", target.Driver.DriverCode, target.CompareStatus, len(ids), ids)

	app := newTestApp(t)
	app.HistoryPath = filepath.Join(t.TempDir(), "history.csv")
	if err := app.WriteHistoryRecord(target, "Installed", "exit=0", target.Driver.Version, target.LocalVersion); err != nil {
		t.Fatalf("ledger write: %v", err)
	}

	byID := ledgerDevicesByCode(app.ReadHistory(context.Background()))
	targeted := byID[target.Driver.DriverCode]
	if len(targeted) != len(ids) {
		t.Fatalf("ledger carries %d device ids, chain captured %d", len(targeted), len(ids))
	}
	for _, id := range ids {
		if !targeted[id] {
			t.Fatalf("device %q matched during compare but is missing from the ledger row", id)
		}
	}

	// The other half: a post-install diff over the same real ids must attribute
	// back to that one row.
	devices := loadRealDevices(t)
	before := deviceSnapshot{}
	after := deviceSnapshot{}
	for _, id := range ids {
		var name, class, inf, ver string
		for _, d := range devices {
			if d.PnpDeviceID == id {
				name, class, inf, ver = d.Name, d.Class, d.InfName, d.DriverVersion
			}
		}
		before.Devices = append(before.Devices, snapEntry(id, name, class, inf, ver, "2024-01-01", 0))
		after.Devices = append(after.Devices, snapEntry(id, name, class, "oem999.inf", "99.99.99.99", "2024-01-01", 0))
	}
	changes := diffDeviceSnapshots(before, after)
	// The diff reports one change per changed field, not per device, so count
	// distinct devices: attribution is a device-level claim.
	changedDevices := map[string]bool{}
	for _, change := range changes {
		changedDevices[change.PnpDeviceID] = true
	}
	if len(changedDevices) != len(ids) {
		t.Fatalf("post-install diff touched %d devices, want %d", len(changedDevices), len(ids))
	}
	for _, change := range changes {
		if !byID[target.Driver.DriverCode][change.PnpDeviceID] {
			t.Fatalf("changed device %q is not attributable to the ledger row", change.PnpDeviceID)
		}
	}
	t.Logf("ledger round trip ok: %d device(s), %d field changes, all attributable by column not by message text",
		len(changedDevices), len(changes))
}

// TestEvidenceChainLocalNewerStayOut pins the reason the set is small: the
// official list ships an AMD driver older than what is already installed, so any
// chain that widens the set puts a downgrade back on the table.
func TestEvidenceChainLocalNewerStayOut(t *testing.T) {
	assessed := chainAssess(t)

	var newer []string
	for _, ad := range assessed {
		if ad.Driver.Version != chainLocalSize {
			continue
		}
		if compare.CompareDriverStatus(ad.Driver.Version, ad.LocalVersion, ad.LocalVendor) == model.StatusLocalNewer {
			newer = append(newer, ad.Driver.DriverCode+" local="+ad.LocalVersion)
		}
	}
	if len(newer) == 0 {
		t.Fatal("fixture no longer contains the downgrade case it exists to guard")
	}
	if model.InAutomaticInstallSet(model.StatusLocalNewer) {
		t.Fatalf("Local newer must stay out of the automatic set, yet %v qualifies", newer)
	}
	for _, n := range newer {
		t.Logf("excluded: %s", n)
	}
}
