package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
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
	var drift *ContractDriftError
	if !errors.As(err, &drift) {
		t.Fatalf("expected ContractDriftError, got %v", err)
	}
	if drift.Source != "QuickFix" || drift.RowCount != 1 {
		t.Fatalf("drift = %#v", drift)
	}
}

func TestFetchWebReportsContractDrift(t *testing.T) {
	client := NewClient()
	client.HTTP = &http.Client{Transport: roundTripFunc(func(req *http.Request) *http.Response {
		resp := WebResponse{StatusCode: "200"}
		resp.Data.PartList = []webPart{{Drivelist: []driverRow{{DriverCode: "d1", Version: "1.0"}}}}
		body, _ := json.Marshal(resp)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(body))}
	})}
	_, err := client.fetchWeb(context.Background(), "cat", "248")
	var drift *ContractDriftError
	if !errors.As(err, &drift) {
		t.Fatalf("expected ContractDriftError, got %v", err)
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
	var drift *ContractDriftError
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
		resp := WebResponse{StatusCode: "200"}
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
	var gate *InterfaceGateError
	if !errors.As(err, &gate) {
		t.Fatalf("expected InterfaceGateError, got %v", err)
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
	var gate *InterfaceGateError
	if !errors.As(err, &gate) {
		t.Fatalf("expected InterfaceGateError, got %v", err)
	}
}

func TestFetchQuickFixUnrelatedJSONShapeIsGate(t *testing.T) {
	client := NewClient()
	client.HTTP = &http.Client{Transport: roundTripFunc(func(req *http.Request) *http.Response {
		// Valid JSON but an unrelated shape (e.g. an auth challenge).
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader([]byte(`{"code":401,"message":"unauthorized"}`)))}
	})}
	_, err := client.fetchQuickFix(context.Background(), "cat", "248")
	var gate *InterfaceGateError
	if !errors.As(err, &gate) {
		t.Fatalf("expected InterfaceGateError, got %v", err)
	}
}

func TestFetchWebUnrelatedJSONShapeIsGate(t *testing.T) {
	client := NewClient()
	client.HTTP = &http.Client{Transport: roundTripFunc(func(req *http.Request) *http.Response {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader([]byte(`{"code":401,"message":"unauthorized"}`)))}
	})}
	_, err := client.fetchWeb(context.Background(), "cat", "248")
	var gate *InterfaceGateError
	if !errors.As(err, &gate) {
		t.Fatalf("expected InterfaceGateError, got %v", err)
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
		resp := WebResponse{StatusCode: "200"}
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
