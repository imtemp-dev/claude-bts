package hook

import (
	"strings"
	"testing"
	"time"

	"github.com/imtemp-dev/claude-jig/internal/state"
)

func holdDecision(t *testing.T, root, recipeID, key, question string) {
	t.Helper()
	if _, err := state.HoldDecision(root, recipeID, &state.DecisionEvent{
		Key: key, Question: question, Doc: "draft.md",
	}); err != nil {
		t.Fatalf("hold: %v", err)
	}
}

// A spec must not finalize while a question that shaped it is unanswered
// — that would bake in an answer nobody gave.
func TestSpecDone_OpenDecisionBlocksCompletion(t *testing.T) {
	root, recipeID := setupStopRoot(t)
	writeVerifyLog(t, root, recipeID, []state.VerifyLogEntry{
		{Iteration: 1, Critical: 0, Major: 0, Doc: "draft.md", FullPass: true,
			Status: "converged", Budget: 3},
	})
	holdDecision(t, root, recipeID, "token-storage", "keychain or httpOnly cookie?")

	out, err := NewStopHandler().Handle(&HookInput{
		CWD: root, SessionID: "s-1", StopHookContent: "<jig>DONE</jig>",
	})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if out.Decision != "block" {
		t.Fatal("a clean verify round must not finalize past an open decision")
	}
	if !strings.Contains(out.Reason, "token-storage") {
		t.Errorf("reason must name the open decision, got: %s", out.Reason)
	}
}

// Resolving the decision removes the block — the gate is about the answer
// existing, not about the question having been asked.
func TestSpecDone_ResolvedDecisionDoesNotBlock(t *testing.T) {
	root, recipeID := setupStopRoot(t)
	writeVerifyLog(t, root, recipeID, []state.VerifyLogEntry{
		{Iteration: 1, Critical: 0, Major: 0, Doc: "draft.md", FullPass: true,
			Status: "converged", Budget: 3},
	})
	holdDecision(t, root, recipeID, "token-storage", "keychain or httpOnly cookie?")
	if err := state.ResolveDecision(root, recipeID, "token-storage", "httpOnly cookie"); err != nil {
		t.Fatal(err)
	}

	out, err := NewStopHandler().Handle(&HookInput{
		CWD: root, SessionID: "s-1", StopHookContent: "<jig>DONE</jig>",
	})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if out.Decision == "block" && strings.Contains(out.Reason, "token-storage") {
		t.Fatalf("a resolved decision must not block, got: %s", out.Reason)
	}
}

// Waiting on a person is a legitimate way for a turn to end — blocking
// would make it impossible to hand the question over.
func TestBlindStop_OpenDecisionAllowsTurnEnd(t *testing.T) {
	root, recipeID := setupStopRoot(t)
	logged := time.Now()
	writeVerifyLog(t, root, recipeID, []state.VerifyLogEntry{
		{Iteration: 4, Critical: 1, Doc: "draft.md", Status: "failed",
			Budget: 3, Timestamp: logged.UTC().Format(time.RFC3339)},
	})
	recordVerification(t, root, recipeID)
	holdDecision(t, root, recipeID, "scope-call", "drop the offline mode or extend the deadline?")

	// Without the hold this state blocks (convergence failed). With the
	// question durably recorded, the handoff has happened and the turn
	// may end.
	if out := blindStop(t, root); out.Decision == "block" {
		t.Fatalf("a recorded decision is the handoff; the turn must be allowed to end, got: %s", out.Reason)
	}
}

// Before the ledger existed this state was indistinguishable from
// "still working". Session start must say so on resume.
func TestSessionStart_SurfacesOpenDecisions(t *testing.T) {
	root, recipeID := setupStopRoot(t)
	holdDecision(t, root, recipeID, "scope-call", "drop offline mode or extend the deadline?")

	out, err := NewSessionStartHandler().Handle(&HookInput{CWD: root, Source: "resume"})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if out.HookSpecificOutput == nil {
		t.Fatal("expected session-start context")
	}
	ctx := out.HookSpecificOutput.AdditionalContext
	if !strings.Contains(ctx, "BLOCKED") || !strings.Contains(ctx, "scope-call") {
		t.Errorf("resume context must surface the open decision, got:\n%s", ctx)
	}
	if !strings.Contains(ctx, "jig recipe decision resolve") {
		t.Errorf("context must carry the command that clears it, got:\n%s", ctx)
	}
}

// No decisions → no notice. The surface must not add noise to the normal
// case.
func TestSessionStart_NoDecisionsNoNotice(t *testing.T) {
	root, _ := setupStopRoot(t)

	out, err := NewSessionStartHandler().Handle(&HookInput{CWD: root, Source: "resume"})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if out.HookSpecificOutput != nil &&
		strings.Contains(out.HookSpecificOutput.AdditionalContext, "BLOCKED") {
		t.Error("no open decisions must produce no blocked notice")
	}
}
