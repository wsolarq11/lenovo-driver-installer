package plan

import (
	"fmt"
	"strconv"
	"strings"

	"lenovo-driver/internal/compare"
	"lenovo-driver/internal/model"
)

// BuildPlanText mirrors Build-PlanText.
func BuildPlanText(drivers []*model.AssessedDriver, generatedAt string) []string {
	lines := []string{
		"Lenovo driver plan",
		"Generated: " + generatedAt,
		"",
	}
	for i, d := range drivers {
		if d == nil || d.Driver == nil {
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
			"  Status : "+string(d.CompareStatus),
			"  Evidence : "+model.StatusEvidenceBasis(d.CompareStatus),
		)
		if d.DeviceProblem != "" {
			lines = append(lines, "  Device health : "+d.DeviceProblem)
		}
		if d.CompareSource != "" {
			lines = append(lines, "  Source note : "+d.CompareSource)
		}
		if d.SourceAudit != nil {
			lines = append(lines, "  Source audit : "+string(d.SourceAudit.Category)+": "+d.SourceAudit.Summary)
			for _, evidenceLine := range d.SourceAudit.EvidenceLines {
				lines = append(lines, "    - "+evidenceLine)
			}
		}
		switch d.CompareStatus {
		case model.StatusNotApplicable:
			reason := d.NonMatchReason
			if reason == "" {
				reason = "no local device match"
			}
			lines = append(lines, "  Reason : "+reason)
		case model.StatusUnknown:
			lines = append(lines, "  Reason : "+unknownReason(d))
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

// driverLabel is how summary notes name a row of theirs. The driver code is
// the identity the ledger and the GUI speak in, so it backs up the display
// name rather than sitting next to it.
func driverLabel(d *model.AssessedDriver) string {
	if d == nil || d.Driver == nil {
		return "driver"
	}
	if d.DriverName != "" {
		return d.DriverName
	}
	if d.DriverCode != "" {
		return d.DriverCode
	}
	return "driver"
}

// Reason lines in BuildPlanText: every status is a version comparison whose
// inputs are already printed on the Remote and Local lines - except Not
// applicable and Unknown, whose decision rests on evidence the block does not
// otherwise show. Those two get a Reason line so the durable artifact carries
// the same explanation the console and the GUI export show.

// unknownReason names the actual cause of an Unknown. The status has three
// distinct origins - an official row with no version, a provisioned-package
// placeholder, and versions that refuse to parse - and one blanket sentence
// guesses at the cause, where a wrong guess reads as fact.
func unknownReason(d *model.AssessedDriver) string {
	if d.LocalVersion == model.LocalVersionProvisioned {
		return "local version is the Windows provisioning placeholder; the package is on the machine " +
			"but no installed component matched, so neither presence nor absence is measured"
	}
	if d.Version == "" {
		return "the official row carries no version to compare against"
	}
	return "neither version could be parsed into comparable numeric parts"
}

// FormatDriverTableLines mirrors Format-DriverTableLines.
func FormatDriverTableLines(drivers []*model.AssessedDriver) []string {
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
		if d == nil || d.Driver == nil {
			continue
		}
		line := compare.FormatCell(strconv.Itoa(i+1), indexWidth, true) + " " +
			compare.FormatCell(compare.GetTruncatedText(d.DriverName, driverWidth), driverWidth, false) + " " +
			compare.FormatCell(compare.GetTruncatedText(d.Version, remoteWidth), remoteWidth, false) + " " +
			compare.FormatCell(compare.GetTruncatedText(d.LocalVersion, localWidth), localWidth, false) + " " +
			compare.FormatCell(string(d.CompareStatus), statusWidth, false)
		lines = append(lines, line)
	}
	lines = append(lines, "")
	return lines
}

// FormatStatusSummaryLines mirrors Format-StatusSummaryLines.
func FormatStatusSummaryLines(drivers []*model.AssessedDriver) []string {
	counts := map[model.CompareStatus]int{}
	for _, status := range []model.CompareStatus{model.StatusUpdate, model.StatusUpToDate, model.StatusNotInstalled, model.StatusLocalNewer, model.StatusUnknown, model.StatusNotApplicable} {
		counts[status] = 0
	}
	for _, d := range drivers {
		if d == nil || d.Driver == nil {
			continue
		}
		if _, ok := counts[d.CompareStatus]; ok {
			counts[d.CompareStatus]++
		}
	}
	lines := []string{
		"",
		fmt.Sprintf("  Update         : %d", counts[model.StatusUpdate]),
		fmt.Sprintf("  Up to date     : %d", counts[model.StatusUpToDate]),
		fmt.Sprintf("  Not installed  : %d", counts[model.StatusNotInstalled]),
		fmt.Sprintf("  Local newer    : %d", counts[model.StatusLocalNewer]),
		fmt.Sprintf("  Unknown        : %d", counts[model.StatusUnknown]),
		fmt.Sprintf("  Not applicable : %d", counts[model.StatusNotApplicable]),
	}
	factCount, inferenceCount, undeterminedCount := 0, 0, 0
	for _, d := range drivers {
		if d == nil || d.Driver == nil {
			continue
		}
		switch model.StatusEvidenceBasis(d.CompareStatus) {
		case "fact":
			factCount++
		case "inference":
			inferenceCount++
		case "undetermined":
			undeterminedCount++
		}
	}
	lines = append(lines, fmt.Sprintf("  Evidence basis : fact=%d inference=%d undetermined=%d", factCount, inferenceCount, undeterminedCount))
	if undeterminedCount > 0 {
		lines = append(lines, "  Note: undetermined = the evidence cannot settle the driver's state; each plan block's Reason line names what was checked.")
	}
	// Unknown names three distinct causes (no official version, a provisioned
	// placeholder, unparseable versions). The per-row reason says which one
	// actually applies instead of one blanket sentence guessing for all.
	if counts[model.StatusUnknown] > 0 {
		shown := 0
		for _, d := range drivers {
			if d == nil || d.Driver == nil || d.CompareStatus != model.StatusUnknown {
				continue
			}
			if shown >= 5 {
				break
			}
			lines = append(lines, "  Note: "+driverLabel(d)+": "+unknownReason(d))
			shown++
		}
	}
	if counts[model.StatusNotApplicable] > 0 {
		lines = append(lines, "  Note: Not applicable = no local device matched the driver's hardware ids or name rules.")
		shown := 0
		for _, d := range drivers {
			if d == nil || d.Driver == nil || d.CompareStatus != model.StatusNotApplicable {
				continue
			}
			if shown >= 5 {
				break
			}
			reason := d.NonMatchReason
			if reason == "" {
				reason = "no local device match"
			}
			lines = append(lines, "  Note: "+driverLabel(d)+": "+reason)
			shown++
		}
	}
	if counts[model.StatusLocalNewer] > 0 {
		shown := 0
		for _, d := range drivers {
			if d == nil || d.Driver == nil || d.CompareStatus != model.StatusLocalNewer {
				continue
			}
			if shown >= 5 {
				break
			}
			note := d.CompareSource
			if note == "" {
				note = "Local newer (source unknown)"
			}
			lines = append(lines, "  Note: "+driverLabel(d)+": "+note)
			shown++
		}
	}
	if problem := formatProblemNotes(drivers); len(problem) > 0 {
		lines = append(lines, problem...)
	}
	lines = append(lines, "")
	return lines
}

// FormatAttentionNotes lists the drivers that need explicit user attention
// before installation: matched devices with a problem code, or comparison
// results that are not a direct version fact. Each category is capped so the
// interactive prompt stays scannable.
func FormatAttentionNotes(drivers []*model.AssessedDriver) []string {
	var lines []string
	problemShown, evidenceShown := 0, 0
	for _, d := range drivers {
		if d == nil || d.Driver == nil {
			continue
		}
		if d.DeviceProblem != "" && problemShown < 5 {
			lines = append(lines, "  attention: "+d.DriverName+": device problem ("+d.DeviceProblem+")")
			problemShown++
			continue
		}
		if model.StatusEvidenceBasis(d.CompareStatus) != "fact" && evidenceShown < 5 {
			lines = append(lines, "  attention: "+d.DriverName+": "+string(d.CompareStatus)+" is "+model.StatusEvidenceBasis(d.CompareStatus))
			evidenceShown++
		}
	}
	return lines
}

// formatProblemNotes lists drivers whose matched devices report a Windows
// problem code, capped at five rows to keep the summary scannable.
func formatProblemNotes(drivers []*model.AssessedDriver) []string {
	var lines []string
	shown := 0
	for _, d := range drivers {
		if d == nil || d.Driver == nil || d.DeviceProblem == "" {
			continue
		}
		if shown >= 5 {
			break
		}
		lines = append(lines, "  Note: "+d.DriverName+": device problem ("+d.DeviceProblem+")")
		shown++
	}
	if len(lines) > 0 {
		lines = append([]string{"  Note: matched devices report problems; review before installing."}, lines...)
	}
	return lines
}

// ParseDriverSelectionTokens mirrors Parse-DriverSelectionTokens.
func ParseDriverSelectionTokens(inputText string, allSelected []*model.AssessedDriver) model.SelectionResult {
	var selected []*model.AssessedDriver
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
		if allSelected[index].CompareStatus == model.StatusNotApplicable {
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

// BuildDriverHistoryRecord mirrors Build-DriverHistoryRecord. It carries the
// driver's matched device ids into the Devices column so every ledger row for
// an action names the devices it targeted; the device audit joins on that
// identity instead of parsing the free-text Message.
func BuildDriverHistoryRecord(driver *model.AssessedDriver, result, message, verifiedVersion, beforeVersion, timestamp string) model.HistoryRecord {
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
		Devices:         driver.MatchedDeviceIDs,
	}
}
