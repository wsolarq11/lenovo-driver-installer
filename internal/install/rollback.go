package install

import (
	"fmt"

	"lenovo-driver/internal/model"
)

// Package cleanup by INF name (the pnputil /delete-driver vector) is
// deliberately absent from this package. Deleting the published package does
// not put a device back on its previous driver and can leave it with none until
// PnP re-enumerates, so it must never be reachable as a rollback action; the
// verifier pins that absence. What remains here is the device-level path.

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
