//go:build windows

package inventory

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"lenovo-driver/internal/model"
)

// The evidence chain needs real-machine ground truth, but it must be captured by
// the same collector the tool uses at runtime. A fixture exported by an external
// script would let the code under test be "verified" against a third party's
// idea of the machine.
//
// Dump it once with:
//
//	LENOVO_EVIDENCE_FIXTURE_DUMP=1 go test ./internal/inventory/ -run TestDumpDeviceFixture
//
// The committed fixture then drives the offline half of the chain, and the
// conclusions it produces are asserted to equal what a live dry-run reports.
func TestDumpDeviceFixture(t *testing.T) {
	if os.Getenv("LENOVO_EVIDENCE_FIXTURE_DUMP") != "1" {
		t.Skip("set LENOVO_EVIDENCE_FIXTURE_DUMP=1 to (re)capture the device fixture")
	}
	devices, err := GetLocalDeviceSnapshot(context.Background())
	if err != nil {
		t.Fatalf("device snapshot: %v", err)
	}
	if len(devices) == 0 {
		t.Fatal("device snapshot empty")
	}
	// GetLocalDeviceSnapshot carries identity but no driver version. The compare
	// step is version-driven, so the fixture is worthless without it.
	// GetDeviceEvidence drops rows it cannot resolve, so read the full PnP
	// property set instead and merge on PnpDeviceID.
	evidence, err := GetNativeDeviceEvidence()
	if err != nil {
		t.Fatalf("native device evidence: %v", err)
	}
	byID := make(map[string]model.Device, len(evidence))
	for _, e := range evidence {
		byID[e.PnpDeviceID] = e
	}
	out := make([]fixtureDevice, 0, len(devices))
	var missing, softwareDevices int
	for _, d := range devices {
		// SWD rows are software-enumerated devices carrying a per-machine GUID
		// (SWD\MSDAS\{CE958E9A-424F-4C88-86F4-11314821E75A} appeared here on a
		// later boot). Recording one would make the fixture unreproducible after a
		// reinstall while telling the compare step nothing: no Lenovo package
		// matches a software device's hardware id.
		if strings.HasPrefix(d.PnpDeviceID, `SWD\`) {
			softwareDevices++
			continue
		}
		merged := d
		if e, ok := byID[d.PnpDeviceID]; ok {
			merged = e
		} else {
			missing++
		}
		out = append(out, fixtureDevice{
			// The instance tail ("\3&11583659&0&00") is machine- and
			// boot-specific and carries no auditing value, but PnpDeviceID is the
			// ledger's join key so its shape must survive. Truncating at the
			// hardware boundary keeps identity checkable and the fixture
			// reproducible across boots.
			PnpDeviceID:   instanceTail.ReplaceAllString(merged.PnpDeviceID, `\<instance>`),
			DeviceID:      merged.DeviceID,
			Name:          merged.Name,
			Class:         merged.Class,
			DriverVersion: merged.DriverVersion,
			InfName:       merged.InfName,
			ProviderName:  merged.ProviderName,
			ProblemNumber: merged.ProblemNumber,
		})
	}
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join("..", "app", "testdata", "local_devices_82jq.json")
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	// A fixture that silently lost its version column would make every
	// downstream compare fall back to "undetermined" and still pass a
	// structural check, so state the coverage explicitly.
	t.Logf("wrote %d devices to %s (%d without driver evidence, %d software-enumerated rows skipped)",
		len(out), path, missing, softwareDevices)

	if err := dumpSoftwareFixture(t); err != nil {
		t.Fatalf("software fixture: %v", err)
	}
}

// dumpSoftwareFixture records the software half of the compare input. Several
// Lenovo drivers (Fn service, Energy Management, X-Rite, AMD Power) resolve
// their local version from InstalledApps rather than from a PnP property, so a
// device-only fixture silently drops them out of the chain.
//
// The whole app list is recorded rather than a hand-picked subset: the driver
// name -> app name rules live in internal/compare and are deliberately private,
// so a filtered fixture would be filtered by a second, drifting copy of them.
func dumpSoftwareFixture(t *testing.T) error {
	ctx := context.Background()
	apps, err := GetInstalledApps(ctx)
	if err != nil {
		return err
	}
	snapshot, err := GetSoftwareSnapshot(ctx, apps)
	if err != nil {
		return err
	}
	// The native enumerator currently returns the same app more than once.
	// De-duplicate here so the fixture is a statement about the machine rather
	// than about how many times the registry was walked.
	seen := make(map[string]bool, len(apps))
	uniq := make([]model.InstalledApp, 0, len(apps))
	for _, a := range apps {
		if seen[a.DisplayName] {
			continue
		}
		seen[a.DisplayName] = true
		uniq = append(uniq, a)
	}
	t.Logf("software: %d apps collected, %d unique, ProvisionedAmdPower=%q LenovoFn=%q",
		len(apps), len(uniq), snapshot.ProvisionedAmdPower, snapshot.LenovoFnServiceVersion)
	snapshot.InstalledApps = uniq
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join("..", "app", "testdata", "local_software_82jq.json")
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

type fixtureDevice struct {
	PnpDeviceID   string `json:"PnpDeviceID"`
	DeviceID      string `json:"DeviceID"`
	Name          string `json:"Name"`
	Class         string `json:"Class"`
	DriverVersion string `json:"DriverVersion"`
	InfName       string `json:"InfName"`
	ProviderName  string `json:"ProviderName"`
	ProblemNumber int    `json:"ProblemNumber"`
}

var instanceTail = regexp.MustCompile(`\\\d+&\w+&\d+&\d+\\?$`)
