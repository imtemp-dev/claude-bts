package hook

import (
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

// A clean round on ANOTHER document must not satisfy draft.md's
// completion gate. Before per-document verify state the hook read the
// last log entry regardless of which document it described, so verifying
// wireframe.md right after a failing draft round would let DONE through
// on counts that belonged to a different file.
func TestStopSpecDone_WireframeRoundCannotSatisfyDraftGate(t *testing.T) {
	root, recipeID := setupStopRoot(t)
	writeVerifyLog(t, root, recipeID, []state.VerifyLogEntry{
		{Iteration: 1, Doc: "draft.md", Critical: 2, Major: 1, FullPass: true, Status: "continue"},
		{Iteration: 2, Doc: "wireframe.md", Critical: 0, Major: 0, FullPass: true, Status: "converged"},
	})

	h := NewStopHandler()
	out, err := h.Handle(&HookInput{CWD: root, StopHookContent: "<bts>DONE</bts>"})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if out.Decision != "block" {
		t.Fatalf("expected block — draft.md still has 2 critical, got decision=%q", out.Decision)
	}
	if !strings.Contains(out.Reason, "draft.md") {
		t.Errorf("reason should name the document that failed, got %q", out.Reason)
	}
}

// A scoped delta round verified only the changed sections plus their
// reference closure. Finalizing on one would ship a spec whose untouched
// sections were never re-checked against the edits.
func TestStopSpecDone_BlocksOnDeltaOnlyVerification(t *testing.T) {
	root, recipeID := setupStopRoot(t)
	writeVerifyLog(t, root, recipeID, []state.VerifyLogEntry{
		{Iteration: 1, Doc: "draft.md", Critical: 0, Major: 0, FullPass: false, Status: "converged"},
	})

	h := NewStopHandler()
	out, err := h.Handle(&HookInput{CWD: root, StopHookContent: "<bts>DONE</bts>"})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if out.Decision != "block" {
		t.Fatalf("expected block on a delta-only pass, got decision=%q", out.Decision)
	}
	if !strings.Contains(out.Reason, "delta pass") {
		t.Errorf("reason should explain the delta/full distinction, got %q", out.Reason)
	}
}

func TestStopSpecDone_AllowsCleanFullPass(t *testing.T) {
	root, recipeID := setupStopRoot(t)
	writeVerifyLog(t, root, recipeID, []state.VerifyLogEntry{
		{Iteration: 1, Doc: "draft.md", Critical: 0, Major: 0, FullPass: true, Status: "converged"},
	})

	h := NewStopHandler()
	out, err := h.Handle(&HookInput{CWD: root, StopHookContent: "<bts>DONE</bts>"})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if out.Decision == "block" {
		t.Fatalf("clean full pass should complete, blocked with: %s", out.Reason)
	}
}

// Legacy logs record no doc and no full_pass. Enforcing either against
// them would block every recipe created before v0.10, so both gates
// stand down when the log is undifferentiated.
func TestStopSpecDone_LegacyLogUnaffected(t *testing.T) {
	root, recipeID := setupStopRoot(t)
	writeVerifyLog(t, root, recipeID, []state.VerifyLogEntry{
		{Iteration: 1, Critical: 0, Major: 0, Status: "converged"},
	})

	h := NewStopHandler()
	out, err := h.Handle(&HookInput{CWD: root, StopHookContent: "<bts>DONE</bts>"})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if out.Decision == "block" {
		t.Fatalf("legacy converged log should still complete, blocked with: %s", out.Reason)
	}
}

// A round the convergence budget marked failed must not be finalized on,
// even though its counts could look acceptable in isolation.
func TestStopSpecDone_BlocksOnFailedStatus(t *testing.T) {
	root, recipeID := setupStopRoot(t)
	writeVerifyLog(t, root, recipeID, []state.VerifyLogEntry{
		{Iteration: 4, Doc: "draft.md", Critical: 0, Major: 0, MinorDeferred: 1, FullPass: true, Status: "failed"},
	})

	h := NewStopHandler()
	out, err := h.Handle(&HookInput{CWD: root, StopHookContent: "<bts>DONE</bts>"})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if out.Decision != "block" {
		t.Fatalf("expected block on a failed round, got decision=%q", out.Decision)
	}
}
