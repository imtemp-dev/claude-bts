package hook

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/review"
	"github.com/imtemp-dev/claude-bts/internal/state"
)

func setSurface(t *testing.T, root, surface string) {
	t.Helper()
	path := filepath.Join(root, ".bts", "config", "settings.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("review:\n  surface: "+surface+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
}

func publish(root string, in map[string]interface{}) *HookInput {
	return &HookInput{CWD: root, HookEventName: string(EventPreToolUse), ToolName: "Artifact", ToolInput: in}
}

// Under review.surface local an Artifact publish of a review page is
// refused, whichever way the call names the page; any other artifact, an
// Artifact call that is not a publish, and the artifact surface pass.
func TestPreToolUse_RefusesPublishingALocalReviewPage(t *testing.T) {
	root := setupPreCompactRoot(t)
	setSurface(t, root, "local")
	page := filepath.Join(state.LocalPath(root), "recipes", "r-001-x", "review", "page.html")
	fpage := filepath.Join(state.LocalPath(root), "features", "f-002-y", "review", "page.html")
	deny := func(in map[string]interface{}) string {
		t.Helper()
		out, err := NewPreToolUseHandler().Handle(publish(root, in))
		if err != nil {
			t.Fatal(err)
		}
		if out.HookSpecificOutput == nil || out.HookSpecificOutput.PermissionDecision != "deny" {
			return ""
		}
		return out.HookSpecificOutput.PermissionDecisionReason
	}
	if r := deny(map[string]interface{}{"file_path": page}); !strings.Contains(r, "bts review open r-001-x") {
		t.Errorf("an absolute page path: %q", r)
	}
	rel, _ := filepath.Rel(root, page)
	if r := deny(map[string]interface{}{"file_path": rel, "icon": "notes"}); r == "" {
		t.Error("a page path relative to the session's directory was let through")
	}
	if r := deny(map[string]interface{}{"file_path": fpage}); !strings.Contains(r, "--feature f-002-y") {
		t.Errorf("a feature page: %q", r)
	}
	kit := map[string]interface{}{"file_path": "/tmp/elsewhere/page.html", "files": map[string]interface{}{
		"bts-review-kit.js": ".claude/skills/bts-review-page/kit/bts-review-kit.js"}}
	if r := deny(kit); r == "" {
		t.Error("a page published with the review kit from elsewhere was let through")
	}
	if r := deny(map[string]interface{}{"file_path": "/tmp/notes/explainer.html"}); r != "" {
		t.Errorf("an ordinary artifact was refused: %q", r)
	}
	if r := deny(map[string]interface{}{"action": "read", "url": "https://claude.ai/artifact/x"}); r != "" {
		t.Errorf("a read was refused: %q", r)
	}
	setSurface(t, root, "artifact")
	if r := deny(map[string]interface{}{"file_path": page}); r != "" {
		t.Errorf("the artifact surface refused its own page: %q", r)
	}
}

// A prompt that asks for an artifact or a review page, in English or
// Korean, gets the local surface's reminder while a review can be under
// way; other prompts, and other surfaces, get nothing from it.
func TestUserPromptSubmit_LocalSurfaceReminder(t *testing.T) {
	root := setupPreCompactRoot(t)
	setSurface(t, root, "local")
	if got := localSurfaceReminder(root, "show the intent as an artifact"); got != "" {
		t.Errorf("no recipe, no reminder: %q", got)
	}
	seedRecipe(t, root, "r-001-test", "scoping")
	// The Korean for "show it as an artifact" and "open the review page",
	// built from code points so this file stays ASCII.
	korean := string([]rune{0xC544, 0xD2F0, 0xD329, 0xD2B8, 0xB85C, ' ', 0xBCF4, 0xC5EC, 0xC918})
	koreanPage := string([]rune{0xB9AC, 0xBDF0, ' ', 0xD398, 0xC774, 0xC9C0, ' ', 0xC5F4, 0xC5B4, 0xC918})
	for _, p := range []string{"show the intent as an artifact", "open the review page", korean, koreanPage} {
		if got := localSurfaceReminder(root, p); !strings.Contains(got, "bts review open") {
			t.Errorf("%q got %q", p, got)
		}
	}
	if got := localSurfaceReminder(root, "run the tests"); got != "" {
		t.Errorf("an unrelated prompt got %q", got)
	}
	out, err := NewUserPromptSubmitHandler().Handle(&HookInput{CWD: root, HookEventName: string(EventUserPromptSubmit), Prompt: korean})
	if err != nil || out.HookSpecificOutput == nil || !strings.Contains(out.HookSpecificOutput.AdditionalContext, "review.surface is local") {
		t.Errorf("the hook did not add the reminder: %+v %v", out, err)
	}
	setSurface(t, root, "artifact")
	if got := localSurfaceReminder(root, "show the intent as an artifact"); got != "" {
		t.Errorf("the artifact surface got %q", got)
	}
}

// A local page's reminder says to import its rounds, not to query
// ArtifactData.
func TestReviewReminder_LocalPageSaysImport(t *testing.T) {
	root := setupPreCompactRoot(t)
	seedRecipe(t, root, "r-001-test", "scoping")
	if err := os.WriteFile(filepath.Join(state.RecipeDir(root, "r-001-test"), "intent.md"), []byte("# Intent\n\nStatus: EXPLORING\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := review.AttachLocal(review.RecipeSubject(root, "r-001-test"), "http://127.0.0.1:1/p/r-001-test/?t=x", "intent"); err != nil {
		t.Fatal(err)
	}
	recipe, _ := state.LoadRecipeState(root, "r-001-test")
	got := reviewReminder(root, recipe)
	if !strings.Contains(got, "bts review import r-001-test") || strings.Contains(got, "ArtifactData") {
		t.Errorf("local reminder: %q", got)
	}
}
