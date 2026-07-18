package hook

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

func setupStopRoot(t *testing.T) (root, recipeID string) {
	t.Helper()
	root = t.TempDir()
	recipeID = "r-001-test"
	recipeDir := filepath.Join(root, ".bts", "specs", "recipes", recipeID)
	if err := os.MkdirAll(recipeDir, 0755); err != nil {
		t.Fatalf("mkdir recipe: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".bts", "local"), 0755); err != nil {
		t.Fatalf("mkdir local: %v", err)
	}
	configDir := filepath.Join(root, ".bts", "config")
	_ = os.MkdirAll(configDir, 0755)
	_ = os.WriteFile(filepath.Join(configDir, ".template-version"), []byte("test"), 0644)
	t.Setenv("HOME", t.TempDir())

	recipe := &state.RecipeState{
		ID:    recipeID,
		Type:  "blueprint",
		Phase: "verify",
	}
	if err := state.SaveRecipeState(root, recipe); err != nil {
		t.Fatalf("save recipe: %v", err)
	}
	// verification.md must exist for the gate to proceed
	verifyPath := filepath.Join(recipeDir, "verification.md")
	if err := os.WriteFile(verifyPath, []byte("stub"), 0644); err != nil {
		t.Fatalf("write verification.md: %v", err)
	}
	// Default changelog satisfying the Sprint 10 blueprint gates:
	// simulate ran at least once and sync-check passed after the last
	// draft modification. Tests exercising those gates overwrite this
	// via resetChangelog.
	resetChangelog(t, root, recipeID,
		state.ChangelogEntry{Action: "draft", Output: "draft.md"},
		state.ChangelogEntry{Action: "simulate", Output: "simulations/001-scenarios.md"},
		state.ChangelogEntry{Action: "sync-check", Result: "pass"},
	)
	return root, recipeID
}

// resetChangelog replaces the recipe's changelog.jsonl with the given
// entries in order.
func resetChangelog(t *testing.T, root, recipeID string, entries ...state.ChangelogEntry) {
	t.Helper()
	path := filepath.Join(state.RecipeDir(root, recipeID), "changelog.jsonl")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create changelog: %v", err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for i := range entries {
		if entries[i].Timestamp == "" {
			entries[i].Timestamp = "2026-01-01T00:00:00Z"
		}
		if err := enc.Encode(&entries[i]); err != nil {
			t.Fatalf("encode changelog: %v", err)
		}
	}
}

func writeVerifyLog(t *testing.T, root, recipeID string, entries []state.VerifyLogEntry) {
	t.Helper()
	path := filepath.Join(state.RecipeDir(root, recipeID), "verify-log.jsonl")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create log: %v", err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for i := range entries {
		if err := enc.Encode(&entries[i]); err != nil {
			t.Fatalf("encode: %v", err)
		}
	}
}

// TestStopSpecDone_BlocksOnResolvableMinor — Phase 1.3 core guarantee:
// a converged-looking entry (critical=0, major=0) with an outstanding
// [resolvable] minor must still block completion.
func TestStopSpecDone_BlocksOnResolvableMinor(t *testing.T) {
	root, recipeID := setupStopRoot(t)
	writeVerifyLog(t, root, recipeID, []state.VerifyLogEntry{
		{Iteration: 1, Critical: 0, Major: 0, MinorResolvable: 1, MinorDeferred: 0, Status: "continue"},
	})

	h := NewStopHandler()
	out, err := h.Handle(&HookInput{CWD: root, StopHookContent: "<bts>DONE</bts>"})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if out.Decision != "block" {
		t.Fatalf("expected block, got decision=%q", out.Decision)
	}
	if !strings.Contains(out.Reason, "minor [resolvable]") {
		t.Errorf("reason should cite minor [resolvable], got %q", out.Reason)
	}
}

// TestStopSpecDone_AllowsOnlyDeferredMinors — [deferred] minors are
// runtime watch-items and do not block completion, PROVIDED they are
// declared as Known Uncertainties entries (rule 3b).
func TestStopSpecDone_AllowsOnlyDeferredMinors(t *testing.T) {
	root, recipeID := setupStopRoot(t)
	writeVerifyLog(t, root, recipeID, []state.VerifyLogEntry{
		{Iteration: 1, Critical: 0, Major: 0, MinorResolvable: 0, MinorDeferred: 2, Status: "converged"},
	})
	draft := `# Draft

## Known Uncertainties

### U-001: animation timing on low-end devices
Why-deferred: needs a physical device frame-time capture.

### U-002: keyboard dismissal window
Why-deferred: observable only in the iOS simulator.
`
	draftPath := filepath.Join(state.RecipeDir(root, recipeID), "draft.md")
	if err := os.WriteFile(draftPath, []byte(draft), 0644); err != nil {
		t.Fatalf("write draft.md: %v", err)
	}

	h := NewStopHandler()
	out, err := h.Handle(&HookInput{CWD: root, StopHookContent: "<bts>DONE</bts>"})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if out.Decision == "block" {
		t.Fatalf("expected pass-through, got block: %s", out.Reason)
	}
}

// Sprint 10 gate 2b: deferred minors WITHOUT a Known Uncertainties
// section must block — otherwise the watch-list never reaches
// /bts-implement.
func TestStopSpecDone_BlocksWhenDeferredMinorsUndeclared(t *testing.T) {
	root, recipeID := setupStopRoot(t)
	writeVerifyLog(t, root, recipeID, []state.VerifyLogEntry{
		{Iteration: 1, Critical: 0, Major: 0, MinorDeferred: 1, Status: "converged"},
	})
	draftPath := filepath.Join(state.RecipeDir(root, recipeID), "draft.md")
	if err := os.WriteFile(draftPath, []byte("# Draft without uncertainties\n"), 0644); err != nil {
		t.Fatalf("write draft.md: %v", err)
	}

	h := NewStopHandler()
	out, err := h.Handle(&HookInput{CWD: root, StopHookContent: "<bts>DONE</bts>"})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if out.Decision != "block" {
		t.Fatalf("expected block, got decision=%q", out.Decision)
	}
	if !strings.Contains(out.Reason, "Known Uncertainties") {
		t.Errorf("reason should cite Known Uncertainties, got %q", out.Reason)
	}
}

// Sprint 10 gate 2c: blueprint recipes with no simulate action in the
// changelog must block (rule 5 is now machine-enforced).
func TestStopSpecDone_BlocksWithoutSimulate(t *testing.T) {
	root, recipeID := setupStopRoot(t)
	writeVerifyLog(t, root, recipeID, []state.VerifyLogEntry{
		{Iteration: 1, Critical: 0, Major: 0, Status: "converged"},
	})
	resetChangelog(t, root, recipeID,
		state.ChangelogEntry{Action: "draft", Output: "draft.md"},
		state.ChangelogEntry{Action: "sync-check", Result: "pass"},
	)

	h := NewStopHandler()
	out, err := h.Handle(&HookInput{CWD: root, StopHookContent: "<bts>DONE</bts>"})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if out.Decision != "block" {
		t.Fatalf("expected block, got decision=%q", out.Decision)
	}
	if !strings.Contains(out.Reason, "simulate") {
		t.Errorf("reason should cite simulate, got %q", out.Reason)
	}
}

// Sprint 10 gate 2c: a sync-check pass that PREDATES the last draft
// modification is stale — rule 8 requires re-running it.
func TestStopSpecDone_BlocksWhenSyncCheckStale(t *testing.T) {
	root, recipeID := setupStopRoot(t)
	writeVerifyLog(t, root, recipeID, []state.VerifyLogEntry{
		{Iteration: 1, Critical: 0, Major: 0, Status: "converged"},
	})
	resetChangelog(t, root, recipeID,
		state.ChangelogEntry{Action: "simulate", Output: "simulations/001-scenarios.md"},
		state.ChangelogEntry{Action: "sync-check", Result: "pass"},
		state.ChangelogEntry{Action: "improve", Output: "draft.md"},
	)

	h := NewStopHandler()
	out, err := h.Handle(&HookInput{CWD: root, StopHookContent: "<bts>DONE</bts>"})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if out.Decision != "block" {
		t.Fatalf("expected block, got decision=%q", out.Decision)
	}
	if !strings.Contains(out.Reason, "sync-check") {
		t.Errorf("reason should cite sync-check, got %q", out.Reason)
	}
}

// Non-blueprint spec recipes (design/analyze) are exempt from the
// simulate / sync-check changelog gates — their skills do not run
// those steps.
func TestStopSpecDone_NonBlueprintSkipsChangelogGates(t *testing.T) {
	root, recipeID := setupStopRoot(t)
	recipe, err := state.LoadRecipeState(root, recipeID)
	if err != nil {
		t.Fatalf("load recipe: %v", err)
	}
	recipe.Type = "design"
	if err := state.SaveRecipeState(root, recipe); err != nil {
		t.Fatalf("save recipe: %v", err)
	}
	writeVerifyLog(t, root, recipeID, []state.VerifyLogEntry{
		{Iteration: 1, Critical: 0, Major: 0, Status: "converged"},
	})
	// Empty the changelog entirely — gates must not even look at it.
	resetChangelog(t, root, recipeID)

	h := NewStopHandler()
	out, err := h.Handle(&HookInput{CWD: root, StopHookContent: "<bts>DONE</bts>"})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if out.Decision == "block" {
		t.Fatalf("expected pass-through for design recipe, got block: %s", out.Reason)
	}
}

// Sprint 9 P21: handleSpecDone must set level=3.0 AND iteration=last
// verify entry's iteration on a successful <bts>DONE</bts>. Prevents
// the r-018 pattern where recipe.json drifts to {level:0, iteration:0}
// even after converged verify-log.
func TestStopSpecDone_UpdatesLevelIteration(t *testing.T) {
	root, recipeID := setupStopRoot(t)
	writeVerifyLog(t, root, recipeID, []state.VerifyLogEntry{
		{Iteration: 1, Critical: 1, Major: 0, Status: "continue"},
		{Iteration: 4, Critical: 0, Major: 0, Status: "converged"},
	})

	h := NewStopHandler()
	out, err := h.Handle(&HookInput{CWD: root, StopHookContent: "<bts>DONE</bts>"})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if out.Decision == "block" {
		t.Fatalf("expected pass-through, got block: %s", out.Reason)
	}

	after, err := state.LoadRecipeState(root, recipeID)
	if err != nil {
		t.Fatalf("load after: %v", err)
	}
	if after.Phase != "finalize" {
		t.Errorf("phase want finalize, got %s", after.Phase)
	}
	if after.Level != 3.0 {
		t.Errorf("level want 3.0, got %v", after.Level)
	}
	if after.Iteration != 4 {
		t.Errorf("iteration want 4 (from last verify entry), got %d", after.Iteration)
	}
}

// TestStopSpecDone_BlockedByOpenBTSBlock — even when the verify-log is
// fully converged, an unresolved [!BTS-BLOCK] callout in any recipe doc
// must block finalize. Reviewer-flagged spec issues take priority over
// the auto-verifier's all-clear.
func TestStopSpecDone_BlockedByOpenBTSBlock(t *testing.T) {
	root, recipeID := setupStopRoot(t)
	writeVerifyLog(t, root, recipeID, []state.VerifyLogEntry{
		{Iteration: 1, Critical: 0, Major: 0, Status: "converged"},
	})
	// Plant an unresolved BTS-BLOCK callout in draft.md.
	draftPath := filepath.Join(state.RecipeDir(root, recipeID), "draft.md")
	if err := os.WriteFile(draftPath, []byte("# Draft\n\n> [!BTS-BLOCK]\n> session storage decision still pending\n"), 0644); err != nil {
		t.Fatalf("write draft.md: %v", err)
	}

	h := NewStopHandler()
	out, err := h.Handle(&HookInput{CWD: root, StopHookContent: "<bts>DONE</bts>"})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if out.Decision != "block" {
		t.Fatalf("expected block, got decision=%q reason=%q", out.Decision, out.Reason)
	}
	if !strings.Contains(out.Reason, "BTS-BLOCK") {
		t.Errorf("reason should cite BTS-BLOCK, got %q", out.Reason)
	}
}

// TestStopSpecDone_NonBlockingCommentsDoNotBlock — plain BTS-COMMENT and
// BTS-Q callouts are non-blocking; finalize must proceed despite them.
func TestStopSpecDone_NonBlockingCommentsDoNotBlock(t *testing.T) {
	root, recipeID := setupStopRoot(t)
	writeVerifyLog(t, root, recipeID, []state.VerifyLogEntry{
		{Iteration: 1, Critical: 0, Major: 0, Status: "converged"},
	})
	draftPath := filepath.Join(state.RecipeDir(root, recipeID), "draft.md")
	if err := os.WriteFile(draftPath, []byte("# Draft\n\n> [!BTS-COMMENT]\n> nice to have\n\n> [!BTS-Q]\n> any TTL?\n"), 0644); err != nil {
		t.Fatalf("write draft.md: %v", err)
	}

	h := NewStopHandler()
	out, err := h.Handle(&HookInput{CWD: root, StopHookContent: "<bts>DONE</bts>"})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if out.Decision == "block" {
		t.Fatalf("expected pass-through, got block: %s", out.Reason)
	}
}

// TestStopSpecDone_LegacyMinorFieldBlocks — legacy log entries predate the
// resolvable/deferred split. EffectiveResolvable() treats legacy Minor>0
// as resolvable (conservative). Ensures existing recipes do not silently
// slip past the new gate.
func TestStopSpecDone_LegacyMinorFieldBlocks(t *testing.T) {
	root, recipeID := setupStopRoot(t)
	writeVerifyLog(t, root, recipeID, []state.VerifyLogEntry{
		{Iteration: 1, Critical: 0, Major: 0, Minor: 3, Status: "continue"},
	})

	h := NewStopHandler()
	out, err := h.Handle(&HookInput{CWD: root, StopHookContent: "<bts>DONE</bts>"})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if out.Decision != "block" {
		t.Fatalf("legacy Minor=3 must block; got decision=%q", out.Decision)
	}
}

// setupImplementRoot prepares a recipe directory with the four artifacts
// that handleImplementDone checks unconditionally. Tests then add or omit
// final.md Known Uncertainties to exercise Phase 8's new gate.
func setupImplementRoot(t *testing.T) (root, recipeID string) {
	t.Helper()
	root = t.TempDir()
	recipeID = "r-001-impl"
	recipeDir := filepath.Join(root, ".bts", "specs", "recipes", recipeID)
	if err := os.MkdirAll(recipeDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".bts", "local"), 0755); err != nil {
		t.Fatalf("mkdir local: %v", err)
	}
	configDir := filepath.Join(root, ".bts", "config")
	_ = os.MkdirAll(configDir, 0755)
	_ = os.WriteFile(filepath.Join(configDir, ".template-version"), []byte("test"), 0644)
	t.Setenv("HOME", t.TempDir())

	recipe := &state.RecipeState{ID: recipeID, Type: "blueprint", Phase: "implement"}
	if err := state.SaveRecipeState(root, recipe); err != nil {
		t.Fatalf("save recipe: %v", err)
	}

	// tasks.json — one done task keeps handleImplementDone past the
	// blocked/pending gate.
	tasks := &state.TaskState{RecipeID: recipeID, Tasks: []state.Task{
		{ID: "t-001", File: "src/a.go", Action: "create", Status: "done", Description: "x"},
	}}
	data, _ := json.MarshalIndent(tasks, "", "  ")
	_ = os.WriteFile(filepath.Join(recipeDir, "tasks.json"), data, 0644)

	// test-results.json — pass.
	tr := &state.TestResults{RecipeID: recipeID, Status: "pass", Total: 1, Passed: 1}
	data, _ = json.MarshalIndent(tr, "", "  ")
	_ = os.WriteFile(filepath.Join(recipeDir, "test-results.json"), data, 0644)

	// review.md + deviation.md — presence only (content not inspected).
	_ = os.WriteFile(filepath.Join(recipeDir, "review.md"), []byte("stub"), 0644)
	_ = os.WriteFile(filepath.Join(recipeDir, "deviation.md"), []byte("stub"), 0644)
	return root, recipeID
}

// Phase 8 gate: missing resolution marker on a Known Uncertainty entry
// must block IMPLEMENT DONE and cite the offending U-NNN id.
func TestStopImplementDone_BlocksOnUnresolvedUncertainty(t *testing.T) {
	root, recipeID := setupImplementRoot(t)
	final := `# Spec

## Known Uncertainties

### U-001: no resolution marker here
Why-deferred: needs a physical device to verify.
`
	_ = os.WriteFile(filepath.Join(state.RecipeDir(root, recipeID), "final.md"), []byte(final), 0644)

	h := NewStopHandler()
	out, err := h.Handle(&HookInput{CWD: root, StopHookContent: "<bts>IMPLEMENT DONE</bts>"})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if out.Decision != "block" {
		t.Fatalf("expected block, got decision=%q", out.Decision)
	}
	if !strings.Contains(out.Reason, "U-001") {
		t.Errorf("reason should cite U-001, got %q", out.Reason)
	}
}

// Resolved uncertainty → gate passes.
func TestStopImplementDone_AllowsResolvedUncertainty(t *testing.T) {
	root, recipeID := setupImplementRoot(t)
	final := `## Known Uncertainties

### U-001: example
Resolved: verified via integration test T-042.
`
	_ = os.WriteFile(filepath.Join(state.RecipeDir(root, recipeID), "final.md"), []byte(final), 0644)

	h := NewStopHandler()
	out, err := h.Handle(&HookInput{CWD: root, StopHookContent: "<bts>IMPLEMENT DONE</bts>"})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if out.Decision == "block" {
		t.Fatalf("resolved uncertainty should not block: %s", out.Reason)
	}
}

// No Known Uncertainties section → gate passes (tracking is optional).
func TestStopImplementDone_NoUncertaintySectionPasses(t *testing.T) {
	root, recipeID := setupImplementRoot(t)
	_ = os.WriteFile(filepath.Join(state.RecipeDir(root, recipeID), "final.md"), []byte("# Spec\n\nNo uncertainties here.\n"), 0644)

	h := NewStopHandler()
	out, err := h.Handle(&HookInput{CWD: root, StopHookContent: "<bts>IMPLEMENT DONE</bts>"})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if out.Decision == "block" {
		t.Fatalf("absent uncertainty section should not block: %s", out.Reason)
	}
}

// --- Rule 3 dirty-doc gate (verify snapshots) ---

func writeSnapshotAndDoc(t *testing.T, root, recipeID, base, snapContent, docContent string) {
	t.Helper()
	docPath := filepath.Join(state.RecipeDir(root, recipeID), base)
	if err := os.WriteFile(docPath, []byte(snapContent), 0644); err != nil {
		t.Fatalf("write doc: %v", err)
	}
	if err := state.SaveVerifySnapshot(root, recipeID, docPath); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if docContent != snapContent {
		if err := os.WriteFile(docPath, []byte(docContent), 0644); err != nil {
			t.Fatalf("rewrite doc: %v", err)
		}
	}
}

// TestStopSpecDone_BlocksDirtyDoc — a doc modified after its last
// verification must block DONE even when the verify-log is converged.
func TestStopSpecDone_BlocksDirtyDoc(t *testing.T) {
	root, recipeID := setupStopRoot(t)
	writeVerifyLog(t, root, recipeID, []state.VerifyLogEntry{
		{Iteration: 1, Critical: 0, Major: 0, Status: "converged"},
	})
	writeSnapshotAndDoc(t, root, recipeID, "draft.md", "verified content\n", "edited AFTER verify\n")

	h := NewStopHandler()
	out, err := h.Handle(&HookInput{CWD: root, StopHookContent: "<bts>DONE</bts>"})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if out.Decision != "block" {
		t.Fatalf("expected block, got %q (reason=%q)", out.Decision, out.Reason)
	}
	if !strings.Contains(out.Reason, "draft.md") || !strings.Contains(out.Reason, "modified after last verification") {
		t.Errorf("reason should name the dirty doc and rule: %q", out.Reason)
	}
}

// TestStopSpecDone_AllowsCleanSnapshot — matching snapshot passes.
func TestStopSpecDone_AllowsCleanSnapshot(t *testing.T) {
	root, recipeID := setupStopRoot(t)
	writeVerifyLog(t, root, recipeID, []state.VerifyLogEntry{
		{Iteration: 1, Critical: 0, Major: 0, Status: "converged"},
	})
	writeSnapshotAndDoc(t, root, recipeID, "draft.md", "verified content\n", "verified content\n")

	h := NewStopHandler()
	out, err := h.Handle(&HookInput{CWD: root, StopHookContent: "<bts>DONE</bts>"})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if out.Decision == "block" {
		t.Fatalf("clean snapshot must not block: %q", out.Reason)
	}
}

// TestStopSpecDone_LegacyNoSnapshotsAllows — recipes that never used
// --doc (pre-v0.8.0) must not be blocked by the new gate.
func TestStopSpecDone_LegacyNoSnapshotsAllows(t *testing.T) {
	root, recipeID := setupStopRoot(t)
	writeVerifyLog(t, root, recipeID, []state.VerifyLogEntry{
		{Iteration: 1, Critical: 0, Major: 0, Status: "converged"},
	})
	// No snapshots at all — gate must pass through.
	h := NewStopHandler()
	out, err := h.Handle(&HookInput{CWD: root, StopHookContent: "<bts>DONE</bts>"})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if out.Decision == "block" {
		t.Fatalf("legacy recipe must not block: %q", out.Reason)
	}
}

// TestStopFixDone_BlocksDirtyFixSpec — same rule-3 gate on FIX DONE.
func TestStopFixDone_BlocksDirtyFixSpec(t *testing.T) {
	root, recipeID := setupStopRoot(t)
	recipe, _ := state.LoadRecipeState(root, recipeID)
	recipe.Type = "fix"
	if err := state.SaveRecipeState(root, recipe); err != nil {
		t.Fatalf("save recipe: %v", err)
	}
	writeSnapshotAndDoc(t, root, recipeID, "fix-spec.md", "verified fix\n", "tweaked after verify\n")
	// Passing tests so only the dirty gate can block.
	tr := `{"recipe_id":"` + recipeID + `","status":"pass","total":3,"passed":3,"failed":0,"skipped":0}`
	if err := os.WriteFile(filepath.Join(state.RecipeDir(root, recipeID), "test-results.json"), []byte(tr), 0644); err != nil {
		t.Fatalf("write test results: %v", err)
	}

	h := NewStopHandler()
	out, err := h.Handle(&HookInput{CWD: root, StopHookContent: "<bts>FIX DONE</bts>"})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if out.Decision != "block" || !strings.Contains(out.Reason, "fix-spec.md") {
		t.Fatalf("expected fix-spec dirty block, got decision=%q reason=%q", out.Decision, out.Reason)
	}
}
