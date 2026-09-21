package app

import "testing"

func TestRollbackCausalBasis(t *testing.T) {
	cases := []struct {
		name          string
		before, after string
		want          string
	}{
		{"clean after install", "", "", ""},
		{"new problem", "", "failed post start", "new"},
		{"pre-existing problem", "driver failed load", "driver failed load", "pre-existing"},
		{"pre-existing changed", "driver failed load", "failed post start", "pre-existing"},
	}
	for _, c := range cases {
		if got := rollbackCausalBasis(c.before, c.after); got != c.want {
			t.Errorf("%s: rollbackCausalBasis(%q, %q) = %q, want %q", c.name, c.before, c.after, got, c.want)
		}
	}
}
