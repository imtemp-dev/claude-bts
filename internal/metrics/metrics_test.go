package metrics

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupMetricsRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, ".forge", "state", "recipes", "r-1000"), 0755)
	return root
}

func TestAppend_GlobalAndRecipe(t *testing.T) {
	root := setupMetricsRoot(t)

	event := &MetricsEvent{
		Kind:      KindSessionStart,
		SessionID: "sess-1",
		RecipeID:  "r-1000",
		Phase:     "draft",
		Model:     "claude-opus-4-6",
		Source:    "startup",
	}

	if err := Append(root, event); err != nil {
		t.Fatalf("Append failed: %v", err)
	}

	// Timestamp should be auto-set
	if event.Timestamp == "" {
		t.Error("Timestamp should be auto-set")
	}

	// Check global log
	globalData, err := os.ReadFile(globalPath(root))
	if err != nil {
		t.Fatalf("read global log: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(globalData)), "\n")
	if len(lines) != 1 {
		t.Fatalf("global log: got %d lines, want 1", len(lines))
	}

	var parsed MetricsEvent
	json.Unmarshal([]byte(lines[0]), &parsed)
	if parsed.Kind != KindSessionStart {
		t.Errorf("Kind: got %s, want session_start", parsed.Kind)
	}
	if parsed.Model != "claude-opus-4-6" {
		t.Errorf("Model: got %s, want claude-opus-4-6", parsed.Model)
	}

	// Check recipe log
	recipeData, err := os.ReadFile(recipePath(root, "r-1000"))
	if err != nil {
		t.Fatalf("read recipe log: %v", err)
	}
	recipeLines := strings.Split(strings.TrimSpace(string(recipeData)), "\n")
	if len(recipeLines) != 1 {
		t.Fatalf("recipe log: got %d lines, want 1", len(recipeLines))
	}
}

func TestAppend_GlobalOnly(t *testing.T) {
	root := setupMetricsRoot(t)

	event := &MetricsEvent{
		Kind:      KindSessionStart,
		SessionID: "sess-1",
		// No RecipeID
	}

	if err := Append(root, event); err != nil {
		t.Fatalf("Append failed: %v", err)
	}

	// Global log should exist
	if _, err := os.Stat(globalPath(root)); err != nil {
		t.Fatalf("global log not created: %v", err)
	}

	// Recipe log should NOT exist (no RecipeID)
	if _, err := os.Stat(recipePath(root, "")); err == nil {
		t.Error("recipe log should not exist when RecipeID is empty")
	}
}

func TestAppendGlobal(t *testing.T) {
	root := setupMetricsRoot(t)

	event := &MetricsEvent{
		Kind:     KindTokenSnapshot,
		RecipeID: "r-1000", // even with RecipeID, AppendGlobal only writes global
		Tokens: &TokenSnapshot{
			InputTokens:  90000,
			OutputTokens: 5000,
			UsedPercentage: 45.2,
		},
	}

	if err := AppendGlobal(root, event); err != nil {
		t.Fatalf("AppendGlobal failed: %v", err)
	}

	// Global should exist
	globalData, _ := os.ReadFile(globalPath(root))
	if len(globalData) == 0 {
		t.Error("global log should have data")
	}

	// Recipe log should NOT exist
	recipePath := recipePath(root, "r-1000")
	if _, err := os.Stat(recipePath); err == nil {
		t.Error("AppendGlobal should not write to recipe log")
	}
}

func TestAppend_MultipleEvents(t *testing.T) {
	root := setupMetricsRoot(t)

	events := []MetricsEvent{
		{Kind: KindSessionStart, SessionID: "s1", RecipeID: "r-1000"},
		{Kind: KindPhaseChange, SessionID: "s1", RecipeID: "r-1000", Phase: "draft", PreviousPhase: "research"},
		{Kind: KindToolUse, SessionID: "s1", RecipeID: "r-1000", ToolName: "Edit", ToolFile: "main.go"},
		{Kind: KindSessionEnd, SessionID: "s1", RecipeID: "r-1000"},
	}

	for i := range events {
		if err := Append(root, &events[i]); err != nil {
			t.Fatalf("Append event %d failed: %v", i, err)
		}
	}

	// Global should have 4 lines
	globalData, _ := os.ReadFile(globalPath(root))
	lines := strings.Split(strings.TrimSpace(string(globalData)), "\n")
	if len(lines) != 4 {
		t.Errorf("global log: got %d lines, want 4", len(lines))
	}

	// Recipe should also have 4 lines
	recipeData, _ := os.ReadFile(recipePath(root, "r-1000"))
	recipeLines := strings.Split(strings.TrimSpace(string(recipeData)), "\n")
	if len(recipeLines) != 4 {
		t.Errorf("recipe log: got %d lines, want 4", len(recipeLines))
	}
}

func TestAppend_PreservesExistingTimestamp(t *testing.T) {
	root := setupMetricsRoot(t)

	event := &MetricsEvent{
		Timestamp: "2026-01-01T00:00:00Z",
		Kind:      KindSessionStart,
		SessionID: "s1",
	}

	Append(root, event)
	if event.Timestamp != "2026-01-01T00:00:00Z" {
		t.Errorf("Timestamp changed: got %s", event.Timestamp)
	}
}

func TestAppend_ToolUseWithExitCode(t *testing.T) {
	root := setupMetricsRoot(t)

	exitCode := 1
	success := false
	event := &MetricsEvent{
		Kind:      KindToolUse,
		SessionID: "s1",
		RecipeID:  "r-1000",
		ToolName:  "Bash",
		ToolFile:  "go test ./...",
		ExitCode:  &exitCode,
		Success:   &success,
	}

	Append(root, event)

	globalData, _ := os.ReadFile(globalPath(root))
	var parsed MetricsEvent
	json.Unmarshal([]byte(strings.TrimSpace(string(globalData))), &parsed)

	if parsed.ExitCode == nil || *parsed.ExitCode != 1 {
		t.Errorf("ExitCode: got %v, want 1", parsed.ExitCode)
	}
	if parsed.Success == nil || *parsed.Success != false {
		t.Errorf("Success: got %v, want false", parsed.Success)
	}
}

func TestAppend_TokenSnapshot(t *testing.T) {
	root := setupMetricsRoot(t)

	event := &MetricsEvent{
		Kind: KindTokenSnapshot,
		Tokens: &TokenSnapshot{
			InputTokens:         90000,
			CacheCreationTokens: 10000,
			CacheReadTokens:     50000,
			OutputTokens:        5000,
			ContextWindowSize:   200000,
			UsedPercentage:      45.2,
		},
	}

	AppendGlobal(root, event)

	globalData, _ := os.ReadFile(globalPath(root))
	var parsed MetricsEvent
	json.Unmarshal([]byte(strings.TrimSpace(string(globalData))), &parsed)

	if parsed.Tokens == nil {
		t.Fatal("Tokens should not be nil")
	}
	if parsed.Tokens.InputTokens != 90000 {
		t.Errorf("InputTokens: got %d, want 90000", parsed.Tokens.InputTokens)
	}
	if parsed.Tokens.UsedPercentage != 45.2 {
		t.Errorf("UsedPercentage: got %f, want 45.2", parsed.Tokens.UsedPercentage)
	}
}
