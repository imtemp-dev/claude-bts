package hook

import "testing"

// A recipe started before the jig rebrand can still emit <bts>DONE</bts> —
// the marker is written by the model from a SKILL.md it may have loaded
// before the templates were updated. Failing to recognise it does not just
// lose a completion: it routes the turn to the blind-stop path, so the
// completion gate never runs at all.
func TestHasMarkerAcceptsBothSpellings(t *testing.T) {
	cases := []struct {
		name    string
		content string
		marker  string
		want    bool
	}{
		{"current spelling", "all set <jig>DONE</jig>", "DONE", true},
		{"legacy spelling", "all set <bts>DONE</bts>", "DONE", true},
		{"legacy fix marker", "<bts>FIX DONE</bts>", "FIX DONE", true},
		{"legacy implement marker", "<bts>IMPLEMENT DONE</bts>", "IMPLEMENT DONE", true},
		{"untagged prose is not a marker", "I am done with this", "DONE", false},
		{"mismatched tags are not a marker", "<jig>DONE</bts>", "DONE", false},
		{"different marker", "<jig>FIX DONE</jig>", "DONE", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := hasMarker(c.content, c.marker); got != c.want {
				t.Errorf("hasMarker(%q, %q) = %v, want %v", c.content, c.marker, got, c.want)
			}
		})
	}
}
