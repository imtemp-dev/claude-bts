package engine

import (
	"fmt"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

// Convergence budget — the machine-enforced half of
// `bts-verification-protocol.md § Convergence`.
//
// The protocol has always said "N consecutive IMPROVE→VERIFY cycles with
// no progress → [CONVERGENCE FAILED], ask human", and settings.yaml has
// always carried verify.max_iterations. Nothing read it: the cap was
// prose the model was expected to enforce by counting its own rounds,
// and measured recipes ran 15+ verify iterations against a cap of 3.
// This file makes the budget a computation over verify-log.jsonl.
//
// Progress is measured lexicographically on (critical, major,
// minor_resolvable) — the same triple the completion gate requires to
// reach (0,0,0). A round makes progress only if it beats the best triple
// seen so far in this document's history. Trading a critical for three
// majors is progress; trading a major for a minor is not regression but
// is not progress either. Deferred minors and info are excluded: they
// never block completion, so churn in them must not reset the budget.

// ProgressKey is the ordered triple that defines verification progress.
type ProgressKey struct {
	Critical        int
	Major           int
	MinorResolvable int
}

// ProgressKeyOf extracts the comparable triple from a log entry,
// honouring the legacy pre-split Minor field via EffectiveResolvable.
func ProgressKeyOf(e *state.VerifyLogEntry) ProgressKey {
	return ProgressKey{
		Critical:        e.Critical,
		Major:           e.Major,
		MinorResolvable: e.EffectiveResolvable(),
	}
}

// Better reports whether k is strictly closer to (0,0,0) than other,
// comparing critical first, then major, then resolvable minors.
func (k ProgressKey) Better(other ProgressKey) bool {
	if k.Critical != other.Critical {
		return k.Critical < other.Critical
	}
	if k.Major != other.Major {
		return k.Major < other.Major
	}
	return k.MinorResolvable < other.MinorResolvable
}

// Clean reports whether the triple satisfies the completion gate.
func (k ProgressKey) Clean() bool {
	return k.Critical == 0 && k.Major == 0 && k.MinorResolvable == 0
}

func (k ProgressKey) String() string {
	return fmt.Sprintf("critical=%d major=%d minor_resolvable=%d",
		k.Critical, k.Major, k.MinorResolvable)
}

// NoProgressStreak counts how many trailing entries failed to improve on
// the best triple seen before them. A log whose last entry set a new
// best returns 0. An empty log returns 0.
//
// Callers should pass entries already narrowed to a single document via
// state.VerifyEntriesForDoc — a wireframe round must not reset the
// draft's budget, which is exactly what the undifferentiated log did.
func NoProgressStreak(entries []state.VerifyLogEntry) int {
	streak := 0
	var best *ProgressKey
	for i := range entries {
		k := ProgressKeyOf(&entries[i])
		if best == nil || k.Better(*best) {
			b := k
			best = &b
			streak = 0
			continue
		}
		streak++
	}
	return streak
}

// ConvergenceVerdict is the outcome of applying the budget to a log.
type ConvergenceVerdict struct {
	Streak    int         // consecutive rounds without progress
	Budget    int         // verify.max_iterations in effect
	Exceeded  bool        // streak >= budget (and budget > 0)
	Best      ProgressKey // best triple reached
	Latest    ProgressKey // triple of the most recent round
	Stagnant  []string    // finding IDs unchanged across the streak
	Iteration int         // iteration number of the most recent round
}

// Message renders the operator-facing [CONVERGENCE FAILED] report.
func (v ConvergenceVerdict) Message(docBase string) string {
	msg := fmt.Sprintf(
		"[CONVERGENCE FAILED] %s: %d consecutive verify rounds without progress (budget: verify.max_iterations=%d).\n"+
			"  best reached : %s\n"+
			"  latest round : %s\n",
		docBase, v.Streak, v.Budget, v.Best, v.Latest)
	if len(v.Stagnant) > 0 {
		msg += fmt.Sprintf("  stagnant findings (unresolved across the streak): %v\n", v.Stagnant)
	}
	msg += "  Further IMPROVE→VERIFY cycles are not converging. Stop the loop and ask the user for guidance\n" +
		"  (per bts-verification-protocol.md § Convergence)."
	return msg
}

// EvaluateConvergence applies the budget to one document's history.
// budget <= 0 disables the check (Exceeded is always false), matching
// the settings normalisation that treats non-positive values as unset.
func EvaluateConvergence(entries []state.VerifyLogEntry, budget int) ConvergenceVerdict {
	v := ConvergenceVerdict{Budget: budget}
	if len(entries) == 0 {
		return v
	}
	v.Streak = NoProgressStreak(entries)
	last := &entries[len(entries)-1]
	v.Latest = ProgressKeyOf(last)
	v.Iteration = last.Iteration

	best := ProgressKeyOf(&entries[0])
	for i := range entries {
		if k := ProgressKeyOf(&entries[i]); k.Better(best) {
			best = k
		}
	}
	v.Best = best

	// A clean document is converged, never "failed" — the streak counts
	// rounds that could not improve on (0,0,0), which is unimprovable.
	v.Exceeded = budget > 0 && v.Streak >= budget && !v.Latest.Clean()
	return v
}
