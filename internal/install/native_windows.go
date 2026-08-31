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
