package api

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"lenovo-driver/internal/model"
	"lenovo-driver/internal/pathutil"
)

// QuickFixResponse is the official QuickFix backend response shape.
type QuickFixResponse struct {
	StatusCode string `json:"StatusCode"`
	Message    string `json:"Message"`
	Data       struct {
		PartList   []quickFixPart      `json:"partList"`
		OSList     []model.OSListEntry `json:"osList"`
		DriverList []driverRow         `json:"driverList"`
	} `json:"Data"`
}

type quickFixPart struct {
	PartID   string `json:"PartID"`
	PartName string `json:"PartName"`
}

// WebResponse is the official webpage API response shape.
type WebResponse struct {
	StatusCode string `json:"statusCode"`
	Message    string `json:"message"`
	Data       struct {
		LocalOSID string              `json:"localOSID"`
		PartList  []webPart           `json:"partList"`
		OSList    []model.OSListEntry `json:"osList"`
	} `json:"data"`
}

type webPart struct {
	PartID    string      `json:"PartID"`
	Drivelist []driverRow `json:"drivelist"`
}

type driverRow struct {
	PartID               string `json:"PartID"`
	PartName             string `json:"PartName"`
	DriverName           string `json:"DriverName"`
	DriverCode           string `json:"DriverCode"`
	DriverEdtionID       string `json:"DriverEdtionId"`
	DriverEditionID      string `json:"DriverEditionId"`
	Version              string `json:"Version"`
	FileName             string `json:"FileName"`
	FilePath             string `json:"FilePath"`
	FileSize             string `json:"FileSize"`
	FileType             string `json:"FileType"`
	InstallCode          string `json:"InstallCode"`
	Parameter            string `json:"Parameter"`
	Bootfile             string `json:"Bootfile"`
	HardwareID           string `json:"HardwareId"`
	MD5                  string `json:"MD5"`
	Status               string `json:"Status"`
	IsEnable             string `json:"IsEnable"`
	DriverIssuedDateTime string `json:"DriverIssuedDateTime"`
	PubTime              string `json:"PubTime"`
	UpdateTime           string `json:"UpdateTime"`
}

var (
	reDate     = regexp.MustCompile(`\d{4}/\d{1,2}/\d{1,2}`)
	reNonDigit = regexp.MustCompile(`[^0-9]`)
)

// ParseQuickFix normalizes a QuickFix response into driver rows.
func ParseQuickFix(resp *QuickFixResponse, osID string) []*model.Driver {
	if resp == nil || resp.Data.DriverList == nil {
		return nil
	}
	partMap := map[string]string{}
	for _, part := range resp.Data.PartList {
		partMap[part.PartID] = part.PartName
	}
	osName := findOSName(resp.Data.OSList, osID)
	var drivers []*model.Driver
	for i := range resp.Data.DriverList {
		if d := normalizeDriver(&resp.Data.DriverList[i], partMap, osID, osName, "QuickFix"); d != nil {
			drivers = append(drivers, d)
		}
	}
	return drivers
}

// ParseWeb normalizes a webpage API response into driver rows.
func ParseWeb(resp *WebResponse, osID string) []*model.Driver {
	if resp == nil || resp.Data.PartList == nil {
		return nil
	}
	partMap := map[string]string{}
	var rows []driverRow
	for _, part := range resp.Data.PartList {
		partMap[part.PartID] = ""
		rows = append(rows, part.Drivelist...)
	}
	osName := findOSName(resp.Data.OSList, osID)
	var drivers []*model.Driver
	for i := range rows {
		if d := normalizeDriver(&rows[i], partMap, osID, osName, "Web"); d != nil {
			drivers = append(drivers, d)
		}
	}
	return drivers
}

func normalizeDriver(row *driverRow, partMap map[string]string, osID, osName, source string) *model.Driver {
	if row.FileName == "" || row.FilePath == "" {
		return nil
	}
	partID := row.PartID
	partName := row.PartName
	if partName == "" {
		partName = partMap[partID]
	}
	rawEdition := row.DriverEdtionID
	if rawEdition == "" {
		rawEdition = row.DriverEditionID
	}
	var edition int64
	if digits := reNonDigit.ReplaceAllString(rawEdition, ""); digits != "" {
		edition, _ = strconv.ParseInt(digits, 10, 64)
	}
	issued := time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC)
	rawIssued := row.DriverIssuedDateTime
	if rawIssued == "" {
		rawIssued = row.PubTime
	}
	if rawIssued == "" {
		rawIssued = row.UpdateTime
	}
	if m := reDate.FindString(rawIssued); m != "" {
		if parsed, err := time.Parse("2006/1/2", m); err == nil {
			issued = parsed
		}
	}
	installCode := row.InstallCode
	if installCode == "" {
		installCode = row.Parameter
	}
	return &model.Driver{
		PartID:           partID,
		PartName:         partName,
		DriverName:       row.DriverName,
		DriverCode:       row.DriverCode,
		DriverEditionID:  edition,
		Version:          row.Version,
		FileName:         pathutil.Base(row.FileName),
		FilePath:         row.FilePath,
		FileSize:         row.FileSize,
		FileType:         row.FileType,
		InstallCode:      installCode,
		InstallParameter: row.Parameter,
		Bootfile:         row.Bootfile,
		HardwareID:       row.HardwareID,
		OfficialMD5:      strings.ToLower(strings.TrimSpace(row.MD5)),
		Status:           strings.TrimSpace(row.Status),
		IsEnable:         strings.TrimSpace(row.IsEnable),
		IssuedDate:       issued,
		OSID:             osID,
		OSName:           osName,
		SourceAPI:        source,
	}
}

func findOSName(osList []model.OSListEntry, osID string) string {
	for _, entry := range osList {
		if entry.OSID == osID {
			return entry.OSName
		}
	}
	return ""
}
