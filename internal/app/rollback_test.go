package app

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"lenovo-driver/internal/install"
	"lenovo-driver/internal/model"
)

func stubRollback(t *testing.T, fn func(string) (bool, error)) {
	original := rollbackDriverNative
	rollbackDriverNative = fn
	t.Cleanup(func() { rollbackDriverNative = original })
}

func stubRollbackReinstall(t *testing.T, fn func(string, string) (bool, error)) {
	original := rollbackReinstallNative
	rollbackReinstallNative = fn
	t.Cleanup(func() { rollbackReinstallNative = original })
}

func stubRollbackVerify(t *testing.T, fn func(context.Context, string) (int, string, bool, error)) {
	original := rollbackDeviceProblem
	rollbackDeviceProblem = fn
	t.Cleanup(func() { rollbackDeviceProblem = original })
}

// recoveredVerify is the default post-rollback verification stub: the device's
// problem code is back to zero and its version moved off the broken version,
// so the ledger may honestly record RolledBack.
func recoveredVerify(context.Context, string) (int, string, bool, error) { return 0, "1.0", true, nil }

// writePendingOffer records the offer in the WORM ledger (RollbackOffered) and
// writes the JSON cache file, matching what verifyInstalled does.
func writePendingOffer(t *testing.T, app *App, offer rollbackOffer) {
	t.Helper()
	if err := app.WriteHistoryRecord(offer.assessedDriver(), "RollbackOffered", rollbackOfferedMessage(offer), offer.BeforeVersion, offer.AfterVersion); err != nil {
		t.Fatal(err)
	}
	if err := app.writeRollbackOffers([]rollbackOffer{offer}); err != nil {
		t.Fatal(err)
	}
}

func TestRollbackCombinationRejected(t *testing.T) {
	app := newTestApp(t)
	if code := app.Run([]string{"-Rollback", "d1", "-DryRun"}); code != 2 {
		t.Fatalf("-Rollback with -DryRun should exit 2, got %d", code)
	}
}

func TestBuildRollbackOffer(t *testing.T) {
	app := newTestApp(t)
	ad := &model.AssessedDriver{
		Driver: &model.Driver{
			DriverCode:  "d1",
			DriverName:  "Audio",
			Version:     "2.0",
			OSID:        "42",
			OSName:      "Windows 10 64-bit",
			FileName:    "audio.exe",
			OfficialMD5: "abc",
			SourceAPI:   "QuickFix",
		},
		DriverAssessment: model.DriverAssessment{BeforeInfName: "oem42.inf", BeforeInfPath: `C:\Windows\INF\oem42.inf`},
	}
	devices := []model.Device{
		{PnpDeviceID: "A", ProblemNumber: 0, InfName: `C:\Windows\INF\oem99.inf`},
		{PnpDeviceID: "B", DeviceID: `PCI\VEN_0000`, ProblemNumber: 43, InfName: `C:\Windows\INF\oem100.inf`},
		{PnpDeviceID: "", ProblemNumber: 28},
		{PnpDeviceID: "D", ProblemNumber: 28, InfName: `C:\Windows\INF\oem101.inf`},
	}
	offer := app.buildRollbackOffer(ad, "1.0", "2.0", devices)
	if offer.DriverCode != "d1" || offer.BeforeVersion != "1.0" || offer.AfterVersion != "2.0" || offer.BeforeInf != "oem42.inf" || offer.BeforeInfPath != `C:\Windows\INF\oem42.inf` {
		t.Fatalf("offer fields wrong: %#v", offer)
	}
	if offer.State != rollbackStatePending {
		t.Fatalf("offer state = %q, want pending", offer.State)
	}
	wantDevices := []rollbackOfferDevice{
		{PnpDeviceID: "B", HardwareID: `PCI\VEN_0000`, Problem: "failed post start", AfterInf: "oem100.inf"},
		{PnpDeviceID: "D", Problem: "failed install", AfterInf: "oem101.inf"},
	}
	if !reflect.DeepEqual(offer.Devices, wantDevices) {
		t.Fatalf("offer devices = %#v, want %#v", offer.Devices, wantDevices)
	}
}

func TestRollbackOfferFileRoundTrip(t *testing.T) {
	app := newTestApp(t)
	offers := []rollbackOffer{{
		DriverCode: "d1", State: rollbackStatePending, BeforeInf: "oem42.inf",
		Devices: []rollbackOfferDevice{{PnpDeviceID: "B", Problem: "driver failed load"}},
	}}
	if err := app.writeRollbackOffers(offers); err != nil {
		t.Fatal(err)
	}
	got, err := app.readRollbackOffers()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, offers) {
		t.Fatalf("offer round-trip = %#v, want %#v", got, offers)
	}
}

func TestPendingRollbackCodes(t *testing.T) {
	records := []model.HistoryRecord{
		{DriverCode: "d1", Result: "RollbackOffered"},
		{DriverCode: "d2", Result: "RollbackOffered"},
		{DriverCode: "d1", Result: "RolledBack"},
		{DriverCode: "d2", Result: "RollbackFailed"},
		{DriverCode: "d1", Result: "RollbackOffered"},
	}
	got := pendingRollbackCodes(records)
	if !got["d1"] {
		t.Fatalf("d1 re-offered after outcome should be pending: %#v", got)
	}
	if got["d2"] {
		t.Fatalf("d2 cleared by RollbackFailed should not be pending: %#v", got)
	}
	if got["d3"] {
		t.Fatalf("d3 never offered should not be pending: %#v", got)
	}
}

func TestRunRollbackSuccess(t *testing.T) {
	app := newTestApp(t)
	stubRollback(t, func(string) (bool, error) { return false, nil })
	stubRollbackVerify(t, recoveredVerify)
	writePendingOffer(t, app, rollbackOffer{
		DriverCode: "d1", DriverName: "Audio", State: rollbackStatePending,
		BeforeVersion: "1.0", AfterVersion: "2.0", BeforeInf: "oem42.inf",
		Devices: []rollbackOfferDevice{{PnpDeviceID: "B", Problem: "driver failed load"}},
	})

	if code := app.runRollback(context.Background(), "d1"); code != 0 {
		t.Fatalf("runRollback = %d, want 0", code)
	}

	offers, err := app.readRollbackOffers()
	if err != nil {
		t.Fatal(err)
	}
	if len(offers) != 1 || offers[0].State != rollbackStateRolledBack {
		t.Fatalf("offer not marked rolled back: %#v", offers)
	}

	records := app.ReadHistory(context.Background())
	var results []string
	for _, r := range records {
		results = append(results, r.Result)
	}
	if !containsString(results, "Rollback") || !containsString(results, "RolledBack") {
		t.Fatalf("ledger missing rollback rows: %#v", results)
	}
}

func TestRunRollbackRequiresLedgerOffer(t *testing.T) {
	app := newTestApp(t)
	called := false
	stubRollback(t, func(string) (bool, error) { called = true; return false, nil })
	// The JSON cache has a pending offer, but no RollbackOffered ledger row
	// exists, so the ledger does not authorize a rollback.
	app.writeRollbackOffers([]rollbackOffer{{
		DriverCode: "d1", State: rollbackStatePending,
		Devices: []rollbackOfferDevice{{PnpDeviceID: "B"}},
	}})

	if code := app.runRollback(context.Background(), "d1"); code != 0 {
		t.Fatalf("runRollback without ledger offer = %d, want 0", code)
	}
	if called {
		t.Fatalf("rollback acted without a RollbackOffered ledger row")
	}
}

func TestPerformRollbackAuditFailureBlocksAction(t *testing.T) {
	app := newTestApp(t)
	called := false
	stubRollback(t, func(string) (bool, error) { called = true; return false, nil })
	offer := rollbackOffer{
		DriverCode: "d1", State: rollbackStatePending,
		Devices: []rollbackOfferDevice{{PnpDeviceID: "B"}},
	}
	// Make the history ledger unwritable so the audit-first intent write fails.
	if err := os.Mkdir(app.HistoryPath, 0o755); err != nil {
		t.Fatal(err)
	}

	if ok := app.performRollback(context.Background(), &offer); ok {
		t.Fatalf("performRollback should fail when the audit write fails")
	}
	if called {
		t.Fatalf("device rollback ran despite audit write failure")
	}
	if offer.State != rollbackStateFailed || offer.Message != "audit write failed" {
		t.Fatalf("offer state = %#v", offer)
	}
}

func TestPerformRollbackPartial(t *testing.T) {
	app := newTestApp(t)
	stubRollback(t, func(id string) (bool, error) {
		if id == "OK" {
			return false, nil
		}
		return false, &install.RollbackError{Code: 5} // ERROR_ACCESS_DENIED
	})
	stubRollbackVerify(t, recoveredVerify)
	offer := rollbackOffer{
		DriverCode: "d1", State: rollbackStatePending,
		BeforeVersion: "1.0", AfterVersion: "2.0",
		Devices: []rollbackOfferDevice{
			{PnpDeviceID: "OK"},
			{PnpDeviceID: "BAD"},
		},
	}

	if ok := app.performRollback(context.Background(), &offer); ok {
		t.Fatalf("partial rollback should return false")
	}
	if offer.State != rollbackStatePartial {
		t.Fatalf("state = %q, want partial", offer.State)
	}
	if offer.Devices[0].State != rollbackStateRolledBack || offer.Devices[1].State != rollbackStateFailed {
		t.Fatalf("per-device states wrong: %#v", offer.Devices)
	}
}

func TestRunRollbackNoBackupLeavesDevice(t *testing.T) {
	app := newTestApp(t)
	stubRollback(t, func(string) (bool, error) { return false, &install.RollbackError{Code: 259} })
	reinstallCalled := false
	stubRollbackReinstall(t, func(string, string) (bool, error) { reinstallCalled = true; return false, nil })
	writePendingOffer(t, app, rollbackOffer{
		DriverCode: "d1", State: rollbackStatePending,
		Devices: []rollbackOfferDevice{{PnpDeviceID: "B", Problem: "driver failed load"}},
	})

	if code := app.runRollback(context.Background(), "d1"); code != 1 {
		t.Fatalf("runRollback with no backup = %d, want 1", code)
	}
	if reinstallCalled {
		t.Fatalf("reinstall ran without a previous INF path")
	}
	offers, err := app.readRollbackOffers()
	if err != nil {
		t.Fatal(err)
	}
	if len(offers) != 1 || offers[0].State != rollbackStateFailed {
		t.Fatalf("no-backup offer should be failed: %#v", offers)
	}
	records := app.ReadHistory(context.Background())
	for _, r := range records {
		if r.Result == "RollbackFailed" && !strings.Contains(r.Message, "no backup") {
			t.Fatalf("no-backup message missing: %#v", r)
		}
	}
}

func TestRunRollbackReinstallFallbackSuccess(t *testing.T) {
	app := newTestApp(t)
	stubRollback(t, func(string) (bool, error) { return false, &install.RollbackError{Code: 259} })
	stubRollbackReinstall(t, func(string, string) (bool, error) { return false, nil })
	stubRollbackVerify(t, recoveredVerify)
	infPath := filepath.Join(t.TempDir(), "oem42.inf")
	if err := os.WriteFile(infPath, []byte("previous"), 0o644); err != nil {
		t.Fatal(err)
	}
	writePendingOffer(t, app, rollbackOffer{
		DriverCode: "d1", State: rollbackStatePending,
		BeforeVersion: "1.0", AfterVersion: "2.0", BeforeInfPath: infPath,
		Devices: []rollbackOfferDevice{{PnpDeviceID: "B", Problem: "driver failed load"}},
	})

	if code := app.runRollback(context.Background(), "d1"); code != 0 {
		t.Fatalf("runRollback reinstall fallback = %d, want 0", code)
	}
	offers, err := app.readRollbackOffers()
	if err != nil {
		t.Fatal(err)
	}
	if len(offers) != 1 || offers[0].State != rollbackStateRolledBack {
		t.Fatalf("reinstall fallback offer should be rolled back: %#v", offers)
	}
	records := app.ReadHistory(context.Background())
	found := false
	for _, r := range records {
		if r.Result == "RolledBack" && strings.Contains(r.Message, "recovered") {
			found = true
		}
	}
	if !found {
		t.Fatalf("reinstall fallback recovery row missing: %#v", records)
	}
}

func TestRunRollbackReinstallMissingInfBlocks(t *testing.T) {
	app := newTestApp(t)
	stubRollback(t, func(string) (bool, error) { return false, &install.RollbackError{Code: 259} })
	reinstallCalled := false
	stubRollbackReinstall(t, func(string, string) (bool, error) { reinstallCalled = true; return false, nil })
	writePendingOffer(t, app, rollbackOffer{
		DriverCode: "d1", State: rollbackStatePending,
		BeforeInfPath: filepath.Join(t.TempDir(), "missing.inf"),
		Devices:       []rollbackOfferDevice{{PnpDeviceID: "B", Problem: "driver failed load"}},
	})

	if code := app.runRollback(context.Background(), "d1"); code != 1 {
		t.Fatalf("runRollback missing previous INF = %d, want 1", code)
	}
	if reinstallCalled {
		t.Fatalf("reinstall ran despite missing previous INF")
	}
	records := app.ReadHistory(context.Background())
	found := false
	for _, r := range records {
		if r.Result == "RollbackFailed" && strings.Contains(r.Message, "previous INF missing") {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing-INF failure row missing: %#v", records)
	}
}

func TestRunRollbackNotRecovered(t *testing.T) {
	app := newTestApp(t)
	stubRollback(t, func(string) (bool, error) { return false, nil })
	// The API reports success but the device still reports problem 43, so the
	// ledger must record a failure, not a false recovery.
	stubRollbackVerify(t, func(context.Context, string) (int, string, bool, error) { return 43, "2.0", true, nil })
	writePendingOffer(t, app, rollbackOffer{
		DriverCode: "d1", State: rollbackStatePending,
		BeforeVersion: "1.0", AfterVersion: "2.0",
		Devices: []rollbackOfferDevice{{PnpDeviceID: "B", Problem: "driver failed load"}},
	})

	if code := app.runRollback(context.Background(), "d1"); code != 1 {
		t.Fatalf("runRollback with unrecovered device = %d, want 1", code)
	}
	offers, err := app.readRollbackOffers()
	if err != nil {
		t.Fatal(err)
	}
	if len(offers) != 1 || offers[0].State != rollbackStateFailed || offers[0].Devices[0].State != rollbackStateFailed {
		t.Fatalf("unrecovered offer should be failed: %#v", offers)
	}
	records := app.ReadHistory(context.Background())
	found := false
	for _, r := range records {
		if r.Result == "RollbackFailed" && strings.Contains(r.Message, "still reports problem 43") {
			found = true
		}
	}
	if !found {
		t.Fatalf("unrecovered failure row missing: %#v", records)
	}
}

func TestRunRollbackRebootRequired(t *testing.T) {
	app := newTestApp(t)
	stubRollback(t, func(string) (bool, error) { return true, nil })
	verified := false
	stubRollbackVerify(t, func(context.Context, string) (int, string, bool, error) { verified = true; return 0, "1.0", true, nil })
	writePendingOffer(t, app, rollbackOffer{
		DriverCode: "d1", State: rollbackStatePending,
		BeforeVersion: "1.0", AfterVersion: "2.0",
		Devices: []rollbackOfferDevice{{PnpDeviceID: "B", Problem: "driver failed load"}},
	})

	if code := app.runRollback(context.Background(), "d1"); code != 0 {
		t.Fatalf("runRollback reboot-required = %d, want 0", code)
	}
	if verified {
		t.Fatalf("verification ran despite a pending reboot (state not committed yet)")
	}
	offers, err := app.readRollbackOffers()
	if err != nil {
		t.Fatal(err)
	}
	if len(offers) != 1 || offers[0].State != rollbackStateRolledBack {
		t.Fatalf("reboot-required offer should be rolled back: %#v", offers)
	}
	records := app.ReadHistory(context.Background())
	found := false
	for _, r := range records {
		if r.Result == "RolledBack" && strings.Contains(r.Message, "recovery unverified") {
			found = true
		}
	}
	if !found {
		t.Fatalf("reboot-required ledger row missing: %#v", records)
	}
}

func TestRunRollbackUnchangedDriverNotRecovered(t *testing.T) {
	app := newTestApp(t)
	stubRollback(t, func(string) (bool, error) { return false, nil })
	// Problem cleared but the driver version did not change away from the
	// broken version: a coincidental/self-healing recovery must not be credited
	// to the rollback.
	stubRollbackVerify(t, func(context.Context, string) (int, string, bool, error) { return 0, "2.0", true, nil })
	writePendingOffer(t, app, rollbackOffer{
		DriverCode: "d1", State: rollbackStatePending,
		BeforeVersion: "1.0", AfterVersion: "2.0",
		Devices: []rollbackOfferDevice{{PnpDeviceID: "B", Problem: "driver failed load"}},
	})

	if code := app.runRollback(context.Background(), "d1"); code != 1 {
		t.Fatalf("runRollback with unchanged driver = %d, want 1", code)
	}
	records := app.ReadHistory(context.Background())
	found := false
	for _, r := range records {
		if r.Result == "RollbackFailed" && strings.Contains(r.Message, "driver still at 2.0") {
			found = true
		}
	}
	if !found {
		t.Fatalf("unchanged-driver failure row missing: %#v", records)
	}
}

func containsString(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}
