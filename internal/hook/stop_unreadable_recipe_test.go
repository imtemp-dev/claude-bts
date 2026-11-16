package hook

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

// Plan N5 (AGEN-78): GetActiveRecipe skips a recipe.json that does not
// parse, so the Stop hook found no recipe and let any completion marker
// pass.  A marker with no readable recipe blocks, naming the file.
func TestStop_MarkerWithAnUnreadableRecipeBlocks(t *testing.T) {
	root, recipeID := setupStopRoot(t)
	path := filepath.Join(state.RecipeDir(root, recipeID), "recipe.json")
	if err := os.WriteFile(path, []byte(`{"id": "`+recipeID+`", "phase": "implement",`), 0644); err != nil {
		t.Fatal(err)
	}

	out, err := NewStopHandler().Handle(&HookInput{CWD: root, SessionID: "s-1", LastAssistantMessage: "All green.\n<bts>IMPLEMENT DONE</bts>"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Decision != "block" {
		t.Fatal("a completion marker with an unreadable recipe.json must not pass as if no recipe were running")
	}
	rel := ".bts/specs/recipes/" + recipeID + "/recipe.json"
	if !strings.Contains(out.Reason, "<bts>IMPLEMENT DONE</bts> was not applied") || !strings.Contains(out.Reason, rel) {
		t.Errorf("the block must name the marker and the unreadable file, got: %s", out.Reason)
	}

	// A turn with no marker is not held for it.
	if out := blindStop(t, root); out.Decision == "block" {
		t.Fatalf("no marker: the turn may end, got: %s", out.Reason)
	}
	// Nor is a quoted marker.
	if out, _ := NewStopHandler().Handle(&HookInput{CWD: root, SessionID: "s-1", LastAssistantMessage: "It said `<bts>DONE</bts>`."}); out.Decision == "block" {
		t.Fatalf("a quoted marker completes nothing and is not held: %s", out.Reason)
	}

	// bts recipe finish names the file too.
	if _, err := Finish(root, recipeID, "s-1"); err == nil || !strings.Contains(err.Error(), rel) {
		t.Errorf("finish must name the unreadable recipe.json, got: %v", err)
	}
}

// A recipe directory with no recipe.json is not an unreadable recipe.
func TestUnreadableRecipes_IgnoresADirectoryWithoutRecipeJSON(t *testing.T) {
	root, _ := setupStopRoot(t)
	if err := os.MkdirAll(filepath.Join(state.SpecsPath(root), "recipes", "r-002-empty"), 0755); err != nil {
		t.Fatal(err)
	}
	if bad := state.UnreadableRecipes(root); len(bad) != 0 {
		t.Fatalf("want none, got %+v", bad)
	}
}
