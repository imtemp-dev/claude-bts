package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/state"
	"github.com/spf13/cobra"
)

// Feature #132, step 1: an express mode for a small build recipe.  Seven
// build recipes spent 70-120 active minutes on fixed process whatever the
// size of the change (the architect's runners and debate, the review loop
// on final.md, two interrogate adversaries).  The mode is recorded in
// recipe.json, where the hooks, gates and skills read it:
// `recipe create --type build --express` makes an express recipe, and
// `recipe mode <id> full` moves it to the full recipe, one way, with a
// changelog row.

// newModeProject is an empty bts project, made the working directory.
func newModeProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, d := range []string{filepath.Join(".bts", "specs", "recipes"), filepath.Join(".bts", "local")} {
		if err := os.MkdirAll(filepath.Join(root, d), 0755); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(root)
	return root
}

// onlyRecipe returns the id of the project's one recipe.
func onlyRecipe(t *testing.T, root string) string {
	t.Helper()
	rs, err := state.ListRecipes(root)
	if err != nil || len(rs) != 1 {
		t.Fatalf("want one recipe, got %d (%v)", len(rs), err)
	}
	return rs[0].ID
}

// recordedMode is recipe.json's "mode" as the file has it.
func recordedMode(t *testing.T, root, id string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(state.RecipeDir(root, id), "recipe.json"))
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	m, _ := raw["mode"].(string)
	return m
}

// modeRows are the results of the recipe's "mode" changelog rows.
func modeRows(t *testing.T, root, id string) []string {
	t.Helper()
	entries, err := state.ReadChangelog(root, id)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		if e.Action == "mode" {
			out = append(out, e.Result)
		}
	}
	return out
}

// subcommand finds a command by its path, so its flags can be reset
// between runs.
func subcommand(t *testing.T, path ...string) *cobra.Command {
	t.Helper()
	cmd, _, err := rootCmd.Find(path)
	if err != nil {
		t.Fatal(err)
	}
	return cmd
}

func createExpress(t *testing.T, root string) string {
	t.Helper()
	if err := runQuiet(t, recipeCreateCmd, "recipe", "create", "--type", "build", "--topic", "add a dry-run flag", "--express"); err != nil {
		t.Fatal(err)
	}
	return onlyRecipe(t, root)
}

func TestRecipeCreate_ExpressMakesAnExpressBuildRecipe(t *testing.T) {
	root := newModeProject(t)
	id := createExpress(t, root)
	if got := recordedMode(t, root, id); got != "express" {
		t.Fatalf("recipe.json mode = %q, want express", got)
	}
	if rows := modeRows(t, root, id); len(rows) != 1 || !strings.Contains(rows[0], "express") {
		t.Errorf("the choice must be in the changelog as one mode row; got %q", rows)
	}
	out, err := runOutput(t, recipeStatusCmd, "recipe", "status")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Mode:") || !strings.Contains(out, "express") {
		t.Errorf("recipe status must show the mode; got:\n%s", out)
	}
}

func TestRecipeCreate_ExpressIsForABuildRecipeOnly(t *testing.T) {
	root := newModeProject(t)
	for _, typ := range []string{"fix", "blueprint", "design"} {
		err := runQuiet(t, recipeCreateCmd, "recipe", "create", "--type", typ, "--topic", "a change", "--express")
		if err == nil || !strings.Contains(err.Error(), "--type build") {
			t.Errorf("--express with --type %s must be refused, naming --type build; got %v", typ, err)
		}
	}
	if rs, _ := state.ListRecipes(root); len(rs) != 0 {
		t.Errorf("a refused create must leave no recipe; got %d", len(rs))
	}
}

func TestRecipeCreate_WithoutExpressRecordsNoMode(t *testing.T) {
	root := newModeProject(t)
	if err := runQuiet(t, recipeCreateCmd, "recipe", "create", "--type", "build", "--topic", "a change"); err != nil {
		t.Fatal(err)
	}
	id := onlyRecipe(t, root)
	if got := recordedMode(t, root, id); got != "" {
		t.Errorf("a recipe made without --express runs the full recipe and records no mode; got %q", got)
	}
	if rows := modeRows(t, root, id); len(rows) != 0 {
		t.Errorf("no mode was chosen, so no mode row; got %q", rows)
	}
}

func TestRecipeMode_FullMovesAnExpressRecipeOneWayAndRecordsIt(t *testing.T) {
	root := newModeProject(t)
	id := createExpress(t, root)
	mode := subcommand(t, "recipe", "mode")

	if err := runQuiet(t, mode, "recipe", "mode", id, "full", "--reason", "nine invariants"); err != nil {
		t.Fatal(err)
	}
	if got := recordedMode(t, root, id); got != "full" {
		t.Fatalf("recipe.json mode = %q, want full", got)
	}
	rows := modeRows(t, root, id)
	if len(rows) != 2 || !strings.Contains(rows[1], "express → full") || !strings.Contains(rows[1], "nine invariants") {
		t.Fatalf("the move must be one mode row naming it and its reason; got %q", rows)
	}

	if err := runQuiet(t, mode, "recipe", "mode", id, "express"); err == nil || !strings.Contains(err.Error(), "one way") {
		t.Errorf("an upgraded recipe must not go back to express; got %v", err)
	}
	if err := runQuiet(t, mode, "recipe", "mode", id, "full"); err == nil || !strings.Contains(err.Error(), "full recipe") {
		t.Errorf("a recipe already on the full recipe must be told so; got %v", err)
	}
	if rows := modeRows(t, root, id); len(rows) != 2 {
		t.Errorf("a refused move must write no row; got %q", rows)
	}
}

func TestRecipeMode_RefusesWhatIsNotAnOpenExpressRecipe(t *testing.T) {
	root := newModeProject(t)
	if err := runQuiet(t, recipeCreateCmd, "recipe", "create", "--type", "build", "--topic", "a change"); err != nil {
		t.Fatal(err)
	}
	full := onlyRecipe(t, root)
	mode := subcommand(t, "recipe", "mode")

	if err := runQuiet(t, mode, "recipe", "mode", full, "full"); err == nil || !strings.Contains(err.Error(), "full recipe") {
		t.Errorf("a recipe made without --express already runs the full recipe; got %v", err)
	}
	if err := runQuiet(t, mode, "recipe", "mode", "r-404", "full"); err == nil || !strings.Contains(err.Error(), "r-404") {
		t.Errorf("an unknown recipe must be named; got %v", err)
	}
	if err := runQuiet(t, mode, "recipe", "mode", full, "fast"); err == nil || !strings.Contains(err.Error(), "fast") {
		t.Errorf("an unknown mode must be named; got %v", err)
	}

	express := createExpress2(t, root)
	r, err := state.LoadRecipeState(root, express)
	if err != nil {
		t.Fatal(err)
	}
	r.Phase = "cancelled"
	if err := state.SaveRecipeState(root, r); err != nil {
		t.Fatal(err)
	}
	if err := runQuiet(t, mode, "recipe", "mode", express, "full"); err == nil || !strings.Contains(err.Error(), "cancelled") {
		t.Errorf("a cancelled recipe has nothing left to move; got %v", err)
	}
}

// createExpress2 makes a second express recipe in a project that has one.
func createExpress2(t *testing.T, root string) string {
	t.Helper()
	before, _ := state.ListRecipes(root)
	if err := runQuiet(t, recipeCreateCmd, "recipe", "create", "--type", "build", "--topic", "another change", "--express"); err != nil {
		t.Fatal(err)
	}
	after, _ := state.ListRecipes(root)
	seen := map[string]bool{}
	for _, r := range before {
		seen[r.ID] = true
	}
	for _, r := range after {
		if !seen[r.ID] {
			return r.ID
		}
	}
	t.Fatal("no new recipe")
	return ""
}
