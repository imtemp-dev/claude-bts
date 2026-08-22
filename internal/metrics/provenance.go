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
// Two logs are consulted, not one. Subagent events written before the
// hook attached a RecipeID landed in the global log with no recipe on
// them, so a project upgrading mid-recipe would otherwise keep reporting
// "none" for its whole history. Global events that DO name a different
// recipe are skipped — they are another recipe's witness, not this
// one's.
func SubagentActivitySince(root, recipeID string, since time.Time) (active bool, ok bool) {
	scoped, err := ReadRecipeEvents(root, recipeID)
	if err != nil && !os.IsNotExist(err) {
		return false, false
	}
	if stopAfter(scoped, recipeID, since, false) {
		return true, true
	}

	global, gerr := ReadAllEvents(root)
	if gerr != nil {
		// An absent log is empty history — a project that has not run
		// anything yet is readable, and reporting it as unreadable would
		// silently drop the claim on every first round.
		if os.IsNotExist(gerr) {
			return false, true
		}
		return false, false
	}
	return stopAfter(global, recipeID, since, true), true
}

// stopAfter reports whether any subagent belonging to recipeID finished
// after `since`.
//
// requireAttribution separates the two logs. In the recipe's own log the
// file's location IS the attribution, so an event with no RecipeID
// belongs to this recipe. In the global log it belongs to nobody in
// particular, and counting it would credit this recipe for a fork some
// other work spawned — which turns agent_evidence from a claim about
// this round into a claim about the machine being busy. A project whose
// hooks never stamp a RecipeID therefore records "none" rather than a
// guess, and `bts doctor` stays silent because absence is only
// informative next to rounds that DID record evidence.
func stopAfter(events []MetricsEvent, recipeID string, since time.Time, requireAttribution bool) bool {
	for i := range events {
		if events[i].Kind != KindSubagentStop {
			continue
		}
		if events[i].RecipeID != recipeID && (requireAttribution || events[i].RecipeID != "") {
			continue
		}
		ts, perr := time.Parse(time.RFC3339, events[i].Timestamp)
		if perr != nil {
			continue
		}
		if since.IsZero() || ts.After(since) {
			return true
		}
	}
	return false
}
