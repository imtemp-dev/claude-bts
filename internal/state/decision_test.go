package state

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupDecisionRoot(t *testing.T) (root, recipeID string) {
	t.Helper()
	root = t.TempDir()
	recipeID = "r-dec"
	if err := os.MkdirAll(RecipeDir(root, recipeID), 0755); err != nil {
		t.Fatal(err)
	}
	return root, recipeID
}

func hold(t *testing.T, root, recipeID, key, question string) bool {
	t.Helper()
	created, err := HoldDecision(root, recipeID, &DecisionEvent{Key: key, Question: question})
	if err != nil {
		t.Fatalf("hold %s: %v", key, err)
	}
	return created
}

func TestHoldDecision_CreatesOpenDecision(t *testing.T) {
	root, recipeID := setupDecisionRoot(t)
	if !hold(t, root, recipeID, "token-storage", "keychain or cookie?") {
		t.Fatal("first hold should create")
	}
	open, err := OpenDecisions(root, recipeID)
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 1 || open[0].Key != "token-storage" {
		t.Fatalf("expected one open decision, got %+v", open)
	}
	if open[0].Raised == "" {
		t.Error("Raised must be stamped so age is knowable")
	}
}

// A retried skill step must not multiply the ledger.
func TestHoldDecision_ExactRepeatIsIdempotent(t *testing.T) {
	root, recipeID := setupDecisionRoot(t)
	hold(t, root, recipeID, "k", "same question")
	if created := hold(t, root, recipeID, "k", "same question"); created {
		t.Fatal("an exact repeat must not create a second event")
	}
	events, _ := ReadDecisionEvents(root, recipeID)
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
}

// Reusing a key for a different question is a second decision wearing the
// first one's identity — silently overwriting would lose the original.
func TestHoldDecision_KeyCollisionRejected(t *testing.T) {
	root, recipeID := setupDecisionRoot(t)
	hold(t, root, recipeID, "k", "first question")
	_, err := HoldDecision(root, recipeID, &DecisionEvent{Key: "k", Question: "different question"})
	if err == nil {
		t.Fatal("a key collision must be rejected")
	}
	if !strings.Contains(err.Error(), "first question") {
		t.Errorf("error must quote the existing question so the caller can tell them apart, got: %v", err)
	}
}

// The answer is history; reopening the same key would lose it.
func TestHoldDecision_ReopeningResolvedRejected(t *testing.T) {
	root, recipeID := setupDecisionRoot(t)
	hold(t, root, recipeID, "k", "q")
	if err := ResolveDecision(root, recipeID, "k", "the answer"); err != nil {
		t.Fatal(err)
	}
	_, err := HoldDecision(root, recipeID, &DecisionEvent{Key: "k", Question: "q"})
	if err == nil || !strings.Contains(err.Error(), "the answer") {
		t.Fatalf("reopening a resolved decision must be refused and cite the answer, got: %v", err)
	}
}

func TestResolveDecision_ClearsOpenState(t *testing.T) {
	root, recipeID := setupDecisionRoot(t)
	hold(t, root, recipeID, "k", "q")
	if err := ResolveDecision(root, recipeID, "k", "httpOnly cookie"); err != nil {
		t.Fatal(err)
	}
	open, _ := OpenDecisions(root, recipeID)
	if len(open) != 0 {
		t.Fatalf("resolved decision must not stay open, got %+v", open)
	}
	all, _ := LoadDecisions(root, recipeID)
	if len(all) != 1 || all[0].Answer != "httpOnly cookie" {
		t.Fatalf("the answer must be retained in the folded state, got %+v", all)
	}
}

func TestResolveDecision_RequiresAnExistingOpenKey(t *testing.T) {
	root, recipeID := setupDecisionRoot(t)
	if err := ResolveDecision(root, recipeID, "nope", "answer"); err == nil {
		t.Fatal("resolving an unknown key would answer a question nobody asked")
	}
	hold(t, root, recipeID, "k", "q")
	if err := ResolveDecision(root, recipeID, "k", "  "); err == nil {
		t.Fatal("an empty answer must be refused")
	}
}

// Drop exists so retiring a question never has to fabricate an answer.
func TestDropDecision_RetiresWithoutAnswer(t *testing.T) {
	root, recipeID := setupDecisionRoot(t)
	hold(t, root, recipeID, "k", "q")
	if err := DropDecision(root, recipeID, "k", "scope changed, no longer relevant"); err != nil {
		t.Fatal(err)
	}
	open, _ := OpenDecisions(root, recipeID)
	if len(open) != 0 {
		t.Fatal("a dropped decision must not stay open")
	}
	all, _ := LoadDecisions(root, recipeID)
	if all[0].Status != DecisionDropped || all[0].Answer != "" {
		t.Fatalf("drop must record no answer, got %+v", all[0])
	}
	if err := DropDecision(root, recipeID, "k", ""); err == nil {
		t.Fatal("dropping without a reason must be refused")
	}
}

// The ledger is append-only and folded; the latest event per key wins.
func TestFoldDecisions_LatestEventWinsAndRaisedIsPreserved(t *testing.T) {
	events := []DecisionEvent{
		{Key: "a", Question: "q", Status: DecisionOpen, Timestamp: "2026-01-01T00:00:00Z"},
		{Key: "b", Question: "q2", Status: DecisionOpen, Timestamp: "2026-01-02T00:00:00Z"},
		{Key: "a", Question: "q", Status: DecisionResolved, Answer: "x", Timestamp: "2026-01-03T00:00:00Z"},
	}
	folded := FoldDecisions(events)
	if len(folded) != 2 {
		t.Fatalf("expected 2 decisions, got %d", len(folded))
	}
	// Sorted by key, so "a" is first.
	if folded[0].Status != DecisionResolved || folded[0].Answer != "x" {
		t.Errorf("latest event must win, got %+v", folded[0])
	}
	if folded[0].Raised != "2026-01-01T00:00:00Z" {
		t.Errorf("Raised must survive later events, got %s", folded[0].Raised)
	}
	if folded[0].Updated != "2026-01-03T00:00:00Z" {
		t.Errorf("Updated must track the latest event, got %s", folded[0].Updated)
	}
}

// A corrupt line must not hide the rest of the ledger.
func TestReadDecisionEvents_SkipsCorruptLines(t *testing.T) {
	root, recipeID := setupDecisionRoot(t)
	path := DecisionsPath(root, recipeID)
	content := `{"key":"a","question":"q","status":"open","timestamp":"2026-01-01T00:00:00Z"}
not json at all
{"key":"b","question":"q2","status":"open","timestamp":"2026-01-02T00:00:00Z"}
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	events, err := ReadDecisionEvents(root, recipeID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("expected the 2 readable events, got %d", len(events))
	}
}

func TestReadDecisionEvents_MissingLedgerIsEmpty(t *testing.T) {
	root, recipeID := setupDecisionRoot(t)
	events, err := ReadDecisionEvents(root, recipeID)
	if err != nil {
		t.Fatalf("a missing ledger is empty history, not an error: %v", err)
	}
	if len(events) != 0 {
		t.Fatal("expected no events")
	}
}

// The ledger belongs to the recipe's tracked directory: a decision that
// shaped a spec is part of that spec's provenance.
func TestDecisionsPath_IsTracked(t *testing.T) {
	got := DecisionsPath("/p", "r-1")
	want := filepath.Join("/p", ".bts", "specs", "recipes", "r-1", "decisions.jsonl")
	if got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}
