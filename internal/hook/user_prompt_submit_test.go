package hook

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/review"
	"github.com/imtemp-dev/claude-bts/internal/state"
)

// A review page may hold a round the session has not read when the user
// comes back to the terminal first: the prompt carries a reminder while
// the review is under way, and nothing otherwise.
func TestUserPromptSubmit_RemindsOfAReviewPageInProgress(t *testing.T) {
	root := setupPreCompactRoot(t)
	seedRecipe(t, root, "r-001-test", "scoping")
	dir := state.RecipeDir(root, "r-001-test")
	if err := os.WriteFile(filepath.Join(dir, "intent.md"), []byte("# Intent\n\nStatus: EXPLORING\n"), 0644); err != nil {
		t.Fatal(err)
	}
	recipe, err := state.LoadRecipeState(root, "r-001-test")
	if err != nil {
		t.Fatal(err)
	}
	if got := reviewReminder(root, recipe); got != "" {
		t.Errorf("no page, no reminder; got %q", got)
	}
	if _, err := review.Attach(review.RecipeSubject(root, "r-001-test"), "https://claude.ai/artifact/x", "intent"); err != nil {
		t.Fatal(err)
	}
	got := reviewReminder(root, recipe)
	if !strings.Contains(got, "https://claude.ai/artifact/x") || !strings.Contains(got, "/bts-review-page") {
		t.Errorf("scoping with a page reminds of it, got %q", got)
	}
	recipe.Phase = "implement"
	if got := reviewReminder(root, recipe); got != "" {
		t.Errorf("past scoping with nothing open, no reminder; got %q", got)
	}

	out, err := NewUserPromptSubmitHandler().Handle(&HookInput{CWD: root, HookEventName: string(EventUserPromptSubmit), AgentID: "a-1"})
	if err != nil || out.HookSpecificOutput != nil {
		t.Errorf("a subagent's prompt gets nothing, got %+v %v", out, err)
	}
}
