package app

import (
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"lenovo-driver/internal/model"
	"lenovo-driver/internal/plan"
)

// historyColumns is the single source of truth for the ledger CSV data-column
// order. Both the header write and every data-row write are derived from it, so
// adding or reordering a data column happens in exactly one place. The contract
// test in history_test.go locks this slice against a golden literal and
// verifies that historyValues stays field-for-field aligned with it.
var historyColumns = []string{
	"Timestamp", "DriverCode", "OSID", "OSName", "DriverName", "Version",
	"VerifiedVersion", "BeforeVersion", "FileName", "MD5", "Source", "Result", "Message",
	"Devices",
}

// historyHashColumn is the trailing integrity column, appended after the data
// columns. It is structural, not a data field: it holds the SHA-256 of the
// previous row's hash chained with this row's data, making the append-only
// ledger tamper-evident (a rewrite, reorder, or deletion breaks the chain).
const historyHashColumn = "Hash"

// historyDeviceSeparator separates device instance ids inside the Devices
// column. PnP instance ids are built from class GUIDs, instance paths,
// ampersands, backslashes, and dots, so a semicolon is unambiguous and needs no
// escaping; the writer and the reader share this one constant.
const historyDeviceSeparator = ";"

// joinDevices renders the device identity cell: unique, non-empty ids in
// first-seen order. Recording a set rather than a multiset keeps the
// device-level join from double-counting a device that matched a driver more
// than once.
func joinDevices(devices []string) string {
	seen := map[string]bool{}
	unique := make([]string, 0, len(devices))
	for _, id := range devices {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		unique = append(unique, id)
	}
	return strings.Join(unique, historyDeviceSeparator)
}

// splitDevices parses the device identity cell back into ids.
func splitDevices(cell string) []string {
	if cell == "" {
		return nil
	}
	var ids []string
	for _, part := range strings.Split(cell, historyDeviceSeparator) {
		if part = strings.TrimSpace(part); part != "" {
			ids = append(ids, part)
		}
	}
	return ids
}

// missingHistoryColumns returns the current schema's columns that an on-disk
// ledger header does not carry. The ledger is append-only, so a file created
// before a column existed keeps its original header forever and that column
// reads empty for every row of that file; callers surface the gap instead of
// letting an absent device identity look like "no devices matched".
func missingHistoryColumns(header []string) []string {
	present := make(map[string]bool, len(header))
	for _, name := range header {
		present[name] = true
	}
	var missing []string
	for _, column := range historyColumns {
		if !present[column] {
			missing = append(missing, column)
		}
	}
	return missing
}

// historyValues renders one HistoryRecord in historyColumns order. The field
// order here must match historyColumns exactly; the contract test enforces the
// length and round-trips a known record through the CSV so a misalignment
// cannot silently drift the ledger.
func historyValues(r model.HistoryRecord) []string {
	return []string{
		r.Timestamp, r.DriverCode, r.OSID, r.OSName, r.DriverName,
		r.Version, r.VerifiedVersion, r.BeforeVersion, r.FileName,
		r.MD5, r.Source, r.Result, r.Message, joinDevices(r.Devices),
	}
}

// ledgerHeaderDriftError returns a non-nil error when an existing ledger's
// header is not the current schema. The ledger is append-only and WORM: rows
// written before a column existed cannot be rewritten, so a file whose header
// predates the current schema can never carry that column. Appending a wider
// row than its header would bind values to the wrong names, and a stale header
// would silently drop the new identity column. Both are silent, expensive
// drift, so a schema mismatch is a loud refusal with the remedy spelled out
// instead of a quiet corruption.
func ledgerHeaderDriftError(header []string, path string) error {
	if len(header) == 0 {
		return nil
	}
	missing := missingHistoryColumns(header)
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf(
		"history ledger schema is older than this build and cannot record %s; "+
			"move %s aside (it is an append-only audit ledger and is never rewritten) and rerun",
		strings.Join(missing, ", "), filepath.Base(path))
}

// resolveHashIndex locates the structural hash column inside a ledger header by
// name. Deriving it from the header rather than from len(historyColumns) keeps
// chain verification working across a schema change: a positional index would
// silently point at a data column once a column was added, so tampering would
// stop being detectable without any error surfacing. ok is false for a ledger
// that carries no hash column (a pre-hash legacy file), which has no chain to
// verify and is handled by the caller.
func resolveHashIndex(header []string) (hashIndex, dataCount int, ok bool) {
	for i, name := range header {
		if name == historyHashColumn {
			return i, i, true
		}
	}
	return 0, 0, false
}

// readHistoryRows reads the ledger file and returns its header and data rows.
// A missing file yields an empty header and no rows; any other failure is
// reported to the caller so it can decide between degrading and refusing.
func readHistoryRows(path string) (header []string, rows [][]string, err error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	defer f.Close()
	rows, err = csv.NewReader(f).ReadAll()
	if err != nil {
		return nil, nil, err
	}
	if len(rows) == 0 {
		return nil, nil, nil
	}
	header = rows[0]
	if len(header) > 0 {
		header[0] = strings.TrimPrefix(header[0], "\uFEFF")
	}
	return header, rows[1:], nil
}

func (a *App) writeHistoryRecordChecked(ctx context.Context, driver *model.AssessedDriver, result, message, verifiedVersion, beforeVersion string) {
	if err := a.WriteHistoryRecord(driver, result, message, verifiedVersion, beforeVersion); err != nil {
		a.Log(ctx, "Failed to write history record: "+err.Error(), "ERROR")
	}
}

// ReadHistory reads the CSV history and returns only rows with a DriverCode.
// A header older than the current schema is reported once, so an absent device
// identity is never mistaken for "no device matched".
func (a *App) ReadHistory(ctx context.Context) []model.HistoryRecord {
	header, rows, err := readHistoryRows(a.HistoryPath)
	if err != nil {
		a.Log(ctx, "Could not read driver history: "+err.Error(), "WARN")
		return nil
	}
	if len(header) == 0 {
		return nil
	}
	if missing := missingHistoryColumns(header); len(missing) > 0 {
		a.Log(ctx, "History ledger predates this build and does not carry: "+strings.Join(missing, ", ")+". Those rows record no device identity.", "WARN")
	}
	index := map[string]int{}
	for i, name := range header {
		index[name] = i
	}
	get := func(row []string, name string) string {
		i, ok := index[name]
		if !ok || i < 0 || i >= len(row) {
			return ""
		}
		return row[i]
	}
	var records []model.HistoryRecord
	for _, row := range rows {
		if get(row, "DriverCode") == "" {
			continue
		}
		records = append(records, model.HistoryRecord{
			Timestamp:       get(row, "Timestamp"),
			DriverCode:      get(row, "DriverCode"),
			OSID:            get(row, "OSID"),
			OSName:          get(row, "OSName"),
			DriverName:      get(row, "DriverName"),
			Version:         get(row, "Version"),
			VerifiedVersion: get(row, "VerifiedVersion"),
			BeforeVersion:   get(row, "BeforeVersion"),
			FileName:        get(row, "FileName"),
			MD5:             get(row, "MD5"),
			Source:          get(row, "Source"),
			Result:          get(row, "Result"),
			Message:         get(row, "Message"),
			Devices:         splitDevices(get(row, "Devices")),
		})
	}
	return records
}

// historyHeader returns the ledger header: the data columns plus the trailing
// integrity hash column.
func historyHeader() []string {
	return append(append([]string(nil), historyColumns...), historyHashColumn)
}

// historyRowHash computes one link of the integrity chain: SHA-256 over the
// previous row's hash and this row's data fields, separated by the unit
// separator (0x1F) which never appears in a data field.
func historyRowHash(prevHash string, fields []string) string {
	h := sha256.New()
	h.Write([]byte(prevHash))
	h.Write([]byte{0x1F})
	for i, f := range fields {
		if i > 0 {
			h.Write([]byte{0x1F})
		}
		h.Write([]byte(f))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// ledgerHeaderAndTail reads the ledger once and returns its header plus the
// previous chain hash for the next append. Reading the header is what lets every
// consumer locate the structural hash column by NAME instead of by the current
// schema's column count; a positional index would shift onto a data column as
// soon as a column was added, and tamper detection would quietly stop working.
// A missing, empty, headerless, or pre-hash ledger yields the header (possibly
// empty) and an empty hash, i.e. a fresh chain to start.
func (a *App) ledgerHeaderAndTail() (header []string, prevHash string, err error) {
	header, rows, err := readHistoryRows(a.HistoryPath)
	if err != nil || len(header) == 0 || len(rows) == 0 {
		return header, "", err
	}
	hashIndex, _, hasHash := resolveHashIndex(header)
	if !hasHash {
		return header, "", nil
	}
	last := rows[len(rows)-1]
	if len(last) <= hashIndex {
		return header, "", nil
	}
	return header, last[hashIndex], nil
}

// verifyHistoryChain recomputes the integrity chain over every hashed row and
// returns an error at the first broken link. Pre-hash (legacy) rows are skipped
// because they were written before the hash column existed and cannot be
// retroactively covered. The hash column is located by name from the on-disk
// header, so a ledger written under an earlier schema is still verified against
// its own column layout rather than against the current one.
func verifyHistoryChain(path string) error {
	header, rows, err := readHistoryRows(path)
	if err != nil {
		return err
	}
	if len(header) == 0 {
		return nil
	}
	hashIndex, dataCount, hasHash := resolveHashIndex(header)
	if !hasHash {
		return nil
	}
	prevHash := ""
	for i, row := range rows {
		if len(row) <= hashIndex {
			continue
		}
		want := historyRowHash(prevHash, row[:dataCount])
		if row[hashIndex] != want {
			return fmt.Errorf("history hash chain broken at row %d", i+2)
		}
		prevHash = row[hashIndex]
	}
	return nil
}

// WriteHistoryRecord appends one history row, creating the header on the first
// write. The record is written in the same open operation as the header so a
// fresh ledger never drops its first entry. Each row carries a chained hash so
// later rewrite/reorder/deletion of the append-only ledger is detectable.
//
// A pre-existing ledger whose header predates the current schema is refused
// rather than appended to: the ledger is append-only and never rewritten, so
// its rows cannot be widened to the new schema, and appending a wider row would
// bind the reader's values to the wrong column names. Refusing keeps the audit
// trail honest instead of silently corrupting it.
func (a *App) WriteHistoryRecord(driver *model.AssessedDriver, result, message, verifiedVersion, beforeVersion string) error {
	header, prevHash, err := a.ledgerHeaderAndTail()
	if err != nil {
		return err
	}
	if err := ledgerHeaderDriftError(header, a.HistoryPath); err != nil {
		return err
	}
	creating := len(header) == 0

	f, err := os.OpenFile(a.HistoryPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	writer := csv.NewWriter(f)
	if creating {
		if err := writer.Write(historyHeader()); err != nil {
			return err
		}
	}
	record := plan.BuildDriverHistoryRecord(
		driver,
		result,
		message,
		verifiedVersion,
		beforeVersion,
		formatTimestamp(time.Now()),
	)
	fields := historyValues(record)
	row := append(fields, historyRowHash(prevHash, fields))
	if err := writer.Write(row); err != nil {
		return err
	}
	writer.Flush()
	return writer.Error()
}
