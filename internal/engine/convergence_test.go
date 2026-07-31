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
