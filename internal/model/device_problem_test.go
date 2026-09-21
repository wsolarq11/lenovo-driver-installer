package model

import "testing"

func TestDeviceProblemLabel(t *testing.T) {
	cases := map[int]string{
		0:  "",
		10: "failed start",
		43: "failed post start",
		52: "unsigned driver",
	}
	for problem, want := range cases {
		if got := DeviceProblemLabel(problem); got != want {
			t.Fatalf("DeviceProblemLabel(%d) = %q, want %q", problem, got, want)
		}
	}
	if DeviceProblemLabel(99) == "" {
		t.Fatal("unknown nonzero problem must have a non-empty fallback label")
	}
}

func TestDeviceProblemSummary(t *testing.T) {
	if got := DeviceProblemSummary(nil); got != "" {
		t.Fatalf("empty devices = %q, want empty", got)
	}
	if got := DeviceProblemSummary([]Device{{ProblemNumber: 0}, {ProblemNumber: 0}}); got != "" {
		t.Fatalf("healthy devices = %q, want empty", got)
	}
	devices := []Device{
		{ProblemNumber: 43},
		{ProblemNumber: 0},
		{ProblemNumber: 43},
		{ProblemNumber: 52},
	}
	got := DeviceProblemSummary(devices)
	want := "Code 43: failed post start; Code 52: unsigned driver"
	if got != want {
		t.Fatalf("DeviceProblemSummary = %q, want %q", got, want)
	}
}
