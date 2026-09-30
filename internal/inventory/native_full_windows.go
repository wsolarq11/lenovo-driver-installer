//go:build windows

package inventory

import (
	"context"
	"fmt"
	"os"
	"strings"
	"syscall"
	"unsafe"

	"lenovo-driver/internal/model"
)

// Native-only inventory: machine/OS identity, installed applications, and
// Lenovo software evidence, all through the registry and version resources
// without spawning PowerShell. PnP device enumeration lives in
// native_windows.go; the entry points here project over its identity pass and
// load driver class-key evidence explicitly only where needed.

const (
	regAppPath64    = `SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`
	regAppPath32    = `SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall`
	regAppPathUser  = `SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`
	regProvisioning = `SOFTWARE\Microsoft\Provisioning\Results`
)

// GetLocalDeviceSnapshot returns present PnP devices with name, class, and IDs
// through SetupAPI + CfgMgr32.
func GetLocalDeviceSnapshot(ctx context.Context) ([]model.Device, error) {
	_ = ctx
	rows, err := enumerateDevices()
	if err != nil {
		return nil, err
	}
	devices := make([]model.Device, 0, len(rows))
	for _, row := range rows {
		devices = append(devices, model.Device{
			Name:        row.name,
			Class:       row.class,
			DeviceID:    row.deviceID,
			PnpDeviceID: row.instanceID,
		})
	}
	return devices, nil
}

// GetMachineInfo resolves model and serial from the registry keys used by the
// SMBIOS/device identity, with an SMBIOS firmware-table fallback for serial.
func GetMachineInfo(ctx context.Context) (MachineInfo, error) {
	_ = ctx
	model := strings.TrimSpace(nativeReadKeyString(`SYSTEM\CurrentControlSet\Control\SystemInformation`, "SystemProductName"))
	serial := strings.TrimSpace(nativeReadKeyString(`SYSTEM\CurrentControlSet\Control\SystemInformation`, "SystemSerialNumber"))
	if model == "" {
		model = strings.TrimSpace(nativeReadKeyString(`SYSTEM\CurrentControlSet\Hardware Profiles\Current`, "SystemProductName"))
	}
	if model == "" {
		model = strings.TrimSpace(nativeReadKeyString(`HARDWARE\DESCRIPTION\System\BIOS`, "SystemProductName"))
	}
	if serial == "" {
		serial = strings.TrimSpace(nativeReadKeyString(`HARDWARE\DESCRIPTION\System\BIOS`, "SystemSerialNumber"))
	}
	if serial == "" {
		if raw, err := readSystemFirmwareTableSMBIOS(); err == nil {
			serial = smbiosSerialNumber(raw)
		}
	}
	return MachineInfo{Model: model, Serial: serial}, nil
}

func nativeReadKeyString(path, name string) string {
	h := nativeOpenKey(hklm, path)
	if h == 0 {
		return ""
	}
	defer nativeCloseKey(h)
	s, _ := nativeReadString(h, name)
	return s
}

// GetOSInfo resolves caption, kind, architecture, and the Lenovo OS match key.
func GetOSInfo(ctx context.Context) (OSInfo, error) {
	_ = ctx
	caption := strings.TrimSpace(nativeReadKeyString(`SOFTWARE\Microsoft\Windows NT\CurrentVersion`, "ProductName"))
	if caption == "" {
		caption = "Windows"
	}
	arch := "64-bit"
	// PROCESSOR_ARCHITECTURE in the environment is reliable on 64-bit OS with
	// a 32-bit process; use the registry machine hardware key for the real OS.
	if strings.Contains(strings.ToLower(nativeReadKeyString(`SYSTEM\CurrentControlSet\Control\Session Manager\Environment`, "PROCESSOR_ARCHITECTURE")), "x86") {
		arch = "32-bit"
	}
	return normalizeOSInfo(osInfoRow{Caption: caption, OSArchitecture: arch}), nil
}

// GetInstalledApps reads uninstall registry entries as installed application evidence.
func GetInstalledApps(ctx context.Context) ([]model.InstalledApp, error) {
	_ = ctx
	var apps []model.InstalledApp
	// Each hive is named at the call site. regAppPathUser and regAppPath64 are
	// the same key path under different hives, so a shared default root would
	// read the machine's 64-bit uninstall list twice and report every one of
	// those programs as a second copy of itself.
	for _, loc := range []struct {
		root uintptr
		path string
	}{
		{hklm, regAppPath64},
		{hklm, regAppPath32},
		{hkcu, regAppPathUser},
	} {
		apps = append(apps, readUninstallEntries(loc.root, loc.path)...)
	}
	return apps, nil
}

func readUninstallEntries(root uintptr, path string) []model.InstalledApp {
	var apps []model.InstalledApp
	enum := nativeOpenKey(root, path)
	if enum == 0 {
		return nil
	}
	defer nativeCloseKey(enum)
	for i := 0; ; i++ {
		sub := nativeEnumKey(enum, i)
		if sub == "" {
			break
		}
		keyPath := path + `\` + sub
		key := nativeOpenKey(root, keyPath)
		if key == 0 {
			continue
		}
		name, _ := nativeReadString(key, "DisplayName")
		if name != "" {
			version, _ := nativeReadString(key, "DisplayVersion")
			apps = append(apps, model.InstalledApp{DisplayName: name, DisplayVersion: version})
		}
		nativeCloseKey(key)
	}
	return apps
}

func nativeEnumKey(parent uintptr, index int) string {
	var nameBuf [512]uint16
	size := uint32(512)
	r, _, _ := nativeAPI.regEnumKeyEx.Call(parent, uintptr(index),
		uintptr(unsafe.Pointer(&nameBuf[0])), uintptr(unsafe.Pointer(&size)),
		0, 0, 0, 0)
	if r != 0 {
		return ""
	}
	return syscall.UTF16ToString(nameBuf[:size:size])
}

// GetSoftwareSnapshot resolves software-only driver evidence.
func GetSoftwareSnapshot(ctx context.Context, installedApps []model.InstalledApp) (model.SoftwareSnapshot, error) {
	_ = ctx
	snapshot := model.SoftwareSnapshot{InstalledApps: installedApps}
	snapshot.ProvisionedAmdPower = provisioningAMD()
	snapshot.LenovoFnServiceVersion = fnServiceVersion()
	return snapshot, nil
}

func provisioningAMD() string {
	enum := nativeOpenKey(hklm, regProvisioning)
	if enum == 0 {
		return ""
	}
	defer nativeCloseKey(enum)
	for i := 0; ; i++ {
		sub := nativeEnumKey(enum, i)
		if sub == "" {
			break
		}
		keyPath := regProvisioning + `\` + sub
		key := nativeOpenKey(hklm, keyPath)
		if key == 0 {
			continue
		}
		pkg, _ := nativeReadString(key, "PackageFileName")
		nativeCloseKey(key)
		if strings.EqualFold(pkg, "AMD.Power.Processor.ppkg") {
			return "Provisioned"
		}
	}
	return ""
}

func fnServiceVersion() string {
	// Win32_Service PathName is mirrored in the service registry key. We use
	// the ControlSet001 service key to avoid depending on the active control
	// set aliasing.
	for _, cset := range []string{`SYSTEM\CurrentControlSet001\Services\LenovoFnAndFunctionKeys`, `SYSTEM\CurrentControlSet\Services\LenovoFnAndFunctionKeys`} {
		h := nativeOpenKey(hklm, cset)
		if h == 0 {
			continue
		}
		imagePath, _ := nativeReadString(h, "ImagePath")
		nativeCloseKey(h)
		imagePath = strings.Trim(strings.TrimSpace(imagePath), `"`)
		if imagePath == "" {
			continue
		}
		expanded := expandWindowsEnv(imagePath)
		if expanded == "" {
			continue
		}
		info, err := os.Stat(expanded)
		if err != nil || info.IsDir() {
			continue
		}
		return nativeFileVersion(expanded)
	}
	return ""
}

func expandWindowsEnv(path string) string {
	// Service ImagePath values commonly use Windows %VAR% syntax; os.ExpandEnv
	// only handles $VAR, so expand the Windows form manually. Each pass removes
	// at least one "%VAR%" pair, so the loop is explicitly bounded by a generous
	// cap that guarantees termination even if an expansion value itself injects
	// further "%" markers (in which case the tail is left as-is after the cap).
	const maxExpansions = 64
	for expansion := 0; expansion < maxExpansions; expansion++ {
		start := strings.IndexByte(path, '%')
		if start < 0 {
			return path
		}
		end := strings.IndexByte(path[start+1:], '%')
		if end < 0 {
			return path
		}
		end = start + 1 + end
		name := path[start+1 : end]
		value := os.Getenv(name)
		if value == "" && strings.EqualFold(name, "SystemRoot") {
			value = os.Getenv("SystemRoot")
			if value == "" {
				value = `C:\Windows`
			}
		}
		path = path[:start] + value + path[end+1:]
	}
	return path
}

// IsAdministrator reports whether the current token is elevated.
func IsAdministrator() (bool, error) {
	const (
		getCurrentProcess     = ^uintptr(0)
		tokenQuery            = 0x0008 // TOKEN_QUERY
		tokenElevation        = 20     // TOKEN_INFORMATION_CLASS TokenElevation
		tokenElevationEnabled = 1
	)
	var token uintptr
	r, _, err := nativeAPI.openProcessToken.Call(
		getCurrentProcess, tokenQuery, uintptr(unsafe.Pointer(&token)))
	if r == 0 {
		return false, fmt.Errorf("OpenProcessToken failed: %v", err)
	}
	defer nativeAPI.closeHandle.Call(token)
	var elevation uint32
	var size uint32 = 4
	r, _, err = nativeAPI.getTokenInformation.Call(token, tokenElevation,
		uintptr(unsafe.Pointer(&elevation)), uintptr(size), uintptr(unsafe.Pointer(&size)))
	if r == 0 {
		return false, fmt.Errorf("GetTokenInformation failed: %v", err)
	}
	return elevation == tokenElevationEnabled, nil
}

// GetDeviceDriverVersions reads the driver version for each of the given PnP
// device IDs. Missing or driverless devices map to an empty string, aligned
// with the input order.
func GetDeviceDriverVersions(ctx context.Context, pnpIDs []string) ([]string, error) {
	_ = ctx
	if len(pnpIDs) == 0 {
		return nil, nil
	}
	byID, err := evidenceRows()
	if err != nil {
		return nil, err
	}
	versions := make([]string, 0, len(pnpIDs))
	for _, id := range pnpIDs {
		versions = append(versions, byID[id].driverVersion)
	}
	return versions, nil
}

// GetDeviceEvidence reads PnP driver properties for matched devices through
// the native SetupAPI/registry inventory.
func GetDeviceEvidence(ctx context.Context, devices []model.Device) ([]model.Device, error) {
	_ = ctx
	byID, err := evidenceRows()
	if err != nil {
		return nil, err
	}
	result := make([]model.Device, 0, len(devices))
	for _, dev := range devices {
		row, ok := byID[dev.PnpDeviceID]
		if !ok {
			continue
		}
		base := dev
		row.applyTo(&base)
		result = append(result, base)
	}
	return result, nil
}
