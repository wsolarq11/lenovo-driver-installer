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
	// A version-comparison status carries its own evidence on the Remote and
	// Local lines; only statuses whose inputs do not settle them get a Reason.
	if strings.Contains(text, "Reason :") {
		t.Fatalf("a plain version comparison grew a reason line:\n%s", text)
	}
}

// The plan file is the durable artifact: a Not applicable row whose reason
// lives only in the GUI export or the console leaves the artifact unable to
// answer "why wasn't this installed". The field's own doc comment claims the
// plan surfaces it, so the assertion pins that claim.
func TestBuildPlanTextCarriesRowReason(t *testing.T) {
	notApplicable := &model.AssessedDriver{
		Driver: &model.Driver{DriverName: "Cardreader Driver", OSID: "42"},
		DriverAssessment: model.DriverAssessment{
			CompareStatus:  "Not applicable",
			NonMatchReason: "hardware ids 05E3_0746 matched no local PnP/device ID",
		},
	}
	provisioned := &model.AssessedDriver{
		Driver: &model.Driver{DriverName: "AMD Power Processor", Version: "6.0.0.9", OSID: "42"},
		DriverAssessment: model.DriverAssessment{
			LocalVersion:  model.LocalVersionProvisioned,
			CompareStatus: "Unknown",
		},
	}
	text := strings.Join(BuildPlanText([]*model.AssessedDriver{notApplicable, provisioned}, "2026-01-01 00:00:00"), "\n")
	if !strings.Contains(text, "Reason : hardware ids 05E3_0746 matched no local PnP/device ID") {
		t.Fatalf("plan block dropped the non-match reason:\n%s", text)
	}
	if !strings.Contains(text, "Reason : local version is the Windows provisioning placeholder") {
		t.Fatalf("plan block said nothing about the provisioned placeholder:\n%s", text)
	}
}

// TestFormatStatusSummaryStatusNotes pins the summary notes on the statuses
// the code can actually reach. Unknown has three distinct causes; the per-row
// note names the real one, and the removed "inference" legend explained a
// category StatusEvidenceBasis is gate-forbidden from ever emitting.
func TestFormatStatusSummaryStatusNotes(t *testing.T) {
	drivers := []*model.AssessedDriver{
		{
			Driver: &model.Driver{DriverName: "AMD Power Processor", Version: "6.0.0.9"},
			DriverAssessment: model.DriverAssessment{
				LocalVersion:  model.LocalVersionProvisioned,
				CompareStatus: "Unknown",
			},
		},
		{
			Driver: &model.Driver{DriverName: "Camera Driver"},
			DriverAssessment: model.DriverAssessment{
				CompareStatus:  "Not applicable",
				NonMatchReason: "hardware ids 0408_2010 matched no local PnP/device ID",
			},
		},
	}
	text := strings.Join(FormatStatusSummaryLines(drivers), "\n")
	if strings.Contains(text, "multi-vendor") {
		t.Fatalf("summary still attributes Unknown to multi-vendor packages:\n%s", text)
	}
	if strings.Contains(text, "inference =") {
		t.Fatalf("summary still explains the unreachable inference category:\n%s", text)
	}
	if !strings.Contains(text, "AMD Power Processor: local version is the Windows provisioning placeholder") {
		t.Fatalf("Unknown note does not name the actual cause:\n%s", text)
	}
	if !strings.Contains(text, "Camera Driver: hardware ids 0408_2010 matched no local PnP/device ID") {
		t.Fatalf("Not applicable note does not name the actual cause:\n%s", text)
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
	text := strings.Join(FormatStatusSummaryLines(drivers), "\n")
	if !strings.Contains(text, "Update         : 2") {
		t.Fatalf("summary count missing: %s", text)
	}
	if !strings.Contains(text, "Evidence basis : fact=2 inference=0 undetermined=1") {
		t.Fatalf("evidence basis count missing: %s", text)
	}
}

func TestFormatAttentionNotes(t *testing.T) {
	drivers := []*model.AssessedDriver{
		{Driver: &model.Driver{DriverName: "Audio"}, DriverAssessment: model.DriverAssessment{CompareStatus: "Update"}},
		{Driver: &model.Driver{DriverName: "Fn"}, DriverAssessment: model.DriverAssessment{CompareStatus: "Not installed", DeviceProblem: "Code 43: unknown problem"}},
		{Driver: &model.Driver{DriverName: "Unknown"}, DriverAssessment: model.DriverAssessment{CompareStatus: "Unknown"}},
	}
	notes := FormatAttentionNotes(drivers)
	text := strings.Join(notes, "\n")
	if !strings.Contains(text, "Fn") || !strings.Contains(text, "device problem") {
		t.Fatalf("problem note missing: %s", text)
	}
	if !strings.Contains(text, "Unknown is undetermined") {
		t.Fatalf("evidence note missing: %s", text)
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
