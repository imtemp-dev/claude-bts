package hook

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

// Trial problem #121: report-build's research subagent (r-002, build,
// 11-10 20:31Z) copied the project into .agent-ephemeral/ to try a
// dependency bump, edited the copy's pyproject.toml, and was told
// "Blueprint creates specs, not code. Save code snippets in the spec
// document instead."  The recipe was a build recipe, whose steps ask for
// throwaway runs, and the copy was gitignored scratch, not product code.
func TestPreToolUse_SpecPhaseWarningSparesGitignoredProbesAndSpeaksBuild(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root, id := setupStopRoot(t)
	r, _ := state.LoadRecipeState(root, id)
	r.Type, r.Phase = "build", "research"
	if err := state.SaveRecipeState(root, r); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", root, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte(".agent-ephemeral/\n"), 0644); err != nil {
		t.Fatal(err)
	}
	edit := func(path string) string {
		t.Helper()
		out, err := NewPreToolUseHandler().Handle(&HookInput{CWD: root, ToolName: "Edit",
			ToolInput: map[string]interface{}{"file_path": path}})
		if err != nil {
			t.Fatal(err)
		}
		if out.HookSpecificOutput == nil {
			return ""
		}
		return out.HookSpecificOutput.AdditionalContext
	}

	probe := filepath.Join(root, ".agent-ephemeral", "r-002-bump-probe", "agent_server", "pyproject.toml")
	if got := edit(probe); got != "" {
		t.Fatalf("a gitignored probe copy is not product code, got %q", got)
	}
	got := edit(filepath.Join(root, "src", "relay.py"))
	if !strings.Contains(got, "spec phase") || strings.Contains(got, "Blueprint") || !strings.Contains(got, "probes/") {
		t.Fatalf("product code in a build recipe's research is warned in the build recipe's terms, got %q", got)
	}
}
