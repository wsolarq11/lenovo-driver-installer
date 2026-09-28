package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"sort"
	"strings"
	"time"

	"lenovo-driver/internal/inventory"
	"lenovo-driver/internal/model"
)

// deviceSnapshot is the persisted machine-state watermark behind -Audit. It
// keeps only the device fields that can change across a driver install or
// update; the full model.Device also carries per-run source-audit provenance
// that would only add noise to a diff.
type deviceSnapshot struct {
	GeneratedAt string                `json:"generatedAt"`
	OS          string                `json:"os"`
	Devices     []deviceSnapshotEntry `json:"devices"`
}

// deviceSnapshotEntry is one device row in the snapshot.
type deviceSnapshotEntry struct {
	PnpDeviceID   string `json:"pnpDeviceId"`
	Class         string `json:"class"`
	Name          string `json:"name"`
	InfName       string `json:"infName"`
	DriverVersion string `json:"driverVersion"`
	DriverDate    string `json:"driverDate"`
	ProviderName  string `json:"providerName"`
	ProblemNumber int    `json:"problemNumber"`
}

// deviceChange is one row of the -Audit diff report.
type deviceChange struct {
	PnpDeviceID string
	Name        string
	Class       string
	Field       string
	Before      string
	After       string
}

// snapshotFromDevices projects the native device list into a stable, sorted
// snapshot. The OS label is cosmetic and a failure to resolve it is not fatal.
func snapshotFromDevices(ctx context.Context, devices []model.Device) (deviceSnapshot, error) {
	osInfo, err := inventory.GetOSInfo(ctx)
	if err != nil {
		osInfo = inventory.OSInfo{}
	}
	snap := deviceSnapshot{GeneratedAt: formatTimestamp(time.Now()), OS: osInfo.OSName}
	for _, d := range devices {
		snap.Devices = append(snap.Devices, deviceSnapshotEntry{
			PnpDeviceID:   d.PnpDeviceID,
			Class:         d.Class,
			Name:          d.Name,
			InfName:       d.InfName,
			DriverVersion: d.DriverVersion,
			DriverDate:    d.DriverDate,
			ProviderName:  d.ProviderName,
			ProblemNumber: d.ProblemNumber,
		})
	}
	sort.Slice(snap.Devices, func(i, j int) bool {
		return snap.Devices[i].PnpDeviceID < snap.Devices[j].PnpDeviceID
	})
	return snap, nil
}

func writeDeviceSnapshot(path string, snap deviceSnapshot) error {
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func readDeviceSnapshot(path string) (deviceSnapshot, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return deviceSnapshot{}, err
	}
	var snap deviceSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return deviceSnapshot{}, err
	}
	return snap, nil
}

// diffDeviceSnapshots reports device-level changes from old to new, keyed by
// PnpDeviceID. Additions and removals are reported whole; for devices present
// in both snapshots each changed field (version, inf, date, problem code) is
// reported as its own row so a single device can surface multiple drift facts.
func diffDeviceSnapshots(oldSnap, newSnap deviceSnapshot) []deviceChange {
	oldByID := map[string]deviceSnapshotEntry{}
	for _, d := range oldSnap.Devices {
		oldByID[d.PnpDeviceID] = d
	}
	newByID := map[string]deviceSnapshotEntry{}
	for _, d := range newSnap.Devices {
		newByID[d.PnpDeviceID] = d
	}

	var changes []deviceChange
	var ids []string
	for id := range newByID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		n := newByID[id]
		o, existed := oldByID[id]
		if !existed {
			changes = append(changes, deviceChange{PnpDeviceID: id, Name: n.Name, Class: n.Class, Field: "added"})
			continue
		}
		if n.DriverVersion != o.DriverVersion {
			changes = append(changes, deviceChange{PnpDeviceID: id, Name: n.Name, Class: n.Class, Field: "version", Before: o.DriverVersion, After: n.DriverVersion})
		}
		if n.InfName != o.InfName {
			changes = append(changes, deviceChange{PnpDeviceID: id, Name: n.Name, Class: n.Class, Field: "inf", Before: o.InfName, After: n.InfName})
		}
		if n.DriverDate != o.DriverDate {
			changes = append(changes, deviceChange{PnpDeviceID: id, Name: n.Name, Class: n.Class, Field: "date", Before: o.DriverDate, After: n.DriverDate})
		}
		if n.ProblemNumber != o.ProblemNumber {
			changes = append(changes, deviceChange{PnpDeviceID: id, Name: n.Name, Class: n.Class, Field: "problem", Before: fmt.Sprintf("%d", o.ProblemNumber), After: fmt.Sprintf("%d", n.ProblemNumber)})
		}
	}
	var removed []string
	for id := range oldByID {
		if _, ok := newByID[id]; !ok {
			removed = append(removed, id)
		}
	}
	sort.Strings(removed)
	for _, id := range removed {
		o := oldByID[id]
		changes = append(changes, deviceChange{PnpDeviceID: id, Name: o.Name, Class: o.Class, Field: "removed"})
	}
	return changes
}

// deviceAttribution indexes the ledger by the PnP device ids each row targeted,
// so an -Audit device change can name the driver actions this tool recorded for
// that device. It is the consumer of the ledger's Devices column: the identity
// is recorded so that a changed device can be attributed to a driver action by
// lookup instead of by re-deriving it or parsing free text. Driver codes are
// kept unique and in ledger order so the attribution is stable across runs.
func deviceAttribution(records []model.HistoryRecord) map[string][]string {
	out := map[string][]string{}
	for _, r := range records {
		if r.DriverCode == "" {
			continue
		}
		for _, id := range r.Devices {
			if id == "" || slices.Contains(out[id], r.DriverCode) {
				continue
			}
			out[id] = append(out[id], r.DriverCode)
		}
	}
	return out
}

// attributionSuffix renders the ledger attribution for one changed device, or ""
// when the ledger records no action against it. An empty suffix means "this tool
// has no recorded action for the device", which is a fact worth keeping distinct
// from "the device changed because of this tool".
func attributionSuffix(byDevice map[string][]string, pnpDeviceID string) string {
	codes := byDevice[pnpDeviceID]
	if len(codes) == 0 {
		return ""
	}
	return " [ledger: " + strings.Join(codes, ",") + "]"
}

// runAudit captures the current device state and either writes a baseline (no
// prior snapshot) or reports the diff since the last snapshot and advances the
// watermark. It never mutates device state: the only write is the snapshot
// file itself. Each reported change is attributed to the ledger rows that
// targeted that device, when any exist.
func (a *App) runAudit(ctx context.Context, path string) int {
	devices, err := inventory.GetLocalDeviceSnapshot(ctx)
	if err != nil {
		a.Log(ctx, "Device snapshot failed: "+err.Error(), "ERROR")
		return 1
	}
	snap, err := snapshotFromDevices(ctx, devices)
	if err != nil {
		a.Log(ctx, "Snapshot build failed: "+err.Error(), "ERROR")
		return 1
	}

	oldSnap, err := readDeviceSnapshot(path)
	if os.IsNotExist(err) {
		if err := writeDeviceSnapshot(path, snap); err != nil {
			a.Log(ctx, "Snapshot write failed: "+err.Error(), "ERROR")
			return 1
		}
		a.Log(ctx, fmt.Sprintf("Audit baseline captured: %d devices -> %s", len(snap.Devices), path), "INFO")
		return 0
	}
	if err != nil {
		a.Log(ctx, "Snapshot read failed: "+err.Error(), "ERROR")
		return 1
	}

	changes := diffDeviceSnapshots(oldSnap, snap)
	if err := writeDeviceSnapshot(path, snap); err != nil {
		a.Log(ctx, "Snapshot write failed: "+err.Error(), "ERROR")
		return 1
	}
	if len(changes) == 0 {
		a.Log(ctx, "No device changes since last audit.", "INFO")
		return 0
	}
	// Attribution is a best-effort annotation on a read-only report: a ledger
	// that cannot be read degrades to unattributed changes rather than failing
	// the audit, and an unattributed change stays visibly unattributed.
	byDevice := deviceAttribution(a.ReadHistory(ctx))
	a.Log(ctx, fmt.Sprintf("%d device change(s) since last audit:", len(changes)), "INFO")
	for _, c := range changes {
		note := attributionSuffix(byDevice, c.PnpDeviceID)
		switch c.Field {
		case "added":
			a.Log(ctx, fmt.Sprintf("+ %s [%s] %s%s", c.PnpDeviceID, c.Class, c.Name, note), "INFO")
		case "removed":
			a.Log(ctx, fmt.Sprintf("- %s [%s] %s%s", c.PnpDeviceID, c.Class, c.Name, note), "INFO")
		default:
			a.Log(ctx, fmt.Sprintf("~ %s %s: %s -> %s (%s)%s", c.PnpDeviceID, c.Field, c.Before, c.After, c.Name, note), "INFO")
		}
	}
	return 0
}
