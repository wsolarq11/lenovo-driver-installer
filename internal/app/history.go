package app

import (
	"context"
	"strings"
	"time"

	"lenovo-driver/internal/ledger"
	"lenovo-driver/internal/model"
	"lenovo-driver/internal/plan"
)

// This file is the orchestrator's thin adapter over internal/ledger. The ledger
// package owns the column order, the integrity chain, and the read/write pair;
// what stays here is the two things that are genuinely orchestration: logging a
// failed append, and tolerating an older on-disk header by reporting it once.

func (a *App) writeHistoryRecordChecked(ctx context.Context, driver *model.AssessedDriver, result, message, verifiedVersion, beforeVersion string) {
	if err := a.WriteHistoryRecord(driver, result, message, verifiedVersion, beforeVersion); err != nil {
		a.Log(ctx, "Failed to write history record: "+err.Error(), "ERROR")
	}
}

// ReadHistory reads the CSV history and returns only rows with a DriverCode.
// A header older than the current schema is reported once, so an absent device
// identity is never mistaken for "no device matched".
func (a *App) ReadHistory(ctx context.Context) []model.HistoryRecord {
	header, rows, err := ledger.ReadRows(a.HistoryPath)
	if err != nil {
		a.Log(ctx, "Could not read driver history: "+err.Error(), "WARN")
		return nil
	}
	if len(header) == 0 {
		return nil
	}
	if missing := ledger.MissingColumns(header); len(missing) > 0 {
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
			Devices:         ledger.SplitDevices(get(row, "Devices")),
		})
	}
	return records
}

// WriteHistoryRecord appends one history row through the ledger package. The
// record's timestamp is taken here because the clock is orchestration input, not
// ledger structure: the ledger package never reads time itself, so replaying a
// record is deterministic.
func (a *App) WriteHistoryRecord(driver *model.AssessedDriver, result, message, verifiedVersion, beforeVersion string) error {
	return ledger.Append(a.HistoryPath, plan.BuildDriverHistoryRecord(
		driver,
		result,
		message,
		verifiedVersion,
		beforeVersion,
		formatTimestamp(time.Now()),
	))
}
