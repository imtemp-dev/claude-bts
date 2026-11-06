package cli

import (
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

// Trial problem #91: on true-shark the PR review changed the code under
// two waived findings (F-003, F-005), so their waiver reasons went stale,
// and `bts interrogate waive` refused to restate them: "only a reproduced
// finding can be waived".  A waived finding's reason can be restated, and
// the changelog keeps both.
func TestInterrogateWaive_RestatesAWaiverReason(t *testing.T) {
	root := newRecipeFixture(t, "r-091", "test", 0, 0, nil)
	run := &state.InterrogateRun{Round: 1, Findings: []*state.InterrogateFinding{
		{ID: "F-003", Status: state.InterrogateWaived, WaiveReason: "numbers are matched trimmed; the user accepts it"},
		{ID: "F-004", Status: state.InterrogateFixed},
	}}
	if err := state.SaveInterrogateRun(root, "r-091", run); err != nil {
		t.Fatal(err)
	}

	if err := executeInterrogateWaive(root, "r-091", "F-003", "numbers are now matched as stored; the user accepts the leading-zero case"); err != nil {
		t.Fatalf("a waived finding's reason can be restated: %v", err)
	}
	got, _ := state.LoadInterrogateRun(root, "r-091")
	if got.Findings[0].Status != state.InterrogateWaived || !strings.Contains(got.Findings[0].WaiveReason, "matched as stored") {
		t.Fatalf("the new reason replaces the old one, got %+v", got.Findings[0])
	}
	entries, _ := state.ReadChangelog(root, "r-091")
	last := entries[len(entries)-1].Result
	if !strings.Contains(last, "restated") || !strings.Contains(last, "matched trimmed") {
		t.Errorf("the changelog keeps the old reason beside the new one, got %q", last)
	}

	if err := executeInterrogateWaive(root, "r-091", "F-004", "anything"); err == nil {
		t.Error("a fixed finding still cannot be waived")
	}
}
