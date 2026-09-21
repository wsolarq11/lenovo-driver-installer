package api

import (
	"encoding/json"
	"os"
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

// TestParseQuickFixContractGolden locks the real QuickFix field names and the
// normalization rules they feed. DriverCode/Version/FileName come from
// docs/records/evidence-82jq.md; the remaining values are fixtures whose only
// purpose is to exercise field mapping (MD5 case, Parameter fallback, the
// DriverEdtionId typo, PubTime date extraction).
func TestParseQuickFixContractGolden(t *testing.T) {
	data, err := os.ReadFile("testdata/quickfix_contract_golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var resp QuickFixResponse
	if err := decodeJSON(data, &resp); err != nil {
		t.Fatal(err)
	}
	drivers := ParseQuickFix(&resp, "248")
	if len(drivers) != 1 {
		t.Fatalf("expected 1 driver, got %d", len(drivers))
	}
	driver := drivers[0]
	if driver.DriverCode != "DRV202009030023" || driver.Version != "2.0.0.25" || driver.FileName != "Fn-01LF0AAFAR2W6JB0.exe" {
		t.Fatalf("documented QuickFix fields drifted: %#v", driver)
	}
	if driver.OfficialMD5 != "abcdef1234567890" {
		t.Fatalf("MD5 should be lowercased: %q", driver.OfficialMD5)
	}
	if driver.InstallCode != "/VERYSILENT /NORESTART" || driver.InstallParameter != "/VERYSILENT /NORESTART" {
		t.Fatalf("Parameter fallback failed: %#v", driver)
	}
	if driver.DriverEditionID != 1678 {
		t.Fatalf("DriverEdtionId typo should map to edition: %d", driver.DriverEditionID)
	}
	if driver.IssuedDate.IsZero() {
		t.Fatal("PubTime should be parsed into IssuedDate")
	}
	if driver.SourceAPI != "QuickFix" {
		t.Fatalf("source should be QuickFix: %q", driver.SourceAPI)
	}
}

// TestParseWebContractGolden locks the webpage API field names and their
// divergences from QuickFix: DriverEditionId spelling, InstallCode (not
// Parameter), DriverIssuedDateTime, and no official MD5.
func TestParseWebContractGolden(t *testing.T) {
	data, err := os.ReadFile("testdata/web_contract_golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var resp WebResponse
	if err := decodeJSON(data, &resp); err != nil {
		t.Fatal(err)
	}
	drivers := ParseWeb(&resp, "248")
	if len(drivers) != 1 {
		t.Fatalf("expected 1 driver, got %d", len(drivers))
	}
	driver := drivers[0]
	if driver.SourceAPI != "Web" || driver.OfficialMD5 != "" {
		t.Fatalf("web source should have no MD5: %#v", driver)
	}
	if driver.InstallCode != "/VERYSILENT" {
		t.Fatalf("InstallCode should be preserved: %q", driver.InstallCode)
	}
	if driver.DriverEditionID != 1722 {
		t.Fatalf("DriverEditionId should map to edition: %d", driver.DriverEditionID)
	}
	if driver.FileName != "audio-2GY507AFZLL741C0.exe" {
		t.Fatalf("FileName should keep basename: %q", driver.FileName)
	}
	if driver.IssuedDate.IsZero() {
		t.Fatal("DriverIssuedDateTime should be parsed into IssuedDate")
	}
}
