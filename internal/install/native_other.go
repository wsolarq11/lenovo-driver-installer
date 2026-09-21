//go:build !windows

package install

import "fmt"

// InstallNativeINF is unsupported off Windows.
func InstallNativeINF(infPath string) (bool, error) {
	return false, fmt.Errorf("native driver install requires Windows")
}

// ForceReinstallINF is unsupported off Windows.
func ForceReinstallINF(infPath, hardwareID string) (bool, error) {
	return false, fmt.Errorf("native driver reinstall requires Windows")
}
