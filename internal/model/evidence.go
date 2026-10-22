package model

// Local version evidence: what the machine was observed to say about a driver's
// installed version, as a closed union rather than a string whose shape the
// reader had to interpret. It lives in its own file because it is the type the
// whole decision pipeline is phrased in, and because the union plus its five
// constructors is a self-contained contract.

import (
	"sort"
	"strings"
)

// LocalVersionProvisioned is the local-version placeholder reported for a
// package Windows has provisioned but not necessarily installed. On 82JQ
// AMD.Power.Processor.ppkg exists under HKLM\SOFTWARE\Microsoft\Provisioning\
// Results while the machine has no such application, no uninstall entry and no
// matching device. It is a real fact about the machine and a false fact about
// the driver: it must never stand in for a version number, because comparing it
// to itself proves nothing. Single owner so no collector can invent its own
// spelling.
//
// It is the display spelling of LocalEvidenceProvisioned; code decides on the
// kind, never on this string.
const LocalVersionProvisioned = "Provisioned"

// LocalEvidence is the resolved local version together with how it was
// established. It is a closed union: the five states have separate constructors,
// the fields are unexported, and the zero value is Unobserved rather than
// Absent. The three properties are what make an illegal state unrepresentable
// rather than merely discouraged.
//
// Why a union and not a tagged struct: a struct with exported fields could hold
// a Conflicted kind carrying one Version, or a Version kind with an empty
// payload, and every consumer had to re-derive which combination was legal. The
// zero value was Absent, so any path that forgot to resolve evidence produced
// the status the automatic install set acts on — the worst possible default.
// Now the zero value measures nothing, and construction goes through a
// constructor named after what was observed.
type LocalEvidence struct {
	kind     LocalEvidenceKind
	version  string   // set only for LocalEvidenceVersion
	versions []string // set only for LocalEvidenceConflicted
}

// LocalEvidenceKind names the observed state. It is exported so consumers can
// switch on it; the payloads are not, so a consumer cannot read a field that
// the current state does not define.
type LocalEvidenceKind int

const (
	// LocalEvidenceUnobserved is the ZERO VALUE: nothing was measured on this
	// machine. Making the zero value the no-information state means a forgotten
	// assignment degrades to "I know nothing", never to "the driver is missing".
	LocalEvidenceUnobserved LocalEvidenceKind = iota
	// LocalEvidenceVersion means exactly one measured version was read.
	LocalEvidenceVersion
	// LocalEvidenceAbsent means matched devices exist and none of them carries a
	// bound driver version. This is a measurement of absence.
	LocalEvidenceAbsent
	// LocalEvidenceConflicted means two or more distinct measured versions were
	// read from devices this driver applies to. The disagreement is a fact; the
	// ordering is not decidable from it.
	LocalEvidenceConflicted
	// LocalEvidenceProvisioned means Windows holds a provisioning package for the
	// driver, which proves neither presence nor absence.
	LocalEvidenceProvisioned
)

// NotObserved constructs the state where this machine said nothing about the
// driver. It returns the zero value on purpose: the whole point of ordering the
// kind constants this way is that "no information" is what an uninitialized
// value means.
func NotObserved() LocalEvidence {
	return LocalEvidence{}
}

// ObservedVersion constructs the single-version state. An empty version is not a
// measurement, so it yields Unobserved: the constructor refuses to relabel "no
// data" as a measured value.
func ObservedVersion(version string) LocalEvidence {
	if version == "" {
		return LocalEvidence{}
	}
	return LocalEvidence{kind: LocalEvidenceVersion, version: version}
}

// ObservedAbsence constructs the measured-absence state. It is only reachable
// from a code path that actually enumerated the driver's devices.
func ObservedAbsence() LocalEvidence {
	return LocalEvidence{kind: LocalEvidenceAbsent}
}

// ObservedConflict constructs the disagreement state. Fewer than two distinct
// versions is not a conflict, so one version collapses to ObservedVersion and
// none collapses to Unobserved; the constructor cannot mint a contradiction.
func ObservedConflict(versions []string) LocalEvidence {
	seen := map[string]bool{}
	unique := make([]string, 0, len(versions))
	for _, v := range versions {
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		unique = append(unique, v)
	}
	switch len(unique) {
	case 0:
		return LocalEvidence{}
	case 1:
		return ObservedVersion(unique[0])
	}
	sort.Strings(unique)
	return LocalEvidence{kind: LocalEvidenceConflicted, versions: unique}
}

// ObservedProvisioning constructs the placeholder state: the system holds a
// package, which proves neither presence nor absence.
func ObservedProvisioning() LocalEvidence {
	return LocalEvidence{kind: LocalEvidenceProvisioned}
}

// Kind reports the observed state. Every consumer switches on this.
func (e LocalEvidence) Kind() LocalEvidenceKind { return e.kind }

// MeasuredVersion returns the single measured version, or "" for every other
// state. Code that needs the number uses this rather than reaching for a field,
// so a conflict or a placeholder can never be read as a version.
func (e LocalEvidence) MeasuredVersion() string {
	if e.kind == LocalEvidenceVersion {
		return e.version
	}
	return ""
}

// ConflictingVersions returns the measured versions of a conflict, sorted, and
// nil for every other state. The slice is copied so a caller cannot mutate the
// evidence.
func (e LocalEvidence) ConflictingVersions() []string {
	if e.kind != LocalEvidenceConflicted {
		return nil
	}
	return append([]string(nil), e.versions...)
}

// Display renders the evidence for a human reader. It is the only place the
// states become text, so the plan, the table, and the export cannot describe
// the same evidence differently.
func (e LocalEvidence) Display() string {
	switch e.kind {
	case LocalEvidenceVersion:
		return e.version
	case LocalEvidenceAbsent:
		return "not detected"
	case LocalEvidenceProvisioned:
		return LocalVersionProvisioned
	case LocalEvidenceConflicted:
		return strings.Join(e.versions, ", ")
	default:
		return "not observed"
	}
}

// Sought reports whether the machine was actually observed for this driver. A
// caller deciding whether "no version" means "not installed" must ask this:
// Unobserved and Provisioned both lack a version, but only a driver whose
// devices were observed can be called not installed.
func (e LocalEvidence) Sought() bool {
	return e.kind != LocalEvidenceUnobserved
}

// IsMeasuredLocalVersion reports whether a local version string is a measured
// version number that can carry a before/after comparison. It is the string form
// of the same question MeasuredVersion answers for LocalEvidence, kept for the
// callers that hold a plain string (a ledger row, a history column). The
// provisioning placeholder and the empty string cannot, so callers must not
// decide that an install took effect from them.
func IsMeasuredLocalVersion(local string) bool {
	return local != "" && local != LocalVersionProvisioned
}
