//go:build windows

package install

import (
	"fmt"
	"syscall"
	"unsafe"
)

// Native driver install calls DiInstallDriverW from newdev.dll. This is the
// Microsoft one-call primitive that imports the INF into the DriverStore and
// installs it for matching present devices, without Windows Update.

type nativeInstallProcSet struct {
	newdev          *syscall.LazyDLL
	diInstallDriver *syscall.LazyProc
}

var nativeInstallAPI = loadNativeInstallAPI()

func loadNativeInstallAPI() nativeInstallProcSet {
	newdev := syscall.NewLazyDLL("newdev.dll")
	return nativeInstallProcSet{
		newdev:          newdev,
		diInstallDriver: newdev.NewProc("DiInstallDriverW"),
	}
}

// NativeInstallEnabled reports whether the native install path is enabled.
// The runtime uses only the native path; pnputil remains as a system-native
// fallback when DiInstallDriverW is unavailable or fails.
func NativeInstallEnabled() bool {
	return true
}

// NativeInstallAvailable reports whether DiInstallDriverW can be loaded on
// this Windows build.
func NativeInstallAvailable() bool {
	return nativeInstallAPI.newdev.Load() == nil
}

// InstallNativeINF installs an INF driver package through DiInstallDriverW.
// rebootRequired is true when Windows asks for a restart.
func InstallNativeINF(infPath string) (rebootRequired bool, err error) {
	if err := nativeInstallAPI.newdev.Load(); err != nil {
		return false, fmt.Errorf("newdev.dll unavailable: %w", err)
	}
	p, err := syscall.UTF16PtrFromString(infPath)
	if err != nil {
		return false, err
	}
	var needReboot uint32
	r, _, callErr := nativeInstallAPI.diInstallDriver.Call(
		0, uintptr(unsafe.Pointer(p)), 0, uintptr(unsafe.Pointer(&needReboot)))
	if r == 0 {
		return false, fmt.Errorf("DiInstallDriver failed: %w", callErr)
	}
	return needReboot != 0, nil
}
