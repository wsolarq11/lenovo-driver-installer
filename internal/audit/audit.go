package audit

import (
	"regexp"
	"sort"
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

// GetSourceMapMatch mirrors Get-SourceMapMatch.
func GetSourceMapMatch(driver *model.Driver, sourceMap map[string]model.SourceMapEntry, localVersion string) *model.SourceMapEntry {
	if len(sourceMap) == 0 || localVersion == "" {
		return nil
	}
	for _, versionKey := range compare.GetVersionMatchKeys(localVersion) {
		nameKey := strings.TrimSpace(driver.DriverName) + "|" + versionKey
		codeKey := driver.DriverCode + "|" + versionKey
		if match, ok := sourceMap[nameKey]; ok {
			entry := match
			return &entry
		}
		if match, ok := sourceMap[codeKey]; ok {
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
		if reSectionImport.MatchString(line) && !reExitOpen.MatchString(line) {
			section = newSection(strings.TrimSpace(submatch(reSectionImport, line, "source")), "Import", lineNo)
			started = true
		} else if reSectionDriverInstall.MatchString(line) && !reExitOpen.MatchString(line) {
			section = newSection(strings.TrimSpace(submatch(reSectionDriverInstall, line, "source")), "DriverInstall", lineNo)
			started = true
		} else if reSectionDeviceInstall.MatchString(line) && !reExitOpen.MatchString(line) {
			section = newSection(strings.TrimSpace(submatch(reSectionDeviceInstall, line, "source")), "DeviceInstall", lineNo)
			started = true
		} else if reSectionStoDvsImport.MatchString(line) && !reExitOpen.MatchString(line) {
			importSource := strings.TrimSpace(submatch(reSectionStoDvsImport, line, "source"))
			if section != nil && (section.HeaderKind == "DriverInstall" || section.HeaderKind == "DeviceInstall") {
				section.SourcePath = importSource
				section.InfName = pathutil.Base(importSource)
				section.LineNumber = lineNo
			} else {
				section = newSection(importSource, "Import", lineNo)
			}
			started = true
		}
		if started {
			continue
		}
		if section == nil {
			continue
		}

		switch {
		case reCommand.MatchString(line):
			section.Command = strings.TrimSpace(submatch(reCommand, line, "cmd"))
		case reVersion.MatchString(line):
			section.Version = strings.TrimSpace(submatch(reVersion, line, "ver"))
		case reTargetPath.MatchString(line):
			if section.PackageDir == "" {
				section.PackageDir = strings.TrimSpace(submatch(reTargetPath, line, "path"))
			}
		case rePublished.MatchString(line):
			published := strings.TrimSpace(submatch(rePublished, line, "published"))
			if section.PackageDir == "" && strings.Contains(published, `\`) {
				section.PackageDir = pathutil.Parent(published)
			}
			if section.OemInfName == "" {
				section.OemInfName = pathutil.Base(strings.TrimSpace(submatch(rePublished, line, "oem")))
			}
		case reCreatedPackage.MatchString(line):
			if section.PackageDir == "" {
				section.PackageDir = strings.TrimSpace(submatch(reCreatedPackage, line, "dir"))
			}
		case reCreatedInf.MatchString(line):
			if section.OemInfName == "" {
				section.OemInfName = pathutil.Base(strings.TrimSpace(submatch(reCreatedInf, line, "oem")))
			}
		case reRegistered.MatchString(line):
			if section.PackageDir == "" {
				section.PackageDir = strings.TrimSpace(submatch(reRegistered, line, "dir"))
			}
			if section.OemInfName == "" {
				section.OemInfName = pathutil.Base(strings.TrimSpace(submatch(reRegistered, line, "oem")))
			}
		case reRegisterPkg.MatchString(line):
			if section.PackageDir == "" {
				section.PackageDir = pathutil.Parent(strings.TrimSpace(submatch(reRegisterPkg, line, "path")))
			}
		case reCoreImport.MatchString(line) && !reExitOpen.MatchString(line):
			if section.PackageDir == "" {
				section.PackageDir = strings.TrimSpace(submatch(reCoreImport, line, "pkg"))
			}
		case reStoreFilename.MatchString(line):
			path := strings.TrimSpace(submatch(reStoreFilename, line, "path"))
			if section.PackageDir == "" {
				section.PackageDir = pathutil.Parent(path)
			}
			if inf := pathutil.Base(path); strings.HasSuffix(strings.ToLower(inf), ".inf") {
				section.InfName = inf
			}
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

// ResolveDriverSourceEvidence mirrors Resolve-DriverSourceEvidence.
func ResolveDriverSourceEvidence(driver *model.Driver, deviceEvidence []model.Device, history []model.HistoryRecord, alternateSourceMap, currentOSMap map[string]model.SourceMapEntry) model.SourceAudit {
	localVersion := driver.LocalVersion
	category := "Unknown"
	summary := "No local version evidence"
	var evidenceLines []string

	if localVersion != "" {
		latestHistory := latestSourceHistory(history, driver, localVersion)
		if latestHistory != nil {
			category = "Install history"
			if latestHistory.OSID == driver.OSID {
				summary = "Installed by this script from the current OS source"
			} else if latestHistory.OSName != "" {
				summary = "Installed by this script from " + latestHistory.OSName
			} else {
				summary = "Installed by this script from OSID " + latestHistory.OSID
			}
			evidenceLines = append(evidenceLines, "History: time="+latestHistory.Timestamp+"; version="+latestHistory.Version+"; verified="+latestHistory.VerifiedVersion+"; before="+latestHistory.BeforeVersion+"; result="+latestHistory.Result)
		}

		for _, device := range deviceEvidence {
			if device.DriverVersion != localVersion {
				continue
			}
			evidenceLines = append(evidenceLines, "Device: "+device.Name+"; version="+device.DriverVersion+"; date="+device.DriverDate+"; INF="+device.InfName+"; provider="+device.ProviderName+"; install-date="+device.InstallDate)
			if device.PackageDir != "" {
				evidenceLines = append(evidenceLines, "DriverStore package: "+device.PackageDir+"; created="+device.PackageCreationTime+"; DriverVer="+device.PackageDriverVer)
			}
			if device.ImportSource != "" {
				if category == "Unknown" {
					if device.ImportKind == "Offline" {
						category = "Offline image integration"
					} else {
						category = "Online package installation"
					}
					summary = category + " source: " + device.ImportSource
				}
				evidenceLines = append(evidenceLines, "Import: source="+device.ImportSource+"; command="+device.ImportCommand+"; log="+device.ImportLog+":"+itoa(device.ImportLine))
			} else if category == "Unknown" {
				category = "Pre-existing DriverStore package"
				summary = "Active driver came from a pre-existing DriverStore package, not current install history"
			}
		}

		if category == "Unknown" {
			if match := GetSourceMapMatch(driver, currentOSMap, localVersion); match != nil {
				category = "Current official OS"
				summary = "Matches the current OS official Lenovo list"
				evidenceLines = append(evidenceLines, "Official current OS: "+match.OSName+" (OSID "+match.OSID+"); version="+localVersion)
			}
		}

		if category == "Unknown" {
			if match := GetSourceMapMatch(driver, alternateSourceMap, localVersion); match != nil {
				category = "Alternate official OS"
				summary = "Matches another OS official Lenovo list"
				evidenceLines = append(evidenceLines, "Official alternate OS: "+match.OSName+" (OSID "+match.OSID+"); version="+localVersion)
			} else {
				summary = "No install history, DriverStore import evidence, or official Lenovo version match"
			}
		}
	}

	return model.SourceAudit{Category: category, Summary: summary, EvidenceLines: evidenceLines}
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
		case "Offline image integration":
			return "Local newer (source offline image integration)"
		case "Online package installation":
			return "Local newer (source online package installation)"
		case "Current official OS":
			return "Local newer (source current OS official list)"
		case "Pre-existing DriverStore package":
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

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
