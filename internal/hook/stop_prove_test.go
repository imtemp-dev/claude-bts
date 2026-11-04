package hook

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

// IMPLEMENT DONE needs the planned user journeys driven through the real
// app on the current code, or deferred, or a recorded no-surface reason
// (prove stage P2, every recipe type).
func TestImplementDone_NeedsTheAppProven(t *testing.T) {
	root, id := setupImplementRoot(t)
	_ = os.Remove(state.ProvePath(root, id))
	done := func() *HookOutput {
		t.Helper()
		out, err := NewStopHandler().Handle(&HookInput{CWD: root, LastAssistantMessage: "<bts>IMPLEMENT DONE</bts>"})
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	if out := done(); out.Decision != "block" || !strings.Contains(out.Reason, "No drive record") {
		t.Fatalf("no drive record must block, got %q %q", out.Decision, out.Reason)
	}
	_ = state.SaveProveRecord(root, id, &state.ProveRecord{Journeys: map[string]*state.Journey{"login": {}}})
	if out := done(); out.Decision != "block" || !strings.Contains(out.Reason, "login not driven") {
		t.Fatalf("an undriven journey must block, got %q", out.Reason)
	}
	_ = state.SaveProveRecord(root, id, &state.ProveRecord{Journeys: map[string]*state.Journey{
		"login": {Deferred: &state.Deferral{Reason: "needs the real IdP", Owner: "live checks"}},
	}})
	if out := done(); out.Decision == "block" {
		t.Fatalf("a deferred journey meets the gate: %s", out.Reason)
	}
}

// The fix recipe's Step 9 runs the prove stage, so FIX DONE checks the
// same records as IMPLEMENT DONE (simulation removed, decision 6).
func TestFixDone_NeedsTheProveStage(t *testing.T) {
	root, id := setupStopRoot(t)
	recipe, _ := state.LoadRecipeState(root, id)
	recipe.Type, recipe.Phase = "fix", "test"
	_ = state.SaveRecipeState(root, recipe)
	dir := state.RecipeDir(root, id)
	_ = os.WriteFile(filepath.Join(dir, "fix-spec.md"), []byte("# Fix\n"), 0644)
	_ = os.WriteFile(filepath.Join(dir, "test-results.json"), []byte(`{"recipe_id":"`+id+`","status":"pass","total":1,"passed":1}`), 0644)
	done := func() *HookOutput {
		t.Helper()
		out, err := NewStopHandler().Handle(&HookInput{CWD: root, LastAssistantMessage: "<bts>FIX DONE</bts>"})
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	if out := done(); out.Decision != "block" || !strings.Contains(out.Reason, "No interrogate run") {
		t.Fatalf("FIX DONE without interrogate must block, got %q %q", out.Decision, out.Reason)
	}
	_ = state.SaveInterrogateRun(root, id, &state.InterrogateRun{PreparedAt: "t", VerifiedAt: "t"})
	if out := done(); out.Decision != "block" || !strings.Contains(out.Reason, "No drive record") {
		t.Fatalf("FIX DONE without a drive record must block, got %q %q", out.Decision, out.Reason)
	}
	_ = state.SaveProveRecord(root, id, &state.ProveRecord{Journeys: map[string]*state.Journey{
		"reported-bug": {Deferred: &state.Deferral{Reason: "needs the customer's tenant", Owner: "support"}},
	}})
	if out := done(); out.Decision == "block" {
		t.Fatalf("a clean prove stage completes the fix: %s", out.Reason)
	}
}
