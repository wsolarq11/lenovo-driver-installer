package install

import (
	"context"
	"fmt"
	"time"

	"lenovo-driver/internal/model"
)

// RemoveDriverPackage removes one published INF from the Windows Driver Store
// by its oemN.inf name. This is package cleanup, NOT device rollback: deleting
// the active package does not switch the device back to its previous driver and
// can leave it with no driver until PnP re-enumerates. Device rollback is a
// per-device operation done in Device Manager or by reinstalling the previous
// package, so this primitive must never be presented as a rollback action.
func RemoveDriverPackage(infName string) ProcessResult {
	return runProcess(context.Background(), "pnputil.exe", deleteDriverArgs(infName), 300*time.Second, "")
}

func deleteDriverArgs(infName string) []string {
	return []string{"/delete-driver", infName, "/uninstall", "/force"}
}

// Rollback error codes returned by DiRollbackDriver through GetLastError.
const (
	rollbackErrAccessDenied = 5   // ERROR_ACCESS_DENIED
	rollbackErrInWow64      = 129 // ERROR_IN_WOW64
	rollbackErrNoMoreItems  = 259 // ERROR_NO_MORE_ITEMS: no backup driver configured
)

// RollbackError classifies a failed DiRollbackDriver call so the caller can
// distinguish "there is no previous driver to roll back to" (not an action
// failure, the device must be left alone) from a real rollback failure
// (permissions, architecture, or an unexpected error).
type RollbackError struct {
	Code uintptr
}

func (e *RollbackError) Error() string {
	switch e.Code {
	case rollbackErrNoMoreItems:
		return "no backup driver available to roll back"
	case rollbackErrAccessDenied:
		return "administrator privileges required to roll back the driver"
	case rollbackErrInWow64:
		return "a 32-bit process cannot roll back a 64-bit driver"
	default:
		return fmt.Sprintf("DiRollbackDriver failed with error %d", e.Code)
	}
}

// NoBackup reports whether the rollback failed only because Windows has no
// backup driver for the device, meaning there is nothing to roll back to and
// the current driver must stay in place.
func (e *RollbackError) NoBackup() bool {
	return e.Code == rollbackErrNoMoreItems
}

// RollbackDeviceIDs returns the PnP instance IDs of the devices that report a
// problem code and are therefore device-level rollback candidates. Empty IDs
// are skipped because they cannot be opened for rollback. Kept pure so the
// candidate selection is testable offline.
func RollbackDeviceIDs(devices []model.Device) []string {
	var ids []string
	for _, device := range devices {
		if device.ProblemNumber != 0 && device.PnpDeviceID != "" {
			ids = append(ids, device.PnpDeviceID)
		}
	}
	return ids
}
