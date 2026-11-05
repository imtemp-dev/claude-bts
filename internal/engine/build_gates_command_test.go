package engine

import (
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

// A mutation run proves the command it ran.  Once the falsifier's command
// changes (a renamed test, `bts falsifier repoint`), that run says nothing
// about the new command, so the able-to-fail gate reads it as stale
// (trial problem #76, AGEN-56).
func TestCheckFalsifiersAbleToFail_MutationUnderAnotherCommandIsStale(t *testing.T) {
	killed := func(cmd string) *state.MutationRun {
		return &state.MutationRun{Command: cmd, ExitCode: 1, Expect: "t", Matched: true, BaseHash: "h1"}
	}
	same := &state.BuildEvidence{Falsifiers: map[string]*state.Falsifier{
		"INV-001": {Command: "pytest -k one_issue", Mutation: killed("pytest -k one_issue")},
	}}
	if reason := CheckFalsifiersAbleToFail(same, []string{"INV-001"}, "h1", "r-1"); reason != "" {
		t.Fatalf("a killed run under the current command on the current code meets the gate, got %q", reason)
	}
	moved := &state.BuildEvidence{Falsifiers: map[string]*state.Falsifier{
		"INV-001": {Command: "pytest -k one_issue", Mutation: killed("pytest -k inv_001")},
	}}
	if reason := CheckFalsifiersAbleToFail(moved, []string{"INV-001"}, "h1", "r-1"); !strings.Contains(reason, "command changed") {
		t.Fatalf("a run under another command must be stale, got %q", reason)
	}
}
