//go:build !windows

package inventory

import (
	"context"
	"fmt"

	"lenovo-driver/internal/model"
)

// GetNativeDeviceEvidence is unsupported off Windows.
func GetNativeDeviceEvidence() ([]model.Device, error) {
	return nil, fmt.Errorf("native device inventory requires Windows")
}

// GetDeviceEvidence is unsupported off Windows.
func GetDeviceEvidence(ctx context.Context, devices []model.Device) ([]model.Device, error) {
	return nil, fmt.Errorf("native device inventory requires Windows")
}

// GetDeviceDriverVersions is unsupported off Windows.
func GetDeviceDriverVersions(ctx context.Context, pnpIDs []string) ([]string, error) {
	return nil, fmt.Errorf("native device inventory requires Windows")
}

// GetLocalDeviceSnapshot is unsupported off Windows.
func GetLocalDeviceSnapshot(ctx context.Context) ([]model.Device, error) {
	return nil, fmt.Errorf("native device inventory requires Windows")
}

// GetMachineInfo is unsupported off Windows.
func GetMachineInfo(ctx context.Context) (MachineInfo, error) {
	return MachineInfo{}, fmt.Errorf("native machine info requires Windows")
}

// GetOSInfo is unsupported off Windows.
func GetOSInfo(ctx context.Context) (OSInfo, error) {
	return OSInfo{}, fmt.Errorf("native OS info requires Windows")
}

// GetInstalledApps is unsupported off Windows.
func GetInstalledApps(ctx context.Context) ([]model.InstalledApp, error) {
	return nil, fmt.Errorf("native installed apps require Windows")
}

// GetSoftwareSnapshot is unsupported off Windows.
func GetSoftwareSnapshot(ctx context.Context, installedApps []model.InstalledApp) (model.SoftwareSnapshot, error) {
	return model.SoftwareSnapshot{}, fmt.Errorf("native software snapshot requires Windows")
}

// IsAdministrator is unsupported off Windows.
func IsAdministrator() (bool, error) {
	return false, fmt.Errorf("IsAdministrator requires Windows")
}
