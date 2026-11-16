package feature

import "testing"

func TestSameID(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"f-3", "f-003-reports", true},
		{"f-03", "f-003", true},
		{"f-003-reports", "f-003-other-slug", true},
		{" f-3 ", "f-003", true},
		{"f-3", "f-030", false},
		{"f-3", "f-4", false},
		{"reports", "f-003", false},
		{"", "", false},
	}
	for _, c := range cases {
		if got := SameID(c.a, c.b); got != c.want {
			t.Errorf("SameID(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}
