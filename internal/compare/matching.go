package compare

import (
	"regexp"
	"sort"
	"strings"

	"lenovo-driver/internal/model"
)

var reHardwarePair = regexp.MustCompile(`^([0-9A-F]{4})_([0-9A-F]{4})$`)

// GetNamePatterns mirrors Get-NamePatterns.
func GetNamePatterns(driverName string) []string {
	var patterns []string
	switch {
	case strings.Contains(driverName, "Realtek Audio"):
		patterns = append(patterns, `Realtek.*Audio|High Definition Audio`)
	case strings.Contains(driverName, "AMD VGA"):
		patterns = append(patterns, `AMD Radeon|Radeon.*Graphics|AMD.*Display`)
	case strings.Contains(driverName, "NVIDIA VGA"):
		patterns = append(patterns, `NVIDIA GeForce|NVIDIA.*Display`)
	case strings.Contains(driverName, "Realtek Lan"):
		patterns = append(patterns, `Realtek.*Ethernet|Realtek.*PCIe|Realtek.*Gbe`)
	case strings.Contains(driverName, "Wlan"):
		patterns = append(patterns, `Wireless-AC|Wireless LAN|Wi-Fi|WLAN|AX20|8852AE|8822CE|MT7921|MediaTek.*Wi`)
	case strings.Contains(driverName, "BlueTooth"):
		patterns = append(patterns, `Bluetooth`)
	case strings.Contains(driverName, "Cardreader"):
		patterns = append(patterns, `Card Reader|Cardreader`)
	case strings.Contains(driverName, "Camera"):
		patterns = append(patterns, `Camera|Integrated Webcam`)
	case strings.Contains(driverName, "Serial-IO"):
		patterns = append(patterns, `Serial IO|Serial-IO|AMD.*IO`)
	case strings.Contains(driverName, "AMD Power"):
		patterns = append(patterns, `AMD Power|Power Processor`)
	case strings.Contains(driverName, "Lenovo Fn"):
		patterns = append(patterns, `Lenovo Fn|LHK2019`)
	case strings.Contains(driverName, "Lenovo Energy"):
		patterns = append(patterns, `Lenovo Energy|Lenovo Utility`)
	case regexp.MustCompile(`(?i)Intel.*连接性|Intel.*Connectivity|Connectivity Performance`).MatchString(driverName):
		patterns = append(patterns, `Intel.*连接性|Intel.*Connectivity|Connectivity Performance`)
	}
	if len(patterns) == 0 {
		head := driverName
		if idx := strings.Index(head, " "); idx >= 0 {
			head = head[:idx]
		}
		patterns = append(patterns, regexp.QuoteMeta(head))
	}
	return patterns
}

// TestHardwareMatch mirrors Test-HardwareMatch.
func TestHardwareMatch(remoteIDs, localPnpID, localDeviceID string) bool {
	if remoteIDs == "" {
		return false
	}
	localRaw := strings.ToUpper(localPnpID + " " + localDeviceID)
	localCompact := stripNonAlphaNumeric(localRaw)
	for _, raw := range regexp.MustCompile(`,|;`).Split(remoteIDs, -1) {
		token := strings.ToUpper(strings.TrimSpace(raw))
		if token == "" {
			continue
		}
		if m := reHardwarePair.FindStringSubmatch(token); len(m) == 3 {
			pattern := `VEN[_]?` + m[1] + `[&_/]DEV[_]?` + m[2]
			if regexp.MustCompile(pattern).MatchString(localRaw) {
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

	name := driver.DriverName
	switch {
	case regexp.MustCompile(`(?i)Camera`).MatchString(name):
		for _, dev := range localDevices {
			if (strings.EqualFold(dev.Class, "Camera") || strings.EqualFold(dev.Class, "Image")) ||
				regexp.MustCompile(`(?i)Camera|Webcam`).MatchString(dev.Name) {
				return true
			}
		}
		return false
	case regexp.MustCompile(`(?i)Cardreader`).MatchString(name):
		for _, dev := range localDevices {
			if regexp.MustCompile(`(?i)Card Reader|Cardreader|SD|MMC`).MatchString(dev.Name) {
				return true
			}
		}
		return false
	case regexp.MustCompile(`(?i)Wlan`).MatchString(name):
		for _, dev := range localDevices {
			if strings.EqualFold(dev.Class, "Net") &&
				!regexp.MustCompile(`(?i)Direct|Virtual`).MatchString(dev.Name) &&
				regexp.MustCompile(`(?i)Intel|Realtek|MediaTek|MTK|Wireless|Wi-Fi|WLAN`).MatchString(dev.Name) {
				return true
			}
		}
		return false
	case regexp.MustCompile(`(?i)BlueTooth`).MatchString(name):
		re := regexp.MustCompile(`(?i)Intel|Realtek|MediaTek|MTK|Bluetooth`)
		if regexp.MustCompile(`(?i)8852AE`).MatchString(name) {
			re = regexp.MustCompile(`(?i)Realtek`)
		}
		for _, dev := range localDevices {
			if strings.EqualFold(dev.Class, "Bluetooth") && re.MatchString(dev.Name) {
				return true
			}
		}
		return false
	case regexp.MustCompile(`(?i)Realtek Audio`).MatchString(name):
		for _, dev := range localDevices {
			if (strings.EqualFold(dev.Class, "MEDIA") || strings.EqualFold(dev.Class, "AudioEndpoint")) &&
				regexp.MustCompile(`(?i)Realtek|Audio`).MatchString(dev.Name) {
				return true
			}
		}
		return false
	case regexp.MustCompile(`(?i)AMD VGA`).MatchString(name):
		for _, dev := range localDevices {
			if strings.EqualFold(dev.Class, "Display") && regexp.MustCompile(`(?i)AMD|Radeon`).MatchString(dev.Name) {
				return true
			}
		}
		return false
	case regexp.MustCompile(`(?i)NVIDIA VGA`).MatchString(name):
		for _, dev := range localDevices {
			if strings.EqualFold(dev.Class, "Display") && regexp.MustCompile(`(?i)NVIDIA`).MatchString(dev.Name) {
				return true
			}
		}
		return false
	case regexp.MustCompile(`(?i)Realtek Lan`).MatchString(name):
		for _, dev := range localDevices {
			if strings.EqualFold(dev.Class, "Net") && regexp.MustCompile(`(?i)Realtek`).MatchString(dev.Name) {
				return true
			}
		}
		return false
	case regexp.MustCompile(`(?i)Serial-IO`).MatchString(name):
		for _, dev := range localDevices {
			if regexp.MustCompile(`(?i)Serial IO|Serial-IO|I2C|AMD.*IO`).MatchString(dev.Name) {
				return true
			}
		}
		return false
	case regexp.MustCompile(`(?i)AMD Power`).MatchString(name):
		for _, dev := range localDevices {
			if regexp.MustCompile(`(?i)AMD`).MatchString(dev.Name) {
				return true
			}
		}
		return false
	case regexp.MustCompile(`(?i)Lenovo Energy|Lenovo Fn|X-Rite`).MatchString(name):
		return true
	}
	return false
}

// GetDeviceVendor mirrors Get-DeviceVendor.
func GetDeviceVendor(names []string) string {
	for _, name := range names {
		switch {
		case regexp.MustCompile(`(?i)Intel|英特尔`).MatchString(name):
			return "Intel"
		case regexp.MustCompile(`(?i)Realtek`).MatchString(name):
			return "Realtek"
		case regexp.MustCompile(`(?i)MediaTek|MTK|MT79`).MatchString(name):
			return "MediaTek"
		case regexp.MustCompile(`(?i)AMD|Radeon`).MatchString(name):
			return "AMD"
		case regexp.MustCompile(`(?i)NVIDIA`).MatchString(name):
			return "NVIDIA"
		case regexp.MustCompile(`(?i)Sonix`).MatchString(name):
			return "Sonix"
		case regexp.MustCompile(`(?i)Sunplus`).MatchString(name):
			return "Sunplus"
		}
	}
	return ""
}

// GetRemoteComponentVendor mirrors Get-RemoteComponentVendor.
func GetRemoteComponentVendor(component string) string {
	switch {
	case regexp.MustCompile(`(?i)Intel`).MatchString(component):
		return "Intel"
	case regexp.MustCompile(`(?i)Realtek`).MatchString(component):
		return "Realtek"
	case regexp.MustCompile(`(?i)MediaTek|MTK|MT79`).MatchString(component):
		return "MediaTek"
	case regexp.MustCompile(`(?i)AMD|Radeon`).MatchString(component):
		return "AMD"
	case regexp.MustCompile(`(?i)NVIDIA`).MatchString(component):
		return "NVIDIA"
	case regexp.MustCompile(`(?i)Sonix`).MatchString(component):
		return "Sonix"
	case regexp.MustCompile(`(?i)Sunplus`).MatchString(component):
		return "Sunplus"
	}
	return ""
}

// GetMatchingRemoteComponent mirrors Get-MatchingRemoteComponent.
func GetMatchingRemoteComponent(remote, vendor string) *Version {
	if vendor == "" {
		return nil
	}
	for _, part := range regexp.MustCompile(`/|,`).Split(remote, -1) {
		if GetRemoteComponentVendor(part) == vendor {
			if v := ParseVersionString(part); v != nil {
				return v
			}
		}
	}
	return nil
}

// CompareDriverStatus mirrors Compare-DriverStatus.
func CompareDriverStatus(remote, local, vendor string) string {
	if local == "" {
		return "Not installed"
	}
	if remote == "" {
		return "Unknown"
	}
	if local == "Provisioned" {
		return "Up to date"
	}
	remoteVersion := GetMatchingRemoteComponent(remote, vendor)
	if remoteVersion == nil {
		remoteVersion = ParseVersionString(remote)
	}
	localVersion := ParseVersionString(local)
	if remoteVersion != nil && localVersion != nil {
		if c := localVersion.Compare(remoteVersion); c > 0 {
			return "Local newer"
		} else if c == 0 {
			return "Up to date"
		}
		return "Update"
	}
	return "Unknown"
}

// ResolveInstalledSoftwareVersion mirrors Resolve-InstalledSoftwareVersion.
func ResolveInstalledSoftwareVersion(driverName string, snapshot *model.SoftwareSnapshot) string {
	if snapshot == nil {
		return ""
	}
	var patterns []string
	switch {
	case regexp.MustCompile(`(?i)Lenovo Fn`).MatchString(driverName):
		patterns = append(patterns, `Lenovo.*Fn|Lenovo.*Hotkey|Lenovo Utility|Hotkeys`)
	case regexp.MustCompile(`(?i)Energy Management`).MatchString(driverName):
		patterns = append(patterns, `Lenovo Energy|Lenovo.*Power|Energy Management`)
	case regexp.MustCompile(`(?i)X-Rite`).MatchString(driverName):
		patterns = append(patterns, `X-Rite|Color Assistant`)
	case regexp.MustCompile(`(?i)AMD Power Processor`).MatchString(driverName):
		patterns = append(patterns, `AMD Power Processor|AMD Power`)
	case regexp.MustCompile(`(?i)Intel.*连接性|Intel.*Connectivity|Connectivity Performance`).MatchString(driverName):
		patterns = append(patterns, `Intel.*连接性|Intel.*Connectivity|Connectivity Performance|ICPS`)
	}
	for _, pattern := range patterns {
		re := regexp.MustCompile(`(?i)` + pattern)
		for _, app := range snapshot.InstalledApps {
			if re.MatchString(app.DisplayName) && app.DisplayVersion != "" {
				return app.DisplayVersion
			}
		}
	}
	if regexp.MustCompile(`(?i)AMD Power Processor`).MatchString(driverName) && snapshot.ProvisionedAmdPower == "Provisioned" {
		return "Provisioned"
	}
	if regexp.MustCompile(`(?i)Lenovo Fn`).MatchString(driverName) && snapshot.LenovoFnServiceVersion != "" {
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
		patterns := GetNamePatterns(driver.DriverName)
		for _, pattern := range patterns {
			re := regexp.MustCompile(`(?i)` + pattern)
			matched = nil
			for _, dev := range localDevices {
				if re.MatchString(dev.Name) &&
					!regexp.MustCompile(`(?i)Direct|Virtual`).MatchString(dev.Name) &&
					!strings.EqualFold(dev.Class, "SoftwareDevice") {
					matched = append(matched, dev)
				}
			}
			if len(matched) > 0 {
				break
			}
		}
	}
	return matched
}

// TestSoftwareVersionedDriver mirrors Test-SoftwareVersionedDriver.
func TestSoftwareVersionedDriver(driverName string) bool {
	return regexp.MustCompile(`(?i)Lenovo Fn|Energy Management|X-Rite|AMD Power|Intel.*连接性|Intel.*Connectivity|Connectivity Performance`).MatchString(driverName)
}

// ResolveLocalDriverVersion mirrors Resolve-LocalDriverVersion.
func ResolveLocalDriverVersion(driver *model.Driver, matchedDevices []model.Device, driverVersions []string, snapshot *model.SoftwareSnapshot) (localVersion, localVendor string) {
	softwareVersion := ResolveInstalledSoftwareVersion(driver.DriverName, snapshot)
	if TestSoftwareVersionedDriver(driver.DriverName) {
		localVendor = GetDeviceVendor(deviceNames(matchedDevices))
		localVersion = softwareVersion
		return localVersion, localVendor
	}
	if len(matchedDevices) > 0 {
		localVendor = GetDeviceVendor(deviceNames(matchedDevices))
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
		case 1:
			localVersion = unique[0]
		case 0:
			localVersion = ""
		default:
			localVersion = strings.Join(unique, ", ")
		}
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
