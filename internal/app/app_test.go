package app

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/json"
	"errors"
	"fmt"
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
	"lenovo-driver/internal/ledger"
	"lenovo-driver/internal/model"
)

func TestParseOptionsHelp(t *testing.T) {
	app := newTestApp(t)
	code := app.Run([]string{"-Help"})
	if code != 0 {
		t.Fatalf("Help exit code = %d", code)
	}
}

func TestInvalidFlagCombination(t *testing.T) {
	app := newTestApp(t)
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
						"FilePath":   "https://newdriverdl.lenovo.com.cn/new.exe",
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

	app := newTestApp(t)
	app.APIClient = api.NewClient()
	app.APIClient.HTTP = client
	app.Downloader = download.NewDownloader()
	app.Downloader.Client = client
	app.driverListCacheDir = t.TempDir()
	downloadDir := t.TempDir()
	outFile := filepath.Join(downloadDir, "d1_driver.exe")
	driver := &model.Driver{DriverCode: "d1", FileName: "driver.exe", FilePath: "https://newdriverdl.lenovo.com.cn/old.exe", FileSize: "12 B"}
	ad := &model.AssessedDriver{Driver: driver}
	vc := &ViewContext{
		Opts:       Options{SkipSignatureCheck: true},
		CategoryID: "cat",
		OsList:     []model.OSListEntry{{OSID: "248"}},
	}
	_, err := app.downloadVerified(context.Background(), vc, ad, outFile, 12, "248", "QuickFix")
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
	if driver.FilePath != "https://newdriverdl.lenovo.com.cn/new.exe" {
		t.Fatalf("driver URL was not refreshed: %q", driver.FilePath)
	}
}

func TestShowActionPreviewIncludesDriverRows(t *testing.T) {
	var out bytes.Buffer
	ad := &model.AssessedDriver{
		Driver:           &model.Driver{DriverName: "Audio", Version: "1.0.0.1"},
		DriverAssessment: model.DriverAssessment{LocalVersion: "1.0.0.0", CompareStatus: "Update"},
	}
	showActionPreview(&out, 'y', "update-only", []*model.AssessedDriver{ad})
	text := out.String()
	if !strings.Contains(text, "Audio") || !strings.Contains(text, "1.0.0.1") || !strings.Contains(text, "Update") {
		t.Fatalf("preview did not include exact driver rows:\n%s", text)
	}
}

func TestReadHistoryStripsUTF8BOM(t *testing.T) {
	app := newTestApp(t)
	app.HistoryPath = filepath.Join(t.TempDir(), "history.csv")
	rec := model.HistoryRecord{
		Timestamp: "2026-01-01 00:00:00", DriverCode: "d1", OSID: "42", OSName: "Windows 10 64-bit",
		DriverName: "Audio", Version: "1.0.0.1", FileName: "audio.exe", MD5: "abc",
		Source: "QuickFix", Result: "Installed", Message: "exit=0",
	}
	content := "\uFEFF" + strings.Join(ledger.Columns, ",") + "\n" + strings.Join(ledger.Values(rec), ",") + "\n"
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
	usable, size, reason := inspectCachedFile(path, int64(len(content)), "", true, true)
	if !usable || size != int64(len(content)) || reason != "" {
		t.Fatalf("cached file should be usable: usable=%v size=%d reason=%q", usable, size, reason)
	}
	usable, _, reason = inspectCachedFile(path, int64(len(content)+2000), "", true, true)
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
	size, err := verifyDownloadedFile(path, int64(len(content)), "", true, true, false)
	if err != nil || size != int64(len(content)) {
		t.Fatalf("verifyDownloadedFile = %d, err=%v", size, err)
	}
}

func TestQuoteWindowsArgument(t *testing.T) {
	// The golden vectors live in testdata/windows_argument_quoting.json and are
	// shared with scripts/verify.ps1, which asserts the WPF PowerShell quoting
	// implementation against the same file. Changing quoting behavior requires
	// updating both implementations and this single source of truth.
	data, err := os.ReadFile("testdata/windows_argument_quoting.json")
	if err != nil {
		t.Fatal(err)
	}
	var golden struct {
		Cases []struct {
			Name  string `json:"name"`
			Input string `json:"input"`
			Token string `json:"token"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(data, &golden); err != nil {
		t.Fatal(err)
	}
	if len(golden.Cases) < 10 {
		t.Fatalf("shared quoting golden has too few cases: %d", len(golden.Cases))
	}
	for _, tc := range golden.Cases {
		if got := quoteWindowsArgument(tc.Input); got != tc.Token {
			t.Fatalf("quoteWindowsArgument(%q) = %q, want %q", tc.Input, got, tc.Token)
		}
	}
}

func TestSelectByCodes(t *testing.T) {
	drivers := []*model.AssessedDriver{
		{Driver: &model.Driver{DriverCode: "d1"}},
		{Driver: &model.Driver{DriverCode: "d2"}},
	}
	selection := selectByCodes(drivers, "d2")
	if len(selection.Selected) != 1 || selection.Selected[0].DriverCode != "d2" {
		t.Fatalf("unexpected GUI selection: %#v", selection)
	}
}

func TestSelectByCodesRejectsPartialAndNotApplicable(t *testing.T) {
	drivers := []*model.AssessedDriver{
		{Driver: &model.Driver{DriverCode: "d1"}, DriverAssessment: model.DriverAssessment{CompareStatus: model.StatusUpdate}},
		{Driver: &model.Driver{DriverCode: "d2"}, DriverAssessment: model.DriverAssessment{CompareStatus: model.StatusNotApplicable}},
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
		Selected: []*model.AssessedDriver{
			{Driver: &model.Driver{DriverCode: "d1"}, DriverAssessment: model.DriverAssessment{CompareStatus: model.StatusUpdate}},
		},
	}
	// A GUI-code path that matches every requested code selects and returns 0.
	match := newTestApp(t)
	matchVC := &ViewContext{Opts: Options{GuiInstallCodes: "d1"}}
	selected, code := match.acquireSelection(context.Background(), matchVC, view, "42")
	if code != 0 || len(selected) != 1 || selected[0].DriverCode != "d1" {
		t.Fatalf("matching codes must select and continue: selected=%#v code=%d", selected, code)
	}
	// A GUI-code mismatch must abort with PS1 exit code 3, never 0 or 1.
	mismatch := newTestApp(t)
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
	app := newTestApp(t)
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
	app := newTestApp(t)
	app.APIClient = api.NewClient()
	app.APIClient.HTTP = client
	app.driverListCacheDir = t.TempDir()
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
	app := newTestApp(t)
	app.APIClient = nil
	app.osDriverCache = map[string]api.SourceDrivers{"248|QuickFix": {Source: "QuickFix"}}
	vc := &ViewContext{CategoryID: "cat"}
	result, ok := app.loadDriverList(context.Background(), vc, "248", "QuickFix")
	if ok || len(result.Drivers) != 0 {
		t.Fatalf("empty driver list should be tolerated as not-ok: %#v ok=%v", result, ok)
	}
}

func TestLoadDriverListReturnsCachedRows(t *testing.T) {
	app := newTestApp(t)
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
	app := newTestApp(t)
	app.APIClient = nil
	app.osDriverCache = map[string]api.SourceDrivers{"248|QuickFix": {Source: "QuickFix"}}
	vc := &ViewContext{CategoryID: "cat"}
	_, err := app.mustDriverList(context.Background(), vc, "248", "QuickFix")
	if err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("mustDriverList empty list should hard-error: %v", err)
	}
}

func TestMustDriverListReturnsCachedRows(t *testing.T) {
	app := newTestApp(t)
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
	app := newTestApp(t)
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

func TestAssessedDriverOwnership(t *testing.T) {
	// The AssessedDriver pairs a shared *Driver transport row with an owned
	// DriverAssessment copy. Mutating the assessment on the view copy must
	// never leak into the API/cache row, and the Driver pointer is shared
	// (non-assessment fields are immutable after construction).
	audit := &model.SourceAudit{Category: model.AuditCategoryExternal, EvidenceLines: []string{"a", "b"}}
	orig := &model.Driver{DriverCode: "d1", DriverName: "Audio", Version: "1.0.0.0"}
	ad := &model.AssessedDriver{
		Driver: orig,
		DriverAssessment: model.DriverAssessment{
			LocalVersion:  "2.0.0.0",
			CompareStatus: model.StatusUpdate,
			CompareSource: "source",
			SourceAudit:   audit,
		},
	}
	// Mutate the assessment on the view copy.
	ad.LocalVersion = "3.0.0.0"
	ad.CompareStatus = model.StatusUpToDate
	ad.CompareSource = "new-source"
	ad.SourceAudit.Summary = "changed"
	// The transport Driver must be unchanged.
	if orig.DriverName != "Audio" || orig.Version != "1.0.0.0" {
		t.Fatalf("transport Driver was mutated through AssessedDriver: %#v", orig)
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

func TestInspectCachedFileRejectsInvalidSignature(t *testing.T) {
	original := verifyFileSignature
	defer func() { verifyFileSignature = original }()
	verifyFileSignature = func(string) error { return errors.New("unsigned") }

	dir := t.TempDir()
	path := filepath.Join(dir, "driver.exe")
	if err := os.WriteFile(path, []byte("bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	usable, _, reason := inspectCachedFile(path, 5, "", true, false)
	if usable || reason != "Cached file signature invalid" {
		t.Fatalf("cached file with invalid signature should reject: usable=%v reason=%q", usable, reason)
	}
}

func TestVerifyDownloadedFileRejectsInvalidSignature(t *testing.T) {
	original := verifyFileSignature
	defer func() { verifyFileSignature = original }()
	verifyFileSignature = func(string) error { return errors.New("unsigned") }

	dir := t.TempDir()
	path := filepath.Join(dir, "driver.exe")
	if err := os.WriteFile(path, []byte("bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := verifyDownloadedFile(path, 5, "", true, false, false); err == nil || !strings.Contains(err.Error(), "signature invalid") {
		t.Fatalf("verifyDownloadedFile should reject invalid signature, got %v", err)
	}
}

func TestFilterDriverRowsFirmwareWhitelist(t *testing.T) {
	rows := []*model.Driver{
		{DriverName: "BIOS Update", FileName: "bios.exe"},
		{DriverName: "Intel Management Engine Firmware", FileName: "me.exe"},
		{DriverName: "Thunderbolt Firmware", FileName: "tb.exe"},
		{DriverName: "TPM Firmware", FileName: "tpm.exe"},
		{DriverName: "EC Version", FileName: "ec.exe"},
		{DriverName: "Realtek Audio", FileName: "audio.exe"},
	}
	got := filterDriverRows(rows, false)
	if len(got) != 1 || got[0].DriverName != "Realtek Audio" {
		t.Fatalf("firmware whitelist should skip all firmware rows, got %#v", got)
	}
	if all := filterDriverRows(rows, true); len(all) != len(rows) {
		t.Fatalf("IncludeBios should keep firmware rows: got %d, want %d", len(all), len(rows))
	}
}

// A row the filter drops must not vanish: between the API response and the
// plan there used to be a counted but unreported gap, so a row the vendor did
// send was indistinguishable from a row that never existed.
func TestFilterDriverRowsWithSkipsReportsReasons(t *testing.T) {
	rows := []*model.Driver{
		{DriverCode: "DRV_A", DriverName: "BIOS Update", FileName: "bios.exe"},
		{DriverCode: "DRV_B", DriverName: "Touchpad readme", FileName: "TouchPadReadme.txt"},
		{DriverCode: "DRV_C", DriverName: "Realtek Audio", FileName: "audio.exe"},
	}
	out, skipped := filterDriverRowsWithSkips(rows, false)
	if len(out) != 1 || len(skipped) != 2 {
		t.Fatalf("filter kept %d and skipped %d, want 1/2", len(out), len(skipped))
	}
	reasons := map[string]string{}
	for _, s := range skipped {
		reasons[s.driver.DriverCode] = s.reason
	}
	if !strings.Contains(reasons["DRV_A"], "firmware") {
		t.Fatalf("firmware skip reason missing: %#v", reasons)
	}
	if !strings.Contains(reasons["DRV_B"], "file type .txt") {
		t.Fatalf("file-type skip reason missing: %#v", reasons)
	}
	if includeBiosRows, _ := filterDriverRowsWithSkips(rows, true); len(includeBiosRows) == len(out) {
		t.Fatalf("IncludeBios should keep firmware rows")
	}
}

// The interactive hint names a control only the selection menu has. Export,
// dry-run, and GUI-code runs never show that menu, so the hint must stay
// silent there (the WPF switches OS with a dropdown, not the t key).
func TestResolveListOSHidesInteractiveHintOutsideInteractiveRuns(t *testing.T) {
	cases := []struct {
		name     string
		apply    func(*Options)
		wantHint bool
	}{
		{"interactive default", func(o *Options) {}, true},
		{"export run", func(o *Options) { o.GuiExportPath = "unused.json" }, false},
		{"dry run", func(o *Options) { o.DryRun = true }, false},
		{"gui install codes", func(o *Options) { o.GuiInstallCodes = "1,2" }, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			app := newTestApp(t)
			app.Stdout = &out
			opts := &Options{}
			tc.apply(opts)
			vc := &ViewContext{
				CurrentSystemOsID: "42",
				OsList: []model.OSListEntry{
					{OSID: "42", OSName: "Windows 10 64-bit"},
					{OSID: "248", OSName: "Windows 11 64-bit"},
				},
			}
			osID, code := app.resolveListOS(context.Background(), vc, opts)
			if code != 0 || osID != "42" {
				t.Fatalf("resolveListOS = (%q, %d), want (42, 0)", osID, code)
			}
			if got := strings.Contains(out.String(), "press t"); got != tc.wantHint {
				t.Fatalf("interactive hint present = %v, want %v:\n%s", got, tc.wantHint, out.String())
			}
		})
	}
}

func TestParseOptionsSignatureFlag(t *testing.T) {
	opts, err := parseOptions([]string{"-SkipSignatureCheck"})
	if err != nil || !opts.SkipSignatureCheck {
		t.Fatalf("SkipSignatureCheck should parse: opts=%#v err=%v", opts, err)
	}
}

func TestInstallBindingLabel(t *testing.T) {
	cases := []struct {
		name, before, afterRaw, pkg, vendor, want string
	}{
		{"bound", "1.0", "2.0", "2.0", "", "bound"},
		{"already bound", "2.0", "2.0", "2.0", "", "unchanged-same"},
		{"staged", "1.0", "1.5", "2.0", "", "staged"},
		{"unchanged", "1.0", "1.0", "2.0", "", "unchanged"},
		{"undetected", "1.0", "", "2.0", "", "undetected"},
		// The live case: the vendor row names three packages in one Version
		// field. Raw string equality could never match a local version against
		// it, so this driver could never be reported as bound at all.
		{
			"composite package resolves by vendor",
			"6001.0.10.336",
			"6001.0.10.336",
			"Intel_22.10.0.7/Realtek8852AE_6001.0.10.336/Mediatek_3.0.1.1314",
			"Realtek",
			"unchanged-same",
		},
		{
			"composite package installs the vendor's component",
			"22.10.0.7",
			"6001.0.10.340",
			"Intel_22.10.0.7/Realtek8852AE_6001.0.10.336/Mediatek_3.0.1.1314",
			"Realtek",
			"staged",
		},
	}
	for _, tc := range cases {
		after := model.ObservedAbsence()
		if tc.afterRaw != "" {
			after = model.ObservedVersion(tc.afterRaw)
		}
		want := verdictNamed(t, tc.want)
		if got := installBindingVerdict(tc.before, after, tc.pkg, tc.vendor); got != want {
			t.Fatalf("%s: installBindingVerdict(%q, %v, %q) = %v, want %v", tc.name, tc.before, after, tc.pkg, got, want)
		}
	}
}

// TestInstallBindingLabelHasNoVerdictWithoutComparableVersions pins the defect
// the 82JQ drill exposed. A package Windows has provisioned but not installed
// reports "Provisioned" before and after regardless of what the installer did,
// so before==after carried no information and the run was counted as a success.
func TestInstallBindingLabelHasNoVerdictWithoutComparableVersions(t *testing.T) {
	provisioned := model.ObservedProvisioning()
	if got := installBindingVerdict(model.LocalVersionProvisioned, provisioned,
		"6.0.0.9", ""); got != bindingUndetected {
		t.Fatalf("provisioned recheck = %v, want undetected", got)
	}
	if model.IsMeasuredLocalVersion(model.LocalVersionProvisioned) {
		t.Fatal("the provisioning placeholder must not count as a measured version")
	}
}

// TestInstallBindingReadsRealSoftwareVersions is the counterpart: a
// software-versioned driver that installed correctly reads its real version back
// from the installed-app list, and that version must carry the verdict. Keying
// comparability on the driver family instead of the value reported every one of
// them as unverified.
func TestInstallBindingReadsRealSoftwareVersions(t *testing.T) {
	for _, tc := range []struct {
		name, before, afterRaw, pkg, vendor, want string
	}{
		{"installed the listed version", "15.11.29.13", "15.11.29.65", "15.11.29.65", "", "bound"},
		{"already on the same version", "2.0.0.25", "2.0.0.25", "2.0.0.25", "", "unchanged-same"},
		{"moved but short of the package", "1.0.2.0", "2.0.0.20", "2.0.0.25", "", "staged"},
		{"first time seen", "", "6.0.0.9", "6.0.0.9", "", "bound"},
		// The vendor writes "15.11.29.13 MS signed"; the trailing words are not
		// part of the number and must not defeat the comparison.
		{"suffixed vendor string", "15.11.29.13", "15.11.29.13", "15.11.29.13 MS signed", "", "unchanged-same"},
	} {
		after := model.ObservedAbsence()
		if tc.afterRaw != "" {
			after = model.ObservedVersion(tc.afterRaw)
		}
		want := verdictNamed(t, tc.want)
		if got := installBindingVerdict(tc.before, after, tc.pkg, tc.vendor); got != want {
			t.Errorf("%s: installBindingVerdict(%q, %v, %q) = %v, want %v", tc.name, tc.before, after, tc.pkg, got, want)
		}
	}
	// A genuinely moved kernel-driver version is still judged on its own evidence.
	if got := installBindingVerdict("1.0", model.ObservedVersion("2.0"), "2.0", ""); got != bindingBound {
		t.Fatalf("kernel-driver recheck = %v, want bound", got)
	}
}

// TestOnlyConfirmedBindingsCountAsSuccess is the gate between the recheck and the
// run summary. "unchanged" and "undetected" must not be reported as installed.
func TestOnlyConfirmedBindingsCountAsSuccess(t *testing.T) {
	for _, tc := range []struct {
		verdict bindingVerdict
		want    bool
	}{
		{bindingBound, true},
		{bindingStaged, true},
		{bindingUnchangedSame, true},
		{bindingUnchanged, false},
		{bindingUndetected, false},
		// The zero value is Undetected, so an uninitialized verdict is never a
		// success. This case is the reason the type exists.
		{bindingVerdict(0), false},
	} {
		if got := tc.verdict.confirmsEffect(); got != tc.want {
			t.Errorf("bindingVerdict(%d).confirmsEffect() = %v, want %v", tc.verdict, got, tc.want)
		}
	}
}

// verdictNamed maps the table's readable spelling to the typed verdict, so the
// cases stay legible while the assertion compares typed values.
func verdictNamed(t *testing.T, name string) bindingVerdict {
	t.Helper()
	switch name {
	case "bound":
		return bindingBound
	case "staged":
		return bindingStaged
	case "unchanged-same":
		return bindingUnchangedSame
	case "unchanged":
		return bindingUnchanged
	case "undetected":
		return bindingUndetected
	}
	t.Fatalf("unknown verdict name %q", name)
	return bindingUndetected
}

func TestClassifyInstallResult(t *testing.T) {
	cases := []struct {
		name string
		code int
		err  error
		want installOutcome
	}{
		{"reboot 3010", 3010, nil, outcomeRebootRequired},
		{"reboot 1641", 1641, nil, outcomeRebootRequired},
		{"terminal error", -2, errors.New("boom"), outcomeFailed},
		{"success", 0, nil, outcomeSuccess},
		{"non-exe fail", 1603, nil, outcomeFailed},
	}
	for _, tc := range cases {
		if got := classifyInstallResult(tc.code, tc.err); got != tc.want {
			t.Fatalf("%s: classifyInstallResult = %d, want %d", tc.name, got, tc.want)
		}
	}
}

func TestManualRollbackGuidance(t *testing.T) {
	guidance := manualRollbackGuidance("oem57.inf")
	if !strings.Contains(guidance, "Roll Back Driver") {
		t.Fatalf("guidance should prescribe device-level rollback: %q", guidance)
	}
	if !strings.Contains(guidance, "do not run pnputil /delete-driver") {
		t.Fatalf("guidance should forbid package deletion as rollback: %q", guidance)
	}
	if !strings.Contains(guidance, "oem57.inf") {
		t.Fatalf("guidance should name the previous INF: %q", guidance)
	}
	if got := manualRollbackGuidance(""); !strings.Contains(got, "unknown") {
		t.Fatalf("guidance should mark unknown previous INF: %q", got)
	}
}

// TestDownloadWithoutOfficialMD5IsAnnounced: an empty OfficialMD5 silently skips
// the vendor-hash comparison, which is right for the webpage source (it never
// sends one) but must not read as a verified download. The run log has to say
// which evidence is left.
func TestDownloadWithoutOfficialMD5IsAnnounced(t *testing.T) {
	content := []byte("driver-bytes")
	client := &http.Client{Transport: appRoundTripFunc(func(req *http.Request) *http.Response {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(content))}
	})}
	app := newTestApp(t)
	app.Downloader = download.NewDownloader()
	app.Downloader.Client = client
	outFile := filepath.Join(t.TempDir(), "d1_driver.exe")
	driver := &model.Driver{
		DriverCode:  "d1",
		FileName:    "driver.exe",
		FilePath:    "https://newdriverdl.lenovo.com.cn/driver.exe",
		FileSize:    "12 B",
		SourceAPI:   "Web",
		OfficialMD5: "",
	}
	vc := &ViewContext{Opts: Options{SkipSignatureCheck: true}}
	if _, err := app.downloadVerified(context.Background(), vc, &model.AssessedDriver{Driver: driver},
		outFile, 12, "42", "Web"); err != nil {
		t.Fatal(err)
	}
	logged := app.Stdout.(*bytes.Buffer).String()
	if !strings.Contains(logged, "No official MD5") || !strings.Contains(logged, "Web") {
		t.Fatalf("a download with no official hash must say so, log was:\n%s", logged)
	}
}

// TestDownloadWithOfficialMD5SaysNothingExtra keeps the warning meaningful: a
// QuickFix row carries an MD5, so the notice must not fire on the normal path.
func TestDownloadWithOfficialMD5SaysNothingExtra(t *testing.T) {
	content := []byte("driver-bytes")
	client := &http.Client{Transport: appRoundTripFunc(func(req *http.Request) *http.Response {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(content))}
	})}
	app := newTestApp(t)
	app.Downloader = download.NewDownloader()
	app.Downloader.Client = client
	outFile := filepath.Join(t.TempDir(), "d1_driver.exe")
	driver := &model.Driver{
		DriverCode:  "d1",
		FileName:    "driver.exe",
		FilePath:    "https://newdriverdl.lenovo.com.cn/driver.exe",
		FileSize:    "12 B",
		SourceAPI:   "QuickFix",
		OfficialMD5: fmt.Sprintf("%x", md5.Sum(content)),
	}
	vc := &ViewContext{Opts: Options{SkipSignatureCheck: true}}
	if _, err := app.downloadVerified(context.Background(), vc, &model.AssessedDriver{Driver: driver},
		outFile, 12, "42", "QuickFix"); err != nil {
		t.Fatal(err)
	}
	if logged := app.Stdout.(*bytes.Buffer).String(); strings.Contains(logged, "No official MD5") {
		t.Fatalf("the notice fired for a row that carries an MD5:\n%s", logged)
	}
}

// TestOSListSourceWarningNamesTheCause: the old line said "webpage OS list
// unavailable" for every fallback, including a decode failure that had nothing
// to do with availability. A parse error reported as a network condition is a
// false diagnosis in the one artifact the operator reads afterwards.
func TestOSListSourceWarningNamesTheCause(t *testing.T) {
	var out bytes.Buffer
	app := newTestApp(t)
	app.Stdout = &out
	res := &api.OSResolution{
		OSEntry:    model.OSListEntry{OSID: "42", OSName: "Windows 10 64-bit"},
		Source:     "QuickFix",
		WebFailure: "Web interface may be gated or blocked: OS list response is not the expected JSON",
	}
	app.logOSResolution(context.Background(), res)
	logged := out.String()
	if !strings.Contains(logged, "QuickFix") || !strings.Contains(logged, "JSON") {
		t.Fatalf("the warning must name the source and the real cause:\n%s", logged)
	}
	if strings.Contains(logged, "unavailable") {
		t.Fatalf("the warning still blames availability:\n%s", logged)
	}
}

// A clean webpage resolution must not warn at all.
func TestOSListSourceWarningSilentOnWeb(t *testing.T) {
	var out bytes.Buffer
	app := newTestApp(t)
	app.Stdout = &out
	res := &api.OSResolution{
		OSEntry: model.OSListEntry{OSID: "42", OSName: "Windows 10 64-bit"},
		Source:  "Web",
	}
	app.logOSResolution(context.Background(), res)
	if strings.Contains(out.String(), "WARN") {
		t.Fatalf("a clean webpage resolution warned:\n%s", out.String())
	}
}

// TestLedgerVersionColumnsCarryOnlyMeasuredVersions pins the boundary between
// prose and a version column. LocalEvidence.Display renders human text
// ("not detected", "a, b"); writing it into VerifiedVersion/BeforeVersion would
// put a sentence where every later join expects a number, and a conflict would
// read as a composite version string. The ledger columns take the measured
// version or nothing, and the display form belongs in Message.
func TestLedgerVersionColumnsCarryOnlyMeasuredVersions(t *testing.T) {
	for _, tc := range []struct {
		name     string
		evidence model.LocalEvidence
		want     string
	}{
		{"absent", model.ObservedAbsence(), ""},
		{"unobserved", model.NotObserved(), ""},
		{"provisioned", model.ObservedProvisioning(), ""},
		{"conflicted", model.ObservedConflict([]string{"1.0", "2.0"}), ""},
		{"measured", model.ObservedVersion("6.0.1.8000"), "6.0.1.8000"},
	} {
		measured := tc.evidence.MeasuredVersion()
		if measured != tc.want {
			t.Fatalf("%s: MeasuredVersion = %q, want %q", tc.name, measured, tc.want)
		}
		// The display form must never equal the ledger form for a non-measured
		// state, or the two could be confused at a call site.
		if tc.want == "" && tc.evidence.Display() == "" {
			t.Fatalf("%s: display is empty, so prose and absence are indistinguishable", tc.name)
		}
	}
}
