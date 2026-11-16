package feature

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

// bts #158: the draft pull request's body carries what a reviewer needs,
// and says which recipe documents it cites are not in the pull request.
func TestPRBodyIsReadableWithoutTheRecipe(t *testing.T) {
	root := t.TempDir()
	id := "r-001-format-elapsed"
	dir := state.RecipeDir(root, id)
	write := func(name, body string) {
		t.Helper()
		p := filepath.Join(dir, filepath.FromSlash(name))
		_ = os.MkdirAll(filepath.Dir(p), 0755)
		if err := os.WriteFile(p, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("final.md", "# Build: format_elapsed\n\n## 1. What ships\n\n"+
		"A new module `agent_server/elapsed_time.py` with `format_elapsed(seconds)`.  The invariants are `domain.md` § 2, "+
		"and the decomposition is `wireframe.md` (File Structure § 4).\n\n"+
		"## 2. Invariants and their falsifiers\n\nThe table below.\n\n"+
		"| Invariant | Test | Command |\n| --- | --- | --- |\n"+
		"| INV-001 | `test_under_a_minute` | `uv run pytest -q tests/test_elapsed_time.py::test_under_a_minute` |\n"+
		"| INV-002 | `test_negative` | `uv run pytest -q tests/test_elapsed_time.py::test_negative` |\n\n"+
		"## 6. Scope coverage\n\n| Scope item | Held by |\n| --- | --- |\n| 1 | INV-001 |\n")
	write("domain.md", "## 2. Invariants\n")
	write("wireframe.md", "## File Structure\n")
	write("deviation.md", "# Deviation Report\n\n## Summary\n\n- Matches: 6\n- Not Implemented: 0\n- Spec Additions Needed: 2\n- Deviations: 2\n")
	if err := state.SaveTestResults(root, id, &state.TestResults{RecipeID: id, Status: "pass", Command: "cd agent_server && uv run pytest tests/test_elapsed_time.py -q", Total: 20, Passed: 20, RecordedBy: "bts"}); err != nil {
		t.Fatal(err)
	}
	if err := state.SaveInterrogateRun(root, id, &state.InterrogateRun{VerifiedAt: "2026-11-16T02:00:00Z", Findings: []*state.InterrogateFinding{
		{ID: "F-001", Status: state.InterrogateFixed}, {ID: "F-002", Status: state.InterrogateFixed}, {ID: "F-003", Status: state.InterrogateDropped},
	}}); err != nil {
		t.Fatal(err)
	}

	pc := &prContext{root: root, r: &state.RecipeState{ID: id}, f: &Feature{ID: "f-003-elapsed"}, plan: PlanItem{ID: "i1", Title: "format_elapsed helper"}}
	body := prBody(pc, "AGEN-172")

	for _, want := range []string{
		"Item i1 of bts feature f-003-elapsed: format_elapsed helper.",
		"A new module `agent_server/elapsed_time.py`",
		"| INV-001 | `test_under_a_minute` |",                              // the invariants table, in the body
		"`cd agent_server && uv run pytest tests/test_elapsed_time.py -q`", // the test command
		"20 passed",
		"2 reproduced, 2 fixed", // interrogate, dropped ones left out
		"2 deviations",          // deviation summary
		"`domain.md` and `wireframe.md` are bts records", // the footer names what the body cites
		"Tracked as AGEN-172.",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the body lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "Scope item") {
		t.Errorf("only the invariants table is carried:\n%s", body)
	}
	if strings.Contains(body, root) {
		t.Errorf("no local path in a body other people read:\n%s", body)
	}
}
