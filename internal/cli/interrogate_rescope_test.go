package cli

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/engine"
	"github.com/imtemp-dev/claude-bts/internal/state"
)

// Trial problem #52: after a large change a verified run takes a scoped
// round, one seat on the files changed since; its findings join the run's,
// and once it is verified the gate reads the change as attacked.
func TestInterrogateRescope_AttacksTheChangeInANewRound(t *testing.T) {
	root, id, run := newInterrogateFixture(t)
	if run.Seen == nil || run.Seen.Files["src/relay.py"] == "" || run.CurrentRound() != 1 {
		t.Fatalf("prepare must record the code the seats copied, got %+v round %d", run.Seen, run.CurrentRound())
	}
	if _, err := executeInterrogateRescope(root, id, 1); err == nil || !strings.Contains(err.Error(), "not verified") {
		t.Fatalf("an unverified round cannot be rescoped, got %v", err)
	}
	a1, a2 := run.Adversaries[0], run.Adversaries[1]
	writeReport(t, root, id, a1, []map[string]any{{"title": "always fails", "command": "sh -c 'exit 3'"}})
	writeReport(t, root, id, a2, nil)
	if _, err := executeInterrogateVerify(root, id, 30, io.Discard); err != nil {
		t.Fatal(err)
	}
	if err := executeInterrogateWaive(root, id, "F-001", "the user accepts it"); err != nil {
		t.Fatal(err)
	}
	if _, err := executeInterrogateRescope(root, id, 1); err == nil || !strings.Contains(err.Error(), "no rescope is needed") {
		t.Fatalf("nothing changed, so no rescope, got %v", err)
	}

	// A large change: a new code file the adversaries never saw.
	dir := state.RecipeDir(root, id)
	_ = os.WriteFile(filepath.Join(dir, "wireframe.md"), []byte("## File Structure\n\n| # | File | Action | Depends On | Responsibility |\n"+
		"|---|---|---|---|---|\n| 1 | `src/relay.py` | create | — | the relay |\n| 2 | `src/retry.py` | create | — | retries |\n"), 0644)
	_ = os.WriteFile(filepath.Join(root, "src", "retry.py"), []byte("def retry(): pass\n"), 0644)
	loaded, _ := state.LoadInterrogateRun(root, id)
	change, ok, err := engine.InterrogateDrift(root, id, loaded)
	if err != nil || !ok {
		t.Fatalf("drift: %v %v", ok, err)
	}
	if v := engine.CheckInterrogateRescope(change, loaded, 2, id); !strings.Contains(v.Block, "src/retry.py") {
		t.Fatalf("a new code file must ask for a rescope, got %+v", v)
	}

	rescoped, err := executeInterrogateRescope(root, id, 1)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, a := range rescoped.Adversaries {
			_ = os.RemoveAll(a.Copy)
		}
	})
	if rescoped.CurrentRound() != 2 || rescoped.VerifiedAt != "" || !reflect.DeepEqual(rescoped.Scope, []string{"src/retry.py"}) {
		t.Fatalf("round 2, unverified, scoped to the new file; got round %d verified %q scope %v",
			rescoped.CurrentRound(), rescoped.VerifiedAt, rescoped.Scope)
	}
	var seats []*state.Adversary
	for _, a := range rescoped.Adversaries {
		if a.InRound(2) {
			seats = append(seats, a)
		}
	}
	if len(seats) != 1 || seats[0].Name != "a3" || seats[0].Angle != rescopeAngle {
		t.Fatalf("one seat a3 on the change, got %+v", seats)
	}
	if got, err := os.ReadFile(filepath.Join(seats[0].Copy, "src", "retry.py")); err != nil || len(got) == 0 {
		t.Fatalf("the seat's copy holds the changed code: %v", err)
	}
	hash, _ := engine.CodeHash(root, id)
	if reason := engine.CheckInterrogateClean(rescoped, hash, id); !strings.Contains(reason, "not verified") {
		t.Fatalf("the new round holds the gate until verified, got %q", reason)
	}

	// Verify reads only the new seat's report: round 1's are gone.
	_ = os.Remove(filepath.Join(dir, filepath.FromSlash(a1.Report)))
	_ = os.Remove(filepath.Join(dir, filepath.FromSlash(a2.Report)))
	writeReport(t, root, id, seats[0], []map[string]any{{"title": "passes now", "command": "true"}})
	verified, err := executeInterrogateVerify(root, id, 30, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(verified.Findings); n != 2 || verified.Findings[1].ID != "F-002" || verified.Findings[0].Status != state.InterrogateWaived {
		t.Fatalf("round 2's finding is F-002 after round 1's, which keeps its status; got %+v", verified.Findings)
	}
	change, ok, err = engine.InterrogateDrift(root, id, verified)
	if err != nil || !ok || engine.CheckInterrogateRescope(change, verified, 2, id) != (engine.RescopeVerdict{}) {
		t.Fatalf("after the scoped round the change counts as attacked, got %+v %v %v", change, ok, err)
	}
	if reason := engine.CheckInterrogateClean(verified, hash, id); reason != "" {
		t.Fatalf("a waived and a dropped finding meet the gate: %s", reason)
	}
}

// Trial problems #139 and #140, through the commands: in a git repository
// a comment-only change is no reason for a scoped round, and a scoped
// round names the changed lines of a changed file.
func TestInterrogateRescope_NamesTheChangedLines(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root, id := newBuildFixture(t)
	code := "def allow(x):\n    # zero is not allowed\n    return x > 0\n\n\ndef deny(x):\n    return not allow(x)\n"
	if err := os.WriteFile(filepath.Join(root, "src", "relay.py"), []byte(code), 0644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-q"}, {"config", "user.email", "t@example.com"}, {"config", "user.name", "t"},
		{"add", "-A"}, {"commit", "-q", "-m", "base"}} {
		if out, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
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
	for _, a := range run.Adversaries {
		writeReport(t, root, id, a, nil)
	}
	if _, err := executeInterrogateVerify(root, id, 30, io.Discard); err != nil {
		t.Fatal(err)
	}

	commented := strings.Replace(code, "# zero is not allowed", "# zero and below are refused", 1)
	_ = os.WriteFile(filepath.Join(root, "src", "relay.py"), []byte(commented), 0644)
	if _, err := executeInterrogateRescope(root, id, 1); err == nil || !strings.Contains(err.Error(), "comments only in src/relay.py") {
		t.Fatalf("a comment-only change needs no scoped round, got %v", err)
	}

	_ = os.WriteFile(filepath.Join(root, "src", "relay.py"), []byte(strings.Replace(commented, "return not allow(x)", "return x <= 0", 1)), 0644)
	rescoped, err := executeInterrogateRescope(root, id, 1)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, a := range rescoped.Adversaries {
			_ = os.RemoveAll(a.Copy)
		}
	})
	if got := rescoped.ScopeLines["src/relay.py"]; got != "7" {
		t.Fatalf("the scope names the changed code line, not the comment: got %q", got)
	}
}

// The rescope rule: a new code file, or more than two changed code files.
func TestCheckInterrogateRescope_TheRule(t *testing.T) {
	first := &state.InterrogateRun{Round: 1}
	small := state.CodeChange{Changed: []string{"a.go", "b.go"}}
	if v := engine.CheckInterrogateRescope(small, first, 2, "r-1"); v != (engine.RescopeVerdict{}) {
		t.Fatalf("two changed files keep the cheap path, got %+v", v)
	}
	large := state.CodeChange{Changed: []string{"a.go", "b.go", "c.go"}}
	if v := engine.CheckInterrogateRescope(large, first, 2, "r-1"); !strings.Contains(v.Block, "bts interrogate prepare r-1 --rescope") {
		t.Fatalf("three changed files need a rescope, got %+v", v)
	}
	added := state.CodeChange{Added: []string{"n.go"}}
	if v := engine.CheckInterrogateRescope(added, first, 2, "r-1"); !strings.Contains(v.Block, "n.go") {
		t.Fatalf("a new file needs a rescope, got %+v", v)
	}
	aside := state.CodeChange{Changed: []string{"a.go", "b.go", "c.go"}, CommentOnly: []string{"d.go"}, Upstream: []string{"e.go"}}
	if v := engine.CheckInterrogateRescope(aside, first, 2, "r-1"); !strings.Contains(v.Block, "comments only: d.go") || !strings.Contains(v.Block, "changed only upstream: e.go") {
		t.Fatalf("the message names the files it did not count, got %+v", v)
	}
}

// Trial problem #140: once a run has held interrogate.max_rescope_rounds
// scoped rounds, a large change no longer requires another; the gate says
// the budget let it through.
func TestCheckInterrogateRescope_BudgetEndsTheRounds(t *testing.T) {
	large := state.CodeChange{Changed: []string{"a.go", "b.go", "c.go"}}
	for round, wantBlock := range map[int]bool{1: true, 2: true, 3: false, 7: false} {
		v := engine.CheckInterrogateRescope(large, &state.InterrogateRun{Round: round}, 2, "r-1")
		if wantBlock && (v.Block == "" || v.Notice != "") {
			t.Errorf("round %d, %d scoped rounds held of 2: a large change needs a scoped round, got %+v", round, round-1, v)
		}
		if !wantBlock && (v.Block != "" || !strings.Contains(v.Notice, "interrogate.max_rescope_rounds: 2")) {
			t.Errorf("round %d: the budget is spent, so the gate passes with a notice, got %+v", round, v)
		}
	}
	if v := engine.CheckInterrogateRescope(large, &state.InterrogateRun{Round: 3}, 5, "r-1"); v.Block == "" {
		t.Errorf("a larger budget keeps asking, got %+v", v)
	}
}
