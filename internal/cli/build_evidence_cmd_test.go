package cli

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/engine"
	"github.com/imtemp-dev/claude-bts/internal/state"
)

// newBuildFixture is a build recipe at the falsify step: domain.md with two
// invariants, a wireframe naming one code file, and that file as a skeleton.
func newBuildFixture(t *testing.T) (root, id string) {
	t.Helper()
	root, id = newTestRunFixture(t)
	dir := state.RecipeDir(root, id)
	write := func(p, s string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(s), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(dir, "domain.md"), "## 2. Invariants\n\n| ID | Statement | Owner |\n|---|---|---|\n"+
		"| INV-001 | one issue per PR | relay |\n| INV-002 | never post with the user token | relay |\n")
	write(filepath.Join(dir, "wireframe.md"), "## File Structure\n\n| # | File | Action | Depends On | Responsibility |\n"+
		"|---|---|---|---|---|\n| 1 | `src/relay.py` | create | — | the relay |\n")
	write(filepath.Join(root, "src", "relay.py"), "def handle(e): raise NotImplementedError\n")
	return root, id
}

func TestExecuteBuildCheck_RecordsExitAndCodeHash(t *testing.T) {
	root, id := newBuildFixture(t)
	run, err := executeBuildCheck(root, id, "exit 0", 30, io.Discard)
	if err != nil || !run.Passed() || run.CodeHash == "" {
		t.Fatalf("a passing build must be recorded with a code hash: %+v, %v", run, err)
	}
	ev, _ := state.LoadBuildEvidence(root, id)
	if ev.Skeleton == nil || ev.Skeleton.CodeHash != run.CodeHash {
		t.Fatalf("build-evidence.json must hold the run: %+v", ev.Skeleton)
	}

	failed, _ := executeBuildCheck(root, id, "exit 3", 30, io.Discard)
	if failed.Passed() || failed.ExitCode != 3 {
		t.Fatalf("a failing build is recorded as failed: %+v", failed)
	}
}

func TestExecuteFalsifierRed(t *testing.T) {
	root, id := newBuildFixture(t)

	if _, err := executeFalsifierRed(root, id, "INV-009", "exit 1", 30, io.Discard); err == nil || !strings.Contains(err.Error(), "not an invariant") {
		t.Fatalf("an id not in domain.md must be refused, got %v", err)
	}

	_, err := executeFalsifierRed(root, id, "INV-001", "exit 0", 30, io.Discard)
	if !errors.Is(err, errFalsifierPassed) {
		t.Fatalf("a test that passes on the skeleton must be refused, got %v", err)
	}
	if ev, _ := state.LoadBuildEvidence(root, id); ev.Falsifiers["INV-001"] != nil {
		t.Fatal("a refused falsifier must not be recorded")
	}

	run, err := executeFalsifierRed(root, id, "INV-001", "exit 1", 30, io.Discard)
	if err != nil || run.Passed() {
		t.Fatalf("a failing test is recorded as red: %+v, %v", run, err)
	}
	ev, _ := state.LoadBuildEvidence(root, id)
	if f := ev.Falsifiers["INV-001"]; f == nil || f.Command != "exit 1" || f.Red == nil {
		t.Fatalf("the falsifier and its red run must be recorded: %+v", f)
	}
}

func TestExecuteFalsifierCheck_GreenAndRed(t *testing.T) {
	root, id := newBuildFixture(t)
	marker := filepath.Join(root, "implemented")
	cmd := "test -f " + marker // fails until the marker exists, like a test before the body is written
	if _, err := executeFalsifierRed(root, id, "INV-001", cmd, 30, io.Discard); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Create(marker); err != nil {
		t.Fatal(err)
	}
	results, err := executeFalsifierCheck(root, id, false, 30, io.Discard)
	if err != nil || !results["INV-001"].Passed() {
		t.Fatalf("after the implementation the falsifier passes: %+v, %v", results, err)
	}
	ev, _ := state.LoadBuildEvidence(root, id)
	if ev.Falsifiers["INV-001"].Green == nil || ev.Falsifiers["INV-001"].Red == nil {
		t.Fatal("a green check is recorded beside the red run, not over it")
	}

	_ = os.Remove(marker)
	if results, _ := executeFalsifierCheck(root, id, true, 30, io.Discard); results["INV-001"].Passed() {
		t.Fatal("--red records the rerun as the red run")
	}
}

// Every changelog action these commands write must be one `bts validate`
// accepts. A smoke run of the real binary found all three rejected.
func TestBuildEvidenceChangelogActionsValidate(t *testing.T) {
	root, id := newBuildFixture(t)
	if _, err := executeBuildCheck(root, id, "exit 0", 30, io.Discard); err != nil {
		t.Fatal(err)
	}
	if _, err := executeFalsifierRed(root, id, "INV-001", "exit 1", 30, io.Discard); err != nil {
		t.Fatal(err)
	}
	if _, err := executeFalsifierCheck(root, id, false, 30, io.Discard); err != nil {
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

func TestExecuteFalsifierCheck_NothingRecorded(t *testing.T) {
	root, id := newBuildFixture(t)
	if _, err := executeFalsifierCheck(root, id, false, 30, io.Discard); err == nil {
		t.Fatal("a check with no falsifiers recorded must say so")
	}
}

// writeCarriedDomain gives the fixture a change recipe's domain.md: INV-001
// is introduced, INV-002 is carried from an earlier recipe (trial #41).
func writeCarriedDomain(t *testing.T, root, id string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(state.RecipeDir(root, id), "domain.md"), []byte(
		"## 2. Invariants\n\n| ID | Statement | Owner | Enforcement point |\n|---|---|---|---|\n"+
			"| INV-001 | one issue per PR | relay | lookup |\n"+
			"| INV-002 [carried: r-002 INV-008] | an issued token still pages | cursor | kept by `tests/test_cursor.py::test_old_token` |\n"), 0644); err != nil {
		t.Fatal(err)
	}
}

// Trial problem #41: a change must keep invariants earlier work
// established, and their tests already pass, so the red-first rule refused
// them and they stayed in prose.  A carried row is recorded the other way
// round: only a passing run counts, and it is kept as the green run.
func TestExecuteFalsifierRed_CarriedRowMustPass(t *testing.T) {
	root, id := newBuildFixture(t)
	writeCarriedDomain(t, root, id)

	_, err := executeFalsifierRed(root, id, "INV-002", "exit 1", 30, io.Discard)
	if err == nil || strings.Contains(err.Error(), "not an invariant") {
		t.Fatalf("a carried invariant's failing test must be refused as failing, got %v", err)
	}
	if ev, _ := state.LoadBuildEvidence(root, id); ev.Falsifiers["INV-002"] != nil {
		t.Fatal("a refused carried test must not be recorded")
	}

	run, err := executeFalsifierRed(root, id, "INV-002", "exit 0", 30, io.Discard)
	if err != nil || !run.Passed() {
		t.Fatalf("a carried invariant's passing test must be recorded: %+v, %v", run, err)
	}
	ev, _ := state.LoadBuildEvidence(root, id)
	if f := ev.Falsifiers["INV-002"]; f == nil || f.Command != "exit 0" || f.Red != nil || !f.Green.Passed() {
		t.Fatalf("a carried test is recorded as its green run, with no red: %+v", f)
	}

	// An introduced invariant is unchanged: it must still start red.
	if _, err := executeFalsifierRed(root, id, "INV-001", "exit 0", 30, io.Discard); !errors.Is(err, errFalsifierPassed) {
		t.Fatalf("an introduced invariant's passing test must still be refused, got %v", err)
	}
}

// `bts falsifier check --red` reruns every recorded falsifier on the
// skeleton.  A carried row is expected to pass there too, so its rerun is
// its green run and the red expectation does not apply to it (trial #41).
func TestExecuteFalsifierCheck_RedRerunExpectsCarriedGreen(t *testing.T) {
	root, id := newBuildFixture(t)
	writeCarriedDomain(t, root, id)
	if _, err := executeFalsifierRed(root, id, "INV-001", "exit 1", 30, io.Discard); err != nil {
		t.Fatal(err)
	}
	if _, err := executeFalsifierRed(root, id, "INV-002", "exit 0", 30, io.Discard); err != nil {
		t.Fatal(err)
	}
	results, err := executeFalsifierCheck(root, id, true, 30, io.Discard)
	if err != nil || results["INV-001"].Passed() || !results["INV-002"].Passed() {
		t.Fatalf("--red reruns both: %+v, %v", results, err)
	}
	ev, _ := state.LoadBuildEvidence(root, id)
	if f := ev.Falsifiers["INV-002"]; f.Red != nil || !f.Green.Passed() {
		t.Fatalf("a carried rerun under --red is its green run: %+v", f)
	}
	if !falsifierRunAsExpected(results["INV-002"], true, true) || falsifierRunAsExpected(results["INV-002"], false, true) {
		t.Fatal("under --red a carried row is as expected when it passes, an introduced one when it fails")
	}
}
