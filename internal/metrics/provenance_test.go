package metrics

import (
	"testing"
	"time"
)

func writeEvents(t *testing.T, root, recipeID string, events ...MetricsEvent) {
	t.Helper()
	for i := range events {
		if err := Append(root, &events[i]); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
}

func TestSubagentActivitySince_DetectsStopAfterCutoff(t *testing.T) {
	root := t.TempDir()
	cutoff := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	writeEvents(t, root, "r-1",
		MetricsEvent{Kind: KindSubagentStop, RecipeID: "r-1",
			Timestamp: cutoff.Add(time.Minute).Format(time.RFC3339)},
	)
	active, ok := SubagentActivitySince(root, "r-1", cutoff)
	if !ok || !active {
		t.Fatalf("a stop after the cutoff is activity; got active=%v ok=%v", active, ok)
	}
}

// A subagent that finished BEFORE the previous round belongs to that
// round, not this one.
func TestSubagentActivitySince_IgnoresStopBeforeCutoff(t *testing.T) {
	root := t.TempDir()
	cutoff := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	writeEvents(t, root, "r-1",
		MetricsEvent{Kind: KindSubagentStop, RecipeID: "r-1",
			Timestamp: cutoff.Add(-time.Hour).Format(time.RFC3339)},
	)
	active, ok := SubagentActivitySince(root, "r-1", cutoff)
	if !ok {
		t.Fatal("expected a readable result")
	}
	if active {
		t.Fatal("a stop before the cutoff belongs to an earlier round")
	}
}

// Only subagent_stop counts; other traffic in the window is not evidence
// that a fork ran.
func TestSubagentActivitySince_OtherKindsAreNotEvidence(t *testing.T) {
	root := t.TempDir()
	cutoff := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	writeEvents(t, root, "r-1",
		MetricsEvent{Kind: KindToolUse, RecipeID: "r-1", ToolName: "Write",
			Timestamp: cutoff.Add(time.Minute).Format(time.RFC3339)},
		MetricsEvent{Kind: KindPhaseChange, RecipeID: "r-1",
			Timestamp: cutoff.Add(2 * time.Minute).Format(time.RFC3339)},
	)
	active, ok := SubagentActivitySince(root, "r-1", cutoff)
	if !ok || active {
		t.Fatalf("only subagent_stop is evidence; got active=%v", active)
	}
}

// A zero cutoff (first round for a document) matches the whole log.
func TestSubagentActivitySince_ZeroCutoffMatchesEverything(t *testing.T) {
	root := t.TempDir()
	writeEvents(t, root, "r-1",
		MetricsEvent{Kind: KindSubagentStop, RecipeID: "r-1",
			Timestamp: "2020-01-01T00:00:00Z"},
	)
	active, ok := SubagentActivitySince(root, "r-1", time.Time{})
	if !ok || !active {
		t.Fatalf("first round must see the whole log; got active=%v ok=%v", active, ok)
	}
}

// No metrics at all is a readable "no activity", not an error — a fresh
// project simply has not run anything yet.
func TestSubagentActivitySince_EmptyLogIsReadable(t *testing.T) {
	root := t.TempDir()
	active, ok := SubagentActivitySince(root, "r-1", time.Time{})
	if !ok {
		t.Fatal("an absent metrics log is empty history, not an unreadable one")
	}
	if active {
		t.Fatal("expected no activity")
	}
}
