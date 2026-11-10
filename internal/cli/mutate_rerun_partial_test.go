package cli

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/engine"
	"github.com/imtemp-dev/claude-bts/internal/state"
)

// INV-002's guard in the fixture is helper()'s "return 1"; its falsifier
// prints its test id and fails when that line is gone.
const (
	inv002Falsifier = `grep -q 'return 1' src/relay.py || { echo 'FAILED tests/test_relay.py::test_inv_002'; exit 1; }`
	inv002TestID    = "tests/test_relay.py::test_inv_002"
	breakHelperDiff = "--- a/src/relay.py\n+++ b/src/relay.py\n@@ -8,3 +8,3 @@\n \n def helper():\n-    return 1\n+    return 2\n"
)

// Trial problem #127: `bts falsifier mutate --rerun` stopped at the first
// stored patch that no longer applied, and recorded none of the reruns
// before it.  On cool-crow (r-005, 11-10 22:26Z) seven reruns, each with a
// Swift build, were lost at INV-035.  The rerun exists for changed code,
// which is when a patch stops applying.
func TestMutateRerun_RecordsWhatRanWhenAPatchNoLongerApplies(t *testing.T) {
	root, id := newMutateFixture(t)
	ev, _ := state.LoadBuildEvidence(root, id)
	ev.Falsifiers["INV-002"] = &state.Falsifier{Command: inv002Falsifier}
	if err := state.SaveBuildEvidence(root, id, ev); err != nil {
		t.Fatal(err)
	}
	for _, o := range []mutateOptions{
		{Inv: "INV-001", PatchFile: writePatch(t, root, breakGuardPatch), Expect: inv001TestID, TimeoutSec: 30},
		{Inv: "INV-002", PatchFile: writePatch(t, root, breakHelperDiff), Expect: inv002TestID, TimeoutSec: 30},
	} {
		if _, err := executeFalsifierMutate(root, id, o, io.Discard); err != nil {
			t.Fatalf("%s: %v", o.Inv, err)
		}
	}

	// The guard INV-001's patch breaks is rewritten; helper() is untouched.
	changed := strings.Replace(guardedCode, "    # the guard\n    return x > 0\n", "    # the guard, rewritten\n    return 0 < x\n", 1)
	if err := os.WriteFile(filepath.Join(root, "src", "relay.py"), []byte(changed), 0644); err != nil {
		t.Fatal(err)
	}
	current, _ := engine.CodeHash(root, id)

	results, err := executeMutateRerun(root, id, 30, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "INV-001") || !strings.Contains(err.Error(), "--inv <id> --patch") {
		t.Fatalf("INV-001's patch no longer applies: the rerun must say so with the next step, got %v", err)
	}
	if m := results["INV-002"]; m == nil || !m.Killed() {
		t.Fatalf("INV-002's rerun ran and must be returned: %+v", m)
	}
	ev, _ = state.LoadBuildEvidence(root, id)
	if m := ev.Falsifiers["INV-002"].Mutation; m == nil || m.BaseHash != current {
		t.Fatalf("INV-002's rerun on the current code must be recorded, got %+v", m)
	}
}
