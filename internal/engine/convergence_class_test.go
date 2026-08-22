package engine

import (
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

func round(iter int, c, m, mr int, dims []string, full bool) state.VerifyLogEntry {
	return state.VerifyLogEntry{
		Iteration: iter, Doc: "draft.md",
		Critical: c, Major: m, MinorResolvable: mr,
		Dimensions: dims, FullPass: full, Status: "continue",
	}
}

// The defect this replaces, reproduced from the measured recipe: a
// verify-only round set the best triple at (0,0,2), and every later
// round that also ran audit and simulate was judged "no progress"
// against a number one instrument had produced. The operator raised
// verify.max_iterations twice to escape a verdict that was an artefact.
func TestNoProgressStreak_WeakRoundDoesNotAnchorStrongRounds(t *testing.T) {
	verifyOnly := []string{"verify"}
	entries := []state.VerifyLogEntry{
		round(1, 0, 8, 8, verifyOnly, true),
		round(2, 0, 0, 2, verifyOnly, true), // weak instrument, small number
		round(3, 0, 6, 4, allDims, true),    // three instruments, first of its class
		round(4, 0, 3, 3, allDims, true),    // improves on its own class
		round(5, 0, 20, 27, allDims, true),  // regresses within its class
	}
	if got := NoProgressStreak(entries); got != 1 {
		t.Fatalf("streak = %d, want 1 — rounds 3 and 4 each set a new best for their own class", got)
	}

	// Same log with the class information stripped: the old behaviour,
	// where the weak round's (0,0,2) is a target nothing can beat.
	legacy := make([]state.VerifyLogEntry, len(entries))
	copy(legacy, entries)
	for i := range legacy {
		legacy[i].Dimensions = nil
	}
	if got := NoProgressStreak(legacy); got != 3 {
		t.Fatalf("legacy streak = %d, want 3 — this is the behaviour the class fix removes", got)
	}
}

// A clean delta round must not become an anchor for full passes. In the
// measured recipe a delta round recorded (0,0,0) and the very next full
// pass over the same bytes found two criticals.
func TestNoProgressStreak_DeltaAndFullAreDifferentClasses(t *testing.T) {
	entries := []state.VerifyLogEntry{
		round(1, 0, 2, 6, allDims, false), // delta
		round(2, 0, 0, 0, allDims, false), // delta, clean
		round(3, 2, 9, 13, allDims, true), // full pass, same bytes
		round(4, 0, 7, 21, allDims, true), // full, improves on its class
	}
	if got := NoProgressStreak(entries); got != 0 {
		t.Fatalf("streak = %d, want 0 — round 4 is the best full pass so far", got)
	}
}

// The streak stays global: alternating instruments every round must not
// be a way to keep the budget at zero forever.
func TestNoProgressStreak_AlternatingClassesStillAccumulate(t *testing.T) {
	a, b := []string{"verify"}, []string{"audit"}
	entries := []state.VerifyLogEntry{
		round(1, 0, 1, 1, a, true), // baseline for the document
		round(2, 0, 1, 1, b, true), // new class: a baseline, not progress
		round(3, 0, 5, 5, a, true), // worse than round 1's class best
		round(4, 0, 5, 5, b, true), // worse than round 2's class best
		round(5, 0, 5, 5, a, true),
	}
	if got := NoProgressStreak(entries); got != 4 {
		t.Fatalf("streak = %d, want 4 — only round 1 is a baseline; every later round failed to improve", got)
	}
}

// The regression that made the per-class best worse than the defect it
// replaced. Treating a class's first sighting as progress reset the
// streak, and since the protocol asks the verifier to declare only the
// dimensions it actually ran, a rotating class is the NORMAL case — so
// the budget stopped firing in ordinary operation. Nine rounds, each
// strictly worse than the last, must exhaust a budget of 3.
func TestNoProgressStreak_RotatingInstrumentsDoNotBuyRounds(t *testing.T) {
	classes := [][]string{
		{"verify"}, {"audit"}, {"simulate"},
		{"audit", "verify"}, {"simulate", "verify"}, {"audit", "simulate"},
		allDims, {"verify"}, {"audit"},
	}
	var entries []state.VerifyLogEntry
	for i, dims := range classes {
		// Strictly worsening: nothing here is progress under any reading.
		entries = append(entries, round(i+1, 0, i+1, i+1, dims, true))
	}
	if got := NoProgressStreak(entries); got != 8 {
		t.Fatalf("streak = %d, want 8 — rotating instruments must not reset the budget", got)
	}
	if v := EvaluateConvergence(entries, 3); !v.Exceeded {
		t.Fatalf("budget of 3 must be exceeded after 9 worsening rounds, got %+v", v)
	}

	// And the same worsening history inside one class gives the same
	// answer: the class no longer changes what the budget counts.
	var single []state.VerifyLogEntry
	for i := range classes {
		single = append(single, round(i+1, 0, i+1, i+1, allDims, true))
	}
	if got := NoProgressStreak(single); got != 8 {
		t.Fatalf("single-class streak = %d, want 8", got)
	}
}

// A new class still must not inherit another class's best as a target:
// that is the defect the per-class map exists to fix, and the streak
// change must not quietly undo it.
func TestNoProgressStreak_NewClassIsJudgedAgainstItsOwnBaseline(t *testing.T) {
	weak, strong := []string{"verify"}, allDims
	entries := []state.VerifyLogEntry{
		round(1, 0, 0, 1, weak, true),   // weak instrument, small number
		round(2, 0, 9, 9, strong, true), // first strong round: baseline, streak 1
		round(3, 0, 4, 4, strong, true), // beats its own class best -> reset
	}
	if got := NoProgressStreak(entries); got != 0 {
		t.Fatalf("streak = %d, want 0 — round 3 improved on the only comparable round", got)
	}
}

// The failure message must name the measurement, so the operator can
// see whether the target it cites was ever reachable.
func TestConvergenceMessageNamesTheMeasurementClass(t *testing.T) {
	entries := []state.VerifyLogEntry{
		round(1, 0, 1, 1, allDims, true),
		round(2, 0, 5, 5, allDims, true),
		round(3, 0, 5, 5, allDims, true),
	}
	v := EvaluateConvergence(entries, 2)
	if !v.Exceeded {
		t.Fatalf("want exceeded, got %+v", v)
	}
	msg := v.Message("draft.md")
	if !strings.Contains(msg, "audit+simulate+verify/full") {
		t.Errorf("message should name the measurement class, got:\n%s", msg)
	}
	if !strings.Contains(msg, "same measurement") {
		t.Errorf("message should say the best came from a comparable round, got:\n%s", msg)
	}
}

func TestStrengthClassAndDimensionNormalisation(t *testing.T) {
	dims, err := state.NormalizeDimensions([]string{"Simulate", "verify", "verify"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(dims, ",") != "simulate,verify" {
		t.Errorf("normalised = %v, want [simulate verify]", dims)
	}
	if _, err := state.NormalizeDimensions([]string{"lint"}); err == nil {
		t.Error("an unknown dimension must be rejected rather than silently recorded")
	}

	e := state.VerifyLogEntry{Dimensions: dims, FullPass: true}
	if got := e.StrengthClass(); got != "simulate+verify/full" {
		t.Errorf("class = %q", got)
	}
	if e.HasAllDimensions() {
		t.Error("simulate+verify is not all dimensions")
	}
	legacy := state.VerifyLogEntry{FullPass: false}
	if got := legacy.StrengthClass(); got != "?/delta" {
		t.Errorf("legacy class = %q, want ?/delta", got)
	}
}
