package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

// `bts falsifier table --write` puts the rendered table into final.md § 2
// and keeps the rest of the document (trial problem #97).
func TestFalsifierTable_WritesSectionTwo(t *testing.T) {
	root := newRecipeFixture(t, "r-097", "falsify", 0, 0, nil)
	dir := state.RecipeDir(root, "r-097")
	_ = os.WriteFile(filepath.Join(dir, "domain.md"), []byte("## 2. Invariants\n\n| ID | Statement | Owner |\n|---|---|---|\n| INV-001 | one | relay |\n"), 0644)
	_ = os.WriteFile(filepath.Join(dir, "final.md"), []byte("# Build\n\n## 2. Invariants and their falsifiers\n\n"+
		"| Invariant | Test | Command |\n| --- | --- | --- |\n| INV-001 | by hand | by hand |\n\n## 3. Boundary contracts\nKept.\n"), 0644)
	if err := state.SaveBuildEvidence(root, "r-097", &state.BuildEvidence{Falsifiers: map[string]*state.Falsifier{
		"INV-001": {Command: "uv run pytest -q tests/test_relay.py::test_one"},
	}}); err != nil {
		t.Fatal(err)
	}
	if _, err := executeFalsifierTable(root, "r-097", true); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "final.md"))
	got := string(data)
	if strings.Contains(got, "by hand") || !strings.Contains(got, "`tests/test_relay.py::test_one`") || !strings.Contains(got, "## 3. Boundary contracts\nKept.") {
		t.Fatalf("§ 2 comes from the records and the rest stays:\n%s", got)
	}
}
