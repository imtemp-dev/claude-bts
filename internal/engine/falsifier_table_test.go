package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

// Trial problem #97: swift-eagle hand-wrote final.md § 2's 30 rows, which
// build-evidence.json already held.  The table renders from the records.
func TestRenderFalsifierTable(t *testing.T) {
	dir := t.TempDir()
	domain := filepath.Join(dir, "domain.md")
	_ = os.WriteFile(domain, []byte("## 2. Invariants\n\n| ID | Statement | Owner |\n|---|---|---|\n"+
		"| INV-010 | ten | relay |\n| INV-002 | two | relay |\n| INV-004 | four | grounding |\n| INV-005 [carried] | five | relay |\n\n"+
		"## 4. Combinatorial State Space\n\n| Cell | Status | Note |\n|---|---|---|\n"+
		"| an ambiguous Reference | **ILLEGAL: guessed record** | prevented by grounding's match count (INV-004) |\n"), 0644)
	ev := &state.BuildEvidence{Falsifiers: map[string]*state.Falsifier{
		"INV-002": {Command: "uv run pytest -q tests/test_relay.py::test_two"},
		"INV-004": {Command: `uv run pytest -q tests/test_grounding.py -k "ambiguous or guessed"`},
		"INV-005": {Command: "dotnet test --filter Name~OldToken"},
	}}
	table, err := RenderFalsifierTable(ev, domain)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"| Invariant | Test | Command |",
		"| INV-002 | `tests/test_relay.py::test_two` | `uv run pytest -q tests/test_relay.py::test_two` |",
		"| INV-004 [illegal-cell: guessed record] | `ambiguous or guessed` |",
		"| INV-005 [carried] | `Name~OldToken` |",
		"| INV-010 | (none recorded) |",
	}
	for _, w := range want {
		if !strings.Contains(table, w) {
			t.Errorf("table lacks %q:\n%s", w, table)
		}
	}
	if strings.Index(table, "INV-002") > strings.Index(table, "INV-010") {
		t.Errorf("rows go in id order, INV-002 before INV-010:\n%s", table)
	}
}

func TestReplaceSectionTable(t *testing.T) {
	doc := "# Build\n\n## 1. What ships\nA thing.\n\n## 2. Invariants and their falsifiers\nThe invariants are domain.md § 2.\n\n" +
		"| Invariant | Test | Command |\n| --- | --- | --- |\n| INV-001 | old | old |\n\nA note after.\n\n## 3. Boundary contracts\nKept.\n"
	got, ok := ReplaceSectionTable(doc, "## 2.", "| Invariant | Test | Command |\n| --- | --- | --- |\n| INV-001 | new | new |\n")
	if !ok || strings.Contains(got, "| old |") || !strings.Contains(got, "| INV-001 | new | new |") ||
		!strings.Contains(got, "A note after.") || !strings.Contains(got, "## 3. Boundary contracts\nKept.") {
		t.Fatalf("the table is replaced in place, the rest kept:\n%s", got)
	}
	if _, ok := ReplaceSectionTable("# Build\n\n## 1. What ships\n", "## 2.", "| x |\n"); ok {
		t.Error("no § 2 heading: nothing to replace")
	}
}
