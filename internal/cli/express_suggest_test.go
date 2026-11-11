package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

// Feature #132, step 4: bts suggests the mode.  When a full build
// recipe's confirmed scope is small, `bts recipe confirm` holds decision
// express-mode, and `bts recipe mode <id> express` carries the user's
// yes.  When an express recipe's domain model outgrows express, the
// phase change past it holds decision full-mode.

// newSuggestRecipe is a build recipe in phase with mode, made the working
// directory's project.
func newSuggestRecipe(t *testing.T, phase, mode string) (root, id string) {
	t.Helper()
	root, id = newTestRunFixture(t)
	r, err := state.LoadRecipeState(root, id)
	if err != nil {
		t.Fatal(err)
	}
	r.Type, r.Phase, r.Mode = "build", phase, mode
	if err := state.SaveRecipeState(root, r); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	return root, id
}

func writeScope(t *testing.T, root, id string, items int) {
	t.Helper()
	var b strings.Builder
	b.WriteString("# Scope\n\nStatus: DRAFT\n\n## In Scope\n\n")
	for i := 1; i <= items; i++ {
		fmt.Fprintf(&b, "%d. Item %d.\n", i, i)
	}
	b.WriteString("\n## Out of Scope\n\n- Everything else.\n")
	if err := os.WriteFile(filepath.Join(state.RecipeDir(root, id), "scope.md"), []byte(b.String()), 0644); err != nil {
		t.Fatal(err)
	}
}

func writeDomainInvariants(t *testing.T, root, id string, n int) {
	t.Helper()
	var b strings.Builder
	b.WriteString("# Domain\n\n## 2. Invariants\n\n| ID | Statement | Owner |\n|---|---|---|\n")
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "| INV-%03d | rule %d holds | relay |\n", i, i)
	}
	if err := os.WriteFile(filepath.Join(state.RecipeDir(root, id), "domain.md"), []byte(b.String()), 0644); err != nil {
		t.Fatal(err)
	}
}

func decisionStatus(t *testing.T, root, id, key string) (string, string) {
	t.Helper()
	ds, err := state.LoadDecisions(root, id)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range ds {
		if d.Key == key {
			return d.Status, d.Question
		}
	}
	return "", ""
}

func TestRecipeConfirm_ASmallScopeOnAFullBuildRecipeAsksAboutExpress(t *testing.T) {
	root, id := newSuggestRecipe(t, "scoping", "")
	writeScope(t, root, id, 2)
	if err := runQuiet(t, recipeConfirmCmd, "recipe", "confirm", id, "scope", "--answer", "yes"); err != nil {
		t.Fatal(err)
	}
	status, q := decisionStatus(t, root, id, "express-mode")
	if status != state.DecisionOpen || !strings.Contains(q, "2 In Scope") {
		t.Fatalf("a two-item scope must hold express-mode, counting the items; got %q %q", status, q)
	}

	mode := subcommand(t, "recipe", "mode")
	if err := runQuiet(t, mode, "recipe", "mode", id, "express", "--reason", "the user chose express"); err != nil {
		t.Fatal(err)
	}
	if got := recordedMode(t, root, id); got != "express" {
		t.Fatalf("the user's yes makes it express; recipe.json mode = %q", got)
	}
	if rows := modeRows(t, root, id); len(rows) != 1 || !strings.Contains(rows[0], "full → express") || !strings.Contains(rows[0], "the user chose express") {
		t.Errorf("the move must be one mode row naming it and its reason; got %q", rows)
	}
}

func TestRecipeConfirm_NoExpressQuestionWhereItDoesNotFit(t *testing.T) {
	for _, c := range []struct {
		name, typ, phase, mode string
		items                  int
	}{
		{"four items", "build", "scoping", "", 4},
		{"already express", "build", "scoping", state.ModeExpress, 2},
		{"moved to full", "build", "scoping", state.ModeFull, 2},
		{"a blueprint", "blueprint", "scoping", "", 2},
		{"past the architect step", "build", "wireframe", "", 2},
	} {
		t.Run(c.name, func(t *testing.T) {
			root, id := newSuggestRecipe(t, c.phase, c.mode)
			r, _ := state.LoadRecipeState(root, id)
			r.Type = c.typ
			_ = state.SaveRecipeState(root, r)
			writeScope(t, root, id, c.items)
			if err := runQuiet(t, recipeConfirmCmd, "recipe", "confirm", id, "scope", "--answer", "yes"); err != nil {
				t.Fatal(err)
			}
			if status, _ := decisionStatus(t, root, id, "express-mode"); status != "" {
				t.Errorf("no express question here; got one %s", status)
			}
		})
	}
}

// A scope confirmed again after research asks once: the user's answer
// stands.
func TestRecipeConfirm_TheExpressQuestionIsAskedOnce(t *testing.T) {
	root, id := newSuggestRecipe(t, "scoping", "")
	writeScope(t, root, id, 2)
	if err := runQuiet(t, recipeConfirmCmd, "recipe", "confirm", id, "scope", "--answer", "yes"); err != nil {
		t.Fatal(err)
	}
	if err := state.ResolveDecision(root, id, "express-mode", "keep the full recipe"); err != nil {
		t.Fatal(err)
	}
	writeScope(t, root, id, 3)
	if err := runQuiet(t, recipeConfirmCmd, "recipe", "confirm", id, "scope", "--answer", "yes, revised"); err != nil {
		t.Fatalf("a second confirmation must not fail on the answered question: %v", err)
	}
	if status, _ := decisionStatus(t, root, id, "express-mode"); status != state.DecisionResolved {
		t.Errorf("the answered question stays answered; got %q", status)
	}
}

func TestRecipeMode_ExpressOnlyForABuildRecipeBeforeItsArchitectStep(t *testing.T) {
	mode := subcommand(t, "recipe", "mode")

	root, id := newSuggestRecipe(t, "wireframe", "")
	if err := runQuiet(t, mode, "recipe", "mode", id, "express"); err == nil || !strings.Contains(err.Error(), "wireframe") {
		t.Errorf("a recipe past its architect step must stay full, naming its phase; got %v", err)
	}
	if got := recordedMode(t, root, id); got != "" {
		t.Errorf("a refused move changes nothing; mode = %q", got)
	}

	root, id = newSuggestRecipe(t, "scoping", "")
	r, _ := state.LoadRecipeState(root, id)
	r.Type = "blueprint"
	_ = state.SaveRecipeState(root, r)
	if err := runQuiet(t, mode, "recipe", "mode", id, "express"); err == nil || !strings.Contains(err.Error(), "build") {
		t.Errorf("express is a mode of the build recipe only; got %v", err)
	}

	_, id = newSuggestRecipe(t, "domain-model", state.ModeExpress)
	if err := runQuiet(t, mode, "recipe", "mode", id, "express"); err == nil || !strings.Contains(err.Error(), "already") {
		t.Errorf("an express recipe is already express; got %v", err)
	}
}

func TestRecipeLog_LeavingTheDomainModelAsksWhenExpressIsOutgrown(t *testing.T) {
	root, id := newSuggestRecipe(t, "domain-model", state.ModeExpress)
	writeDomainInvariants(t, root, id, 9)
	if err := runQuiet(t, recipeLogCmd, "recipe", "log", id, "--phase", "wireframe"); err != nil {
		t.Fatal(err)
	}
	status, q := decisionStatus(t, root, id, "full-mode")
	if status != state.DecisionOpen || !strings.Contains(q, "9") {
		t.Fatalf("nine introduced invariants must hold full-mode, counting them; got %q %q", status, q)
	}

	// Answered, it is not asked again at the next phase.
	if err := state.ResolveDecision(root, id, "full-mode", "stay express"); err != nil {
		t.Fatal(err)
	}
	if err := runQuiet(t, recipeLogCmd, "recipe", "log", id, "--phase", "skeleton"); err != nil {
		t.Fatal(err)
	}
	if status, _ := decisionStatus(t, root, id, "full-mode"); status != state.DecisionResolved {
		t.Errorf("the answer stands; got %q", status)
	}
}

func TestRecipeLog_EightInvariantsOrAFullRecipeAsksNothing(t *testing.T) {
	root, id := newSuggestRecipe(t, "domain-model", state.ModeExpress)
	writeDomainInvariants(t, root, id, 8)
	if err := runQuiet(t, recipeLogCmd, "recipe", "log", id, "--phase", "wireframe"); err != nil {
		t.Fatal(err)
	}
	if status, _ := decisionStatus(t, root, id, "full-mode"); status != "" {
		t.Errorf("eight introduced invariants fit express; got a question %s", status)
	}

	root, id = newSuggestRecipe(t, "domain-model", "")
	writeDomainInvariants(t, root, id, 12)
	if err := runQuiet(t, recipeLogCmd, "recipe", "log", id, "--phase", "architect"); err != nil {
		t.Fatal(err)
	}
	if status, _ := decisionStatus(t, root, id, "full-mode"); status != "" {
		t.Errorf("a full recipe has nothing to move; got a question %s", status)
	}
}
