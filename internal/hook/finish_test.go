package hook

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

// Trial problem #155: `bts recipe finish` runs the Stop hook's completion
// gate without ending the session.

func lastPhaseEntry(t *testing.T, root, id string) state.ChangelogEntry {
	t.Helper()
	entries, err := state.ReadChangelog(root, id)
	if err != nil {
		t.Fatal(err)
	}
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].Action == "phase" {
			return entries[i]
		}
	}
	t.Fatal("no phase entry")
	return state.ChangelogEntry{}
}

func TestFinish_CompletesThroughTheGate(t *testing.T) {
	root, id := setupImplementRoot(t)
	setOwner(t, root, id, ownerSID)
	res, err := Finish(root, id, ownerSID)
	if err != nil {
		t.Fatal(err)
	}
	if res.Blocked != "" || res.Marker != "<bts>IMPLEMENT DONE</bts>" || res.From != "implement" || res.To != "complete" {
		t.Fatalf("the gate passes and completes the recipe: %+v", res)
	}
	if e := lastPhaseEntry(t, root, id); e.Via != state.PhaseViaGate || !strings.HasSuffix(e.Result, "→ complete") {
		t.Fatalf("the phase change is recorded as via the gate: %+v", e)
	}
}

// The refusal is the Stop hook's, word for word, and changes nothing.
func TestFinish_RefusesWithTheHooksReason(t *testing.T) {
	root, id := setupImplementRoot(t)
	setOwner(t, root, id, ownerSID)
	_ = os.Remove(filepath.Join(state.RecipeDir(root, id), "review.md"))

	res, err := Finish(root, id, ownerSID)
	if err != nil {
		t.Fatal(err)
	}
	if res.Blocked == "" || res.To != "implement" {
		t.Fatalf("a failing gate refuses and leaves the phase: %+v", res)
	}
	out, err := NewStopHandler().Handle(&HookInput{CWD: root, SessionID: ownerSID, LastAssistantMessage: "<bts>IMPLEMENT DONE</bts>"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Decision != "block" || out.Reason != res.Blocked {
		t.Fatalf("finish and the hook must give one reason:\nfinish: %s\nhook:   %s", res.Blocked, out.Reason)
	}
}

func TestFinish_AnotherSessionCannotComplete(t *testing.T) {
	root, id := setupImplementRoot(t)
	setOwner(t, root, id, ownerSID)
	res, err := Finish(root, id, observerSID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Blocked, "does not own") || res.To != "implement" {
		t.Fatalf("another session's finish is refused, as its marker is: %+v", res)
	}
}

func TestFinish_OnlyTheActiveRecipe(t *testing.T) {
	root, _ := setupImplementRoot(t)
	if _, err := Finish(root, "r-999-other", ownerSID); err == nil || !strings.Contains(err.Error(), "not the active recipe") {
		t.Fatalf("finish names the active recipe: %v", err)
	}
}

// A build recipe's spec loop finishes with DONE, through the build spec
// gate, and the session goes on to implement it.
func TestFinish_BuildSpecDone(t *testing.T) {
	root, id := setupBuildRoot(t)
	recordEvidence(t, root, id, 0, "INV-001", "INV-002")
	writeVerifyLog(t, root, id, []state.VerifyLogEntry{
		{Iteration: 1, Doc: "final.md", FullPass: true, Critical: 0, Major: 0, Status: "continue"},
	})
	res, err := Finish(root, id, "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Marker != "<bts>DONE</bts>" || res.Blocked != "" || res.To != "finalize" {
		t.Fatalf("the build spec finalizes: %+v", res)
	}
}

func TestFinishMarker(t *testing.T) {
	for _, c := range []struct {
		typ, phase, want string
	}{
		{"fix", "implement", "<bts>FIX DONE</bts>"},
		{"fix", "research", "<bts>FIX DONE</bts>"},
		{"build", "test", "<bts>IMPLEMENT DONE</bts>"},
		{"blueprint", "status", "<bts>IMPLEMENT DONE</bts>"},
		{"build", "falsify", "<bts>DONE</bts>"},
		{"blueprint", "verify", "<bts>DONE</bts>"},
	} {
		if got := FinishMarker(&state.RecipeState{Type: c.typ, Phase: c.phase}); got != c.want {
			t.Errorf("%s in %s: %s, want %s", c.typ, c.phase, got, c.want)
		}
	}
}
