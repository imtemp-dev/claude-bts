package cli

import (
	"reflect"
	"testing"
)

// `bts prove plan --journey X --inv ...` records which invariants the
// journey's run observes, adding to what an earlier plan named (trial
// problem #111).
func TestProvePlan_RecordsTheInvariantsAJourneyObserves(t *testing.T) {
	root := newRecipeFixture(t, "r-p111", "test", 0, 0, nil)
	if _, err := executeProvePlan(root, "r-p111", []string{"review-synthetic"}, "", "", "INV-018,INV-019"); err != nil {
		t.Fatal(err)
	}
	rec, err := executeProvePlan(root, "r-p111", []string{"review-synthetic"}, "", "", "INV-004", "INV-018")
	if err != nil {
		t.Fatal(err)
	}
	got := rec.Journeys["review-synthetic"].Invariants
	if want := []string{"INV-004", "INV-018", "INV-019"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if _, err := executeProvePlan(root, "r-p111", nil, "", "", "INV-004"); err == nil {
		t.Error("--inv without --journey names nothing it belongs to; refuse it")
	}
}
