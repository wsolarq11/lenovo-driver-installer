package plan

import (
	"fmt"
	"strconv"
	"strings"

	"lenovo-driver/internal/compare"
	"lenovo-driver/internal/model"
)

// BuildPlanText mirrors Build-PlanText.
func BuildPlanText(drivers []*model.Driver, generatedAt string) []string {
	lines := []string{
		"Lenovo driver plan",
		"Generated: " + generatedAt,
		"",
	}
	for i, d := range drivers {
		if d == nil {
			continue
		}
		local := d.LocalVersion
		if local == "" {
			local = "not detected"
		}
		lines = append(lines,
			fmt.Sprintf("[%d] %s", i+1, d.DriverName),
			"  Remote : "+d.Version,
			"  Local  : "+local,
			"  Status : "+d.CompareStatus,
		)
		if d.CompareSource != "" {
			lines = append(lines, "  Source note : "+d.CompareSource)
		}
		if d.SourceAudit != nil {
			lines = append(lines, "  Source audit : "+d.SourceAudit.Category+": "+d.SourceAudit.Summary)
			for _, evidenceLine := range d.SourceAudit.EvidenceLines {
				lines = append(lines, "    - "+evidenceLine)
			}
		}
		lines = append(lines,
			"  OS     : "+d.OSName+" (OSID "+d.OSID+")",
			"  Source : "+d.SourceAPI,
		)
		md5 := d.OfficialMD5
		if md5 == "" {
			md5 = "not provided"
		}
		lines = append(lines,
			"  MD5    : "+md5,
			"  File   : "+d.FileName,
			"  URL    : "+d.FilePath,
			"",
		)
	}
	return lines
}

// FormatDriverTableLines mirrors Format-DriverTableLines.
func FormatDriverTableLines(drivers []*model.Driver) []string {
	const (
		indexWidth  = 3
		driverWidth = 42
		remoteWidth = 36
		localWidth  = 20
		statusWidth = 14
	)
	header := compare.FormatCell("#", indexWidth, true) + " " +
		compare.FormatCell("Driver", driverWidth, false) + " " +
		compare.FormatCell("Remote", remoteWidth, false) + " " +
		compare.FormatCell("Local", localWidth, false) + " " +
		compare.FormatCell("Status", statusWidth, false)
	separator := compare.FormatCell("---", indexWidth, true) + " " +
		compare.FormatCell("------", driverWidth, false) + " " +
		compare.FormatCell("------", remoteWidth, false) + " " +
		compare.FormatCell("-----", localWidth, false) + " " +
		compare.FormatCell("------", statusWidth, false)
	lines := []string{"", header, separator}
	for i, d := range drivers {
		if d == nil {
			continue
		}
		line := compare.FormatCell(strconv.Itoa(i+1), indexWidth, true) + " " +
			compare.FormatCell(compare.GetTruncatedText(d.DriverName, driverWidth), driverWidth, false) + " " +
			compare.FormatCell(compare.GetTruncatedText(d.Version, remoteWidth), remoteWidth, false) + " " +
			compare.FormatCell(compare.GetTruncatedText(d.LocalVersion, localWidth), localWidth, false) + " " +
			compare.FormatCell(d.CompareStatus, statusWidth, false)
		lines = append(lines, line)
	}
	lines = append(lines, "")
	return lines
}

// FormatStatusSummaryLines mirrors Format-StatusSummaryLines.
func FormatStatusSummaryLines(drivers []*model.Driver) []string {
	counts := map[string]int{}
	for _, status := range []string{"Update", "Up to date", "Not installed", "Local newer", "Unknown", "Not applicable"} {
		counts[status] = 0
	}
	for _, d := range drivers {
		if d == nil {
			continue
		}
		if _, ok := counts[d.CompareStatus]; ok {
			counts[d.CompareStatus]++
		}
	}
	lines := []string{
		"",
		fmt.Sprintf("  Update         : %d", counts["Update"]),
		fmt.Sprintf("  Up to date     : %d", counts["Up to date"]),
		fmt.Sprintf("  Not installed  : %d", counts["Not installed"]),
		fmt.Sprintf("  Local newer    : %d", counts["Local newer"]),
		fmt.Sprintf("  Unknown        : %d", counts["Unknown"]),
		fmt.Sprintf("  Not applicable : %d", counts["Not applicable"]),
	}
	if counts["Unknown"] > 0 {
		lines = append(lines, "  Note: Unknown = multi-vendor package, no matching local component found.")
	}
	if counts["Not applicable"] > 0 {
		lines = append(lines, "  Note: Not applicable = hardware not detected on this machine.")
	}
	if counts["Local newer"] > 0 {
		shown := 0
		for _, d := range drivers {
			if d == nil || d.CompareStatus != "Local newer" {
				continue
			}
			if shown >= 5 {
				break
			}
			note := d.CompareSource
			if note == "" {
				note = "Local newer (source unknown)"
			}
			lines = append(lines, "  Note: "+d.DriverName+": "+note)
			shown++
		}
	}
	lines = append(lines, "")
	return lines
}

// ParseDriverSelectionTokens mirrors Parse-DriverSelectionTokens.
func ParseDriverSelectionTokens(inputText string, allSelected []*model.Driver) model.SelectionResult {
	var selected []*model.Driver
	var invalid []string
	var notApplicable []string
	seen := map[int]bool{}
	for _, token := range strings.FieldsFunc(inputText, func(r rune) bool {
		return r == ',' || r == ';' || r == ' '
	}) {
		if token == "" {
			continue
		}
		number, err := strconv.Atoi(token)
		if err != nil {
			invalid = append(invalid, token)
			continue
		}
		index := number - 1
		if index < 0 || index >= len(allSelected) {
			invalid = append(invalid, strconv.Itoa(number))
			continue
		}
		if allSelected[index].CompareStatus == "Not applicable" {
			notApplicable = append(notApplicable, strconv.Itoa(number))
			continue
		}
		if seen[index] {
			continue
		}
		seen[index] = true
		selected = append(selected, allSelected[index])
	}
	return model.SelectionResult{Selected: selected, Invalid: invalid, NotApplicable: notApplicable}
}

// BuildDriverHistoryRecord mirrors Build-DriverHistoryRecord.
func BuildDriverHistoryRecord(driver *model.Driver, result, message, verifiedVersion, beforeVersion, timestamp string) model.HistoryRecord {
	return model.HistoryRecord{
		Timestamp:       timestamp,
		DriverCode:      driver.DriverCode,
		OSID:            driver.OSID,
		OSName:          driver.OSName,
		DriverName:      driver.DriverName,
		Version:         driver.Version,
		VerifiedVersion: verifiedVersion,
		BeforeVersion:   beforeVersion,
		FileName:        driver.FileName,
		MD5:             driver.OfficialMD5,
		Source:          driver.SourceAPI,
		Result:          result,
		Message:         message,
	}
}
