//go:build !windows

package install

import "fmt"

// RollbackDriverNative is unsupported off Windows.
func RollbackDriverNative(instanceID string) (bool, error) {
	return false, fmt.Errorf("device driver rollback requires Windows")
}
