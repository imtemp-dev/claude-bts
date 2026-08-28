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
// runs `jig recipe log`, and a round logged without any fork having run
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
// Two logs are consulted, not one, but they answer different questions.
// The recipe's own log is the primary source: its location IS the
// attribution, so an event there counts whatever the RecipeID field
// says. The global read is a fallback for one case only — metrics.Append
// writes the per-recipe copy best-effort and ignores its error, so an
// event can be stamped with this recipe and exist only globally. That
// read therefore REQUIRES the explicit field.
//
// It deliberately does not rescue events written before the hook stamped
// a RecipeID at all. Those name no recipe, and counting them would make
// agent_evidence a claim that the machine was busy rather than that this
// round forked. A project on the old hook records "none" instead of a
// guess, and `jig doctor` stays quiet, because absence is only
// informative next to rounds that did record evidence.
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
// guess, and `jig doctor` stays silent because absence is only
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
