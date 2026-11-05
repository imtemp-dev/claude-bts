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

// A drive stand-in: it "launches" the app (reads its code), checks the end
// state a user would see, and writes its evidence; it fails when the code
// does not do what the journey expects.
const driveCmd = `mkdir -p .verify && grep -q 'return x > 0' src/relay.py && echo 'allow(0)=False' > .verify/zero.txt && ` +
	`printf '{"checks":[{"check":"allow(0) is refused","pass":true}]}' > .verify/verdict.json`

const driveVerdict = ".verify/verdict.json"

func TestProve_PlanRunDeferAndTheGate(t *testing.T) {
	root, id := newBuildFixture(t)
	hash := func() string { h, _ := engine.CodeHash(root, id); return h }
	gate := func() string {
		rec, _ := state.LoadProveRecord(root, id)
		return engine.CheckAppProven(rec, hash(), id)
	}

	if r := gate(); !strings.Contains(r, "No drive record") {
		t.Fatalf("no record must block, got %q", r)
	}
	if _, err := executeProvePlan(root, id, nil, "", ""); err == nil {
		t.Fatal("a plan with neither journeys nor --no-surface must be refused")
	}
	if _, err := executeProvePlan(root, id, []string{"zero-is-refused", "slack-in"}, "features/allow.md", ""); err != nil {
		t.Fatal(err)
	}
	if r := gate(); !strings.Contains(r, "slack-in, zero-is-refused not driven") {
		t.Fatalf("planned journeys must be driven or deferred, got %q", r)
	}
	if _, err := executeProveRun(root, id, "unplanned", driveCmd, nil, driveVerdict, 30, io.Discard); err == nil {
		t.Fatal("driving an unplanned journey must be refused")
	}

	// The code is wrong: the drive fails, and the evidence is missing.
	_ = os.WriteFile(filepath.Join(root, "src", "relay.py"), []byte(buggyCode), 0644)
	j, err := executeProveRun(root, id, "zero-is-refused", driveCmd, []string{".verify/zero.txt"}, driveVerdict, 30, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if j.Passed() || len(j.EvidenceMissing) != 1 {
		t.Fatalf("a failing drive with missing evidence must not pass: %+v", j)
	}

	// A drive that exits 0 but leaves no evidence proves nothing either.
	if j, err = executeProveRun(root, id, "zero-is-refused", "true", []string{".verify/never-written.txt"}, driveVerdict, 30, io.Discard); err != nil || j.Passed() {
		t.Fatalf("an exit 0 without its evidence must not pass: %+v %v", j, err)
	}

	// Fixed: the drive passes and leaves its evidence.
	_ = os.WriteFile(filepath.Join(root, "src", "relay.py"), []byte(fixedCode), 0644)
	if j, err = executeProveRun(root, id, "zero-is-refused", driveCmd, []string{".verify/zero.txt"}, driveVerdict, 30, io.Discard); err != nil || !j.Passed() {
		t.Fatalf("a passing drive with its evidence must pass: %+v %v", j, err)
	}
	if err := executeProveDefer(root, id, "slack-in", "needs the installed Slack app", ""); err == nil {
		t.Fatal("a deferral needs an owner")
	}
	if err := executeProveDefer(root, id, "slack-in", "needs the installed Slack app and real tokens", "AGEN-39 live checks"); err != nil {
		t.Fatal(err)
	}
	if r := gate(); r != "" {
		t.Fatalf("a driven journey and a deferred one meet the gate: %s", r)
	}

	// A later code change makes the drive stale.
	_ = os.WriteFile(filepath.Join(root, "src", "relay.py"), []byte(fixedCode+"# later\n"), 0644)
	if r := gate(); !strings.Contains(r, "the code changed after zero-is-refused was driven") {
		t.Fatalf("a drive on older code must be stale, got %q", r)
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

func TestProve_NoSurface(t *testing.T) {
	root, id := newBuildFixture(t)
	if _, err := executeProvePlan(root, id, []string{"x"}, "", "library internals"); err == nil {
		t.Fatal("--no-surface and --journey exclude each other")
	}
	rec, err := executeProvePlan(root, id, nil, "", "a refactor of an internal helper; nothing a user drives changes")
	if err != nil {
		t.Fatal(err)
	}
	if r := engine.CheckAppProven(rec, "", id); r != "" {
		t.Fatalf("a recorded no-surface reason meets the gate: %s", r)
	}
}
