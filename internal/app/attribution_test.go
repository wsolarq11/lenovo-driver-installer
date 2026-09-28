package app

import (
	"bytes"
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"lenovo-driver/internal/model"
)

// This file covers the ledger attribution consumed by -Audit: the Devices column
// exists so a changed device can be tied to the driver actions this tool
// recorded for it. These tests keep that consumer honest, because a recorded
// column nobody reads is governance that earns nothing.

// TestDeviceAttributionIndexesLedger proves the index is built from the
// structured Devices column: one device maps to every driver code that recorded
// an action against it, with driver codes unique and in ledger order.
func TestDeviceAttributionIndexesLedger(t *testing.T) {
	records := []model.HistoryRecord{
		{DriverCode: "DRV1", Devices: []string{"devA", "devB"}},
		{DriverCode: "DRV2", Devices: []string{"devB"}},
		{DriverCode: "DRV1", Devices: []string{"devB"}}, // repeat must not duplicate
		{DriverCode: "", Devices: []string{"devC"}},     // no code, not attributable
		{DriverCode: "DRV3", Devices: nil},              // no devices
	}
	got := deviceAttribution(records)
	want := map[string][]string{
		"devA": {"DRV1"},
		"devB": {"DRV1", "DRV2"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("attribution index drift:\n got %#v\nwant %#v", got, want)
	}
}

// TestAttributionSuffixIsExplicitWhenAbsent proves an unattributed device stays
// visibly unattributed. "No ledger row recorded an action for this device" is a
// different fact from "this tool caused the change", and collapsing the two
// would let an empty result read as a clean bill of health.
func TestAttributionSuffixIsExplicitWhenAbsent(t *testing.T) {
	byDevice := map[string][]string{"devA": {"DRV1", "DRV2"}}
	if got := attributionSuffix(byDevice, "devA"); got != " [ledger: DRV1,DRV2]" {
		t.Fatalf("attributed suffix = %q", got)
	}
	if got := attributionSuffix(byDevice, "devZ"); got != "" {
		t.Fatalf("unattributed device must yield no suffix, got %q", got)
	}
}

// TestAuditAttributionComesFromDeviceColumn is the seam assertion: given a
// ledger row that names a device only through the structured column, the audit
// attribution must find it. This is the behaviour that was impossible before the
// identity became a column, because the id lived inside the free-text Message.
func TestAuditAttributionComesFromDeviceColumn(t *testing.T) {
	app := New(&bytes.Buffer{}, &bytes.Buffer{}, strings.NewReader(""))
	app.HistoryPath = filepath.Join(t.TempDir(), "history.csv")

	const dev = `PCI\VEN_10DE&DEV_2520`
	ad := &model.AssessedDriver{
		Driver:           &model.Driver{DriverCode: "DRV1"},
		DriverAssessment: model.DriverAssessment{MatchedDeviceIDs: []string{dev}},
	}
	if err := app.WriteHistoryRecord(ad, "Installed", "exit=0", "", ""); err != nil {
		t.Fatal(err)
	}

	byDevice := deviceAttribution(app.ReadHistory(context.Background()))
	if !reflect.DeepEqual(byDevice[dev], []string{"DRV1"}) {
		t.Fatalf("audit cannot attribute %q from the ledger: %#v", dev, byDevice)
	}
}

// TestAuditAttributionIgnoresMessageText proves the attribution is not derived
// from the free-text Message. A message that merely mentions a device id must
// not create an attribution; only the structured column may.
func TestAuditAttributionIgnoresMessageText(t *testing.T) {
	const dev = `PCI\VEN_10DE&DEV_2520`
	records := []model.HistoryRecord{
		{DriverCode: "DRV1", Message: "device " + dev + " rolled back", Devices: nil},
	}
	if got := deviceAttribution(records); len(got) != 0 {
		t.Fatalf("attribution must not be parsed out of Message text, got %#v", got)
	}
}
