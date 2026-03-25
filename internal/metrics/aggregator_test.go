package metrics

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func ptr[T any](v T) *T { return &v }

func ts(minute int) string {
	return time.Date(2026, 1, 1, 10, minute, 0, 0, time.UTC).Format(time.RFC3339)
}

func TestAggregateRecipe_SessionCounting(t *testing.T) {
	events := []MetricsEvent{
		{Timestamp: ts(0), Kind: KindSessionStart, SessionID: "s1", RecipeID: "r-1000", Model: "claude-opus-4-6"},
		{Timestamp: ts(10), Kind: KindSessionEnd, SessionID: "s1"},
		{Timestamp: ts(20), Kind: KindSessionStart, SessionID: "s2", RecipeID: "r-1000", Model: "claude-sonnet-4-6"},
		{Timestamp: ts(30), Kind: KindSessionEnd, SessionID: "s2"},
	}

	stats := AggregateRecipe(events)
	if stats.TotalSessions != 2 {
		t.Errorf("TotalSessions: got %d, want 2", stats.TotalSessions)
	}
	if stats.RecipeID != "r-1000" {
		t.Errorf("RecipeID: got %s, want r-1000", stats.RecipeID)
	}
	if len(stats.Models) != 2 {
		t.Errorf("Models: got %d, want 2", len(stats.Models))
	}
	// 30 minutes total
	if stats.TotalDuration != 30*time.Minute {
		t.Errorf("TotalDuration: got %s, want 30m", stats.TotalDuration)
	}
}

func TestAggregateRecipe_PhaseTimeline(t *testing.T) {
	events := []MetricsEvent{
		{Timestamp: ts(0), Kind: KindSessionStart, SessionID: "s1", RecipeID: "r-1000"},
		{Timestamp: ts(0), Kind: KindPhaseChange, RecipeID: "r-1000", Phase: "research", PreviousPhase: "scoping"},
		{Timestamp: ts(5), Kind: KindPhaseChange, RecipeID: "r-1000", Phase: "draft", PreviousPhase: "research"},
		{Timestamp: ts(15), Kind: KindPhaseChange, RecipeID: "r-1000", Phase: "verify", PreviousPhase: "draft"},
		{Timestamp: ts(20), Kind: KindPhaseChange, RecipeID: "r-1000", Phase: "finalize", PreviousPhase: "verify"},
	}

	stats := AggregateRecipe(events)

	if len(stats.Phases) != 3 {
		t.Fatalf("Phases: got %d, want 3 (research→draft→verify)", len(stats.Phases))
	}

	// research: 0→5 = 5min
	if stats.Phases[0].Phase != "research" {
		t.Errorf("Phase[0]: got %s, want research", stats.Phases[0].Phase)
	}
	if stats.Phases[0].Duration != 5*time.Minute {
		t.Errorf("Phase[0] duration: got %s, want 5m", stats.Phases[0].Duration)
	}

	// draft: 5→15 = 10min
	if stats.Phases[1].Phase != "draft" {
		t.Errorf("Phase[1]: got %s, want draft", stats.Phases[1].Phase)
	}
	if stats.Phases[1].Duration != 10*time.Minute {
		t.Errorf("Phase[1] duration: got %s, want 10m", stats.Phases[1].Duration)
	}

	// verify: 15→20 = 5min
	if stats.Phases[2].Phase != "verify" {
		t.Errorf("Phase[2]: got %s, want verify", stats.Phases[2].Phase)
	}
	if stats.Phases[2].Duration != 5*time.Minute {
		t.Errorf("Phase[2] duration: got %s, want 5m", stats.Phases[2].Duration)
	}
}

func TestAggregateRecipe_ToolCounts(t *testing.T) {
	events := []MetricsEvent{
		{Kind: KindToolUse, ToolName: "Read", Success: ptr(true)},
		{Kind: KindToolUse, ToolName: "Read", Success: ptr(true)},
		{Kind: KindToolUse, ToolName: "Edit", Success: ptr(true)},
		{Kind: KindToolUse, ToolName: "Bash", Success: ptr(false)},
		{Kind: KindToolUse, ToolName: "Bash", Success: ptr(true)},
		{Kind: KindToolUse, ToolName: "Bash", Success: ptr(false)},
	}

	stats := AggregateRecipe(events)

	if stats.ToolCounts["Read"] != 2 {
		t.Errorf("Read count: got %d, want 2", stats.ToolCounts["Read"])
	}
	if stats.ToolCounts["Bash"] != 3 {
		t.Errorf("Bash count: got %d, want 3", stats.ToolCounts["Bash"])
	}
	if stats.ToolFailures["Bash"] != 2 {
		t.Errorf("Bash failures: got %d, want 2", stats.ToolFailures["Bash"])
	}
	if stats.ToolFailures["Read"] != 0 {
		t.Errorf("Read failures: got %d, want 0", stats.ToolFailures["Read"])
	}
}

func TestAggregateRecipe_TokensLatestSnapshot(t *testing.T) {
	// Token snapshots are cumulative. Only the last one matters.
	events := []MetricsEvent{
		{Kind: KindTokenSnapshot, SessionID: "s1", Tokens: &TokenSnapshot{InputTokens: 10000, OutputTokens: 1000}},
		{Kind: KindTokenSnapshot, SessionID: "s1", Tokens: &TokenSnapshot{InputTokens: 50000, OutputTokens: 3000}},
		{Kind: KindTokenSnapshot, SessionID: "s2", Tokens: &TokenSnapshot{InputTokens: 80000, OutputTokens: 4000}},
	}

	stats := AggregateRecipe(events)

	// Should be the LAST snapshot only (80K), not a sum
	if stats.TokensTotal.InputTokens != 80000 {
		t.Errorf("InputTokens: got %d, want 80000 (last snapshot)", stats.TokensTotal.InputTokens)
	}
	if stats.TokensTotal.OutputTokens != 4000 {
		t.Errorf("OutputTokens: got %d, want 4000", stats.TokensTotal.OutputTokens)
	}
}

func TestAggregateRecipe_TokensNoSessionID(t *testing.T) {
	// Statusline snapshots have no SessionID — should still be captured as latest
	events := []MetricsEvent{
		{Kind: KindTokenSnapshot, Tokens: &TokenSnapshot{InputTokens: 50000}},
		{Kind: KindTokenSnapshot, Tokens: &TokenSnapshot{InputTokens: 90000}},
	}

	stats := AggregateRecipe(events)
	if stats.TokensTotal.InputTokens != 90000 {
		t.Errorf("InputTokens: got %d, want 90000 (last snapshot)", stats.TokensTotal.InputTokens)
	}
}

func TestAggregateRecipe_Compacts(t *testing.T) {
	events := []MetricsEvent{
		{Kind: KindCompact, SessionID: "s1"},
		{Kind: KindCompact, SessionID: "s1"},
		{Kind: KindCompact, SessionID: "s2"},
	}

	stats := AggregateRecipe(events)
	if stats.TotalCompacts != 3 {
		t.Errorf("TotalCompacts: got %d, want 3", stats.TotalCompacts)
	}
}

func TestAggregateRecipe_EmptyEvents(t *testing.T) {
	stats := AggregateRecipe(nil)
	if stats.TotalSessions != 0 {
		t.Error("should handle nil events")
	}
	if stats.ToolCounts == nil {
		t.Error("ToolCounts should be initialized")
	}
}

func TestAggregateProject(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, ".forge", "state", "recipes"), 0755)

	// Create two recipes
	for _, id := range []string{"r-1000", "r-2000"} {
		dir := filepath.Join(root, ".forge", "state", "recipes", id)
		os.MkdirAll(dir, 0755)
		phase := "complete"
		if id == "r-2000" {
			phase = "draft"
		}
		r := &MetricsEvent{} // just for dir creation
		_ = r
		// Write recipe.json directly
		recipe := `{"id":"` + id + `","type":"blueprint","topic":"test","phase":"` + phase + `","started_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T01:00:00Z"}`
		os.WriteFile(filepath.Join(dir, "recipe.json"), []byte(recipe), 0644)
	}

	// Write global metrics
	events := []MetricsEvent{
		{Timestamp: ts(0), Kind: KindSessionStart, SessionID: "s1", Model: "claude-opus-4-6"},
		{Timestamp: ts(1), Kind: KindToolUse, SessionID: "s1", ToolName: "Read", Success: ptr(true)},
		{Timestamp: ts(2), Kind: KindToolUse, SessionID: "s1", ToolName: "Read", Success: ptr(true)},
		{Timestamp: ts(3), Kind: KindToolUse, SessionID: "s1", ToolName: "Bash", Success: ptr(false)},
		{Timestamp: ts(5), Kind: KindCompact, SessionID: "s1"},
		{Timestamp: ts(10), Kind: KindSessionStart, SessionID: "s2", Model: "claude-opus-4-6"},
		{Timestamp: ts(15), Kind: KindTokenSnapshot, SessionID: "s2", Tokens: &TokenSnapshot{InputTokens: 90000, OutputTokens: 5000}},
	}
	for i := range events {
		if err := AppendGlobal(root, &events[i]); err != nil {
			t.Fatalf("append event %d: %v", i, err)
		}
	}

	stats, err := AggregateProject(root)
	if err != nil {
		t.Fatalf("AggregateProject: %v", err)
	}

	if stats.TotalRecipes != 2 {
		t.Errorf("TotalRecipes: got %d, want 2", stats.TotalRecipes)
	}
	if stats.CompletedCount != 1 {
		t.Errorf("CompletedCount: got %d, want 1", stats.CompletedCount)
	}
	if stats.ActiveCount != 1 {
		t.Errorf("ActiveCount: got %d, want 1", stats.ActiveCount)
	}
	if stats.TotalSessions != 2 {
		t.Errorf("TotalSessions: got %d, want 2", stats.TotalSessions)
	}
	if stats.TotalCompacts != 1 {
		t.Errorf("TotalCompacts: got %d, want 1", stats.TotalCompacts)
	}
	if len(stats.Models) != 1 || stats.Models[0] != "claude-opus-4-6" {
		t.Errorf("Models: got %v, want [claude-opus-4-6]", stats.Models)
	}
	if stats.TotalTokens.InputTokens != 90000 {
		t.Errorf("TotalTokens.Input: got %d, want 90000", stats.TotalTokens.InputTokens)
	}

	// Tool stats
	if len(stats.TopTools) != 2 {
		t.Fatalf("TopTools: got %d, want 2", len(stats.TopTools))
	}
	// Read should be first (2 calls > 1)
	if stats.TopTools[0].Name != "Read" || stats.TopTools[0].Count != 2 {
		t.Errorf("TopTools[0]: got %s/%d, want Read/2", stats.TopTools[0].Name, stats.TopTools[0].Count)
	}
	if stats.TopTools[1].Name != "Bash" || stats.TopTools[1].FailRate != 100 {
		t.Errorf("TopTools[1]: got %s/%.0f%% fail, want Bash/100%%", stats.TopTools[1].Name, stats.TopTools[1].FailRate)
	}
}

func TestAggregateProject_NoMetrics(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, ".forge", "state", "recipes"), 0755)

	stats, err := AggregateProject(root)
	if err != nil {
		t.Fatalf("AggregateProject: %v", err)
	}
	if stats.TotalRecipes != 0 {
		t.Errorf("TotalRecipes: got %d, want 0", stats.TotalRecipes)
	}
	if stats.TotalSessions != 0 {
		t.Errorf("TotalSessions: got %d, want 0", stats.TotalSessions)
	}
}

func TestAggregateProject_TokensLatestSnapshot(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, ".forge", "state", "recipes"), 0755)

	// Multiple snapshots — only last should be kept
	events := []MetricsEvent{
		{Kind: KindTokenSnapshot, Tokens: &TokenSnapshot{InputTokens: 50000}},
		{Kind: KindTokenSnapshot, Tokens: &TokenSnapshot{InputTokens: 90000}},
	}
	for i := range events {
		AppendGlobal(root, &events[i])
	}

	stats, err := AggregateProject(root)
	if err != nil {
		t.Fatalf("AggregateProject: %v", err)
	}

	// Should be 90000 (last snapshot), not 140000 (sum)
	if stats.TotalTokens.InputTokens != 90000 {
		t.Errorf("InputTokens: got %d, want 90000 (last snapshot)", stats.TotalTokens.InputTokens)
	}
}

func TestAggregateSessions_Basic(t *testing.T) {
	events := []MetricsEvent{
		{Timestamp: ts(0), Kind: KindSessionStart, SessionID: "s1", Model: "claude-opus-4-6", Source: "startup"},
		{Timestamp: ts(1), Kind: KindToolUse, SessionID: "s1", ToolName: "Read", Success: ptr(true)},
		{Timestamp: ts(2), Kind: KindToolUse, SessionID: "s1", ToolName: "Bash", Success: ptr(false)},
		{Timestamp: ts(5), Kind: KindCompact, SessionID: "s1"},
		{Timestamp: ts(10), Kind: KindSessionEnd, SessionID: "s1"},
		{Timestamp: ts(20), Kind: KindSessionStart, SessionID: "s2", Model: "claude-sonnet-4-6", Source: "resume"},
		{Timestamp: ts(25), Kind: KindToolUse, SessionID: "s2", ToolName: "Edit", Success: ptr(true)},
		{Timestamp: ts(30), Kind: KindSessionEnd, SessionID: "s2"},
	}

	sessions := AggregateSessions(events)
	if len(sessions) != 2 {
		t.Fatalf("got %d sessions, want 2", len(sessions))
	}

	s1 := sessions[0]
	if s1.SessionID != "s1" {
		t.Errorf("s1 ID: got %s", s1.SessionID)
	}
	if s1.Model != "claude-opus-4-6" {
		t.Errorf("s1 Model: got %s", s1.Model)
	}
	if s1.Source != "startup" {
		t.Errorf("s1 Source: got %s", s1.Source)
	}
	if s1.ToolCount != 2 {
		t.Errorf("s1 ToolCount: got %d, want 2", s1.ToolCount)
	}
	if s1.ToolFails != 1 {
		t.Errorf("s1 ToolFails: got %d, want 1", s1.ToolFails)
	}
	if s1.Compacts != 1 {
		t.Errorf("s1 Compacts: got %d, want 1", s1.Compacts)
	}
	if s1.Duration != 10*time.Minute {
		t.Errorf("s1 Duration: got %s, want 10m", s1.Duration)
	}

	s2 := sessions[1]
	if s2.Model != "claude-sonnet-4-6" {
		t.Errorf("s2 Model: got %s", s2.Model)
	}
	if s2.ToolCount != 1 {
		t.Errorf("s2 ToolCount: got %d, want 1", s2.ToolCount)
	}
}

func TestAggregateSessions_OrphanSnapshot(t *testing.T) {
	// Token snapshots without SessionID should be attributed to the most recent session.
	events := []MetricsEvent{
		{Timestamp: ts(0), Kind: KindSessionStart, SessionID: "s1", Model: "claude-opus-4-6"},
		{Timestamp: ts(1), Kind: KindTokenSnapshot, Tokens: &TokenSnapshot{InputTokens: 50000, OutputTokens: 3000}},
		{Timestamp: ts(2), Kind: KindTokenSnapshot, Tokens: &TokenSnapshot{InputTokens: 90000, OutputTokens: 5000}},
	}

	sessions := AggregateSessions(events)
	if len(sessions) != 1 {
		t.Fatalf("got %d sessions, want 1", len(sessions))
	}
	// Should have the LAST snapshot (90K)
	if sessions[0].Tokens.InputTokens != 90000 {
		t.Errorf("InputTokens: got %d, want 90000", sessions[0].Tokens.InputTokens)
	}
	// Cost should be calculated with opus pricing
	if sessions[0].Cost.Total <= 0 {
		t.Error("expected non-zero cost for opus session with tokens")
	}
}

func TestAggregateSessions_Cost(t *testing.T) {
	events := []MetricsEvent{
		{Timestamp: ts(0), Kind: KindSessionStart, SessionID: "s1", Model: "claude-opus-4-6"},
		{Kind: KindTokenSnapshot, SessionID: "s1", Tokens: &TokenSnapshot{
			InputTokens: 100_000, OutputTokens: 10_000,
			CacheReadTokens: 200_000, CacheCreationTokens: 50_000,
		}},
	}

	sessions := AggregateSessions(events)
	if len(sessions) != 1 {
		t.Fatalf("got %d sessions, want 1", len(sessions))
	}
	// Same expected values as TestCalculateCost_Opus
	cost := sessions[0].Cost
	if !almostEqual(cost.Total, 1.1625) {
		t.Errorf("Total cost: got %f, want 1.1625", cost.Total)
	}
}

func TestAggregateSessions_MultiModel(t *testing.T) {
	events := []MetricsEvent{
		{Timestamp: ts(0), Kind: KindSessionStart, SessionID: "s1", Model: "claude-opus-4-6"},
		{Kind: KindTokenSnapshot, SessionID: "s1", Tokens: &TokenSnapshot{InputTokens: 100_000, OutputTokens: 10_000}},
		{Timestamp: ts(10), Kind: KindSessionEnd, SessionID: "s1"},
		{Timestamp: ts(20), Kind: KindSessionStart, SessionID: "s2", Model: "claude-sonnet-4-6"},
		{Kind: KindTokenSnapshot, SessionID: "s2", Tokens: &TokenSnapshot{InputTokens: 100_000, OutputTokens: 10_000}},
		{Timestamp: ts(30), Kind: KindSessionEnd, SessionID: "s2"},
	}

	// Use AggregateRecipe to test CostByModel
	stats := AggregateRecipe(events)
	if len(stats.CostByModel) != 2 {
		t.Fatalf("CostByModel: got %d models, want 2", len(stats.CostByModel))
	}

	opusCost := stats.CostByModel["claude-opus-4-6"]
	sonnetCost := stats.CostByModel["claude-sonnet-4-6"]

	// Opus: 100K * $5/MTok + 10K * $25/MTok = $0.50 + $0.25 = $0.75
	if !almostEqual(opusCost.Total, 0.75) {
		t.Errorf("Opus total: got %f, want 0.75", opusCost.Total)
	}
	// Sonnet: 100K * $3/MTok + 10K * $15/MTok = $0.30 + $0.15 = $0.45
	if !almostEqual(sonnetCost.Total, 0.45) {
		t.Errorf("Sonnet total: got %f, want 0.45", sonnetCost.Total)
	}
	// Recipe total
	if !almostEqual(stats.TotalCost.Total, 1.20) {
		t.Errorf("Recipe total: got %f, want 1.20", stats.TotalCost.Total)
	}
}

func TestAggregateProject_WithCost(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, ".forge", "state", "recipes"), 0755)

	events := []MetricsEvent{
		{Timestamp: ts(0), Kind: KindSessionStart, SessionID: "s1", Model: "claude-opus-4-6"},
		{Kind: KindTokenSnapshot, SessionID: "s1", Tokens: &TokenSnapshot{InputTokens: 100_000, OutputTokens: 10_000}},
		{Timestamp: ts(10), Kind: KindSessionEnd, SessionID: "s1"},
	}
	for i := range events {
		AppendGlobal(root, &events[i])
	}

	stats, err := AggregateProject(root)
	if err != nil {
		t.Fatalf("AggregateProject: %v", err)
	}

	if stats.TotalCost.Total <= 0 {
		t.Error("expected non-zero TotalCost")
	}
	if len(stats.RecentSessions) != 1 {
		t.Errorf("RecentSessions: got %d, want 1", len(stats.RecentSessions))
	}
	if stats.RecentSessions[0].SessionID != "s1" {
		t.Errorf("RecentSessions[0].SessionID: got %s, want s1", stats.RecentSessions[0].SessionID)
	}
}

func TestReadEvents_MalformedLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.jsonl")
	content := `{"ts":"2026-01-01T00:00:00Z","kind":"session_start","session_id":"s1"}
{invalid json line
{"ts":"2026-01-01T00:01:00Z","kind":"session_end","session_id":"s1"}
`
	os.WriteFile(path, []byte(content), 0644)

	events, err := ReadEvents(path)
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}
	if len(events) != 2 {
		t.Errorf("got %d events, want 2 (malformed line skipped)", len(events))
	}
}

func TestReadEvents_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.jsonl")
	os.WriteFile(path, []byte(""), 0644)

	events, err := ReadEvents(path)
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("got %d events, want 0", len(events))
	}
}

func TestReadEvents_NotFound(t *testing.T) {
	_, err := ReadEvents("/nonexistent/path.jsonl")
	if err == nil {
		t.Error("expected error for missing file")
	}
}
