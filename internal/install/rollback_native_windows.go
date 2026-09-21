//go:build windows

package install

import (
	"fmt"
	"syscall"
	"unsafe"
)

// Native device rollback calls DiRollbackDriver from newdev.dll after opening
// the target device's info element with SetupDiOpenDeviceInfoW. This is the
// same primitive Device Manager's "Roll Back Driver" uses, so it is a
// per-device operation, never the package cleanup that pnputil /delete-driver
// performs. LazyProc loads the DLLs on first call.

const (
	digcfPresentRollback    = 0x00000002 // DIGCF_PRESENT
	digcfAllClassesRollback = 0x00000004 // DIGCF_ALLCLASSES
	rollbackFlagNoUI        = 0x1        // ROLLBACK_FLAG_NO_UI
)

var (
	setupAPIRollback       = syscall.NewLazyDLL("setupapi.dll")
	newdevRollback         = syscall.NewLazyDLL("newdev.dll")
	procRollbackGetDevs    = setupAPIRollback.NewProc("SetupDiGetClassDevsW")
	procRollbackOpenInfo   = setupAPIRollback.NewProc("SetupDiOpenDeviceInfoW")
	procRollbackDestroy    = setupAPIRollback.NewProc("SetupDiDestroyDeviceInfoList")
	procRollbackDriverCall = newdevRollback.NewProc("DiRollbackDriver")
)

// rollbackDevInfoData mirrors the SetupAPI SP_DEVINFO_DATA layout needed by
// SetupDiOpenDeviceInfoW and DiRollbackDriver.
type rollbackDevInfoData struct {
	cbSize    uint32
	classGUID [16]byte
	devInst   uint32
	reserved  uintptr
}

// RollbackDriverNative rolls one present device's driver back to its previous
// backup driver through DiRollbackDriver. rebootRequired reports whether
// Windows needs a restart to finish the rollback. When Windows has no backup
// driver for the device, the error is a *RollbackError whose NoBackup() is
// true and the current driver is left untouched.
func RollbackDriverNative(instanceID string) (rebootRequired bool, err error) {
	hdev, _, callErr := procRollbackGetDevs.Call(0, 0, 0, digcfPresentRollback|digcfAllClassesRollback)
	if hdev == 0 || hdev == ^uintptr(0) {
		return false, fmt.Errorf("SetupDiGetClassDevsW failed: %v", callErr)
	}
	defer procRollbackDestroy.Call(hdev)

	dev := &rollbackDevInfoData{}
	dev.cbSize = uint32(unsafe.Sizeof(*dev))
	idPtr, err := syscall.UTF16PtrFromString(instanceID)
	if err != nil {
		return false, err
	}
	r, _, callErr := procRollbackOpenInfo.Call(hdev, uintptr(unsafe.Pointer(idPtr)), 0, 0, uintptr(unsafe.Pointer(dev)))
	if r == 0 {
		return false, fmt.Errorf("SetupDiOpenDeviceInfoW failed for %s: %v", instanceID, callErr)
	}

	var needReboot uint32
	r, _, callErr = procRollbackDriverCall.Call(hdev, uintptr(unsafe.Pointer(dev)), 0, rollbackFlagNoUI, uintptr(unsafe.Pointer(&needReboot)))
	if r == 0 {
		errno, ok := callErr.(syscall.Errno)
		if !ok {
			return false, fmt.Errorf("DiRollbackDriver failed: %v", callErr)
		}
		return false, &RollbackError{Code: uintptr(errno)}
	}
	return needReboot != 0, nil
}
