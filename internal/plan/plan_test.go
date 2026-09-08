package plan

import (
	"strings"
	"testing"

	"lenovo-driver/internal/model"
)

func TestBuildPlanText(t *testing.T) {
	driver := &model.AssessedDriver{
		Driver: &model.Driver{
			DriverName:  "Realtek Audio",
			Version:     "6.0.9363.1",
			OSName:      "Windows 10 64-bit",
			OSID:        "42",
			SourceAPI:   "QuickFix",
			FileName:    "audio.exe",
			FilePath:    "https://example.invalid/audio.exe",
			OfficialMD5: "abc",
		},
		DriverAssessment: model.DriverAssessment{
			LocalVersion:  "6.0.9363.0",
			CompareStatus: "Update",
		},
	}
	lines := BuildPlanText([]*model.AssessedDriver{driver}, "2026-01-01 00:00:00")
	text := strings.Join(lines, "\n")
	for _, expected := range []string{"Realtek Audio", "6.0.9363.1", "Update", "Windows 10 64-bit", "QuickFix", "audio.exe"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("plan missing %q:\n%s", expected, text)
		}
	}
}

func TestFormatDriverTableLines(t *testing.T) {
	driver := &model.AssessedDriver{
		Driver: &model.Driver{DriverName: "AMD VGA", Version: "1.0.0.1"},
		DriverAssessment: model.DriverAssessment{
			LocalVersion:  "1.0.0.0",
			CompareStatus: "Update",
		},
	}
	lines := FormatDriverTableLines([]*model.AssessedDriver{driver})
	if len(lines) != 5 {
		t.Fatalf("expected 4 table lines, got %d", len(lines))
	}
	if !strings.Contains(strings.Join(lines, "\n"), "AMD VGA") {
		t.Fatalf("driver row missing name: %#v", lines)
	}
}

func TestFormatStatusSummaryLines(t *testing.T) {
	drivers := []*model.AssessedDriver{
		{Driver: &model.Driver{}, DriverAssessment: model.DriverAssessment{CompareStatus: "Update"}},
		{Driver: &model.Driver{}, DriverAssessment: model.DriverAssessment{CompareStatus: "Update"}},
		{Driver: &model.Driver{}, DriverAssessment: model.DriverAssessment{CompareStatus: "Not applicable"}},
	}
	lines := FormatStatusSummaryLines(drivers)
	if !strings.Contains(strings.Join(lines, "\n"), "Update         : 2") {
		t.Fatalf("summary count missing: %#v", lines)
	}
}

func TestParseDriverSelectionTokens(t *testing.T) {
	drivers := []*model.AssessedDriver{
		{Driver: &model.Driver{DriverName: "Audio"}, DriverAssessment: model.DriverAssessment{CompareStatus: "Update"}},
		{Driver: &model.Driver{DriverName: "Camera"}, DriverAssessment: model.DriverAssessment{CompareStatus: "Not applicable"}},
		{Driver: &model.Driver{DriverName: "Wi-Fi"}, DriverAssessment: model.DriverAssessment{CompareStatus: "Update"}},
	}
	result := ParseDriverSelectionTokens("1,2,3,9,abc", drivers)
	if len(result.Selected) != 2 {
		t.Fatalf("selected count = %d", len(result.Selected))
	}
	if result.Selected[0].DriverName != "Audio" || result.Selected[1].DriverName != "Wi-Fi" {
		t.Fatalf("unexpected selection: %#v", result.Selected)
	}
	if len(result.Invalid) != 2 {
		t.Fatalf("invalid count = %d", len(result.Invalid))
	}
	if len(result.NotApplicable) != 1 {
		t.Fatalf("not applicable count = %d", len(result.NotApplicable))
	}
}

func TestBuildDriverHistoryRecord(t *testing.T) {
	driver := &model.AssessedDriver{
		Driver: &model.Driver{
			DriverCode:  "d1",
			OSID:        "42",
			OSName:      "Windows 10 64-bit",
			DriverName:  "Audio",
			Version:     "6.0.9363.1",
			FileName:    "audio.exe",
			OfficialMD5: "abc",
			SourceAPI:   "QuickFix",
		},
	}
	record := BuildDriverHistoryRecord(driver, "Installed", "exit=0", "6.0.9363.1", "6.0.9363.0", "2026-01-01 00:00:00")
	if record.DriverCode != "d1" || record.Result != "Installed" || record.VerifiedVersion != "6.0.9363.1" {
		t.Fatalf("history record mismatch: %#v", record)
	}
}
