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
	Selected   []*model.AssessedDriver
	Applicable []*model.AssessedDriver
	Updates    []*model.AssessedDriver
	Source     string
	OsID       string
}

var (
	// reFirmware names firmware-flashing packages. It deliberately covers more
	// than BIOS/EC because Management Engine, TPM, Thunderbolt, and generic
	// "Firmware" packages flash non-driver firmware and are just as risky to
	// run silently.
	//
	// Lenovo driver rows expose no firmware category field (only FileType
	// exe/inf/zip/cab plus Bootfile), so this name match is the current
	// evidence-based heuristic, not a durable classification. If a future API
	// adds a category field, switch this filter to that field instead.
	reFirmware      = regexp.MustCompile(`(?i)\bBIOS\b|\bUEFI\b|\bFirmware\b|\bEC\b|\bManagement Engine\b|\bTPM\b|\bThunderbolt\b`)
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

// matchedByDriver resolves the matching local devices for each applicable
// assessed driver, returning the per-driver lists (index-aligned with
// assessed) and the deduped union of all matched devices. This is the single
// place that walks the "applicable drivers and their matched PnP devices"
// traversal; the version index and the evidence audit both consume its result
// instead of re-walking the same assessed set.
func matchedByDriver(assessed []*model.AssessedDriver, localDevices []model.Device) (perDriver [][]model.Device, union []model.Device) {
	perDriver = make([][]model.Device, len(assessed))
	seen := make(map[string]struct{})
	for i, ad := range assessed {
		if ad == nil || ad.Driver == nil || !compare.TestDriverApplicable(ad.Driver, localDevices) {
			continue
		}
		matched := compare.GetMatchingLocalDevices(ad.Driver, localDevices)
		perDriver[i] = matched
		for _, device := range matched {
			if device.PnpDeviceID == "" {
				continue
			}
			if _, dup := seen[device.PnpDeviceID]; dup {
				continue
			}
			seen[device.PnpDeviceID] = struct{}{}
			union = append(union, device)
		}
	}
	return perDriver, union
}

// deviceVersionIndex resolves the driver version of every PnP device in the
// given deduped device set in a single inventory pass, keyed by PnP device ID.
// A fetch failure aborts the pass: a whole-tree enumeration that fails for one
// driver fails for all of them.
func (a *App) deviceVersionIndex(ctx context.Context, devices []model.Device) (map[string]string, error) {
	ids := pnpIDsOf(devices)
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

	viewDrivers = filterDriverRows(viewDrivers, vc.Opts.IncludeBios)
	selected := compare.SelectLatestDrivers(viewDrivers, listOsID)
	a.Log(ctx, fmt.Sprintf("Drivers selected : %d", len(selected)), "INFO")
	a.Log(ctx, "Comparing with locally installed versions...", "INFO")

	currentSourceMap := buildDriverSourceMap(selected)
	assessed := a.assessSelectedDrivers(ctx, vc, selected, currentSourceMap, driverResult.Source)

	a.presentDriverView(ctx, assessed)

	applicable, updates := partitionViewDrivers(assessed)
	a.Log(ctx, fmt.Sprintf("Applicable candidates : %d; update-only drivers : %d", len(applicable), len(updates)), "INFO")
	return &DriverView{
		Selected:   assessed,
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
func (a *App) presentDriverView(ctx context.Context, selected []*model.AssessedDriver) {
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

// assessSelectedDrivers wraps each selected transport row in an
// AssessedDriver, then resolves the local version, compare status and
// source-evidence audit per assessed driver. The matched-device traversal,
// the local version index, and the PnP evidence enumeration are each performed
// once over the pass instead of once per driver (see matchedByDriver,
// deviceVersionIndex and buildDeviceEvidenceMap). Any alternate-source map
// needed for source auditing is built lazily and reused across the remaining
// drivers in the pass. Assessment writes land only on each AssessedDriver's
// owned DriverAssessment copy, never on the shared transport rows.
func (a *App) assessSelectedDrivers(ctx context.Context, vc *ViewContext, selected []*model.Driver, currentSourceMap map[string]model.SourceMapEntry, preferredSource string) []*model.AssessedDriver {
	out := make([]*model.AssessedDriver, len(selected))
	for i, d := range selected {
		if d == nil {
			continue
		}
		out[i] = &model.AssessedDriver{Driver: d}
	}

	var alternateSourceMap map[string]model.SourceMapEntry
	matchedForDriver, evidenceUnion := matchedByDriver(out, vc.LocalDevices)
	versionIndex, err := a.deviceVersionIndex(ctx, evidenceUnion)
	if err != nil {
		a.Log(ctx, "Could not read local driver versions: "+err.Error(), "WARN")
		return out
	}

	var evidenceByID map[string]model.Device
	evidenceBuilt := false
	for i, ad := range out {
		if ad == nil || ad.Driver == nil {
			continue
		}
		driver := ad.Driver
		if !compare.TestDriverApplicable(driver, vc.LocalDevices) {
			ad.CompareStatus = model.StatusNotApplicable
			ad.NonMatchReason = compare.DiagnoseDriverNonMatch(driver, vc.LocalDevices)
			continue
		}
		matchedDevices := matchedForDriver[i]
		localVersion, localVendor := a.localDriverState(driver, matchedDevices, versionIndex, &vc.SoftwareSnapshot)
		ad.LocalVersion = localVersion
		ad.LocalVendor = localVendor
		ad.CompareStatus = compare.CompareDriverStatus(driver.Version, localVersion, localVendor)
		if localVersion == "" {
			continue
		}
		if ad.CompareStatus == model.StatusLocalNewer && alternateSourceMap == nil {
			alternateSourceMap = a.initAlternateSourceMap(ctx, vc, vc.CurrentSystemOsID, preferredSource)
		}
		if !evidenceBuilt {
			evidenceByID, err = a.buildDeviceEvidenceMap(ctx, evidenceUnion)
			if err != nil {
				a.Log(ctx, "Device evidence lookup failed: "+err.Error(), "WARN")
				evidenceByID = nil
			}
			evidenceBuilt = true
		}
		ad.SourceAudit = a.resolveDriverSourceAudit(ad, matchedDevices, evidenceByID, vc.History, currentSourceMap, alternateSourceMap)
		ad.DeviceProblem = model.DeviceProblemSummary(projectMatchedEvidence(matchedDevices, evidenceByID))
		ad.BeforeInfName = firstInfName(projectMatchedEvidence(matchedDevices, evidenceByID))
		if ad.CompareStatus == model.StatusLocalNewer {
			ad.CompareSource = audit.ResolveDriverSourceLabel(ad, vc.History, alternateSourceMap, ad.SourceAudit)
			a.Log(ctx, "["+driver.DriverCode+"] "+ad.CompareSource, "WARN")
		}
	}
	return out
}

// partitionViewDrivers splits the assessed drivers into the applicable and
// update-only groups used by the interactive view.
func partitionViewDrivers(selected []*model.AssessedDriver) (applicable, updates []*model.AssessedDriver) {
	for _, ad := range selected {
		if ad == nil || ad.Driver == nil {
			continue
		}
		if ad.CompareStatus != model.StatusNotApplicable {
			applicable = append(applicable, ad)
		}
		if ad.CompareStatus == model.StatusUpdate {
			updates = append(updates, ad)
		}
	}
	return applicable, updates
}

func filterDriverRows(rows []*model.Driver, includeBios bool) []*model.Driver {
	var out []*model.Driver
	firmwareSkipped := 0
	extSkipped := 0
	for _, driver := range rows {
		if driver == nil {
			continue
		}
		if driver.Disabled() {
			continue
		}
		if !includeBios && reFirmware.MatchString(driver.DriverName) {
			firmwareSkipped++
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
	ad *model.AssessedDriver,
	matchedDevices []model.Device,
	evidenceByID map[string]model.Device,
	history []model.HistoryRecord,
	currentSourceMap map[string]model.SourceMapEntry,
	alternateSourceMap map[string]model.SourceMapEntry,
) *model.SourceAudit {
	deviceEvidence := projectMatchedEvidence(matchedDevices, evidenceByID)
	auditResult := audit.ResolveDriverSourceEvidence(ad, deviceEvidence, history, alternateSourceMap, currentSourceMap)
	return &auditResult
}

// WritePlanFile mirrors Write-PlanFile. The UTF-8 BOM is deliberate: the plan
// is opened in Notepad on Chinese-locale Windows, and without the BOM Notepad
// decodes the UTF-8 bytes as GBK, garbling every Chinese driver name.
func (a *App) WritePlanFile(drivers []*model.AssessedDriver) error {
	lines := plan.BuildPlanText(drivers, formatTimestamp(time.Now()))
	content := "\uFEFF" + strings.Join(lines, "\r\n") + "\r\n"
	return os.WriteFile(a.PlanPath, []byte(content), 0o644)
}
