package install

import (
	"os"
	"strings"

	"lenovo-driver/internal/model"
)

// The silent-install formula, expressed once.
//
// Lenovo's own tool installs silently because it dispatches on the installer
// family rather than on the driver's Parameter column. Measured on the 82JQ
// lists: that column carries three non-empty values, and one of them is false.
// AMD-2GY501AFHN99VBC0.exe declares "-QuietInstall" and the binary contains
// no spelling of it, but does carry "Inno Setup Setup Data" — the Inno Setup
// header — which proves the package is Inno no matter what the column says.
// The other two are "-n -s" on the
// NVIDIA package (whose outer wrapper's family is not established by its
// bytes; setupapi.dev.log shows it extracting to the Inno-style temp dir
// is-6UIF7.tmp, so the "-n -s" column is a hint, not proof) and
// "/add-driver *.inf /install /subdirs" on 41 rows that are INF payload
// wrappers needing no vendor switch at all.
//
// So the column is a hint and the binary is the evidence. This file owns the
// table; nothing else may invent an installer family.

// InstallerFamily is the payload kind proven by a marker inside the package.
type InstallerFamily int

const (
	FamilyUnproven InstallerFamily = iota
	FamilyInnoPayload
	FamilyNSIS
	FamilySevenZipSFX
	FamilyInstallShield
	FamilyWiXBurn
)

// String names the family for the audit trail. The name is written into the
// history record so a wrong verdict can be traced to the marker that caused it,
// which is the difference between a diagnosis and a mystery.
func (f InstallerFamily) String() string {
	switch f {
	case FamilyInnoPayload:
		return "Inno Setup"
	case FamilyNSIS:
		return "NSIS"
	case FamilySevenZipSFX:
		return "7-Zip SFX"
	case FamilyInstallShield:
		return "InstallShield"
	case FamilyWiXBurn:
		return "WiX Burn"
	default:
		return "unproven"
	}
}

// silentArgs are the flags that family's own documentation defines. A family
// that is not listed here is never sent a guess, because a flag the installer
// does not recognize is a silent no-op: the process exits 0 having changed
// nothing, which is exactly how an install gets reported as done.
func (f InstallerFamily) silentArgs() []string {
	switch f {
	case FamilyInnoPayload:
		return []string{"/VERYSILENT", "/SUPPRESSMSGBOXES", "/NORESTART", "/SP-"}
	case FamilyNSIS:
		return []string{"/S"}
	case FamilySevenZipSFX:
		return []string{"-s"}
	case FamilyInstallShield:
		// InstallShield wraps an MSI engine, so silence needs both halves.
		return []string{"/s", `/v"/qn /norestart"`}
	case FamilyWiXBurn:
		return []string{"/quiet", "/norestart"}
	default:
		return nil
	}
}

// familyMarkers is the priority order of the formula. Order is part of the
// specification, not an accident: these packages are self-extracting shells that
// embed the strings of every payload they contain, so several markers can match
// one binary and the first hit in this list wins. The wrapper markers come
// before the payload markers because the wrapper is what actually runs.
var familyMarkers = []struct {
	family InstallerFamily
	ascii  string
}{
	// The wrapper's own configuration block, present only when the archive is
	// driven by a 7-Zip SFX stub.
	{FamilySevenZipSFX, "!@Install@!UTF-8!"},
	// NSIS stores this magic in the firstheader of its own archive.
	{FamilyNSIS, "NullsoftInst"},
	// Inno writes this exact phrase as the header of its embedded setup data.
	{FamilyInnoPayload, "Inno Setup Setup Data"},
	// A WiX Burn stub carries a PE section named .wixburn.
	{FamilyWiXBurn, ".wixburn"},
	// The weakest signal, so it is consulted last: a Lenovo wrapper can quote
	// the word while carrying an entirely different payload.
	{FamilyInstallShield, "InstallShield"},
}

// detectInstallerFamily reads the package and returns the family its own bytes
// prove. It returns FamilyUnproven rather than a default, because a default
// would be an assumption wearing the costume of a measurement.
func detectInstallerFamily(path string) InstallerFamily {
	data, err := os.ReadFile(path)
	if err != nil {
		return FamilyUnproven
	}
	return familyFromBytes(data)
}

// familyFromBytes is the pure half, so the table can be tested against recorded
// bytes instead of only against packages that happen to be on disk.
func familyFromBytes(data []byte) InstallerFamily {
	ascii := string(data)
	// Windows PE files carry their strings in both single-byte and UTF-16LE
	// form. Searching only the single-byte form would miss markers that live in
	// the resource section, which is where .wixburn and the SFX stub keep them.
	utf16 := utf16LEToString(data)
	for _, marker := range familyMarkers {
		if strings.Contains(ascii, marker.ascii) || strings.Contains(utf16, marker.ascii) {
			return marker.family
		}
	}
	return FamilyUnproven
}

// utf16LEToString decodes the UTF-16LE runs an ASCII scan would otherwise miss.
func utf16LEToString(data []byte) string {
	var b strings.Builder
	b.Grow(len(data) / 2)
	for i := 0; i+1 < len(data); i += 2 {
		if data[i] != 0 || data[i+1] == 0 {
			continue
		}
		b.WriteByte(data[i+1])
	}
	return b.String()
}

// silentPlan is the formula's output: which mechanism to run, the flags that
// mechanism needs, and the family the decision rests on.
type silentPlan struct {
	family   InstallerFamily
	args     []string
	ok       bool
	evidence string
}

// planSilentInstall applies the formula. The vendor column is consulted for one
// thing only, because it is the one thing it is reliable for: it states what the
// payload is. It never states which flag silences the installer, and after the
// 82JQ drill it is not allowed to pretend to.
func planSilentInstall(filePath string, driver *model.Driver, logPath string) silentPlan {
	// An INF payload wrapper has nothing to silence. Running it with no flag
	// makes it extract and hand the INF to the driver store, which is already
	// unattended, so the formula exits before reading any bytes.
	if raw := vendorParameter(driver); raw != "" && strings.HasPrefix(strings.ToLower(raw), "/add-driver") {
		return silentPlan{evidence: "vendor payload is an INF package; no silent switch needed"}
	}
	family := detectInstallerFamily(filePath)
	args := family.silentArgs()
	if len(args) == 0 {
		return silentPlan{family: family, evidence: "no installer marker in the package"}
	}
	// /LOG is Inno-only and is justified by the marker that just selected this
	// family, never by the vendor column.
	if family == FamilyInnoPayload && logPath != "" {
		args = append(args, "/LOG="+logPath)
	}
	return silentPlan{family: family, args: args, ok: true, evidence: family.String() + " marker"}
}

// vendorParameter is the single reader of the vendor column, kept here so the
// only remaining use of that column is visible in one place.
func vendorParameter(driver *model.Driver) string {
	if driver == nil {
		return ""
	}
	if driver.InstallParameter != "" {
		return driver.InstallParameter
	}
	return driver.InstallCode
}

// SilentPlanFor reports whether the formula can drive this package unattended,
// and names the mechanism the decision rests on.
//
// The second value exists for the audit trail. A run that installs a package
// with nobody present has to be able to answer, afterwards, why the tool
// believed it could: a wrong family and a right one produce the same exit code
// and the same silent outcome, so the exit code cannot carry this. Without the
// evidence the ledger says "exit=0" and the reason is gone.
func SilentPlanFor(filePath string, driver *model.Driver) (bool, string) {
	plan := planSilentInstall(filePath, driver, "")
	return plan.ok, plan.evidence
}

// HasSilentParameters reports only the decision. Callers that write an audit row
// want SilentPlanFor instead, so the evidence is never computed and then
// dropped on the floor.
func HasSilentParameters(filePath string, driver *model.Driver) bool {
	canRun, _ := SilentPlanFor(filePath, driver)
	return canRun
}
