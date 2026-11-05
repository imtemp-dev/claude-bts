package cli

import (
	"io"
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/engine"
	"github.com/imtemp-dev/claude-bts/internal/state"
)

// A falsifier's test renamed after it was recorded could not be followed
// (trial problem #76, Multica AGEN-56).  true-shark's tests carried the
// invariant ids the falsify skill asked for, which Carepatron forbids in
// committed code; but `falsifier run` records only a test that fails on
// the skeleton, so after the implementation nothing could take the new
// name, and `falsifier check` and the mutation rerun kept the old command,
// whose filter may then match nothing and pass.
//
// `bts falsifier repoint` takes the new command when it gives the result
// the recorded one has, and the able-to-fail gate treats a mutation run
// under another command as stale until it is rerun.
func TestFalsifierRepoint_FollowsARenamedTest(t *testing.T) {
	root, id := newMutateFixture(t)
	if _, err := executeFalsifierCheck(root, id, false, 30, io.Discard); err != nil {
		t.Fatal(err)
	}
	if _, err := executeFalsifierMutate(root, id, mutateOptions{
		Inv: "INV-001", PatchFile: writePatch(t, root, breakGuardPatch), Expect: inv001TestID, TimeoutSec: 30,
	}, io.Discard); err != nil {
		t.Fatal(err)
	}

	renamed := strings.ReplaceAll(fixtureFalsifier, "test_inv_001", "test_one_issue_per_pr")
	renamedID := "tests/test_relay.py::test_one_issue_per_pr"

	for _, c := range []struct{ name, inv, cmd, expect, want string }{
		{"no falsifier recorded", "INV-002", renamed, renamedID, "no falsifier recorded"},
		{"fails where the recorded run passes", "INV-001", "exit 1", renamedID, "must pass"},
		{"a mutation run and no --expect", "INV-001", renamed, "", "--expect"},
	} {
		if _, err := executeFalsifierRepoint(root, id, c.inv, c.cmd, c.expect, 30, io.Discard); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: want an error naming %q, got %v", c.name, c.want, err)
		}
	}
	if ev, _ := state.LoadBuildEvidence(root, id); ev.Falsifiers["INV-001"].Command != fixtureFalsifier {
		t.Fatal("a refused repoint must change nothing")
	}

	run, err := executeFalsifierRepoint(root, id, "INV-001", renamed, renamedID, 30, io.Discard)
	if err != nil || !run.Passed() {
		t.Fatalf("the renamed test passes as the old one did: %+v, %v", run, err)
	}
	ev, _ := state.LoadBuildEvidence(root, id)
	f := ev.Falsifiers["INV-001"]
	if f.Command != renamed || f.Green == nil || f.Green.Command != renamed || f.Mutation.Expect != renamedID {
		t.Fatalf("the command, its green run and the mutation's expected id must follow the rename: %+v / %+v", f, f.Mutation)
	}

	current, _ := engine.CodeHash(root, id)
	if reason := engine.CheckFalsifiersAbleToFail(ev, []string{"INV-001"}, current, id); !strings.Contains(reason, "command changed") {
		t.Fatalf("a mutation run under the old command proves nothing about the new one, got %q", reason)
	}
	results, err := executeMutateRerun(root, id, 30, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if m := results["INV-001"]; m == nil || !m.Killed() || m.Command != renamed {
		t.Fatalf("the rerun must kill the guard break under the new command: %+v", m)
	}
	ev, _ = state.LoadBuildEvidence(root, id)
	if reason := engine.CheckFalsifiersAbleToFail(ev, []string{"INV-001"}, current, id); reason != "" {
		t.Fatalf("after the rerun the gate must be met, got %q", reason)
	}
}

// On the skeleton the recorded run is red, so the new command must fail
// too, and it replaces the red run.
func TestFalsifierRepoint_OnTheSkeletonStaysRed(t *testing.T) {
	root, id := newBuildFixture(t)
	if _, err := executeFalsifierRed(root, id, "INV-001", "exit 1", 30, io.Discard); err != nil {
		t.Fatal(err)
	}
	if _, err := executeFalsifierRepoint(root, id, "INV-001", "exit 0", "", 30, io.Discard); err == nil || !strings.Contains(err.Error(), "must fail") {
		t.Fatalf("a command that passes on the skeleton must be refused, got %v", err)
	}
	if _, err := executeFalsifierRepoint(root, id, "INV-001", "exit 2", "", 30, io.Discard); err != nil {
		t.Fatal(err)
	}
	ev, _ := state.LoadBuildEvidence(root, id)
	if f := ev.Falsifiers["INV-001"]; f.Command != "exit 2" || f.Red == nil || f.Red.Command != "exit 2" || f.Green != nil {
		t.Fatalf("the red run must follow the new command: %+v", f)
	}
}
