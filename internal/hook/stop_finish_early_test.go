package hook

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

// Trial problem #125: on cool-crow (r-005, 11-10 22:23Z-22:32Z) the user
// said to finish a recipe whose implementation was complete and whose
// tests were green, without the interrogate round or the drive.  No
// implement gate could be overridden, so the session jumped the phase by
// hand, met each gate in turn, and cancelled a finished recipe.  The
// implement gates now take a recorded override, pinned to the code as it
// is ("(code)"), and each block names how to record one.
func TestImplementDone_TheUserCanFinishPastImplementGatesOnRecord(t *testing.T) {
	root, id := setupImplementRoot(t)
	writeTaskFile(t, root, "package a\n")
	recordImplHash(t, root, id)
	dir := state.RecipeDir(root, id)
	_ = os.Remove(state.InterrogatePath(root, id))
	_ = os.Remove(state.ProvePath(root, id))
	_ = os.Remove(filepath.Join(dir, "review.md"))
	_ = os.Remove(filepath.Join(dir, "deviation.md"))
	done := func() *HookOutput {
		t.Helper()
		out, err := NewStopHandler().Handle(&HookInput{CWD: root, LastAssistantMessage: "<bts>IMPLEMENT DONE</bts>"})
		if err != nil {
			t.Fatal(err)
		}
		return out
	}

	out := done()
	if out.Decision != "block" || !strings.Contains(out.Reason, "--gate interrogate_clean --no-findings") {
		t.Fatalf("the block must name how to record the user's decision to pass it, got %q", out.Reason)
	}

	grant := func() {
		t.Helper()
		hash, err := state.RecipeImplHash(root, id)
		if err != nil {
			t.Fatal(err)
		}
		for _, gate := range []string{"interrogate_clean", "app_proven", "review_before_done", "sync_after_test"} {
			if err := state.AppendOverride(root, id, &state.OverrideRecord{
				Gate: gate, Doc: "(code)", DocHash: hash,
				Reason: "the user decided to finish: implementation complete, tests green",
			}); err != nil {
				t.Fatal(err)
			}
		}
	}
	grant()

	// The overrides were about the code as it was.
	writeTaskFile(t, root, "package a\n\n// later\n")
	recordImplHash(t, root, id)
	if out := done(); out.Decision != "block" || !strings.Contains(out.Reason, "interrogate") {
		t.Fatalf("a change to the code makes the overrides stale, got %q", out.Reason)
	}

	grant()
	if out := done(); out.Decision == "block" {
		t.Fatalf("every unmet gate is overridden on the current code, so IMPLEMENT DONE passes; got %q", out.Reason)
	}
}
