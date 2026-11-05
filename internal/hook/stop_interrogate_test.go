package hook

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

// IMPLEMENT DONE needs a verified interrogate run with nothing open, for
// every recipe type (prove stage, decision 5).
func TestImplementDone_NeedsACleanInterrogateRun(t *testing.T) {
	root, id := setupImplementRoot(t)
	removeInterrogate(t, root, id)

	out, err := NewStopHandler().Handle(&HookInput{CWD: root, LastAssistantMessage: "<bts>IMPLEMENT DONE</bts>"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Decision != "block" || !strings.Contains(out.Reason, "No interrogate run") {
		t.Fatalf("no interrogate run must block, got %q %q", out.Decision, out.Reason)
	}

	run := &state.InterrogateRun{PreparedAt: "t", VerifiedAt: "t", Findings: []*state.InterrogateFinding{
		{ID: "F-001", Status: state.InterrogateReproduced, Title: "x"},
	}}
	_ = state.SaveInterrogateRun(root, id, run)
	out, _ = NewStopHandler().Handle(&HookInput{CWD: root, LastAssistantMessage: "<bts>IMPLEMENT DONE</bts>"})
	if out.Decision != "block" || !strings.Contains(out.Reason, "F-001 reproduced and not fixed") {
		t.Fatalf("an open finding must block, got %q %q", out.Decision, out.Reason)
	}

	run.Findings[0].Status, run.Findings[0].WaiveReason = state.InterrogateWaived, "user accepted"
	_ = state.SaveInterrogateRun(root, id, run)
	out, _ = NewStopHandler().Handle(&HookInput{CWD: root, LastAssistantMessage: "<bts>IMPLEMENT DONE</bts>"})
	if out.Decision == "block" {
		t.Fatalf("a waived finding meets the gate: %s", out.Reason)
	}
}

func removeInterrogate(t *testing.T, root, id string) {
	t.Helper()
	_ = removeFile(state.InterrogatePath(root, id))
}

func removeFile(p string) error { return os.Remove(p) }

// Trial problem #52: a clean interrogate run used to never go stale. The
// run now records the code its adversaries copied; at IMPLEMENT DONE a
// change past the rule (a new code file, or more than two changed code
// files) needs a scoped round, and a small one keeps the cheap path.
func TestImplementDone_ALargeChangeAfterInterrogateNeedsARescope(t *testing.T) {
	root, id := setupImplementRoot(t)
	dir := state.RecipeDir(root, id)
	files := []string{"src/a.go", "src/b.go", "src/c.go", "src/d.go", "src/a_test.go", "docs/notes.md"}
	writeTasks := func(paths []string) {
		t.Helper()
		ts := &state.TaskState{RecipeID: id}
		for i, p := range paths {
			ts.Tasks = append(ts.Tasks, state.Task{ID: fmt.Sprintf("t-%03d", i+1), File: p, Action: "create", Status: "done", Description: "x"})
		}
		data, _ := json.MarshalIndent(ts, "", "  ")
		if err := os.WriteFile(filepath.Join(dir, "tasks.json"), data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	put := func(rel, body string) {
		t.Helper()
		_ = os.MkdirAll(filepath.Dir(filepath.Join(root, rel)), 0755)
		if err := os.WriteFile(filepath.Join(root, rel), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	writeTasks(files)
	for _, f := range files {
		put(f, "v1 "+f+"\n")
	}

	// interrogate.json as `bts interrogate prepare` writes it: the content
	// hash of each code file the adversaries copied. Raw JSON, so the test
	// reads the same on a bts that does not know the field yet.
	seen := map[string]string{}
	for _, f := range files {
		h, _, err := state.FileContentHash(filepath.Join(root, f))
		if err != nil {
			t.Fatal(err)
		}
		seen[f] = h
	}
	data, _ := json.Marshal(map[string]any{
		"adversaries": []any{}, "prepared_at": "t", "verified_at": "t", "round": 1,
		"seen": map[string]any{"files": seen},
	})
	if err := os.WriteFile(state.InterrogatePath(root, id), data, 0644); err != nil {
		t.Fatal(err)
	}
	done := func() *HookOutput {
		t.Helper()
		out, err := NewStopHandler().Handle(&HookInput{CWD: root, LastAssistantMessage: "<bts>IMPLEMENT DONE</bts>"})
		if err != nil {
			t.Fatal(err)
		}
		// A pass completes the recipe; reopen it for the next step.
		r, _ := state.LoadRecipeState(root, id)
		r.Phase = "implement"
		_ = state.SaveRecipeState(root, r)
		return out
	}
	if out := done(); out.Decision == "block" {
		t.Fatalf("unchanged code passes: %s", out.Reason)
	}

	// Small: two code files, a test and a doc changed. Cheap path.
	for _, f := range []string{"src/a.go", "src/b.go", "src/a_test.go", "docs/notes.md"} {
		put(f, "v2 "+f+"\n")
	}
	if out := done(); out.Decision == "block" {
		t.Fatalf("two changed code files keep the cheap path: %s", out.Reason)
	}

	// Large: a third code file changed.
	put("src/c.go", "v2 src/c.go\n")
	out := done()
	if out.Decision != "block" || !strings.Contains(out.Reason, "3 changed code files") || !strings.Contains(out.Reason, "--rescope") {
		t.Fatalf("three changed code files must ask for a scoped interrogate, got %q %q", out.Decision, out.Reason)
	}

	// A new code file the adversaries never saw needs one too.
	put("src/c.go", "v1 src/c.go\n")
	writeTasks(append(files, "src/e.go"))
	put("src/e.go", "new\n")
	if out := done(); out.Decision != "block" || !strings.Contains(out.Reason, "src/e.go") {
		t.Fatalf("a new code file must ask for a scoped interrogate, got %q %q", out.Decision, out.Reason)
	}
}
