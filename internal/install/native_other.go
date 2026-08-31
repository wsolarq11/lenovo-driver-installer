//go:build !windows

package install

import "fmt"

// NativeInstallEnabled reports whether the native install path is enabled.
func NativeInstallEnabled() bool {
	return false
}

// NativeInstallAvailable reports whether DiInstallDriverW can be loaded.
func NativeInstallAvailable() bool {
	return false
}

// InstallNativeINF is unsupported off Windows.
func InstallNativeINF(infPath string) (bool, error) {
	return false, fmt.Errorf("native driver install requires Windows")
}
