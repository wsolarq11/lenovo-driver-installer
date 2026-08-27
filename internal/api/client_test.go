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

func TestGetRefreshedDriverURL(t *testing.T) {
	client := NewClient()
	client.HTTP = &http.Client{Transport: roundTripFunc(func(req *http.Request) *http.Response {
		resp := QuickFixResponse{StatusCode: "200"}
		resp.Data.DriverList = []driverRow{{
			DriverCode: "d1",
			FileName:   "d1.exe",
			FilePath:   "https://cdn.example.invalid/d1-new.exe",
		}}
		body, _ := json.Marshal(resp)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewReader(body)),
		}
	})}
	osList := []model.OSListEntry{{OSID: "248"}, {OSID: "249"}}
	driver := &model.Driver{DriverCode: "d1"}
	got, err := client.GetRefreshedDriverURL(context.Background(), driver, "cat", "248", osList, true, true)
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://cdn.example.invalid/d1-new.exe" {
		t.Fatalf("GetRefreshedDriverURL = %q", got)
	}
}

func TestGetRefreshedDriverURLMissingReturnsError(t *testing.T) {
	client := NewClient()
	client.HTTP = &http.Client{Transport: roundTripFunc(func(req *http.Request) *http.Response {
		resp := QuickFixResponse{StatusCode: "200"}
		resp.Data.DriverList = []driverRow{{
			DriverCode: "other",
			FileName:   "other.exe",
			FilePath:   "https://cdn.example.invalid/other.exe",
		}}
		body, _ := json.Marshal(resp)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewReader(body)),
		}
	})}
	driver := &model.Driver{DriverCode: "d1"}
	_, err := client.GetRefreshedDriverURL(context.Background(), driver, "cat", "248", nil, false, true)
	if err == nil {
		t.Fatal("expected an error for a missing refreshed driver URL")
	}
}
