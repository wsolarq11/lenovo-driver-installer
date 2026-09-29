package app

import (
	"testing"

	"lenovo-driver/internal/model"
)

// These tests pin invariant 3 ("default inaction") at the partition that feeds
// the interactive "a" action. On 82JQ the previous rule admitted every status
// except Not applicable, so the 9 Local newer drivers were inside the set the
// tool would install unattended; pressing a would have downgraded AMD VGA from
// 30.0.14052.9003 to 27.20.15026.8004 and NVIDIA from 31.0.15.4630 to
// 31.0.15.2799.

func assessed(status model.CompareStatus) *model.AssessedDriver {
	return &model.AssessedDriver{Driver: &model.Driver{DriverCode: "d1"}, CompareStatus: status}
}

func TestAutomaticInstallSetExcludesDowngradeAndNoop(t *testing.T) {
	all := []model.CompareStatus{
		model.StatusUpdate,
		model.StatusUpToDate,
		model.StatusNotInstalled,
		model.StatusLocalNewer,
		model.StatusUnknown,
		model.StatusNotApplicable,
	}
	var rows []*model.AssessedDriver
	for _, status := range all {
		rows = append(rows, assessed(status))
	}
	applicable, updates := partitionViewDrivers(rows)

	if len(applicable) != 2 {
		t.Fatalf("automatic install set = %d drivers, want 2 (Update + Not installed)", len(applicable))
	}
	for _, ad := range applicable {
		if !model.InAutomaticInstallSet(ad.CompareStatus) {
			t.Fatalf("status %q leaked into the automatic install set", ad.CompareStatus)
		}
	}
	if len(updates) != 1 || updates[0].CompareStatus != model.StatusUpdate {
		t.Fatalf("update-only set = %v, want exactly the Update driver", updates)
	}
}

// The 82JQ measurement that motivated the rule: 9 of 11 candidates were
// Local newer. A regression here re-arms a silent driver downgrade.
func TestLocalNewerNeverEntersAutomaticSet(t *testing.T) {
	var rows []*model.AssessedDriver
	for range 9 {
		rows = append(rows, assessed(model.StatusLocalNewer))
	}
	rows = append(rows, assessed(model.StatusNotInstalled))

	applicable, _ := partitionViewDrivers(rows)
	if len(applicable) != 1 {
		t.Fatalf("automatic install set = %d, want 1 (the 9 Local newer must be excluded)", len(applicable))
	}
	if applicable[0].CompareStatus != model.StatusNotInstalled {
		t.Fatalf("surviving driver status = %q, want Not installed", applicable[0].CompareStatus)
	}
}

func TestInAutomaticInstallSetIsExplicit(t *testing.T) {
	// Documented membership; a status added to the enum later must be decided
	// here rather than silently admitted by a loose comparison.
	for status, want := range map[model.CompareStatus]bool{
		model.StatusUpdate:        true,
		model.StatusNotInstalled:  true,
		model.StatusUpToDate:      false,
		model.StatusLocalNewer:    false,
		model.StatusUnknown:       false,
		model.StatusNotApplicable: false,
	} {
		if got := model.InAutomaticInstallSet(status); got != want {
			t.Fatalf("InAutomaticInstallSet(%q) = %v, want %v", status, got, want)
		}
	}
}
