package compare

// Version resolution for drivers whose "version" is not the PnP device version:
// vendor-component matching, software-versioned packages resolved from the
// installed-application snapshot, and the local-version pipeline the compare and
// recheck passes share. Split from hardware matching because these answer a
// different question: not "does this package apply" but "what is installed".

import (
	"regexp"
	"strings"

	"lenovo-driver/internal/model"
)

// deviceVendorFor mirrors Get-DeviceVendor.
func deviceVendorFor(names []string) string {
	for _, name := range names {
		if vendor := matchVendor(name); vendor != "" {
			return vendor
		}
	}
	return ""
}

// GetMatchingRemoteComponent mirrors Get-MatchingRemoteComponent.
func GetMatchingRemoteComponent(remote, vendor string) *Version {
	if vendor == "" {
		return nil
	}
	for _, part := range reComponentSplit.Split(remote, -1) {
		if matchVendor(part) == vendor {
			if v := parseVersionString(part); v != nil {
				return v
			}
		}
	}
	return nil
}

// CompareDriverStatus decides the status from the official version and the
// local evidence. The four evidence kinds map to four different conclusions, and
// the mapping is exhaustive on purpose: a kind added later fails to compile here
// rather than falling into a default branch that silently means "act on it".
//
// "Not installed" is reserved for measured absence — a matched device that
// carries no bound driver version. An unobserved driver (no device matched at
// all) is NOT the same claim: nothing was measured, so nothing may be concluded.
// Treating it as absence put drivers with no local evidence at all into the
// automatic install set, which is the one set the tool acts on unattended.
func CompareDriverStatus(remote string, local model.LocalEvidence, vendor string) model.CompareStatus {
	switch local.Kind() {
	case model.LocalEvidenceProvisioned:
		// Windows provisioned the package but that proves neither that the driver
		// is installed nor that it is absent: the 82JQ drill found
		// AMD.Power.Processor.ppkg under Provisioning\Results with no
		// application, no uninstall entry and no matching device. Reporting
		// UpToDate handed the automatic set a fact-grade claim the machine does
		// not support. Neither alternative is better: NotInstalled would assert
		// absence while the system holds a package for it. A status the evidence
		// cannot settle must stay undetermined, which keeps it out of the
		// automatic set under the fact-only rule.
		return model.StatusUnknown
	case model.LocalEvidenceConflicted:
		// Two measured versions disagree. The disagreement is a fact, but no
		// direction follows from it, so the comparison stays undecided. The plan
		// names the conflict; guessing which device is authoritative would be
		// inventing a fact.
		return model.StatusUnknown
	case model.LocalEvidenceUnobserved:
		// Nothing on this machine was measured for this driver.
		return model.StatusUnknown
	case model.LocalEvidenceAbsent:
		if remote == "" {
			return model.StatusUnknown
		}
		return model.StatusNotInstalled
	case model.LocalEvidenceVersion:
		if remote == "" {
			return model.StatusUnknown
		}
	}
	remoteVersion := ResolveComparableVersion(remote, vendor)
	if remoteVersion == nil {
		remoteVersion = parseVersionString(remote)
	}
	localVersion := ResolveComparableVersion(local.MeasuredVersion(), vendor)
	switch Order(localVersion, remoteVersion) {
	case OrderUndecided:
		return model.StatusUnknown
	case orderGreater:
		return model.StatusLocalNewer
	case OrderEqual:
		return model.StatusUpToDate
	default:
		return model.StatusUpdate
	}
}

type softwareRule struct {
	driver *regexp.Regexp
	app    *regexp.Regexp
}

var softwareRules = []softwareRule{
	{regexp.MustCompile(`(?i)Lenovo Fn`), regexp.MustCompile(`(?i)Lenovo.*Fn|Lenovo.*Hotkey|Lenovo Utility|Hotkeys`)},
	{regexp.MustCompile(`(?i)Energy Management`), regexp.MustCompile(`(?i)Lenovo Energy|Lenovo.*Power|Energy Management`)},
	{regexp.MustCompile(`(?i)X-Rite`), regexp.MustCompile(`(?i)X-Rite|Color Assistant`)},
	{regexp.MustCompile(`(?i)AMD Power Processor`), regexp.MustCompile(`(?i)AMD Power Processor|AMD Power`)},
	{regexp.MustCompile(`(?i)Intel.*连接性|Intel.*Connectivity|Connectivity Performance`), regexp.MustCompile(`(?i)Intel.*连接性|Intel.*Connectivity|Connectivity Performance|ICPS`)},
}

// resolveInstalledSoftwareVersion mirrors Resolve-InstalledSoftwareVersion.
func resolveInstalledSoftwareVersion(driverName string, snapshot *model.SoftwareSnapshot) string {
	if snapshot == nil {
		return ""
	}
	for _, rule := range softwareRules {
		if rule.driver.MatchString(driverName) {
			for _, app := range snapshot.InstalledApps {
				if rule.app.MatchString(app.DisplayName) && app.DisplayVersion != "" {
					return app.DisplayVersion
				}
			}
		}
	}
	if reAmdPowerProcessor.MatchString(driverName) && snapshot.ProvisionedAmdPower == model.LocalVersionProvisioned {
		return model.LocalVersionProvisioned
	}
	if reLenovoFn.MatchString(driverName) && snapshot.LenovoFnServiceVersion != "" {
		return snapshot.LenovoFnServiceVersion
	}
	return ""
}

// GetMatchingLocalDevices mirrors Get-MatchingLocalDevices.
func GetMatchingLocalDevices(driver *model.Driver, localDevices []model.Device) []model.Device {
	var matched []model.Device
	if driver != nil && driver.HardwareID != "" {
		for _, dev := range localDevices {
			if hardwareMatches(driver.HardwareID, dev.PnpDeviceID, dev.DeviceID) {
				matched = append(matched, dev)
			}
		}
	}
	if len(matched) == 0 && driver != nil {
		for _, rule := range driverMatchRules {
			if !rule.matches(driver.DriverName) {
				continue
			}
			matched = rule.matchingDevices(localDevices)
			if len(matched) > 0 {
				return matched
			}
		}
		head := defaultNamePattern(driver.DriverName)
		for _, dev := range localDevices {
			if strings.Contains(strings.ToLower(dev.Name), head) &&
				!reVirtual.MatchString(dev.Name) &&
				!strings.EqualFold(dev.Class, "SoftwareDevice") {
				matched = append(matched, dev)
			}
		}
	}
	return matched
}

func defaultNamePattern(driverName string) string {
	head := driverName
	if idx := strings.Index(head, " "); idx >= 0 {
		head = head[:idx]
	}
	return strings.ToLower(head)
}

// isSoftwareVersionedDriver mirrors Test-SoftwareVersionedDriver.
func isSoftwareVersionedDriver(driverName string) bool {
	return reSoftwareVersioned.MatchString(driverName)
}

// deviceEvidenceFrom classifies what the matched devices reported. The three
// outcomes used to be one string: "" for both "no device matched" and "the
// devices report no version", and "a, b" for a disagreement. Those differ in
// what the operator may conclude, so they are distinct kinds here.
//
// A disagreement is evidence, not an unreadable value: two devices this driver
// applies to are running different versions, which is something to report. It is
// only the ORDER that cannot be decided from it, so it never yields a direction.
func deviceEvidenceFrom(matchedDevices []model.Device, driverVersions []string) model.LocalEvidence {
	if len(matchedDevices) == 0 {
		return model.NotObserved()
	}
	// The constructor collapses zero versions to Unobserved and one to a single
	// measurement, so "no device carries a driver" has to be said explicitly
	// here: the devices were enumerated and none reported a version.
	if evidence := model.ObservedConflict(driverVersions); evidence.Kind() != model.LocalEvidenceUnobserved {
		return evidence
	}
	return model.ObservedAbsence()
}

// ResolveLocalDriverVersion resolves the local evidence and the vendor label for
// one driver. The returned kind says whether the machine was observed at all and
// whether the observation settled on a single version, so callers never have to
// read absence of a string as evidence of absence.
func ResolveLocalDriverVersion(driver *model.Driver, matchedDevices []model.Device, driverVersions []string, snapshot *model.SoftwareSnapshot) (local model.LocalEvidence, localVendor string) {
	softwareVersion := resolveInstalledSoftwareVersion(driver.DriverName, snapshot)
	deviceEvidence := deviceEvidenceFrom(matchedDevices, driverVersions)
	localVendor = deviceVendorFor(deviceNames(matchedDevices))

	if isSoftwareVersionedDriver(driver.DriverName) {
		// Software-versioned drivers normally resolve through InstalledApps, but
		// "not present in InstalledApps" is absence of evidence, not evidence of
		// absence. The device still carries a version read straight from PnP,
		// which is a measured fact and must outrank a failed lookup: returning
		// "" here made CompareDriverStatus report Not installed for a device that
		// already runs a vendor package, and Not installed is the one status the
		// automatic set acts on. On 82JQ that put a redundant reinstall of
		// ACPI\VPC2004 (Lenovo Energy Management, oem90.inf, 15.11.29.65) in front
		// of the user.
		if softwareVersion == model.LocalVersionProvisioned {
			return model.ObservedProvisioning(), localVendor
		}
		if softwareVersion != "" {
			return model.ObservedVersion(softwareVersion), localVendor
		}
		return deviceEvidence, localVendor
	}

	// A device-measured version outranks the software list for a hardware
	// driver, but a disagreement between devices is not resolvable by preferring
	// one of them: report the conflict.
	// Any device observation outranks the software list: a measured version, a
	// measured absence, and a measured disagreement are all facts about this
	// machine, while the software list is a different instrument.
	if deviceEvidence.Sought() {
		return deviceEvidence, localVendor
	}
	if softwareVersion == model.LocalVersionProvisioned {
		return model.ObservedProvisioning(), localVendor
	}
	if softwareVersion != "" {
		return model.ObservedVersion(softwareVersion), localVendor
	}
	return deviceEvidence, localVendor
}

func deviceNames(devices []model.Device) []string {
	names := make([]string, 0, len(devices))
	for _, dev := range devices {
		names = append(names, dev.Name)
	}
	return names
}
