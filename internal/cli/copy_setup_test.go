package cli

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/feature"
	"github.com/imtemp-dev/claude-bts/internal/state"
)

// Trial problems #141 and #156: with a setup, every copy bts makes has the
// project's environment.

func writeSetupSetting(t *testing.T, root, setup string) {
	t.Helper()
	dir := filepath.Join(root, ".bts", "config")
	_ = os.MkdirAll(dir, 0755)
	if err := os.WriteFile(filepath.Join(dir, "settings.yaml"), []byte("implement:\n  setup_cmd: \""+strings.ReplaceAll(setup, `\`, `\\`)+"\"\n"), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestInterrogateVerify_SetupGivesTheCopiesTheEnvironment(t *testing.T) {
	root, id, run := newEnvFixture(t)
	reportEnvBug(t, root, id, run)
	verified, err := executeInterrogateVerifyWith(root, id, envSetup, 30, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	f := verified.Findings[0]
	if f.Status != state.InterrogateReproduced || f.Verify.Setup != envSetup || f.Verify.ExitCode != 1 {
		t.Fatalf("with the setup the real bug reproduces, and the run records its setup: %+v %+v", f, f.Verify)
	}

	// Fixed, rechecked without a setup: the rerun cannot run, so F-001 keeps
	// its status and the recheck says so.
	_ = os.WriteFile(filepath.Join(root, "src", "relay.py"), []byte(fixedCode), 0644)
	after, err := executeInterrogateRecheck(root, id, 30, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "could not run") || !strings.Contains(err.Error(), "--recheck") {
		t.Fatalf("a recheck the copy cannot run must say so: %v", err)
	}
	if g := after.Findings[0]; g.Status != state.InterrogateReproduced || g.Recheck != nil {
		t.Fatalf("its status stays, with no recheck recorded: %s %+v", g.Status, g.Recheck)
	}
	// The setting gives the copies the environment: the recheck finds it fixed.
	writeSetupSetting(t, root, envSetup)
	after, err = executeInterrogateRecheck(root, id, 30, io.Discard)
	if err != nil || after.Findings[0].Status != state.InterrogateFixed {
		t.Fatalf("with implement.setup_cmd the recheck runs: %v %+v", err, after.Findings[0])
	}
}

// What a repro finds missing is the finding's own when the recipe's tests
// pass in the same kind of copy.
func TestInterrogateVerify_MissingNameInASoundCopyStands(t *testing.T) {
	root, id, run := newEnvFixture(t)
	if err := state.SaveTestResults(root, id, &state.TestResults{RecipeID: id, Status: "pass", Command: "true", RecordedBy: "bts"}); err != nil {
		t.Fatal(err)
	}
	writeReport(t, root, id, run.Adversaries[0], []map[string]any{
		{"title": "the relay calls a tool it never installs", "command": "./bin/relay-tool"},
	})
	writeReport(t, root, id, run.Adversaries[1], nil)
	verified, err := executeInterrogateVerify(root, id, 30, io.Discard)
	if err != nil || verified.Findings[0].Status != state.InterrogateReproduced {
		t.Fatalf("the copy is sound, so the finding stands: %v %+v", err, verified)
	}
}

func TestInterrogatePrepare_RunsTheSetupInEachCopy(t *testing.T) {
	root, id := newBuildFixture(t)
	writeSetupSetting(t, root, envSetup)
	run, err := executeInterrogatePrepare(root, id, 2)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, a := range run.Adversaries {
			_ = os.RemoveAll(a.Copy)
		}
	})
	for _, a := range run.Adversaries {
		if _, err := os.Stat(filepath.Join(a.Copy, "env", "bin", "depcli")); err != nil {
			t.Fatalf("%s's copy must have run the setup: %v", a.Name, err)
		}
	}

	writeSetupSetting(t, root, "exit 3")
	_ = os.Remove(filepath.Join(state.RecipeDir(root, id), "interrogate.json"))
	if _, err := executeInterrogatePrepare(root, id, 2); err == nil || !strings.Contains(err.Error(), "setup") {
		t.Fatalf("a failing setup stops prepare: %v", err)
	}
}

// The flag wins, then implement.setup_cmd, then the bound feature's setup.
func TestCopySetupOrder(t *testing.T) {
	root, id := newBuildFixture(t)
	if got := copySetup(root, id, ""); got != "" {
		t.Fatalf("no setup anywhere: %q", got)
	}
	t.Setenv(feature.StoreEnv, t.TempDir())
	dir, f, _, err := feature.Create(feature.CreateOptions{Title: "x", Repo: &feature.Repo{Key: "local/x-1"}, Setup: "make deps"})
	if err != nil {
		t.Fatal(err)
	}
	r, _ := state.LoadRecipeState(root, id)
	r.Feature, r.FeatureItem, r.FeatureAttempt, r.FeatureDir = f.ID, "i1", 1, dir
	if err := state.SaveRecipeState(root, r); err != nil {
		t.Fatal(err)
	}
	if got := copySetup(root, id, ""); got != "make deps" {
		t.Fatalf("the feature's setup: %q", got)
	}
	writeSetupSetting(t, root, "uv sync")
	if got := copySetup(root, id, ""); got != "uv sync" {
		t.Fatalf("implement.setup_cmd wins over the feature: %q", got)
	}
	if got := copySetup(root, id, " npm ci "); got != "npm ci" {
		t.Fatalf("the flag wins: %q", got)
	}
	if err := feature.SetSetup(dir, ""); err != nil {
		t.Fatal(err)
	}
	if g, _ := feature.Load(dir); g.Setup != "" {
		t.Fatalf("cleared: %q", g.Setup)
	}
}
