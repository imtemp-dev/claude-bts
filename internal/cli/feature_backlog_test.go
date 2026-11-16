package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/feature"
	"github.com/imtemp-dev/claude-bts/internal/state"
)

// Trial problem #152 (AGEN-210): feature context compared the feature id
// with its own text prefix, so a short id never matched the bound recipe
// and the context was not written into it.
func TestFeatureContext_ShortIDWritesIntoTheBoundRecipe(t *testing.T) {
	t.Setenv(feature.StoreEnv, t.TempDir())
	_, wts := featureRepo(t, 1)
	a := wts[0]
	fid, _, items := confirmedFeature(t, a, []string{"Door"}, nil)
	id, err := createFeatureRecipe(a, fid, items[0], "", "")
	if err != nil {
		t.Fatal(err)
	}
	if fid[:6] != "f-001-" {
		t.Fatalf("fixture id %q: want f-001-<slug>", fid)
	}
	t.Chdir(a)
	resetFlags(featureContextCmd)
	for _, short := range []string{"f-1", "f-01", "f-001"} {
		_ = os.Remove(filepath.Join(state.RecipeDir(a, id), "feature-context.md"))
		if err := featureContextCmd.RunE(featureContextCmd, []string{short, items[0]}); err != nil {
			t.Fatalf("%s: %v", short, err)
		}
		if _, err := os.Stat(filepath.Join(state.RecipeDir(a, id), "feature-context.md")); err != nil {
			t.Errorf("feature context %s %s must write the context into the bound recipe %s: %v", short, items[0], id, err)
		}
	}
}
