package cli

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

// envSetup makes the fixture's ignored environment in a copy, the way
// `uv sync` makes .venv.
const envSetup = "mkdir -p env/bin && printf '#!/bin/sh\\nexit 0\\n' > env/bin/depcli && chmod +x env/bin/depcli"

// newEnvFixture is the interrogate fixture in a git repository whose
// environment, env/ (a stand-in for .venv), is ignored, so no copy bts
// makes has it.  The recipe's own tests need it, as test-results.json
// records them.
func newEnvFixture(t *testing.T) (root, id string, run *state.InterrogateRun) {
	t.Helper()
	root, id = newBuildFixture(t)
	if err := os.WriteFile(filepath.Join(root, "src", "relay.py"), []byte(buggyCode), 0644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "init", "-q", root).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	_ = os.WriteFile(filepath.Join(root, ".gitignore"), []byte("/env/\n"), 0644)
	setup := exec.Command("sh", "-c", envSetup)
	setup.Dir = root
	if out, err := setup.CombinedOutput(); err != nil {
		t.Fatalf("env: %v %s", err, out)
	}
	if err := state.SaveTestResults(root, id, &state.TestResults{RecipeID: id, Status: "pass", Command: "./env/bin/depcli", RecordedBy: "bts"}); err != nil {
		t.Fatal(err)
	}
	run, err := executeInterrogatePrepare(root, id, 2)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, a := range run.Adversaries {
			_ = os.RemoveAll(a.Copy)
		}
	})
	return root, id, run
}

// reportEnvBug has a1 report the real bug through a command that needs the
// environment, as a pytest run needs the venv.
func reportEnvBug(t *testing.T, root, id string, run *state.InterrogateRun) {
	t.Helper()
	a1, a2 := run.Adversaries[0], run.Adversaries[1]
	_ = os.MkdirAll(filepath.Join(a1.Copy, "repro"), 0755)
	_ = os.WriteFile(filepath.Join(a1.Copy, "repro", "zero.sh"), []byte(reproScript), 0755)
	writeReport(t, root, id, a1, []map[string]any{
		{"title": "allow(0) is true", "command": "./env/bin/depcli && sh repro/zero.sh", "repro_files": []string{"repro/zero.sh"}},
	})
	writeReport(t, root, id, a2, nil)
}

// Trial problem #141: a rerun that fails because the copy lacks the
// project's environment is not a reproduction, and verify records nothing.
func TestInterrogateVerify_CopyWithoutEnvironmentIsNotAReproduction(t *testing.T) {
	root, id, run := newEnvFixture(t)
	reportEnvBug(t, root, id, run)
	_, err := executeInterrogateVerify(root, id, 30, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "could not run") || !strings.Contains(err.Error(), "F-001") {
		t.Fatalf("a rerun the copy cannot run must be refused, not counted: %v", err)
	}
	after, _ := state.LoadInterrogateRun(root, id)
	if after.VerifiedAt != "" || len(after.Findings) != 0 {
		t.Fatalf("nothing is recorded: verified %q, findings %d", after.VerifiedAt, len(after.Findings))
	}
	for _, a := range after.Adversaries {
		if _, err := os.Stat(a.Copy); err != nil {
			t.Fatalf("the adversary copies stay for the next verify: %v", err)
		}
	}
}
