package hook

import (
	"os"
	"path/filepath"
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

// The completion contract: clean, over the whole document, with every
// dimension, reproduced on one unchanged revision. See
// engine/completion_evidence.go for the measurements behind each clause.
func TestStopSpecDone_AllowsConfirmedCleanFullPass(t *testing.T) {
	root, recipeID := setupStopRoot(t)
	all := []string{"audit", "simulate", "verify"}
	writeVerifyLog(t, root, recipeID, []state.VerifyLogEntry{
		{Iteration: 1, Doc: "draft.md", Critical: 0, Major: 0, FullPass: true,
			Dimensions: all, DocHash: "sha256:aaa", VerificationHash: "sha256:v1", Status: "converged"},
		{Iteration: 2, Doc: "draft.md", Critical: 0, Major: 0, FullPass: true,
			Dimensions: all, DocHash: "sha256:aaa", VerificationHash: "sha256:v2", Status: "converged"},
	})

	h := NewStopHandler()
	out, err := h.Handle(&HookInput{CWD: root, StopHookContent: "<bts>DONE</bts>"})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if out.Decision == "block" {
		t.Fatalf("two confirming full-dimension passes should complete, blocked with: %s", out.Reason)
	}
}

// Two rows are not two readings. Re-running `bts recipe log` against the
// same verification.md produced a second confirming round without the
// document ever being read again, which made the replication gate
// satisfiable by re-typing one command.
func TestStopSpecDone_BlocksWhenBothRoundsCiteTheSameVerification(t *testing.T) {
	root, recipeID := setupStopRoot(t)
	all := []string{"audit", "simulate", "verify"}
	writeVerifyLog(t, root, recipeID, []state.VerifyLogEntry{
		{Iteration: 1, Doc: "draft.md", Critical: 0, Major: 0, FullPass: true,
			Dimensions: all, DocHash: "sha256:aaa", VerificationHash: "sha256:v1", Status: "converged"},
		{Iteration: 2, Doc: "draft.md", Critical: 0, Major: 0, FullPass: true,
			Dimensions: all, DocHash: "sha256:aaa", VerificationHash: "sha256:v1", Status: "converged"},
	})

	h := NewStopHandler()
	out, err := h.Handle(&HookInput{CWD: root, StopHookContent: "<bts>DONE</bts>"})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if out.Decision != "block" {
		t.Fatalf("one measurement recorded twice must not finalize, got decision=%q", out.Decision)
	}
	if !strings.Contains(out.Reason, "recorded twice") {
		t.Errorf("reason should say the two rounds are the same measurement, got %q", out.Reason)
	}
}

// The structural gates fire on rounds with NO findings, so the footer
// that tells the operator how to record an override must not ask for a
// finding ID that cannot exist.
func TestStopSpecDone_OverrideFooterAsksForTheFlagGrantAccepts(t *testing.T) {
	root, recipeID := setupStopRoot(t)
	writeVerifyLog(t, root, recipeID, []state.VerifyLogEntry{
		{Iteration: 1, Doc: "draft.md", Critical: 0, Major: 0, FullPass: true,
			Dimensions: []string{"audit", "simulate", "verify"},
			DocHash:    "sha256:aaa", VerificationHash: "sha256:v1", Status: "converged"},
	})

	h := NewStopHandler()
	out, err := h.Handle(&HookInput{CWD: root, StopHookContent: "<bts>DONE</bts>"})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if out.Decision != "block" {
		t.Fatalf("expected the replication block, got %q", out.Decision)
	}
	if !strings.Contains(out.Reason, "--no-findings") {
		t.Errorf("a findings-free gate must offer --no-findings, got %q", out.Reason)
	}
	if strings.Contains(out.Reason, "--finding <F-") {
		t.Errorf("a round with zero findings has no ID to name, got %q", out.Reason)
	}
}

// ...and the gates that ARE about findings still ask for them.
func TestStopSpecDone_FindingGateFooterAsksForFindingIDs(t *testing.T) {
	root, recipeID := setupStopRoot(t)
	writeVerifyLog(t, root, recipeID, []state.VerifyLogEntry{
		{Iteration: 1, Doc: "draft.md", Critical: 0, Major: 2, FullPass: true,
			Dimensions: []string{"audit", "simulate", "verify"},
			DocHash:    "sha256:aaa", VerificationHash: "sha256:v1", Status: "continue"},
	})

	h := NewStopHandler()
	out, err := h.Handle(&HookInput{CWD: root, StopHookContent: "<bts>DONE</bts>"})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if out.Decision != "block" {
		t.Fatalf("expected a block on open majors, got %q", out.Decision)
	}
	if !strings.Contains(out.Reason, "--finding <F-") {
		t.Errorf("a findings gate must ask which findings are excused, got %q", out.Reason)
	}
	if !strings.Contains(out.Reason, "--gate verification_not_passed") {
		t.Errorf("the footer must name a gate `override grant` accepts, got %q", out.Reason)
	}
}

// One clean round is a sample. Unchanged documents in the measured
// recipe produced criticals on re-verification, so a lone clean pass is
// not evidence the document is clean.
func TestStopSpecDone_BlocksOnUnreplicatedCleanPass(t *testing.T) {
	root, recipeID := setupStopRoot(t)
	writeVerifyLog(t, root, recipeID, []state.VerifyLogEntry{
		{Iteration: 1, Doc: "draft.md", Critical: 0, Major: 0, FullPass: true,
			Dimensions: []string{"audit", "simulate", "verify"}, DocHash: "sha256:aaa", Status: "converged"},
	})

	h := NewStopHandler()
	out, err := h.Handle(&HookInput{CWD: root, StopHookContent: "<bts>DONE</bts>"})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if out.Decision != "block" {
		t.Fatalf("a single clean round must not finalize, got decision=%q", out.Decision)
	}
	if !strings.Contains(out.Reason, "1 of 2 independent confirming rounds") {
		t.Errorf("reason should name the replication shortfall, got %q", out.Reason)
	}
}

// An edit between the two clean rounds changes the revision, so they are
// not two measurements of the same thing.
func TestStopSpecDone_EditBetweenCleanRoundsResetsConfirmation(t *testing.T) {
	root, recipeID := setupStopRoot(t)
	all := []string{"audit", "simulate", "verify"}
	writeVerifyLog(t, root, recipeID, []state.VerifyLogEntry{
		{Iteration: 1, Doc: "draft.md", Critical: 0, Major: 0, FullPass: true,
			Dimensions: all, DocHash: "sha256:aaa", VerificationHash: "sha256:v1", Status: "converged"},
		{Iteration: 2, Doc: "draft.md", Critical: 0, Major: 0, FullPass: true,
			Dimensions: all, DocHash: "sha256:bbb", VerificationHash: "sha256:v2", Status: "converged"},
	})

	h := NewStopHandler()
	out, err := h.Handle(&HookInput{CWD: root, StopHookContent: "<bts>DONE</bts>"})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if out.Decision != "block" {
		t.Fatalf("clean rounds on different revisions must not confirm each other, got %q", out.Decision)
	}
}

// A clean triple from one instrument is not evidence the others agree.
// The measured recipe set its convergence anchor on a verify-only round
// and ran its first completeness pass fifteen rounds later, which found
// four majors in the same text.
func TestStopSpecDone_BlocksOnSingleDimensionCleanPass(t *testing.T) {
	root, recipeID := setupStopRoot(t)
	writeVerifyLog(t, root, recipeID, []state.VerifyLogEntry{
		{Iteration: 1, Doc: "draft.md", Critical: 0, Major: 0, FullPass: true,
			Dimensions: []string{"verify"}, DocHash: "sha256:aaa", Status: "converged"},
		{Iteration: 2, Doc: "draft.md", Critical: 0, Major: 0, FullPass: true,
			Dimensions: []string{"verify"}, DocHash: "sha256:aaa", Status: "converged"},
	})

	h := NewStopHandler()
	out, err := h.Handle(&HookInput{CWD: root, StopHookContent: "<bts>DONE</bts>"})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if out.Decision != "block" {
		t.Fatalf("verify-only rounds must not finalize, got decision=%q", out.Decision)
	}
	if !strings.Contains(out.Reason, "recorded verify only") {
		t.Errorf("reason should name the missing dimensions, got %q", out.Reason)
	}
}

// A round with no recorded revision cannot be replicated against, and
// the gate says so instead of falling open — the failure mode that left
// rule 3 unenforced for the last seven rounds of the measured recipe.
func TestStopSpecDone_BlocksWhenRevisionUnrecorded(t *testing.T) {
	root, recipeID := setupStopRoot(t)
	writeVerifyLog(t, root, recipeID, []state.VerifyLogEntry{
		{Iteration: 1, Doc: "draft.md", Critical: 0, Major: 0, FullPass: true,
			Dimensions: []string{"audit", "simulate", "verify"}, Status: "converged"},
	})

	h := NewStopHandler()
	out, err := h.Handle(&HookInput{CWD: root, StopHookContent: "<bts>DONE</bts>"})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if out.Decision != "block" {
		t.Fatalf("a round with no doc_hash must not finalize, got decision=%q", out.Decision)
	}
	if !strings.Contains(out.Reason, "no doc_hash") {
		t.Errorf("reason should name the missing revision, got %q", out.Reason)
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

// The CLI refuses to grant an override for a gate that protects the
// integrity of the record, but overrides.jsonl is a plain file in the
// repo — a hand-written record must not get further than a granted one.
func TestStopSpecDone_HandWrittenOverrideOfANonOverridableGateIsIgnored(t *testing.T) {
	root, recipeID := setupStopRoot(t)
	writeVerifyLog(t, root, recipeID, []state.VerifyLogEntry{
		{Iteration: 1, Doc: "draft.md", Critical: 0, Major: 0, FullPass: true,
			Dimensions: []string{"audit", "simulate", "verify"}, DocHash: "sha256:aaa", Status: "converged"},
	})
	// per_document_verify_state protects the integrity of the record —
	// it is in the registry but not in the overridable set, so no grant
	// can produce this line and a hand-written one must not work either.
	if err := state.AppendOverride(root, recipeID, &state.OverrideRecord{
		Gate: "per_document_verify_state", Doc: "draft.md", Reason: "hand-written",
	}); err != nil {
		t.Fatal(err)
	}
	// And a legitimately overridable gate is still blocked by a record
	// naming a DIFFERENT gate.
	h := NewStopHandler()
	out, err := h.Handle(&HookInput{CWD: root, StopHookContent: "<bts>DONE</bts>"})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if out.Decision != "block" {
		t.Fatalf("an override of another gate must not excuse replicated_clean_pass, got %q", out.Decision)
	}
}

// The recorded, in-scope override does let the turn through.
func TestStopSpecDone_RecordedOverrideAllowsTheNamedGate(t *testing.T) {
	root, recipeID := setupStopRoot(t)
	writeVerifyLog(t, root, recipeID, []state.VerifyLogEntry{
		{Iteration: 1, Doc: "draft.md", Critical: 0, Major: 0, FullPass: true,
			Dimensions: []string{"audit", "simulate", "verify"}, DocHash: "sha256:aaa", Status: "converged"},
	})
	if err := state.AppendOverride(root, recipeID, &state.OverrideRecord{
		Gate: "replicated_clean_pass", Doc: "draft.md", DocHash: "sha256:aaa",
		Reason: "frozen for release", Findings: []string{"F-abc12345"},
	}); err != nil {
		t.Fatal(err)
	}
	h := NewStopHandler()
	out, err := h.Handle(&HookInput{CWD: root, StopHookContent: "<bts>DONE</bts>"})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if out.Decision == "block" && strings.Contains(out.Reason, "confirming rounds") {
		t.Fatalf("the recorded override should clear the replication gate, blocked with: %s", out.Reason)
	}
}

// An override granted on another revision must not apply: the operator
// weighed a specific text.
func TestStopSpecDone_StaleOverrideDoesNotApply(t *testing.T) {
	root, recipeID := setupStopRoot(t)
	writeVerifyLog(t, root, recipeID, []state.VerifyLogEntry{
		{Iteration: 1, Doc: "draft.md", Critical: 0, Major: 0, FullPass: true,
			Dimensions: []string{"audit", "simulate", "verify"}, DocHash: "sha256:bbb", Status: "converged"},
	})
	if err := state.AppendOverride(root, recipeID, &state.OverrideRecord{
		Gate: "replicated_clean_pass", Doc: "draft.md", DocHash: "sha256:aaa", Reason: "old text",
	}); err != nil {
		t.Fatal(err)
	}
	h := NewStopHandler()
	out, err := h.Handle(&HookInput{CWD: root, StopHookContent: "<bts>DONE</bts>"})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if out.Decision != "block" {
		t.Fatalf("a stale override must not apply, got %q", out.Decision)
	}
}

// One override must excuse one gate. An unclean round fails the clean
// check first, so overriding `verification_not_passed` used to let a
// delta, dimensionless round finalize without full_pass, dimensions or
// replication ever being evaluated.
func TestStopSpecDone_OverridingOneGateDoesNotCarryTheOthers(t *testing.T) {
	root, recipeID := setupStopRoot(t)
	writeVerifyLog(t, root, recipeID, []state.VerifyLogEntry{
		{Iteration: 1, Doc: "draft.md", Critical: 0, Major: 2, FullPass: false,
			DocHash: "sha256:aaa", VerificationHash: "sha256:v1", Status: "continue"},
	})
	if err := state.AppendOverride(root, recipeID, &state.OverrideRecord{
		Gate: "verification_not_passed", Doc: "draft.md", DocHash: "sha256:aaa",
		Reason:   "both majors are false claims in justification prose",
		Findings: []string{"F-abc12345", "F-def67890"},
	}); err != nil {
		t.Fatal(err)
	}

	h := NewStopHandler()
	out, err := h.Handle(&HookInput{CWD: root, StopHookContent: "<bts>DONE</bts>"})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if out.Decision != "block" {
		t.Fatalf("excusing the findings must not also excuse the delta pass, got %q", out.Decision)
	}
	if !strings.Contains(out.Reason, "delta pass") {
		t.Errorf("the block should now name the next unmet condition, got %q", out.Reason)
	}
}

// revision_recorded_before_final was unoverridable in principle: grant
// always pins a hash, and a pinned record read as stale against the
// empty round hash the gate fires on — so the block message named a
// command whose result could never apply. The pin now falls back to what
// the document hashes to right now, which is the question it asks.
func TestStopSpecDone_PinnedOverrideAppliesWhenTheRoundRecordedNoRevision(t *testing.T) {
	// setup returns a root whose last round recorded no doc_hash, with an
	// override of that gate pinned to pinTo.
	setup := func(t *testing.T, contents, pinTo string) string {
		t.Helper()
		root, recipeID := setupStopRoot(t)
		writeVerifyLog(t, root, recipeID, []state.VerifyLogEntry{
			{Iteration: 1, Doc: "draft.md", Critical: 0, Major: 0, FullPass: true,
				Dimensions:       []string{"audit", "simulate", "verify"},
				VerificationHash: "sha256:v1", Status: "converged"}, // no DocHash
		})
		docPath := filepath.Join(state.RecipeDir(root, recipeID), "draft.md")
		if err := os.WriteFile(docPath, []byte(contents), 0644); err != nil {
			t.Fatal(err)
		}
		if pinTo == "" {
			h, ok, err := state.FileContentHash(docPath)
			if err != nil || !ok {
				t.Fatalf("hash draft.md: %v ok=%v", err, ok)
			}
			pinTo = h
		}
		if err := state.AppendOverride(root, recipeID, &state.OverrideRecord{
			Gate: "revision_recorded_before_final", Doc: "draft.md", DocHash: pinTo,
			Reason: "the log predates --doc resolution; this is the text that was read",
		}); err != nil {
			t.Fatal(err)
		}
		return root
	}

	// Pinned to the text on disk: the override applies.
	root := setup(t, "# draft\n", "")
	out, err := NewStopHandler().Handle(&HookInput{CWD: root, StopHookContent: "<bts>DONE</bts>"})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if out.Decision == "block" && strings.Contains(out.Reason, "no doc_hash") {
		t.Fatalf("a pin matching the document on disk must apply, blocked with: %s", out.Reason)
	}

	// Pinned to text the document no longer carries: it does not.
	stale := setup(t, "# draft, revised\n", "sha256:"+strings.Repeat("a", 64))
	out2, err := NewStopHandler().Handle(&HookInput{CWD: stale, StopHookContent: "<bts>DONE</bts>"})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if out2.Decision != "block" {
		t.Errorf("an override pinned to a vanished revision must not apply; got decision=%q reason=%q",
			out2.Decision, out2.Reason)
	}
}
