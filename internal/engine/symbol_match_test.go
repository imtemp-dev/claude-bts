package engine

import "testing"

// Task scope symbols are not always identifiers: a path (".venv/"), a
// flag ("--dry-run") or a dotted name. `\b` needs a word character on one
// side, so `\b.venv/\b` never matched anything (AGEN-35 owner report 4).
func TestContainsSymbol(t *testing.T) {
	cases := []struct {
		content, sym string
		want         bool
	}{
		{"keep .venv/ out of git", ".venv/", true},
		{"run it with --dry-run.", "--dry-run", true},
		{"from relay import foo_bar\n", "foo_bar", true},
		{"def foobar(): pass", "foo", false},
		{"x = myfoo_bar", "foo_bar", false},
		{"config.yaml is read", "config.yaml", true},
		{"myconfig.yaml is read", "config.yaml", false},
		{"no such thing here", ".venv/", false},
	}
	for _, c := range cases {
		if got := ContainsSymbol(c.content, c.sym); got != c.want {
			t.Errorf("ContainsSymbol(%q, %q) = %v, want %v", c.content, c.sym, got, c.want)
		}
	}
}
