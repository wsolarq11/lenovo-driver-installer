//go:build windows

package inventory

import (
	"fmt"
	"syscall"
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

// GetNativeDeviceEvidence returns PnP device driver properties read through
// SetupAPI + the class registry keys.
func GetNativeDeviceEvidence() ([]model.Device, error) {
	hdev, _, errno := nativeAPI.getClassDevs.Call(0, 0, 0, digcfPresent|digcfAllClasses)
	if hdev == 0 || hdev == ^uintptr(0) {
		return nil, fmt.Errorf("SetupDiGetClassDevs failed: %v", errno)
	}
	defer nativeAPI.destroyDeviceInfoList.Call(hdev)

	dev := &spDevInfoData{}
	dev.cbSize = uint32(unsafe.Sizeof(*dev))

	var out []model.Device
	for index := 0; ; index++ {
		r, _, _ := nativeAPI.enumDeviceInfo.Call(hdev, uintptr(index), uintptr(unsafe.Pointer(dev)))
		if r == 0 {
			break
		}
		id, err := nativeInstanceID(hdev, dev)
		if err != nil {
			continue
		}
		d, ok := readDriverClassProperties(id)
		if !ok {
			continue
		}
		out = append(out, d)
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

// readDriverClassProperties resolves a device's Driver class key and returns a
// Device row populated with the standard PnP driver properties.
func readDriverClassProperties(instanceID string) (model.Device, bool) {
	enumPath := `SYSTEM\CurrentControlSet\Enum\` + instanceID
	enumKey := nativeOpenKey(enumPath)
	if enumKey == 0 {
		return model.Device{}, false
	}
	driver, ok := nativeReadString(enumKey, "Driver")
	nativeCloseKey(enumKey)
	if !ok || driver == "" {
		return model.Device{}, false
	}

	classPath := `SYSTEM\CurrentControlSet\Control\Class\` + driver
	classKey := nativeOpenKey(classPath)
	if classKey == 0 {
		return model.Device{}, false
	}
	defer nativeCloseKey(classKey)

	version, _ := nativeReadString(classKey, "DriverVersion")
	date, _ := nativeReadString(classKey, "DriverDate")
	infName, _ := nativeReadString(classKey, "InfPath")
	provider, _ := nativeReadString(classKey, "ProviderName")

	if version == "" {
		return model.Device{}, false
	}
	return model.Device{
		Name:          "",
		Class:         "",
		DeviceID:      "",
		PnpDeviceID:   instanceID,
		DriverVersion: version,
		DriverDate:    date,
		InfName:       infName,
		ProviderName:  provider,
		InstallDate:   "",
	}, true
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
	if r != 0 || size < 2 {
		return "", false
	}
	if typ != 1 && typ != 2 { // REG_SZ / REG_EXPAND_SZ
		return "", false
	}
	s := decodeUTF16(buf[:size])
	return s, s != ""
}

func nativeCloseKey(h uintptr) {
	nativeAPI.regCloseKey.Call(h)
}

// decodeUTF16 converts raw little-endian UTF-16 bytes into a Go string. It is
// used by the native registry reads and kept pure so it can be tested offline.
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
