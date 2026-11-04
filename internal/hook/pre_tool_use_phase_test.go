package hook

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

// The build recipe's Step 11 fixes the skeleton during its single review
// (phase verify), so the blueprint warning "creates specs, not code" is
// wrong there. A blueprint in the same phase still gets it.
func TestPreToolUse_SpecPhaseWarningIsForBlueprintsOnly(t *testing.T) {
	root, id := setupStopRoot(t)
	warn := func(recipeType string) string {
		t.Helper()
		r, _ := state.LoadRecipeState(root, id)
		r.Type, r.Phase = recipeType, "verify"
		if err := state.SaveRecipeState(root, r); err != nil {
			t.Fatal(err)
		}
		out, err := NewPreToolUseHandler().Handle(&HookInput{
			CWD: root, ToolName: "Edit",
			ToolInput: map[string]interface{}{"file_path": filepath.Join(root, "src", "relay.py")},
		})
		if err != nil {
			t.Fatal(err)
		}
		if out.HookSpecificOutput == nil {
			return ""
		}
		return out.HookSpecificOutput.AdditionalContext
	}
	if got := warn("blueprint"); !strings.Contains(got, "spec phase") {
		t.Fatalf("a blueprint writing code in verify must be warned, got %q", got)
	}
	if got := warn("build"); got != "" {
		t.Fatalf("a build recipe fixing its skeleton in verify must not be warned, got %q", got)
	}
}
