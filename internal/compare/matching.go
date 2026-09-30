package compare

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"lenovo-driver/internal/model"
)

var (
	reHardwarePair   = regexp.MustCompile(`^([0-9A-F]{4})_([0-9A-F]{4})$`)
	reListSplit      = regexp.MustCompile(`,|;`)
	reComponentSplit = regexp.MustCompile(`/|,`)
	reVirtual        = regexp.MustCompile(`(?i)Direct|Virtual`)

	reSoftwareVersioned = regexp.MustCompile(`(?i)Lenovo Fn|Energy Management|X-Rite|AMD Power|Intel.*连接性|Intel.*Connectivity|Connectivity Performance`)
	reAmdPowerProcessor = regexp.MustCompile(`(?i)AMD Power Processor`)
	reLenovoFn          = regexp.MustCompile(`(?i)Lenovo Fn`)
)

type vendorRule struct {
	pattern *regexp.Regexp
	vendor  string
}

var vendorRules = []vendorRule{
	{regexp.MustCompile(`(?i)Intel|英特尔`), "Intel"},
	{regexp.MustCompile(`(?i)Realtek`), "Realtek"},
	{regexp.MustCompile(`(?i)MediaTek|MTK|MT79`), "MediaTek"},
	{regexp.MustCompile(`(?i)AMD|Radeon`), "AMD"},
	{regexp.MustCompile(`(?i)NVIDIA`), "NVIDIA"},
	{regexp.MustCompile(`(?i)Sonix`), "Sonix"},
	{regexp.MustCompile(`(?i)Sunplus`), "Sunplus"},
}

func matchVendor(value string) string {
	for _, rule := range vendorRules {
		if rule.pattern.MatchString(value) {
			return rule.vendor
		}
	}
	return ""
}

// driverMatchRule maps a driver name pattern to the classifier rules for
// driver applicability and device matching. Rules are evaluated in slice
// order; the priority field documents the intended order and is checked
// by a regression test.
type driverMatchRule struct {
	priority    int
	key         string
	driver      *regexp.Regexp
	class       *regexp.Regexp
	device      *regexp.Regexp
	name        *regexp.Regexp
	exclude     *regexp.Regexp
	classOrName bool
	always      bool
}

func (r *driverMatchRule) matches(driverName string) bool {
	return r.driver.MatchString(driverName)
}

func (r *driverMatchRule) namePattern() *regexp.Regexp {
	if r.name != nil {
		return r.name
	}
	return r.device
}

func (r *driverMatchRule) applicable(driver *model.Driver, localDevices []model.Device) bool {
	if r.always {
		return true
	}
	for _, dev := range localDevices {
		if r.exclude != nil && r.exclude.MatchString(dev.Name) {
			continue
		}
		classMatch := r.class != nil && r.class.MatchString(dev.Class)
		deviceMatch := r.device != nil && r.device.MatchString(dev.Name)
		if r.classOrName {
			if classMatch || deviceMatch {
				return true
			}
			continue
		}
		if (r.class == nil || classMatch) && (r.device == nil || deviceMatch) {
			return true
		}
	}
	return false
}

func (r *driverMatchRule) matchingDevices(localDevices []model.Device) []model.Device {
	pattern := r.namePattern()
	if pattern == nil {
		return nil
	}
	var matched []model.Device
	for _, dev := range localDevices {
		if pattern.MatchString(dev.Name) &&
			!reVirtual.MatchString(dev.Name) &&
			!strings.EqualFold(dev.Class, "SoftwareDevice") {
			matched = append(matched, dev)
		}
	}
	return matched
}

var driverMatchRules = []driverMatchRule{
	{priority: 1, key: "Bluetooth 8852AE", driver: regexp.MustCompile(`(?i)BlueTooth.*8852AE`), class: regexp.MustCompile(`(?i)Bluetooth`), device: regexp.MustCompile(`(?i)Realtek`), name: regexp.MustCompile(`(?i)Realtek.*Bluetooth|Bluetooth.*Realtek`)},
	{priority: 2, key: "Camera", driver: regexp.MustCompile(`(?i)Camera`), class: regexp.MustCompile(`(?i)Camera|Image`), device: regexp.MustCompile(`(?i)Camera|Webcam`), name: regexp.MustCompile(`(?i)Camera|Integrated Webcam`), classOrName: true},
	{priority: 3, key: "Card reader", driver: regexp.MustCompile(`(?i)Cardreader`), device: regexp.MustCompile(`(?i)Card Reader|Cardreader|SD|MMC`), name: regexp.MustCompile(`(?i)Card Reader|Cardreader`)},
	{priority: 4, key: "WLAN", driver: regexp.MustCompile(`(?i)Wlan`), class: regexp.MustCompile(`(?i)Net`), device: regexp.MustCompile(`(?i)Intel|Realtek|MediaTek|MTK|Wireless|Wi-Fi|WLAN`), name: regexp.MustCompile(`(?i)Wireless-AC|Wireless LAN|Wi-Fi|WLAN|AX20|8852AE|8822CE|MT7921|MediaTek.*Wi`), exclude: reVirtual},
	{priority: 5, key: "Bluetooth", driver: regexp.MustCompile(`(?i)BlueTooth`), class: regexp.MustCompile(`(?i)Bluetooth`), device: regexp.MustCompile(`(?i)Intel|Realtek|MediaTek|MTK|Bluetooth`), name: regexp.MustCompile(`(?i)Bluetooth`)},
	{priority: 6, key: "Realtek Audio", driver: regexp.MustCompile(`(?i)Realtek Audio`), class: regexp.MustCompile(`(?i)MEDIA|AudioEndpoint`), device: regexp.MustCompile(`(?i)Realtek|Audio`), name: regexp.MustCompile(`(?i)Realtek.*Audio|High Definition Audio`), classOrName: true},
	{priority: 7, key: "AMD VGA", driver: regexp.MustCompile(`(?i)AMD VGA`), class: regexp.MustCompile(`(?i)Display`), device: regexp.MustCompile(`(?i)AMD|Radeon`), name: regexp.MustCompile(`(?i)AMD Radeon|Radeon.*Graphics|AMD.*Display`)},
	{priority: 8, key: "NVIDIA VGA", driver: regexp.MustCompile(`(?i)NVIDIA VGA`), class: regexp.MustCompile(`(?i)Display`), device: regexp.MustCompile(`(?i)NVIDIA`), name: regexp.MustCompile(`(?i)NVIDIA GeForce|NVIDIA.*Display`)},
	{priority: 9, key: "Realtek LAN", driver: regexp.MustCompile(`(?i)Realtek Lan`), class: regexp.MustCompile(`(?i)Net`), device: regexp.MustCompile(`(?i)Realtek`), name: regexp.MustCompile(`(?i)Realtek.*Ethernet|Realtek.*PCIe|Realtek.*Gbe`)},
	{priority: 10, key: "Serial-IO", driver: regexp.MustCompile(`(?i)Serial-IO`), device: regexp.MustCompile(`(?i)Serial IO|Serial-IO|I2C|AMD.*IO`), name: regexp.MustCompile(`(?i)Serial IO|Serial-IO|AMD.*IO`)},
	{priority: 11, key: "AMD Power", driver: regexp.MustCompile(`(?i)AMD Power`), device: regexp.MustCompile(`(?i)AMD`), name: regexp.MustCompile(`(?i)AMD Power|Power Processor`)},
	{priority: 12, key: "Lenovo software", driver: regexp.MustCompile(`(?i)Lenovo Energy|Lenovo Fn|X-Rite`), always: true, name: regexp.MustCompile(`(?i)Lenovo Fn|LHK2019|Lenovo Energy|Lenovo Utility|X-Rite`)},
}

// TestHardwareMatch mirrors Test-HardwareMatch.
func TestHardwareMatch(remoteIDs, localPnpID, localDeviceID string) bool {
	if remoteIDs == "" {
		return false
	}
	localRaw := strings.ToUpper(localPnpID + " " + localDeviceID)
	localCompact := stripNonAlphaNumeric(localRaw)
	for _, raw := range reListSplit.Split(remoteIDs, -1) {
		token := strings.ToUpper(strings.TrimSpace(raw))
		if token == "" {
			continue
		}
		if m := reHardwarePair.FindStringSubmatch(token); len(m) == 3 {
			if hardwarePairMatches(localRaw, m[1], m[2]) {
				return true
			}
		}
		compact := stripNonAlphaNumeric(token)
		if len(compact) >= 4 && (strings.Contains(localRaw, token) || strings.Contains(localCompact, compact)) {
			return true
		}
	}
	return false
}

func hardwarePairMatches(localRaw, vendorID, deviceID string) bool {
	for _, separator := range []string{"&", "_", "/"} {
		for _, vendorPrefix := range []string{"VEN_", "VEN"} {
			for _, devicePrefix := range []string{"DEV_", "DEV"} {
				if strings.Contains(localRaw, vendorPrefix+vendorID+separator+devicePrefix+deviceID) {
					return true
				}
			}
		}
	}
	return false
}

func stripNonAlphaNumeric(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// TestDriverApplicable mirrors Test-DriverApplicable.
func TestDriverApplicable(driver *model.Driver, localDevices []model.Device) bool {
	if driver == nil {
		return false
	}
	if driver.HardwareID != "" {
		for _, dev := range localDevices {
			if TestHardwareMatch(driver.HardwareID, dev.PnpDeviceID, dev.DeviceID) {
				return true
			}
		}
		return false
	}

	for _, rule := range driverMatchRules {
		if !rule.matches(driver.DriverName) {
			continue
		}
		return rule.applicable(driver, localDevices)
	}
	return false
}

// DiagnoseDriverNonMatch explains why a driver is Not applicable. It names the
// evidence actually checked (hardware id, matching rule, or the absence of a
// rule) so a plan reader can tell an honest hardware mismatch apart from a
// missing rule instead of collapsing every case into "hardware not detected".
func DiagnoseDriverNonMatch(driver *model.Driver, localDevices []model.Device) string {
	if driver == nil {
		return "driver is nil"
	}
	if driver.HardwareID != "" {
		return hardwareIDNonMatchReason(driver.HardwareID)
	}
	for _, rule := range driverMatchRules {
		if !rule.matches(driver.DriverName) {
			continue
		}
		return fmt.Sprintf("driver name matched rule %q but no local device satisfied it", rule.key)
	}
	return fmt.Sprintf("no matching rule for driver name %q", driver.DriverName)
}

// hardwareIDNonMatchReason summarizes an unmatched hardware id list without
// dumping every vendor/device pair into the plan. The full list already lives
// in the source driver row, so the note keeps the first two ids plus the total
// count for a readable, still-honest diagnosis.
func hardwareIDNonMatchReason(raw string) string {
	var ids []string
	for _, token := range reListSplit.Split(raw, -1) {
		if token = strings.TrimSpace(token); token != "" {
			ids = append(ids, token)
		}
	}
	switch len(ids) {
	case 0:
		return "hardware id matched no local PnP/device ID"
	case 1:
		return fmt.Sprintf("hardware id %s matched no local PnP/device ID", ids[0])
	case 2:
		return fmt.Sprintf("hardware ids %s, %s matched no local PnP/device ID", ids[0], ids[1])
	default:
		return fmt.Sprintf("hardware ids %s, %s, ... (%d ids) matched no local PnP/device ID", ids[0], ids[1], len(ids))
	}
}

// GetDeviceVendor mirrors Get-DeviceVendor.
func GetDeviceVendor(names []string) string {
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
			if v := ParseVersionString(part); v != nil {
				return v
			}
		}
	}
	return nil
}

// CompareDriverStatus mirrors Compare-DriverStatus.
func CompareDriverStatus(remote, local, vendor string) model.CompareStatus {
	if local == "" {
		return model.StatusNotInstalled
	}
	if remote == "" {
		return model.StatusUnknown
	}
	if local == model.LocalVersionProvisioned {
		return model.StatusUpToDate
	}
	remoteVersion := GetMatchingRemoteComponent(remote, vendor)
	if remoteVersion == nil {
		remoteVersion = ParseVersionString(remote)
	}
	localVersion := ParseVersionString(local)
	if remoteVersion != nil && localVersion != nil {
		if c := localVersion.Compare(remoteVersion); c > 0 {
			return model.StatusLocalNewer
		} else if c == 0 {
			return model.StatusUpToDate
		}
		return model.StatusUpdate
	}
	return model.StatusUnknown
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

// ResolveInstalledSoftwareVersion mirrors Resolve-InstalledSoftwareVersion.
func ResolveInstalledSoftwareVersion(driverName string, snapshot *model.SoftwareSnapshot) string {
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
			if TestHardwareMatch(driver.HardwareID, dev.PnpDeviceID, dev.DeviceID) {
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

// TestSoftwareVersionedDriver mirrors Test-SoftwareVersionedDriver.
func TestSoftwareVersionedDriver(driverName string) bool {
	return reSoftwareVersioned.MatchString(driverName)
}

// deviceVersionFrom collapses the matched devices' measured driver versions into
// the single string the compare step consumes. A device with no version, or with
// several distinct ones, is reported as the empty string or the joined list
// respectively; in both cases the caller decides what that means.
func deviceVersionFrom(matchedDevices []model.Device, driverVersions []string) string {
	if len(matchedDevices) == 0 {
		return ""
	}
	seen := map[string]bool{}
	var unique []string
	for _, v := range driverVersions {
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		unique = append(unique, v)
	}
	sort.Strings(unique)
	switch len(unique) {
	case 0:
		return ""
	case 1:
		return unique[0]
	default:
		return strings.Join(unique, ", ")
	}
}

// ResolveLocalDriverVersion mirrors Resolve-LocalDriverVersion.
func ResolveLocalDriverVersion(driver *model.Driver, matchedDevices []model.Device, driverVersions []string, snapshot *model.SoftwareSnapshot) (localVersion, localVendor string) {
	softwareVersion := ResolveInstalledSoftwareVersion(driver.DriverName, snapshot)
	if TestSoftwareVersionedDriver(driver.DriverName) {
		localVendor = GetDeviceVendor(deviceNames(matchedDevices))
		localVersion = softwareVersion
		// Software-versioned drivers normally resolve through InstalledApps,
		// but "not present in InstalledApps" is absence of evidence, not evidence
		// of absence. The device still carries a version read straight from PnP,
		// which is a measured fact and must outrank a failed lookup: returning
		// "" here made CompareDriverStatus report Not installed for a device that
		// already runs a vendor package, and Not installed is the one status the
		// automatic set acts on. On 82JQ that put a redundant reinstall of
		// ACPI\VPC2004 (Lenovo Energy Management, oem90.inf, 15.11.29.65) in front
		// of the user.
		if localVersion == "" {
			localVersion = deviceVersionFrom(matchedDevices, driverVersions)
		}
		return localVersion, localVendor
	}
	if len(matchedDevices) > 0 {
		localVendor = GetDeviceVendor(deviceNames(matchedDevices))
		localVersion = deviceVersionFrom(matchedDevices, driverVersions)
	}
	if localVersion == "" {
		localVersion = softwareVersion
	}
	return localVersion, localVendor
}

func deviceNames(devices []model.Device) []string {
	names := make([]string, 0, len(devices))
	for _, dev := range devices {
		names = append(names, dev.Name)
	}
	return names
}
