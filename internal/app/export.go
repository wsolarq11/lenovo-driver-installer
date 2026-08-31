package app

import (
	"encoding/json"
	"os"
	"time"

	"lenovo-driver/internal/model"
)

// This file owns the WPF-compatible JSON view export. Splitting the export
// concern out of view.go keeps the comparison/view pipeline file under the
// 500-line ceiling while the payload spec stays one hop away from the driver
// view it serializes.

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

// guiDriverRow is the WPF-facing projection of one assessed driver.
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
func (a *App) ExportGUIView(path string, vc *ViewContext, view *DriverView) error {
	currentOSName := model.OSNameByID(vc.OsList, vc.CurrentSystemOsID)
	listOSName := model.OSNameByID(vc.OsList, view.OsID)
	payload := guiExportPayload{
		GeneratedAt:   formatTimestamp(time.Now()),
		MachineModel:  vc.Machine.Model,
		SerialNumber:  vc.Machine.Serial,
		SystemCaption: vc.OSInfo.Caption,
		CurrentOsID:   vc.CurrentSystemOsID,
		CurrentOsName: currentOSName,
		ListOsID:      view.OsID,
		ListOsName:    listOSName,
		DataSource:    view.Source,
		OsList:        vc.OsList,
		Drivers:       make([]guiDriverRow, 0, len(view.Selected)),
	}
	for _, driver := range view.Selected {
		sourceAudit := ""
		if driver.SourceAudit != nil {
			sourceAudit = string(driver.SourceAudit.Category) + ": " + driver.SourceAudit.Summary
		}
		payload.Drivers = append(payload.Drivers, guiDriverRow{
			DriverCode:    driver.DriverCode,
			DriverName:    driver.DriverName,
			Version:       driver.Version,
			LocalVersion:  driver.LocalVersion,
			CompareStatus: string(driver.CompareStatus),
			SourceAudit:   sourceAudit,
			CompareSource: driver.CompareSource,
			FileName:      driver.FileName,
			FilePath:      driver.FilePath,
			FileSize:      driver.FileSize,
			MD5:           driver.OfficialMD5,
			IsApplicable:  driver.CompareStatus != model.StatusNotApplicable,
			IsUpdate:      driver.CompareStatus == model.StatusUpdate,
		})
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
