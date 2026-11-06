package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

// Trial problem #92: after true-shark's recipe completed, a PR review
// deleted WorkItemNumber.cs and changed the code; doctor and validate
// stayed clean, and `bts verify` read the deleted file as "File to be
// created".  A completed recipe's planned files and fingerprint are now
// checked against the disk.
func TestCheckCompleteDrift(t *testing.T) {
	root := newRecipeFixture(t, "r-092", "complete", 3, 0, nil)
	dir := state.RecipeDir(root, "r-092")
	_ = os.MkdirAll(filepath.Join(root, "src"), 0755)
	_ = os.WriteFile(filepath.Join(root, "src", "relay.py"), []byte("def handle(): pass\n"), 0644)
	_ = os.WriteFile(filepath.Join(dir, "wireframe.md"), []byte("## File Structure\n\n| File | Action | Depends On | Responsibility |\n|---|---|---|---|\n"+
		"| `src/relay.py` | create | — | the relay |\n| `src/WorkItemNumber.cs` | create | — | the number |\n"), 0644)
	_ = os.WriteFile(filepath.Join(dir, "tasks.json"), []byte(`{"tasks":[{"id":"t-001","file":"src/relay.py","action":"create","status":"done"}]}`), 0644)
	_ = os.WriteFile(filepath.Join(dir, "test-results.json"), []byte(`{"status":"pass","recorded_by":"bts","impl_hash":"sha256:before-the-review"}`), 0644)

	issues := checkCompleteDrift(root, "r-092")
	var msgs []string
	for _, is := range issues {
		msgs = append(msgs, is.message)
	}
	got := strings.Join(msgs, " | ")
	if !strings.Contains(got, "src/WorkItemNumber.cs") {
		t.Errorf("a planned file gone after completion is drift: %s", got)
	}
	if !strings.Contains(got, "changed after it completed") {
		t.Errorf("a fingerprint unlike the one its tests ran on is drift: %s", got)
	}

	r, _ := state.LoadRecipeState(root, "r-092")
	r.Phase = "implement"
	_ = state.SaveRecipeState(root, r)
	if issues := checkCompleteDrift(root, "r-092"); len(issues) != 0 {
		t.Errorf("a recipe still in progress is not drifting, got %v", issues)
	}
}
