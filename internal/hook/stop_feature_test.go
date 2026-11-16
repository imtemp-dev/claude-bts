package hook

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/feature"
	"github.com/imtemp-dev/claude-bts/internal/state"
)

// The binary registers the feature observer in the cli package; hook tests
// register it once here.  It acts only on feature-bound recipes.
var registerFeatureObserver sync.Once

// A feature item's recipe completed by IMPLEMENT DONE through the Stop
// hook's gates reaches the store as done via the gate.  The cli tests set
// the phase themselves, so only this one proves stop.go passes the source.
func TestStop_FeatureItemCompletesThroughTheGate(t *testing.T) {
	registerFeatureObserver.Do(func() { state.AddPhaseObserver(feature.OnPhaseChange) })
	root, id := setupImplementRoot(t)
	setOwner(t, root, id, ownerSID)
	t.Setenv(ProjectEnv, root)
	t.Setenv(feature.StoreEnv, t.TempDir())

	dir, f, _, err := feature.Create(feature.CreateOptions{Title: "x", Repo: &feature.Repo{Key: "local/x-1"}})
	if err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(dir, feature.IntentFile), []byte("# Intent\n\nx\n"), 0644)
	title, build := "the fixture", "blueprint"
	i1, _ := feature.AddItem(dir, feature.ItemSpec{Title: &title, Type: &build})
	if _, err := feature.Confirm(dir, "go"); err != nil {
		t.Fatal(err)
	}
	// Bind the fixture's recipe as attempt a1, as recipe create --feature would.
	ad := filepath.Join(dir, feature.ItemsDir, i1, "a1")
	_ = os.MkdirAll(ad, 0755)
	if err := state.WriteJSON(filepath.Join(ad, feature.ClaimFile), &feature.Claim{
		Schema: feature.Schema, Feature: f.ID, FeatureDir: dir, Item: i1, Attempt: 1, Kind: "build", RecipeID: id, RecipeType: "blueprint",
	}); err != nil {
		t.Fatal(err)
	}
	r, _ := state.LoadRecipeState(root, id)
	r.Feature, r.FeatureItem, r.FeatureAttempt, r.FeatureDir = f.ID, i1, 1, dir
	if err := state.SaveRecipeState(root, r); err != nil {
		t.Fatal(err)
	}

	out, err := NewStopHandler().Handle(&HookInput{CWD: root, SessionID: ownerSID, LastAssistantMessage: "All green.\n<bts>IMPLEMENT DONE</bts>"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Decision == "block" {
		t.Fatalf("the marker must complete: %s", out.Reason)
	}
	var m feature.Manifest
	if err := state.ReadJSON(filepath.Join(ad, feature.SnapshotDir, feature.ManifestFile), &m); err != nil {
		t.Fatalf("the completion must reach the store: %v", err)
	}
	if m.Outcome != "done" || m.CompletedVia != "gate" {
		t.Fatalf("manifest: outcome %q via %q, want done via gate", m.Outcome, m.CompletedVia)
	}
}

// bts #143: what a turn wrote without a phase change reaches the store at
// the turn's end: a Multica worker's session may never start again in that
// worktree.
func TestStop_TurnEndSyncsTheFeatureStore(t *testing.T) {
	root, id := setupImplementRoot(t)
	setOwner(t, root, id, ownerSID)
	t.Setenv(ProjectEnv, root)
	t.Setenv(feature.StoreEnv, t.TempDir())

	dir, f, _, err := feature.Create(feature.CreateOptions{Title: "x", Repo: &feature.Repo{Key: "local/x-1"}})
	if err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(dir, feature.IntentFile), []byte("# Intent\n\nx\n"), 0644)
	title, build := "the fixture", "blueprint"
	i1, _ := feature.AddItem(dir, feature.ItemSpec{Title: &title, Type: &build})
	if _, err := feature.Confirm(dir, "go"); err != nil {
		t.Fatal(err)
	}
	ad := filepath.Join(dir, feature.ItemsDir, i1, "a1")
	_ = os.MkdirAll(ad, 0755)
	if err := state.WriteJSON(filepath.Join(ad, feature.ClaimFile), &feature.Claim{
		Schema: feature.Schema, Feature: f.ID, FeatureDir: dir, Item: i1, Attempt: 1, Kind: "build", RecipeID: id, RecipeType: "blueprint",
	}); err != nil {
		t.Fatal(err)
	}
	r, _ := state.LoadRecipeState(root, id)
	r.Feature, r.FeatureItem, r.FeatureAttempt, r.FeatureDir = f.ID, i1, 1, dir
	if err := state.SaveRecipeState(root, r); err != nil {
		t.Fatal(err)
	}
	if err := feature.Sync(root, id); err != nil {
		t.Fatal(err)
	}

	// The turn writes a record, with no phase change.
	if err := os.MkdirAll(filepath.Join(state.RecipeDir(root, id), "mutations"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state.RecipeDir(root, id), "mutations", "INV-001.patch"), []byte("--- a\n+++ b\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(ad, feature.SnapshotDir, "recipe", "mutations", "INV-001.patch")); err == nil {
		t.Fatal("the record must not be in the store before the turn ends")
	}
	if _, err := NewStopHandler().Handle(&HookInput{CWD: root, SessionID: ownerSID, LastAssistantMessage: "Tests written; continuing."}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(ad, feature.SnapshotDir, "recipe", "mutations", "INV-001.patch")); err != nil {
		t.Fatalf("the turn's record must reach the store at the Stop hook: %v", err)
	}
}
