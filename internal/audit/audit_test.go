package audit

import (
	"strings"
	"testing"

	"lenovo-driver/internal/model"
)

func TestGetSourceMapMatch(t *testing.T) {
	driver := &model.Driver{DriverCode: "d1", DriverName: "Audio"}
	sourceMap := map[string]model.SourceMapEntry{
		"Audio|2.0.0.1": {OSID: "248", OSName: "Windows 11 64-bit"},
	}
	match := sourceMapMatch(driver, sourceMap, "2.0.0.1")
	if match == nil || match.OSID != "248" {
		t.Fatalf("source map match failed: %v", match)
	}
}

func TestResolveExternalDriverSourceLabel(t *testing.T) {
	driver := &model.Driver{DriverName: "Audio", DriverCode: "d1"}
	alternate := map[string]model.SourceMapEntry{"Audio|2.0.0.1": {OSID: "248", OSName: "Windows 11 64-bit"}}
	got := resolveExternalDriverSourceLabel(driver, alternate, []string{"2.0.0.1"})
	if got != "Local newer (version matches Windows 11 64-bit list)" {
		t.Fatalf("unexpected label: %q", got)
	}
}

func TestConvertFromImportLogText(t *testing.T) {
	lines := []string{
		`>>>  [Driver Install (DrvSetupInstallDriver) - C:\Windows\TempInst\is-EFU44.tmp\source\LenovoFnAndFunctionKeys.inf]`,
		`>>>  Section start 2026/08/24 19:00:07.162`,
		`      cmd: "C:\Windows\system32\pnputil.exe" /add-driver C:\Windows\TempInst\is-EFU44.tmp\source\LenovoFnAndFunctionKeys.inf /install`,
		`     dvs: {Driver Setup Import Driver Package: C:\Windows\TempInst\is-EFU44.tmp\source\LenovoFnAndFunctionKeys.inf} 19:00:07.162`,
		`     sto: {Core Driver Package Import: lenovofnandfunctionkeys.inf_amd64_9bbed958a3d29caf} 19:00:07.344`,
		`     idb:                Created driver package object 'lenovofnandfunctionkeys.inf_amd64_9bbed958a3d29caf' in DRIVERS database node.`,
		`     idb:                Created driver INF file object 'oem55.inf' in DRIVERS database node.`,
		`     idb:                Registered driver package 'lenovofnandfunctionkeys.inf_amd64_9bbed958a3d29caf' with 'oem55.inf'.`,
		`     dvs: {Driver Setup Import Driver Package - exit (0x00000000)} 19:00:07.500`,
	}
	rows := ConvertFromImportLogText(lines, `C:\Windows\INF\setupapi.dev.log`)
	if len(rows) != 1 {
		t.Fatalf("expected 1 import row, got %d", len(rows))
	}
	row := rows[0]
	if row.InfName != "LenovoFnAndFunctionKeys.inf" {
		t.Fatalf("unexpected INF: %q", row.InfName)
	}
	if row.OemInfName != "oem55.inf" {
		t.Fatalf("unexpected OEM INF: %q", row.OemInfName)
	}
	if row.PackageDir != "lenovofnandfunctionkeys.inf_amd64_9bbed958a3d29caf" {
		t.Fatalf("unexpected package dir: %q", row.PackageDir)
	}
	if row.Command == "" {
		t.Fatal("command should be captured")
	}
}

func TestResolveDriverSourceEvidence(t *testing.T) {
	driver := &model.AssessedDriver{
		Driver: &model.Driver{DriverCode: "d1", DriverName: "Audio", OSID: "42"},
		DriverAssessment: model.DriverAssessment{
			LocalEvidence: model.ObservedVersion("2.0.0.1"),
			LocalVersion:  "2.0.0.1",
		},
	}
	device := model.Device{Name: "Audio", DriverVersion: "2.0.0.1", InfName: "oem1.inf"}
	auditResult := ResolveDriverSourceEvidence(driver, []model.Device{device}, nil, nil, nil)
	if auditResult.Category != model.AuditCategoryExternal {
		t.Fatalf("unexpected audit category: %q", auditResult.Category)
	}
}

func TestSourceEvidenceHistoryOutranksDevice(t *testing.T) {
	driver := &model.AssessedDriver{
		Driver: &model.Driver{DriverCode: "d1", DriverName: "Audio", OSID: "42"},
		DriverAssessment: model.DriverAssessment{
			LocalEvidence: model.ObservedVersion("2.0.0.1"),
			LocalVersion:  "2.0.0.1",
		},
	}
	history := []model.HistoryRecord{{
		Timestamp: "2026-01-01 00:00:00", DriverCode: "d1", DriverName: "Audio",
		Version: "2.0.0.1", Result: "Installed", OSID: "42",
	}}
	device := model.Device{Name: "Audio", DriverVersion: "2.0.0.1", InfName: "oem1.inf"}
	auditResult := ResolveDriverSourceEvidence(driver, []model.Device{device}, history, nil, nil)
	if auditResult.Category != model.AuditCategoryInstallHistory {
		t.Fatalf("history should outrank device evidence, got %q", auditResult.Category)
	}
	if auditResult.Summary != "Installed by this script from the current OS source" {
		t.Fatalf("unexpected history summary: %q", auditResult.Summary)
	}
}

func TestSourceEvidenceOfflineImageAvailable(t *testing.T) {
	driver := &model.AssessedDriver{
		Driver: &model.Driver{DriverCode: "d1", DriverName: "Audio", OSID: "42"},
		DriverAssessment: model.DriverAssessment{
			LocalEvidence: model.ObservedVersion("2.0.0.1"),
			LocalVersion:  "2.0.0.1",
		},
	}
	device := model.Device{Name: "Audio", DriverVersion: "2.0.0.1", ImportSource: "SW_DVD5", ImportKind: "Offline"}
	auditResult := ResolveDriverSourceEvidence(driver, []model.Device{device}, nil, nil, nil)
	if auditResult.Category != model.AuditCategoryExternal {
		t.Fatalf("offline image should be External, got %q", auditResult.Category)
	}
	if !strings.Contains(auditResult.Summary, "offline image integration") || !strings.Contains(auditResult.Summary, "SW_DVD5") {
		t.Fatalf("offline summary missing mechanism/source: %q", auditResult.Summary)
	}
}

func TestSourceEvidenceDeviceOutranksCurrentOS(t *testing.T) {
	driver := &model.AssessedDriver{
		Driver: &model.Driver{DriverCode: "d1", DriverName: "Audio", OSID: "42"},
		DriverAssessment: model.DriverAssessment{
			LocalEvidence: model.ObservedVersion("2.0.0.1"),
			LocalVersion:  "2.0.0.1",
		},
	}
	device := model.Device{Name: "Audio", DriverVersion: "2.0.0.1", InfName: "oem1.inf"}
	current := map[string]model.SourceMapEntry{"Audio|2.0.0.1": {OSID: "42", OSName: "Windows 10 64-bit"}}
	auditResult := ResolveDriverSourceEvidence(driver, []model.Device{device}, nil, current, nil)
	if auditResult.Category != model.AuditCategoryExternal {
		t.Fatalf("device evidence should outrank current OS list, got %q", auditResult.Category)
	}
}

func TestSourceEvidenceNoMatchFallsBackToUnknown(t *testing.T) {
	driver := &model.AssessedDriver{
		Driver: &model.Driver{DriverCode: "d1", DriverName: "Audio", OSID: "42"},
		DriverAssessment: model.DriverAssessment{
			LocalEvidence: model.ObservedVersion("2.0.0.1"),
			LocalVersion:  "2.0.0.1",
		},
	}
	auditResult := ResolveDriverSourceEvidence(driver, nil, nil, nil, nil)
	if auditResult.Category != model.AuditCategoryUnknown {
		t.Fatalf("no evidence should be Unknown, got %q", auditResult.Category)
	}
	if auditResult.Summary != "No install history, DriverStore import evidence, or official Lenovo version match" {
		t.Fatalf("unexpected fallback summary: %q", auditResult.Summary)
	}
}

// TestConflictedDriverKeepsItsSourceEvidence pins that multi-device version
// disagreement does not erase source evidence. The audit used to compare a
// single version string; for a conflicted driver that string was the joined
// display form "a, b", which matched no history row and no device row, so a
// driver with recorded install history was reported as having no evidence at
// all. Every measured version is evidence and must be tried.
func TestConflictedDriverKeepsItsSourceEvidence(t *testing.T) {
	const (
		older = "30.0.15026.8004"
		newer = "31.0.14052.9003"
	)
	ad := &model.AssessedDriver{
		Driver: &model.Driver{DriverCode: "D1", DriverName: "AMD VGA", OSID: "42"},
		DriverAssessment: model.DriverAssessment{
			LocalEvidence: model.ObservedConflict([]string{older, newer}),
			LocalVersion:  older + ", " + newer,
			CompareStatus: model.StatusUnknown,
		},
	}
	history := []model.HistoryRecord{{
		Timestamp: "2026-01-01T00:00:00Z", DriverCode: "D1",
		Result: "Installed", Version: older, VerifiedVersion: older,
	}}
	devices := []model.Device{{Name: "AMD Radeon", DriverVersion: newer}}

	got := ResolveDriverSourceEvidence(ad, devices, history, nil, nil)
	if got.Category != model.AuditCategoryInstallHistory {
		t.Fatalf("category = %q, want install history; a conflicted driver lost its evidence", got.Category)
	}
	if !strings.Contains(got.Summary, "Installed by this script") {
		t.Fatalf("summary = %q, want the install-history attribution", got.Summary)
	}
}

// TestProseIsNotAVersionCandidate pins that the display projection never enters
// the evidence chain. A driver with no measured version but a display string of
// "not detected" used to fall back to that string as a candidate, match a ledger
// row whose version cell held the same sentence, and be attributed an install
// that never happened. The union is the authority: no measured version means
// nothing to match.
func TestProseIsNotAVersionCandidate(t *testing.T) {
	for _, prose := range []string{"not detected", "not observed", "Provisioned", "1.0, 2.0"} {
		ad := &model.AssessedDriver{
			Driver:           &model.Driver{DriverCode: "D1", DriverName: "X"},
			DriverAssessment: model.DriverAssessment{LocalVersion: prose},
		}
		if got := candidateVersions(ad); len(got) != 0 {
			t.Fatalf("display prose %q became a version candidate: %v", prose, got)
		}
		history := []model.HistoryRecord{{
			Timestamp: "t", DriverCode: "D1", Result: "Installed",
			Version: prose, VerifiedVersion: prose,
		}}
		got := ResolveDriverSourceEvidence(ad, nil, history, nil, nil)
		if got.Category == model.AuditCategoryInstallHistory {
			t.Fatalf("prose %q was matched to a ledger row and attributed an install", prose)
		}
	}
}

// The positive direction: a real measured version still attributes.
func TestMeasuredVersionStillAttributes(t *testing.T) {
	ad := &model.AssessedDriver{
		Driver: &model.Driver{DriverCode: "D1", DriverName: "X", OSID: "42"},
		DriverAssessment: model.DriverAssessment{
			LocalEvidence: model.ObservedVersion("2.0.0.1"),
			LocalVersion:  "2.0.0.1",
		},
	}
	history := []model.HistoryRecord{{
		Timestamp: "t", DriverCode: "D1", Result: "Installed", Version: "2.0.0.1", OSID: "42",
	}}
	if got := ResolveDriverSourceEvidence(ad, nil, history, nil, nil); got.Category != model.AuditCategoryInstallHistory {
		t.Fatalf("measured version lost its attribution: %q", got.Category)
	}
}
