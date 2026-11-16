package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

// Trial problem #135: assess-precheck on a build recipe judged draft.md,
// which a build recipe never writes, and sent the loop to /bts-assess.
func TestPrecheckBuild_ReadsTheBuildGates(t *testing.T) {
	root, id := newBuildFixture(t)
	recipe, err := state.LoadRecipeState(root, id)
	if err != nil {
		t.Fatal(err)
	}
	recipe.Type, recipe.Phase = "build", "falsify"
	if err := state.SaveRecipeState(root, recipe); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state.RecipeDir(root, id), "final.md"), []byte("# Build spec\n"), 0644); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	code := precheckBuild(&out, &errOut, root, recipe, filepath.Join(state.RecipeDir(root, id), "draft.md"))
	if code != 10 {
		t.Fatalf("exit = %d, want 10 while a build gate is not met; output:\n%s", code, out.String())
	}
	got := out.String()
	if !strings.Contains(got, "build gate skeleton_builds is not met") {
		t.Errorf("want the first gate not met (no skeleton build recorded), got:\n%s", got)
	}
	if strings.Contains(got, "verification history for draft.md") || strings.Contains(got, "Run /bts-assess") {
		t.Errorf("a build recipe has no draft.md and no /bts-assess round, got:\n%s", got)
	}
	if !strings.Contains(errOut.String(), "its spec document is final.md") {
		t.Errorf("--doc draft.md on a build recipe should be noted, stderr:\n%s", errOut.String())
	}
}
