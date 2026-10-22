package audit

// Source attribution over the resolved evidence: which versions this machine
// actually carries, which rule wins, and the lines that back the conclusion. It
// is split from the setupapi log parser because the two answer different
// questions — one reads a log, the other weighs evidence — and because the
// attribution half is what must keep honouring multi-version evidence.

import (
	"sort"
	"strconv"
	"strings"

	"lenovo-driver/internal/model"
)

// candidateVersions returns the version strings an audit may match against. A
// driver whose matched devices disagree has more than one real version on this
// machine, and every one of them is evidence: comparing only the joined display
// string ("a, b") against history and device rows matched nothing, so a driver
// with recorded install history was reported as having no source evidence at
// all.
func candidateVersions(driver *model.AssessedDriver) []string {
	if driver == nil {
		return nil
	}
	if driver.LocalEvidence.Kind() == model.LocalEvidenceConflicted {
		return driver.LocalEvidence.ConflictingVersions()
	}
	if v := driver.LocalEvidence.MeasuredVersion(); v != "" {
		return []string{v}
	}
	// No fallback to driver.LocalVersion. That field is the display projection,
	// so its non-empty states are prose ("not detected", "a, b"); treating one as
	// a candidate version let a driver with no evidence match a ledger row whose
	// version cell happened to hold the same sentence, and the audit then
	// attributed an install that never happened. The union is the authority: no
	// measured version means nothing to match against.
	return nil
}

// sourceEvidenceRules assembles the priority table for one driver in the
// documented order: install history, matching device, current official OS
// list, then alternate official OS list.
func sourceEvidenceRules(
	driver *model.AssessedDriver,
	history []model.HistoryRecord,
	deviceEvidence []model.Device,
	currentOSMap, alternateSourceMap map[string]model.SourceMapEntry,
) []sourceCategoryRule {
	candidates := candidateVersions(driver)
	rules := make([]sourceCategoryRule, 0, 4)

	if latest := latestSourceHistory(history, driver, candidates); latest != nil {
		summary := "Installed by this script from OSID " + latest.OSID
		if latest.OSID == driver.OSID {
			summary = "Installed by this script from the current OS source"
		} else if latest.OSName != "" {
			summary = "Installed by this script from " + latest.OSName
		}
		rules = append(rules, sourceCategoryRule{
			name:     "install-history",
			category: model.AuditCategoryInstallHistory,
			summary:  summary,
			match:    true,
		})
	}

	for _, device := range deviceEvidence {
		if !versionInCandidates(device.DriverVersion, candidates) {
			continue
		}
		// Device evidence comes from shared setupapi logs and the DriverStore,
		// which record a mechanism (offline image, online package, pre-existing
		// store) but not an attributable provenance. The category is therefore
		// External rather than a claim about who installed the driver; the
		// mechanism stays in the summary and the raw evidence lines.
		rule := sourceCategoryRule{name: "device:" + device.Name, match: true}
		rule.category = model.AuditCategoryExternal
		switch {
		case device.ImportSource != "" && device.ImportKind == "Offline":
			rule.summary = "External source (offline image integration): " + device.ImportSource
		case device.ImportSource != "":
			rule.summary = "External source (online package installation): " + device.ImportSource
		default:
			rule.summary = "External source (pre-existing DriverStore package)"
		}
		rules = append(rules, rule)
	}

	if sourceMapMatchAny(driver.Driver, currentOSMap, candidates) != nil {
		rules = append(rules, sourceCategoryRule{
			name:     "current-os",
			category: model.AuditCategoryCurrentOfficialOS,
			summary:  "Matches the current OS official Lenovo list",
			match:    true,
		})
	}

	if sourceMapMatchAny(driver.Driver, alternateSourceMap, candidates) != nil {
		rules = append(rules, sourceCategoryRule{
			name:     "alternate-os",
			category: model.AuditCategoryAlternateOfficialOS,
			summary:  "Matches another OS official Lenovo list",
			match:    true,
		})
	}
	return rules
}

// driverEvidenceLines gathers the always-on audit lines (install history and
// every matching device), independent of which source ultimately wins the
// category. The current/alternate OS lines are attached separately when their
// rule wins, to preserve the first-match summary contract.
func driverEvidenceLines(driver *model.AssessedDriver, deviceEvidence []model.Device, history []model.HistoryRecord) []string {
	candidates := candidateVersions(driver)
	var lines []string
	if latest := latestSourceHistory(history, driver, candidates); latest != nil {
		lines = append(lines, "History: time="+latest.Timestamp+"; version="+latest.Version+"; verified="+latest.VerifiedVersion+"; before="+latest.BeforeVersion+"; result="+latest.Result)
	}
	for _, device := range deviceEvidence {
		if !versionInCandidates(device.DriverVersion, candidates) {
			continue
		}
		lines = append(lines, "Device: "+device.Name+"; version="+device.DriverVersion+"; date="+device.DriverDate+"; INF="+device.InfName+"; provider="+device.ProviderName+"; install-date="+device.InstallDate)
		if device.PackageDir != "" {
			lines = append(lines, "DriverStore package: "+device.PackageDir+"; created="+device.PackageCreationTime+"; DriverVer="+device.PackageDriverVer)
		}
		if device.ImportSource != "" {
			lines = append(lines, "Import: source="+device.ImportSource+"; command="+device.ImportCommand+"; log="+device.ImportLog+":"+strconv.Itoa(device.ImportLine))
		}
	}
	return lines
}

// ResolveDriverSourceEvidence mirrors Resolve-DriverSourceEvidence.
func ResolveDriverSourceEvidence(driver *model.AssessedDriver, deviceEvidence []model.Device, history []model.HistoryRecord, alternateSourceMap, currentOSMap map[string]model.SourceMapEntry) model.SourceAudit {
	candidates := candidateVersions(driver)
	if len(candidates) == 0 {
		return model.SourceAudit{Category: model.AuditCategoryUnknown, Summary: "No local version evidence"}
	}
	localVersion := strings.Join(candidates, ", ")
	evidence := driverEvidenceLines(driver, deviceEvidence, history)
	for _, rule := range sourceEvidenceRules(driver, history, deviceEvidence, currentOSMap, alternateSourceMap) {
		if !rule.match {
			continue
		}
		audit := model.SourceAudit{Category: rule.category, Summary: rule.summary, EvidenceLines: evidence}
		if rule.category == model.AuditCategoryCurrentOfficialOS {
			if match := sourceMapMatchAny(driver.Driver, currentOSMap, candidates); match != nil {
				audit.EvidenceLines = append(audit.EvidenceLines, "Official current OS: "+match.OSName+" (OSID "+match.OSID+"); version="+localVersion)
			}
		} else if rule.category == model.AuditCategoryAlternateOfficialOS {
			if match := sourceMapMatchAny(driver.Driver, alternateSourceMap, candidates); match != nil {
				audit.EvidenceLines = append(audit.EvidenceLines, "Official alternate OS: "+match.OSName+" (OSID "+match.OSID+"); version="+localVersion)
			}
		}
		return audit
	}
	return model.SourceAudit{
		Category:      model.AuditCategoryUnknown,
		Summary:       "No install history, DriverStore import evidence, or official Lenovo version match",
		EvidenceLines: evidence,
	}
}

// ResolveDriverSourceLabel mirrors Resolve-DriverSourceLabel.
func ResolveDriverSourceLabel(driver *model.AssessedDriver, history []model.HistoryRecord, alternateSourceMap map[string]model.SourceMapEntry, sourceAudit *model.SourceAudit) string {
	if latest := latestSourceHistory(history, driver, candidateVersions(driver)); latest != nil {
		sourceOSID := latest.OSID
		sourceOSName := latest.OSName
		if sourceOSID != "" && sourceOSID != driver.OSID {
			if sourceOSName != "" {
				return "Local newer (source " + sourceOSName + ")"
			}
			return "Local newer (source OSID " + sourceOSID + ")"
		}
		if sourceOSID == driver.OSID {
			return "Local newer (same current OS source)"
		}
	}
	if sourceAudit != nil && sourceAudit.Category == model.AuditCategoryCurrentOfficialOS {
		return "Local newer (version matches current OS official list)"
	}
	return resolveExternalDriverSourceLabel(driver.Driver, alternateSourceMap, candidateVersions(driver))
}

func latestSourceHistory(history []model.HistoryRecord, driver *model.AssessedDriver, candidates []string) *model.HistoryRecord {
	if len(candidates) == 0 {
		return nil
	}
	var matches []model.HistoryRecord
	for _, h := range history {
		if h.Result != "Installed" && h.Result != "Verified" {
			continue
		}
		if !versionInCandidates(h.Version, candidates) && !versionInCandidates(h.VerifiedVersion, candidates) {
			continue
		}
		if h.DriverCode != driver.DriverCode && h.DriverName != driver.DriverName {
			continue
		}
		// A row recording the version this action replaced describes a past
		// state, not the current one, so it cannot be the source of what runs now.
		if h.BeforeVersion != "" && versionInCandidates(h.BeforeVersion, candidates) &&
			!versionInCandidates(h.Version, candidates) && !versionInCandidates(h.VerifiedVersion, candidates) {
			continue
		}
		matches = append(matches, h)
	}
	if len(matches) == 0 {
		return nil
	}
	sort.SliceStable(matches, func(i, j int) bool {
		return matches[i].Timestamp > matches[j].Timestamp
	})
	best := matches[0]
	return &best
}
