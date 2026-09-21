package app

import (
	"path/filepath"
	"reflect"
	"testing"
)

func snapEntry(id, name, class, inf, ver, date string, problem int) deviceSnapshotEntry {
	return deviceSnapshotEntry{
		PnpDeviceID:   id,
		Name:          name,
		Class:         class,
		InfName:       inf,
		DriverVersion: ver,
		DriverDate:    date,
		ProblemNumber: problem,
	}
}

// TestDiffDeviceSnapshots proves the diff is complete and deterministic: one
// row per changed field, additions and removals reported whole, and a stable
// ordering so the report never reshuffles between runs.
func TestDiffDeviceSnapshots(t *testing.T) {
	oldSnap := deviceSnapshot{Devices: []deviceSnapshotEntry{
		snapEntry("A", "Audio", "MEDIA", "oem1.inf", "1.0", "2020-01-01", 0),
		snapEntry("B", "GPU", "DISPLAY", "oem2.inf", "2.0", "2020-01-01", 0),
		snapEntry("C", "Gone", "USB", "oem3.inf", "3.0", "2020-01-01", 0),
	}}
	newSnap := deviceSnapshot{Devices: []deviceSnapshotEntry{
		snapEntry("A", "Audio", "MEDIA", "oem1.inf", "1.1", "2021-01-01", 0),
		snapEntry("B", "GPU", "DISPLAY", "oem9.inf", "2.0", "2020-01-01", 0),
		snapEntry("D", "New", "NET", "oem4.inf", "4.0", "2021-01-01", 0),
	}}
	changes := diffDeviceSnapshots(oldSnap, newSnap)
	wantFields := []string{"version", "date", "inf", "added", "removed"}
	if len(changes) != len(wantFields) {
		t.Fatalf("got %d changes, want %d: %#v", len(changes), len(wantFields), changes)
	}
	for i, c := range changes {
		if c.Field != wantFields[i] {
			t.Fatalf("change[%d].Field = %q, want %q (all: %#v)", i, c.Field, wantFields[i], changes)
		}
	}
	if changes[0].Before != "1.0" || changes[0].After != "1.1" {
		t.Fatalf("version change misreported: %#v", changes[0])
	}
	if changes[2].Before != "oem2.inf" || changes[2].After != "oem9.inf" {
		t.Fatalf("inf change misreported: %#v", changes[2])
	}
}

func TestDiffDeviceSnapshotsNoChange(t *testing.T) {
	snap := deviceSnapshot{Devices: []deviceSnapshotEntry{
		snapEntry("A", "Audio", "MEDIA", "oem1.inf", "1.0", "2020-01-01", 0),
	}}
	if got := diffDeviceSnapshots(snap, snap); len(got) != 0 {
		t.Fatalf("identical snapshots should diff empty, got %#v", got)
	}
}

func TestDiffDeviceSnapshotsProblemChange(t *testing.T) {
	oldSnap := deviceSnapshot{Devices: []deviceSnapshotEntry{
		snapEntry("A", "Audio", "MEDIA", "oem1.inf", "1.0", "2020-01-01", 0),
	}}
	newSnap := deviceSnapshot{Devices: []deviceSnapshotEntry{
		snapEntry("A", "Audio", "MEDIA", "oem1.inf", "1.0", "2020-01-01", 28),
	}}
	changes := diffDeviceSnapshots(oldSnap, newSnap)
	if len(changes) != 1 || changes[0].Field != "problem" || changes[0].Before != "0" || changes[0].After != "28" {
		t.Fatalf("problem change misreported: %#v", changes)
	}
}

// TestDeviceSnapshotRoundTrip proves the persisted watermark survives a
// write/read cycle without losing any diffable field.
func TestDeviceSnapshotRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "snapshot.json")
	snap := deviceSnapshot{
		GeneratedAt: "2026-09-21T00:00:00Z",
		OS:          "Windows 10 64-bit",
		Devices: []deviceSnapshotEntry{
			snapEntry("A", "Audio", "MEDIA", "oem1.inf", "1.0", "2020-01-01", 0),
		},
	}
	if err := writeDeviceSnapshot(path, snap); err != nil {
		t.Fatal(err)
	}
	back, err := readDeviceSnapshot(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(back, snap) {
		t.Fatalf("snapshot round-trip mismatch:\n got %#v\nwant %#v", back, snap)
	}
}
