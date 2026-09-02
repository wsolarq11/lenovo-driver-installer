package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

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
	vc := &ViewContext{
		Opts:       Options{},
		CategoryID: "cat",
		OsList:     []model.OSListEntry{{OSID: "248"}},
	}
	_, err := app.downloadVerified(context.Background(), vc, driver, outFile, 12, "248", "QuickFix")
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

func TestInspectCachedFileRejectsMismatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "driver.bin")
	content := []byte(strings.Repeat("x", 5000))
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	usable, size, reason := inspectCachedFile(path, int64(len(content)), "", true)
	if !usable || size != int64(len(content)) || reason != "" {
		t.Fatalf("cached file should be usable: usable=%v size=%d reason=%q", usable, size, reason)
	}
	usable, _, reason = inspectCachedFile(path, int64(len(content)+2000), "", true)
	if usable || reason != "Cached file size mismatch" {
		t.Fatalf("cached file mismatch should reject: usable=%v reason=%q", usable, reason)
	}
}

func TestVerifyDownloadedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "driver.bin")
	content := []byte("driver bytes")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	size, err := verifyDownloadedFile(path, int64(len(content)), "", true, false)
	if err != nil || size != int64(len(content)) {
		t.Fatalf("verifyDownloadedFile = %d, err=%v", size, err)
	}
}

func TestQuoteWindowsArgument(t *testing.T) {
	cases := []struct {
		input, want string
	}{
		{"", `""`},
		{"82JQ", "82JQ"},
		{"82 JQ", `"82 JQ"`},
		{`a"b`, `"a\"b"`},
		{`C:\temp with space\file`, `"C:\temp with space\file"`},
		{"trailing\\", "trailing\\"},
	}
	for _, tc := range cases {
		if got := quoteWindowsArgument(tc.input); got != tc.want {
			t.Fatalf("quoteWindowsArgument(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestSelectByCodes(t *testing.T) {
	drivers := []*model.Driver{
		{DriverCode: "d1"},
		{DriverCode: "d2"},
	}
	selection := selectByCodes(drivers, "d2")
	if len(selection.Selected) != 1 || selection.Selected[0].DriverCode != "d2" {
		t.Fatalf("unexpected GUI selection: %#v", selection)
	}
}

func TestSelectByCodesRejectsPartialAndNotApplicable(t *testing.T) {
	drivers := []*model.Driver{
		{DriverCode: "d1", CompareStatus: model.StatusUpdate},
		{DriverCode: "d2", CompareStatus: model.StatusNotApplicable},
	}
	selection := selectByCodes(drivers, "d1,missing,d2")
	if len(selection.Selected) != 1 || selection.Selected[0].DriverCode != "d1" {
		t.Fatalf("unexpected selected drivers: %#v", selection)
	}
	if len(selection.Missing) != 1 || selection.Missing[0] != "missing" {
		t.Fatalf("unexpected missing codes: %#v", selection.Missing)
	}
	if len(selection.NotApplicable) != 1 || selection.NotApplicable[0] != "d2" {
		t.Fatalf("unexpected not-applicable codes: %#v", selection.NotApplicable)
	}
}

func TestAcquireSelectionPreservesExitCodeContract(t *testing.T) {
	view := &DriverView{
		Selected: []*model.Driver{
			{DriverCode: "d1", CompareStatus: model.StatusUpdate},
		},
	}
	// A GUI-code path that matches every requested code selects and returns 0.
	match := New(&bytes.Buffer{}, &bytes.Buffer{}, strings.NewReader(""))
	matchVC := &ViewContext{Opts: Options{GuiInstallCodes: "d1"}}
	selected, code := match.acquireSelection(context.Background(), matchVC, view, "42")
	if code != 0 || len(selected) != 1 || selected[0].DriverCode != "d1" {
		t.Fatalf("matching codes must select and continue: selected=%#v code=%d", selected, code)
	}
	// A GUI-code mismatch must abort with PS1 exit code 3, never 0 or 1.
	mismatch := New(&bytes.Buffer{}, &bytes.Buffer{}, strings.NewReader(""))
	mismatchVC := &ViewContext{Opts: Options{GuiInstallCodes: "missing"}}
	selected, code = mismatch.acquireSelection(context.Background(), mismatchVC, view, "42")
	if code != 3 || selected != nil {
		t.Fatalf("missing GUI codes must return code 3 with no selection: selected=%#v code=%d", selected, code)
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

func TestFormatTimestampIsRFC3339UTC(t *testing.T) {
	got := formatTimestamp(time.Date(2026, 8, 30, 12, 3, 4, 0, time.FixedZone("+8", 8*3600)))
	want := "2026-08-30T04:03:04Z"
	if got != want {
		t.Fatalf("formatTimestamp = %q, want %q (UTC RFC3339)", got, want)
	}
}

func TestOtherOSIDsExcludesCurrentAndPreservesOrder(t *testing.T) {
	osList := []model.OSListEntry{
		{OSID: "42", OSName: "W10"},
		{OSID: "248", OSName: "W11"},
		{OSID: "7", OSName: "W7"},
	}
	if got := otherOSIDs(osList, "248"); !reflect.DeepEqual(got, []string{"42", "7"}) {
		t.Fatalf("otherOSIDs('248') = %#v, want [42 7]", got)
	}
	if got := otherOSIDs(osList, "nope"); !reflect.DeepEqual(got, []string{"42", "248", "7"}) {
		t.Fatalf("otherOSIDs(unknown) = %#v, want all in order", got)
	}
	if got := otherOSIDs(nil, "42"); len(got) != 0 {
		t.Fatalf("otherOSIDs(nil) = %#v, want empty", got)
	}
}

func TestCachedDriverObjectsAvoidsDuplicateFetch(t *testing.T) {
	app := New(&bytes.Buffer{}, &bytes.Buffer{}, strings.NewReader(""))
	app.APIClient = nil
	app.osDriverCache = map[string]api.SourceDrivers{
		"248|QuickFix": {Source: "QuickFix", Drivers: []*model.Driver{{DriverCode: "d1"}}},
	}
	vc := &ViewContext{CategoryID: "cat"}
	result, err := app.cachedDriverObjects(context.Background(), vc, "248", "QuickFix")
	if err != nil {
		t.Fatal(err)
	}
	if result.Source != "QuickFix" || len(result.Drivers) != 1 {
		t.Fatalf("cached driver result mismatch: %#v", result)
	}
}

func TestCachedDriverObjectsDoesNotCacheFailure(t *testing.T) {
	client := &http.Client{Transport: appRoundTripFunc(func(req *http.Request) *http.Response {
		return &http.Response{StatusCode: http.StatusInternalServerError, Body: io.NopCloser(bytes.NewReader(nil))}
	})}
	app := New(&bytes.Buffer{}, &bytes.Buffer{}, strings.NewReader(""))
	app.APIClient = api.NewClient()
	app.APIClient.HTTP = client
	vc := &ViewContext{CategoryID: "cat"}
	_, err := app.cachedDriverObjects(context.Background(), vc, "248", "QuickFix")
	if err == nil {
		t.Fatal("expected API failure to propagate")
	}
	if len(app.osDriverCache) != 0 {
		t.Fatalf("failed API result was cached: %#v", app.osDriverCache)
	}
}

func TestBuildDriverSourceMapUsesSharedKeys(t *testing.T) {
	drivers := []*model.Driver{
		{DriverCode: "d1", DriverName: "Audio", Version: "1.0.0.1", OSID: "248", OSName: "Windows 11 64-bit"},
	}
	sourceMap := buildDriverSourceMap(drivers)
	if got := sourceMap["Audio|1.0.0.1"]; got.OSID != "248" {
		t.Fatalf("source map name key mismatch: %#v", got)
	}
	if got := sourceMap["d1|1.0.0.1"]; got.OSID != "248" {
		t.Fatalf("source map code key mismatch: %#v", got)
	}
}

func TestPnpIDsOfSkipsEmpty(t *testing.T) {
	devices := []model.Device{
		{Name: "a", PnpDeviceID: "VEN_0000&DEV_0001"},
		{Name: "b", PnpDeviceID: ""},
		{Name: "c", PnpDeviceID: "VEN_0002&DEV_0003"},
	}
	got := pnpIDsOf(devices)
	if len(got) != 2 || got[0] != "VEN_0000&DEV_0001" || got[1] != "VEN_0002&DEV_0003" {
		t.Fatalf("pnpIDsOf = %#v", got)
	}
}

func TestLoadDriverListToleratesEmpty(t *testing.T) {
	app := New(&bytes.Buffer{}, &bytes.Buffer{}, strings.NewReader(""))
	app.APIClient = nil
	app.osDriverCache = map[string]api.SourceDrivers{"248|QuickFix": {Source: "QuickFix"}}
	vc := &ViewContext{CategoryID: "cat"}
	result, ok := app.loadDriverList(context.Background(), vc, "248", "QuickFix")
	if ok || len(result.Drivers) != 0 {
		t.Fatalf("empty driver list should be tolerated as not-ok: %#v ok=%v", result, ok)
	}
}

func TestLoadDriverListReturnsCachedRows(t *testing.T) {
	app := New(&bytes.Buffer{}, &bytes.Buffer{}, strings.NewReader(""))
	app.APIClient = nil
	app.osDriverCache = map[string]api.SourceDrivers{
		"248|QuickFix": {Source: "QuickFix", Drivers: []*model.Driver{{DriverCode: "d1"}}},
	}
	vc := &ViewContext{CategoryID: "cat"}
	result, ok := app.loadDriverList(context.Background(), vc, "248", "QuickFix")
	if !ok || result.Source != "QuickFix" || len(result.Drivers) != 1 {
		t.Fatalf("loadDriverList = %#v ok=%v", result, ok)
	}
}

func TestMustDriverListHardErrorsOnEmpty(t *testing.T) {
	app := New(&bytes.Buffer{}, &bytes.Buffer{}, strings.NewReader(""))
	app.APIClient = nil
	app.osDriverCache = map[string]api.SourceDrivers{"248|QuickFix": {Source: "QuickFix"}}
	vc := &ViewContext{CategoryID: "cat"}
	_, err := app.mustDriverList(context.Background(), vc, "248", "QuickFix")
	if err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("mustDriverList empty list should hard-error: %v", err)
	}
}

func TestMustDriverListReturnsCachedRows(t *testing.T) {
	app := New(&bytes.Buffer{}, &bytes.Buffer{}, strings.NewReader(""))
	app.APIClient = nil
	app.osDriverCache = map[string]api.SourceDrivers{
		"248|QuickFix": {Source: "QuickFix", Drivers: []*model.Driver{{DriverCode: "d1"}}},
	}
	vc := &ViewContext{CategoryID: "cat"}
	result, err := app.mustDriverList(context.Background(), vc, "248", "QuickFix")
	if err != nil || result.Source != "QuickFix" || len(result.Drivers) != 1 {
		t.Fatalf("mustDriverList = %#v err=%v", result, err)
	}
}

func TestLoadDriverListsForOSIDsPreservesOrder(t *testing.T) {
	app := New(&bytes.Buffer{}, &bytes.Buffer{}, strings.NewReader(""))
	app.APIClient = nil
	app.osDriverCache = map[string]api.SourceDrivers{
		"42|QuickFix":  {Source: "QuickFix", Drivers: []*model.Driver{{DriverCode: "a"}}},
		"248|QuickFix": {Source: "QuickFix", Drivers: []*model.Driver{{DriverCode: "b"}}},
	}
	vc := &ViewContext{CategoryID: "cat"}
	loads := app.loadDriverListsForOSIDs(context.Background(), vc, []string{"248", "42"}, "QuickFix")
	if len(loads) != 2 || loads[0].osID != "248" || loads[1].osID != "42" {
		t.Fatalf("input order not preserved: %#v", loads)
	}
	if loads[0].err != nil || loads[1].err != nil {
		t.Fatalf("cached loads should have no error: %#v", loads)
	}
}

func TestToleratedDriverListMissPolicy(t *testing.T) {
	if toleratedDriverListMiss(nil, api.SourceDrivers{Drivers: []*model.Driver{{DriverCode: "d1"}}}) != nil {
		t.Fatal("a usable list should not count as a miss")
	}
	if toleratedDriverListMiss(nil, api.SourceDrivers{}) == nil {
		t.Fatal("an empty list should count as a miss")
	}
	if toleratedDriverListMiss(errors.New("boom"), api.SourceDrivers{Drivers: []*model.Driver{{DriverCode: "d1"}}}) == nil {
		t.Fatal("a fetch error should count as a miss")
	}
}

func TestCloneDriversIsOwnedCopy(t *testing.T) {
	audit := &model.SourceAudit{Category: model.AuditCategoryOnlinePackage, EvidenceLines: []string{"a", "b"}}
	source := []*model.Driver{
		{DriverCode: "d1", DriverName: "Audio", Version: "1.0.0.0", SourceAudit: audit},
	}
	clones := cloneDrivers(source)
	if len(clones) != 1 || clones[0] == source[0] {
		t.Fatalf("expected a distinct owned copy")
	}
	// Mutating an owned view copy must never leak into the shared/cached row.
	clones[0].DriverName = "Audio-X"
	clones[0].LocalVersion = "2.0.0.0"
	clones[0].CompareStatus = model.StatusUpdate
	clones[0].CompareSource = "source"
	clones[0].SourceAudit.Summary = "changed"
	if source[0].DriverName != "Audio" || source[0].LocalVersion != "" ||
		source[0].CompareStatus != "" || source[0].CompareSource != "" {
		t.Fatalf("clone mutation leaked into the shared driver row: %#v", source[0])
	}
	if source[0].SourceAudit.Summary != "" {
		t.Fatalf("nested source audit was not deep-copied: %#v", source[0].SourceAudit)
	}
}

// TestProjectMatchedEvidencePreservesPerDriverSemantics locks the batch
// evidence projection added by the single-pass compare-flow dedup: each driver's
// source audit must see the same enriched rows a per-driver GetDeviceEvidence
// call returned, and degrade to the plain matched rows when the batch pass
// failed.
func TestProjectMatchedEvidencePreservesPerDriverSemantics(t *testing.T) {
	matched := []model.Device{
		{PnpDeviceID: "A", Class: "class-a"},
		{PnpDeviceID: "B", Class: "class-b"},
	}
	// A failed (nil) evidence pass must fall back to the raw matched rows.
	got := projectMatchedEvidence(matched, nil)
	if len(got) != 2 || got[0].PnpDeviceID != "A" || got[1].PnpDeviceID != "B" {
		t.Fatalf("nil evidence map must return the matched rows unchanged: %#v", got)
	}
	// A successful pass projects only the matched rows present in the map, in
	// matched order, carrying the enriched values.
	byID := map[string]model.Device{
		"A": {PnpDeviceID: "A", Class: "class-a-enriched", ImportSource: "setupapi"},
		"X": {PnpDeviceID: "X", Class: "unrelated"},
	}
	want := []model.Device{{PnpDeviceID: "A", Class: "class-a-enriched", ImportSource: "setupapi"}}
	if got := projectMatchedEvidence(matched, byID); !reflect.DeepEqual(got, want) {
		t.Fatalf("projection must pick enriched in-map rows in matched order: got %#v want %#v", got, want)
	}
}
