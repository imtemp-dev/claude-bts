package engine

import (
	"fmt"
	"sort"
	"strings"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

// Gates of the build recipe. Each returns "" when it passes and otherwise
// a reason written for the agent that has to act on it. They read only
// what `bts check build` and `bts falsifier` recorded, plus the code hash
// recomputed now, so a run that no longer describes the code on disk does
// not count.

// CheckSkeletonBuilds is gate skeleton_builds: the last recorded build of
// the skeleton passed, on the code as it is now.
func CheckSkeletonBuilds(ev *state.BuildEvidence, currentHash, recipeID string) string {
	run := ev.Skeleton
	switch {
	case run == nil:
		return fmt.Sprintf("No skeleton build is recorded. Write the skeleton, then run `bts check build %s --cmd \"<typecheck or build command>\"`.", recipeID)
	case !run.Passed():
		return fmt.Sprintf("The last skeleton build failed (exit %d). Fix the skeleton until `bts check build %s --cmd %q` passes.", run.ExitCode, recipeID, run.Command)
	case currentHash != "" && run.CodeHash != currentHash:
		return fmt.Sprintf("The skeleton changed after its last build. Run `bts check build %s --cmd %q` again.", recipeID, run.Command)
	}
	return ""
}

// CheckFalsifiersRed is gate falsifiers_red: every invariant in domain.md
// has a falsifier that was run on the skeleton as it is now and failed.
func CheckFalsifiersRed(ev *state.BuildEvidence, invariants []string, currentHash, recipeID string) string {
	var missing, stale, notRed []string
	for _, id := range invariants {
		f := ev.Falsifiers[id]
		switch {
		case f == nil || f.Red == nil:
			missing = append(missing, id)
		case f.Red.Passed():
			notRed = append(notRed, id)
		case currentHash != "" && f.Red.CodeHash != currentHash:
			stale = append(stale, id)
		}
	}
	var parts []string
	if len(missing) > 0 {
		parts = append(parts, fmt.Sprintf("no falsifier recorded for %s: write a test for each and record it with `bts falsifier run %s --inv <id> --cmd \"<test command>\"`", strings.Join(missing, ", "), recipeID))
	}
	if len(notRed) > 0 {
		parts = append(parts, fmt.Sprintf("%s passed on the skeleton, so it does not test the invariant yet", strings.Join(notRed, ", ")))
	}
	if len(stale) > 0 {
		parts = append(parts, fmt.Sprintf("the skeleton changed after %s ran: run `bts falsifier check %s --red`", strings.Join(stale, ", "), recipeID))
	}
	if len(parts) == 0 {
		return ""
	}
	return "Falsifiers are not all red on the current skeleton: " + strings.Join(parts, "; ") + "."
}

// CheckFalsifiersGreen is gate falsifiers_green: every invariant's
// falsifier passed on the code as it is now.
func CheckFalsifiersGreen(ev *state.BuildEvidence, invariants []string, currentHash, recipeID string) string {
	var missing, failing, stale []string
	for _, id := range invariants {
		f := ev.Falsifiers[id]
		switch {
		case f == nil || f.Command == "":
			missing = append(missing, id)
		case f.Green == nil || !f.Green.Passed():
			failing = append(failing, id)
		case currentHash != "" && f.Green.CodeHash != currentHash:
			stale = append(stale, id)
		}
	}
	var parts []string
	if len(missing) > 0 {
		parts = append(parts, fmt.Sprintf("no falsifier recorded for %s", strings.Join(missing, ", ")))
	}
	if len(failing) > 0 {
		parts = append(parts, fmt.Sprintf("%s not passing", strings.Join(failing, ", ")))
	}
	if len(stale) > 0 {
		parts = append(parts, fmt.Sprintf("the code changed after %s last passed", strings.Join(stale, ", ")))
	}
	if len(parts) == 0 {
		return ""
	}
	return fmt.Sprintf("Falsifiers are not all green on the current code: %s. Run `bts falsifier check %s`.", strings.Join(parts, "; "), recipeID)
}

// CheckFalsifiersAbleToFail is gate falsifiers_able_to_fail: every
// invariant's falsifier has been seen failing, for the invariant's own
// reason, on a copy of the current code with the invariant's guard broken
// (`bts falsifier mutate`), or the invariant is recorded as having no code
// guard to break. It returns "" when the gate is met.
func CheckFalsifiersAbleToFail(ev *state.BuildEvidence, invariants []string, currentHash, recipeID string) string {
	var unproven, survived, stale []string
	for _, id := range invariants {
		f := ev.Falsifiers[id]
		switch {
		case f != nil && strings.TrimSpace(f.NotMutable) != "":
			continue
		case f == nil || f.Mutation == nil:
			unproven = append(unproven, id)
		case !f.Mutation.Killed():
			survived = append(survived, id)
		case currentHash != "" && f.Mutation.BaseHash != currentHash:
			stale = append(stale, id)
		}
	}
	var parts []string
	if len(unproven) > 0 {
		parts = append(parts, fmt.Sprintf("no mutation run for %s", strings.Join(unproven, ", ")))
	}
	if len(survived) > 0 {
		parts = append(parts, fmt.Sprintf("the falsifier did not fail in its own test with the guard broken for %s, so it does not test that invariant yet", strings.Join(survived, ", ")))
	}
	if len(stale) > 0 {
		parts = append(parts, fmt.Sprintf("the code changed after the mutation runs for %s", strings.Join(stale, ", ")))
	}
	if len(parts) == 0 {
		return ""
	}
	return fmt.Sprintf("Falsifiers are not shown able to fail: %s. Break each invariant's guard in a patch and run "+
		"`bts falsifier mutate %s --inv <id> --patch <file> --expect \"<its test id>\"`; after a code change run "+
		"`bts falsifier mutate %s --rerun`; an invariant with no code guard is recorded with --not-mutable \"<reason>\".",
		strings.Join(parts, "; "), recipeID, recipeID)
}

// FalsifierIDs returns the recorded invariant ids in sorted order.
func FalsifierIDs(ev *state.BuildEvidence) []string {
	ids := make([]string, 0, len(ev.Falsifiers))
	for id := range ev.Falsifiers {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
