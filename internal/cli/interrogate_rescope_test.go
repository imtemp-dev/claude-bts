package cli

import (
	"io"
	"os"
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
	if reason := engine.CheckInterrogateRescope(change, id); !strings.Contains(reason, "src/retry.py") {
		t.Fatalf("a new code file must ask for a rescope, got %q", reason)
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
	if err != nil || !ok || engine.CheckInterrogateRescope(change, id) != "" {
		t.Fatalf("after the scoped round the change counts as attacked, got %+v %v %v", change, ok, err)
	}
	if reason := engine.CheckInterrogateClean(verified, hash, id); reason != "" {
		t.Fatalf("a waived and a dropped finding meet the gate: %s", reason)
	}
}

// The rescope rule: a new code file, or more than two changed code files.
func TestCheckInterrogateRescope_TheRule(t *testing.T) {
	small := state.CodeChange{Changed: []string{"a.go", "b.go"}}
	if reason := engine.CheckInterrogateRescope(small, "r-1"); reason != "" {
		t.Fatalf("two changed files keep the cheap path, got %q", reason)
	}
	large := state.CodeChange{Changed: []string{"a.go", "b.go", "c.go"}}
	if reason := engine.CheckInterrogateRescope(large, "r-1"); !strings.Contains(reason, "bts interrogate prepare r-1 --rescope") {
		t.Fatalf("three changed files need a rescope, got %q", reason)
	}
	added := state.CodeChange{Added: []string{"n.go"}}
	if reason := engine.CheckInterrogateRescope(added, "r-1"); !strings.Contains(reason, "n.go") {
		t.Fatalf("a new file needs a rescope, got %q", reason)
	}
}
