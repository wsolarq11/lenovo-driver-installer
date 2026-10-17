package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"lenovo-driver/internal/model"
)

type roundTripFunc func(*http.Request) *http.Response

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req), nil
}

func TestFindOSEntryCaseInsensitive(t *testing.T) {
	osList := []model.OSListEntry{{OSID: "248", OSName: "Windows 11 64-bit"}}
	entry, ok := findOSEntry(osList, "windows 11", "")
	if !ok || entry.OSID != "248" {
		t.Fatalf("case-insensitive OS lookup failed: %#v, ok=%v", entry, ok)
	}
}

func TestGetDriverObjectsReturnsErrorWhenBothSourcesFail(t *testing.T) {
	client := NewClient()
	client.HTTP = &http.Client{Transport: roundTripFunc(func(req *http.Request) *http.Response {
		return &http.Response{StatusCode: http.StatusInternalServerError, Body: io.NopCloser(bytes.NewReader(nil))}
	})}
	_, err := client.GetDriverObjects(context.Background(), "cat", "248", "QuickFix")
	if err == nil {
		t.Fatal("expected GetDriverObjects to return an error when both sources fail")
	}
}

func TestGetDriverObjectsReturnsQuickFix(t *testing.T) {
	client := NewClient()
	client.HTTP = &http.Client{Transport: roundTripFunc(func(req *http.Request) *http.Response {
		resp := QuickFixResponse{StatusCode: "200"}
		resp.Data.DriverList = []driverRow{{
			DriverCode: "d1",
			FileName:   "d1.exe",
			FilePath:   "https://cdn.example.invalid/d1.exe",
		}}
		body, _ := json.Marshal(resp)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewReader(body)),
		}
	})}
	result, err := client.GetDriverObjects(context.Background(), "cat", "248", "QuickFix")
	if err != nil {
		t.Fatal(err)
	}
	if result.Source != "QuickFix" || len(result.Drivers) != 1 {
		t.Fatalf("GetDriverObjects = %#v", result)
	}
}

func TestFetchQuickFixReportsContractDrift(t *testing.T) {
	client := NewClient()
	client.HTTP = &http.Client{Transport: roundTripFunc(func(req *http.Request) *http.Response {
		resp := QuickFixResponse{StatusCode: "200"}
		// Non-empty list, but every row lacks FileName/FilePath so the parser
		// drops them all: this is interface drift, not "no drivers".
		resp.Data.DriverList = []driverRow{{DriverCode: "d1", Version: "1.0"}}
		body, _ := json.Marshal(resp)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(body))}
	})}
	_, err := client.fetchQuickFix(context.Background(), "cat", "248")
	var drift *contractDriftError
	if !errors.As(err, &drift) {
		t.Fatalf("expected contractDriftError, got %v", err)
	}
	if drift.Source != "QuickFix" || drift.RowCount != 1 {
		t.Fatalf("drift = %#v", drift)
	}
}

func TestFetchWebReportsContractDrift(t *testing.T) {
	client := NewClient()
	client.HTTP = &http.Client{Transport: roundTripFunc(func(req *http.Request) *http.Response {
		resp := webResponse{StatusCode: json.Number("200")}
		resp.Data.PartList = []webPart{{Drivelist: []driverRow{{DriverCode: "d1", Version: "1.0"}}}}
		body, _ := json.Marshal(resp)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(body))}
	})}
	_, err := client.fetchWeb(context.Background(), "cat", "248")
	var drift *contractDriftError
	if !errors.As(err, &drift) {
		t.Fatalf("expected contractDriftError, got %v", err)
	}
	if drift.Source != "Web" || drift.RowCount != 1 {
		t.Fatalf("drift = %#v", drift)
	}
}

func TestFetchQuickFixEmptyListIsNotDrift(t *testing.T) {
	client := NewClient()
	client.HTTP = &http.Client{Transport: roundTripFunc(func(req *http.Request) *http.Response {
		resp := QuickFixResponse{StatusCode: "200"}
		body, _ := json.Marshal(resp)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(body))}
	})}
	_, err := client.fetchQuickFix(context.Background(), "cat", "248")
	var drift *contractDriftError
	if errors.As(err, &drift) {
		t.Fatalf("empty list must not be reported as drift: %v", err)
	}
	if err == nil {
		t.Fatal("empty list should still error")
	}
}

func TestGetDriverObjectsReportsDriftOnFallback(t *testing.T) {
	client := NewClient()
	client.HTTP = &http.Client{Transport: roundTripFunc(func(req *http.Request) *http.Response {
		if req.URL.Path == "/home/driver/SearchForXbb" {
			resp := QuickFixResponse{StatusCode: "200"}
			resp.Data.DriverList = []driverRow{{DriverCode: "d1", Version: "1.0"}}
			body, _ := json.Marshal(resp)
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(body))}
		}
		resp := webResponse{StatusCode: json.Number("200")}
		resp.Data.PartList = []webPart{{Drivelist: []driverRow{{DriverCode: "d2", FileName: "d2.exe", FilePath: "https://download.lenovo.com/d2.exe"}}}}
		body, _ := json.Marshal(resp)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(body))}
	})}
	result, err := client.GetDriverObjects(context.Background(), "cat", "248", "QuickFix")
	if err != nil {
		t.Fatal(err)
	}
	if result.Source != "Web" || len(result.Drivers) != 1 {
		t.Fatalf("GetDriverObjects = %#v", result)
	}
	if result.DriftWarning == "" || !strings.Contains(result.DriftWarning, "QuickFix") {
		t.Fatalf("expected QuickFix drift warning, got %q", result.DriftWarning)
	}
}

func TestFetchQuickFixReportsInterfaceGateOnHTTPStatus(t *testing.T) {
	client := NewClient()
	client.HTTP = &http.Client{Transport: roundTripFunc(func(req *http.Request) *http.Response {
		return &http.Response{StatusCode: http.StatusUnauthorized, Body: io.NopCloser(bytes.NewReader([]byte(`{}`)))}
	})}
	_, err := client.fetchQuickFix(context.Background(), "cat", "248")
	var gate *interfaceGateError
	if !errors.As(err, &gate) {
		t.Fatalf("expected interfaceGateError, got %v", err)
	}
	if gate.Source != "QuickFix" || !strings.Contains(gate.Reason, "401") {
		t.Fatalf("gate = %#v", gate)
	}
}

func TestFetchQuickFixNonJSONIsGate(t *testing.T) {
	client := NewClient()
	client.HTTP = &http.Client{Transport: roundTripFunc(func(req *http.Request) *http.Response {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader([]byte(`<html>login</html>`)))}
	})}
	_, err := client.fetchQuickFix(context.Background(), "cat", "248")
	var gate *interfaceGateError
	if !errors.As(err, &gate) {
		t.Fatalf("expected interfaceGateError, got %v", err)
	}
}

func TestFetchQuickFixUnrelatedJSONShapeIsGate(t *testing.T) {
	client := NewClient()
	client.HTTP = &http.Client{Transport: roundTripFunc(func(req *http.Request) *http.Response {
		// Valid JSON but an unrelated shape (e.g. an auth challenge).
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader([]byte(`{"code":401,"message":"unauthorized"}`)))}
	})}
	_, err := client.fetchQuickFix(context.Background(), "cat", "248")
	var gate *interfaceGateError
	if !errors.As(err, &gate) {
		t.Fatalf("expected interfaceGateError, got %v", err)
	}
}

func TestFetchWebUnrelatedJSONShapeIsGate(t *testing.T) {
	client := NewClient()
	client.HTTP = &http.Client{Transport: roundTripFunc(func(req *http.Request) *http.Response {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader([]byte(`{"code":401,"message":"unauthorized"}`)))}
	})}
	_, err := client.fetchWeb(context.Background(), "cat", "248")
	var gate *interfaceGateError
	if !errors.As(err, &gate) {
		t.Fatalf("expected interfaceGateError, got %v", err)
	}
	if gate.Source != "Web" {
		t.Fatalf("gate = %#v", gate)
	}
}

func TestGetDriverObjectsReportsGateOnFallback(t *testing.T) {
	client := NewClient()
	client.HTTP = &http.Client{Transport: roundTripFunc(func(req *http.Request) *http.Response {
		if req.URL.Path == "/home/driver/SearchForXbb" {
			return &http.Response{StatusCode: http.StatusUnauthorized, Body: io.NopCloser(bytes.NewReader([]byte(`{}`)))}
		}
		resp := webResponse{StatusCode: json.Number("200")}
		resp.Data.PartList = []webPart{{Drivelist: []driverRow{{DriverCode: "d2", FileName: "d2.exe", FilePath: "https://newdriverdl.lenovo.com.cn/d2.exe"}}}}
		body, _ := json.Marshal(resp)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(body))}
	})}
	result, err := client.GetDriverObjects(context.Background(), "cat", "248", "QuickFix")
	if err != nil {
		t.Fatal(err)
	}
	if result.Source != "Web" || len(result.Drivers) != 1 {
		t.Fatalf("GetDriverObjects = %#v", result)
	}
	if result.DriftWarning == "" || !strings.Contains(result.DriftWarning, "gated") {
		t.Fatalf("expected gate warning, got %q", result.DriftWarning)
	}
}

// servingFixture returns a transport that answers every request with the
// recorded fixture, so a test exercises the real response shape instead of a
// struct it built itself.
func servingFixture(t *testing.T, path string) *http.Client {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Client{Transport: roundTripFunc(func(req *http.Request) *http.Response {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewReader(body)),
		}
	})}
}

// TestFetchWebParsesRecordedResponse is the regression test for the decode bug:
// the recorded response carries statusCode and localOSID as JSON numbers, and
// the string-typed struct rejected it. The whole webpage source was dead while
// every hand-written fixture stayed green.
func TestFetchWebParsesRecordedResponse(t *testing.T) {
	client := NewClient()
	client.HTTP = servingFixture(t, "testdata/web_real_82jq_os42.json")
	drivers, err := client.fetchWeb(context.Background(), "3124166", "42")
	if err != nil {
		t.Fatalf("the recorded webpage response must decode: %v", err)
	}
	if len(drivers) != 17 {
		t.Fatalf("expected 17 drivers from the recorded response, got %d", len(drivers))
	}
	for _, d := range drivers {
		if d.PartName == "" {
			t.Fatalf("%s: part name was dropped even though every recorded part carries one", d.DriverCode)
		}
	}
}

func TestResolveOSEntryPrefersWebList(t *testing.T) {
	var paths []string
	client := NewClient()
	client.HTTP = &http.Client{Transport: roundTripFunc(func(req *http.Request) *http.Response {
		paths = append(paths, req.URL.Path)
		body := `{"statusCode":200,"data":{"localOSID":42,` +
			`"osList":[{"OSID":"248","OSName":"Windows 11 64-bit"},{"OSID":"42","OSName":"Windows 10 64-bit"}]}}`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}
	})}
	res, err := client.ResolveOSEntry(context.Background(), "3124166", "Windows 10 64-bit", "Windows 10")
	if err != nil {
		t.Fatal(err)
	}
	if res.Source != "Web" || res.OSEntry.OSID != "42" {
		t.Fatalf("resolved = %#v", res)
	}
	if len(paths) != 1 {
		t.Fatalf("a usable webpage OS list must be one request, got %v", paths)
	}
}

// TestResolveOSEntryFallsBackToLocalOSID pins the behaviour that replaces the
// hardcoded OSID probes: when no name matches, the endpoint's own localOSID
// decides, and the list is not requested twice.
func TestResolveOSEntryFallsBackToLocalOSID(t *testing.T) {
	client := NewClient()
	client.HTTP = &http.Client{Transport: roundTripFunc(func(req *http.Request) *http.Response {
		body := `{"statusCode":200,"data":{"localOSID":42,` +
			`"osList":[{"OSID":"248","OSName":"Windows 11 64-bit"},{"OSID":"42","OSName":"Windows 10 64-bit"}]}}`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}
	})}
	res, err := client.ResolveOSEntry(context.Background(), "3124166", "Windows Server 2025", "")
	if err != nil {
		t.Fatal(err)
	}
	if res.OSEntry.OSID != "42" || res.Source != "Web" {
		t.Fatalf("localOSID fallback failed: %#v", res)
	}
}

// TestResolveOSEntryNamesTheWebFailure: when the webpage list cannot be decoded
// the log must carry the decode error, not a claim that the network was down.
func TestResolveOSEntryNamesTheWebFailure(t *testing.T) {
	client := NewClient()
	client.HTTP = &http.Client{Transport: roundTripFunc(func(req *http.Request) *http.Response {
		if req.URL.Path == "/home/driver/SearchForXbb" {
			body := `{"StatusCode":"200","Data":{"osList":[{"OSID":"42","OSName":"Windows 10 64-bit"}]}}`
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`<html>login</html>`))}
	})}
	res, err := client.ResolveOSEntry(context.Background(), "3124166", "Windows 10", "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Source != "QuickFix" {
		t.Fatalf("expected the QuickFix fallback, got %#v", res)
	}
	if !strings.Contains(res.WebFailure, "JSON") {
		t.Fatalf("the failure must name the decode problem, got %q", res.WebFailure)
	}
}

// TestResolveOSEntryQuickFixProbeSendsNoHardcodedOSID pins that the OS list is
// read with the probe osid rather than by guessing which OSIDs exist.
func TestResolveOSEntryQuickFixProbeSendsNoHardcodedOSID(t *testing.T) {
	var sent string
	client := NewClient()
	client.HTTP = &http.Client{Transport: roundTripFunc(func(req *http.Request) *http.Response {
		if req.URL.Path == "/home/driver/SearchForXbb" {
			var body struct {
				OSID string `json:"osid"`
			}
			_ = json.NewDecoder(req.Body).Decode(&body)
			sent = body.OSID
			resp := `{"StatusCode":"200","Data":{"osList":[{"OSID":"42","OSName":"Windows 10 64-bit"}]}}`
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(resp))}
		}
		return &http.Response{StatusCode: http.StatusInternalServerError, Body: io.NopCloser(strings.NewReader(""))}
	})}
	res, err := client.ResolveOSEntry(context.Background(), "3124166", "Windows 10", "")
	if err != nil {
		t.Fatal(err)
	}
	if res.OSEntry.OSID != "42" {
		t.Fatalf("resolved = %#v", res)
	}
	if sent != osIDProbe {
		t.Fatalf("QuickFix OS-list probe sent osid %q, want %q", sent, osIDProbe)
	}
}
