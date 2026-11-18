package hook

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

// bts #173: a feature item's recipe may not write the project-wide
// documents, in any phase; a recipe of no feature may; other files of the
// item are untouched by this rule.
func TestPreToolUse_FeatureItemLeavesProjectDocsAlone(t *testing.T) {
	write := func(t *testing.T, feature, phase, file string) *HookOutput {
		t.Helper()
		root := t.TempDir()
		r := &state.RecipeState{ID: "r-001-x", Type: "build", Phase: phase, Feature: feature, FeatureItem: "i2"}
		if err := os.MkdirAll(state.RecipeDir(root, r.ID), 0755); err != nil {
			t.Fatal(err)
		}
		if err := state.SaveRecipeState(root, r); err != nil {
			t.Fatal(err)
		}
		out, err := (&preToolUseHandler{}).Handle(&HookInput{CWD: root, ToolName: "Write",
			ToolInput: map[string]interface{}{"file_path": filepath.Join(root, file)}})
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	denied := func(out *HookOutput) bool {
		return out.HookSpecificOutput != nil && out.HookSpecificOutput.PermissionDecision == "deny"
	}

	for _, phase := range []string{"status", "verify", "implement"} {
		for _, doc := range []string{".bts/specs/project-status.md", ".bts/specs/project-map.md"} {
			out := write(t, "f-004-greeting", phase, doc)
			if !denied(out) || !strings.Contains(out.HookSpecificOutput.PermissionDecisionReason, "bts #173") {
				t.Errorf("a feature item writing %s in %s must be refused, got %+v", doc, phase, out.HookSpecificOutput)
			}
		}
	}
	if out := write(t, "", "status", ".bts/specs/project-status.md"); denied(out) {
		t.Errorf("a recipe of no feature writes project-status.md as before, got %+v", out.HookSpecificOutput)
	}
	if out := write(t, "f-004-greeting", "status", ".bts/specs/recipes/r-001-x/final.md"); denied(out) {
		t.Errorf("the item's own documents are not touched by this rule, got %+v", out.HookSpecificOutput)
	}
}
