package compare

import (
	"strings"
	"testing"

	"lenovo-driver/internal/model"
)

// This file pins the evidence-kind distinctions that replaced a single string.
// Each test states a case where the old string form made two different epistemic
// situations look identical, so the assertion fails if they are ever merged
// again.

// Two matched devices reporting different versions is a measured disagreement.
// The old code joined them into "a, b", which failed to parse, so the plan
// explained it as "neither version could be parsed" — reporting a measurement as
// an unreadable value and hiding the conflict from the operator.
func TestConflictingDeviceVersionsAreReportedAsConflict(t *testing.T) {
	evidence := deviceEvidenceFrom(
		[]model.Device{{Name: "AMD Radeon"}, {Name: "AMD Radeon"}},
		[]string{"30.0.15026.8004", "31.0.14052.9003"},
	)
	if evidence.Kind() != model.LocalEvidenceConflicted {
		t.Fatalf("kind = %v, want Conflicted", evidence.Kind())
	}
	if len(evidence.ConflictingVersions()) != 2 {
		t.Fatalf("conflict must carry both versions, got %v", evidence.ConflictingVersions())
	}
	if got := evidence.MeasuredVersion(); got != "" {
		t.Fatalf("a conflict must not expose a single version, got %q", got)
	}
	if strings.Contains(evidence.Display(), "/") {
		t.Fatalf("display must not invent a composite version: %q", evidence.Display())
	}
}

// Two devices agreeing is one measured version, and duplicate strings must not
// be mistaken for a conflict.
func TestAgreeingDevicesYieldOneVersion(t *testing.T) {
	evidence := deviceEvidenceFrom(
		[]model.Device{{Name: "NVIDIA"}, {Name: "NVIDIA"}},
		[]string{"31.0.15.4630", "31.0.15.4630"},
	)
	if evidence.Kind() != model.LocalEvidenceVersion {
		t.Fatalf("kind = %v, want Version", evidence.Kind())
	}
	if evidence.MeasuredVersion() != "31.0.15.4630" {
		t.Fatalf("version = %q", evidence.MeasuredVersion())
	}
}

// A matched device with no bound driver is measured absence; no matched device
// at all is not. The always-applicable software rules make the second reachable
// while the driver still reports Applicable, so the distinction is load-bearing:
// only the first may produce Not installed, which the automatic set acts on.
func TestNoMatchedDeviceIsUnobservedNotAbsent(t *testing.T) {
	unobserved := deviceEvidenceFrom(nil, nil)
	if unobserved.Kind() != model.LocalEvidenceUnobserved {
		t.Fatalf("kind = %v, want Unobserved", unobserved.Kind())
	}
	if unobserved.Sought() {
		t.Fatal("an unobserved driver must report that nothing was measured")
	}
	absent := deviceEvidenceFrom([]model.Device{{Name: "Camera"}}, []string{""})
	if absent.Kind() != model.LocalEvidenceAbsent {
		t.Fatalf("kind = %v, want Absent", absent.Kind())
	}
	if !absent.Sought() {
		t.Fatal("a matched device with no version is still an observation")
	}
}

// The always-applicable software rule is the concrete path to Unobserved: the
// driver is applicable, matches no device, and has no installed-app row.
func TestSoftwareRuleWithNoEvidenceIsUnobserved(t *testing.T) {
	driver := &model.Driver{DriverName: "Lenovo Fn and Function Keys", Version: "10.1.0.5"}
	if !TestDriverApplicable(driver, nil) {
		t.Skip("fixture relies on an always-applicable rule; the rule set changed")
	}
	if len(GetMatchingLocalDevices(driver, nil)) != 0 {
		t.Fatal("fixture must match no device")
	}
	local, vendor := ResolveLocalDriverVersion(driver, nil, nil, &model.SoftwareSnapshot{})
	if local.Kind() != model.LocalEvidenceUnobserved {
		t.Fatalf("kind = %v, want Unobserved", local.Kind())
	}
	status := CompareDriverStatus(driver.Version, local, vendor)
	if status == model.StatusNotInstalled {
		t.Fatal("a driver with no local evidence must not be reported as Not installed")
	}
	if model.InAutomaticInstallSet(status) {
		t.Fatalf("status %q entered the automatic install set without any evidence", status)
	}
}
