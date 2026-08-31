//go:build windows

package inventory

import (
	"encoding/binary"
	"fmt"
	"syscall"
	"time"
	"unsafe"

	"lenovo-driver/internal/model"
)

// Native device driver inventory reads PnP driver properties through SetupAPI
// and the registry. It avoids spawning PowerShell per query and keeps the
// runtime dependency-free (stdlib syscall only).

const (
	digcfPresent    = 0x00000002
	digcfAllClasses = 0x00000004
	hklm            = 0x80000002
	keyRead         = 0x00020019
	spdrpDeviceDesc = 0x0 // SPDRP_DEVICEDESC
	spdrpHardwareID = 0x1 // SPDRP_HARDWAREID
	spdrpClass      = 0x7 // SPDRP_CLASS
)

// spDevInfoData mirrors the SetupAPI SP_DEVINFO_DATA layout.
type spDevInfoData struct {
	cbSize    uint32
	classGUID [16]byte
	devInst   uint32
	reserved  uintptr
}

type nativeProcSet struct {
	getClassDevs           *syscall.LazyProc
	enumDeviceInfo         *syscall.LazyProc
	destroyDeviceInfoList  *syscall.LazyProc
	getDeviceInstanceID    *syscall.LazyProc
	cmGetDeviceID          *syscall.LazyProc
	getDevRegProp          *syscall.LazyProc
	getDeviceProperty      *syscall.LazyProc
	regOpenKeyEx           *syscall.LazyProc
	regQueryValueEx        *syscall.LazyProc
	regEnumKeyEx           *syscall.LazyProc
	regCloseKey            *syscall.LazyProc
	getFileVersionInfoSize *syscall.LazyProc
	getFileVersionInfo     *syscall.LazyProc
	verQueryValue          *syscall.LazyProc
	openProcessToken       *syscall.LazyProc
	getTokenInformation    *syscall.LazyProc
	closeHandle            *syscall.LazyProc
}

var nativeAPI = loadNativeAPI()

func loadNativeAPI() nativeProcSet {
	setupapi := syscall.NewLazyDLL("setupapi.dll")
	cfgmgr32 := syscall.NewLazyDLL("cfgmgr32.dll")
	advapi := syscall.NewLazyDLL("advapi32.dll")
	version := syscall.NewLazyDLL("version.dll")
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	return nativeProcSet{
		getClassDevs:           setupapi.NewProc("SetupDiGetClassDevsW"),
		enumDeviceInfo:         setupapi.NewProc("SetupDiEnumDeviceInfo"),
		destroyDeviceInfoList:  setupapi.NewProc("SetupDiDestroyDeviceInfoList"),
		getDeviceInstanceID:    setupapi.NewProc("SetupDiGetDeviceInstanceIdW"),
		cmGetDeviceID:          cfgmgr32.NewProc("CM_Get_Device_IDW"),
		getDevRegProp:          setupapi.NewProc("SetupDiGetDeviceRegistryPropertyW"),
		getDeviceProperty:      setupapi.NewProc("SetupDiGetDevicePropertyW"),
		regOpenKeyEx:           advapi.NewProc("RegOpenKeyExW"),
		regQueryValueEx:        advapi.NewProc("RegQueryValueExW"),
		regEnumKeyEx:           advapi.NewProc("RegEnumKeyExW"),
		regCloseKey:            advapi.NewProc("RegCloseKey"),
		getFileVersionInfoSize: version.NewProc("GetFileVersionInfoSizeW"),
		getFileVersionInfo:     version.NewProc("GetFileVersionInfoW"),
		verQueryValue:          version.NewProc("VerQueryValueW"),
		openProcessToken:       advapi.NewProc("OpenProcessToken"),
		getTokenInformation:    advapi.NewProc("GetTokenInformation"),
		closeHandle:            kernel32.NewProc("CloseHandle"),
	}
}

// nativeDeviceRow is the complete per-device inventory row collected in one
// SetupAPI enumeration pass: identity properties plus driver class-key
// evidence and the install date.
type nativeDeviceRow struct {
	name          string
	class         string
	deviceID      string // SPDRP_HARDWAREID, first entry
	instanceID    string
	driverVersion string
	driverDate    string
	infName       string
	providerName  string
	installDate   string
}

// enumerateDevices scans the present PnP tree once, collecting identity
// properties and the install date for every device. The registry-heavy
// driver class-key evidence is a separate explicit load (loadDriverEvidence)
// run only by entry points that expose it, so the identity-only snapshot path
// stays light.
func enumerateDevices() ([]nativeDeviceRow, error) {
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
		row := nativeDeviceRow{
			instanceID:  instanceID,
			name:        nativeDeviceRegistryString(hdev, dev, spdrpDeviceDesc),
			deviceID:    nativeDeviceRegistryString(hdev, dev, spdrpHardwareID),
			class:       nativeDeviceRegistryString(hdev, dev, spdrpClass),
			installDate: readDeviceInstallDate(hdev, dev),
		}
		out = append(out, row)
	}
	return out, nil
}

// loadDriverEvidence fills the driver class-key evidence (version, date, INF
// path, provider) for every row. It is the registry-heavy half of the device
// pass and is only run by entry points that expose driver evidence.
func loadDriverEvidence(rows []nativeDeviceRow) {
	for i := range rows {
		fillDriverClassProperties(&rows[i])
	}
}

// indexRows builds an instanceID-keyed index of the device rows.
func indexRows(rows []nativeDeviceRow) map[string]nativeDeviceRow {
	byID := make(map[string]nativeDeviceRow, len(rows))
	for _, row := range rows {
		byID[row.instanceID] = row
	}
	return byID
}

// evidenceRows enumerates the PnP tree once, loads the registry-heavy driver
// class-key evidence for every row, and returns an instanceID-keyed index. It
// is the shared enumeration prefix of the evidence entry points (
// GetDeviceEvidence, GetDeviceDriverVersions) so neither performs its own full
// enumeration + evidence load.
func evidenceRows() (map[string]nativeDeviceRow, error) {
	rows, err := enumerateDevices()
	if err != nil {
		return nil, err
	}
	loadDriverEvidence(rows)
	return indexRows(rows), nil
}

// applyTo copies the row's evidence fields onto a device row. Identity fields
// (PnpDeviceID) stay under the caller's control.
func (row nativeDeviceRow) applyTo(dev *model.Device) {
	dev.Name = row.name
	dev.Class = row.class
	dev.DeviceID = row.deviceID
	dev.DriverVersion = row.driverVersion
	dev.DriverDate = row.driverDate
	dev.InfName = row.infName
	dev.ProviderName = row.providerName
	dev.InstallDate = row.installDate
}

// fillDriverClassProperties resolves the device's Driver class key and fills
// the standard PnP driver properties into row.
func fillDriverClassProperties(row *nativeDeviceRow) {
	enumPath := `SYSTEM\CurrentControlSet\Enum\` + row.instanceID
	enumKey := nativeOpenKey(enumPath)
	if enumKey == 0 {
		return
	}
	driver, ok := nativeReadString(enumKey, "Driver")
	nativeCloseKey(enumKey)
	if !ok || driver == "" {
		return
	}

	classPath := `SYSTEM\CurrentControlSet\Control\Class\` + driver
	classKey := nativeOpenKey(classPath)
	if classKey == 0 {
		return
	}
	defer nativeCloseKey(classKey)
	row.driverVersion, _ = nativeReadString(classKey, "DriverVersion")
	row.driverDate, _ = nativeReadString(classKey, "DriverDate")
	row.infName, _ = nativeReadString(classKey, "InfPath")
	row.providerName, _ = nativeReadString(classKey, "ProviderName")
}

// devPropKey mirrors the DEVPROPKEY layout: a GUID fmtid plus a property id.
type devPropKey struct {
	fmtid [16]byte
	pid   uint32
}

// devpropKeyDeviceInstallDate is DEVPKEY_Device_InstallDate
// (fmtid A45C254E-DF1C-4EFD-8020-67D146A850E0, pid 3).
var devpropKeyDeviceInstallDate = devPropKey{
	fmtid: [16]byte{0x4e, 0x25, 0x5c, 0xa4, 0x1c, 0xdf, 0xfd, 0x4e,
		0x80, 0x20, 0x67, 0xd1, 0x46, 0xa8, 0x50, 0xe0},
	pid: 3,
}

// readDeviceInstallDate reads DEVPKEY_Device_InstallDate (a FILETIME) through
// SetupDiGetDevicePropertyW and formats it in UTC.
func readDeviceInstallDate(hdev uintptr, dev *spDevInfoData) string {
	const devpropTypeFiletime = 0x00000040
	var typ uint32
	var size uint32 = 8
	var ft [8]byte
	r, _, _ := nativeAPI.getDeviceProperty.Call(
		hdev, uintptr(unsafe.Pointer(dev)),
		uintptr(unsafe.Pointer(&devpropKeyDeviceInstallDate)),
		uintptr(unsafe.Pointer(&typ)), uintptr(unsafe.Pointer(&ft[0])),
		uintptr(unsafe.Pointer(&size)), 0)
	if r == 0 || size < 8 || typ != devpropTypeFiletime {
		return ""
	}
	return formatFileTime(ft)
}

// formatFileTime renders a little-endian FILETIME (100ns ticks since
// 1601-01-01 UTC) as a UTC RFC3339 timestamp (AGENTS.md time-unification
// boundary). Zero ticks means never installed.
func formatFileTime(ft [8]byte) string {
	ticks := binary.LittleEndian.Uint64(ft[:])
	if ticks == 0 {
		return ""
	}
	const unixEpochOffset = 11644473600 // seconds from 1601-01-01 to 1970-01-01
	secs := int64(ticks/10_000_000) - unixEpochOffset
	return time.Unix(secs, 0).UTC().Format(time.RFC3339)
}

// GetNativeDeviceEvidence returns PnP device driver properties read through
// SetupAPI + the class registry keys, for every device with a driver version.
func GetNativeDeviceEvidence() ([]model.Device, error) {
	rows, err := enumerateDevices()
	if err != nil {
		return nil, err
	}
	loadDriverEvidence(rows)
	out := make([]model.Device, 0, len(rows))
	for _, row := range rows {
		if row.driverVersion == "" {
			continue
		}
		dev := model.Device{PnpDeviceID: row.instanceID}
		row.applyTo(&dev)
		out = append(out, dev)
	}
	return out, nil
}

func nativeInstanceID(hdev uintptr, dev *spDevInfoData) (string, error) {
	// CfgMgr32 is the stable lower-level route and is the Microsoft-recommended
	// migration target for SetupAPI device enumeration.
	const maxChars = 512
	buf := make([]uint16, maxChars)
	r, _, errno := nativeAPI.cmGetDeviceID.Call(uintptr(dev.devInst),
		uintptr(unsafe.Pointer(&buf[0])), maxChars, 0)
	if r == 0 {
		return syscall.UTF16ToString(buf), nil
	}
	// Fallback to SetupAPI if CfgMgr32 is unavailable on the running build.
	size := uint32(maxChars)
	r, _, errno = nativeAPI.getDeviceInstanceID.Call(hdev, uintptr(unsafe.Pointer(dev)),
		uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)))
	if r == 0 {
		return "", fmt.Errorf("SetupDiGetDeviceInstanceIdW failed: %v", errno)
	}
	return syscall.UTF16ToString(buf), nil
}

// nativeDeviceRegistryString reads a string device property. REG_MULTI_SZ and
// REG_SZ are both NUL-terminated UTF-16; decodeUTF16 stops at the first NUL,
// so the first string of a multi-string value is returned.
func nativeDeviceRegistryString(hdev uintptr, dev *spDevInfoData, property uint32) string {
	buf := make([]byte, 4096)
	var typ uint32
	var size uint32 = 4096
	r, _, _ := nativeAPI.getDevRegProp.Call(hdev, uintptr(unsafe.Pointer(dev)), uintptr(property),
		uintptr(unsafe.Pointer(&typ)), uintptr(unsafe.Pointer(&buf[0])), uintptr(size), uintptr(unsafe.Pointer(&size)))
	if r == 0 || size < 2 {
		return ""
	}
	return decodeUTF16(buf[:size])
}

func nativeOpenKey(path string) uintptr {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0
	}
	var h uintptr
	r, _, _ := nativeAPI.regOpenKeyEx.Call(hklm, uintptr(unsafe.Pointer(p)), 0, keyRead, uintptr(unsafe.Pointer(&h)))
	if r != 0 {
		return 0
	}
	return h
}

func nativeReadString(h uintptr, name string) (string, bool) {
	pname, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return "", false
	}
	var typ uint32
	var size uint32 = 4096
	buf := make([]byte, size)
	r, _, _ := nativeAPI.regQueryValueEx.Call(h, uintptr(unsafe.Pointer(pname)),
		0, uintptr(unsafe.Pointer(&typ)), uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)))
	if r != 0 || size < 2 || !isStringType(typ) {
		return "", false
	}
	s := decodeUTF16(buf[:size])
	return s, s != ""
}

// isStringType reports whether a registry value type is a string
// (REG_SZ or REG_EXPAND_SZ).
func isStringType(typ uint32) bool {
	return typ == 1 || typ == 2 // REG_SZ / REG_EXPAND_SZ
}

func nativeCloseKey(h uintptr) {
	nativeAPI.regCloseKey.Call(h)
}

// decodeUTF16 converts raw little-endian UTF-16 bytes into a Go string,
// stopping at the first NUL. It is used by the native registry reads and kept
// pure so it can be tested offline.
func decodeUTF16(b []byte) string {
	if len(b) < 2 {
		return ""
	}
	u := make([]uint16, len(b)/2)
	for j := 0; j < len(u); j++ {
		u[j] = uint16(b[2*j]) | uint16(b[2*j+1])<<8
	}
	return syscall.UTF16ToString(u)
}
