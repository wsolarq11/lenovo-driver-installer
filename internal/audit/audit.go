package audit

import (
	"regexp"
	"sort"
	"strconv"
	"strings"

	"lenovo-driver/internal/compare"
	"lenovo-driver/internal/model"
	"lenovo-driver/internal/pathutil"
)

var (
	reExitOpen             = regexp.MustCompile(`exit\s*\(`)
	reSectionImport        = regexp.MustCompile(`^>>>\s+\[(?:Import Driver Package|Setup Import Driver Package)\s*-\s*(?P<source>.+?)\]`)
	reSectionDriverInstall = regexp.MustCompile(`^>>>\s+\[Driver Install(?:\s*\([^)]*\))?\s*-\s*(?P<source>.+?)\]\s*$`)
	reSectionDeviceInstall = regexp.MustCompile(`^>>>\s+\[Device Install(?:\s*\([^)]*\))?\s*-\s*(?P<source>.+?)\]\s*$`)
	reSectionStoDvsImport  = regexp.MustCompile(`^\s*(?:sto|dvs):\s*\{\s*(?:Driver\s+)?(?:Setup\s+)?Import Driver Package\s*:\s*(?P<source>.+?)\}\s*(?:\d{2}:\d{2}:\d{2}(?:\.\d+)?)?$`)
	reCommand              = regexp.MustCompile(`^\s*cmd:\s*(?P<cmd>.*)`)
	reVersion              = regexp.MustCompile(`^\s*(?:inf|utl):\s+Driver Version\s*(?::|=|-)\s*(?P<ver>.*)`)
	reTargetPath           = regexp.MustCompile(`^\s*cpy:\s+Target Path\s*=\s*(?P<path>.+?)\s*$`)
	rePublished            = regexp.MustCompile(`^\s*cpy:\s+Published '(?P<published>[^']+)' to '(?P<oem>[^']+)'\.?`)
	reCreatedPackage       = regexp.MustCompile(`^\s*idb:\s+Created driver package object '(?P<dir>[^']+)' in DRIVERS database node\.?`)
	reCreatedInf           = regexp.MustCompile(`^\s*idb:\s+Created driver INF file object '(?P<oem>[^']+)' in DRIVERS database node\.?`)
	reRegistered           = regexp.MustCompile(`^\s*idb:\s+Registered driver package '(?P<dir>[^']+)' with '(?P<oem>[^']+)'\.?`)
	reRegisterPkg          = regexp.MustCompile(`^\s*idb:\s*\{\s*(?:Register|Publish) Driver Package\s*:\s*(?P<path>.+?)\}\s*(?:\d{2}:\d{2}:\d{2}(?:\.\d+)?)?$`)
	reCoreImport           = regexp.MustCompile(`^\s*sto:\s*\{\s*Core Driver Package Import:\s*(?P<pkg>[^}]+)\}\s*(?:\d{2}:\d{2}:\d{2}(?:\.\d+)?)?$`)
	reStoreFilename        = regexp.MustCompile(`^\s*sto:\s+Driver Store Filename\s*=\s*(?P<path>.+?)\s*$`)
	reSectionEnd           = regexp.MustCompile(`^<<<\s+(?:\[Exit status|Section end)`)
)

// SourceMapKeys returns the name/code version keys used by source maps.
func SourceMapKeys(driverName, driverCode, version string) []string {
	var keys []string
	for _, versionKey := range compare.GetVersionMatchKeys(version) {
		keys = append(keys, strings.TrimSpace(driverName)+"|"+versionKey)
		keys = append(keys, driverCode+"|"+versionKey)
	}
	return keys
}

// GetSourceMapMatch mirrors Get-SourceMapMatch.
func GetSourceMapMatch(driver *model.Driver, sourceMap map[string]model.SourceMapEntry, localVersion string) *model.SourceMapEntry {
	if len(sourceMap) == 0 || localVersion == "" {
		return nil
	}
	for _, key := range SourceMapKeys(driver.DriverName, driver.DriverCode, localVersion) {
		if match, ok := sourceMap[key]; ok {
			entry := match
			return &entry
		}
	}
	return nil
}

// ResolveExternalDriverSourceLabel mirrors Resolve-ExternalDriverSourceLabel.
func ResolveExternalDriverSourceLabel(driver *model.Driver, alternateSourceMap map[string]model.SourceMapEntry) string {
	if match := GetSourceMapMatch(driver, alternateSourceMap, driver.LocalVersion); match != nil {
		if match.OSName != "" {
			return "Local newer (source " + match.OSName + ")"
		}
		return "Local newer (source OSID " + match.OSID + ")"
	}
	return "Local newer (source unknown)"
}

type sectionStartRule struct {
	re            *regexp.Regexp
	kind          string
	updateCurrent bool
}

var sectionStartRules = []sectionStartRule{
	{re: reSectionImport, kind: "Import"},
	{re: reSectionDriverInstall, kind: "DriverInstall"},
	{re: reSectionDeviceInstall, kind: "DeviceInstall"},
	{re: reSectionStoDvsImport, kind: "Import", updateCurrent: true},
}

// fieldExtractRule maps one setupapi log line shape to a field setter. Each
// setter applies its own first-win guard ("only set if still empty"), so the
// priority order is explicit in the table instead of a chain of conditionals.
type fieldExtractRule struct {
	re    *regexp.Regexp
	extra func(string) bool // optional additional condition; nil means none
	set   func(*model.ImportRecord, string)
}

var fieldExtractRules = []fieldExtractRule{
	{re: reCommand, set: func(s *model.ImportRecord, line string) {
		s.Command = strings.TrimSpace(submatch(reCommand, line, "cmd"))
	}},
	{re: reVersion, set: func(s *model.ImportRecord, line string) {
		s.Version = strings.TrimSpace(submatch(reVersion, line, "ver"))
	}},
	{re: reTargetPath, set: func(s *model.ImportRecord, line string) {
		if s.PackageDir == "" {
			s.PackageDir = strings.TrimSpace(submatch(reTargetPath, line, "path"))
		}
	}},
	{re: rePublished, set: func(s *model.ImportRecord, line string) {
		published := strings.TrimSpace(submatch(rePublished, line, "published"))
		if s.PackageDir == "" && strings.Contains(published, `\`) {
			s.PackageDir = pathutil.Parent(published)
		}
		if s.OemInfName == "" {
			s.OemInfName = pathutil.Base(strings.TrimSpace(submatch(rePublished, line, "oem")))
		}
	}},
	{re: reCreatedPackage, set: func(s *model.ImportRecord, line string) {
		if s.PackageDir == "" {
			s.PackageDir = strings.TrimSpace(submatch(reCreatedPackage, line, "dir"))
		}
	}},
	{re: reCreatedInf, set: func(s *model.ImportRecord, line string) {
		if s.OemInfName == "" {
			s.OemInfName = pathutil.Base(strings.TrimSpace(submatch(reCreatedInf, line, "oem")))
		}
	}},
	{re: reRegistered, set: func(s *model.ImportRecord, line string) {
		if s.PackageDir == "" {
			s.PackageDir = strings.TrimSpace(submatch(reRegistered, line, "dir"))
		}
		if s.OemInfName == "" {
			s.OemInfName = pathutil.Base(strings.TrimSpace(submatch(reRegistered, line, "oem")))
		}
	}},
	{re: reRegisterPkg, set: func(s *model.ImportRecord, line string) {
		if s.PackageDir == "" {
			s.PackageDir = pathutil.Parent(strings.TrimSpace(submatch(reRegisterPkg, line, "path")))
		}
	}},
	{re: reCoreImport, extra: func(line string) bool { return !reExitOpen.MatchString(line) }, set: func(s *model.ImportRecord, line string) {
		if s.PackageDir == "" {
			s.PackageDir = strings.TrimSpace(submatch(reCoreImport, line, "pkg"))
		}
	}},
	{re: reStoreFilename, set: func(s *model.ImportRecord, line string) {
		path := strings.TrimSpace(submatch(reStoreFilename, line, "path"))
		if s.PackageDir == "" {
			s.PackageDir = pathutil.Parent(path)
		}
		if inf := pathutil.Base(path); strings.HasSuffix(strings.ToLower(inf), ".inf") {
			s.InfName = inf
		}
	}},
}

// ConvertFromImportLogText mirrors ConvertFrom-ImportLogText.
func ConvertFromImportLogText(lines []string, logPath string) []model.ImportRecord {
	var rows []model.ImportRecord
	var section *model.ImportRecord
	lineNo := 0
	newSection := func(sourcePath, headerKind string, lineNumber int) *model.ImportRecord {
		return &model.ImportRecord{
			SourcePath: sourcePath,
			InfName:    pathutil.Base(sourcePath),
			LogPath:    logPath,
			LineNumber: lineNumber,
			HeaderKind: headerKind,
		}
	}
	for _, line := range lines {
		lineNo++
		started := false
		for _, rule := range sectionStartRules {
			if !rule.re.MatchString(line) || reExitOpen.MatchString(line) {
				continue
			}
			sourcePath := strings.TrimSpace(submatch(rule.re, line, "source"))
			if rule.updateCurrent && section != nil && (section.HeaderKind == "DriverInstall" || section.HeaderKind == "DeviceInstall") {
				section.SourcePath = sourcePath
				section.InfName = pathutil.Base(sourcePath)
				section.LineNumber = lineNo
			} else {
				section = newSection(sourcePath, rule.kind, lineNo)
			}
			started = true
			break
		}
		if started {
			continue
		}
		if section == nil {
			continue
		}

		for _, rule := range fieldExtractRules {
			if !rule.re.MatchString(line) || (rule.extra != nil && !rule.extra(line)) {
				continue
			}
			rule.set(section, line)
			break
		}

		if reSectionEnd.MatchString(line) || (strings.Contains(line, "Import Driver Package") && reExitOpen.MatchString(line)) {
			row := *section
			rows = append(rows, row)
			section = nil
		}
	}
	if section != nil {
		rows = append(rows, *section)
	}
	return rows
}

// sourceCategoryRule is one entry in the source-audit priority table. Rules are
// evaluated in ascending slice order; the first rule whose match is true claims
// the category. The priority order is therefore explicit in the table order
// instead of a chain of conditionals.
type sourceCategoryRule struct {
	name     string
	category model.AuditCategory
	summary  string
	match    bool
}

// sourceEvidenceRules assembles the priority table for one driver in the
// documented order: install history, matching device, current official OS
// list, then alternate official OS list.
func sourceEvidenceRules(
	driver *model.Driver,
	history []model.HistoryRecord,
	deviceEvidence []model.Device,
	currentOSMap, alternateSourceMap map[string]model.SourceMapEntry,
) []sourceCategoryRule {
	localVersion := driver.LocalVersion
	rules := make([]sourceCategoryRule, 0, 4)

	if latest := latestSourceHistory(history, driver, localVersion); latest != nil {
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
		if device.DriverVersion != localVersion {
			continue
		}
		rule := sourceCategoryRule{name: "device:" + device.Name, match: true}
		switch {
		case device.ImportSource != "" && device.ImportKind == "Offline":
			rule.category = model.AuditCategoryOfflineImage
			rule.summary = "Offline image integration source: " + device.ImportSource
		case device.ImportSource != "":
			rule.category = model.AuditCategoryOnlinePackage
			rule.summary = "Online package installation source: " + device.ImportSource
		default:
			rule.category = model.AuditCategoryPreExistingStore
			rule.summary = "Active driver came from a pre-existing DriverStore package, not current install history"
		}
		rules = append(rules, rule)
	}

	if GetSourceMapMatch(driver, currentOSMap, localVersion) != nil {
		rules = append(rules, sourceCategoryRule{
			name:     "current-os",
			category: model.AuditCategoryCurrentOfficialOS,
			summary:  "Matches the current OS official Lenovo list",
			match:    true,
		})
	}

	if GetSourceMapMatch(driver, alternateSourceMap, localVersion) != nil {
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
func driverEvidenceLines(driver *model.Driver, deviceEvidence []model.Device, history []model.HistoryRecord) []string {
	localVersion := driver.LocalVersion
	var lines []string
	if latest := latestSourceHistory(history, driver, localVersion); latest != nil {
		lines = append(lines, "History: time="+latest.Timestamp+"; version="+latest.Version+"; verified="+latest.VerifiedVersion+"; before="+latest.BeforeVersion+"; result="+latest.Result)
	}
	for _, device := range deviceEvidence {
		if device.DriverVersion != localVersion {
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
func ResolveDriverSourceEvidence(driver *model.Driver, deviceEvidence []model.Device, history []model.HistoryRecord, alternateSourceMap, currentOSMap map[string]model.SourceMapEntry) model.SourceAudit {
	localVersion := driver.LocalVersion
	if localVersion == "" {
		return model.SourceAudit{Category: model.AuditCategoryUnknown, Summary: "No local version evidence"}
	}
	evidence := driverEvidenceLines(driver, deviceEvidence, history)
	for _, rule := range sourceEvidenceRules(driver, history, deviceEvidence, currentOSMap, alternateSourceMap) {
		if !rule.match {
			continue
		}
		audit := model.SourceAudit{Category: rule.category, Summary: rule.summary, EvidenceLines: evidence}
		if rule.category == model.AuditCategoryCurrentOfficialOS {
			if match := GetSourceMapMatch(driver, currentOSMap, localVersion); match != nil {
				audit.EvidenceLines = append(audit.EvidenceLines, "Official current OS: "+match.OSName+" (OSID "+match.OSID+"); version="+localVersion)
			}
		} else if rule.category == model.AuditCategoryAlternateOfficialOS {
			if match := GetSourceMapMatch(driver, alternateSourceMap, localVersion); match != nil {
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
func ResolveDriverSourceLabel(driver *model.Driver, history []model.HistoryRecord, alternateSourceMap map[string]model.SourceMapEntry, sourceAudit *model.SourceAudit) string {
	if latest := latestSourceHistory(history, driver, driver.LocalVersion); latest != nil {
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
	if sourceAudit != nil {
		switch sourceAudit.Category {
		case model.AuditCategoryOfflineImage:
			return "Local newer (source offline image integration)"
		case model.AuditCategoryOnlinePackage:
			return "Local newer (source online package installation)"
		case model.AuditCategoryCurrentOfficialOS:
			return "Local newer (source current OS official list)"
		case model.AuditCategoryPreExistingStore:
			return "Local newer (source pre-existing DriverStore package)"
		}
	}
	return ResolveExternalDriverSourceLabel(driver, alternateSourceMap)
}

func latestSourceHistory(history []model.HistoryRecord, driver *model.Driver, localVersion string) *model.HistoryRecord {
	var matches []model.HistoryRecord
	for _, h := range history {
		if h.Result != "Installed" && h.Result != "Verified" {
			continue
		}
		if h.Version != localVersion && h.VerifiedVersion != localVersion {
			continue
		}
		if h.DriverCode != driver.DriverCode && h.DriverName != driver.DriverName {
			continue
		}
		if h.BeforeVersion != "" && h.BeforeVersion == localVersion {
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

func submatch(re *regexp.Regexp, line, name string) string {
	idx := re.SubexpIndex(name)
	m := re.FindStringSubmatch(line)
	if idx < 0 || idx >= len(m) {
		return ""
	}
	return m[idx]
}
