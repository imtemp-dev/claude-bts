package state

import (
	"reflect"
	"testing"
)

// Trial problem #102: swift-eagle resolved a deferred finding at L4
// (F-778c0dc4, U-013) and its round listed it in confirmed_fixed, but only
// open and unreported findings could close, so it stayed a watch-item.
// The warning then named two causes, neither of which applied.
func TestConfirmFixedFindings_ClosesADeferredFinding(t *testing.T) {
	root, id := findingsRoot(t)
	if _, err := SyncFindings(root, id, legacyRound("final.md", 1), []ReportedFinding{
		{Severity: "minor_deferred", Title: "flag-off rollback assumes the model accepts an offer call"},
	}, 3); err != nil {
		t.Fatal(err)
	}
	deferred := FindingID("final.md", "flag-off rollback assumes the model accepts an offer call")
	round2 := legacyRound("final.md", 2)
	fixed, skipped, err := ConfirmFixedFindings(root, id, round2, []string{deferred}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(fixed, []string{deferred}) || len(skipped) != 0 {
		t.Fatalf("a deferred finding found fixed closes as fixed: fixed %v, skipped %v", fixed, skipped)
	}
}

func TestExplainConfirmSkips_NamesEachReason(t *testing.T) {
	root, id := findingsRoot(t)
	if _, err := SyncFindings(root, id, legacyRound("final.md", 1), []ReportedFinding{
		{Severity: "major", Title: "the switch has no answer for a failed start"},
		{Severity: "minor_resolvable", Title: "INV-008 claim overstated"},
	}, 3); err != nil {
		t.Fatal(err)
	}
	major := FindingID("final.md", "the switch has no answer for a failed start")
	again := FindingID("final.md", "INV-008 claim overstated")
	round2 := legacyRound("final.md", 2)
	reported := []ReportedFinding{{Severity: "minor_resolvable", Title: "INV-008 claim overstated"}}
	if _, _, err := ConfirmFixedFindings(root, id, round2, []string{major}, reported); err != nil {
		t.Fatal(err)
	}
	got := ExplainConfirmSkips(root, id, round2, []string{major, again, "F-unknown"}, reported)
	want := []string{
		major + " (already fixed)",
		again + " (reported again this round)",
		"F-unknown (no such finding on final.md)",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}
