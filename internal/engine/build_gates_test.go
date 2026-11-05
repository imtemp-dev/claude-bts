package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

func run(exit int, hash string) *state.CommandRun {
	return &state.CommandRun{Command: "x", ExitCode: exit, CodeHash: hash}
}

func TestCheckSkeletonBuilds(t *testing.T) {
	cases := []struct {
		name string
		ev   *state.BuildEvidence
		want string // substring; "" means pass
	}{
		{"nothing recorded", &state.BuildEvidence{}, "No skeleton build"},
		{"failed", &state.BuildEvidence{Skeleton: run(2, "h1")}, "failed (exit 2)"},
		{"stale", &state.BuildEvidence{Skeleton: run(0, "old")}, "changed after"},
		{"passes", &state.BuildEvidence{Skeleton: run(0, "h1")}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := CheckSkeletonBuilds(c.ev, "h1", "r-1")
			if c.want == "" && got != "" || c.want != "" && !strings.Contains(got, c.want) {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestCheckFalsifiersRed(t *testing.T) {
	invs := []string{"INV-001", "INV-002"}
	ev := &state.BuildEvidence{Falsifiers: map[string]*state.Falsifier{
		"INV-001": {Command: "t1", Red: run(1, "h1")},
	}}
	if got := CheckFalsifiersRed(ev, invs, "h1", "r-1"); !strings.Contains(got, "no falsifier recorded for INV-002") {
		t.Fatalf("an invariant with no falsifier must block, got %q", got)
	}

	ev.Falsifiers["INV-002"] = &state.Falsifier{Command: "t2", Red: run(0, "h1")}
	if got := CheckFalsifiersRed(ev, invs, "h1", "r-1"); !strings.Contains(got, "INV-002 passed on the skeleton") {
		t.Fatalf("a falsifier that passes on the skeleton must block, got %q", got)
	}

	ev.Falsifiers["INV-002"] = &state.Falsifier{Command: "t2", Red: run(1, "old")}
	if got := CheckFalsifiersRed(ev, invs, "h1", "r-1"); !strings.Contains(got, "changed after INV-002") {
		t.Fatalf("a red run on an older skeleton must block, got %q", got)
	}

	ev.Falsifiers["INV-002"] = &state.Falsifier{Command: "t2", Red: run(1, "h1")}
	if got := CheckFalsifiersRed(ev, invs, "h1", "r-1"); got != "" {
		t.Fatalf("all red on the current skeleton must pass, got %q", got)
	}
}

func TestCheckFalsifiersGreen(t *testing.T) {
	invs := []string{"INV-001", "INV-002"}
	ev := &state.BuildEvidence{Falsifiers: map[string]*state.Falsifier{
		"INV-001": {Command: "t1", Red: run(1, "s"), Green: run(0, "h2")},
		"INV-002": {Command: "t2", Red: run(1, "s"), Green: run(1, "h2")},
	}}
	if got := CheckFalsifiersGreen(ev, invs, "h2", "r-1"); !strings.Contains(got, "INV-002 not passing") {
		t.Fatalf("a failing falsifier must block, got %q", got)
	}
	ev.Falsifiers["INV-002"].Green = run(0, "h1")
	if got := CheckFalsifiersGreen(ev, invs, "h2", "r-1"); !strings.Contains(got, "changed after INV-002") {
		t.Fatalf("a green run on older code must block, got %q", got)
	}
	ev.Falsifiers["INV-002"].Green = run(0, "h2")
	if got := CheckFalsifiersGreen(ev, invs, "h2", "r-1"); got != "" {
		t.Fatalf("all green on the current code must pass, got %q", got)
	}
}

const buildWireframe = "## File Structure\n\n" +
	"| # | File | Action | Depends On | Responsibility |\n" +
	"|---|------|--------|------------|----------------|\n" +
	"| 1 | `src/relay.py` | create | — | owns the relay |\n" +
	"| 2 | `src/rules.py` | **modify** | 1 | pure filter rules |\n" +
	"| 3 | `README.md` | unchanged | — | documents it |\n"

func TestCodeHash_FallsBackToWireframeRowsBeforeTasks(t *testing.T) {
	root := t.TempDir()
	id := "r-1-build"
	dir := state.RecipeDir(root, id)
	if err := os.MkdirAll(filepath.Join(root, "src"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(dir, "wireframe.md"), []byte(buildWireframe), 0644)
	_ = os.WriteFile(filepath.Join(root, "src", "relay.py"), []byte("def handle(): raise NotImplementedError\n"), 0644)
	_ = os.WriteFile(filepath.Join(root, "src", "rules.py"), []byte("def accept(): raise NotImplementedError\n"), 0644)

	fromRows, err := CodeHash(root, id)
	if err != nil || fromRows == "" {
		t.Fatalf("rows must give a hash before tasks.json exists: %q, %v", fromRows, err)
	}

	// The unchanged row is not code the recipe writes.
	_ = os.WriteFile(filepath.Join(root, "README.md"), []byte("changed\n"), 0644)
	if again, _ := CodeHash(root, id); again != fromRows {
		t.Fatal("an unchanged row's file must not be part of the hash")
	}

	// The bold **modify** row counts.
	_ = os.WriteFile(filepath.Join(root, "src", "rules.py"), []byte("def accept(): return True\n"), 0644)
	if again, _ := CodeHash(root, id); again == fromRows {
		t.Fatal("editing a modify row's file must change the hash")
	}

	// Once tasks.json names the same files, it is the source and agrees.
	rowsNow, _ := CodeHash(root, id)
	if err := state.WriteJSON(filepath.Join(dir, "tasks.json"), &state.TaskState{RecipeID: id, Tasks: []state.Task{
		{ID: "t-1", File: "src/relay.py", Action: "create"},
		{ID: "t-2", File: "src/rules.py", Action: "modify"},
	}}); err != nil {
		t.Fatal(err)
	}
	if fromTasks, _ := CodeHash(root, id); fromTasks != rowsNow {
		t.Fatalf("tasks.json and the rows name the same files, so the hashes must agree: %q vs %q", fromTasks, rowsNow)
	}
}

func TestDomainInvariantIDs(t *testing.T) {
	p := filepath.Join(t.TempDir(), "domain.md")
	_ = os.WriteFile(p, []byte("# Domain\n\n## 2. Invariants\n\n| ID | Statement | Owner | Enforcement |\n|---|---|---|---|\n"+
		"| INV-001 | one PR per issue | relay | lookup |\n| INV-002 | never post with the user token | relay | client split |\n\n## 3. Next\n"), 0644)
	ids, err := DomainInvariantIDs(p)
	if err != nil || strings.Join(ids, ",") != "INV-001,INV-002" {
		t.Fatalf("got %v, %v", ids, err)
	}
	if _, err := DomainInvariantIDs(filepath.Join(t.TempDir(), "missing.md")); err == nil {
		t.Fatal("a missing domain.md must be an error")
	}
}

// Trial problem #53: the domain-model skill's example heads the section
// `### 2. Invariants`, which the parser did not see, so such a domain.md
// read as having no invariants and every invariant gate passed with nothing
// checked.  A level-3 heading now parses, ending at the next level-3
// heading, and a domain.md with no invariant row is an error.
func TestDomainInvariantIDs_LevelThreeHeadingAndNoRows(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "domain.md")
	_ = os.WriteFile(p, []byte("# Domain\n\n### 1. Entities\n\n| Entity | Definition |\n|---|---|\n| Relay | the bot |\n\n"+
		"### 2. Invariants (SINGLE OWNER per invariant)\n\n| ID | Statement | Owner | Enforcement point |\n|---|---|---|---|\n"+
		"| INV-013 | only a configured group counts | Rules | screen() |\n\n### 3. State Partitioning\n\n| INV-999 | not an invariant row | x | y |\n"), 0644)
	ids, err := DomainInvariantIDs(p)
	if err != nil || strings.Join(ids, ",") != "INV-013" {
		t.Fatalf("a `### 2. Invariants` section must parse and end at `### 3.`, got %v, %v", ids, err)
	}

	empty := filepath.Join(dir, "empty.md")
	_ = os.WriteFile(empty, []byte("# Domain\n\n## 1. Entities\n\n| Entity | Definition |\n|---|---|\n| Relay | the bot |\n"), 0644)
	if ids, err := DomainInvariantIDs(empty); err == nil {
		t.Fatalf("a domain.md with no invariant row must be an error, got %v", ids)
	}
}
