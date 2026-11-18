package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

// Trial problem #88: confirmation was a hand edit of the status line.
// `bts recipe confirm` records the user's words as a resolved decision and
// sets the line, and a revision is confirmed again under a new key.
func TestRecipeConfirm_RecordsTheWordsAndSetsTheStatus(t *testing.T) {
	root := newRecipeFixture(t, "r-c088", "scoping", 0, 0, nil)
	scope := filepath.Join(state.RecipeDir(root, "r-c088"), "scope.md")
	_ = os.WriteFile(scope, []byte("# Scope\n\n### Status: DRAFT\n\n## In Scope\n\n1. One.\n"), 0644)

	key, err := executeRecipeConfirm(root, "r-c088", "scope", "approved (translated from Korean)")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(scope)
	if state.DocStatus(string(data)) != "CONFIRMED" {
		t.Fatalf("scope.md must read CONFIRMED, got:\n%s", data)
	}
	decisions, _ := state.LoadDecisions(root, "r-c088")
	if len(decisions) != 1 || decisions[0].Key != key || decisions[0].Status != state.DecisionResolved ||
		decisions[0].Answer != "approved (translated from Korean)" {
		t.Fatalf("the user's words are the decision's answer, got %+v", decisions)
	}

	// Revised after research and confirmed again: a new key, both kept.
	_ = os.WriteFile(scope, []byte("# Scope\n\n### Status: DRAFT\n\n## In Scope\n\n1. One, narrowed.\n"), 0644)
	again, err := executeRecipeConfirm(root, "r-c088", "scope", "yes, the narrowed scope")
	if err != nil {
		t.Fatal(err)
	}
	if again != "scope-confirm-2" {
		t.Errorf("a second confirmation takes a new key, got %q", again)
	}

	// A confirmation held open while the user is away is the one resolved.
	if _, err := state.HoldDecision(root, "r-c088", &state.DecisionEvent{Key: "intent-confirm", Question: "Confirm the intent in intent.md?"}); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(state.RecipeDir(root, "r-c088"), "intent.md"), []byte("# Intent\n\nStatus: EXPLORING\n"), 0644)
	if k, err := executeRecipeConfirm(root, "r-c088", "intent", "go ahead"); err != nil || k != "intent-confirm" {
		t.Fatalf("the open intent-confirm is resolved, got %q, %v", k, err)
	}
	if open, _ := state.OpenDecisions(root, "r-c088"); len(open) != 0 {
		t.Errorf("nothing is left open, got %+v", open)
	}

	if _, err := executeRecipeConfirm(root, "r-c088", "scope", "  "); err == nil {
		t.Error("a confirmation without the user's words is refused")
	}
	if _, err := executeRecipeConfirm(root, "r-c088", "wireframe", "yes"); err == nil {
		t.Error("only intent, scope and the spec are confirmed this way")
	}
}
