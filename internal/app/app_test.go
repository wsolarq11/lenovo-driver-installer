package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lenovo-driver/internal/api"
	"lenovo-driver/internal/download"
	"lenovo-driver/internal/model"
)

func TestParseOptionsHelp(t *testing.T) {
	app := New(&bytes.Buffer{}, &bytes.Buffer{}, strings.NewReader(""))
	code := app.Run([]string{"-Help"})
	if code != 0 {
		t.Fatalf("Help exit code = %d", code)
	}
}

func TestInvalidFlagCombination(t *testing.T) {
	app := New(&bytes.Buffer{}, &bytes.Buffer{}, strings.NewReader(""))
	code := app.Run([]string{"-CurrentOSOnly", "-LatestAcrossOS"})
	if code != 2 {
		t.Fatalf("invalid combination exit code = %d", code)
	}
}

type appRoundTripFunc func(*http.Request) *http.Response

func (f appRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req), nil
}

func TestDownloadVerifiedRefreshes403URL(t *testing.T) {
	downloadCalls := 0
	content := []byte("driver-bytes")
	client := &http.Client{Transport: appRoundTripFunc(func(req *http.Request) *http.Response {
		if req.URL.Path == "/home/driver/SearchForXbb" {
			payload := map[string]any{
				"StatusCode": "200",
				"Message":    "",
				"Data": map[string]any{
					"partList": []any{},
					"osList":   []any{},
					"driverList": []map[string]any{{
						"DriverCode": "d1",
						"FileName":   "driver.exe",
						"FilePath":   "https://download.example/new.exe",
					}},
				},
			}
			body, _ := json.Marshal(payload)
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(body))}
		}
		downloadCalls++
		if strings.HasSuffix(req.URL.Path, "/old.exe") {
			return &http.Response{StatusCode: http.StatusForbidden, Body: io.NopCloser(bytes.NewReader(nil))}
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(content))}
	})}

	app := New(&bytes.Buffer{}, &bytes.Buffer{}, strings.NewReader(""))
	app.APIClient = api.NewClient()
	app.APIClient.HTTP = client
	app.Downloader = download.NewDownloader()
	app.Downloader.Client = client
	downloadDir := t.TempDir()
	outFile := filepath.Join(downloadDir, "d1_driver.exe")
	driver := &model.Driver{DriverCode: "d1", FileName: "driver.exe", FilePath: "https://download.example/old.exe", FileSize: "12 B"}
	_, err := app.downloadVerified(context.Background(), &Options{}, driver, outFile, 12, "cat", "248", []model.OSListEntry{{OSID: "248"}}, "QuickFix")
	if err != nil {
		t.Fatal(err)
	}
	got, readErr := os.ReadFile(outFile)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("downloaded content = %q", got)
	}
	if downloadCalls != 4 {
		t.Fatalf("download calls = %d, want 4 (3 old URL retries + refreshed URL)", downloadCalls)
	}
	if driver.FilePath != "https://download.example/new.exe" {
		t.Fatalf("driver URL was not refreshed: %q", driver.FilePath)
	}
}

func TestShowActionPreviewIncludesDriverRows(t *testing.T) {
	var out bytes.Buffer
	driver := &model.Driver{DriverName: "Audio", Version: "1.0.0.1", LocalVersion: "1.0.0.0", CompareStatus: "Update"}
	showActionPreview(&out, 'y', "update-only", []*model.Driver{driver})
	text := out.String()
	if !strings.Contains(text, "Audio") || !strings.Contains(text, "1.0.0.1") || !strings.Contains(text, "Update") {
		t.Fatalf("preview did not include exact driver rows:\n%s", text)
	}
}

func TestReadHistoryStripsUTF8BOM(t *testing.T) {
	app := New(&bytes.Buffer{}, &bytes.Buffer{}, strings.NewReader(""))
	app.HistoryPath = filepath.Join(t.TempDir(), "history.csv")
	row := []string{"2026-01-01 00:00:00", "d1", "42", "Windows 10 64-bit", "Audio", "1.0.0.1", "", "", "audio.exe", "abc", "QuickFix", "Installed", "exit=0"}
	content := "\uFEFF" + strings.Join(historyHeader, ",") + "\n" + strings.Join(row, ",") + "\n"
	if err := os.WriteFile(app.HistoryPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	records := app.ReadHistory(context.Background())
	if len(records) != 1 || records[0].DriverCode != "d1" || records[0].Result != "Installed" {
		t.Fatalf("unexpected history records: %#v", records)
	}
}

func TestSelectByCodes(t *testing.T) {
	drivers := []*model.Driver{
		{DriverCode: "d1"},
		{DriverCode: "d2"},
	}
	selected := selectByCodes(drivers, "d2")
	if len(selected) != 1 || selected[0].DriverCode != "d2" {
		t.Fatalf("unexpected GUI selection: %#v", selected)
	}
}

func TestNextOSLabelIncludesOSID(t *testing.T) {
	osList := []model.OSListEntry{
		{OSID: "42", OSName: "Windows 10 64-bit"},
		{OSID: "248", OSName: "Windows 11 64-bit"},
	}
	got := nextOSLabel(osList, "42")
	if got != "Windows 11 64-bit (OSID 248)" {
		t.Fatalf("nextOSLabel = %q", got)
	}
}
