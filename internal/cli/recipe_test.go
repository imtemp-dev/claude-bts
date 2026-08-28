package cli

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imtemp-dev/jig/internal/state"
)

// newRecipeFixture builds a minimal project tree — `.jig/specs/recipes/
// {id}/` with recipe.json + verify-log.jsonl — ready for reconcile
// tests. Returns the project root so callers can pass it to
// reconcileRecipe directly.
func newRecipeFixture(t *testing.T, recipeID, phase string, level float64, iter int, entries []state.VerifyLogEntry) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, ".jig", "specs", "recipes", recipeID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".jig", "local"), 0755); err != nil {
		t.Fatalf("mkdir local: %v", err)
	}
	_ = os.MkdirAll(filepath.Join(root, ".jig", "config"), 0755)
	_ = os.WriteFile(filepath.Join(root, ".jig", "config", ".template-version"), []byte("test"), 0644)
	t.Setenv("HOME", t.TempDir())

	recipe := &state.RecipeState{
		ID:        recipeID,
		Type:      "spec",
		Phase:     phase,
		Level:     level,
		Iteration: iter,
	}
	if err := state.SaveRecipeState(root, recipe); err != nil {
		t.Fatalf("save recipe: %v", err)
	}

	path := filepath.Join(dir, "verify-log.jsonl")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create log: %v", err)
	}
	enc := json.NewEncoder(f)
	for i := range entries {
		if err := enc.Encode(&entries[i]); err != nil {
			t.Fatalf("encode: %v", err)
		}
	}
	_ = f.Close()
	return root
}

// Blueprint-phase + converged verify-log → reconcile promotes to finalize.
func TestReconcile_BlueprintPhase_Converged_Applies(t *testing.T) {
	root := newRecipeFixture(t, "r-001", "simulate", 0.0, 0, []state.VerifyLogEntry{
		{Iteration: 1, Critical: 1, Major: 0, Status: "continue"},
		{Iteration: 3, Critical: 0, Major: 0, Status: "converged"},
	})

	plan, err := reconcileRecipe(root, "r-001", reconcileOpts{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if plan.ToPhase != "finalize" || plan.ToLevel != 3.0 || plan.ToIteration != 3 {
		t.Errorf("plan wrong: %+v", plan)
	}

	// Verify persistence
	after, _ := state.LoadRecipeState(root, "r-001")
	if after.Phase != "finalize" || after.Level != 3.0 || after.Iteration != 3 {
		t.Errorf("recipe.json not updated: %+v", after)
	}

	// Backup exists
	if _, err := os.Stat(filepath.Join(state.RecipeDir(root, "r-001"), "recipe.json.bak")); err != nil {
		t.Errorf("recipe.json.bak missing: %v", err)
	}

	// Changelog has the reconcile marker
	clData, _ := os.ReadFile(filepath.Join(state.RecipeDir(root, "r-001"), "changelog.jsonl"))
	if !strings.Contains(string(clData), "reconciled from phase=simulate") {
		t.Errorf("changelog missing reconcile entry: %s", clData)
	}
}

// Implement-phase must never reconcile, even with --force.
func TestReconcile_ImplementPhase_Blocked(t *testing.T) {
	root := newRecipeFixture(t, "r-002", "test", 3.0, 3, []state.VerifyLogEntry{
		{Iteration: 3, Critical: 0, Major: 0, Status: "converged"},
	})

	// Plain call → blocked
	_, err := reconcileRecipe(root, "r-002", reconcileOpts{})
	if !errors.Is(err, ErrProtectedPhase) {
		t.Errorf("want ErrProtectedPhase, got %v", err)
	}

	// --force must also be blocked for implement-phase (hardcoded safety)
	_, err = reconcileRecipe(root, "r-002", reconcileOpts{force: true})
	if !errors.Is(err, ErrProtectedPhase) {
		t.Errorf("want ErrProtectedPhase even with --force, got %v", err)
	}
}

// Verify-log last entry not converged → refuse.
func TestReconcile_NotConverged_Refuses(t *testing.T) {
	root := newRecipeFixture(t, "r-003", "simulate", 0, 0, []state.VerifyLogEntry{
		{Iteration: 1, Critical: 0, Major: 1, Status: "continue"},
	})

	_, err := reconcileRecipe(root, "r-003", reconcileOpts{})
	if !errors.Is(err, ErrNotConverged) {
		t.Errorf("want ErrNotConverged, got %v", err)
	}

	// Resolvable > 0 also refuses
	root2 := newRecipeFixture(t, "r-003b", "simulate", 0, 0, []state.VerifyLogEntry{
		{Iteration: 1, Critical: 0, Major: 0, MinorResolvable: 1, Status: "continue"},
	})
	_, err = reconcileRecipe(root2, "r-003b", reconcileOpts{})
	if !errors.Is(err, ErrNotConverged) {
		t.Errorf("resolvable>0 should refuse, got %v", err)
	}
}

// Missing verify-log → refuse with ErrNoVerifyLog.
func TestReconcile_NoVerifyLog_Refuses(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".jig", "specs", "recipes", "r-004")
	_ = os.MkdirAll(dir, 0755)
	_ = os.MkdirAll(filepath.Join(root, ".jig", "local"), 0755)
	t.Setenv("HOME", t.TempDir())
	recipe := &state.RecipeState{ID: "r-004", Type: "spec", Phase: "simulate"}
	if err := state.SaveRecipeState(root, recipe); err != nil {
		t.Fatalf("save: %v", err)
	}

	_, err := reconcileRecipe(root, "r-004", reconcileOpts{})
	if !errors.Is(err, ErrNoVerifyLog) {
		t.Errorf("want ErrNoVerifyLog, got %v", err)
	}
}

// Already finalized → refuse, even with --force.
func TestReconcile_AlreadyFinalize_Refuses(t *testing.T) {
	root := newRecipeFixture(t, "r-005", "finalize", 3.0, 2, []state.VerifyLogEntry{
		{Iteration: 2, Critical: 0, Major: 0, Status: "converged"},
	})

	_, err := reconcileRecipe(root, "r-005", reconcileOpts{})
	if !errors.Is(err, ErrAlreadyFinal) {
		t.Errorf("want ErrAlreadyFinal, got %v", err)
	}
	_, err = reconcileRecipe(root, "r-005", reconcileOpts{force: true})
	if !errors.Is(err, ErrAlreadyFinal) {
		t.Errorf("force cannot re-finalize; want ErrAlreadyFinal, got %v", err)
	}
}

// Dry-run should return the plan without touching disk.
func TestReconcile_DryRun_NoWrite(t *testing.T) {
	root := newRecipeFixture(t, "r-006", "audit", 1.5, 1, []state.VerifyLogEntry{
		{Iteration: 2, Critical: 0, Major: 0, Status: "converged"},
	})

	plan, err := reconcileRecipe(root, "r-006", reconcileOpts{dryRun: true})
	if err != nil {
		t.Fatalf("dry-run failed: %v", err)
	}
	if plan.ToPhase != "finalize" {
		t.Errorf("plan wrong: %+v", plan)
	}
	// Backup must not exist
	if _, err := os.Stat(filepath.Join(state.RecipeDir(root, "r-006"), "recipe.json.bak")); !os.IsNotExist(err) {
		t.Errorf("dry-run wrote backup (should not): %v", err)
	}
	// Recipe state must remain untouched
	after, _ := state.LoadRecipeState(root, "r-006")
	if after.Phase != "audit" || after.Level != 1.5 || after.Iteration != 1 {
		t.Errorf("dry-run mutated state: %+v", after)
	}
}

// Iteration must be monotonic — if recipe.json already has a higher
// iteration than verify-log last entry (weird but legal), keep the
// higher value.
func TestReconcile_IterationMonotonic(t *testing.T) {
	root := newRecipeFixture(t, "r-007", "simulate", 0, 5, []state.VerifyLogEntry{
		{Iteration: 3, Critical: 0, Major: 0, Status: "converged"},
	})

	plan, err := reconcileRecipe(root, "r-007", reconcileOpts{})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if plan.ToIteration != 5 {
		t.Errorf("iteration must not regress; got %d want 5", plan.ToIteration)
	}
}

// --force enables reconciling a phase outside the spec whitelist
// (hypothetical future phases we haven't added yet).
func TestReconcile_ForceOverridesWhitelist(t *testing.T) {
	root := newRecipeFixture(t, "r-008", "custom-phase", 0, 1, []state.VerifyLogEntry{
		{Iteration: 1, Critical: 0, Major: 0, Status: "converged"},
	})

	// Without force → blocked
	_, err := reconcileRecipe(root, "r-008", reconcileOpts{})
	if !errors.Is(err, ErrProtectedPhase) {
		t.Errorf("want ErrProtectedPhase, got %v", err)
	}
	// With force → accepted
	plan, err := reconcileRecipe(root, "r-008", reconcileOpts{force: true})
	if err != nil {
		t.Fatalf("force should allow, got %v", err)
	}
	if plan.ToPhase != "finalize" {
		t.Errorf("plan wrong: %+v", plan)
	}
}

// The root cause of a measured recipe's missing doc_hash: `jig recipe
// log --doc draft.md` is run from the PROJECT ROOT, where draft.md does
// not exist — it lives in the recipe directory. FileContentHash returned
// ok=false with a nil error, stampContentHashes recorded nothing, and it
// said nothing, so both rule-3 gates and the completion replication
// check stood down for the rest of the run.
func TestStampContentHashes_ResolvesADocRelativeToTheRecipe(t *testing.T) {
	root := t.TempDir()
	recipeID := "r-001-test"
	dir := state.RecipeDir(root, recipeID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "draft.md"), []byte("# draft\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// The bare basename, exactly as the skills pass it.
	var entry state.VerifyLogEntry
	stampContentHashes(root, recipeID, "draft.md", &entry)
	if entry.DocHash == "" {
		t.Fatal("a bare --doc basename must resolve against the recipe directory")
	}

	// A project-relative path still works, and names the same content.
	rel := filepath.Join(".jig", "specs", "recipes", recipeID, "draft.md")
	var viaRel state.VerifyLogEntry
	stampContentHashes(root, recipeID, rel, &viaRel)
	if viaRel.DocHash != entry.DocHash {
		t.Errorf("the same file reached two ways gave %q and %q", entry.DocHash, viaRel.DocHash)
	}

	// A path that resolves nowhere records nothing — and must not do so
	// in silence, which is the part that made this invisible.
	var missing state.VerifyLogEntry
	stampContentHashes(root, recipeID, "nonexistent.md", &missing)
	if missing.DocHash != "" {
		t.Errorf("a missing document must not produce a hash, got %q", missing.DocHash)
	}
}
