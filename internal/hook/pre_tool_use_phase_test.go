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

// Trial problem #51: true-shark, in its build recipe's discovery phase, wrote
// a report for another session to a directory outside the project and got
// "Writing source code during spec phase".  Only the project's files count.
func TestPreToolUse_SpecPhaseWarningIgnoresFilesOutsideTheProject(t *testing.T) {
	root, id := setupStopRoot(t)
	r, _ := state.LoadRecipeState(root, id)
	r.Type, r.Phase = "build", "discovery"
	if err := state.SaveRecipeState(root, r); err != nil {
		t.Fatal(err)
	}
	write := func(path string) string {
		t.Helper()
		out, err := NewPreToolUseHandler().Handle(&HookInput{CWD: root, ToolName: "Write",
			ToolInput: map[string]interface{}{"file_path": path}})
		if err != nil {
			t.Fatal(err)
		}
		if out.HookSpecificOutput == nil {
			return ""
		}
		return out.HookSpecificOutput.AdditionalContext
	}
	outside := filepath.Join(filepath.Dir(root), "elsewhere", ".agent-scratch", "report.md")
	if got := write(outside); got != "" {
		t.Fatalf("a file outside the project must not be warned, got %q", got)
	}
	if got := write(filepath.Join(root, "src", "relay.py")); !strings.Contains(got, "spec phase") {
		t.Fatalf("the project's own code in a spec phase must still be warned, got %q", got)
	}
}
