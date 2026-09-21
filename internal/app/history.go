package app

import (
	"context"
	"encoding/csv"
	"os"
	"strings"
	"time"

	"lenovo-driver/internal/model"
	"lenovo-driver/internal/plan"
)

var historyHeader = []string{
	"Timestamp", "DriverCode", "OSID", "OSName", "DriverName", "Version",
	"VerifiedVersion", "BeforeVersion", "FileName", "MD5", "Source", "Result", "Message",
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

// WriteHistoryRecord appends one history row, creating the header on the first
// write. The record is written in the same open operation as the header so a
// fresh ledger never drops its first entry.
func (a *App) WriteHistoryRecord(driver *model.AssessedDriver, result, message, verifiedVersion, beforeVersion string) error {
	_, statErr := os.Stat(a.HistoryPath)
	creating := os.IsNotExist(statErr)
	f, err := os.OpenFile(a.HistoryPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	writer := csv.NewWriter(f)
	if creating {
		if err := writer.Write(historyHeader); err != nil {
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
	if err := writer.Write([]string{
		record.Timestamp, record.DriverCode, record.OSID, record.OSName, record.DriverName,
		record.Version, record.VerifiedVersion, record.BeforeVersion, record.FileName,
		record.MD5, record.Source, record.Result, record.Message,
	}); err != nil {
		return err
	}
	writer.Flush()
	return writer.Error()
}
