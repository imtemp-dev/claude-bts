package state

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A project initialized before the jig rebrand keeps its state under .bts/.
// FindRoot has to adopt it, because every path helper derives from .jig/ —
// leaving it alone would report "not a jig project" for a project that is one.
func TestFindRootAdoptsLegacyStateDir(t *testing.T) {
	for _, legacy := range []string{".bts", ".forge"} {
		t.Run(legacy, func(t *testing.T) {
			dir := t.TempDir()
			specs := filepath.Join(dir, legacy, "specs", "recipes", "r-001")
			if err := os.MkdirAll(specs, 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, ".gitignore"),
				[]byte(legacy+"/local/\n"), 0644); err != nil {
				t.Fatal(err)
			}

			root, err := FindRoot(dir)
			if err != nil {
				t.Fatalf("FindRoot on a %s/ project: %v", legacy, err)
			}
			if root != dir {
				t.Errorf("root = %s, want %s", root, dir)
			}

			// The directory is renamed, not copied: leaving both would give
			// the project two divergent sets of recipe state.
			if _, err := os.Stat(filepath.Join(dir, legacy)); !os.IsNotExist(err) {
				t.Errorf("%s/ still exists after migration", legacy)
			}
			if _, err := os.Stat(filepath.Join(dir, ".jig", "specs", "recipes", "r-001")); err != nil {
				t.Errorf("recipe state did not survive the rename: %v", err)
			}

			gi, _ := os.ReadFile(filepath.Join(dir, ".gitignore"))
			if !strings.Contains(string(gi), ".jig/local/") {
				t.Errorf(".gitignore still ignores the old path, so local state gets committed: %q", gi)
			}
		})
	}
}

// A recipe.json written before the rebrand carries the retired type name.
// Every type comparison now tests the new spelling, so a legacy value that
// survives the load silently drops that recipe's gates.
func TestLoadRecipeStateNormalizesRetiredTypes(t *testing.T) {
	cases := map[string]string{"blueprint": "spec", "analyze": "map", "fix": "fix"}
	for stored, want := range cases {
		t.Run(stored, func(t *testing.T) {
			root := t.TempDir()
			dir := RecipeDir(root, "r-001")
			if err := os.MkdirAll(dir, 0755); err != nil {
				t.Fatal(err)
			}
			body := `{"id":"r-001","type":"` + stored + `","topic":"t","phase":"draft",` +
				`"iteration":0,"level":0,"started_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"}`
			if err := os.WriteFile(filepath.Join(dir, "recipe.json"), []byte(body), 0644); err != nil {
				t.Fatal(err)
			}

			got, err := LoadRecipeState(root, "r-001")
			if err != nil {
				t.Fatal(err)
			}
			if got.Type != want {
				t.Errorf("type %q loaded as %q, want %q", stored, got.Type, want)
			}
		})
	}
}
