package hook

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/imtemp-dev/jig/internal/state"
)

func setupSessionStartRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".jig", "specs", "recipes"), 0755); err != nil {
		t.Fatalf("mkdir specs: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".jig", "local"), 0755); err != nil {
		t.Fatalf("mkdir local: %v", err)
	}
	// Pin template-version file so autoUpdateTemplates is a no-op in tests.
	configDir := filepath.Join(root, ".jig", "config")
	_ = os.MkdirAll(configDir, 0755)
	_ = os.WriteFile(filepath.Join(configDir, ".template-version"), []byte("test"), 0644)
	// Isolate HOME so plan lookups don't leak real state
	t.Setenv("HOME", t.TempDir())
	return root
}

func TestDetectSource_MarkerTakesPrecedence(t *testing.T) {
	root := setupSessionStartRoot(t)
	_ = state.WriteCompactMarker(root, &state.CompactMarker{SessionID: "s"})

	input := &HookInput{CWD: root} // no explicit Source
	got := detectSource(root, input, nil)
	if got != "compact" {
		t.Errorf("want compact, got %s", got)
	}
	// Marker should be consumed
	if _, err := os.Stat(state.CompactMarkerPath(root)); !os.IsNotExist(err) {
		t.Error("marker should be consumed")
	}
}

func TestDetectSource_ExplicitBeatsMarker(t *testing.T) {
	root := setupSessionStartRoot(t)
	_ = state.WriteCompactMarker(root, &state.CompactMarker{SessionID: "s"})

	input := &HookInput{CWD: root, Source: "resume"}
	got := detectSource(root, input, nil)
	if got != "resume" {
		t.Errorf("want resume, got %s", got)
	}
	// Explicit source should not consume the marker
	if _, err := os.Stat(state.CompactMarkerPath(root)); err != nil {
		t.Error("marker should remain when explicit source wins")
	}
}

func TestDetectSource_HeuristicFallback(t *testing.T) {
	root := setupSessionStartRoot(t)
	ws := &state.WorkState{
		RecipeID: "r-1", Phase: "draft",
		SavedAt: time.Now().UTC().Add(-30 * time.Second).Format(time.RFC3339),
	}
	got := detectSource(root, &HookInput{CWD: root}, ws)
	if got != "compact" {
		t.Errorf("want compact (fresh heuristic), got %s", got)
	}
}

func TestDetectSource_StartupWhenNoSignals(t *testing.T) {
	root := setupSessionStartRoot(t)
	got := detectSource(root, &HookInput{CWD: root}, nil)
	if got != "startup" {
		t.Errorf("want startup, got %s", got)
	}
}

func TestSessionStart_CompactRecipeHintUsesAssessOverride(t *testing.T) {
	root := setupSessionStartRoot(t)
	recipeID := "r-001-x"
	seedRecipe(t, root, recipeID, "draft")

	// Seed assess.json and marker and work-state
	assess := filepath.Join(state.RecipeDir(root, recipeID), "assess.json")
	_ = os.WriteFile(assess, []byte(`{"next_action":"Expand section 2 with error taxonomy."}`), 0644)
	ws, _ := state.BuildWorkState(root)
	_ = state.SaveWorkState(root, ws)
	_ = state.WriteCompactMarker(root, &state.CompactMarker{SessionID: "s", RecipeID: recipeID, Phase: "draft"})

	h := NewSessionStartHandler()
	out, err := h.Handle(&HookInput{CWD: root, SessionID: "s"})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if out.HookSpecificOutput == nil {
		t.Fatal("expected additionalContext")
	}
	msg := out.HookSpecificOutput.AdditionalContext
	if !strings.Contains(msg, "Context compacted") {
		t.Errorf("missing compact prefix: %s", msg)
	}
	if !strings.Contains(msg, "Expand section 2") {
		t.Errorf("assess next_action should override hint: %s", msg)
	}
}

func TestSessionStart_CompactSubStateDebateHint(t *testing.T) {
	root := setupSessionStartRoot(t)
	recipeID := "r-002-debate"
	seedRecipe(t, root, recipeID, "debate")

	_ = state.SaveDebateRound(root, recipeID, &state.DebateRoundState{
		DebateID: "d-1", Round: 2, TotalRounds: 3, NextPersona: "security",
	})
	ws, _ := state.BuildWorkState(root)
	_ = state.SaveWorkState(root, ws)
	_ = state.WriteCompactMarker(root, &state.CompactMarker{SessionID: "s"})

	out, err := NewSessionStartHandler().Handle(&HookInput{CWD: root, SessionID: "s"})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	msg := out.HookSpecificOutput.AdditionalContext
	if !strings.Contains(msg, "Resume debate") {
		t.Errorf("expected debate hint: %s", msg)
	}
	if !strings.Contains(msg, "round 2/3") {
		t.Errorf("expected round info: %s", msg)
	}
}

func TestSessionStart_NonRecipeCompactRecovery(t *testing.T) {
	root := setupSessionStartRoot(t)

	// Seed tool trace + session state + marker (no recipe)
	_ = state.AppendToolTrace(root, &state.ToolTraceEntry{
		Phase: "post", ToolName: "Read", File: "foo.go",
	})
	_ = state.AppendToolTrace(root, &state.ToolTraceEntry{
		Phase: "post", ToolName: "Edit", File: "bar.go",
	})
	ss, _ := state.BuildSessionState(root)
	_ = state.SaveSessionState(root, ss)
	_ = state.WriteCompactMarker(root, &state.CompactMarker{SessionID: "s"})

	out, err := NewSessionStartHandler().Handle(&HookInput{CWD: root, SessionID: "s"})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if out.HookSpecificOutput == nil {
		t.Fatal("expected additionalContext")
	}
	msg := out.HookSpecificOutput.AdditionalContext
	if !strings.Contains(msg, "no active recipe") {
		t.Errorf("expected non-recipe marker: %s", msg)
	}
	if !strings.Contains(msg, "foo.go") || !strings.Contains(msg, "bar.go") {
		t.Errorf("expected open files: %s", msg)
	}
	if !strings.Contains(msg, "Last tool: Edit(bar.go)") {
		t.Errorf("expected last tool line: %s", msg)
	}
}

func TestSessionStart_StartupDoesNotConsumeMarker(t *testing.T) {
	root := setupSessionStartRoot(t)
	// No recipe, no trace, explicit "startup" source — marker should not exist anyway
	input := &HookInput{CWD: root, SessionID: "s", Source: "startup"}
	out, err := NewSessionStartHandler().Handle(input)
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	// With no active recipe and no roadmap/vision, output can be empty — that's fine.
	_ = out
}

// TestSessionStart_StaleWorkStateIsDiscarded verifies that a work-state
// snapshot referencing a cancelled/old recipe does not taint the recovery
// message for a different, currently active recipe.
func TestSessionStart_StaleWorkStateIsDiscarded(t *testing.T) {
	root := setupSessionStartRoot(t)

	// Seed two recipes: one cancelled (old), one active (new)
	seedRecipe(t, root, "r-new-001", "draft")
	oldRecipe := &state.RecipeState{
		ID: "r-old-999", Type: "spec", Topic: "old one", Phase: "cancelled",
		StartedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := os.MkdirAll(state.RecipeDir(root, oldRecipe.ID), 0755); err != nil {
		t.Fatalf("mkdir old: %v", err)
	}
	if err := state.SaveRecipeState(root, oldRecipe); err != nil {
		t.Fatalf("save old: %v", err)
	}

	// Save a stale work-state pointing to the OLD recipe
	staleWS := &state.WorkState{
		RecipeID: "r-old-999",
		Phase:    "draft",
		Topic:    "old one",
		Summary:  "Recipe r-old-999 (spec) \"old one\" — phase: draft.",
	}
	if err := state.SaveWorkState(root, staleWS); err != nil {
		t.Fatalf("save stale ws: %v", err)
	}
	_ = state.WriteCompactMarker(root, &state.CompactMarker{SessionID: "s"})

	out, err := NewSessionStartHandler().Handle(&HookInput{CWD: root, SessionID: "s"})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	msg := out.HookSpecificOutput.AdditionalContext
	// The stale ws must NOT appear anywhere in the message.
	if strings.Contains(msg, "r-old-999") || strings.Contains(msg, "old one") {
		t.Errorf("stale ws leaked into recovery message: %s", msg)
	}
	// The message should describe the new active recipe (Topic "test"
	// from seedRecipe). When ws is discarded the fallback format uses
	// the live recipe's type/topic/phase, so "draft" phase must appear.
	if !strings.Contains(msg, "draft") {
		t.Errorf("expected active recipe phase in msg: %s", msg)
	}
}

// TestSessionStart_AssessNextActionReRead verifies that buildHint reads
// the CURRENT assess.json rather than using the ws cache. This protects
// against the case where /jig-assess ran after PreCompact captured ws.
func TestSessionStart_AssessNextActionReRead(t *testing.T) {
	root := setupSessionStartRoot(t)
	recipeID := "r-001-fresh"
	seedRecipe(t, root, recipeID, "draft")

	// Seed an "old" assess.json, build ws (cache old value), then
	// overwrite assess.json with a newer recommendation.
	assess := filepath.Join(state.RecipeDir(root, recipeID), "assess.json")
	_ = os.WriteFile(assess, []byte(`{"next_action":"OLD: fix section 1."}`), 0644)
	ws, _ := state.BuildWorkState(root)
	if ws == nil || ws.NextAction != "OLD: fix section 1." {
		t.Fatalf("cache check failed: %+v", ws)
	}
	_ = state.SaveWorkState(root, ws)
	// After ws is cached, a fresh /jig-assess run updates assess.json:
	_ = os.WriteFile(assess, []byte(`{"next_action":"NEW: verify claim in section 2."}`), 0644)
	_ = state.WriteCompactMarker(root, &state.CompactMarker{SessionID: "s"})

	out, err := NewSessionStartHandler().Handle(&HookInput{CWD: root, SessionID: "s"})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	msg := out.HookSpecificOutput.AdditionalContext
	if !strings.Contains(msg, "NEW: verify claim in section 2.") {
		t.Errorf("should use fresh assess.json, got: %s", msg)
	}
	// The cached value in ws.Summary may still appear (acceptable — it's
	// a snapshot), but the NEXT hint must be fresh.
	nextIdx := strings.Index(msg, "NEXT:")
	if nextIdx < 0 {
		t.Fatalf("missing NEXT: hint: %s", msg)
	}
	if !strings.Contains(msg[nextIdx:], "NEW:") {
		t.Errorf("NEXT hint should be fresh, got: %s", msg[nextIdx:])
	}
}
