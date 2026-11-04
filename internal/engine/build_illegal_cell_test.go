package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A build recipe has no simulations. Its illegal cells are proven by
// falsifier tests tagged in final.md, and `bts validate` must not send it
// back to simulate. Found on the first real build recipe (AGEN-35), where
// validate reported seven uncovered cells before the skeleton existed.
func TestValidate_BuildRecipeIllegalCellsAreCoveredByFalsifiers(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("recipe.json", `{"id":"r-1","type":"build","phase":"skeleton","topic":"x","created":"2026-11-05T00:00:00Z","updated":"2026-11-05T00:00:00Z"}`)
	write("domain.md", "## 4. Combinatorial State Space\n\n| x | ILLEGAL: second issue for one PR |\n| y | ILLEGAL: speaking as jongyoon |\n")

	cellErrors := func() []string {
		errs, err := ValidateRecipeDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, e := range errs {
			if strings.Contains(e.Message, "illegal_cell") {
				out = append(out, e.Message)
			}
		}
		return out
	}

	if got := cellErrors(); len(got) != 0 {
		t.Fatalf("before final.md exists a build recipe has nothing to cover yet, got %v", got)
	}

	write("final.md", "## 2. Invariants and their falsifiers\n\n| Invariant | Test | Command |\n|---|---|---|\n"+
		"| INV-003 [illegal-cell: second issue for one PR] | test_inv_003 | pytest -k inv_003 |\n")
	got := cellErrors()
	if len(got) != 1 || !strings.Contains(got[0], "untested_illegal_cell: speaking as jongyoon") {
		t.Fatalf("an untagged cell must be named, and only it: %v", got)
	}
	for _, m := range got {
		if strings.Contains(m, "simulation") && strings.Contains(m, "uncovered_illegal_cell") {
			t.Fatalf("a build recipe must not be asked for simulation scenarios: %s", m)
		}
	}

	write("final.md", "| INV-003 [illegal-cell: second issue for one PR] | a | b |\n| INV-007 [illegal-cell: Speaking as jongyoon] | c | d |\n")
	if got := cellErrors(); len(got) != 0 {
		t.Fatalf("every cell tagged in final.md must be clean, got %v", got)
	}
}
