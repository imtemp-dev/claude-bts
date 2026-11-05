package state

import (
	"reflect"
	"testing"
)

// Holding an open decision again with the same key and question was an
// "exact repeat" whatever doc, options or blocks it carried, so a wrong
// blocks list could not be corrected (trial problem #12, Multica AGEN-63).
// The repeat now replaces what it sets; one that sets nothing new changes
// nothing, and a different question is still refused.
func TestHoldDecision_RepeatCorrectsDocOptionsAndBlocks(t *testing.T) {
	root, recipeID := setupDecisionRoot(t)
	open := func() DecisionState {
		t.Helper()
		ds, err := LoadDecisions(root, recipeID)
		if err != nil || len(ds) != 1 {
			t.Fatalf("one decision expected: %+v, %v", ds, err)
		}
		return ds[0]
	}
	if _, err := HoldDecision(root, recipeID, &DecisionEvent{
		Key: "k", Question: "q", Doc: "final.md", Options: []string{"a", "b"}, Blocks: []string{"F-1"},
	}); err != nil {
		t.Fatal(err)
	}
	raised := open().Raised

	if _, err := HoldDecision(root, recipeID, &DecisionEvent{
		Key: "k", Question: "q", Doc: "domain.md", Options: []string{"a", "c"}, Blocks: []string{"F-2", "t-003"},
	}); err != nil {
		t.Fatal(err)
	}
	got := open()
	if got.Doc != "domain.md" || !reflect.DeepEqual(got.Options, []string{"a", "c"}) || !reflect.DeepEqual(got.Blocks, []string{"F-2", "t-003"}) {
		t.Fatalf("the repeat must correct doc, options and blocks: %+v", got)
	}
	if got.Status != DecisionOpen || got.Raised != raised {
		t.Fatalf("still open, raised when first held: %+v", got)
	}

	if _, err := HoldDecision(root, recipeID, &DecisionEvent{Key: "k", Question: "q"}); err != nil {
		t.Fatal(err)
	}
	if got := open(); !reflect.DeepEqual(got.Blocks, []string{"F-2", "t-003"}) || got.Doc != "domain.md" {
		t.Fatalf("a repeat that sets nothing keeps what was there: %+v", got)
	}
	events, _ := ReadDecisionEvents(root, recipeID)
	if len(events) != 2 {
		t.Fatalf("only the correcting repeat is recorded, got %d events", len(events))
	}

	same := &DecisionEvent{Key: "k", Question: "q", Doc: "domain.md", Blocks: []string{"F-2", "t-003"}}
	if outcome, err := HoldOrCorrectDecision(root, recipeID, same); err != nil || outcome != HoldUnchanged {
		t.Fatalf("an identical repeat is unchanged, got %q, %v", outcome, err)
	}
	fix := &DecisionEvent{Key: "k", Question: "q", Blocks: []string{"F-2"}}
	if outcome, err := HoldOrCorrectDecision(root, recipeID, fix); err != nil || outcome != HoldCorrected {
		t.Fatalf("a repeat with other blocks corrects, got %q, %v", outcome, err)
	}
}
