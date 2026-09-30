package compare

import (
	"testing"

	"lenovo-driver/internal/model"
)

// A software-versioned driver normally resolves its local version from
// InstalledApps. When that lookup comes back empty the device's own measured
// version must take over, because the device side is a fact read from PnP
// while an empty lookup is only the absence of evidence.
//
// Getting this backwards is what put a redundant reinstall in front of the
// user: on 82JQ the automatic set contained DRV201907160015 (Lenovo Energy
// Management) as "Not installed" even though ACPI\VPC2004 was running Lenovo's
// own oem90.inf at 15.11.29.65, newer than the 15.11.29.13 the list offers.
func TestSoftwareVersionedDriverFallsBackToMeasuredDeviceVersion(t *testing.T) {
	device := model.Device{
		Name: "Lenovo ACPI-Compliant Virtual Power Controller", Class: "System",
		DeviceID: `ACPI\VEN_VPC&DEV_2004`, PnpDeviceID: `ACPI\VPC2004\0`,
		DriverVersion: "15.11.29.65", InfName: "oem90.inf", ProviderName: "Lenovo",
	}
	driver := &model.Driver{
		DriverCode: "DRV201907160015",
		DriverName: "Lenovo Energy Management 联想电源管理驱动",
		Version:    "15.11.29.13 MS signed",
	}
	if !TestSoftwareVersionedDriver(driver.DriverName) {
		t.Fatal("fixture is not a software-versioned driver; this test would prove nothing")
	}

	// No matching entry in InstalledApps, which is the case that used to fail.
	empty := &model.SoftwareSnapshot{InstalledApps: []model.InstalledApp{
		{DisplayName: "NVIDIA 图形驱动程序 546.30", DisplayVersion: "546.30"},
	}}
	local, vendor := ResolveLocalDriverVersion(driver, []model.Device{device}, []string{device.DriverVersion}, empty)
	if local != "15.11.29.65" {
		t.Fatalf("local version = %q, want the measured device version 15.11.29.65", local)
	}
	// GetDeviceVendor does not recognise this device's name, so vendor comes
	// back empty and the compare falls back to parsing the whole remote string.
	// That is a property of the fixture, not of the fallback under test.
	t.Logf("vendor resolved from %q: %q", device.Name, vendor)
	// With the measured version in hand the driver can no longer be classified
	// as uninstalled, which is the whole point.
	if got := CompareDriverStatus(driver.Version, local, vendor); got == model.StatusNotInstalled {
		t.Fatalf("status = %q; a device running a vendor package must not read as uninstalled", got)
	}
}

// The fallback must not override a software-side answer: when InstalledApps
// does carry the version, that version is the one being compared.
func TestSoftwareVersionedDriverPrefersSoftwareVersionWhenPresent(t *testing.T) {
	device := model.Device{
		Name: "Lenovo ACPI-Compliant Virtual Power Controller", Class: "System",
		DeviceID: `ACPI\VEN_VPC&DEV_2004`, PnpDeviceID: `ACPI\VPC2004\0`,
		DriverVersion: "15.11.29.65", InfName: "oem90.inf", ProviderName: "Lenovo",
	}
	driver := &model.Driver{
		DriverCode: "DRV201907160015",
		DriverName: "Lenovo Energy Management 联想电源管理驱动",
		Version:    "2.0.0.30",
	}
	present := &model.SoftwareSnapshot{InstalledApps: []model.InstalledApp{
		{DisplayName: "Lenovo Energy Management", DisplayVersion: "1.0.2.0"},
	}}
	local, _ := ResolveLocalDriverVersion(driver, []model.Device{device}, []string{device.DriverVersion}, present)
	if local != "1.0.2.0" {
		t.Fatalf("local version = %q, want the software-side 1.0.2.0", local)
	}
	if got := CompareDriverStatus(driver.Version, local, ""); got != model.StatusUpdate {
		t.Fatalf("status = %q, want Update", got)
	}
}

// A local version of "" reaching the compare step means "a device matched but
// carries no driver version", never "we have no idea" — TestDriverApplicable
// already removed every driver with no matching device before the compare runs.
// This test pins that guarantee, because the automatic set acts on the
// Not installed status and that status is only meaningful if "" is impossible
// to reach without a matched device.
func TestEmptyLocalVersionAlwaysHasAMatchedDevice(t *testing.T) {
	driver := &model.Driver{
		DriverCode: "DRVX", DriverName: "Lenovo Energy Management 驱动",
		Version: "2.0.0.30", HardwareID: `ACPI\VEN_VPC&DEV_2004`,
	}
	if TestDriverApplicable(driver, nil) {
		t.Fatal("a driver with no matching device must not be applicable")
	}
	// With a matching device the driver IS applicable, and an empty version then
	// means the device is present without a bound driver.
	devices := []model.Device{{PnpDeviceID: `ACPI\VPC2004\0`, DeviceID: `ACPI\VEN_VPC&DEV_2004`}}
	if !TestDriverApplicable(driver, devices) {
		t.Fatal("driver with a matching device must be applicable")
	}
	local, _ := ResolveLocalDriverVersion(driver, devices, []string{""}, &model.SoftwareSnapshot{})
	if local != "" {
		t.Fatalf("local version = %q, want empty for a device with no bound driver", local)
	}
	if got := CompareDriverStatus(driver.Version, local, ""); got != model.StatusNotInstalled {
		t.Fatalf("status = %q, want Not installed", got)
	}
}
