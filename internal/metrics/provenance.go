package metrics

import (
	"os"
	"time"
)

// Gate-evidence provenance.
//
// The gate skills (verify, audit, simulate, cross-check, sync-check,
// assess) run with `context: fork` and spawn their own agent, so the
// isolation itself is enforced by the harness. What was never tied
// together is the RECORD: the orchestrator writes verification.md and
// runs `bts recipe log`, and a round logged without any fork having run
// looked exactly like a round logged after one.
//
// SubagentStop events are the cheapest available witness. They are
// evidence, not proof — and their absence is not an accusation, because a
// harness that does not emit them produces the same absence. Callers must
// treat "none" as informative only when the same project has also
// produced "observed" rounds.

// SubagentActivitySince reports whether any subagent finished after the
// given instant. A zero `since` matches the whole log.
//
// Read errors report false with ok=false so a caller can distinguish
// "no activity" from "could not tell" and record neither.
func SubagentActivitySince(root, recipeID string, since time.Time) (active bool, ok bool) {
	events, err := ReadRecipeEvents(root, recipeID)
	if err != nil {
		// An absent log is empty history — a project that has not run
		// anything yet is readable, and reporting it as unreadable would
		// silently drop the claim on every first round.
		if os.IsNotExist(err) {
			return false, true
		}
		return false, false
	}
	for i := range events {
		if events[i].Kind != KindSubagentStop {
			continue
		}
		ts, perr := time.Parse(time.RFC3339, events[i].Timestamp)
		if perr != nil {
			continue
		}
		if since.IsZero() || ts.After(since) {
			return true, true
		}
	}
	return false, true
}
