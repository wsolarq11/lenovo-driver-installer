package audit

import (
	"strings"
	"testing"

	"lenovo-driver/internal/model"
)

func TestGetSourceMapMatch(t *testing.T) {
	driver := &model.Driver{DriverCode: "d1", DriverName: "Audio", LocalVersion: "2.0.0.1"}
	sourceMap := map[string]model.SourceMapEntry{
		"Audio|2.0.0.1": {OSID: "248", OSName: "Windows 11 64-bit"},
	}
	match := GetSourceMapMatch(driver, sourceMap, driver.LocalVersion)
	if match == nil || match.OSID != "248" {
		t.Fatalf("source map match failed: %v", match)
	}
}

func TestResolveExternalDriverSourceLabel(t *testing.T) {
	driver := &model.Driver{DriverName: "Audio", DriverCode: "d1", LocalVersion: "2.0.0.1"}
	alternate := map[string]model.SourceMapEntry{"Audio|2.0.0.1": {OSID: "248", OSName: "Windows 11 64-bit"}}
	got := ResolveExternalDriverSourceLabel(driver, alternate)
	if got != "Local newer (source Windows 11 64-bit)" {
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
	driver := &model.Driver{DriverCode: "d1", DriverName: "Audio", LocalVersion: "2.0.0.1", OSID: "42"}
	device := model.Device{Name: "Audio", DriverVersion: "2.0.0.1", InfName: "oem1.inf"}
	auditResult := ResolveDriverSourceEvidence(driver, []model.Device{device}, nil, nil, nil)
	if auditResult.Category != model.AuditCategoryPreExistingStore {
		t.Fatalf("unexpected audit category: %q", auditResult.Category)
	}
}

func TestSourceEvidenceHistoryOutranksDevice(t *testing.T) {
	driver := &model.Driver{DriverCode: "d1", DriverName: "Audio", LocalVersion: "2.0.0.1", OSID: "42"}
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
	driver := &model.Driver{DriverCode: "d1", DriverName: "Audio", LocalVersion: "2.0.0.1", OSID: "42"}
	device := model.Device{Name: "Audio", DriverVersion: "2.0.0.1", ImportSource: "SW_DVD5", ImportKind: "Offline"}
	auditResult := ResolveDriverSourceEvidence(driver, []model.Device{device}, nil, nil, nil)
	if auditResult.Category != model.AuditCategoryOfflineImage {
		t.Fatalf("offline image should win, got %q", auditResult.Category)
	}
	if !strings.Contains(auditResult.Summary, "SW_DVD5") {
		t.Fatalf("offline summary missing source: %q", auditResult.Summary)
	}
}

func TestSourceEvidenceDeviceOutranksCurrentOS(t *testing.T) {
	driver := &model.Driver{DriverCode: "d1", DriverName: "Audio", LocalVersion: "2.0.0.1", OSID: "42"}
	device := model.Device{Name: "Audio", DriverVersion: "2.0.0.1", InfName: "oem1.inf"}
	current := map[string]model.SourceMapEntry{"Audio|2.0.0.1": {OSID: "42", OSName: "Windows 10 64-bit"}}
	auditResult := ResolveDriverSourceEvidence(driver, []model.Device{device}, nil, current, nil)
	if auditResult.Category != model.AuditCategoryPreExistingStore {
		t.Fatalf("device evidence should outrank current OS list, got %q", auditResult.Category)
	}
}

func TestSourceEvidenceNoMatchFallsBackToUnknown(t *testing.T) {
	driver := &model.Driver{DriverCode: "d1", DriverName: "Audio", LocalVersion: "2.0.0.1", OSID: "42"}
	auditResult := ResolveDriverSourceEvidence(driver, nil, nil, nil, nil)
	if auditResult.Category != model.AuditCategoryUnknown {
		t.Fatalf("no evidence should be Unknown, got %q", auditResult.Category)
	}
	if auditResult.Summary != "No install history, DriverStore import evidence, or official Lenovo version match" {
		t.Fatalf("unexpected fallback summary: %q", auditResult.Summary)
	}
}
