package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"lenovo-driver/internal/api"
	"lenovo-driver/internal/audit"
	"lenovo-driver/internal/compare"
	"lenovo-driver/internal/inventory"
	"lenovo-driver/internal/model"
	"lenovo-driver/internal/plan"
)

// DriverView is the comparable selected driver set.
type DriverView struct {
	Selected   []*model.Driver
	Applicable []*model.Driver
	Updates    []*model.Driver
	Source     string
	OsID       string
}

var (
	reBios          = regexp.MustCompile(`(?i)BIOS|EC Version`)
	installableExts = map[string]bool{".exe": true, ".msi": true, ".zip": true, ".inf": true, ".cab": true}
)

func (a *App) cachedDriverObjects(ctx context.Context, vc *ViewContext, osID, preferredSource string) (api.SourceDrivers, error) {
	key := osID + "|" + preferredSource
	a.osDriverCacheMu.Lock()
	if a.osDriverCache == nil {
		a.osDriverCache = map[string]api.SourceDrivers{}
	} else if result, ok := a.osDriverCache[key]; ok {
		a.osDriverCacheMu.Unlock()
		return result, nil
	}
	a.osDriverCacheMu.Unlock()

	result, err := a.APIClient.GetDriverObjects(ctx, vc.CategoryID, osID, preferredSource)
	if err != nil {
		return api.SourceDrivers{}, err
	}
	a.osDriverCacheMu.Lock()
	a.osDriverCache[key] = result
	a.osDriverCacheMu.Unlock()
	return result, nil
}

// toleratedDriverListMiss reports why a driver list is unusable under the soft-
// miss policy: a fetch error, or an official list with no rows. A nil return
// means the list is usable. Both the cross-OS merge and the alternate-OS source
// map share this single fetch/tolerate policy.
func toleratedDriverListMiss(err error, result api.SourceDrivers) error {
	if err != nil {
		return err
	}
	if len(result.Drivers) == 0 {
		return fmt.Errorf("the official driver list was empty")
	}
	return nil
}

// driverListLoad is one OSID's fetch outcome, kept in input order so that
// callers can apply their per-OSID policy deterministically after the parallel
// fetch phase completes.
type driverListLoad struct {
	osID   string
	result api.SourceDrivers
	err    error
}

// loadDriverListsForOSIDs fetches the cached driver lists for a group of
// independent OSIDs in parallel (the reads are unrelated network calls that
// only benefit from concurrency), preserving the input order in the returned
// slice. The soft-miss policy is applied by the caller per slot in order.
func (a *App) loadDriverListsForOSIDs(ctx context.Context, vc *ViewContext, osIDs []string, preferredSource string) []driverListLoad {
	out := make([]driverListLoad, len(osIDs))
	for i, osID := range osIDs {
		out[i].osID = osID
	}
	var wg sync.WaitGroup
	for i, osID := range osIDs {
		wg.Add(1)
		go func(i int, osID string) {
			defer wg.Done()
			out[i].result, out[i].err = a.cachedDriverObjects(ctx, vc, osID, preferredSource)
		}(i, osID)
	}
	wg.Wait()
	return out
}

// loadDriverList returns the cached driver list for an OSID, logging a WARN and
// returning ok=false when the source is unavailable or empty. Both the
// cross-OS merge and the alternate-OS source map share this single
// fetch/tolerate policy.
func (a *App) loadDriverList(ctx context.Context, vc *ViewContext, osID, preferredSource string) (api.SourceDrivers, bool) {
	result, err := a.cachedDriverObjects(ctx, vc, osID, preferredSource)
	if miss := toleratedDriverListMiss(err, result); miss != nil {
		a.Log(ctx, "Could not load a driver list for OSID "+osID+": "+miss.Error(), "WARN")
		return api.SourceDrivers{}, false
	}
	return result, true
}

// pnpIDsOf collects the non-empty PnP device ids of a device set.
func pnpIDsOf(devices []model.Device) []string {
	ids := make([]string, 0, len(devices))
	for _, dev := range devices {
		if dev.PnpDeviceID != "" {
			ids = append(ids, dev.PnpDeviceID)
		}
	}
	return ids
}

// localDriverState resolves the current local version and vendor for a driver
// against the pre-fetched per-device version index. It is the single canonical
// pipeline shared by the driver-comparison view and post-install verification;
// the index itself is fetched once per pass by deviceVersionIndex.
func (a *App) localDriverState(driver *model.Driver, matchedDevices []model.Device, versionIndex map[string]string, snapshot *model.SoftwareSnapshot) (localVersion, localVendor string) {
	versions := make([]string, 0, len(matchedDevices))
	for _, dev := range matchedDevices {
		versions = append(versions, versionIndex[dev.PnpDeviceID])
	}
	return compare.ResolveLocalDriverVersion(driver, matchedDevices, versions, snapshot)
}

// deviceVersionIndex resolves the driver version of every matched device
// across the applicable drivers in a single inventory pass, keyed by PnP
// device ID. A fetch failure aborts the pass: a whole-tree enumeration that
// fails for one driver fails for all of them.
func (a *App) deviceVersionIndex(ctx context.Context, localDevices []model.Device, selected []*model.Driver) (map[string]string, error) {
	var ids []string
	seen := make(map[string]struct{})
	for _, driver := range selected {
		if driver == nil || !compare.TestDriverApplicable(driver, localDevices) {
			continue
		}
		for _, id := range pnpIDsOf(compare.GetMatchingLocalDevices(driver, localDevices)) {
			if _, dup := seen[id]; dup {
				continue
			}
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return map[string]string{}, nil
	}
	versions, err := inventory.GetDeviceDriverVersions(ctx, ids)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]string, len(ids))
	for i, id := range ids {
		byID[id] = versions[i]
	}
	return byID, nil
}

// mustDriverList is the strict counterpart of loadDriverList: the primary
// comparison view cannot proceed without the official list, so an empty or
// failed load is a hard error rather than a tolerated soft miss.
func (a *App) mustDriverList(ctx context.Context, vc *ViewContext, osID, preferredSource string) (api.SourceDrivers, error) {
	driverResult, err := a.cachedDriverObjects(ctx, vc, osID, preferredSource)
	if err != nil {
		return api.SourceDrivers{}, fmt.Errorf("could not load the official driver list from %s or the webpage API: %w", preferredSource, err)
	}
	if miss := toleratedDriverListMiss(nil, driverResult); miss != nil {
		return api.SourceDrivers{}, fmt.Errorf("could not load the official driver list from %s or the webpage API: %w", preferredSource, miss)
	}
	return driverResult, nil
}

// CompareOSDriverView mirrors Compare-OsDriverView.
func (a *App) CompareOSDriverView(ctx context.Context, vc *ViewContext, listOsID string) (*DriverView, error) {
	driverResult, err := a.mustDriverList(ctx, vc, listOsID, "QuickFix")
	if err != nil {
		return nil, err
	}
	viewDrivers := append([]*model.Driver(nil), driverResult.Drivers...)
	a.Log(ctx, "Driver source : "+driverResult.Source+" (OSID "+listOsID+")", "INFO")

	if vc.Opts.LatestAcrossOS {
		alternateIDs := otherOSIDs(vc.OsList, listOsID)
		// The alternate OS lists are independent reads; fetch them in parallel,
		// then merge and log in deterministic OSID order.
		for _, load := range a.loadDriverListsForOSIDs(ctx, vc, alternateIDs, driverResult.Source) {
			if miss := toleratedDriverListMiss(load.err, load.result); miss != nil {
				a.Log(ctx, "Could not load a driver list for OSID "+load.osID+": "+miss.Error(), "WARN")
				continue
			}
			viewDrivers = append(viewDrivers, load.result.Drivers...)
			a.Log(ctx, "Merged driver source : "+load.result.Source+" (OSID "+load.osID+")", "INFO")
		}
	}

	// Own each row before filtering/selecting/assessing: assessment writes
	// (compare status, source audit, ...) must never reach the shared driver
	// list cache or API rows, so the transport/API DTOs stay immutable.
	viewDrivers = cloneDrivers(viewDrivers)

	viewDrivers = filterDriverRows(viewDrivers, vc.Opts.IncludeBios)
	selected := compare.SelectLatestDrivers(viewDrivers, listOsID)
	a.Log(ctx, fmt.Sprintf("Drivers selected : %d", len(selected)), "INFO")
	a.Log(ctx, "Comparing with locally installed versions...", "INFO")

	currentSourceMap := buildDriverSourceMap(selected)
	a.assessSelectedDrivers(ctx, vc, selected, currentSourceMap, driverResult.Source)

	a.presentDriverView(ctx, selected)

	applicable, updates := partitionViewDrivers(selected)
	a.Log(ctx, fmt.Sprintf("Applicable candidates : %d; update-only drivers : %d", len(applicable), len(updates)), "INFO")
	return &DriverView{
		Selected:   selected,
		Applicable: applicable,
		Updates:    updates,
		Source:     driverResult.Source,
		OsID:       listOsID,
	}, nil
}

// presentDriverView writes the plan artifact and prints the comparison table
// and status summary for an assessed driver set. It is the presentation
// counterpart of the pure comparison/build pipeline and is the single place
// that couples the decision result to stdout and the plan file.
func (a *App) presentDriverView(ctx context.Context, selected []*model.Driver) {
	if err := a.WritePlanFile(selected); err != nil {
		a.Log(ctx, "Could not write plan file: "+err.Error(), "WARN")
	} else {
		a.Log(ctx, "Plan file : "+a.PlanPath, "INFO")
	}
	fmt.Fprintln(a.Stdout)
	fmt.Fprintln(a.Stdout, "Driver version comparison:")
	for _, line := range plan.FormatDriverTableLines(selected) {
		fmt.Fprintln(a.Stdout, line)
	}
	for _, line := range plan.FormatStatusSummaryLines(selected) {
		fmt.Fprintln(a.Stdout, line)
	}
}

// assessSelectedDrivers resolves the local version, compare status and
// source-evidence audit for each driver, mutating only that driver's
// comparison fields. The local version index is fetched once for all
// applicable drivers instead of per driver. Any alternate-source map needed
// for source auditing is built lazily and reused across the remaining drivers
// in the pass.
func (a *App) assessSelectedDrivers(ctx context.Context, vc *ViewContext, selected []*model.Driver, currentSourceMap map[string]model.SourceMapEntry, preferredSource string) {
	var alternateSourceMap map[string]model.SourceMapEntry
	versionIndex, err := a.deviceVersionIndex(ctx, vc.LocalDevices, selected)
	if err != nil {
		a.Log(ctx, "Could not read local driver versions: "+err.Error(), "WARN")
		return
	}
	for _, driver := range selected {
		if driver == nil {
			continue
		}
		if !compare.TestDriverApplicable(driver, vc.LocalDevices) {
			driver.CompareStatus = model.StatusNotApplicable
			continue
		}
		matchedDevices := compare.GetMatchingLocalDevices(driver, vc.LocalDevices)
		localVersion, localVendor := a.localDriverState(driver, matchedDevices, versionIndex, &vc.SoftwareSnapshot)
		driver.LocalVersion = localVersion
		driver.LocalVendor = localVendor
		driver.CompareStatus = compare.CompareDriverStatus(driver.Version, localVersion, localVendor)
		if localVersion == "" {
			continue
		}
		if driver.CompareStatus == model.StatusLocalNewer && alternateSourceMap == nil {
			alternateSourceMap = a.initAlternateSourceMap(ctx, vc, vc.CurrentSystemOsID, preferredSource)
		}
		driver.SourceAudit = a.resolveDriverSourceAudit(ctx, driver, matchedDevices, vc.History, currentSourceMap, alternateSourceMap)
		if driver.CompareStatus == model.StatusLocalNewer {
			driver.CompareSource = audit.ResolveDriverSourceLabel(driver, vc.History, alternateSourceMap, driver.SourceAudit)
			a.Log(ctx, "["+driver.DriverCode+"] "+driver.CompareSource, "WARN")
		}
	}
}

// partitionViewDrivers splits the selected drivers into the applicable and
// update-only groups used by the interactive view.
func partitionViewDrivers(selected []*model.Driver) (applicable, updates []*model.Driver) {
	for _, driver := range selected {
		if driver.CompareStatus != model.StatusNotApplicable {
			applicable = append(applicable, driver)
		}
		if driver.CompareStatus == model.StatusUpdate {
			updates = append(updates, driver)
		}
	}
	return applicable, updates
}

// cloneDriver deep-copies a driver row so the comparison/assessment pass can
// write comparison fields without mutating a shared fetch/cache object. It
// preserves the row value including its nested source audit.
func cloneDriver(d *model.Driver) *model.Driver {
	if d == nil {
		return nil
	}
	c := *d
	if d.SourceAudit != nil {
		a := *d.SourceAudit
		a.EvidenceLines = append([]string(nil), d.SourceAudit.EvidenceLines...)
		c.SourceAudit = &a
	}
	return &c
}

// cloneDrivers returns a slice of independent deep copies of the rows. The
// comparison view owns its copies, so nothing it writes leaks back into the
// shared driver list cache or the API transport rows.
func cloneDrivers(drivers []*model.Driver) []*model.Driver {
	out := make([]*model.Driver, len(drivers))
	for i, d := range drivers {
		out[i] = cloneDriver(d)
	}
	return out
}

func filterDriverRows(rows []*model.Driver, includeBios bool) []*model.Driver {
	var out []*model.Driver
	biosSkipped := 0
	extSkipped := 0
	for _, driver := range rows {
		if driver == nil {
			continue
		}
		if driver.Disabled() {
			continue
		}
		if !includeBios && reBios.MatchString(driver.DriverName) {
			biosSkipped++
			continue
		}
		if !installableExts[strings.ToLower(filepath.Ext(driver.FileName))] {
			extSkipped++
			continue
		}
		out = append(out, driver)
	}
	return out
}

// otherOSIDs returns the OSIDs of every entry in osList except excludeID, in
// list order. It is the single source of truth for "which OS lists are the
// alternates for the current one" shared by the cross-OS merge, the
// alternate-source map, and URL refresh.
func otherOSIDs(osList []model.OSListEntry, excludeID string) []string {
	out := make([]string, 0, len(osList))
	for _, entry := range osList {
		if entry.OSID == excludeID {
			continue
		}
		out = append(out, entry.OSID)
	}
	return out
}

func buildDriverSourceMap(drivers []*model.Driver) map[string]model.SourceMapEntry {
	sourceMap := map[string]model.SourceMapEntry{}
	for _, driver := range drivers {
		if driver == nil {
			continue
		}
		for _, key := range audit.SourceMapKeys(driver.DriverName, driver.DriverCode, driver.Version) {
			if _, ok := sourceMap[key]; !ok {
				sourceMap[key] = model.SourceMapEntry{OSID: driver.OSID, OSName: driver.OSName}
			}
		}
	}
	return sourceMap
}

func (a *App) initAlternateSourceMap(ctx context.Context, vc *ViewContext, sysID, preferredSource string) map[string]model.SourceMapEntry {
	sourceMap := map[string]model.SourceMapEntry{}
	alternateIDs := otherOSIDs(vc.OsList, sysID)
	// Fetch the alternate OS lists in parallel; merge the source map in OSID
	// order so the existing first-wins tie-break is preserved deterministically.
	for _, load := range a.loadDriverListsForOSIDs(ctx, vc, alternateIDs, preferredSource) {
		if toleratedDriverListMiss(load.err, load.result) != nil {
			continue
		}
		for key, entry := range buildDriverSourceMap(load.result.Drivers) {
			if _, ok := sourceMap[key]; !ok {
				sourceMap[key] = entry
			}
		}
	}
	return sourceMap
}

func (a *App) resolveDriverSourceAudit(
	ctx context.Context,
	driver *model.Driver,
	matchedDevices []model.Device,
	history []model.HistoryRecord,
	currentSourceMap map[string]model.SourceMapEntry,
	alternateSourceMap map[string]model.SourceMapEntry,
) *model.SourceAudit {
	deviceEvidence := a.enrichDeviceEvidence(ctx, matchedDevices)
	auditResult := audit.ResolveDriverSourceEvidence(driver, deviceEvidence, history, alternateSourceMap, currentSourceMap)
	return &auditResult
}

// WritePlanFile mirrors Write-PlanFile.
func (a *App) WritePlanFile(drivers []*model.Driver) error {
	lines := plan.BuildPlanText(drivers, formatTimestamp(time.Now()))
	content := strings.Join(lines, "\r\n") + "\r\n"
	return os.WriteFile(a.PlanPath, []byte(content), 0o644)
}
