package cli

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

func newTestRunFixture(t *testing.T) (root, recipeID string) {
	t.Helper()
	root = t.TempDir()
	recipeID = "r-777-test-run"
	dir := filepath.Join(root, ".bts", "specs", "recipes", recipeID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".bts", "local"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := state.SaveRecipeState(root, &state.RecipeState{
		ID: recipeID, Type: "blueprint", Phase: "test",
	}); err != nil {
		t.Fatal(err)
	}
	return root, recipeID
}

func TestExecuteTestRun_PassFromExitZero(t *testing.T) {
	root, id := newTestRunFixture(t)
	tr, err := executeTestRun(root, id, "echo all good; exit 0", "go", 30, testCounts{}, io.Discard)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if tr.Status != "pass" || tr.ExitCode != 0 || tr.RecordedBy != "bts" || tr.Iterations != 1 {
		t.Fatalf("unexpected result: %+v", tr)
	}
	// Persisted file must round-trip identically.
	loaded, err := state.LoadTestResults(root, id)
	if err != nil || loaded.Status != "pass" || loaded.RecordedBy != "bts" {
		t.Fatalf("persisted results wrong: %+v err=%v", loaded, err)
	}
}

// The run records which code it judged, so the stop gate can refuse a
// pass once a task file changes. The hash is taken by the CLI, never
// transcribed by the orchestrator.
func TestExecuteTestRun_RecordsImplHash(t *testing.T) {
	root, id := newTestRunFixture(t)
	if err := os.MkdirAll(filepath.Join(root, "src"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "a.go"), []byte("package a\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := state.WriteJSON(filepath.Join(state.RecipeDir(root, id), "tasks.json"), &state.TaskState{
		RecipeID: id,
		Tasks:    []state.Task{{ID: "t-001", File: "src/a.go", Action: "create", Status: "done"}},
	}); err != nil {
		t.Fatal(err)
	}

	tr, err := executeTestRun(root, id, "exit 0", "go", 30, testCounts{}, io.Discard)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	want, _ := state.RecipeImplHash(root, id)
	if want == "" || tr.ImplHash != want {
		t.Fatalf("impl_hash = %q, want %q", tr.ImplHash, want)
	}
	if loaded, err := state.LoadTestResults(root, id); err != nil || loaded.ImplHash != want {
		t.Fatalf("persisted impl_hash wrong: %+v err=%v", loaded, err)
	}
}

func TestExecuteTestRun_FailFromNonzeroExit(t *testing.T) {
	root, id := newTestRunFixture(t)
	tr, err := executeTestRun(root, id, "echo boom >&2; exit 3", "", 30, testCounts{}, io.Discard)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if tr.Status != "fail" || tr.ExitCode != 3 {
		t.Fatalf("expected fail/3, got %+v", tr)
	}
}

func TestExecuteTestRun_CommandNotFound(t *testing.T) {
	root, id := newTestRunFixture(t)
	tr, err := executeTestRun(root, id, "definitely-not-a-command-xyz-123", "", 30, testCounts{}, io.Discard)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if tr.Status != "fail" || tr.ExitCode != 127 {
		t.Fatalf("expected fail/127 for missing command, got %+v", tr)
	}
}

func TestExecuteTestRun_TimeoutKillsAndFails(t *testing.T) {
	root, id := newTestRunFixture(t)
	start := time.Now()
	tr, err := executeTestRun(root, id, "sleep 30", "", 1, testCounts{}, io.Discard)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if tr.Status != "fail" {
		t.Fatalf("timeout must fail, got %+v", tr)
	}
	if elapsed > 10*time.Second {
		t.Fatalf("process not killed promptly: %v", elapsed)
	}
	// Timeout note must land in the output log.
	logData, err := os.ReadFile(filepath.Join(state.LocalPath(root), "recipes", id, "test-output.log"))
	if err != nil || !strings.Contains(string(logData), "TIMEOUT") {
		t.Fatalf("output log missing timeout note: %v %q", err, logData)
	}
}

func TestExecuteTestRun_CountsRecordedButStatusNotOverridable(t *testing.T) {
	root, id := newTestRunFixture(t)
	// Caller claims 5/5 passed — but the exit code says fail. Status
	// must come from the exit code, counts are informational only.
	tr, err := executeTestRun(root, id, "exit 1", "jest", 30,
		testCounts{Total: 5, Passed: 5}, io.Discard)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if tr.Status != "fail" {
		t.Fatalf("counts must not override exit-code status: %+v", tr)
	}
	if tr.Total != 5 || tr.Passed != 5 {
		t.Fatalf("counts should still be recorded: %+v", tr)
	}
}

func TestExecuteTestRun_IterationsIncrement(t *testing.T) {
	root, id := newTestRunFixture(t)
	if _, err := executeTestRun(root, id, "exit 1", "", 30, testCounts{}, io.Discard); err != nil {
		t.Fatal(err)
	}
	tr, err := executeTestRun(root, id, "exit 0", "", 30, testCounts{}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if tr.Iterations != 2 || tr.Status != "pass" {
		t.Fatalf("expected iteration 2 pass, got %+v", tr)
	}
}

func TestExecuteTestRun_OutputLogAndChangelog(t *testing.T) {
	root, id := newTestRunFixture(t)
	if _, err := executeTestRun(root, id, "echo MARKER-OUTPUT-42", "", 30, testCounts{}, io.Discard); err != nil {
		t.Fatal(err)
	}
	logData, err := os.ReadFile(filepath.Join(state.LocalPath(root), "recipes", id, "test-output.log"))
	if err != nil || !strings.Contains(string(logData), "MARKER-OUTPUT-42") {
		t.Fatalf("output log missing command output: %v %q", err, logData)
	}
	entries, err := state.ReadChangelog(root, id)
	if err != nil || len(entries) == 0 {
		t.Fatalf("changelog: %v", err)
	}
	last := entries[len(entries)-1]
	if last.Action != "test" || !strings.Contains(last.Result, "status=pass") {
		t.Fatalf("changelog entry wrong: %+v", last)
	}
}

func TestExecuteTestRun_UnknownRecipeErrors(t *testing.T) {
	root, _ := newTestRunFixture(t)
	if _, err := executeTestRun(root, "r-does-not-exist", "exit 0", "", 30, testCounts{}, io.Discard); err == nil {
		t.Fatal("expected error for unknown recipe")
	}
}

func TestExecuteTestRun_EmptyCmdErrors(t *testing.T) {
	root, id := newTestRunFixture(t)
	if _, err := executeTestRun(root, id, "", "", 30, testCounts{}, io.Discard); err == nil {
		t.Fatal("expected error for empty --cmd")
	}
}
