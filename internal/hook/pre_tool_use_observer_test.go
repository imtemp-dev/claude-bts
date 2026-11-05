package hook

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

func editBy(root, sid, tool, path string) *HookInput {
	return &HookInput{CWD: root, SessionID: sid, HookEventName: "PreToolUse", ToolName: tool,
		ToolInput: map[string]interface{}{"file_path": path}}
}

func preToolContext(t *testing.T, in *HookInput) (ctx, decision string) {
	t.Helper()
	out, err := NewPreToolUseHandler().Handle(in)
	if err != nil {
		t.Fatal(err)
	}
	if out.HookSpecificOutput == nil {
		return "", ""
	}
	return out.HookSpecificOutput.AdditionalContext, out.HookSpecificOutput.PermissionDecision
}

// Trial problem #56: in the AGEN-43 handoff a stood-down session could
// still edit and test r-003's code, and only its own discipline kept the
// two sessions from editing together.  A session that does not own the
// active recipe is warned, not refused, when it writes a file the recipe's
// tasks.json or wireframe File Structure names.
func TestPreToolUse_WarnsAnObserverThatEditsTheRecipesFiles(t *testing.T) {
	root, id := setupImplementRoot(t) // tasks.json names src/a.go
	setOwner(t, root, id, ownerSID)
	wireframe := "# Wireframe\n\n## 4. File Structure\n\n| File | Action | Depends On | Responsibility |\n" +
		"| --- | --- | --- | --- |\n| `src/b.go` | create | - | the second unit |\n"
	if err := os.WriteFile(filepath.Join(state.RecipeDir(root, id), "wireframe.md"), []byte(wireframe), 0644); err != nil {
		t.Fatal(err)
	}

	ctx, decision := preToolContext(t, editBy(root, observerSID, "Edit", filepath.Join(root, "src", "a.go")))
	for _, want := range []string{"does not own recipe " + id, ownerSID[:8], "bts recipe claim " + id} {
		if !strings.Contains(ctx, want) {
			t.Errorf("the warning must say %q, got %q", want, ctx)
		}
	}
	if decision != "" {
		t.Errorf("a warning, not a refusal: permission decision %q", decision)
	}

	// A wireframe row counts too, and a path relative to the session's cwd.
	if ctx, _ := preToolContext(t, editBy(root, observerSID, "Write", "src/b.go")); !strings.Contains(ctx, "does not own recipe") {
		t.Errorf("a file the wireframe names must warn, got %q", ctx)
	}

	// Not for a file the recipe does not name, nor for a read.
	if ctx, _ := preToolContext(t, editBy(root, observerSID, "Edit", filepath.Join(root, "src", "other.go"))); ctx != "" {
		t.Errorf("a file outside the recipe must not warn, got %q", ctx)
	}
	if ctx, _ := preToolContext(t, editBy(root, observerSID, "Read", filepath.Join(root, "src", "a.go"))); ctx != "" {
		t.Errorf("a read must not warn, got %q", ctx)
	}

	// The owner, and a payload with no session id, are unaffected.
	if ctx, _ := preToolContext(t, editBy(root, ownerSID, "Edit", filepath.Join(root, "src", "a.go"))); strings.Contains(ctx, "does not own") {
		t.Errorf("the owner must not be warned, got %q", ctx)
	}
	if ctx, _ := preToolContext(t, editBy(root, "", "Edit", filepath.Join(root, "src", "a.go"))); strings.Contains(ctx, "does not own") {
		t.Errorf("a session with no id must not be warned, got %q", ctx)
	}
}
