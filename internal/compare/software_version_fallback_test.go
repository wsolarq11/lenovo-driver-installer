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
	if !isSoftwareVersionedDriver(driver.DriverName) {
		t.Fatal("fixture is not a software-versioned driver; this test would prove nothing")
	}

	// No matching entry in InstalledApps, which is the case that used to fail.
	empty := &model.SoftwareSnapshot{InstalledApps: []model.InstalledApp{
		{DisplayName: "NVIDIA 图形驱动程序 546.30", DisplayVersion: "546.30"},
	}}
	local, vendor := ResolveLocalDriverVersion(driver, []model.Device{device}, []string{device.DriverVersion}, empty)
	if got := local.MeasuredVersion(); got != "15.11.29.65" {
		t.Fatalf("local version = %q, want the measured device version 15.11.29.65", got)
	}
	// deviceVendorFor does not recognise this device's name, so vendor comes
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
	if got := local.MeasuredVersion(); got != "1.0.2.0" {
		t.Fatalf("local version = %q, want the software-side 1.0.2.0", got)
	}
	if got := CompareDriverStatus(driver.Version, local, ""); got != model.StatusUpdate {
		t.Fatalf("status = %q, want Update", got)
	}
}

// An absent local version means "a device matched but carries no driver
// version". That reading is only sound while a matched device is known to exist:
// the always-applicable software rules (Lenovo Fn, Energy Management, X-Rite)
// make a driver applicable with no matching device at all, so applicability does
// NOT imply a matched device. Both halves are pinned here; treating the second
// as the first put evidence-free drivers into the automatic install set.
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
	if local.Kind() != model.LocalEvidenceAbsent {
		t.Fatalf("evidence kind = %v, want Absent for a matched device with no bound driver", local.Kind())
	}
	if got := CompareDriverStatus(driver.Version, local, ""); got != model.StatusNotInstalled {
		t.Fatalf("status = %q, want Not installed", got)
	}
	// The counterpart: no matched device at all is a different claim, and it
	// must not produce the status the automatic set acts on.
	none, _ := ResolveLocalDriverVersion(driver, nil, nil, &model.SoftwareSnapshot{})
	if none.Kind() != model.LocalEvidenceUnobserved {
		t.Fatalf("evidence kind = %v, want Unobserved with no matched device", none.Kind())
	}
	if got := CompareDriverStatus(driver.Version, none, ""); got == model.StatusNotInstalled {
		t.Fatal("an unobserved driver must not be reported as Not installed")
	}
}
