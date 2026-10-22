package ledger

import (
	"encoding/csv"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"lenovo-driver/internal/model"
)

// TestColumnsGolden locks the ledger column order against a literal.
// Reordering, adding, or removing a column must be an explicit, reviewed edit
// to this golden, not a silent drift between the header and the row writer.
func TestColumnsGolden(t *testing.T) {
	want := []string{
		"Timestamp", "DriverCode", "OSID", "OSName", "DriverName", "Version",
		"VerifiedVersion", "BeforeVersion", "FileName", "MD5", "Source", "Result", "Message",
		"Devices",
	}
	if !reflect.DeepEqual(Columns, want) {
		t.Fatalf("Columns drift: got %#v, want %#v", Columns, want)
	}
}

func TestColumnsUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, col := range Columns {
		if seen[col] {
			t.Fatalf("duplicate history column %q", col)
		}
		seen[col] = true
	}
}

// TestValuesAlignsWithColumns proves the value writer stays field-for-field
// aligned with the column order. It round-trips a fully populated record through
// the column names, so both a wrong order and a wrong field land in the wrong
// slot and fail the assertion.
func TestValuesAlignsWithColumns(t *testing.T) {
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
	values := Values(rec)
	if len(values) != len(Columns) {
		t.Fatalf("Values has %d fields, Columns has %d", len(values), len(Columns))
	}
	var rebuilt model.HistoryRecord
	for i, col := range Columns {
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
			rebuilt.Devices = SplitDevices(values[i])
		default:
			t.Fatalf("unknown history column %q", col)
		}
	}
	if !reflect.DeepEqual(rebuilt, rec) {
		t.Fatalf("Values misaligned with Columns: got %#v, want %#v", rebuilt, rec)
	}
}

// TestAppendHeaderUsesColumns proves a freshly created ledger writes the
// single-source header first and the single-source row values second, so the
// header and the first data row can never disagree about column order.
func TestAppendHeaderUsesColumns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.csv")
	if err := Append(path, model.HistoryRecord{DriverCode: "d1", DriverName: "Audio", Result: "Installed", Message: "exit=0"}); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
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
	if !reflect.DeepEqual(rows[0], ledgerHeader()) {
		t.Fatalf("ledger header = %#v, want %#v", rows[0], ledgerHeader())
	}
	if rows[1][1] != "d1" || rows[1][11] != "Installed" {
		t.Fatalf("data row landed in wrong columns: %#v", rows[1])
	}
	if len(rows[1]) != len(Columns)+1 || len(rows[1][len(Columns)]) != 64 {
		t.Fatalf("data row must carry a 64-char hash: %#v", rows[1])
	}
}

// TestChainIntact writes two rows and verifies the chained hash is recomputable
// from the on-disk bytes.
func TestChainIntact(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.csv")
	if err := Append(path, model.HistoryRecord{DriverCode: "d1", Result: "Installed", Message: "exit=0"}); err != nil {
		t.Fatal(err)
	}
	if err := Append(path, model.HistoryRecord{DriverCode: "d1", Result: "Verified", Message: "binding=bound"}); err != nil {
		t.Fatal(err)
	}
	if err := VerifyChain(path); err != nil {
		t.Fatalf("intact chain should verify: %v", err)
	}
}

// TestChainDetectsTamper proves a single rewritten data field breaks the chain,
// i.e. the ledger is tamper-evident rather than merely append-only.
func TestChainDetectsTamper(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.csv")
	if err := Append(path, model.HistoryRecord{DriverCode: "d1", Result: "Installed", Message: "exit=0"}); err != nil {
		t.Fatal(err)
	}
	if err := Append(path, model.HistoryRecord{DriverCode: "d1", Result: "Verified", Message: "binding=bound"}); err != nil {
		t.Fatal(err)
	}
	rewriteCSV(t, path, 1, 1, "dX")
	if err := VerifyChain(path); err == nil {
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

// TestHashChainIndexSurvivesSchemaGrowth proves the integrity chain is located by
// column name, not by the current schema's column count. A ledger written under
// the older 13-column schema must still verify against its own layout after the
// schema grew; a positional index would have silently started verifying the
// wrong cells and stopped detecting tampering.
func TestHashChainIndexSurvivesSchemaGrowth(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.csv")
	legacyColumns := []string{
		"Timestamp", "DriverCode", "OSID", "OSName", "DriverName", "Version",
		"VerifiedVersion", "BeforeVersion", "FileName", "MD5", "Source", "Result", "Message",
	}
	data := []string{"2026-01-01T00:00:00Z", "d1", "42", "Win10", "Audio", "1.0", "1.0", "0.9", "a.exe", "md5", "QuickFix", "Installed", "exit=0"}
	row := append(append([]string(nil), data...), rowHash("", data))

	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w := csv.NewWriter(f)
	if err := w.Write(append(append([]string(nil), legacyColumns...), hashColumn)); err != nil {
		t.Fatal(err)
	}
	if err := w.Write(row); err != nil {
		t.Fatal(err)
	}
	w.Flush()
	f.Close()

	if err := VerifyChain(path); err != nil {
		t.Fatalf("legacy ledger must verify against its own column layout: %v", err)
	}

	// Tampering with the legacy row must still be caught.
	rewriteCSV(t, path, 1, 5, "tampered")
	if err := VerifyChain(path); err == nil {
		t.Fatal("tampering in a legacy-layout ledger must break the chain")
	}
}

// TestAppendRefusesStaleSchema proves an existing ledger whose header predates
// the current schema is refused loudly instead of being appended to. Its rows are
// append-only and cannot be widened, so appending a wider row would silently bind
// values to the wrong column names.
func TestAppendRefusesStaleSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.csv")

	stale := []string{
		"Timestamp", "DriverCode", "OSID", "OSName", "DriverName", "Version",
		"VerifiedVersion", "BeforeVersion", "FileName", "MD5", "Source", "Result", "Message",
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w := csv.NewWriter(f)
	if err := w.Write(append(append([]string(nil), stale...), hashColumn)); err != nil {
		t.Fatal(err)
	}
	w.Flush()
	f.Close()

	err = Append(path, model.HistoryRecord{DriverCode: "d1", Result: "Installed", Message: "exit=0"})
	if err == nil {
		t.Fatal("appending to a stale-schema ledger must be refused")
	}
	if !strings.Contains(err.Error(), "Devices") {
		t.Fatalf("refusal must name the missing column: %v", err)
	}
	if !strings.Contains(err.Error(), "history.csv") {
		t.Fatalf("refusal must name the file: %v", err)
	}

	// The refused ledger must be left untouched (no partial row appended).
	_, rows, readErr := ReadRows(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(rows) != 0 {
		t.Fatalf("refused write must not append anything, got %#v", rows)
	}
}

// TestJoinDevicesDedupes proves the recorded identity is a set: a device that
// matched a driver more than once is recorded once, so the audit join cannot
// double-count it.
func TestJoinDevicesDedupes(t *testing.T) {
	if got := joinDevices([]string{"A", "A", "", "B", "A", "B"}); got != "A;B" {
		t.Fatalf("joinDevices = %q, want %q", got, "A;B")
	}
}
