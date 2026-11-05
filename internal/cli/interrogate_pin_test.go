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

// A proven finding closed only when the adversary's own command passed.
// On true-shark the chosen fix refused the input another way than two
// repros asserted (a 400 before the token rule, where one repro wanted
// InvalidContinuationToken and another a narrowed 200), so the repros kept
// failing and `waive` was the only way out, although a regression test red
// before the fix and green after pinned it (trial problem #83, Multica
// AGEN-60).
//
// Here the repro asserts a ValueError, the fix returns False instead, and
// the regression test pins the fix: it passes on the code and fails with
// the fix reverted.
const (
	shapeRepro    = "grep -q 'raise ValueError' src/relay.py || { echo 'BUG: allow(0) is not refused with ValueError'; exit 1; }\n"
	pinTest       = `grep -q 'return x > 0' src/relay.py || { echo 'FAILED tests/test_relay.py::test_zero_is_refused'; exit 1; }`
	pinTestID     = "tests/test_relay.py::test_zero_is_refused"
	revertFixDiff = "--- a/src/relay.py\n+++ b/src/relay.py\n@@ -1,2 +1,2 @@\n def allow(x):\n-    return x > 0\n+    return x >= 0\n"
	editTestDiff  = "--- a/tests/test_relay.py\n+++ b/tests/test_relay.py\n@@ -1 +1 @@\n-x\n+y\n"
)

func TestInterrogatePin_ClosesAFindingFixedAnotherWay(t *testing.T) {
	root, id, run := newInterrogateFixture(t)
	a1, a2 := run.Adversaries[0], run.Adversaries[1]
	_ = os.MkdirAll(filepath.Join(a1.Copy, "repro"), 0755)
	_ = os.WriteFile(filepath.Join(a1.Copy, "repro", "shape.sh"), []byte(shapeRepro), 0755)
	writeReport(t, root, id, a1, []map[string]any{
		{"title": "allow(0) is true", "command": "sh repro/shape.sh", "repro_files": []string{"repro/shape.sh"}},
	})
	writeReport(t, root, id, a2, []map[string]any{})
	if _, err := executeInterrogateVerify(root, id, 30, io.Discard); err != nil {
		t.Fatal(err)
	}

	// The fix refuses allow(0) by returning False; the repro still fails.
	_ = os.WriteFile(filepath.Join(root, "src", "relay.py"), []byte(fixedCode), 0644)
	if r, _ := executeInterrogateRecheck(root, id, 30, io.Discard); r.Findings[0].Status != state.InterrogateReproduced {
		t.Fatalf("the repro asserts another refusal, so it still fails: %s", r.Findings[0].Status)
	}

	patch := func(body string) string { return writePatch(t, root, body) }
	for _, c := range []struct{ name, cmd, revert, want string }{
		{"a revert that edits a test", pinTest, patch(editTestDiff), "a test"},
		{"a test that fails on the code", "sh -c 'exit 1'", patch(revertFixDiff), "fails on the current code"},
		{"a test that passes with the fix reverted", "true", patch(revertFixDiff), "still passes with the fix reverted"},
		{"a failure that does not name the test", "grep -q 'return x > 0' src/relay.py", patch(revertFixDiff), "--expect"},
	} {
		_, err := executeInterrogatePin(root, id, pinOptions{Finding: "F-001", Command: c.cmd, RevertFile: c.revert, Expect: pinTestID, TimeoutSec: 30}, io.Discard)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: want an error naming %q, got %v", c.name, c.want, err)
		}
	}
	if r, _ := state.LoadInterrogateRun(root, id); r.Findings[0].Status != state.InterrogateReproduced || r.Findings[0].Pin != nil {
		t.Fatalf("a refused pin must change nothing: %+v", r.Findings[0])
	}
	if _, err := os.Stat(filepath.Join(state.RecipeDir(root, id), "interrogate", "pins", "F-001.patch")); err == nil {
		t.Fatal("a refused pin must store no patch")
	}

	f, err := executeInterrogatePin(root, id, pinOptions{Finding: "F-001", Command: pinTest, RevertFile: patch(revertFixDiff), Expect: pinTestID, TimeoutSec: 30}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if f.Status != state.InterrogateFixed || f.Pin == nil || !f.Pin.Revert.Killed() || !f.Recheck.Passed() {
		t.Fatalf("the finding is fixed, pinned by a test that passes now and failed with the fix reverted: %+v", f)
	}
	hash, _ := engine.CodeHash(root, id)
	r, _ := state.LoadInterrogateRun(root, id)
	if reason := engine.CheckInterrogateClean(r, hash, id); reason != "" {
		t.Fatalf("a pinned finding meets the gate: %s", reason)
	}

	// The recheck reruns the pinned test, not the repro: the finding stays
	// fixed while the fix holds, and reopens when it is undone.
	if r, _ := executeInterrogateRecheck(root, id, 30, io.Discard); r.Findings[0].Status != state.InterrogateFixed {
		t.Fatalf("the pinned test passes, so the finding stays fixed: %s", r.Findings[0].Status)
	}
	_ = os.WriteFile(filepath.Join(root, "src", "relay.py"), []byte(buggyCode), 0644)
	if r, _ := executeInterrogateRecheck(root, id, 30, io.Discard); r.Findings[0].Status != state.InterrogateReproduced {
		t.Fatalf("with the fix undone the pinned test fails and the finding reopens: %s", r.Findings[0].Status)
	}

	errs, err := engine.ValidateRecipeDir(state.RecipeDir(root, id))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range errs {
		if e.File == "changelog.jsonl" && strings.Contains(e.Message, "invalid action") {
			t.Errorf("bts validate rejects an action the CLI writes: %s", e.Message)
		}
	}
}
