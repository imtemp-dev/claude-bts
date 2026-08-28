package hook

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/imtemp-dev/jig/internal/state"
)

func setupPreCompactRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".jig", "specs", "recipes"), 0755); err != nil {
		t.Fatalf("mkdir specs: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".jig", "local"), 0755); err != nil {
		t.Fatalf("mkdir local: %v", err)
	}
	return root
}

func seedRecipe(t *testing.T, root, id, phase string) {
	t.Helper()
	if err := os.MkdirAll(state.RecipeDir(root, id), 0755); err != nil {
		t.Fatalf("mkdir recipe: %v", err)
	}
	r := &state.RecipeState{
		ID: id, Type: "spec", Topic: "test", Phase: phase,
		StartedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := state.SaveRecipeState(root, r); err != nil {
		t.Fatalf("save recipe: %v", err)
	}
}

func TestPreCompact_ActiveRecipe_WritesWorkStateAndMarker(t *testing.T) {
	root := setupPreCompactRoot(t)
	seedRecipe(t, root, "r-001-test", "draft")

	h := NewPreCompactHandler()
	input := &HookInput{SessionID: "sess-1", CWD: root, HookEventName: "pre-compact"}
	if _, err := h.Handle(input); err != nil {
		t.Fatalf("handle: %v", err)
	}

	if _, err := os.Stat(state.WorkStatePath(root)); err != nil {
		t.Errorf("expected work-state.json: %v", err)
	}
	if _, err := os.Stat(state.CompactMarkerPath(root)); err != nil {
		t.Errorf("expected compact-pending.json: %v", err)
	}
	if _, err := os.Stat(state.SessionStatePath(root)); !os.IsNotExist(err) {
		t.Errorf("session-state.json should NOT be written when recipe active")
	}

	// Marker should carry recipe metadata
	m, err := state.ConsumeCompactMarker(root)
	if err != nil {
		t.Fatalf("consume marker: %v", err)
	}
	if m == nil || m.RecipeID != "r-001-test" || m.Phase != "draft" {
		t.Errorf("marker: %+v", m)
	}
	if m.SessionID != "sess-1" {
		t.Errorf("SessionID: %q", m.SessionID)
	}
}

func TestPreCompact_NoRecipe_WritesSessionStateIfTraceExists(t *testing.T) {
	root := setupPreCompactRoot(t)

	// Seed a tool-trace entry so BuildSessionState returns non-nil
	if err := state.AppendToolTrace(root, &state.ToolTraceEntry{
		Phase: "post", ToolName: "Read", File: "foo.go",
	}); err != nil {
		t.Fatalf("seed trace: %v", err)
	}

	h := NewPreCompactHandler()
	input := &HookInput{SessionID: "sess-2", CWD: root, HookEventName: "pre-compact"}
	if _, err := h.Handle(input); err != nil {
		t.Fatalf("handle: %v", err)
	}

	if _, err := os.Stat(state.SessionStatePath(root)); err != nil {
		t.Errorf("expected session-state.json: %v", err)
	}
	if _, err := os.Stat(state.WorkStatePath(root)); !os.IsNotExist(err) {
		t.Errorf("work-state.json should NOT be written when no recipe")
	}
	if _, err := os.Stat(state.CompactMarkerPath(root)); err != nil {
		t.Errorf("marker must be written even without recipe: %v", err)
	}
}

func TestPreCompact_NoRecipeNoTrace_MarkerOnly(t *testing.T) {
	root := setupPreCompactRoot(t)
	// Isolate HOME so plan lookup doesn't accidentally create non-nil session state.
	t.Setenv("HOME", t.TempDir())

	h := NewPreCompactHandler()
	input := &HookInput{SessionID: "sess-3", CWD: root, HookEventName: "pre-compact"}
	if _, err := h.Handle(input); err != nil {
		t.Fatalf("handle: %v", err)
	}

	if _, err := os.Stat(state.WorkStatePath(root)); !os.IsNotExist(err) {
		t.Error("work-state.json should not exist")
	}
	if _, err := os.Stat(state.SessionStatePath(root)); !os.IsNotExist(err) {
		t.Error("session-state.json should not exist (no trace, no plan)")
	}
	if _, err := os.Stat(state.CompactMarkerPath(root)); err != nil {
		t.Errorf("marker should still exist: %v", err)
	}
}

func TestPreCompact_InvalidRoot_SilentExit(t *testing.T) {
	// CWD outside any .jig/ tree — handler should no-op
	h := NewPreCompactHandler()
	input := &HookInput{SessionID: "s", CWD: "/tmp/nonexistent-jig-root-xyz", HookEventName: "pre-compact"}
	out, err := h.Handle(input)
	if err != nil {
		t.Fatalf("should not error: %v", err)
	}
	if out == nil {
		t.Error("expected empty output, got nil")
	}
}
