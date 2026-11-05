package mdtable

import (
	"reflect"
	"strings"
	"testing"
)

func trimmed(cells []string) []string {
	out := make([]string, len(cells))
	for i, c := range cells {
		out[i] = strings.TrimSpace(c)
	}
	return out
}

func TestSplitRow(t *testing.T) {
	cases := map[string]struct {
		line string
		want []string
	}{
		"plain row": {"| INV-001 | a | Owner |", []string{"INV-001", "a", "Owner"}},
		// Trial problem #40: r-003's INV-014 row held `<!subteam^S\|label>` in a
		// code span; the old split made two cells of it and a false
		// invariant_multiple_owners critical.
		"escaped pipe in a code span": {"| INV-014 | `<!subteam^S0\\|team>` is a mention | Rules |",
			[]string{"INV-014", "`<!subteam^S0|team>` is a mention", "Rules"}},
		"escaped pipe outside code":   {"| a \\| b | c |", []string{"a | b", "c"}},
		"no outer pipes":              {"a | b", []string{"a", "b"}},
		"escaped pipe at the end":     {"| a | b \\|", []string{"a", "b |"}},
		"empty inner cell kept":       {"| a |  | c |", []string{"a", "", "c"}},
		"backslash not before a pipe": {"| C:\\path | x |", []string{"C:\\path", "x"}},
	}
	for name, tc := range cases {
		if got := trimmed(SplitRow(tc.line)); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: SplitRow(%q) = %q, want %q", name, tc.line, got, tc.want)
		}
	}
}
