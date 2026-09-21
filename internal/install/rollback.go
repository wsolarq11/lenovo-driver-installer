package install

import (
	"context"
	"time"
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
