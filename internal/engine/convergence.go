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
// minor_resolvable). Trading a critical for three majors is progress;
// trading a major for a minor is not regression but is not progress
// either. Deferred minors and info are excluded: they never block
// completion, so churn in them must not reset the budget.
//
// A round makes progress only if it beats the best triple reached by an
// earlier round of the SAME measurement class — same dimensions, same
// scope. A triple carries "the document got better" only against another
// triple produced by the same instruments; see NoProgressStreak and
// state.VerifyLogEntry.StrengthClass for what went wrong without that.
// The first round of a class has no such predecessor, so it sets that
// class's baseline and leaves the streak where it was.
//
// The clean triple (0,0,0) is necessary for completion but no longer
// sufficient — the gate also requires every dimension, a full pass, and
// replication on one revision. See completion_evidence.go.

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

// Clean reports whether the triple is at (0,0,0). That is necessary for
// completion, not sufficient: the gate also asks which instruments
// produced it, over how much of the document, and whether an independent
// round agreed. See completion_evidence.go.
func (k ProgressKey) Clean() bool {
	return k.Critical == 0 && k.Major == 0 && k.MinorResolvable == 0
}

func (k ProgressKey) String() string {
	return fmt.Sprintf("critical=%d major=%d minor_resolvable=%d",
		k.Critical, k.Major, k.MinorResolvable)
}

// NoProgressStreak counts how many trailing rounds failed to demonstrate
// progress. A round demonstrates progress only by strictly beating the
// best triple reached by an earlier round of the SAME measurement class.
// An empty log returns 0.
//
// The per-class best is the correction that makes the budget mean what
// the protocol says it means. Progress is "this document got better",
// and a triple only carries that meaning against another triple
// produced by the same instruments over the same scope. Comparing a
// three-dimension round against a one-dimension round's best measures
// the instrument, not the document — and because the weaker instrument
// reliably reports the smaller number, its best becomes a target no
// honest round can ever beat. See state.VerifyLogEntry.StrengthClass.
//
// The streak is global and a class's first sighting neither resets it
// nor advances it. Both halves of that matter.
//
// Not resetting is what keeps the budget honest: the protocol asks the
// verifier to declare only the dimensions it actually ran, so rotating
// instruments is ordinary, and a rule that reset on novelty would make
// "measure differently" a way to buy unlimited rounds.
//
// Not advancing is what keeps it usable. A first sighting is not a
// failure to improve — there was nothing to improve on. Counting it
// would mean three honest single-dimension rounds exhaust a default
// budget of 3 before any measurement has repeated, and the loop stops to
// ask the user on a document that is getting better. Rotation therefore
// DELAYS the budget by at most the number of distinct classes and cannot
// prevent it: once the instruments stop being new, every round that
// fails to beat its own class counts.
//
// Callers should pass entries already narrowed to a single document via
// state.VerifyEntriesForDoc — a wireframe round must not reset the
// draft's budget, which is exactly what the undifferentiated log did.
func NoProgressStreak(entries []state.VerifyLogEntry) int {
	streak := 0
	best := make(map[string]ProgressKey, 4)
	for i := range entries {
		class := entries[i].StrengthClass()
		k := ProgressKeyOf(&entries[i])
		b, seen := best[class]
		switch {
		case !seen:
			// No same-class predecessor: this round has nothing it could
			// have improved on. Record the baseline and HOLD the streak —
			// neither reset nor advanced.
			best[class] = k
		case k.Better(b):
			best[class] = k
			streak = 0
		default:
			streak++
		}
	}
	return streak
}

// BestForClass returns the best triple reached by rounds of the given
// measurement class, and whether any round of that class exists.
func BestForClass(entries []state.VerifyLogEntry, class string) (ProgressKey, bool) {
	var best ProgressKey
	found := false
	for i := range entries {
		if entries[i].StrengthClass() != class {
			continue
		}
		k := ProgressKeyOf(&entries[i])
		if !found || k.Better(best) {
			best, found = k, true
		}
	}
	return best, found
}

// ConvergenceVerdict is the outcome of applying the budget to a log.
type ConvergenceVerdict struct {
	Streak    int         // consecutive rounds without progress
	Budget    int         // verify.max_iterations in effect
	Exceeded  bool        // streak >= budget (and budget > 0)
	Best      ProgressKey // best triple reached by a round of Class
	Latest    ProgressKey // triple of the most recent round
	Stagnant  []string    // finding IDs unchanged across the streak
	Iteration int         // iteration number of the most recent round
	Class     string      // measurement class of the most recent round
}

// Message renders the operator-facing [CONVERGENCE FAILED] report.
func (v ConvergenceVerdict) Message(docBase string) string {
	msg := fmt.Sprintf(
		"[CONVERGENCE FAILED] %s: %d consecutive verify rounds without progress (budget: verify.max_iterations=%d).\n"+
			"  measurement  : %s (dimensions/scope)\n"+
			"  best reached : %s  — by a round of the same measurement\n"+
			"  latest round : %s\n",
		docBase, v.Streak, v.Budget, v.Class, v.Best, v.Latest)
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

	// The best worth reporting is the best a round of the LATEST round's
	// own class reached. Reporting a global best would name a target the
	// latest measurement was never judged against, which is how the
	// [CONVERGENCE FAILED] message came to cite (0,0,0) at an operator
	// who could see the document had never been measured that way.
	// entries is non-empty and Class is the last entry's own class, so
	// there is always at least one round of it to report.
	v.Class = last.StrengthClass()
	v.Best, _ = BestForClass(entries, v.Class)

	// A clean document is converged, never "failed" — the streak counts
	// rounds that could not improve on (0,0,0), which is unimprovable.
	v.Exceeded = budget > 0 && v.Streak >= budget && !v.Latest.Clean()
	return v
}
