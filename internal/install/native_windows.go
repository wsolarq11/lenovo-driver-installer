//go:build windows

package install

import (
	"fmt"
	"syscall"
	"unsafe"
)

// Native driver install calls DiInstallDriverW from newdev.dll. This is the
// Microsoft one-call primitive that imports the INF into the DriverStore and
// installs it for matching present devices, without Windows Update. LazyProc
// loads the DLL on first call, so unavailability surfaces as a DiInstallDriver
// failure and installNativeOrPnPUtil falls back to pnputil.

var diInstallDriver = syscall.NewLazyDLL("newdev.dll").NewProc("DiInstallDriverW")

// updateDriverForPnP is UpdateDriverForPlugAndPlayDevicesW: the primitive that
// can force a device to a specific driver even when a newer driver is already
// bound. It is the correct downgrade path; DiInstallDriverW never downgrades.
var updateDriverForPnP = syscall.NewLazyDLL("newdev.dll").NewProc("UpdateDriverForPlugAndPlayDevicesW")

// installFlagForce forces the driver install even if a better driver already
// exists (INSTALLFLAG_FORCE). It is the only way to bind an older driver over a
// newer one without first deleting the newer package.
const installFlagForce = 0x1

// InstallNativeINF installs an INF driver package through DiInstallDriverW.
// rebootRequired is true when Windows asks for a restart.
func InstallNativeINF(infPath string) (rebootRequired bool, err error) {
	p, err := syscall.UTF16PtrFromString(infPath)
	if err != nil {
		return false, err
	}
	var needReboot uint32
	r, _, callErr := diInstallDriver.Call(
		0, uintptr(unsafe.Pointer(p)), 0, uintptr(unsafe.Pointer(&needReboot)))
	if r == 0 {
		return false, fmt.Errorf("DiInstallDriver failed: %w", callErr)
	}
	return needReboot != 0, nil
}

// ForceReinstallINF reinstalls the given INF for the devices matching hardwareID
// through UpdateDriverForPlugAndPlayDevicesW with INSTALLFLAG_FORCE. Unlike
// InstallNativeINF it downgrades the device to this driver even when a newer
// driver is currently bound, which is what a rollback needs. The caller must
// re-verify device health afterward because the call returns success even when
// the device did not actually change (see reinstallPreviousInf's verification).
func ForceReinstallINF(infPath, hardwareID string) (rebootRequired bool, err error) {
	infPtr, err := syscall.UTF16PtrFromString(infPath)
	if err != nil {
		return false, err
	}
	hwPtr, err := syscall.UTF16PtrFromString(hardwareID)
	if err != nil {
		return false, err
	}
	var needReboot uint32
	r, _, callErr := updateDriverForPnP.Call(
		0,
		uintptr(unsafe.Pointer(hwPtr)),
		uintptr(unsafe.Pointer(infPtr)),
		installFlagForce,
		uintptr(unsafe.Pointer(&needReboot)))
	if r == 0 {
		return false, fmt.Errorf("UpdateDriverForPlugAndPlayDevices failed: %w", callErr)
	}
	return needReboot != 0, nil
}
