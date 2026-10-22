package app

import "testing"

func TestRollbackCausalBasis(t *testing.T) {
	cases := []struct {
		name          string
		before, after string
		want          causalBasis
	}{
		{"clean after install", "", "", causalNone},
		{"new problem", "", "failed post start", causalNew},
		{"pre-existing problem", "driver failed load", "driver failed load", causalPreExisting},
		{"pre-existing changed", "driver failed load", "failed post start", causalPreExisting},
	}
	for _, c := range cases {
		if got := rollbackCausalBasis(c.before, c.after); got != c.want {
			t.Errorf("%s: rollbackCausalBasis(%q, %q) = %v, want %v", c.name, c.before, c.after, got, c.want)
		}
	}
}
