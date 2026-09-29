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

// InAutomaticInstallSet reports whether a status may enter the automatic
// install set (the interactive "all applicable" action). It is the single
// authority for that membership, so the rule cannot drift between the view
// partition and the plan/console wording.
//
// Invariant 3 ("default inaction") admits only a driver that is measurably
// missing or measurably behind. Two statuses are deliberately excluded even
// though their hardware matches:
//
//   - StatusLocalNewer: the machine already runs a newer version than the
//     official list offers, so installing it DOWNGRADES the device. On 82JQ
//     this covered 9 of 11 candidates, including AMD VGA 30.0.14052.9003 being
//     replaced by 27.20.15026.8004. Its evidence basis is inference, not fact.
//   - StatusUpToDate: versions match, so the install is a no-op.
//
// Excluding them constrains only what the tool does unattended. A driver in
// either status stays selectable explicitly (interactive "s", or -GuiInstallCodes),
// which is where the operator, not the tool, makes the call.
func InAutomaticInstallSet(status CompareStatus) bool {
	return status == StatusUpdate || status == StatusNotInstalled
}

// StatusEvidenceBasis classifies how strongly a comparison status is backed by
// measured evidence. It is the single projection used by plan text, console
// summary, interactive prompts, and GUI export so the fact/inference/
// undetermined wording never drifts between consumers.
//
//	fact:         both remote and local versions were measured and compared.
//	inference:    the status depends on source audit or the absence of a local match.
//	undetermined: the comparison could not reach a decision.
func StatusEvidenceBasis(status CompareStatus) string {
	switch status {
	case StatusUpdate, StatusUpToDate:
		return "fact"
	case StatusNotInstalled, StatusLocalNewer:
		return "inference"
	default:
		return "undetermined"
	}
}

// AuditCategory is the resolved source-evidence category.
type AuditCategory string

const (
	AuditCategoryUnknown             AuditCategory = "Unknown"
	AuditCategoryInstallHistory      AuditCategory = "Install history"
	AuditCategoryCurrentOfficialOS   AuditCategory = "Current official OS"
	AuditCategoryAlternateOfficialOS AuditCategory = "Alternate official OS"
	AuditCategoryExternal            AuditCategory = "External"
)

// DriverAssessment is the comparison and source-audit results computed for a
// driver after it is selected for the view. It is a separate type from Driver
// so the transport payload from the Lenovo API is never mutated by assessment
// code; an AssessedDriver pairs the two.
type DriverAssessment struct {
	LocalVersion  string        `json:"LocalVersion"`
	LocalVendor   string        `json:"LocalVendor"`
	CompareStatus CompareStatus `json:"CompareStatus"`
	CompareSource string        `json:"CompareSource"`
	SourceAudit   *SourceAudit  `json:"SourceAudit,omitempty"`
	// DeviceProblem is the collapsed problem-code summary for the matched local
	// devices, or "" when every matched device is healthy. It is derived from
	// Windows CM_Get_DevNode_Status, not from the driver version comparison.
	DeviceProblem string `json:"DeviceProblem,omitempty"`
	// NonMatchReason explains why a driver was assessed Not applicable. It is
	// populated from compare.DiagnoseDriverNonMatch so the plan and GUI export
	// surface the actual mismatch instead of the generic "hardware not detected".
	NonMatchReason string `json:"NonMatchReason,omitempty"`
	// BeforeInfName is the published INF name (e.g. oem42.inf) of the matched
	// device captured before this tool installs anything. It is the restore
	// target a rollback hint can reference when a post-install device problem
	// appears; it is read-only evidence, never a rollback trigger.
	BeforeInfName string `json:"BeforeInfName,omitempty"`
	// BeforeInfPath is the absolute path (e.g. C:\Windows\INF\oem42.inf) of the
	// matched device's previously-bound INF, captured before this tool installs
	// anything. It is the reinstall target when a post-install device problem
	// has no DiRollbackDriver backup; read-only evidence, never a rollback
	// trigger.
	BeforeInfPath string `json:"BeforeInfPath,omitempty"`
	// MatchedDeviceIDs is the set of PnP device instance ids this driver was
	// assessed against. It is captured once at assessment time (the matched
	// traversal already resolved it) and is the device identity every ledger row
	// for this driver records, so the device audit can join a changed device to
	// the driver actions that targeted it without parsing free-text messages.
	MatchedDeviceIDs []string `json:"MatchedDeviceIDs,omitempty"`
}

// Driver is the normalized Lenovo driver row from the API. It carries only
// transport fields; assessment results (version compare, source audit) live in
// DriverAssessment, paired through AssessedDriver.
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

// AssessedDriver pairs a transport Driver with its computed assessment. The
// Driver is never mutated after construction; assessment fields are promoted
// from DriverAssessment so consumers read them as flat fields.
type AssessedDriver struct {
	*Driver
	DriverAssessment
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

	// StatusFlags is the raw CM_Get_DevNode_Status status bitmask.
	StatusFlags uint32
	// ProblemNumber is the Windows device problem code; 0 means no problem.
	ProblemNumber int
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
	// Devices carries the PnP device instance ids this row's action targeted.
	// It is the ledger's device identity: the join key between "which driver
	// this tool acted on" and "how devices actually changed", which previously
	// existed only as free text inside Message.
	Devices []string
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
	Selected      []*AssessedDriver
	Invalid       []string
	NotApplicable []string
}
