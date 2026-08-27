package api

import (
	"encoding/json"
	"strings"
	"testing"
)

func quickFixPayload() []byte {
	return []byte(`{
  "StatusCode": "200",
  "Message": "ok",
  "Data": {
    "partList": [{"PartID": "p1", "PartName": "Audio"}],
    "osList": [{"OSID": "42", "OSName": "Windows 10 64-bit"}],
    "driverList": [
      {
        "PartID": "p1",
        "DriverName": "Realtek Audio",
        "DriverCode": "d1",
        "DriverEdtionId": "123",
        "Version": "6.0.9363.1",
        "FileName": "audio.exe",
        "FilePath": "https://example.invalid/audio.exe",
        "FileSize": "1 MB",
        "FileType": "exe",
        "InstallCode": "/VERYSILENT",
        "Bootfile": "",
        "HardwareId": "10EC_0257",
        "MD5": "ABCDEF",
        "Status": "",
        "IsEnable": "",
        "DriverIssuedDateTime": "2026/1/1"
      }
    ]
  }
}`)
}

func TestParseQuickFix(t *testing.T) {
	var resp QuickFixResponse
	if err := decodeJSON(quickFixPayload(), &resp); err != nil {
		t.Fatal(err)
	}
	drivers := ParseQuickFix(&resp, "42")
	if len(drivers) != 1 {
		t.Fatalf("expected 1 driver, got %d", len(drivers))
	}
	driver := drivers[0]
	if driver.PartID != "p1" || driver.PartName != "Audio" {
		t.Fatalf("part mapping failed: %#v", driver)
	}
	if driver.OfficialMD5 != "abcdef" {
		t.Fatalf("MD5 normalization failed: %q", driver.OfficialMD5)
	}
	if driver.IssuedDate.IsZero() {
		t.Fatal("issued date should be parsed")
	}
}

func TestParseWeb(t *testing.T) {
	payload := []byte(`{
  "statusCode": "200",
  "data": {
    "localOSID": "42",
    "osList": [{"OSID": "42", "OSName": "Windows 10 64-bit"}],
    "partList": [{"PartID": "p1", "drivelist": [{
      "PartID": "p1",
      "DriverName": "AMD VGA",
      "DriverCode": "d2",
      "DriverEditionId": "456",
      "Version": "30.0.14052.9003",
      "FileName": "amd.exe",
      "FilePath": "https://example.invalid/amd.exe",
      "FileSize": "1 MB",
      "FileType": "exe",
      "InstallCode": "/VERYSILENT",
      "HardwareId": "1002_1638",
      "MD5": "",
      "Status": "",
      "IsEnable": "",
      "DriverIssuedDateTime": "2026/1/1"
    }]}]
  }
}`)
	var resp WebResponse
	if err := decodeJSON(payload, &resp); err != nil {
		t.Fatal(err)
	}
	drivers := ParseWeb(&resp, "42")
	if len(drivers) != 1 {
		t.Fatalf("expected 1 driver, got %d", len(drivers))
	}
	if drivers[0].SourceAPI != "Web" || drivers[0].OfficialMD5 != "" {
		t.Fatalf("web normalization failed: %#v", drivers[0])
	}
}

func decodeJSON(data []byte, target any) error {
	return json.NewDecoder(strings.NewReader(string(data))).Decode(target)
}
