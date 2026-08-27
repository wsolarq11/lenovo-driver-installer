package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

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

// CompareOSDriverView mirrors Compare-OsDriverView.
func (a *App) CompareOSDriverView(
	ctx context.Context,
	opts *Options,
	categoryID string,
	listOsID string,
	currentSystemOsID string,
	osList []model.OSListEntry,
	localDevices []model.Device,
	installedApps []model.InstalledApp,
	softwareSnapshot model.SoftwareSnapshot,
	history []model.HistoryRecord,
) (*DriverView, error) {
	driverResult := a.APIClient.GetDriverObjects(ctx, categoryID, listOsID, "QuickFix")
	if driverResult.Source == "" || len(driverResult.Drivers) == 0 {
		return nil, fmt.Errorf("could not load the official driver list from QuickFix or the webpage API")
	}
	viewDrivers := append([]*model.Driver(nil), driverResult.Drivers...)
	a.Log(ctx, "Driver source : "+driverResult.Source+" (OSID "+listOsID+")", "INFO")

	if opts.LatestAcrossOS {
		for _, entry := range osList {
			if entry.OSID == listOsID {
				continue
			}
			alternate := a.APIClient.GetDriverObjects(ctx, categoryID, entry.OSID, driverResult.Source)
			if alternate.Source == "" || len(alternate.Drivers) == 0 {
				a.Log(ctx, "Could not merge OSID "+entry.OSID+" ("+entry.OSName+"): "+alternate.Source, "WARN")
				continue
			}
			viewDrivers = append(viewDrivers, alternate.Drivers...)
			a.Log(ctx, "Merged driver source : "+alternate.Source+" (OSID "+entry.OSID+")", "INFO")
		}
	}

	viewDrivers = filterDriverRows(viewDrivers, opts.IncludeBios)
	selected := compare.SelectLatestDrivers(viewDrivers, listOsID)
	a.Log(ctx, fmt.Sprintf("Drivers selected : %d", len(selected)), "INFO")
	a.Log(ctx, "Comparing with locally installed versions...", "INFO")

	currentSourceMap := initCurrentSourceMap(selected)
	var alternateSourceMap map[string]model.SourceMapEntry

	for _, driver := range selected {
		if driver == nil {
			continue
		}
		if !compare.TestDriverApplicable(driver, localDevices) {
			driver.CompareStatus = "Not applicable"
			continue
		}
		matchedDevices := compare.GetMatchingLocalDevices(driver, localDevices)
		var pnpIDs []string
		for _, dev := range matchedDevices {
			if dev.PnpDeviceID != "" {
				pnpIDs = append(pnpIDs, dev.PnpDeviceID)
			}
		}
		driverVersions, _ := inventory.GetDeviceDriverVersions(ctx, pnpIDs)
		localVersion, localVendor := compare.ResolveLocalDriverVersion(driver, matchedDevices, driverVersions, &softwareSnapshot)
		driver.LocalVersion = localVersion
		driver.LocalVendor = localVendor
		driver.CompareStatus = compare.CompareDriverStatus(driver.Version, localVersion, localVendor)
		if localVersion != "" {
			if driver.CompareStatus == "Local newer" && alternateSourceMap == nil {
				alternateSourceMap = a.initAlternateSourceMap(ctx, categoryID, osList, currentSystemOsID, driverResult.Source)
			}
			driver.SourceAudit = a.resolveDriverSourceAudit(ctx, driver, matchedDevices, history, currentSourceMap, alternateSourceMap)
			if driver.CompareStatus == "Local newer" {
				driver.CompareSource = audit.ResolveDriverSourceLabel(driver, history, alternateSourceMap, driver.SourceAudit)
				a.Log(ctx, "["+driver.DriverCode+"] "+driver.CompareSource, "WARN")
			}
		}
	}

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

	var applicable []*model.Driver
	var updates []*model.Driver
	for _, driver := range selected {
		if driver.CompareStatus != "Not applicable" {
			applicable = append(applicable, driver)
		}
		if driver.CompareStatus == "Update" {
			updates = append(updates, driver)
		}
	}
	a.Log(ctx, fmt.Sprintf("Applicable candidates : %d; update-only drivers : %d", len(applicable), len(updates)), "INFO")
	return &DriverView{
		Selected:   selected,
		Applicable: applicable,
		Updates:    updates,
		Source:     driverResult.Source,
		OsID:       listOsID,
	}, nil
}

func filterDriverRows(rows []*model.Driver, includeBios bool) []*model.Driver {
	installableExts := map[string]bool{".exe": true, ".msi": true, ".zip": true, ".inf": true, ".cab": true}
	var out []*model.Driver
	biosSkipped := 0
	extSkipped := 0
	reBios := regexp.MustCompile(`(?i)BIOS|EC Version`)
	for _, driver := range rows {
		if driver == nil {
			continue
		}
		if (driver.Status != "" && driver.Status == "0") || (driver.IsEnable != "" && driver.IsEnable == "0") {
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

func initCurrentSourceMap(drivers []*model.Driver) map[string]model.SourceMapEntry {
	sourceMap := map[string]model.SourceMapEntry{}
	for _, driver := range drivers {
		for _, versionKey := range compare.GetVersionMatchKeys(driver.Version) {
			nameKey := strings.TrimSpace(driver.DriverName) + "|" + versionKey
			codeKey := driver.DriverCode + "|" + versionKey
			if _, ok := sourceMap[nameKey]; !ok {
				sourceMap[nameKey] = model.SourceMapEntry{OSID: driver.OSID, OSName: driver.OSName}
			}
			if _, ok := sourceMap[codeKey]; !ok {
				sourceMap[codeKey] = model.SourceMapEntry{OSID: driver.OSID, OSName: driver.OSName}
			}
		}
	}
	return sourceMap
}

func (a *App) initAlternateSourceMap(ctx context.Context, categoryID string, osList []model.OSListEntry, sysID, preferredSource string) map[string]model.SourceMapEntry {
	sourceMap := map[string]model.SourceMapEntry{}
	for _, alt := range osList {
		if alt.OSID == sysID {
			continue
		}
		result := a.APIClient.GetDriverObjects(ctx, categoryID, alt.OSID, preferredSource)
		for _, altDriver := range result.Drivers {
			for _, versionKey := range compare.GetVersionMatchKeys(altDriver.Version) {
				nameKey := strings.TrimSpace(altDriver.DriverName) + "|" + versionKey
				codeKey := altDriver.DriverCode + "|" + versionKey
				if _, ok := sourceMap[nameKey]; !ok {
					sourceMap[nameKey] = model.SourceMapEntry{OSID: altDriver.OSID, OSName: altDriver.OSName}
				}
				if _, ok := sourceMap[codeKey]; !ok {
					sourceMap[codeKey] = model.SourceMapEntry{OSID: altDriver.OSID, OSName: altDriver.OSName}
				}
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
	lines := plan.BuildPlanText(drivers, time.Now().Format("2006-01-02 15:04:05"))
	content := strings.Join(lines, "\r\n") + "\r\n"
	return os.WriteFile(a.PlanPath, []byte(content), 0o644)
}

// guiExportPayload matches Export-LenovoDriverViewJson.
type guiExportPayload struct {
	GeneratedAt   string              `json:"GeneratedAt"`
	MachineModel  string              `json:"MachineModel"`
	SerialNumber  string              `json:"SerialNumber"`
	SystemCaption string              `json:"SystemCaption"`
	CurrentOsID   string              `json:"CurrentOsId"`
	CurrentOsName string              `json:"CurrentOsName"`
	ListOsID      string              `json:"ListOsId"`
	ListOsName    string              `json:"ListOsName"`
	DataSource    string              `json:"DataSource"`
	OsList        []model.OSListEntry `json:"OsList"`
	Drivers       []guiDriverRow      `json:"Drivers"`
}

type guiDriverRow struct {
	Selected      bool   `json:"Selected"`
	DriverCode    string `json:"DriverCode"`
	DriverName    string `json:"DriverName"`
	Version       string `json:"Version"`
	LocalVersion  string `json:"LocalVersion"`
	CompareStatus string `json:"CompareStatus"`
	SourceAudit   string `json:"SourceAudit"`
	CompareSource string `json:"CompareSource"`
	FileName      string `json:"FileName"`
	FilePath      string `json:"FilePath"`
	FileSize      string `json:"FileSize"`
	MD5           string `json:"MD5"`
	IsApplicable  bool   `json:"IsApplicable"`
	IsUpdate      bool   `json:"IsUpdate"`
}

// ExportGUIView writes the WPF-compatible JSON view.
func (a *App) ExportGUIView(path string, view *DriverView, osList []model.OSListEntry, currentSystemOsID string, machine inventory.MachineInfo, osInfo inventory.OSInfo) error {
	currentOSName := ""
	listOSName := ""
	for _, entry := range osList {
		if entry.OSID == currentSystemOsID {
			currentOSName = entry.OSName
		}
		if entry.OSID == view.OsID {
			listOSName = entry.OSName
		}
	}
	payload := guiExportPayload{
		GeneratedAt:   time.Now().Format("2006-01-02 15:04:05"),
		MachineModel:  machine.Model,
		SerialNumber:  machine.Serial,
		SystemCaption: osInfo.Caption,
		CurrentOsID:   currentSystemOsID,
		CurrentOsName: currentOSName,
		ListOsID:      view.OsID,
		ListOsName:    listOSName,
		DataSource:    view.Source,
		OsList:        osList,
		Drivers:       make([]guiDriverRow, 0, len(view.Selected)),
	}
	for _, driver := range view.Selected {
		sourceAudit := ""
		if driver.SourceAudit != nil {
			sourceAudit = driver.SourceAudit.Category + ": " + driver.SourceAudit.Summary
		}
		payload.Drivers = append(payload.Drivers, guiDriverRow{
			DriverCode:    driver.DriverCode,
			DriverName:    driver.DriverName,
			Version:       driver.Version,
			LocalVersion:  driver.LocalVersion,
			CompareStatus: driver.CompareStatus,
			SourceAudit:   sourceAudit,
			CompareSource: driver.CompareSource,
			FileName:      driver.FileName,
			FilePath:      driver.FilePath,
			FileSize:      driver.FileSize,
			MD5:           driver.OfficialMD5,
			IsApplicable:  driver.CompareStatus != "Not applicable",
			IsUpdate:      driver.CompareStatus == "Update",
		})
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
