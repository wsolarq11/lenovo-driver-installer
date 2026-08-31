package inventory

import (
	"context"
	"os"
	"testing"

	"lenovo-driver/internal/model"
)

// TestNativeSmokePrint is an opt-in real-machine smoke test. It is skipped by
// the offline gate unless LENOVO_NATIVE_SMOKE=1 is set, because it enumerates
// the local PnP tree through SetupAPI.
func TestNativeSmokePrint(t *testing.T) {
	if os.Getenv("LENOVO_NATIVE_SMOKE") != "1" {
		t.Skip("set LENOVO_NATIVE_SMOKE=1 to run native real-machine smoke")
	}
	ctx := context.Background()
	devs, err := GetNativeDeviceEvidence()
	if err != nil {
		t.Fatalf("native inventory failed: %v", err)
	}
	if len(devs) == 0 {
		t.Fatal("native inventory returned no devices")
	}
	sawDriver := false
	for _, d := range devs {
		if d.DriverVersion != "" {
			sawDriver = true
			break
		}
	}
	if !sawDriver {
		t.Fatal("native inventory returned devices without any DriverVersion")
	}
	snapshot, err := GetLocalDeviceSnapshot(ctx)
	if err != nil || len(snapshot) == 0 {
		t.Fatalf("native device snapshot failed: %v (len=%d)", err, len(snapshot))
	}
	machine, err := GetMachineInfo(ctx)
	if err != nil || machine.Model == "" {
		t.Fatalf("native machine info failed: %#v err=%v", machine, err)
	}
	t.Logf("machine model=%q serial=%q", machine.Model, machine.Serial)
	osInfo, err := GetOSInfo(ctx)
	if err != nil || osInfo.OSName == "" || osInfo.Arch == "" {
		t.Fatalf("native OS info failed: %#v err=%v", osInfo, err)
	}
	apps, err := GetInstalledApps(ctx)
	if err != nil || len(apps) == 0 {
		t.Fatalf("native installed apps failed: len=%d err=%v", len(apps), err)
	}
	sw, err := GetSoftwareSnapshot(ctx, apps)
	if err != nil {
		t.Fatalf("native software snapshot failed: %v", err)
	}
	_ = sw
}

// TestNativeSmokeIntegration verifies the evidence entry points against the
// native-only device snapshot.
func TestNativeSmokeIntegration(t *testing.T) {
	if os.Getenv("LENOVO_NATIVE_SMOKE") != "1" {
		t.Skip("set LENOVO_NATIVE_SMOKE=1 to run native real-machine smoke")
	}
	ctx := context.Background()
	devs, err := GetLocalDeviceSnapshot(ctx)
	if err != nil || len(devs) == 0 {
		t.Fatalf("native device snapshot unavailable: %v", err)
	}
	input := make([]model.Device, 0, len(devs))
	for _, d := range devs {
		input = append(input, model.Device{PnpDeviceID: d.PnpDeviceID})
	}
	rows, err := GetDeviceEvidence(ctx, input)
	if err != nil {
		t.Fatalf("GetDeviceEvidence failed: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("GetDeviceEvidence returned no rows")
	}
	sawLenovo := false
	for _, d := range rows {
		if d.ProviderName == "Lenovo" && d.DriverVersion != "" {
			sawLenovo = true
			break
		}
	}
	if !sawLenovo {
		t.Fatal("native GetDeviceEvidence did not return a Lenovo driver row")
	}
	ids := make([]string, 0, len(input))
	for _, d := range input {
		ids = append(ids, d.PnpDeviceID)
	}
	versions, err := GetDeviceDriverVersions(ctx, ids)
	if err != nil {
		t.Fatalf("GetDeviceDriverVersions failed: %v", err)
	}
	if len(versions) != len(ids) {
		t.Fatalf("GetDeviceDriverVersions returned %d values, want %d", len(versions), len(ids))
	}
	sawNonEmpty := false
	for _, v := range versions {
		if v != "" {
			sawNonEmpty = true
			break
		}
	}
	if !sawNonEmpty {
		t.Fatal("GetDeviceDriverVersions returned no non-empty versions")
	}
}
