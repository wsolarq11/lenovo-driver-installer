package app

import (
	"context"
	"encoding/csv"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"lenovo-driver/internal/model"
)

// TestHistoryColumnsGolden locks the ledger column order against a literal.
// Reordering, adding, or removing a column must be an explicit, reviewed edit
// to this golden, not a silent drift between the header and the row writer.
func TestHistoryColumnsGolden(t *testing.T) {
	want := []string{
		"Timestamp", "DriverCode", "OSID", "OSName", "DriverName", "Version",
		"VerifiedVersion", "BeforeVersion", "FileName", "MD5", "Source", "Result", "Message",
		"Devices",
	}
	if !reflect.DeepEqual(historyColumns, want) {
		t.Fatalf("historyColumns drift: got %#v, want %#v", historyColumns, want)
	}
}

func TestHistoryColumnsUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, col := range historyColumns {
		if seen[col] {
			t.Fatalf("duplicate history column %q", col)
		}
		seen[col] = true
	}
}

// TestHistoryValuesAlignsWithColumns proves the value writer stays field-for-
// field aligned with the column order. It round-trips a fully populated record
// through the column names, so both a wrong order and a wrong field land in the
// wrong slot and fail the assertion.
func TestHistoryValuesAlignsWithColumns(t *testing.T) {
	rec := model.HistoryRecord{
		Timestamp:       "ts",
		DriverCode:      "dc",
		OSID:            "os",
		OSName:          "osn",
		DriverName:      "dn",
		Version:         "v",
		VerifiedVersion: "vv",
		BeforeVersion:   "bv",
		FileName:        "fn",
		MD5:             "md5",
		Source:          "src",
		Result:          "res",
		Message:         "msg",
		Devices:         []string{"dev-a", "dev-b"},
	}
	values := historyValues(rec)
	if len(values) != len(historyColumns) {
		t.Fatalf("historyValues has %d fields, historyColumns has %d", len(values), len(historyColumns))
	}
	var rebuilt model.HistoryRecord
	for i, col := range historyColumns {
		switch col {
		case "Timestamp":
			rebuilt.Timestamp = values[i]
		case "DriverCode":
			rebuilt.DriverCode = values[i]
		case "OSID":
			rebuilt.OSID = values[i]
		case "OSName":
			rebuilt.OSName = values[i]
		case "DriverName":
			rebuilt.DriverName = values[i]
		case "Version":
			rebuilt.Version = values[i]
		case "VerifiedVersion":
			rebuilt.VerifiedVersion = values[i]
		case "BeforeVersion":
			rebuilt.BeforeVersion = values[i]
		case "FileName":
			rebuilt.FileName = values[i]
		case "MD5":
			rebuilt.MD5 = values[i]
		case "Source":
			rebuilt.Source = values[i]
		case "Result":
			rebuilt.Result = values[i]
		case "Message":
			rebuilt.Message = values[i]
		case "Devices":
			rebuilt.Devices = splitDevices(values[i])
		default:
			t.Fatalf("unknown history column %q", col)
		}
	}
	if !reflect.DeepEqual(rebuilt, rec) {
		t.Fatalf("historyValues misaligned with historyColumns: got %#v, want %#v", rebuilt, rec)
	}
}

// TestWriteHistoryRecordHeaderUsesColumns proves a freshly created ledger writes
// the single-source header first and the single-source row values second, so the
// header and the first data row can never disagree about column order.
func TestWriteHistoryRecordHeaderUsesColumns(t *testing.T) {
	app := newTestApp(t)
	app.HistoryPath = filepath.Join(t.TempDir(), "history.csv")
	driver := &model.AssessedDriver{Driver: &model.Driver{DriverCode: "d1", DriverName: "Audio"}}
	if err := app.WriteHistoryRecord(driver, "Installed", "exit=0", "", ""); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(app.HistoryPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	rows, err := csv.NewReader(f).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected header + 1 row, got %d rows", len(rows))
	}
	if !reflect.DeepEqual(rows[0], historyHeader()) {
		t.Fatalf("ledger header = %#v, want %#v", rows[0], historyHeader())
	}
	if rows[1][1] != "d1" || rows[1][11] != "Installed" {
		t.Fatalf("data row landed in wrong columns: %#v", rows[1])
	}
	if len(rows[1]) != len(historyColumns)+1 || len(rows[1][len(historyColumns)]) != 64 {
		t.Fatalf("data row must carry a 64-char hash: %#v", rows[1])
	}
}

// TestHistoryChainIntact writes two rows and verifies the chained hash is
// recomputable from the on-disk bytes.
func TestHistoryChainIntact(t *testing.T) {
	app := newTestApp(t)
	app.HistoryPath = filepath.Join(t.TempDir(), "history.csv")
	driver := &model.AssessedDriver{Driver: &model.Driver{DriverCode: "d1"}}
	if err := app.WriteHistoryRecord(driver, "Installed", "exit=0", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := app.WriteHistoryRecord(driver, "Verified", "binding=bound", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := verifyHistoryChain(app.HistoryPath); err != nil {
		t.Fatalf("intact chain should verify: %v", err)
	}
}

// TestHistoryChainDetectsTamper proves a single rewritten data field breaks the
// chain, i.e. the ledger is tamper-evident rather than merely append-only.
func TestHistoryChainDetectsTamper(t *testing.T) {
	app := newTestApp(t)
	app.HistoryPath = filepath.Join(t.TempDir(), "history.csv")
	driver := &model.AssessedDriver{Driver: &model.Driver{DriverCode: "d1"}}
	if err := app.WriteHistoryRecord(driver, "Installed", "exit=0", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := app.WriteHistoryRecord(driver, "Verified", "binding=bound", "", ""); err != nil {
		t.Fatal(err)
	}
	rewriteCSV(t, app.HistoryPath, 1, 1, "dX")
	if err := verifyHistoryChain(app.HistoryPath); err == nil {
		t.Fatal("tampered chain must fail verification")
	}
}

// rewriteCSV rewrites one cell of an existing CSV file in place, simulating an
// external edit or corruption.
func rewriteCSV(t *testing.T, path string, rowIdx, colIdx int, value string) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(f).ReadAll()
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	rows[rowIdx][colIdx] = value
	out, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	if err := csv.NewWriter(out).WriteAll(rows); err != nil {
		t.Fatal(err)
	}
}

// TestHistoryRoundTrip writes a record and reads it back through ReadHistory,
// confirming the single-source writer and the tolerant reader agree end to end.
func TestHistoryRoundTrip(t *testing.T) {
	app := newTestApp(t)
	app.HistoryPath = filepath.Join(t.TempDir(), "history.csv")
	driver := &model.AssessedDriver{Driver: &model.Driver{
		DriverCode:  "d1",
		OSID:        "42",
		OSName:      "Windows 10 64-bit",
		DriverName:  "Audio",
		Version:     "1.0.0.1",
		FileName:    "audio.exe",
		OfficialMD5: "abc",
		SourceAPI:   "QuickFix",
	}}
	if err := app.WriteHistoryRecord(driver, "Installed", "exit=0", "1.0.0.1", "1.0.0.0"); err != nil {
		t.Fatal(err)
	}
	records := app.ReadHistory(context.Background())
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	r := records[0]
	if r.DriverCode != "d1" || r.OSID != "42" || r.OSName != "Windows 10 64-bit" ||
		r.DriverName != "Audio" || r.Version != "1.0.0.1" || r.VerifiedVersion != "1.0.0.1" ||
		r.BeforeVersion != "1.0.0.0" || r.FileName != "audio.exe" || r.MD5 != "abc" ||
		r.Source != "QuickFix" || r.Result != "Installed" || r.Message != "exit=0" {
		t.Fatalf("round-trip mismatch: %#v", r)
	}
}
