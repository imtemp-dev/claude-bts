package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imtemp-dev/jig/internal/state"
)

func writeOutcomeRecipe(t *testing.T, root, id string, opts func(dir string)) {
	t.Helper()
	dir := filepath.Join(root, ".jig", "specs", "recipes", id)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := state.SaveRecipeState(root, &state.RecipeState{
		ID: id, Type: "spec", Phase: "complete",
	}); err != nil {
		t.Fatal(err)
	}
	if opts != nil {
		opts(dir)
	}
}

func TestGatherOutcomes_FullRecipe(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".jig", "local"), 0755); err != nil {
		t.Fatal(err)
	}
	writeOutcomeRecipe(t, root, "r-001-full", func(dir string) {
		// 3 verify iterations, first with findings, converged at the end.
		vlog := `{"iteration":1,"critical":2,"major":3,"status":"continue"}
{"iteration":2,"critical":1,"major":0,"status":"continue"}
not json — must be skipped
{"iteration":3,"critical":0,"major":0,"status":"converged"}
`
		if err := os.WriteFile(filepath.Join(dir, "verify-log.jsonl"), []byte(vlog), 0644); err != nil {
			t.Fatal(err)
		}
		clog := `{"time":"t","action":"simulate","output":"simulations/001.md"}
{"time":"t","action":"improve"}
{"time":"t","action":"simulate","output":"simulations/002.md"}
`
		if err := os.WriteFile(filepath.Join(dir, "changelog.jsonl"), []byte(clog), 0644); err != nil {
			t.Fatal(err)
		}
		tasks := `{"recipe_id":"r-001-full","tasks":[
{"id":"t-1","file":"a.go","action":"create","status":"done","description":"a","retry_count":2},
{"id":"t-2","file":"b.go","action":"create","status":"blocked","description":"b","retry_count":5},
{"id":"t-3","file":"c.go","action":"create","status":"done","description":"c"}]}`
		if err := os.WriteFile(filepath.Join(dir, "tasks.json"), []byte(tasks), 0644); err != nil {
			t.Fatal(err)
		}
		tr := `{"recipe_id":"r-001-full","status":"pass","iterations":4,"recorded_by":"jig","exit_code":0}`
		if err := os.WriteFile(filepath.Join(dir, "test-results.json"), []byte(tr), 0644); err != nil {
			t.Fatal(err)
		}
		dev := "# Deviation Report\n\n## Deviations\n- swapped lib X for Y\n- endpoint renamed\n\nprose line\n- not-implemented: metrics hook\n"
		if err := os.WriteFile(filepath.Join(dir, "deviation.md"), []byte(dev), 0644); err != nil {
			t.Fatal(err)
		}
	})

	outs, err := gatherOutcomes(root)
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	if len(outs) != 1 {
		t.Fatalf("expected 1 outcome, got %d", len(outs))
	}
	o := outs[0]
	if o.VerifyIterations != 3 || o.FirstCritical != 2 || o.FirstMajor != 3 || o.FinalStatus != "converged" {
		t.Errorf("verify side wrong: %+v", o)
	}
	if o.SimulateRuns != 2 {
		t.Errorf("simulate runs: %d", o.SimulateRuns)
	}
	if o.Tasks != 3 || o.ImplRetries != 7 || o.BlockedTasks != 1 || !o.HasImplement {
		t.Errorf("implement side wrong: %+v", o)
	}
	if o.TestIterations != 4 || o.TestStatus != "pass" || o.TestRecordedBy != "jig" {
		t.Errorf("test side wrong: %+v", o)
	}
	if o.Deviations != 3 {
		t.Errorf("deviations: %d", o.Deviations)
	}
}

func TestGatherOutcomes_PartialAndEmptyRecipesDegrade(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".jig", "local"), 0755); err != nil {
		t.Fatal(err)
	}
	// Recipe with only a verify log — no changelog/tasks/tests/deviation.
	writeOutcomeRecipe(t, root, "r-002-partial", func(dir string) {
		vlog := `{"iteration":1,"critical":0,"major":0,"status":"converged"}` + "\n"
		if err := os.WriteFile(filepath.Join(dir, "verify-log.jsonl"), []byte(vlog), 0644); err != nil {
			t.Fatal(err)
		}
	})
	// Recipe with nothing at all.
	writeOutcomeRecipe(t, root, "r-003-empty", nil)

	outs, err := gatherOutcomes(root)
	if err != nil {
		t.Fatalf("gather must not fail on partial data: %v", err)
	}
	if len(outs) != 2 {
		t.Fatalf("expected 2 outcomes, got %d", len(outs))
	}
	for _, o := range outs {
		if o.HasImplement || o.ImplRetries != 0 || o.Deviations != 0 {
			t.Errorf("partial recipe should degrade to zeros: %+v", o)
		}
	}
}

func TestRenderOutcomes_SmallSampleCaveatAndHandRecorded(t *testing.T) {
	outs := []RecipeOutcome{
		{ID: "r-1", Type: "spec", Phase: "complete", VerifyIterations: 2,
			HasImplement: true, Tasks: 3, ImplRetries: 1,
			TestIterations: 1, TestStatus: "pass", TestRecordedBy: ""},
		{ID: "r-2", Type: "spec", Phase: "complete", VerifyIterations: 4,
			SimulateRuns: 1, HasImplement: true, Tasks: 5, ImplRetries: 9,
			TestIterations: 3, TestStatus: "pass", TestRecordedBy: "jig"},
	}
	out := renderOutcomes(outs)
	if !strings.Contains(out, "directional signal only") {
		t.Errorf("small sample must carry the honesty caveat:\n%s", out)
	}
	if !strings.Contains(out, "hand-recorded") {
		t.Errorf("non-jig test results must be labeled:\n%s", out)
	}
	if !strings.Contains(out, "verify ≤2 iterations: n=1") || !strings.Contains(out, "verify ≥3 iterations: n=1") {
		t.Errorf("grouped means missing:\n%s", out)
	}
}

func TestRenderOutcomes_NoRecipes(t *testing.T) {
	if out := renderOutcomes(nil); !strings.Contains(out, "No recipes") {
		t.Errorf("empty project message wrong: %s", out)
	}
}

func TestRenderOutcomes_NoImplementationYet(t *testing.T) {
	outs := []RecipeOutcome{{ID: "r-1", Type: "design", Phase: "finalize", VerifyIterations: 2}}
	if out := renderOutcomes(outs); !strings.Contains(out, "No recipes with implementation data") {
		t.Errorf("spec-only project message wrong: %s", out)
	}
}
