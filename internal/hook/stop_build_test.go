package hook

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/engine"
	"github.com/imtemp-dev/claude-bts/internal/state"
)

// setupBuildRoot is a build recipe ready to finalize except for what each
// test leaves out: domain.md with two invariants, a wireframe naming one
// code file, the skeleton file, and final.md.
func setupBuildRoot(t *testing.T) (root, id string) {
	t.Helper()
	root, id = setupStopRoot(t)
	recipe, _ := state.LoadRecipeState(root, id)
	recipe.Type = "build"
	recipe.Phase = "falsify"
	if err := state.SaveRecipeState(root, recipe); err != nil {
		t.Fatal(err)
	}
	dir := state.RecipeDir(root, id)
	write := func(p, s string) {
		t.Helper()
		_ = os.MkdirAll(filepath.Dir(p), 0755)
		if err := os.WriteFile(p, []byte(s), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(dir, "domain.md"), "## 2. Invariants\n\n| ID | Statement | Owner |\n|---|---|---|\n"+
		"| INV-001 | one issue per PR | relay |\n| INV-002 | never post with the user token | relay |\n")
	write(filepath.Join(dir, "wireframe.md"), "## File Structure\n\n| # | File | Action | Depends On | Responsibility |\n"+
		"|---|---|---|---|---|\n| 1 | `src/relay.py` | create | — | the relay |\n")
	write(filepath.Join(root, "src", "relay.py"), "def handle(e): raise NotImplementedError\n")
	write(filepath.Join(dir, "final.md"), "# Build spec\n")
	return root, id
}

func recordEvidence(t *testing.T, root, id string, skeletonExit int, redInvs ...string) {
	t.Helper()
	hash, err := engine.CodeHash(root, id)
	if err != nil || hash == "" {
		t.Fatalf("code hash: %q, %v", hash, err)
	}
	ev := &state.BuildEvidence{
		Skeleton:   &state.CommandRun{Command: "mypy src", ExitCode: skeletonExit, CodeHash: hash},
		Falsifiers: map[string]*state.Falsifier{},
	}
	for _, inv := range redInvs {
		ev.Falsifiers[inv] = &state.Falsifier{Command: "pytest -k " + inv, Red: &state.CommandRun{Command: "pytest", ExitCode: 1, CodeHash: hash}}
	}
	if err := state.SaveBuildEvidence(root, id, ev); err != nil {
		t.Fatal(err)
	}
}

func buildDone(t *testing.T, root string) *HookOutput {
	t.Helper()
	out, err := NewStopHandler().Handle(&HookInput{CWD: root, StopHookContent: "<bts>DONE</bts>"})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	return out
}

func TestBuildSpecDone_BlocksWithoutSkeletonBuild(t *testing.T) {
	root, _ := setupBuildRoot(t)
	if out := buildDone(t, root); out.Decision != "block" || !strings.Contains(out.Reason, "No skeleton build") {
		t.Fatalf("expected skeleton_builds block, got %q %q", out.Decision, out.Reason)
	}
}

func TestBuildSpecDone_BlocksAnInvariantWithoutARedFalsifier(t *testing.T) {
	root, id := setupBuildRoot(t)
	recordEvidence(t, root, id, 0, "INV-001")
	if out := buildDone(t, root); out.Decision != "block" || !strings.Contains(out.Reason, "INV-002") {
		t.Fatalf("expected falsifiers_red block naming INV-002, got %q %q", out.Decision, out.Reason)
	}
}

func TestBuildSpecDone_BlocksWhenTheSkeletonChangedAfterItsBuild(t *testing.T) {
	root, id := setupBuildRoot(t)
	recordEvidence(t, root, id, 0, "INV-001", "INV-002")
	_ = os.WriteFile(filepath.Join(root, "src", "relay.py"), []byte("def handle(e, extra): raise NotImplementedError\n"), 0644)
	if out := buildDone(t, root); out.Decision != "block" || !strings.Contains(out.Reason, "changed after its last build") {
		t.Fatalf("expected a stale skeleton build to block, got %q %q", out.Decision, out.Reason)
	}
}

func TestBuildSpecDone_NeedsOneCleanReviewOfFinal(t *testing.T) {
	root, id := setupBuildRoot(t)
	recordEvidence(t, root, id, 0, "INV-001", "INV-002")

	if out := buildDone(t, root); out.Decision != "block" || !strings.Contains(out.Reason, "No review of final.md") {
		t.Fatalf("expected build_review_clean to ask for a review, got %q %q", out.Decision, out.Reason)
	}

	writeVerifyLog(t, root, id, []state.VerifyLogEntry{
		{Iteration: 1, Doc: "final.md", FullPass: true, Critical: 0, Major: 1, Status: "continue"},
	})
	if out := buildDone(t, root); out.Decision != "block" || !strings.Contains(out.Reason, "1 major") {
		t.Fatalf("expected a major in the review to block, got %q %q", out.Decision, out.Reason)
	}

	// Minors do not hold the build recipe: they are fixed or declared, and
	// nothing waits for a replicated clean pass.
	writeVerifyLog(t, root, id, []state.VerifyLogEntry{
		{Iteration: 2, Doc: "final.md", FullPass: true, Critical: 0, Major: 0, MinorResolvable: 3, Status: "continue"},
	})
	out := buildDone(t, root)
	if out.Decision == "block" {
		t.Fatalf("one clean review with the evidence in place must finalize: %s", out.Reason)
	}
	if r, _ := state.LoadRecipeState(root, id); r.Phase != "finalize" {
		t.Fatalf("phase must be finalize, got %q", r.Phase)
	}
}

func TestBuildSpecDone_DoesNotUseTheBlueprintLoop(t *testing.T) {
	root, id := setupBuildRoot(t)
	recordEvidence(t, root, id, 0, "INV-001", "INV-002")
	writeVerifyLog(t, root, id, []state.VerifyLogEntry{
		{Iteration: 1, Doc: "final.md", FullPass: true, Dimensions: []string{"verify"}, Status: "continue"},
	})
	// No audit, no simulate, no replicated pass, no draft.md: the blueprint
	// gates would all block this; the build recipe does not ask for them.
	if out := buildDone(t, root); out.Decision == "block" {
		t.Fatalf("a build recipe must not be held to the blueprint loop: %s", out.Reason)
	}
}

func TestImplementDone_BuildRecipeNeedsGreenFalsifiers(t *testing.T) {
	root, id := setupImplementRoot(t)
	recipe, _ := state.LoadRecipeState(root, id)
	recipe.Type = "build"
	_ = state.SaveRecipeState(root, recipe)
	dir := state.RecipeDir(root, id)
	_ = os.WriteFile(filepath.Join(dir, "domain.md"), []byte("## 2. Invariants\n\n| ID | Statement | Owner |\n|---|---|---|\n| INV-001 | x | a |\n"), 0644)
	_ = os.MkdirAll(filepath.Join(root, "src"), 0755)
	_ = os.WriteFile(filepath.Join(root, "src", "a.go"), []byte("package a\n"), 0644)

	hash, _ := engine.CodeHash(root, id)
	ev := &state.BuildEvidence{Falsifiers: map[string]*state.Falsifier{
		"INV-001": {Command: "go test -run Inv001", Red: &state.CommandRun{ExitCode: 1}, Green: &state.CommandRun{ExitCode: 1, CodeHash: hash}},
	}}
	_ = state.SaveBuildEvidence(root, id, ev)

	out, err := NewStopHandler().Handle(&HookInput{CWD: root, StopHookContent: "<bts>IMPLEMENT DONE</bts>"})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if out.Decision != "block" || !strings.Contains(out.Reason, "INV-001 not passing") {
		t.Fatalf("expected falsifiers_green block, got %q %q", out.Decision, out.Reason)
	}

	ev.Falsifiers["INV-001"].Green = &state.CommandRun{ExitCode: 0, CodeHash: hash}
	_ = state.SaveBuildEvidence(root, id, ev)
	out, _ = NewStopHandler().Handle(&HookInput{CWD: root, StopHookContent: "<bts>IMPLEMENT DONE</bts>"})
	if out.Decision == "block" {
		t.Fatalf("green falsifiers on the current code must pass: %s", out.Reason)
	}
}
