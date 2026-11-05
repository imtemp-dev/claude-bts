package cli

import (
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

// No command moved a task's status (trial problem #75, Multica AGEN-57).
// The implement skill had the agent edit tasks.json by hand, and `bts
// check task` rewrites the file after each run, so every hand edit needed
// a fresh read first.  `bts task status` makes the move.
func TestExecuteTaskStatus(t *testing.T) {
	root, id := newTestRunFixture(t)
	if err := persistTasks(root, id, &state.TaskState{RecipeID: id, Tasks: []state.Task{
		{ID: "t-001", File: "src/a.py", Action: "create", Status: "pending", LastError: "build: x", RetryCount: 3, AttemptsInTier: 2, RetryTier: 2},
		{ID: "t-002", File: "src/b.py", Action: "create", Status: "done"},
	}}); err != nil {
		t.Fatal(err)
	}
	task := func(tid string) state.Task {
		t.Helper()
		ts, err := state.LoadTaskState(root, id)
		if err != nil {
			t.Fatal(err)
		}
		for _, k := range ts.Tasks {
			if k.ID == tid {
				return k
			}
		}
		t.Fatalf("task %s gone", tid)
		return state.Task{}
	}

	for _, c := range []struct{ name, tid, status, errText, want string }{
		{"unknown status", "t-001", "finished", "", "pending, in_progress, done, blocked, skipped"},
		{"unknown task", "t-009", "done", "", "t-001, t-002"},
		{"an error on done", "t-001", "done", "boom", "--error"},
	} {
		if _, err := executeTaskStatus(root, id, c.tid, c.status, c.errText, false); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: want an error naming %q, got %v", c.name, c.want, err)
		}
	}
	if got := task("t-001"); got.Status != "pending" || got.LastError != "build: x" {
		t.Fatalf("a refused move must change nothing: %+v", got)
	}

	from, err := executeTaskStatus(root, id, "t-001", "in_progress", "", false)
	if err != nil || from != "pending" || task("t-001").Status != "in_progress" {
		t.Fatalf("pending → in_progress: from %q, %v, %+v", from, err, task("t-001"))
	}
	if _, err := executeTaskStatus(root, id, "t-001", "blocked", "scope_expansion: needs Foo", false); err != nil {
		t.Fatal(err)
	}
	if got := task("t-001"); got.Status != "blocked" || got.LastError != "scope_expansion: needs Foo" {
		t.Fatalf("blocked with its error: %+v", got)
	}
	if _, err := executeTaskStatus(root, id, "t-001", "pending", "", true); err != nil {
		t.Fatal(err)
	}
	if got := task("t-001"); got.Status != "pending" || got.RetryCount != 0 || got.AttemptsInTier != 0 || got.RetryTier != 0 || got.LastError != "scope_expansion: needs Foo" {
		t.Fatalf("--reset-retries clears the retry budget and keeps the error: %+v", got)
	}
	if _, err := executeTaskStatus(root, id, "t-001", "done", "", false); err != nil {
		t.Fatal(err)
	}
	if got := task("t-001"); got.Status != "done" || got.LastError != "" {
		t.Fatalf("done clears the last error: %+v", got)
	}
	if got := task("t-002"); got.Status != "done" || got.File != "src/b.py" {
		t.Fatalf("other tasks are untouched: %+v", got)
	}
}
