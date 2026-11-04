package hook

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/engine"
	"github.com/imtemp-dev/claude-bts/internal/metrics"
	"github.com/imtemp-dev/claude-bts/internal/state"
)

const (
	ownerSID    = "aaaaaaaa-1111-4111-8111-111111111111"
	observerSID = "bbbbbbbb-2222-4222-8222-222222222222"
)

func ranBash(root, sid, cmd, stdout string) *HookInput {
	return &HookInput{
		CWD: root, SessionID: sid, HookEventName: "PostToolUse", ToolName: "Bash",
		ToolInput:    map[string]interface{}{"command": cmd},
		ToolResponse: map[string]interface{}{"stdout": stdout, "stderr": "", "interrupted": false},
	}
}

func owner(t *testing.T, root, id string) string {
	t.Helper()
	r, err := state.LoadRecipeState(root, id)
	if err != nil {
		t.Fatal(err)
	}
	return r.OwnerSession
}

func TestOwnership_CreateBindsAndClaimRebinds(t *testing.T) {
	root, id := setupStopRoot(t)

	if _, err := NewPostToolUseHandler().Handle(ranBash(root, ownerSID, "bts recipe create --type build --topic x", id+"\n")); err != nil {
		t.Fatal(err)
	}
	if got := owner(t, root, id); got != ownerSID {
		t.Fatalf("the session that created the recipe must own it, owner=%q", got)
	}

	// Another session's create does not take an owned recipe.
	_, _ = NewPostToolUseHandler().Handle(ranBash(root, observerSID, "bts recipe create --topic y", id+"\n"))
	if got := owner(t, root, id); got != ownerSID {
		t.Fatalf("an owned recipe must not move on another create, owner=%q", got)
	}

	// An explicit claim does, and is logged.
	_, _ = NewPostToolUseHandler().Handle(ranBash(root, observerSID, "bts recipe claim "+id, "claimed\n"))
	if got := owner(t, root, id); got != observerSID {
		t.Fatalf("a claim must rebind the recipe, owner=%q", got)
	}
	errs, err := engine.ValidateRecipeDir(state.RecipeDir(root, id))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range errs {
		if e.File == "changelog.jsonl" && strings.Contains(e.Message, "invalid action") {
			t.Errorf("bts validate rejects the claim entry: %s", e.Message)
		}
	}
}

func setOwner(t *testing.T, root, id, sid string) {
	t.Helper()
	r, _ := state.LoadRecipeState(root, id)
	r.OwnerSession = sid
	if err := state.SaveRecipeState(root, r); err != nil {
		t.Fatal(err)
	}
}

func TestOwnership_AnotherSessionGetsNoGates(t *testing.T) {
	root, id := setupImplementRoot(t)
	setOwner(t, root, id, ownerSID)

	// Its completion marker is refused, with the owner named.
	out, err := NewStopHandler().Handle(&HookInput{CWD: root, SessionID: observerSID, LastAssistantMessage: "<bts>IMPLEMENT DONE</bts>"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Decision != "block" || !strings.Contains(out.Reason, "bts recipe claim") || !strings.Contains(out.Reason, ownerSID[:8]) {
		t.Fatalf("another session's marker must be refused with the owner named, got %q %q", out.Decision, out.Reason)
	}
	if r, _ := state.LoadRecipeState(root, id); r.Phase != "implement" {
		t.Fatalf("nothing may complete, phase is %q", r.Phase)
	}

	// The owner's marker still completes.
	out, _ = NewStopHandler().Handle(&HookInput{CWD: root, SessionID: ownerSID, LastAssistantMessage: "<bts>IMPLEMENT DONE</bts>"})
	if out.Decision == "block" {
		t.Fatalf("the owner's marker must complete: %s", out.Reason)
	}
}

func TestOwnership_AnotherSessionIsNotStoppedOrWarnedInASpecPhase(t *testing.T) {
	root, id := setupStopRoot(t) // blueprint, phase verify
	setOwner(t, root, id, ownerSID)
	finalPath := filepath.Join(state.RecipeDir(root, id), "draft.md")
	_ = os.WriteFile(finalPath, []byte("# d\n"), 0644)
	hash, _, _ := state.FileContentHash(finalPath)
	writeVerifyLog(t, root, id, []state.VerifyLogEntry{{Iteration: 1, Doc: "draft.md", FullPass: true, DocHash: hash, Status: "continue"}})
	_ = os.WriteFile(finalPath, []byte("# d\nedited\n"), 0644) // Rule B would block the owner

	if out, _ := NewStopHandler().Handle(&HookInput{CWD: root, SessionID: observerSID, LastAssistantMessage: "Reviewed the PR."}); out.Decision == "block" {
		t.Fatalf("another session must not be held by the recipe's rules: %s", out.Reason)
	}
	if out, _ := NewStopHandler().Handle(&HookInput{CWD: root, SessionID: ownerSID, LastAssistantMessage: "Edited the draft."}); out.Decision != "block" {
		t.Fatal("the owner is still held by Rule B")
	}

	out, err := NewPreToolUseHandler().Handle(&HookInput{CWD: root, SessionID: observerSID, ToolName: "Edit",
		ToolInput: map[string]interface{}{"file_path": filepath.Join(root, "src", "x.go")}})
	if err != nil {
		t.Fatal(err)
	}
	if out.HookSpecificOutput != nil && out.HookSpecificOutput.AdditionalContext != "" {
		t.Fatalf("another session must get no spec-phase warning: %q", out.HookSpecificOutput.AdditionalContext)
	}
}

func TestOwnership_AnotherSessionIsMeteredAsAnObserver(t *testing.T) {
	root, id := setupStopRoot(t)
	setOwner(t, root, id, ownerSID)

	_, _ = NewPostToolUseHandler().Handle(ranBash(root, ownerSID, "ls", ""))
	_, _ = NewPostToolUseHandler().Handle(ranBash(root, observerSID, "ls", ""))
	_, _ = NewSubagentStartHandler().Handle(&HookInput{CWD: root, SessionID: observerSID, AgentID: "a1"})

	project, _ := metrics.ReadAllEvents(root)
	var ownerEv, observerEv int
	for _, e := range project {
		switch e.SessionID {
		case ownerSID:
			if e.RecipeID != id || e.Observer {
				t.Errorf("an owner event belongs to the recipe: %+v", e)
			}
			ownerEv++
		case observerSID:
			if e.RecipeID != "" || !e.Observer {
				t.Errorf("an observer event is flagged and kept out of the recipe: %+v", e)
			}
			observerEv++
		}
	}
	if ownerEv != 1 || observerEv != 2 {
		t.Fatalf("project metrics keep every event: owner=%d observer=%d", ownerEv, observerEv)
	}
	recipeEvents, _ := metrics.ReadRecipeEvents(root, id)
	for _, e := range recipeEvents {
		if e.SessionID == observerSID {
			t.Fatalf("the recipe's metrics must not hold observer events: %+v", e)
		}
	}
}

func TestOwnership_SessionStartTellsAnotherSessionItIsNotBound(t *testing.T) {
	root, id := setupStopRoot(t)
	setOwner(t, root, id, ownerSID)
	out, err := NewSessionStartHandler().Handle(&HookInput{CWD: root, SessionID: observerSID, Source: "startup"})
	if err != nil {
		t.Fatal(err)
	}
	if out.HookSpecificOutput == nil || !strings.Contains(out.HookSpecificOutput.AdditionalContext, "is worked by session "+ownerSID[:8]) {
		t.Fatalf("another session must be told the recipe is not its own, got %+v", out.HookSpecificOutput)
	}
}
