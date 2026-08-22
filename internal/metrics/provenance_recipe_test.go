package metrics

import (
	"testing"
	"time"
)

// The defect: subagent hooks wrote no RecipeID, metrics.Append therefore
// skipped the per-recipe log, and SubagentActivitySince read only that
// log. AgentEvidence returned "none" on every round of every recipe. A
// measured project held 7,632 subagent events globally and 0 under its
// recipe. The fix is at the hook — subagent events now carry the active
// recipe — not in reading the global log loosely.
//
// An unattributed event in the GLOBAL log belongs to nobody in
// particular. Counting it would make agent_evidence a claim that the
// machine was busy rather than that this round forked, and it is exactly
// the shape a project produces before the hook fix lands, so it would
// manufacture "observed" for rounds nothing was watching.
func TestSubagentActivitySince_UnattributedGlobalEventIsNotEvidence(t *testing.T) {
	root := t.TempDir()
	if err := AppendGlobal(root, &MetricsEvent{
		Kind: KindSubagentStop, Timestamp: "2026-08-12T10:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	active, ok := SubagentActivitySince(root, "r-001", time.Time{})
	if !ok {
		t.Fatal("readable log reported unreadable")
	}
	if active {
		t.Error("a global subagent_stop naming no recipe must not count as this recipe's witness")
	}
}

// In the recipe's own log the file's location IS the attribution, so an
// event there counts even if the field is empty.
func TestSubagentActivitySince_ScopedLogNeedsNoExplicitAttribution(t *testing.T) {
	root := t.TempDir()
	if err := Append(root, &MetricsEvent{
		Kind: KindSubagentStop, RecipeID: "r-001", Timestamp: "2026-08-12T10:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	// Strip the field from the scoped copy the way a legacy writer would.
	events, err := ReadRecipeEvents(root, "r-001")
	if err != nil {
		t.Fatal(err)
	}
	for i := range events {
		events[i].RecipeID = ""
	}
	if !stopAfter(events, "r-001", time.Time{}, false) {
		t.Error("the recipe-scoped log is attribution by location")
	}
	if stopAfter(events, "r-001", time.Time{}, true) {
		t.Error("the global read must require the explicit field")
	}
}

// Another recipe's witness is not this one's.
func TestSubagentActivitySince_IgnoresOtherRecipes(t *testing.T) {
	root := t.TempDir()
	if err := AppendGlobal(root, &MetricsEvent{
		Kind: KindSubagentStop, RecipeID: "r-002", Timestamp: "2026-08-12T10:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	active, ok := SubagentActivitySince(root, "r-001", time.Time{})
	if !ok {
		t.Fatal("readable log reported unreadable")
	}
	if active {
		t.Error("a subagent belonging to r-002 must not be evidence for r-001")
	}
}

// Events attributed to the recipe are found via the per-recipe log,
// which is what the fixed hook now writes.
func TestSubagentActivitySince_FindsRecipeScopedEvents(t *testing.T) {
	root := t.TempDir()
	if err := Append(root, &MetricsEvent{
		Kind: KindSubagentStop, RecipeID: "r-001", Timestamp: "2026-08-12T10:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	cutoff, _ := time.Parse(time.RFC3339, "2026-08-12T09:00:00Z")
	active, ok := SubagentActivitySince(root, "r-001", cutoff)
	if !ok || !active {
		t.Errorf("active=%v ok=%v, want both true", active, ok)
	}
	later, _ := time.Parse(time.RFC3339, "2026-08-12T11:00:00Z")
	if active, _ := SubagentActivitySince(root, "r-001", later); active {
		t.Error("a stop before the cutoff must not count")
	}
}
