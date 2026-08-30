package model

import "time"

// CompareStatus is the user-facing driver comparison result.
type CompareStatus string

const (
	StatusUpdate        CompareStatus = "Update"
	StatusUpToDate      CompareStatus = "Up to date"
	StatusNotInstalled  CompareStatus = "Not installed"
	StatusLocalNewer    CompareStatus = "Local newer"
	StatusUnknown       CompareStatus = "Unknown"
	StatusNotApplicable CompareStatus = "Not applicable"
)

// AuditCategory is the resolved source-evidence category.
type AuditCategory string

const (
	AuditCategoryUnknown             AuditCategory = "Unknown"
	AuditCategoryInstallHistory      AuditCategory = "Install history"
	AuditCategoryOfflineImage        AuditCategory = "Offline image integration"
	AuditCategoryOnlinePackage       AuditCategory = "Online package installation"
	AuditCategoryCurrentOfficialOS   AuditCategory = "Current official OS"
	AuditCategoryAlternateOfficialOS AuditCategory = "Alternate official OS"
	AuditCategoryPreExistingStore    AuditCategory = "Pre-existing DriverStore package"
)

// Driver is the normalized Lenovo driver row used by all higher layers.
type Driver struct {
	PartID           string    `json:"PartId"`
	PartName         string    `json:"PartName"`
	DriverName       string    `json:"DriverName"`
	DriverCode       string    `json:"DriverCode"`
	DriverEditionID  int64     `json:"DriverEditionId"`
	Version          string    `json:"Version"`
	FileName         string    `json:"FileName"`
	FilePath         string    `json:"FilePath"`
	FileSize         string    `json:"FileSize"`
	FileType         string    `json:"FileType"`
	InstallCode      string    `json:"InstallCode"`
	InstallParameter string    `json:"InstallParameter"`
	Bootfile         string    `json:"Bootfile"`
	HardwareID       string    `json:"HardwareId"`
	OfficialMD5      string    `json:"MD5"`
	Status           string    `json:"Status"`
	IsEnable         string    `json:"IsEnable"`
	IssuedDate       time.Time `json:"IssuedDate"`
	OSID             string    `json:"OSID"`
	OSName           string    `json:"OsName"`
	SourceAPI        string    `json:"SourceApi"`

	LocalVersion  string        `json:"LocalVersion"`
	LocalVendor   string        `json:"LocalVendor"`
	CompareStatus CompareStatus `json:"CompareStatus"`
	CompareSource string        `json:"CompareSource"`
	SourceAudit   *SourceAudit  `json:"SourceAudit,omitempty"`
}

// Disabled reports whether the API marks the driver row as disabled or not
// enabled (its Status / IsEnable string signals are "0"). Centralizing this
// names the enable/disable semantic instead of scattering "0" comparisons.
func (d *Driver) Disabled() bool {
	if d == nil {
		return true
	}
	return (d.Status != "" && d.Status == "0") || (d.IsEnable != "" && d.IsEnable == "0")
}

// Device is a normalized PnP device row supplied by inventory.
type Device struct {
	Name          string
	Class         string
	DeviceID      string
	PnpDeviceID   string
	DriverVersion string
	DriverDate    string
	InfName       string
	ProviderName  string
	InstallDate   string

	PackageDir          string
	PackageCreationTime string
	PackageDriverVer    string
	ImportSource        string
	ImportCommand       string
	ImportLog           string
	ImportLine          int
	ImportKind          string
}

// InstalledApp is one installed application row.
type InstalledApp struct {
	DisplayName    string
	DisplayVersion string
}

// SoftwareSnapshot carries software-only driver evidence.
type SoftwareSnapshot struct {
	InstalledApps          []InstalledApp
	ProvisionedAmdPower    string
	LenovoFnServiceVersion string
}

// OSListEntry is one Lenovo supported OS entry.
type OSListEntry struct {
	OSID   string `json:"OSID"`
	OSName string `json:"OSName"`
}

// OSNameByID returns the OSName for the entry whose OSID matches, or "" when no
// such entry exists. It is the single OSID-to-name lookup shared by API
// normalization, GUI export, and the interactive OS switcher so their idea of
// "which OS this id means" never drifts.
func OSNameByID(osList []OSListEntry, osID string) string {
	for _, entry := range osList {
		if entry.OSID == osID {
			return entry.OSName
		}
	}
	return ""
}

// HistoryRecord is one row in lenovo_driver_history.csv.
type HistoryRecord struct {
	Timestamp       string
	DriverCode      string
	OSID            string
	OSName          string
	DriverName      string
	Version         string
	VerifiedVersion string
	BeforeVersion   string
	FileName        string
	MD5             string
	Source          string
	Result          string
	Message         string
}

// SourceAudit is the resolved source evidence for a local driver version.
type SourceAudit struct {
	Category      AuditCategory
	Summary       string
	EvidenceLines []string
}

// SourceMapEntry is a version match against a Lenovo OS list.
type SourceMapEntry struct {
	OSID   string
	OSName string
}

// ImportRecord is one parsed setupapi import section.
type ImportRecord struct {
	SourcePath string
	Command    string
	Version    string
	InfName    string
	OemInfName string
	PackageDir string
	LogPath    string
	LineNumber int
	HeaderKind string
}

// SelectionResult is the parsed manual selection result.
type SelectionResult struct {
	Selected      []*Driver
	Invalid       []string
	NotApplicable []string
}
