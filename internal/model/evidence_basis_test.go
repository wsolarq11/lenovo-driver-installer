package model

import "testing"

func TestStatusEvidenceBasis(t *testing.T) {
	cases := map[CompareStatus]string{
		StatusUpdate:        "fact",
		StatusUpToDate:      "fact",
		StatusNotInstalled:  "inference",
		StatusLocalNewer:    "inference",
		StatusUnknown:       "undetermined",
		StatusNotApplicable: "undetermined",
	}
	for status, want := range cases {
		if got := StatusEvidenceBasis(status); got != want {
			t.Fatalf("StatusEvidenceBasis(%q) = %q, want %q", status, got, want)
		}
	}
}
