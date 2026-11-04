package cli

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/engine"
	"github.com/imtemp-dev/claude-bts/internal/state"
)

// The fixture's guard is the line "return x > 0" in src/relay.py, and
// INV-001's falsifier is a shell stand-in for a test: it passes while the
// guard is there and prints its test id and fails when it is gone.
const (
	guardedCode      = "import os\n\n\ndef allow(x):\n    # the guard\n    return x > 0\n\n\ndef helper():\n    return 1\n"
	fixtureFalsifier = `grep -q 'return x > 0' src/relay.py || { echo 'FAILED tests/test_relay.py::test_inv_001'; exit 1; }`
	inv001TestID     = "tests/test_relay.py::test_inv_001"
)

func newMutateFixture(t *testing.T) (root, id string) {
	t.Helper()
	root, id = newBuildFixture(t)
	if err := os.WriteFile(filepath.Join(root, "src", "relay.py"), []byte(guardedCode), 0644); err != nil {
		t.Fatal(err)
	}
	ev := &state.BuildEvidence{Falsifiers: map[string]*state.Falsifier{
		"INV-001": {Command: fixtureFalsifier},
	}}
	if err := state.SaveBuildEvidence(root, id, ev); err != nil {
		t.Fatal(err)
	}
	return root, id
}

func writePatch(t *testing.T, root, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "m.patch")
	if err := os.WriteFile(p, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	return p
}

// Both patches carry git's usual three lines of context, so they still
// apply after an edit elsewhere in the file.
const breakGuardPatch = "--- a/src/relay.py\n+++ b/src/relay.py\n@@ -3,7 +3,7 @@\n \n" +
	" def allow(x):\n     # the guard\n-    return x > 0\n+    return True\n \n \n def helper():\n"

const commentOnlyPatch = "--- a/src/relay.py\n+++ b/src/relay.py\n@@ -2,7 +2,7 @@\n \n \n" +
	" def allow(x):\n-    # the guard\n+    # the guard, reworded\n     return x > 0\n \n \n"

func TestFalsifierMutate_KilledIsRecordedAndTheProjectIsUntouched(t *testing.T) {
	root, id := newMutateFixture(t)
	before, _ := engine.CodeHash(root, id)

	m, err := executeFalsifierMutate(root, id, mutateOptions{
		Inv: "INV-001", PatchFile: writePatch(t, root, breakGuardPatch), Expect: inv001TestID, TimeoutSec: 30,
	}, io.Discard)
	if err != nil {
		t.Fatalf("a mutation the falsifier catches must succeed: %v", err)
	}
	if !m.Killed() || m.BaseHash != before || m.Isolation != "walk" {
		t.Fatalf("expected a killed run on the current code, got %+v", m)
	}
	after, _ := engine.CodeHash(root, id)
	if after != before {
		t.Fatal("mutate must never change the project on disk")
	}
	if data, _ := os.ReadFile(filepath.Join(root, "src", "relay.py")); string(data) != guardedCode {
		t.Fatalf("the guard must still be in the real file: %q", data)
	}
	ev, _ := state.LoadBuildEvidence(root, id)
	if got := ev.Falsifiers["INV-001"].Mutation; got == nil || !got.Killed() || got.Patch != "mutations/INV-001.patch" {
		t.Fatalf("build-evidence.json must hold the run and the stored patch: %+v", got)
	}
	if _, err := os.Stat(filepath.Join(state.RecipeDir(root, id), "mutations", "INV-001.patch")); err != nil {
		t.Fatalf("the patch must be stored for --rerun: %v", err)
	}
	if reason := engine.CheckFalsifiersAbleToFail(ev, []string{"INV-001"}, before, id); reason != "" {
		t.Fatalf("the gate must accept a killed mutation on the current code: %s", reason)
	}
}

func TestFalsifierMutate_SurvivedIsRecordedAndRefused(t *testing.T) {
	root, id := newMutateFixture(t)
	_, err := executeFalsifierMutate(root, id, mutateOptions{
		Inv: "INV-001", PatchFile: writePatch(t, root, commentOnlyPatch), Expect: inv001TestID, TimeoutSec: 30,
	}, io.Discard)
	if !errors.Is(err, errMutationSurvived) {
		t.Fatalf("a falsifier that passes with the guard intact-in-effect must be reported as survived, got %v", err)
	}
	ev, _ := state.LoadBuildEvidence(root, id)
	m := ev.Falsifiers["INV-001"].Mutation
	if m == nil || m.Killed() || m.ExitCode != 0 {
		t.Fatalf("the survived run is evidence and must be recorded: %+v", m)
	}
}

func TestFalsifierMutate_FailureElsewhereIsNotAKill(t *testing.T) {
	root, id := newMutateFixture(t)
	_, err := executeFalsifierMutate(root, id, mutateOptions{
		Inv: "INV-001", PatchFile: writePatch(t, root, breakGuardPatch), Expect: "tests/test_relay.py::test_inv_999", TimeoutSec: 30,
	}, io.Discard)
	if !errors.Is(err, errMutationUnmatched) {
		t.Fatalf("a failure that does not print the expected test id must not count, got %v", err)
	}
}

func TestFalsifierMutate_RefusesPatchesThatAreNotAGuardBreak(t *testing.T) {
	root, id := newMutateFixture(t)
	cases := map[string]struct{ patch, want string }{
		"edits a test": {
			"--- a/tests/test_relay.py\n+++ b/tests/test_relay.py\n@@ -1 +1 @@\n-x\n+y\n", "a test",
		},
		"edits no recipe code": {
			"--- a/README.md\n+++ b/README.md\n@@ -1 +1 @@\n-x\n+y\n", "none of the recipe's code files",
		},
		"not git-style": {
			"--- src/relay.py\n+++ src/relay.py\n@@ -1 +1 @@\n-x\n+y\n", "git-style",
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := executeFalsifierMutate(root, id, mutateOptions{
				Inv: "INV-001", PatchFile: writePatch(t, root, c.patch), Expect: inv001TestID, TimeoutSec: 30,
			}, io.Discard)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("want an error containing %q, got %v", c.want, err)
			}
		})
	}
	if ev, _ := state.LoadBuildEvidence(root, id); ev.Falsifiers["INV-001"].Mutation != nil {
		t.Fatal("a refused patch must record nothing")
	}
}

func TestFalsifierMutate_NotMutableAndRerun(t *testing.T) {
	root, id := newMutateFixture(t)
	if err := executeMutateNotMutable(root, id, "INV-001", "held by the type system"); err != nil {
		t.Fatal(err)
	}
	ev, _ := state.LoadBuildEvidence(root, id)
	if ev.Falsifiers["INV-001"].NotMutable == "" {
		t.Fatal("the reason must be recorded")
	}

	if _, err := executeFalsifierMutate(root, id, mutateOptions{
		Inv: "INV-001", PatchFile: writePatch(t, root, breakGuardPatch), Expect: inv001TestID, TimeoutSec: 30,
	}, io.Discard); err != nil {
		t.Fatal(err)
	}

	// The code changes after the run: the gate sees it stale, and --rerun
	// reapplies the stored patch to the new code.
	if err := os.WriteFile(filepath.Join(root, "src", "relay.py"), []byte(guardedCode+"\n\ndef other():\n    pass\n"), 0644); err != nil {
		t.Fatal(err)
	}
	current, _ := engine.CodeHash(root, id)
	ev, _ = state.LoadBuildEvidence(root, id)
	if reason := engine.CheckFalsifiersAbleToFail(ev, []string{"INV-001"}, current, id); !strings.Contains(reason, "code changed after") {
		t.Fatalf("a run on older code must be stale, got %q", reason)
	}
	results, err := executeMutateRerun(root, id, 30, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if m := results["INV-001"]; m == nil || !m.Killed() || m.BaseHash != current {
		t.Fatalf("rerun must record a killed run on the current code: %+v", m)
	}
}

// In a git repository the copy is what git lists, tracked or not, without
// ignored files, so uncommitted code is mutated and virtualenvs are not
// copied. The repository here has no commits at all.
func TestFalsifierMutate_GitCopyIncludesUntrackedAndSkipsIgnored(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root, id := newMutateFixture(t)
	if out, err := exec.Command("git", "-C", root, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	_ = os.WriteFile(filepath.Join(root, ".gitignore"), []byte(".venv/\n"), 0644)
	_ = os.MkdirAll(filepath.Join(root, ".venv"), 0755)
	_ = os.WriteFile(filepath.Join(root, ".venv", "marker"), []byte("x"), 0644)

	ev, _ := state.LoadBuildEvidence(root, id)
	ev.Falsifiers["INV-001"].Command = "test ! -e .venv/marker && test ! -e .bts && " + fixtureFalsifier
	_ = state.SaveBuildEvidence(root, id, ev)

	m, err := executeFalsifierMutate(root, id, mutateOptions{
		Inv: "INV-001", PatchFile: writePatch(t, root, breakGuardPatch), Expect: inv001TestID, TimeoutSec: 30,
	}, io.Discard)
	if err != nil || m.Isolation != "git-ls-files" {
		t.Fatalf("expected a killed run on a git-listed copy without ignored files or .bts, got %+v, %v", m, err)
	}
}

func TestMutateChangelogActionValidates(t *testing.T) {
	root, id := newMutateFixture(t)
	if _, err := executeFalsifierMutate(root, id, mutateOptions{
		Inv: "INV-001", PatchFile: writePatch(t, root, breakGuardPatch), Expect: inv001TestID, TimeoutSec: 30,
	}, io.Discard); err != nil {
		t.Fatal(err)
	}
	errs, err := engine.ValidateRecipeDir(state.RecipeDir(root, id))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range errs {
		if e.File == "changelog.jsonl" && strings.Contains(e.Message, "invalid action") {
			t.Errorf("bts validate rejects an action the CLI writes: %s %s", e.Field, e.Message)
		}
	}
}
