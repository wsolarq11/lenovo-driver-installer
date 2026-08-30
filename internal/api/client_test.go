package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
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
