//go:build windows

package inventory

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"lenovo-driver/internal/model"
)

// Native-only inventory: PnP device snapshot, machine/OS identity, installed
// applications, and Lenovo software evidence, all through SetupAPI/CfgMgr32
// and the registry without spawning PowerShell.

const (
	regAppPath64    = `SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`
	regAppPath32    = `SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall`
	regAppPathUser  = `SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`
	regProvisioning = `SOFTWARE\Microsoft\Provisioning\Results`
	regClassRoot    = `SYSTEM\CurrentControlSet\Control\Class`
	regEnumRoot     = `SYSTEM\CurrentControlSet\Enum`
)

// GetLocalDeviceSnapshot returns present PnP devices with name, class, and IDs
// through SetupAPI + CfgMgr32.
func GetLocalDeviceSnapshot(ctx context.Context) ([]model.Device, error) {
	_ = ctx
	rows, err := enumeratePresentDevices()
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

type nativeDeviceRow struct {
	name       string
	class      string
	deviceID   string
	instanceID string
}

func enumeratePresentDevices() ([]nativeDeviceRow, error) {
	hdev, _, errno := nativeAPI.getClassDevs.Call(0, 0, 0, digcfPresent|digcfAllClasses)
	if hdev == 0 || hdev == ^uintptr(0) {
		return nil, fmt.Errorf("SetupDiGetClassDevs failed: %v", errno)
	}
	defer nativeAPI.destroyDeviceInfoList.Call(hdev)

	dev := &spDevInfoData{}
	dev.cbSize = uint32(unsafe.Sizeof(*dev))

	var out []nativeDeviceRow
	for index := 0; ; index++ {
		r, _, _ := nativeAPI.enumDeviceInfo.Call(hdev, uintptr(index), uintptr(unsafe.Pointer(dev)))
		if r == 0 {
			break
		}
		instanceID, err := nativeInstanceID(hdev, dev)
		if err != nil || instanceID == "" {
			continue
		}
		row := nativeDeviceRow{instanceID: instanceID}
		row.name = nativeDeviceRegistryString(hdev, dev, 0x0)     // SPDRP_DEVICEDESC
		row.deviceID = nativeDeviceRegistryString(hdev, dev, 0x1) // SPDRP_HARDWAREID
		row.class = nativeDeviceRegistryString(hdev, dev, 0x7)    // SPDRP_CLASS
		out = append(out, row)
	}
	return out, nil
}

func nativeDeviceRegistryString(hdev uintptr, dev *spDevInfoData, property uint32) string {
	buf := make([]byte, 4096)
	var typ uint32
	var size uint32 = 4096
	r, _, _ := nativeAPI.getDevRegProp.Call(hdev, uintptr(unsafe.Pointer(dev)), uintptr(property),
		uintptr(unsafe.Pointer(&typ)), uintptr(unsafe.Pointer(&buf[0])), uintptr(size), uintptr(unsafe.Pointer(&size)))
	if r == 0 || size < 2 {
		return ""
	}
	if typ == 7 { // REG_MULTI_SZ; take first string
		return decodeFirstMultiString(buf[:size])
	}
	return decodeUTF16(buf[:size])
}

func decodeFirstMultiString(b []byte) string {
	if len(b) < 2 {
		return ""
	}
	u := make([]uint16, len(b)/2)
	for j := 0; j < len(u); j++ {
		u[j] = uint16(b[2*j]) | uint16(b[2*j+1])<<8
	}
	for i, v := range u {
		if v == 0 {
			return syscall.UTF16ToString(u[:i])
		}
	}
	return syscall.UTF16ToString(u)
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
	h := nativeOpenKey(path)
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
	edition := strings.TrimSpace(nativeReadKeyString(`SOFTWARE\Microsoft\Windows NT\CurrentVersion`, "EditionID"))
	if edition != "" && !strings.Contains(caption, edition) {
		caption = caption + " " + edition
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
	for _, path := range []string{regAppPath64, regAppPath32, regAppPathUser} {
		apps = append(apps, readUninstallEntries(path)...)
	}
	return apps, nil
}

func readUninstallEntries(path string) []model.InstalledApp {
	var apps []model.InstalledApp
	enum := nativeOpenKey(path)
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
		key := nativeOpenKey(keyPath)
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
	enum := nativeOpenKey(regProvisioning)
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
		key := nativeOpenKey(keyPath)
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
		h := nativeOpenKey(cset)
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
		if err != nil {
			continue
		}
		if info.IsDir() {
			continue
		}
		return fileVersion(expanded)
	}
	return ""
}

func expandWindowsEnv(path string) string {
	// Service ImagePath values commonly use Windows %VAR% syntax; os.ExpandEnv
	// only handles $VAR, so expand the Windows form manually.
	for {
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
}

func fileVersion(path string) string {
	// Pure stdlib has no version resource reader, so use the best stable
	// alternative: the version is available as FileDescription through
	// GetFileVersionInfo. We bind the API and return the fixed version.
	return nativeFileVersion(path)
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func nativeFileVersion(path string) string {
	size, _, _ := nativeAPI.getFileVersionInfoSize.Call(
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(path))), 0)
	if size == 0 {
		return ""
	}
	buf := make([]byte, size)
	ok, _, _ := nativeAPI.getFileVersionInfo.Call(
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(path))), 0, uintptr(size), uintptr(unsafe.Pointer(&buf[0])))
	if ok == 0 {
		return ""
	}
	var handle unsafe.Pointer
	_, _, _ = nativeAPI.verQueryValue.Call(
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(`\`))),
		uintptr(unsafe.Pointer(&handle)), uintptr(unsafe.Pointer(&size)))
	if handle == nil {
		return ""
	}
	// VS_FIXEDFILEINFO is 13 DWORDs; FileVersionMS at +8, FileVersionLS at +12.
	// We return the dotted version string.
	info := (*vsFixedFileInfo)(handle)
	if info.signature != 0xFEEF04BD {
		return ""
	}
	return fmt.Sprintf("%d.%d.%d.%d",
		(info.fileVersionMS>>16)&0xffff, info.fileVersionMS&0xffff,
		(info.fileVersionLS>>16)&0xffff, info.fileVersionLS&0xffff)
}

type vsFixedFileInfo struct {
	signature        uint32
	structVersion    uint32
	fileVersionMS    uint32
	fileVersionLS    uint32
	productVersionMS uint32
	productVersionLS uint32
	fileFlagsMask    uint32
	fileFlags        uint32
	fileOS           uint32
	fileType         uint32
	fileSubtype      uint32
	fileDateMS       uint32
	fileDateLS       uint32
}

// IsAdministrator reports whether the current token is elevated.
func IsAdministrator() (bool, error) {
	// Windows built-in token elevation check through advapi32 without PS.
	var token uintptr
	r, _, err := nativeAPI.openProcessToken.Call(
		uintptr(0xffffffffffffffff), 0x0008, uintptr(unsafe.Pointer(&token))) // PROCESS_QUERY_INFORMATION, TokenRead? use TOKEN_QUERY=0x0008
	if r == 0 {
		return false, fmt.Errorf("OpenProcessToken failed: %v", err)
	}
	defer nativeAPI.closeHandle.Call(token)
	var elevation uint32
	var size uint32 = 4
	r, _, err = nativeAPI.getTokenInformation.Call(token, 20, /*TokenElevation*/
		uintptr(unsafe.Pointer(&elevation)), uintptr(size), uintptr(unsafe.Pointer(&size)))
	if r == 0 {
		return false, fmt.Errorf("GetTokenInformation failed: %v", err)
	}
	return elevation != 0, nil
}

// GetDeviceDriverVersions reads DEVPKEY_Device_DriverVersion for matching PnP devices.
func GetDeviceDriverVersions(ctx context.Context, pnpIDs []string) ([]string, error) {
	_ = ctx
	if len(pnpIDs) == 0 {
		return nil, nil
	}
	devices, err := GetNativeDeviceEvidence()
	if err != nil {
		return nil, err
	}
	byID := make(map[string]string, len(devices))
	for _, dev := range devices {
		if dev.DriverVersion != "" {
			byID[dev.PnpDeviceID] = dev.DriverVersion
		}
	}
	versions := make([]string, 0, len(pnpIDs))
	for _, id := range pnpIDs {
		versions = append(versions, byID[id])
	}
	return versions, nil
}

// GetDeviceEvidence reads PnP driver properties for matched devices through
// the native SetupAPI/registry inventory.
func GetDeviceEvidence(ctx context.Context, devices []model.Device) ([]model.Device, error) {
	_ = ctx
	native, err := GetNativeDeviceEvidence()
	if err != nil {
		return nil, err
	}
	byID := make(map[string]model.Device, len(native))
	for _, dev := range native {
		byID[dev.PnpDeviceID] = dev
	}
	result := make([]model.Device, 0, len(devices))
	for _, dev := range devices {
		if row, ok := byID[dev.PnpDeviceID]; ok {
			base := dev
			base.Name = row.Name
			base.Class = row.Class
			base.DriverVersion = row.DriverVersion
			base.DriverDate = row.DriverDate
			base.InfName = row.InfName
			base.ProviderName = row.ProviderName
			base.InstallDate = row.InstallDate
			result = append(result, base)
		}
	}
	return result, nil
}

var _ = filepath.Separator
