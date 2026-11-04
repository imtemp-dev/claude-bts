package cli

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/engine"
	"github.com/imtemp-dev/claude-bts/internal/state"
)

// The fixture's code has a real bug: allow(0) is true, though the intent
// is "only positive x". Adversary a1 proves it with a repro script that
// fails while the bug is there; the rest of the reports show what verify
// must drop.
const buggyCode = "def allow(x):\n    return x >= 0\n"
const fixedCode = "def allow(x):\n    return x > 0\n"
const reproScript = "grep -q 'return x > 0' src/relay.py || { echo 'BUG: allow(0) is true'; exit 1; }\n"

func newInterrogateFixture(t *testing.T) (root, id string, run *state.InterrogateRun) {
	t.Helper()
	root, id = newBuildFixture(t)
	if err := os.WriteFile(filepath.Join(root, "src", "relay.py"), []byte(buggyCode), 0644); err != nil {
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

func writeReport(t *testing.T, root, id string, a *state.Adversary, findings []map[string]any) {
	t.Helper()
	data, _ := json.Marshal(map[string]any{"adversary": a.Name, "findings": findings})
	if err := os.WriteFile(filepath.Join(state.RecipeDir(root, id), filepath.FromSlash(a.Report)), data, 0644); err != nil {
		t.Fatal(err)
	}
}

func TestInterrogatePrepare_GivesEachAdversaryAPrivateCopy(t *testing.T) {
	root, id, run := newInterrogateFixture(t)
	if len(run.Adversaries) != 2 || run.Adversaries[0].Angle == run.Adversaries[1].Angle {
		t.Fatalf("two seats with different angles, got %+v", run.Adversaries)
	}
	for _, a := range run.Adversaries {
		got, err := os.ReadFile(filepath.Join(a.Copy, "src", "relay.py"))
		if err != nil || string(got) != buggyCode {
			t.Fatalf("%s's copy must hold the current code: %q %v", a.Name, got, err)
		}
		if _, err := os.Stat(filepath.Join(a.Copy, ".bts")); err == nil {
			t.Fatalf("%s's copy must not carry .bts", a.Name)
		}
	}
	if _, err := executeInterrogatePrepare(root, id, 2); err == nil {
		t.Fatal("a second prepare must be refused while a run exists")
	}
	if _, err := executeInterrogatePrepare(root, "r-777-test-run", 5); err == nil {
		t.Fatal("more than three seats must be refused")
	}
}

func TestInterrogateVerify_KeepsOnlyWhatFailsAgainOnAFreshCopy(t *testing.T) {
	root, id, run := newInterrogateFixture(t)
	a1, a2 := run.Adversaries[0], run.Adversaries[1]

	_ = os.MkdirAll(filepath.Join(a1.Copy, "repro"), 0755)
	_ = os.WriteFile(filepath.Join(a1.Copy, "repro", "zero.sh"), []byte(reproScript), 0755)
	writeReport(t, root, id, a1, []map[string]any{
		{"title": "allow(0) is true", "command": "sh repro/zero.sh", "expected": "allow(0) is false", "repro_files": []string{"repro/zero.sh"}},
		{"title": "claims a bug it cannot show", "command": "true"},
	})
	writeReport(t, root, id, a2, []map[string]any{
		{"title": "no command at all"},
		{"title": "edits the code to make a bug", "command": "sh -c 'exit 1'", "repro_files": []string{"src/relay.py"}},
	})

	verified, err := executeInterrogateVerify(root, id, 30, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"F-001": "reproduced", "F-002": "dropped", "F-003": "dropped", "F-004": "dropped"}
	for _, f := range verified.Findings {
		if f.Status != want[f.ID] {
			t.Errorf("%s (%s): status %s, want %s (%s)", f.ID, f.Title, f.Status, want[f.ID], f.DropReason)
		}
	}
	if !strings.Contains(verified.Findings[3].DropReason, "code files") {
		t.Errorf("a repro that replaces the code under test must be dropped for that reason: %q", verified.Findings[3].DropReason)
	}
	for _, a := range verified.Adversaries {
		if _, err := os.Stat(a.Copy); err == nil {
			t.Errorf("the adversary copies are removed after verify: %s", a.Copy)
		}
	}
	hash, _ := engine.CodeHash(root, id)
	if reason := engine.CheckInterrogateClean(verified, hash, id); !strings.Contains(reason, "F-001 reproduced and not fixed") {
		t.Fatalf("an open reproduced finding must hold the gate, got %q", reason)
	}
	if _, err := executeInterrogateVerify(root, id, 30, io.Discard); err == nil {
		t.Fatal("a second verify must point to --recheck")
	}
}

func TestInterrogateRecheckAndWaive(t *testing.T) {
	root, id, run := newInterrogateFixture(t)
	a1, a2 := run.Adversaries[0], run.Adversaries[1]
	_ = os.MkdirAll(filepath.Join(a1.Copy, "repro"), 0755)
	_ = os.WriteFile(filepath.Join(a1.Copy, "repro", "zero.sh"), []byte(reproScript), 0755)
	writeReport(t, root, id, a1, []map[string]any{
		{"title": "allow(0) is true", "command": "sh repro/zero.sh", "repro_files": []string{"repro/zero.sh"}},
	})
	writeReport(t, root, id, a2, []map[string]any{
		{"title": "always fails", "command": "sh -c 'exit 3'"},
	})
	if _, err := executeInterrogateVerify(root, id, 30, io.Discard); err != nil {
		t.Fatal(err)
	}

	// Fix the bug; the recheck turns F-001 fixed on the current code.
	_ = os.WriteFile(filepath.Join(root, "src", "relay.py"), []byte(fixedCode), 0644)
	rechecked, err := executeInterrogateRecheck(root, id, 30, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if rechecked.Findings[0].Status != state.InterrogateFixed || rechecked.Findings[1].Status != state.InterrogateReproduced {
		t.Fatalf("F-001 fixed and F-002 still open, got %s and %s", rechecked.Findings[0].Status, rechecked.Findings[1].Status)
	}

	if err := executeInterrogateWaive(root, id, "F-001", "anything"); err == nil {
		t.Fatal("only a reproduced finding can be waived")
	}
	if err := executeInterrogateWaive(root, id, "F-002", "the user accepts it until the next release"); err != nil {
		t.Fatal(err)
	}
	hash, _ := engine.CodeHash(root, id)
	final, _ := state.LoadInterrogateRun(root, id)
	if reason := engine.CheckInterrogateClean(final, hash, id); reason != "" {
		t.Fatalf("fixed and waived findings meet the gate: %s", reason)
	}

	// A later code change makes the fix's recheck stale.
	_ = os.WriteFile(filepath.Join(root, "src", "relay.py"), []byte(fixedCode+"\n# later edit\n"), 0644)
	newHash, _ := engine.CodeHash(root, id)
	if reason := engine.CheckInterrogateClean(final, newHash, id); !strings.Contains(reason, "the code changed after F-001 was rechecked") {
		t.Fatalf("a recheck on older code must be stale, got %q", reason)
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

func TestCheckInterrogateClean_NoRunOrUnverified(t *testing.T) {
	if reason := engine.CheckInterrogateClean(nil, "", "r-1"); !strings.Contains(reason, "No interrogate run") {
		t.Fatalf("got %q", reason)
	}
	if reason := engine.CheckInterrogateClean(&state.InterrogateRun{PreparedAt: "x"}, "", "r-1"); !strings.Contains(reason, "not verified") {
		t.Fatalf("got %q", reason)
	}
}
