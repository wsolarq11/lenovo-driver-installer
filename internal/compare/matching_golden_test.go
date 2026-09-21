package compare

import (
	"strings"
	"testing"

	"lenovo-driver/internal/model"
)

// The device shapes below follow the Windows PnP naming conventions the
// matching table was built against. They are regression fixtures for the regex
// table, not captured 82JQ evidence: the 82JQ driver names come from
// docs/records/evidence-82jq.md, while the device names are representative
// Windows device-tree strings (inference, not a real inventory dump).
func TestDriverApplicableGolden(t *testing.T) {
	local := []model.Device{
		{Name: "Intel(R) Wi-Fi 6 AX201 160MHz", Class: "Net"},
		{Name: "Realtek Bluetooth Adapter", Class: "Bluetooth"},
		{Name: "AMD Serial IO Controller", Class: "System"},
		{Name: "Realtek(R) Audio", Class: "MEDIA"},
		{Name: "Realtek PCIe GbE Family Controller", Class: "Net"},
		{Name: "AMD Radeon(TM) Graphics", Class: "Display"},
		{Name: "NVIDIA GeForce RTX 3060 Laptop GPU", Class: "Display"},
	}
	cases := []struct {
		name   string
		driver *model.Driver
		want   bool
	}{
		{"Intel WLAN", &model.Driver{DriverName: "Intel WLAN"}, true},
		{"Bluetooth", &model.Driver{DriverName: "Bluetooth"}, true},
		{"AMD Serial-IO", &model.Driver{DriverName: "AMD Serial-IO"}, true},
		{"Realtek Audio", &model.Driver{DriverName: "Realtek Audio"}, true},
		{"Realtek LAN", &model.Driver{DriverName: "Realtek LAN"}, true},
		{"AMD VGA", &model.Driver{DriverName: "AMD VGA"}, true},
		{"NVIDIA VGA", &model.Driver{DriverName: "NVIDIA VGA"}, true},
		{"Lenovo Fn software", &model.Driver{DriverName: "Lenovo Fn"}, true},
		{"absent Camera", &model.Driver{DriverName: "Camera"}, false},
		{"absent Cardreader", &model.Driver{DriverName: "Cardreader"}, false},
	}
	for _, tc := range cases {
		if got := TestDriverApplicable(tc.driver, local); got != tc.want {
			t.Fatalf("TestDriverApplicable(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestMatchingLocalDevicesGolden(t *testing.T) {
	local := []model.Device{
		{Name: "Realtek(R) Audio", Class: "MEDIA"},
		{Name: "Intel(R) Wi-Fi 6 AX201 160MHz", Class: "Net"},
	}
	matched := GetMatchingLocalDevices(&model.Driver{DriverName: "Realtek Audio"}, local)
	if len(matched) != 1 || matched[0].Name != "Realtek(R) Audio" {
		t.Fatalf("Realtek Audio matched = %#v, want the Realtek audio device", matched)
	}
}

func TestDiagnoseDriverNonMatch(t *testing.T) {
	amd := []model.Device{{Name: "AMD Radeon Graphics", Class: "Display"}}
	if got := DiagnoseDriverNonMatch(&model.Driver{HardwareID: "10EC_0257", DriverName: "Realtek Audio"}, amd); got != "hardware id 10EC_0257 matched no local PnP/device ID" {
		t.Fatalf("hardware id diagnosis = %q", got)
	}
	if got := DiagnoseDriverNonMatch(&model.Driver{HardwareID: "0408_2010,04CA_7036,04CA_7047"}, amd); got != "hardware ids 0408_2010, 04CA_7036, ... (3 ids) matched no local PnP/device ID" {
		t.Fatalf("multi-id diagnosis = %q", got)
	}
	if got := DiagnoseDriverNonMatch(&model.Driver{DriverName: "Battery Driver"}, amd); got != `no matching rule for driver name "Battery Driver"` {
		t.Fatalf("no-rule diagnosis = %q", got)
	}
	if got := DiagnoseDriverNonMatch(&model.Driver{DriverName: "Realtek LAN"}, amd); !strings.Contains(got, `rule "Realtek LAN"`) {
		t.Fatalf("rule diagnosis = %q, want it to name the Realtek LAN rule", got)
	}
}
