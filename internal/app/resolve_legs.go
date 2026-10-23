package app

// The two parallel legs of resolveRuntime, split out of app.go so the run
// loop stays under the file ceiling. The network leg decides the exit code;
// the local leg keeps its degrade-to-empty policy. Each leg logs its own
// progress, so the joined log interleaves but every line names its leg.

import (
	"context"

	"lenovo-driver/internal/api"
	"lenovo-driver/internal/inventory"
	"lenovo-driver/internal/model"
)

// resolveLenovoIdentity is the network leg of resolveRuntime: the machine
// category lookup followed by the OS entry resolution. It returns the
// PS1-style exit code (0 on success).
func (a *App) resolveLenovoIdentity(ctx context.Context, opts *Options, machine inventory.MachineInfo, osInfo inventory.OSInfo) (string, *api.OSResolution, int) {
	categoryID, err := a.APIClient.ResolveCategoryID(ctx, firstNonEmpty(opts.Model, machine.Model), machine.Serial)
	if err != nil || categoryID == "" {
		reason := ""
		if err != nil {
			reason = err.Error()
		}
		a.Log(ctx, "Could not resolve the Lenovo machine category. Use -Model \"82JQ\" if the automatic lookup fails: "+reason, "ERROR")
		return "", nil, 1
	}
	a.Log(ctx, "Lenovo category ID : "+categoryID, "INFO")

	osResolution, err := a.APIClient.ResolveOSEntry(ctx, categoryID, osInfo.OSName, osInfo.Kind)
	if err != nil || osResolution == nil {
		a.Log(ctx, "Could not resolve the current OS entry from Lenovo. Use -LatestAcrossOS only after confirming the OS list is complete.", "ERROR")
		return "", nil, 1
	}
	a.Log(ctx, "Matched OS entry : "+osResolution.OSEntry.OSName+" (OSID "+osResolution.OSEntry.OSID+")", "INFO")
	a.logOSResolution(ctx, osResolution)
	return categoryID, osResolution, 0
}

// localSnapshots is the local leg of resolveRuntime: device, installed-app,
// and software snapshots. Each read degrades to empty on failure; none of
// them is a hard prerequisite.
func (a *App) localSnapshots(ctx context.Context) ([]model.Device, []model.InstalledApp, model.SoftwareSnapshot) {
	localDevices, err := inventory.GetLocalDeviceSnapshot(ctx)
	if err != nil {
		a.Log(ctx, "Local device snapshot failed: "+err.Error(), "WARN")
		localDevices = nil
	}
	installedApps, err := inventory.GetInstalledApps(ctx)
	if err != nil {
		a.Log(ctx, "Installed application snapshot failed: "+err.Error(), "WARN")
		installedApps = nil
	}
	softwareSnapshot, err := inventory.GetSoftwareSnapshot(ctx, installedApps)
	if err != nil {
		a.Log(ctx, "Software snapshot failed: "+err.Error(), "WARN")
	}
	return localDevices, installedApps, softwareSnapshot
}
