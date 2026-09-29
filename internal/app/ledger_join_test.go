package app

import (
	"context"
	"encoding/csv"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"lenovo-driver/internal/model"
)

// This file is the end-to-end seam test for the ledger's device identity. It
// exercises the whole chain rather than one function: assessment captures the
// matched PnP ids, a ledger row carries them, ReadHistory reads them back, and
// the -Audit device snapshot diffs by the same id. The join key is what lets
// "which driver this tool acted on" be matched to "how devices actually
// changed" without parsing free-text messages.

// TestLedgerCarriesDeviceIdentity proves the captured matched device ids reach
// the on-disk Devices column and survive the read-back, so a ledger row names
// the devices its action targeted.
func TestLedgerCarriesDeviceIdentity(t *testing.T) {
	app := newTestApp(t)
	app.HistoryPath = filepath.Join(t.TempDir(), "history.csv")

	ad := &model.AssessedDriver{
		Driver: &model.Driver{DriverCode: "d1", DriverName: "Audio", Version: "1.0.0.1"},
		DriverAssessment: model.DriverAssessment{
			MatchedDeviceIDs: []string{`PCI\VEN_1002&DEV_1638`, `HDAUDIO\FUNC_01`},
		},
	}
	if err := app.WriteHistoryRecord(ad, "Installed", "exit=0", "", ""); err != nil {
		t.Fatal(err)
	}

	records := app.ReadHistory(context.Background())
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	want := []string{`PCI\VEN_1002&DEV_1638`, `HDAUDIO\FUNC_01`}
	if !reflect.DeepEqual(records[0].Devices, want) {
		t.Fatalf("device identity lost in round-trip: got %#v, want %#v", records[0].Devices, want)
	}
}

// TestLedgerDeviceIdentityIsDeduped proves the recorded identity is a set: a
// device that matched a driver more than once is recorded once, so the audit
// join cannot double-count it.
func TestLedgerDeviceIdentityIsDeduped(t *testing.T) {
	got := joinDevices([]string{"A", "A", "", "B", "A", "B"})
	if got != "A;B" {
		t.Fatalf("joinDevices = %q, want %q", got, "A;B")
	}
}

// TestLedgerRowJoinsToDeviceSnapshot is the seam test proper: it writes an
// install row carrying device identity, then diffs device snapshots keyed by
// PnpDeviceID, and proves the changed device and the ledger row agree on the
// same id. Before the Devices column existed this join was impossible: the
// ledger held only a driver code and the device id lived inside free text.
func TestLedgerRowJoinsToDeviceSnapshot(t *testing.T) {
	app := newTestApp(t)
	app.HistoryPath = filepath.Join(t.TempDir(), "history.csv")

	const changedDevice = `PCI\VEN_10DE&DEV_2520`
	ad := &model.AssessedDriver{
		Driver:           &model.Driver{DriverCode: "DRV1", DriverName: "NVIDIA VGA", Version: "31.0.15.4630"},
		DriverAssessment: model.DriverAssessment{MatchedDeviceIDs: []string{changedDevice}},
	}
	if err := app.WriteHistoryRecord(ad, "Installed", "exit=0", "31.0.15.4630", "31.0.15.4000"); err != nil {
		t.Fatal(err)
	}

	oldSnap := deviceSnapshot{Devices: []deviceSnapshotEntry{
		snapEntry(changedDevice, "NVIDIA GPU", "DISPLAY", "oem1.inf", "31.0.15.4000", "2024-01-01", 0),
	}}
	newSnap := deviceSnapshot{Devices: []deviceSnapshotEntry{
		snapEntry(changedDevice, "NVIDIA GPU", "DISPLAY", "oem9.inf", "31.0.15.4630", "2024-01-01", 0),
	}}
	changes := diffDeviceSnapshots(oldSnap, newSnap)
	if len(changes) == 0 {
		t.Fatal("expected the device to diff as changed")
	}

	// The join: every changed device id must be attributable to a ledger row
	// that targeted it. This is the fact the audit needs and could not obtain
	// before the identity was a column.
	byID := ledgerDevicesByCode(app.ReadHistory(context.Background()))
	for _, change := range changes {
		if !byID["DRV1"][change.PnpDeviceID] {
			t.Fatalf("changed device %q is not attributable to any ledger row (have %#v)", change.PnpDeviceID, byID)
		}
	}
}

// ledgerDevicesByCode indexes ledger rows by driver code and the set of devices
// each targeted. It stands in for the consumer the Devices column exists to
// serve: a device-level reconcile that answers "which driver action touched this
// device" by identity lookup rather than by parsing the Message text.
func ledgerDevicesByCode(records []model.HistoryRecord) map[string]map[string]bool {
	out := map[string]map[string]bool{}
	for _, r := range records {
		if out[r.DriverCode] == nil {
			out[r.DriverCode] = map[string]bool{}
		}
		for _, id := range r.Devices {
			out[r.DriverCode][id] = true
		}
	}
	return out
}

// TestRollbackRowsCarryDeviceIdentity proves the rollback path records device
// identity as a structured column instead of embedding the id in free text, so
// per-device rollback outcomes are joinable the same way install rows are.
func TestRollbackRowsCarryDeviceIdentity(t *testing.T) {
	offer := rollbackOffer{
		DriverCode:    "DRV1",
		DriverName:    "NVIDIA VGA",
		Version:       "31.0.15.4630",
		BeforeInf:     "oem1.inf",
		BeforeInfPath: `C:\Windows\INF\oem1.inf`,
		State:         rollbackStatePending,
		Devices: []rollbackOfferDevice{
			{PnpDeviceID: `PCI\VEN_10DE&DEV_2520`, Problem: "Code 43"},
			{PnpDeviceID: `PCI\VEN_10DE&DEV_2521`, Problem: "Code 43"},
		},
		GeneratedAt: "2026-01-01T00:00:00Z",
	}

	offered := offer.assessedDriver()
	if got := offered.MatchedDeviceIDs; len(got) != 2 {
		t.Fatalf("offer row must carry both device ids, got %#v", got)
	}
	if msg := rollbackOfferedMessage(offer); strings.Contains(msg, `PCI\VEN`) {
		t.Fatalf("device ids must not be duplicated into free text: %q", msg)
	}

	// A per-device outcome row names exactly one device.
	scoped := deviceScopedDriver(offered, `PCI\VEN_10DE&DEV_2521`)
	if !reflect.DeepEqual(scoped.MatchedDeviceIDs, []string{`PCI\VEN_10DE&DEV_2521`}) {
		t.Fatalf("per-device row identity = %#v", scoped.MatchedDeviceIDs)
	}
	if scoped.LocalVersion != "" || scoped.CompareStatus != "" {
		t.Fatalf("scoping must not invent assessment fields: %#v", scoped.DriverAssessment)
	}
}

// TestHashChainIndexSurvivesSchemaGrowth proves the integrity chain is located
// by column name, not by the current schema's column count. A ledger written
// under the older 13-column schema must still verify against its own layout
// after the schema grew; a positional index would have silently started
// verifying the wrong cells and stopped detecting tampering.
func TestHashChainIndexSurvivesSchemaGrowth(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.csv")
	legacyColumns := []string{
		"Timestamp", "DriverCode", "OSID", "OSName", "DriverName", "Version",
		"VerifiedVersion", "BeforeVersion", "FileName", "MD5", "Source", "Result", "Message",
	}
	data := []string{"2026-01-01T00:00:00Z", "d1", "42", "Win10", "Audio", "1.0", "1.0", "0.9", "a.exe", "md5", "QuickFix", "Installed", "exit=0"}
	row := append(append([]string(nil), data...), historyRowHash("", data))

	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w := csv.NewWriter(f)
	if err := w.Write(append(append([]string(nil), legacyColumns...), historyHashColumn)); err != nil {
		t.Fatal(err)
	}
	if err := w.Write(row); err != nil {
		t.Fatal(err)
	}
	w.Flush()
	f.Close()

	if err := verifyHistoryChain(path); err != nil {
		t.Fatalf("legacy ledger must verify against its own column layout: %v", err)
	}

	// Tampering with the legacy row must still be caught.
	rewriteCSV(t, path, 1, 5, "tampered")
	if err := verifyHistoryChain(path); err == nil {
		t.Fatal("tampering in a legacy-layout ledger must break the chain")
	}
}

// TestWriteHistoryRecordRefusesStaleSchema proves an existing ledger whose
// header predates the current schema is refused loudly instead of being
// appended to. Its rows are append-only and cannot be widened, so appending a
// wider row would silently bind values to the wrong column names.
func TestWriteHistoryRecordRefusesStaleSchema(t *testing.T) {
	app := newTestApp(t)
	app.HistoryPath = filepath.Join(t.TempDir(), "history.csv")

	stale := []string{
		"Timestamp", "DriverCode", "OSID", "OSName", "DriverName", "Version",
		"VerifiedVersion", "BeforeVersion", "FileName", "MD5", "Source", "Result", "Message",
	}
	f, err := os.Create(app.HistoryPath)
	if err != nil {
		t.Fatal(err)
	}
	w := csv.NewWriter(f)
	if err := w.Write(append(append([]string(nil), stale...), historyHashColumn)); err != nil {
		t.Fatal(err)
	}
	w.Flush()
	f.Close()

	driver := &model.AssessedDriver{Driver: &model.Driver{DriverCode: "d1"}}
	err = app.WriteHistoryRecord(driver, "Installed", "exit=0", "", "")
	if err == nil {
		t.Fatal("appending to a stale-schema ledger must be refused")
	}
	if !strings.Contains(err.Error(), "Devices") {
		t.Fatalf("refusal must name the missing column: %v", err)
	}
	if !strings.Contains(err.Error(), "history.csv") {
		t.Fatalf("refusal must name the file: %v", err)
	}

	// The refused ledger must be left untouched (no partial row appended).
	_, rows, readErr := readHistoryRows(app.HistoryPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(rows) != 0 {
		t.Fatalf("refused write must not append anything, got %#v", rows)
	}
}
