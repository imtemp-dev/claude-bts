package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

// Trial problem #125: the implement gates take an override, pinned to the
// code as it is, so a user who decides to finish without a remaining step
// has a recorded way to do it.  It is about the code, so --doc is refused.
func TestOverrideGrant_PinsAnImplementGateToTheCode(t *testing.T) {
	root, id := newTestRunFixture(t)
	if err := os.MkdirAll(filepath.Join(root, "src"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "a.go"), []byte("package a\n"), 0644); err != nil {
		t.Fatal(err)
	}
	tasks, _ := json.Marshal(&state.TaskState{RecipeID: id, Tasks: []state.Task{
		{ID: "t-001", File: "src/a.go", Action: "create", Status: "done", Description: "x"},
	}})
	if err := os.WriteFile(filepath.Join(state.RecipeDir(root, id), "tasks.json"), tasks, 0644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	if err := runQuiet(t, recipeOverrideGrantCmd, "recipe", "override", "grant", id,
		"--gate", "interrogate_clean", "--no-findings",
		"--reason", "the user decided to finish without the interrogate round"); err != nil {
		t.Fatal(err)
	}
	recs, _ := state.ReadOverrides(root, id)
	want, _ := state.RecipeImplHash(root, id)
	if len(recs) != 1 || recs[0].Doc != "(code)" || recs[0].DocHash == "" || recs[0].DocHash != want {
		t.Fatalf("the override must be pinned to the code's fingerprint %q, got %+v", want, recs)
	}
	if err := runQuiet(t, recipeOverrideGrantCmd, "recipe", "override", "grant", id,
		"--gate", "app_proven", "--doc", "final.md", "--no-findings", "--reason", "x"); err == nil || !strings.Contains(err.Error(), "code") {
		t.Fatalf("an implement gate is about the code, so --doc must be refused; got %v", err)
	}
}
