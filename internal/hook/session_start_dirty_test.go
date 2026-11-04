package hook

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

// After implementation, sync edits final.md to match the code on purpose.
// Rule 3 ("every modification needs /bts-verify") is a spec-phase rule; a
// warning in an implement-family phase sent the AGEN-35 worker back into
// model review rounds on a spec that now only records tested code.
func TestDirtyDocWarning_SpecPhasesOnly(t *testing.T) {
	root, id := setupStopRoot(t)
	finalPath := filepath.Join(state.RecipeDir(root, id), "final.md")
	if err := os.WriteFile(finalPath, []byte("# Spec\n"), 0644); err != nil {
		t.Fatal(err)
	}
	hash, ok, err := state.FileContentHash(finalPath)
	if err != nil || !ok {
		t.Fatalf("hash: %v %v", ok, err)
	}
	writeVerifyLog(t, root, id, []state.VerifyLogEntry{
		{Iteration: 1, Doc: "final.md", FullPass: true, DocHash: hash, Status: "continue"},
	})
	if err := os.WriteFile(finalPath, []byte("# Spec\n\nSynced with the code.\n"), 0644); err != nil {
		t.Fatal(err)
	}

	for phase, wantWarning := range map[string]bool{
		"verify": true, "finalize": true,
		"implement": false, "test": false, "review": false, "sync": false, "status": false,
	} {
		recipe, _ := state.LoadRecipeState(root, id)
		recipe.Phase = phase
		got := dirtyDocWarning(root, recipe)
		if wantWarning != strings.Contains(got, "final.md modified after last verification") {
			t.Errorf("phase %s: warning %q, want warning=%v", phase, got, wantWarning)
		}
	}
}
