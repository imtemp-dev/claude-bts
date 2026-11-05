package hook

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/engine"
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

// A fix recipe writes no tasks.json, so its test run, interrogate recheck
// and drive are fingerprinted by the whole git tree. Before that fallback
// the fingerprint was "" and FIX DONE took all three as current after any
// later edit. Each must go stale on an edit and come back when re-run on
// the code as it is now; the .bts/ writes in between must not count.
func TestFixDone_RunsGoStaleWhenTheTreeChanges(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root, id := setupStopRoot(t)
	if out, err := exec.Command("git", "-C", root, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	src := filepath.Join(root, "src", "login.py")
	_ = os.MkdirAll(filepath.Dir(src), 0755)
	_ = os.WriteFile(src, []byte("ok = given == stored\n"), 0644)
	recipe, _ := state.LoadRecipeState(root, id)
	recipe.Type, recipe.Phase = "fix", "test"
	_ = state.SaveRecipeState(root, recipe)
	dir := state.RecipeDir(root, id)
	_ = os.WriteFile(filepath.Join(dir, "fix-spec.md"), []byte("# Fix\n"), 0644)

	hashNow := func() string {
		t.Helper()
		h, err := engine.CodeHash(root, id)
		if err != nil || h == "" {
			t.Fatalf("a fix recipe in a git repository must have a fingerprint, got %q %v", h, err)
		}
		return h
	}
	recordTests := func() {
		h, _ := state.RecipeImplHash(root, id)
		_ = os.WriteFile(filepath.Join(dir, "test-results.json"), []byte(fmt.Sprintf(
			`{"recipe_id":%q,"status":"pass","total":1,"passed":1,"recorded_by":"bts","impl_hash":%q}`, id, h)), 0644)
	}
	recordRecheck := func() {
		_ = state.SaveInterrogateRun(root, id, &state.InterrogateRun{PreparedAt: "t", VerifiedAt: "t",
			Findings: []*state.InterrogateFinding{{ID: "F-001", Status: state.InterrogateFixed,
				Recheck: &state.CommandRun{Command: "pytest -k F001", CodeHash: hashNow()}}}})
	}
	recordDrive := func() {
		_ = state.SaveProveRecord(root, id, &state.ProveRecord{Journeys: map[string]*state.Journey{
			"reported-bug": {Run: &state.CommandRun{Command: "drive.sh", CodeHash: hashNow()}}}})
	}
	done := func() *HookOutput {
		t.Helper()
		out, err := NewStopHandler().Handle(&HookInput{CWD: root, LastAssistantMessage: "<bts>FIX DONE</bts>"})
		if err != nil {
			t.Fatal(err)
		}
		return out
	}

	recordTests()
	recordRecheck()
	recordDrive()
	_ = os.WriteFile(src, []byte("ok = bcrypt.check(given, stored)\n"), 0644)

	if out := done(); out.Decision != "block" || !strings.Contains(out.Reason, "Tests passed on different code") {
		t.Fatalf("an edit after the test run must block, got %q %q", out.Decision, out.Reason)
	}
	recordTests()
	if out := done(); out.Decision != "block" || !strings.Contains(out.Reason, "the code changed after F-001 was rechecked") {
		t.Fatalf("an edit after the recheck must block, got %q %q", out.Decision, out.Reason)
	}
	recordRecheck()
	if out := done(); out.Decision != "block" || !strings.Contains(out.Reason, "the code changed after reported-bug was driven") {
		t.Fatalf("an edit after the drive must block, got %q %q", out.Decision, out.Reason)
	}
	recordDrive()
	if out := done(); out.Decision == "block" {
		t.Fatalf("every run on the current code completes the fix: %s", out.Reason)
	}
}
