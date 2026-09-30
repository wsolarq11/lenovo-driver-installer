package install

import (
	"os"
	"strings"

	"lenovo-driver/internal/model"
)

// The silent-install formula, expressed once.
//
// Whether a package can be installed unattended is a decision about one fact:
// does an unattended mechanism exist for it, and has that mechanism been
// verified end-to-end?
//
// The .inf/.zip/.cab payloads need no decision here: the tool hands them to
// pnputil / DiInstallDriverW directly, which is headless by construction (there
// is no GUI to suppress, so there is no switch to get wrong). Those paths live
// in InstallDriverFile.
//
// An .exe installer has a GUI, so driving it unattended needs a silent switch.
// On 82JQ no .exe silent switch has ever been verified end-to-end: the vendor
// Parameter column is falsified (AMD declares "-QuietInstall" and the binary
// contains no such literal), and the real Inno switch "/VERYSILENT" has never
// been run to a verified install. A wrong switch is a silent no-op — the
// process exits 0 having changed nothing, which is exactly how an install gets
// reported as done.
//
// So every .exe is interactive. The family is still recognised from the
// package's own bytes and named in the audit evidence, because recognition is
// not verification: knowing a package is Inno does not verify that /VERYSILENT
// installs it. Those are two different facts, and only the second one can
// authorise an unattended run. This file owns the recognition table; nothing
// else may invent an installer family.

// InstallerFamily is the installer kind proven by a marker inside the package.
// It is recognised for the audit trail; recognition alone never authorises an
// unattended run.
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
// history record so a refusal can be traced to the marker that caused it, which
// is the difference between a diagnosis and a mystery.
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

// silentPlan is the formula's output: which family was recognised, and the
// evidence the decision rests on. There is no args/ok pair because no family's
// switch is verified, so no .exe is ever launched with invented flags.
type silentPlan struct {
	family   InstallerFamily
	evidence string
}

// planSilentInstall applies the formula. Every .exe is interactive because no
// .exe silent switch has a verified end-to-end run. The evidence names the
// recognised family (or the absence of one) so the audit can say "we saw Inno
// and refused it" rather than just "refused".
func planSilentInstall(filePath string, driver *model.Driver, logPath string) silentPlan {
	family := detectInstallerFamily(filePath)
	if family == FamilyUnproven {
		return silentPlan{family: family, evidence: "no installer marker in the package; interactive"}
	}
	return silentPlan{family: family, evidence: family.String() + " marker, but its silent switch has no verified end-to-end run; interactive"}
}

// SilentPlanFor reports whether the formula can drive this package unattended
// (it cannot, until a switch is verified) and the evidence for the audit trail.
//
// The bool stays because the answer is about to change: the moment one family's
// switch is verified end-to-end, this function starts returning true for it and
// callers need not change. The evidence exists so a refused install still says
// which mechanism it was refused for.
func SilentPlanFor(filePath string, driver *model.Driver) (bool, string) {
	plan := planSilentInstall(filePath, driver, "")
	return false, plan.evidence
}
