package hook

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

// Trial problem #101: the hint was keyed to the phase, so a build recipe
// heard "Run /bts-falsify" through steps 9 to 13 (four times on
// swift-eagle after its falsifiers were recorded), and in the test phase
// the hint skipped Step 5.2 and sent the agent to drive the app before any
// adversary had reported.

func hintFor(t *testing.T, root, id string) string {
	t.Helper()
	r, err := state.LoadRecipeState(root, id)
	if err != nil {
		t.Fatal(err)
	}
	return nextStepHint(root, r)
}

func TestNextStepHint_BuildTailNamesTheFirstUnmetGate(t *testing.T) {
	root, id := setupBuildRoot(t)
	if h := hintFor(t, root, id); !strings.Contains(h, "/bts-falsify") {
		t.Fatalf("no falsifier recorded yet: falsify is next, got %q", h)
	}

	recordEvidence(t, root, id, 0, "INV-001", "INV-002")
	if h := hintFor(t, root, id); strings.Contains(h, "/bts-falsify") || !strings.Contains(h, "build_review_clean") {
		t.Fatalf("falsifiers recorded, final.md unreviewed: the review is next, got %q", h)
	}

	_ = os.Remove(filepath.Join(state.RecipeDir(root, id), "final.md"))
	if h := hintFor(t, root, id); !strings.Contains(h, "Write final.md") {
		t.Fatalf("no final.md: writing it is next, got %q", h)
	}

	_ = os.WriteFile(filepath.Join(state.RecipeDir(root, id), "final.md"), []byte("# Build spec\n"), 0644)
	writeVerifyLog(t, root, id, []state.VerifyLogEntry{
		{Iteration: 1, Doc: "final.md", FullPass: true, Status: "continue"},
	})
	if h := hintFor(t, root, id); !strings.Contains(h, "<bts>DONE</bts>") {
		t.Fatalf("every gate holds: DONE is next, got %q", h)
	}
}

func TestNextStepHint_TestPhaseFollowsTheResumeTable(t *testing.T) {
	root, id := setupBuildRoot(t)
	recordEvidence(t, root, id, 0, "INV-001", "INV-002")
	r, _ := state.LoadRecipeState(root, id)
	r.Phase = "test"
	_ = state.SaveRecipeState(root, r)
	dir := state.RecipeDir(root, id)
	_ = os.WriteFile(filepath.Join(dir, "test-results.json"), []byte(`{"status":"pass","recorded_by":"bts"}`), 0644)

	if h := hintFor(t, root, id); !strings.Contains(h, "Step 5.2") {
		t.Fatalf("no mutation recorded: Step 5.2 comes before the interrogate, got %q", h)
	}

	ev, _ := state.LoadBuildEvidence(root, id)
	for _, inv := range []string{"INV-001", "INV-002"} {
		ev.Falsifiers[inv].Mutation = &state.MutationRun{Files: []string{"src/relay.py"}, ExitCode: 1, Matched: true}
	}
	_ = state.SaveBuildEvidence(root, id, ev)

	_ = os.WriteFile(filepath.Join(dir, "interrogate.json"), []byte(`{"round":1,"prepared_at":"2026-11-06T03:15:08Z"}`), 0644)
	if h := hintFor(t, root, id); !strings.Contains(h, "finish the interrogate") || strings.Contains(h, "drive the app") {
		t.Fatalf("prepared, not verified: the interrogate is not done, got %q", h)
	}

	_ = os.WriteFile(filepath.Join(dir, "interrogate.json"), []byte(`{"round":1,"prepared_at":"2026-11-06T03:15:08Z","verified_at":"2026-11-06T03:40:54Z"}`), 0644)
	if h := hintFor(t, root, id); !strings.Contains(h, "drive the app") {
		t.Fatalf("interrogate verified: driving the app is next, got %q", h)
	}
}
