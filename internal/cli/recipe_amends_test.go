package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

// Trial problem #90: true-shark's PR review asked for two changes after its
// recipe completed; the session hand-edited the sealed domain.md and left
// final.md, tasks.json and the evidence stale.  jongyoon chose an amending
// fix recipe: `bts recipe create --type fix --amends <id>` links the change
// to the completed recipe, and once the fix completes, its records answer
// for the files it touched.
func TestRecipeCreate_AmendsACompletedRecipe(t *testing.T) {
	root := newRecipeFixture(t, "r-003", "complete", 3, 0, nil)
	t.Chdir(root)

	id, err := executeRecipeCreate(root, "fix", "Match numbers as stored (PR review)", "r-003", "", false)
	if err != nil {
		t.Fatal(err)
	}
	fix, _ := state.LoadRecipeState(root, id)
	if fix.Amends != "r-003" || fix.RefRecipe != "r-003" || fix.Type != "fix" {
		t.Fatalf("the fix records what it amends, got %+v", fix)
	}
	entries, _ := state.ReadChangelog(root, "r-003")
	if last := entries[len(entries)-1].Result; !strings.Contains(last, id) {
		t.Errorf("the original's changelog names its amendment, got %q", last)
	}

	if _, err := executeRecipeCreate(root, "build", "x", "r-003", "", false); err == nil {
		t.Error("only a fix recipe amends")
	}
	if _, err := executeRecipeCreate(root, "fix", "x", "r-nope", "", false); err == nil {
		t.Error("an unknown recipe cannot be amended")
	}
	if _, err := executeRecipeCreate(root, "fix", "x", id, "", false); err == nil {
		t.Error("a recipe still in progress is not amended; it is continued")
	}
}

func TestCheckCompleteDrift_AnAmendmentAnswersForWhatItTouched(t *testing.T) {
	root := newRecipeFixture(t, "r-003", "complete", 3, 0, nil)
	dir := state.RecipeDir(root, "r-003")
	_ = os.MkdirAll(filepath.Join(root, "src"), 0755)
	_ = os.WriteFile(filepath.Join(root, "src", "relay.py"), []byte("v2\n"), 0644)
	_ = os.WriteFile(filepath.Join(dir, "wireframe.md"), []byte("## File Structure\n\n| File | Action | Depends On | Responsibility |\n|---|---|---|---|\n"+
		"| `src/relay.py` | create | — | the relay |\n| `src/WorkItemNumber.cs` | create | — | the number |\n"), 0644)
	_ = os.WriteFile(filepath.Join(dir, "tasks.json"), []byte(`{"tasks":[{"id":"t-001","file":"src/relay.py","action":"create","status":"done"}]}`), 0644)
	_ = os.WriteFile(filepath.Join(dir, "test-results.json"), []byte(`{"status":"pass","recorded_by":"bts","impl_hash":"sha256:before"}`), 0644)

	fix := &state.RecipeState{ID: "r-005", Type: "fix", Phase: "implement", Amends: "r-003", RefRecipe: "r-003"}
	_ = state.SaveRecipeState(root, fix)
	_ = os.WriteFile(filepath.Join(state.RecipeDir(root, "r-005"), "tasks.json"),
		[]byte(`{"tasks":[{"id":"t-001","file":"src/WorkItemNumber.cs","action":"delete","status":"done"},{"id":"t-002","file":"src/relay.py","action":"modify","status":"done"}]}`), 0644)

	inProgress := checkCompleteDrift(root, "r-003")
	if len(inProgress) == 0 || !strings.Contains(inProgress[0].message, "being amended by r-005") {
		t.Fatalf("while the fix runs, the drift names it, got %+v", inProgress)
	}

	fix.Phase = "complete"
	_ = state.SaveRecipeState(root, fix)
	if done := checkCompleteDrift(root, "r-003"); len(done) != 0 {
		t.Errorf("the completed fix answers for the files it touched, got %+v", done)
	}
}
