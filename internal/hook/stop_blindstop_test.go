package hook

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

// touchVerification sets verification.md's mtime, so a test can place it
// before or after the last recorded verify round.
func touchVerification(t *testing.T, root, recipeID string, when time.Time) {
	t.Helper()
	path := filepath.Join(state.RecipeDir(root, recipeID), "verification.md")
	if err := os.Chtimes(path, when, when); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
}

// blindStop runs the Stop hook with no completion marker.
func blindStop(t *testing.T, root string) *HookOutput {
	t.Helper()
	out, err := NewStopHandler().Handle(&HookInput{CWD: root, SessionID: "s-1"})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	return out
}

// A recipe mid-loop with open findings is the NORMAL state. Stopping for
// the day must stay possible — the backstop is not "you have work left".
func TestBlindStop_OpenFindingsAlone_Allows(t *testing.T) {
	root, recipeID := setupStopRoot(t)
	logged := time.Now()
	writeVerifyLog(t, root, recipeID, []state.VerifyLogEntry{
		{Iteration: 1, Critical: 2, Major: 1, Doc: "draft.md", Status: "continue",
			Budget: 3, Timestamp: logged.UTC().Format(time.RFC3339)},
	})
	touchVerification(t, root, recipeID, logged.Add(-time.Minute))

	if out := blindStop(t, root); out.Decision == "block" {
		t.Fatalf("mid-loop open findings must not block a turn end, got: %s", out.Reason)
	}
}

// C: the convergence budget was exhausted. Ending quietly means the next
// session resumes as if the loop had not given up.
func TestBlindStop_ConvergenceFailed_Blocks(t *testing.T) {
	root, recipeID := setupStopRoot(t)
	logged := time.Now()
	writeVerifyLog(t, root, recipeID, []state.VerifyLogEntry{
		{Iteration: 4, Critical: 1, Major: 2, Doc: "draft.md", Status: "failed",
			Budget: 3, Timestamp: logged.UTC().Format(time.RFC3339)},
	})
	touchVerification(t, root, recipeID, logged.Add(-time.Minute))

	out := blindStop(t, root)
	if out.Decision != "block" {
		t.Fatal("convergence failure must not end the turn silently")
	}
	if !strings.Contains(out.Reason, "verify.max_iterations=3") {
		t.Errorf("reason must name the budget it failed under, got: %s", out.Reason)
	}
	if !strings.Contains(out.Reason, "draft.md") {
		t.Errorf("reason must name the document, got: %s", out.Reason)
	}
}

// A: a verification ran but was never recorded. verify-log is what every
// downstream gate reads, so an unlogged round is invisible work.
func TestBlindStop_UnloggedVerification_Blocks(t *testing.T) {
	root, recipeID := setupStopRoot(t)
	logged := time.Now().Add(-time.Hour)
	writeVerifyLog(t, root, recipeID, []state.VerifyLogEntry{
		{Iteration: 1, Critical: 1, Doc: "draft.md", Status: "continue",
			Budget: 3, Timestamp: logged.UTC().Format(time.RFC3339)},
	})
	// verification.md rewritten after that round was logged.
	touchVerification(t, root, recipeID, time.Now())

	out := blindStop(t, root)
	if out.Decision != "block" {
		t.Fatal("a verification newer than the last logged round must block")
	}
	if !strings.Contains(out.Reason, "--from-verification") {
		t.Errorf("reason must give the recovery command, got: %s", out.Reason)
	}
}

// verification.md with no verify-log at all is the same class: a round
// that was never recorded.
func TestBlindStop_VerificationWithNoLog_Blocks(t *testing.T) {
	root, _ := setupStopRoot(t)

	out := blindStop(t, root)
	if out.Decision != "block" {
		t.Fatal("verification.md with no verify-log must block")
	}
	if !strings.Contains(out.Reason, "verify-log.jsonl") {
		t.Errorf("reason must explain what is missing, got: %s", out.Reason)
	}
}

// B: a verified document edited after its verification (rule 3). This was
// enforced only on the DONE path before the backstop existed.
func TestBlindStop_DirtyVerifiedDoc_Blocks(t *testing.T) {
	root, recipeID := setupStopRoot(t)
	logged := time.Now()
	writeVerifyLog(t, root, recipeID, []state.VerifyLogEntry{
		{Iteration: 1, Critical: 0, Major: 0, Doc: "draft.md", Status: "converged",
			Budget: 3, Timestamp: logged.UTC().Format(time.RFC3339)},
	})
	touchVerification(t, root, recipeID, logged.Add(-time.Minute))

	recipeDir := state.RecipeDir(root, recipeID)
	if err := os.WriteFile(filepath.Join(recipeDir, "draft.md"), []byte("edited after verify"), 0644); err != nil {
		t.Fatal(err)
	}
	snapDir := state.VerifySnapshotDir(root, recipeID)
	if err := os.MkdirAll(snapDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(snapDir, "draft.md"), []byte("as verified"), 0644); err != nil {
		t.Fatal(err)
	}

	out := blindStop(t, root)
	if out.Decision != "block" {
		t.Fatal("a doc modified after verification must not pass a turn end silently")
	}
	if !strings.Contains(out.Reason, "draft.md") {
		t.Errorf("reason must name the dirty doc, got: %s", out.Reason)
	}
}

// Implement-side phases run their own gates and stop mid-task by design;
// /bts-sync legitimately rewrites final.md there.
func TestBlindStop_ImplementPhase_Allows(t *testing.T) {
	root, recipeID := setupStopRoot(t)
	recipe, err := state.LoadRecipeState(root, recipeID)
	if err != nil {
		t.Fatal(err)
	}
	recipe.Phase = "implement"
	if err := state.SaveRecipeState(root, recipe); err != nil {
		t.Fatal(err)
	}
	logged := time.Now().Add(-time.Hour)
	writeVerifyLog(t, root, recipeID, []state.VerifyLogEntry{
		{Iteration: 4, Critical: 1, Doc: "draft.md", Status: "failed",
			Budget: 3, Timestamp: logged.UTC().Format(time.RFC3339)},
	})
	touchVerification(t, root, recipeID, time.Now())

	if out := blindStop(t, root); out.Decision == "block" {
		t.Fatalf("implement phase is out of the backstop's scope, got: %s", out.Reason)
	}
}

// No bts project / no active recipe → the hook is inert.
func TestBlindStop_NoActiveRecipe_Allows(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".bts", "local"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", t.TempDir())

	if out := blindStop(t, root); out.Decision == "block" {
		t.Fatalf("no active recipe must never block, got: %s", out.Reason)
	}
}

// The block budget bounds the loop: the same complaint three times, then
// bts stands down rather than letting Claude's opaque 8-block override
// decide.
func TestStopBlockBudget_StandsDownAfterThreeIdenticalBlocks(t *testing.T) {
	root, _ := setupStopRoot(t)

	for i := 1; i <= state.DefaultStopBlockBudget-1; i++ {
		out := blindStop(t, root)
		if out.Decision != "block" {
			t.Fatalf("block %d: expected block, got allow", i)
		}
	}
	final := blindStop(t, root)
	if final.Decision == "block" {
		t.Fatalf("budget of %d exhausted — must stand down", state.DefaultStopBlockBudget)
	}
	// Standing down clears the counter, so the next episode gets a full
	// budget rather than being permanently disarmed.
	if again := blindStop(t, root); again.Decision != "block" {
		t.Fatal("budget must reset after standing down, so the gate re-arms")
	}
}

// Progress changes the reason text, which restarts the count. A model
// that is actually fixing things never exhausts the budget.
func TestStopBlockBudget_ProgressResetsCount(t *testing.T) {
	root, recipeID := setupStopRoot(t)
	logged := time.Now()

	writeVerifyLog(t, root, recipeID, []state.VerifyLogEntry{
		{Iteration: 1, Critical: 3, Major: 0, Doc: "draft.md", Status: "failed",
			Budget: 3, Timestamp: logged.UTC().Format(time.RFC3339)},
	})
	touchVerification(t, root, recipeID, logged.Add(-time.Minute))
	for i := 0; i < 2; i++ {
		if out := blindStop(t, root); out.Decision != "block" {
			t.Fatalf("round %d: expected block", i)
		}
	}

	// One critical resolved: same gate, different message.
	writeVerifyLog(t, root, recipeID, []state.VerifyLogEntry{
		{Iteration: 2, Critical: 2, Major: 0, Doc: "draft.md", Status: "failed",
			Budget: 3, Timestamp: logged.UTC().Format(time.RFC3339)},
	})
	touchVerification(t, root, recipeID, logged.Add(-time.Minute))

	for i := 0; i < 2; i++ {
		if out := blindStop(t, root); out.Decision != "block" {
			t.Fatalf("after progress, round %d: budget should have restarted, got allow", i)
		}
	}
}

// An allowed stop clears the counter — a recipe that recovers must not
// carry a partially-spent budget into its next episode.
func TestStopBlockBudget_AllowClearsCounter(t *testing.T) {
	root, recipeID := setupStopRoot(t)

	if out := blindStop(t, root); out.Decision != "block" {
		t.Fatal("expected an initial block")
	}
	if state.LoadStopBudget(root).Count != 1 {
		t.Fatalf("expected count 1, got %d", state.LoadStopBudget(root).Count)
	}

	// Resolve the condition: a logged round newer than verification.md.
	logged := time.Now()
	writeVerifyLog(t, root, recipeID, []state.VerifyLogEntry{
		{Iteration: 1, Critical: 1, Doc: "draft.md", Status: "continue",
			Budget: 3, Timestamp: logged.UTC().Format(time.RFC3339)},
	})
	touchVerification(t, root, recipeID, logged.Add(-time.Minute))

	if out := blindStop(t, root); out.Decision == "block" {
		t.Fatalf("condition resolved, expected allow, got: %s", out.Reason)
	}
	if c := state.LoadStopBudget(root).Count; c != 0 {
		t.Errorf("an allowed stop must clear the counter, got %d", c)
	}
}

// The DONE path keeps its own gates; the budget wraps it too, so a model
// that cannot satisfy a completion gate is not stuck re-emitting forever.
func TestStopBlockBudget_AppliesToDonePath(t *testing.T) {
	root, recipeID := setupStopRoot(t)
	writeVerifyLog(t, root, recipeID, []state.VerifyLogEntry{
		{Iteration: 1, Critical: 2, Major: 0, Doc: "draft.md", Status: "continue", Budget: 3},
	})

	h := NewStopHandler()
	var last *HookOutput
	for i := 0; i < state.DefaultStopBlockBudget; i++ {
		out, err := h.Handle(&HookInput{CWD: root, SessionID: "s-1", StopHookContent: "<bts>DONE</bts>"})
		if err != nil {
			t.Fatalf("handle: %v", err)
		}
		last = out
	}
	if last.Decision == "block" {
		t.Fatal("the DONE path must also be bounded by the block budget")
	}
}
