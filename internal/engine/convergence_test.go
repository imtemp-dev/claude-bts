package engine

import (
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

func entry(c, m, mr int) state.VerifyLogEntry {
	return state.VerifyLogEntry{Critical: c, Major: m, MinorResolvable: mr}
}

func TestNoProgressStreak(t *testing.T) {
	tests := []struct {
		name    string
		entries []state.VerifyLogEntry
		want    int
	}{
		{"empty", nil, 0},
		{"single round is progress", []state.VerifyLogEntry{entry(2, 3, 1)}, 0},
		{"monotonic improvement", []state.VerifyLogEntry{
			entry(2, 3, 1), entry(1, 3, 1), entry(0, 2, 1), entry(0, 0, 0),
		}, 0},
		{"flat after first", []state.VerifyLogEntry{
			entry(0, 3, 2), entry(0, 3, 2), entry(0, 3, 2),
		}, 2},
		{"regression counts as no progress", []state.VerifyLogEntry{
			entry(0, 1, 0), entry(1, 3, 4), entry(0, 2, 1),
		}, 2},
		{"trading critical for majors is progress", []state.VerifyLogEntry{
			entry(1, 0, 0), entry(0, 5, 5),
		}, 0},
		{"improvement resets the streak", []state.VerifyLogEntry{
			entry(0, 3, 0), entry(0, 3, 0), entry(0, 2, 0), entry(0, 2, 0),
		}, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NoProgressStreak(tt.entries); got != tt.want {
				t.Errorf("NoProgressStreak() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestNoProgressStreakUsesLegacyMinor(t *testing.T) {
	// Pre-split entries carry only Minor; EffectiveResolvable maps it to
	// resolvable so old logs are scored the same way as new ones.
	entries := []state.VerifyLogEntry{
		{Critical: 0, Major: 0, Minor: 3},
		{Critical: 0, Major: 0, Minor: 3},
	}
	if got := NoProgressStreak(entries); got != 1 {
		t.Errorf("legacy minor stream: streak = %d, want 1", got)
	}
}

func TestEvaluateConvergenceExceeded(t *testing.T) {
	entries := []state.VerifyLogEntry{
		entry(0, 3, 2), entry(0, 3, 2), entry(0, 3, 2), entry(0, 3, 2),
	}
	v := EvaluateConvergence(entries, 3)
	if !v.Exceeded {
		t.Fatalf("expected budget exceeded, got streak=%d budget=%d", v.Streak, v.Budget)
	}
	if v.Best != (ProgressKey{0, 3, 2}) {
		t.Errorf("best = %v, want {0 3 2}", v.Best)
	}
	msg := v.Message("draft.md")
	if !strings.Contains(msg, "[CONVERGENCE FAILED]") || !strings.Contains(msg, "draft.md") {
		t.Errorf("message missing marker or doc: %q", msg)
	}
}

func TestEvaluateConvergenceCleanIsNeverFailed(t *testing.T) {
	// A clean document cannot improve on (0,0,0), so repeated clean
	// rounds must read as converged, not as an exhausted budget.
	entries := []state.VerifyLogEntry{
		entry(0, 1, 0), entry(0, 0, 0), entry(0, 0, 0), entry(0, 0, 0), entry(0, 0, 0),
	}
	v := EvaluateConvergence(entries, 3)
	if v.Exceeded {
		t.Errorf("clean document marked as convergence failure (streak=%d)", v.Streak)
	}
	if !v.Latest.Clean() {
		t.Errorf("latest should be clean, got %v", v.Latest)
	}
}

func TestEvaluateConvergenceBudgetDisabled(t *testing.T) {
	entries := []state.VerifyLogEntry{entry(1, 1, 1), entry(1, 1, 1), entry(1, 1, 1), entry(1, 1, 1)}
	if v := EvaluateConvergence(entries, 0); v.Exceeded {
		t.Error("budget 0 must disable the check")
	}
}

func TestProgressKeyBetter(t *testing.T) {
	tests := []struct {
		a, b ProgressKey
		want bool
	}{
		{ProgressKey{0, 0, 0}, ProgressKey{0, 0, 1}, true},
		{ProgressKey{0, 5, 5}, ProgressKey{1, 0, 0}, true}, // critical dominates
		{ProgressKey{0, 3, 0}, ProgressKey{0, 2, 9}, false},
		{ProgressKey{1, 1, 1}, ProgressKey{1, 1, 1}, false}, // equal is not better
	}
	for _, tt := range tests {
		if got := tt.a.Better(tt.b); got != tt.want {
			t.Errorf("%v.Better(%v) = %v, want %v", tt.a, tt.b, got, tt.want)
		}
	}
}

// r-012 is the measured worst case: 15 rounds that oscillated and never
// converged. The budget must stop it, and stop it early.
func TestConvergenceHaltsTheMeasuredWorstCase(t *testing.T) {
	observed := [][3]int{
		{1, 3, 4}, {0, 3, 6}, {1, 3, 4}, {1, 3, 4}, {0, 1, 6}, {0, 3, 4},
		{0, 3, 6}, {0, 3, 6}, {1, 1, 6}, {1, 3, 7}, {0, 3, 5}, {0, 2, 5},
		{0, 1, 3}, {0, 1, 3}, {0, 3, 3},
	}
	var entries []state.VerifyLogEntry
	halted := 0
	for i, o := range observed {
		entries = append(entries, entry(o[0], o[1], o[2]))
		if EvaluateConvergence(entries, 3).Exceeded {
			halted = i + 1
			break
		}
	}
	if halted == 0 {
		t.Fatal("budget never halted the non-converging r-012 trace")
	}
	if halted > 8 {
		t.Errorf("halted at round %d, expected to stop the thrash well before round 8", halted)
	}
}

// The cap counts rounds and judges nothing. Every hole found in the
// progress budget — per-document, per-class, baseline staleness — was
// found only after a recipe had escaped through it; this backstop has
// no judgement to escape through.
func TestRoundCap(t *testing.T) {
	round := func(n, c, m, r int) state.VerifyLogEntry {
		return state.VerifyLogEntry{
			Iteration: n, Critical: c, Major: m, MinorResolvable: r,
			Dimensions: []string{"audit", "simulate", "verify"}, FullPass: true,
			Status: "continue",
		}
	}
	// Six rounds that improve every time: the progress budget is never
	// touched, and the cap fires anyway.
	var improving []state.VerifyLogEntry
	for i := 0; i < 6; i++ {
		improving = append(improving, round(i+1, 0, 10-i, 10-i))
	}
	v := EvaluateConvergenceWithCap(improving, 3, 6)
	if v.Streak != 0 {
		t.Fatalf("streak = %d, want 0 — every round improved", v.Streak)
	}
	if !v.CapHit {
		t.Errorf("cap must fire at %d rounds with max_rounds=6, got %+v", len(improving), v)
	}
	if v.Rounds != 6 || v.RoundCap != 6 {
		t.Errorf("Rounds/RoundCap = %d/%d, want 6/6", v.Rounds, v.RoundCap)
	}

	// Under the cap, nothing fires.
	if v := EvaluateConvergenceWithCap(improving[:5], 3, 6); v.CapHit {
		t.Error("cap fired at 5 rounds with max_rounds=6")
	}
	// A clean document is finished, not capped.
	clean := append(append([]state.VerifyLogEntry{}, improving...), round(7, 0, 0, 0))
	if v := EvaluateConvergenceWithCap(clean, 3, 6); v.CapHit {
		t.Error("a clean latest round must never be reported as capped")
	}
	// 0 disables.
	if v := EvaluateConvergenceWithCap(improving, 3, 0); v.CapHit {
		t.Error("max_rounds=0 must disable the cap")
	}
}
