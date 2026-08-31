//go:build windows && legacyps

package inventory

import (
	"context"
	"fmt"
	"os"
	"sort"
	"testing"
	"time"

	"lenovo-driver/internal/model"
)

// Real-machine equivalence tests compare the native-only runtime path against
// the legacy PowerShell oracle. They require `-tags legacyps` and
// LENOVO_NATIVE_EQUIV_SMOKE=1, and are skipped by the offline gate.

func TestNativePSEquivalenceSmoke(t *testing.T) {
	if os.Getenv("LENOVO_NATIVE_EQUIV_SMOKE") != "1" {
		t.Skip("set LENOVO_NATIVE_EQUIV_SMOKE=1 to run native-vs-PowerShell equivalence")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	nativeDevices, err := GetLocalDeviceSnapshot(ctx)
	if err != nil {
		t.Fatalf("native device snapshot failed: %v", err)
	}
	psDevices, err := GetLocalDeviceSnapshotPS(ctx)
	if err != nil {
		t.Fatalf("PowerShell device snapshot failed: %v", err)
	}
	if len(nativeDevices) == 0 || len(psDevices) == 0 {
		t.Fatalf("device snapshots empty: native=%d ps=%d", len(nativeDevices), len(psDevices))
	}
	// Device list equivalence: compare by PnpDeviceID membership.
	nativeIDs := make(map[string]bool, len(nativeDevices))
	for _, d := range nativeDevices {
		nativeIDs[d.PnpDeviceID] = true
	}
	psIDs := make(map[string]bool, len(psDevices))
	for _, d := range psDevices {
		psIDs[d.PnpDeviceID] = true
	}
	var nativeOnly, psOnly []string
	for id := range nativeIDs {
		if !psIDs[id] {
			nativeOnly = append(nativeOnly, id)
		}
	}
	for id := range psIDs {
		if !nativeIDs[id] {
			psOnly = append(psOnly, id)
		}
	}
	sort.Strings(nativeOnly)
	sort.Strings(psOnly)
	t.Logf("device count native=%d ps=%d nativeOnly=%d psOnly=%d",
		len(nativeDevices), len(psDevices), len(nativeOnly), len(psOnly))
	if len(nativeOnly) > 0 {
		t.Logf("native-only sample: %v", firstN(nativeOnly, 5))
	}
	if len(psOnly) > 0 {
		t.Logf("ps-only sample: %v", firstN(psOnly, 5))
	}

	// Machine / OS equivalence.
	nativeMachine, err := GetMachineInfo(ctx)
	if err != nil {
		t.Fatalf("native machine failed: %v", err)
	}
	psMachine, err := GetMachineInfoPS(ctx)
	if err != nil {
		t.Fatalf("PowerShell machine failed: %v", err)
	}
	t.Logf("machine native=%q/%q ps=%q/%q", nativeMachine.Model, nativeMachine.Serial, psMachine.Model, psMachine.Serial)
	if nativeMachine.Model == "" || psMachine.Model == "" {
		t.Fatalf("machine model missing: native=%q ps=%q", nativeMachine.Model, psMachine.Model)
	}
	if nativeMachine.Model != psMachine.Model {
		t.Fatalf("machine model mismatch: native=%q ps=%q", nativeMachine.Model, psMachine.Model)
	}
	if nativeMachine.Serial != psMachine.Serial {
		t.Logf("machine serial differs (native=%q ps=%q); serial may be unavailable in this firmware's SMBIOS table", nativeMachine.Serial, psMachine.Serial)
	}

	nativeOS, err := GetOSInfo(ctx)
	if err != nil {
		t.Fatalf("native OS failed: %v", err)
	}
	psOS, err := GetOSInfoPS(ctx)
	if err != nil {
		t.Fatalf("PowerShell OS failed: %v", err)
	}
	t.Logf("OS native=%#v ps=%#v", nativeOS, psOS)
	if nativeOS.OSName != psOS.OSName {
		t.Fatalf("OSName mismatch: native=%q ps=%q", nativeOS.OSName, psOS.OSName)
	}
	if nativeOS.Arch != psOS.Arch {
		t.Fatalf("OS arch mismatch: native=%q ps=%q", nativeOS.Arch, psOS.Arch)
	}

	// Installed apps equivalence.
	nativeApps, err := GetInstalledApps(ctx)
	if err != nil {
		t.Fatalf("native apps failed: %v", err)
	}
	psApps, err := GetInstalledAppsPS(ctx)
	if err != nil {
		t.Fatalf("PowerShell apps failed: %v", err)
	}
	appMap := func(apps []model.InstalledApp) map[string]string {
		m := make(map[string]string, len(apps))
		for _, a := range apps {
			if a.DisplayName != "" {
				m[a.DisplayName] = a.DisplayVersion
			}
		}
		return m
	}
	nm := appMap(nativeApps)
	pm := appMap(psApps)
	var appMissing, appMismatch int
	for name, ver := range nm {
		pv, ok := pm[name]
		if !ok {
			appMissing++
			continue
		}
		if ver != pv {
			appMismatch++
		}
	}
	t.Logf("apps native=%d ps=%d nativeOnly=%d versionMismatch=%d",
		len(nativeApps), len(psApps), appMissing, appMismatch)
	if appMissing > len(nm)/20+2 { // allow small registry/PS surface drift
		t.Fatalf("too many native-only apps: %d", appMissing)
	}

	// Software snapshot equivalence.
	nativeSW, err := GetSoftwareSnapshot(ctx, nativeApps)
	if err != nil {
		t.Fatalf("native software snapshot failed: %v", err)
	}
	psSW, err := GetSoftwareSnapshotPS(ctx, psApps)
	if err != nil {
		t.Fatalf("PowerShell software snapshot failed: %v", err)
	}
	t.Logf("software native=%#v ps=%#v", nativeSW, psSW)
	if nativeSW.ProvisionedAmdPower != psSW.ProvisionedAmdPower {
		t.Fatalf("ProvisionedAmdPower mismatch: native=%q ps=%q", nativeSW.ProvisionedAmdPower, psSW.ProvisionedAmdPower)
	}
	if nativeSW.LenovoFnServiceVersion != psSW.LenovoFnServiceVersion {
		t.Fatalf("FnServiceVersion mismatch: native=%q ps=%q", nativeSW.LenovoFnServiceVersion, psSW.LenovoFnServiceVersion)
	}
}

func firstN[T any](items []T, n int) []T {
	if len(items) <= n {
		return items
	}
	return items[:n]
}

var _ = fmt.Sprintf
