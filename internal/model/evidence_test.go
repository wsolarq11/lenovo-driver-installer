package model

import "testing"

// This file pins the union's structural guarantees. They are not style points:
// each one is a way the old string form let a forgotten or malformed value reach
// the automatic install set.

// The zero value must mean "nothing was observed". It used to mean Absent, so a
// path that never resolved evidence produced Not installed, the one status the
// unattended set acts on.
func TestZeroEvidenceMeansNothingObserved(t *testing.T) {
	var zero LocalEvidence
	if zero.Kind() != LocalEvidenceUnobserved {
		t.Fatalf("zero evidence kind = %d, want Unobserved", zero.Kind())
	}
	if zero.Sought() {
		t.Fatal("the zero value must not claim the machine was observed")
	}
	if zero.MeasuredVersion() != "" {
		t.Fatalf("the zero value must carry no version, got %q", zero.MeasuredVersion())
	}
}

// Constructors refuse to mint contradictions. A "conflict" of one version is
// just a version; a "version" of nothing is nothing.
func TestConstructorsRejectIllegalStates(t *testing.T) {
	if got := ObservedVersion(""); got.Kind() != LocalEvidenceUnobserved {
		t.Fatalf("ObservedVersion(\"\") = %d, want Unobserved", got.Kind())
	}
	if got := ObservedConflict([]string{"1.0"}); got.Kind() != LocalEvidenceVersion {
		t.Fatalf("one-version conflict = %d, want Version", got.Kind())
	}
	if got := ObservedConflict(nil); got.Kind() != LocalEvidenceUnobserved {
		t.Fatalf("empty conflict = %d, want Unobserved", got.Kind())
	}
	if got := ObservedConflict([]string{"2.0", "2.0"}); got.Kind() != LocalEvidenceVersion {
		t.Fatalf("duplicate versions are one measurement, got kind %d", got.Kind())
	}
}

// The conflict payload is a copy, so a caller holding the result cannot rewrite
// the evidence another consumer is about to read.
func TestConflictPayloadIsCopied(t *testing.T) {
	e := ObservedConflict([]string{"2.0", "1.0"})
	got := e.ConflictingVersions()
	if len(got) != 2 || got[0] != "1.0" || got[1] != "2.0" {
		t.Fatalf("conflicting versions = %v, want sorted [1.0 2.0]", got)
	}
	got[0] = "tampered"
	if e.Display() != "1.0, 2.0" {
		t.Fatalf("mutating the returned slice changed the evidence: %q", e.Display())
	}
}

// A state that carries no version must not expose one, and one that carries a
// version must not expose a conflict.
func TestPayloadsDoNotLeakAcrossStates(t *testing.T) {
	measured := ObservedVersion("1.2.3.4")
	if measured.ConflictingVersions() != nil {
		t.Fatal("a single measurement must not report a conflict payload")
	}
	conflicted := ObservedConflict([]string{"1.0", "2.0"})
	if conflicted.MeasuredVersion() != "" {
		t.Fatalf("a conflict must not expose a single version, got %q", conflicted.MeasuredVersion())
	}
	provisioned := ObservedProvisioning()
	if provisioned.MeasuredVersion() != "" || provisioned.ConflictingVersions() != nil {
		t.Fatal("the provisioning state must carry neither a version nor a conflict")
	}
}

// Only a measured absence may back "Not installed". The comparison is made
// through the evidence kind, so this pins that the three non-absence states are
// never mistaken for one.
func TestNotInstalledRequiresObservedAbsence(t *testing.T) {
	absent := ObservedAbsence()
	if !absent.Sought() || absent.Kind() != LocalEvidenceAbsent {
		t.Fatal("ObservedAbsence must report a sought, absent observation")
	}
	for name, e := range map[string]LocalEvidence{
		"unobserved":  NotObserved(),
		"provisioned": ObservedProvisioning(),
		"conflicted":  ObservedConflict([]string{"1.0", "2.0"}),
		"measured":    ObservedVersion("1.0"),
	} {
		if e.Kind() == LocalEvidenceAbsent {
			t.Fatalf("%s was classified as measured absence", name)
		}
	}
}
