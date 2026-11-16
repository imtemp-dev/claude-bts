package feature

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

// Trial problem #156: a Multica worktree's settings are fresh, so an item
// learns the repository's setup from its feature.
func TestFeatureSetupReachesTheItem(t *testing.T) {
	t.Setenv(StoreEnv, t.TempDir())
	dir, f, _, err := Create(CreateOptions{Title: "x", Repo: &Repo{Key: "github.com/org/app"}, Base: "main", Setup: " cd app && uv sync "})
	if err != nil {
		t.Fatal(err)
	}
	if f.Setup != "cd app && uv sync" {
		t.Fatalf("create keeps the setup, trimmed: %q", f.Setup)
	}
	if err := os.WriteFile(filepath.Join(dir, IntentFile), []byte("# Intent\n\nx\n"), 0644); err != nil {
		t.Fatal(err)
	}
	i1, _ := AddItem(dir, ItemSpec{Title: ptr("one"), Type: ptr("build")})
	if _, err := Confirm(dir, "go"); err != nil {
		t.Fatal(err)
	}
	doc, _, err := Context(f.ID, i1, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, "Setup: `cd app && uv sync`") {
		t.Fatalf("the context names the setup:\n%s", doc)
	}
	r := &state.RecipeState{Feature: f.ID, FeatureDir: dir}
	if got := SetupFor(r); got != "cd app && uv sync" {
		t.Fatalf("SetupFor: %q", got)
	}
	if err := SetSetup(dir, "make deps"); err != nil {
		t.Fatal(err)
	}
	r.FeatureDir = "/gone"
	if got := SetupFor(r); got != "make deps" {
		t.Fatalf("SetupFor finds a moved feature by id: %q", got)
	}
}
