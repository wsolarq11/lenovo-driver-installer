package app

import (
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"fmt"
	"os"
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
}

// historyHashColumn is the trailing integrity column, appended after the data
// columns. It is structural, not a data field: it holds the SHA-256 of the
// previous row's hash chained with this row's data, making the append-only
// ledger tamper-evident (a rewrite, reorder, or deletion breaks the chain).
const historyHashColumn = "Hash"

// historyValues renders one HistoryRecord in historyColumns order. The field
// order here must match historyColumns exactly; the contract test enforces the
// length and round-trips a known record through the CSV so a misalignment
// cannot silently drift the ledger.
func historyValues(r model.HistoryRecord) []string {
	return []string{
		r.Timestamp, r.DriverCode, r.OSID, r.OSName, r.DriverName,
		r.Version, r.VerifiedVersion, r.BeforeVersion, r.FileName,
		r.MD5, r.Source, r.Result, r.Message,
	}
}

func (a *App) writeHistoryRecordChecked(ctx context.Context, driver *model.AssessedDriver, result, message, verifiedVersion, beforeVersion string) {
	if err := a.WriteHistoryRecord(driver, result, message, verifiedVersion, beforeVersion); err != nil {
		a.Log(ctx, "Failed to write history record: "+err.Error(), "ERROR")
	}
}

// ReadHistory reads the CSV history and returns only rows with a DriverCode.
func (a *App) ReadHistory(ctx context.Context) []model.HistoryRecord {
	f, err := os.Open(a.HistoryPath)
	if err != nil {
		return nil
	}
	defer f.Close()
	reader := csv.NewReader(f)
	rows, err := reader.ReadAll()
	if err != nil {
		a.Log(ctx, "Could not read driver history: "+err.Error(), "WARN")
		return nil
	}
	if len(rows) == 0 {
		return nil
	}
	if len(rows[0]) > 0 {
		rows[0][0] = strings.TrimPrefix(rows[0][0], "\uFEFF")
	}
	header := rows[0]
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
	for _, row := range rows[1:] {
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

// lastHistoryHash returns the trailing hash of the last ledger row, or "" when
// the ledger is absent, empty, or its last row predates the hash column.
func (a *App) lastHistoryHash() (string, error) {
	f, err := os.Open(a.HistoryPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	defer f.Close()
	rows, err := csv.NewReader(f).ReadAll()
	if err != nil {
		return "", err
	}
	if len(rows) < 2 {
		return "", nil
	}
	last := rows[len(rows)-1]
	if len(last) <= len(historyColumns) {
		return "", nil
	}
	return last[len(historyColumns)], nil
}

// verifyHistoryChain recomputes the integrity chain over every hashed row and
// returns an error at the first broken link. Pre-hash (legacy) rows are skipped
// because they were written before the hash column existed and cannot be
// retroactively covered.
func verifyHistoryChain(path string) error {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()
	rows, err := csv.NewReader(f).ReadAll()
	if err != nil {
		return err
	}
	prevHash := ""
	for i := 1; i < len(rows); i++ {
		row := rows[i]
		if len(row) <= len(historyColumns) {
			continue
		}
		want := historyRowHash(prevHash, row[:len(historyColumns)])
		if row[len(historyColumns)] != want {
			return fmt.Errorf("history hash chain broken at row %d", i+1)
		}
		prevHash = row[len(historyColumns)]
	}
	return nil
}

// WriteHistoryRecord appends one history row, creating the header on the first
// write. The record is written in the same open operation as the header so a
// fresh ledger never drops its first entry. Each row carries a chained hash so
// later rewrite/reorder/deletion of the append-only ledger is detectable.
func (a *App) WriteHistoryRecord(driver *model.AssessedDriver, result, message, verifiedVersion, beforeVersion string) error {
	_, statErr := os.Stat(a.HistoryPath)
	creating := os.IsNotExist(statErr)
	prevHash, err := a.lastHistoryHash()
	if err != nil {
		return err
	}
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
