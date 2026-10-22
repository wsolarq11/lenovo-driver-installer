package audit

import (
	"regexp"
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

// versionInCandidates reports whether a version string is one of the versions
// this driver has on this machine.
func versionInCandidates(version string, candidates []string) bool {
	if version == "" {
		return false
	}
	for _, candidate := range candidates {
		if candidate == version {
			return true
		}
	}
	return false
}

// sourceMapMatchAny returns the first match across every candidate version, so a
// conflicted driver can still be attributed to an official list through either
// of its measured versions.
func sourceMapMatchAny(driver *model.Driver, sourceMap map[string]model.SourceMapEntry, candidates []string) *model.SourceMapEntry {
	for _, candidate := range candidates {
		if match := sourceMapMatch(driver, sourceMap, candidate); match != nil {
			return match
		}
	}
	return nil
}

// sourceMapMatch mirrors Get-SourceMapMatch.
func sourceMapMatch(driver *model.Driver, sourceMap map[string]model.SourceMapEntry, localVersion string) *model.SourceMapEntry {
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

// resolveExternalDriverSourceLabel mirrors Resolve-ExternalDriverSourceLabel.
// candidates are the driver's measured local versions, not fields of
// *model.Driver; taking the set keeps a disagreeing driver attributable through
// either version instead of matching a joined string that equals no key.
func resolveExternalDriverSourceLabel(driver *model.Driver, alternateSourceMap map[string]model.SourceMapEntry, candidates []string) string {
	if match := sourceMapMatchAny(driver, alternateSourceMap, candidates); match != nil {
		if match.OSName != "" {
			return "Local newer (version matches " + match.OSName + " list)"
		}
		return "Local newer (version matches OSID " + match.OSID + " list)"
	}
	return "Local newer (source external/unknown)"
}

type sectionStartRule struct {
	re            *regexp.Regexp
	updateCurrent bool
}

var sectionStartRules = []sectionStartRule{
	{re: reSectionImport},
	{re: reSectionDriverInstall},
	{re: reSectionDeviceInstall},
	{re: reSectionStoDvsImport, updateCurrent: true},
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
		if s.PackageDir == "" {
			s.PackageDir = strings.TrimSpace(submatch(rePublished, line, "published"))
		}
		if s.OemInfName == "" {
			s.OemInfName = strings.TrimSpace(submatch(rePublished, line, "oem"))
		}
	}},
	{re: reCreatedPackage, set: func(s *model.ImportRecord, line string) {
		if s.PackageDir == "" {
			s.PackageDir = strings.TrimSpace(submatch(reCreatedPackage, line, "dir"))
		}
	}},
	{re: reCreatedInf, set: func(s *model.ImportRecord, line string) {
		if s.OemInfName == "" {
			s.OemInfName = strings.TrimSpace(submatch(reCreatedInf, line, "oem"))
		}
	}},
	{re: reRegistered, set: func(s *model.ImportRecord, line string) {
		if s.PackageDir == "" {
			s.PackageDir = strings.TrimSpace(submatch(reRegistered, line, "dir"))
		}
		if s.OemInfName == "" {
			s.OemInfName = strings.TrimSpace(submatch(reRegistered, line, "oem"))
		}
	}},
	{re: reRegisterPkg, set: func(s *model.ImportRecord, line string) {
		if s.PackageDir == "" {
			s.PackageDir = strings.TrimSpace(submatch(reRegisterPkg, line, "path"))
		}
	}},
	{re: reCoreImport, set: func(s *model.ImportRecord, line string) {
		if s.PackageDir == "" {
			s.PackageDir = strings.TrimSpace(submatch(reCoreImport, line, "pkg"))
		}
	}},
	{re: reStoreFilename, set: func(s *model.ImportRecord, line string) {
		if s.PackageDir == "" {
			s.PackageDir = strings.TrimSpace(submatch(reStoreFilename, line, "path"))
		}
	}},
}

// ConvertFromImportLogText mirrors ConvertFrom-ImportLogText.
func ConvertFromImportLogText(lines []string, logPath string) []model.ImportRecord {
	var rows []model.ImportRecord
	var section *model.ImportRecord
	for lineno, line := range lines {
		lineNum := lineno + 1
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || reExitOpen.MatchString(trimmed) {
			continue
		}
		isNewSection := false
		for _, rule := range sectionStartRules {
			if rule.re.MatchString(trimmed) {
				if rule.updateCurrent && section != nil {
					// sto/dvs lines inside a section update the current
					// section's source path instead of starting a new one.
					if section.SourcePath == "" {
						section.SourcePath = strings.TrimSpace(submatch(rule.re, trimmed, "source"))
					}
					isNewSection = true
					break
				}
				if section != nil {
					rows = append(rows, *section)
				}
				section = &model.ImportRecord{
					SourcePath: strings.TrimSpace(submatch(rule.re, trimmed, "source")),
					LogPath:    logPath,
					LineNumber: lineNum,
				}
				section.InfName = pathutil.Base(section.SourcePath)
				isNewSection = true
				break
			}
		}
		if isNewSection {
			continue
		}
		if section == nil {
			continue
		}
		if reSectionEnd.MatchString(trimmed) {
			rows = append(rows, *section)
			section = nil
			continue
		}
		for _, rule := range fieldExtractRules {
			if rule.re.MatchString(trimmed) {
				rule.set(section, trimmed)
				break
			}
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

func submatch(re *regexp.Regexp, line, name string) string {
	idx := re.SubexpIndex(name)
	m := re.FindStringSubmatch(line)
	if idx < 0 || idx >= len(m) {
		return ""
	}
	return m[idx]
}
