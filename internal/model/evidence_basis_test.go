package model

import "testing"

func TestStatusEvidenceBasis(t *testing.T) {
	cases := map[CompareStatus]string{
		StatusUpdate:        "fact",
		StatusUpToDate:      "fact",
		StatusNotInstalled:  "fact",
		StatusLocalNewer:    "fact",
		StatusUnknown:       "undetermined",
		StatusNotApplicable: "undetermined",
	}
	for status, want := range cases {
		if got := StatusEvidenceBasis(status); got != want {
			t.Fatalf("StatusEvidenceBasis(%q) = %q, want %q", status, got, want)
		}
	}
}

// TestAutomaticSetStatusesAreMeasured pairs two decisions that live in
// different places: which statuses may be installed unattended, and how strongly
// each status is evidenced. The automatic set is the only unattended path that
// changes machine state, so every member of it must be a fact. A status that is
// added to the set while its basis is undetermined — or whose basis is later
// downgraded — would let the tool act on something it did not measure.
func TestAutomaticSetStatusesAreMeasured(t *testing.T) {
	all := []CompareStatus{
		StatusUpdate, StatusUpToDate, StatusNotInstalled,
		StatusLocalNewer, StatusUnknown, StatusNotApplicable,
	}
	members := 0
	for _, status := range all {
		if !InAutomaticInstallSet(status) {
			continue
		}
		members++
		if basis := StatusEvidenceBasis(status); basis != "fact" {
			t.Errorf("status %q may enter the automatic install set but its evidence basis is %q, want fact",
				status, basis)
		}
	}
	if members == 0 {
		t.Fatal("automatic install set is empty; the membership rule is no longer being tested")
	}
}

// TestNonFactStatusesStayOutOfTheAutomaticSet is the same pairing from the
// other direction, with the reasons spelled out. It is here so that a future
// widening of the set has to argue with these three specific cases rather than
// just passing a loop.
func TestNonFactStatusesStayOutOfTheAutomaticSet(t *testing.T) {
	for _, status := range []CompareStatus{StatusUnknown, StatusNotApplicable} {
		if basis := StatusEvidenceBasis(status); basis == "fact" {
			t.Fatalf("%q is undetermined; if that changes, this test must be re-argued, not deleted", status)
		}
		if InAutomaticInstallSet(status) {
			t.Fatalf("%q has no measurement behind it and must not enter the automatic install set", status)
		}
	}
}
