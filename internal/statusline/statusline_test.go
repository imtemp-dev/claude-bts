package statusline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

// The status line read the work state first, and the work state can still
// name a recipe that was cancelled since, so the line showed that recipe's
// phase (trial problem #14, Multica AGEN-64).  A work state whose recipe is
// cancelled or gone is skipped; a live one is shown as before.
func TestRenderRecipeSegment_SkipsACancelledRecipesWorkState(t *testing.T) {
	root := t.TempDir()
	for _, id := range []string{"r-1-old", "r-2-live"} {
		if err := os.MkdirAll(state.RecipeDir(root, id), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(root, ".bts", "local"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := state.SaveRecipeState(root, &state.RecipeState{ID: "r-1-old", Type: "build", Phase: "cancelled"}); err != nil {
		t.Fatal(err)
	}
	if err := state.SaveWorkState(root, &state.WorkState{RecipeID: "r-1-old", Phase: "implement"}); err != nil {
		t.Fatal(err)
	}
	if got := renderRecipeSegment(root); strings.Contains(got, "implement") {
		t.Fatalf("a cancelled recipe's phase must not show, got %q", got)
	}

	for _, ws := range []*state.WorkState{{RecipeID: "r-9-gone", Phase: "implement"}} {
		if err := state.SaveWorkState(root, ws); err != nil {
			t.Fatal(err)
		}
		if got := renderRecipeSegment(root); strings.Contains(got, "implement") {
			t.Fatalf("a recipe with no recipe.json must not show, got %q", got)
		}
	}

	if err := state.SaveRecipeState(root, &state.RecipeState{ID: "r-2-live", Type: "build", Phase: "implement"}); err != nil {
		t.Fatal(err)
	}
	if err := state.SaveWorkState(root, &state.WorkState{RecipeID: "r-2-live", Phase: "implement"}); err != nil {
		t.Fatal(err)
	}
	if got := renderRecipeSegment(root); !strings.Contains(got, "implement") {
		t.Fatalf("a live recipe's phase still shows, got %q", got)
	}
}
