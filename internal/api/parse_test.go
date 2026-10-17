package api

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"lenovo-driver/internal/model"
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
	// The live endpoint sends statusCode and localOSID as bare JSON numbers.
	// Quoting them here is what let the string-typed struct stay green while
	// every real response failed to decode.
	payload := []byte(`{
  "statusCode": 200,
  "data": {
    "localOSID": 42,
    "osList": [{"OSID": "42", "OSName": "Windows 10 64-bit"}],
    "partList": [{"PartID": "p1", "PartName": "Audio", "drivelist": [{
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
	var resp webResponse
	if err := decodeJSON(payload, &resp); err != nil {
		t.Fatal(err)
	}
	drivers := parseWeb(&resp, "42")
	if len(drivers) != 1 {
		t.Fatalf("expected 1 driver, got %d", len(drivers))
	}
	if drivers[0].SourceAPI != "Web" || drivers[0].OfficialMD5 != "" {
		t.Fatalf("web normalization failed: %#v", drivers[0])
	}
	if drivers[0].PartName != "Audio" {
		t.Fatalf("part name must come from the partList entry: %#v", drivers[0])
	}
}

// TestWebResponseAcceptsBothStatusSpellings pins the tolerance that makes the
// fix durable: the live endpoint uses numbers today, but a server-side flip to
// a quoted string must not disable the source again.
func TestWebResponseAcceptsBothStatusSpellings(t *testing.T) {
	for _, payload := range []string{
		`{"statusCode":200,"data":{"localOSID":42}}`,
		`{"statusCode":"200","data":{"localOSID":"42"}}`,
	} {
		var resp webResponse
		if err := decodeJSON([]byte(payload), &resp); err != nil {
			t.Fatalf("%s: %v", payload, err)
		}
		if resp.StatusCode.String() != "200" || resp.Data.LocalOSID.String() != "42" {
			t.Fatalf("%s: decoded as %q/%q", payload, resp.StatusCode, resp.Data.LocalOSID)
		}
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

// TestParseWebContractGolden locks the webpage API contract against a response
// recorded from the live endpoint (82JQ, OSID 42) with per-fetch download
// tokens redacted. It is a capture, not a hand-written sample: the previous
// golden quoted statusCode and localOSID, which made a struct that could not
// decode any real response look correct.
//
// The shape pinned here is the one the parser depends on: numbers at the top
// level, PartName on the part (never on the row), DriverEdtionId (typo and
// all), InstallCode, DriverIssuedDateTime, and no MD5 anywhere.
func TestParseWebContractGolden(t *testing.T) {
	data, err := os.ReadFile("testdata/web_real_82jq_os42.json")
	if err != nil {
		t.Fatal(err)
	}
	var resp webResponse
	if err := decodeJSON(data, &resp); err != nil {
		t.Fatalf("the recorded webpage response no longer decodes: %v", err)
	}
	if resp.StatusCode.String() != "200" || resp.Data.LocalOSID.String() != "42" {
		t.Fatalf("recorded status/localOSID drifted: %q/%q", resp.StatusCode, resp.Data.LocalOSID)
	}
	drivers := parseWeb(&resp, "42")
	if len(drivers) != 17 {
		t.Fatalf("expected the 17 recorded rows to parse, got %d", len(drivers))
	}
	byCode := map[string]*model.Driver{}
	for _, d := range drivers {
		byCode[d.DriverCode] = d
	}
	audio := byCode["DRV202102040011"]
	if audio == nil {
		t.Fatal("recorded audio row DRV202102040011 is missing from the parse result")
	}
	if audio.PartName != "声卡" || audio.PartID != "32" {
		t.Fatalf("part name must be joined from partList: %#v", audio)
	}
	if audio.Version != "6.0.9088.1" || audio.FileName != "Audio-2GY503AFZLL741C0.exe" {
		t.Fatalf("recorded row fields drifted: %#v", audio)
	}
	if audio.DriverEditionID != 82116 {
		t.Fatalf("DriverEdtionId (typo spelling) should map to edition: %d", audio.DriverEditionID)
	}
	if audio.InstallCode != "/add-driver *.inf /install /subdirs" {
		t.Fatalf("InstallCode should be preserved: %q", audio.InstallCode)
	}
	if audio.IssuedDate.IsZero() {
		t.Fatal("DriverIssuedDateTime should be parsed into IssuedDate")
	}
	if audio.SourceAPI != "Web" {
		t.Fatalf("source should be Web: %q", audio.SourceAPI)
	}
	for _, d := range drivers {
		if d.OfficialMD5 != "" {
			t.Fatalf("%s: the webpage source carries no MD5, got %q", d.DriverCode, d.OfficialMD5)
		}
	}
}
