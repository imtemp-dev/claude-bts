package hook

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/imtemp-dev/claude-bts/internal/engine"
	"github.com/imtemp-dev/claude-bts/internal/metrics"
	"github.com/imtemp-dev/claude-bts/internal/state"
)

// handleBuildSpecDone is <bts>DONE</bts> for a build recipe. The blueprint
// finalizes after rounds of verify, audit and simulate over prose converge;
// the build recipe takes its truth from runs instead, so its gates read
// what ran:
//
//   - skeleton_builds:    the skeleton compiles, on the code as it is now;
//   - falsifiers_red:     every invariant domain.md introduces has a test
//     that fails on that skeleton, and every one it carries has a test
//     that passes there (trial problem #41);
//   - build_review_clean: one full verify pass over the short final.md left
//     no critical or major finding, and final.md has not changed since.
//
// None is overridable. Each remedy is to run something again, which is
// cheap; an override could only mean "trust the prose instead".
func (h *stopHandler) handleBuildSpecDone(root string, recipe *state.RecipeState) (*HookOutput, error) {
	recipeDir := state.RecipeDir(root, recipe.ID)

	if open, derr := state.OpenDecisions(root, recipe.ID); derr == nil && len(open) > 0 {
		return blockOutput(fmt.Sprintf(
			"%d decision(s) still waiting on the user: %s. Get the answer and record it with "+
				"`bts recipe decision resolve %s <key> --answer \"...\"`, or retire the question with `bts recipe decision drop`.",
			len(open), decisionSummary(open), recipe.ID,
		)), nil
	}

	finalPath := filepath.Join(recipeDir, "final.md")
	if _, err := os.Stat(finalPath); os.IsNotExist(err) {
		return blockOutput("No final.md. A build recipe finalizes a short build spec (bts-recipe-build, Build spec); write it, review it once, then complete."), nil
	}

	ev, err := state.LoadBuildEvidence(root, recipe.ID)
	if err != nil {
		return blockOutput(fmt.Sprintf("build-evidence.json is unreadable (%v). Re-record it with `bts check build` and `bts falsifier check --red`.", err)), nil
	}
	hash, herr := engine.CodeHash(root, recipe.ID)
	if herr != nil {
		fmt.Fprintf(os.Stderr, "[bts] warning: code fingerprint: %v (freshness not checked)\n", herr)
	}

	if reason := engine.CheckSkeletonBuilds(ev, hash, recipe.ID); reason != "" {
		return blockOutput(reason), nil
	}

	introduced, carried, derr := engine.DomainInvariantKinds(filepath.Join(recipeDir, "domain.md"))
	if derr != nil {
		return blockOutput(fmt.Sprintf("domain.md invariants are unreadable (%v). The build recipe falsifies each invariant in domain.md § 2.", derr)), nil
	}
	if reason := engine.CheckFalsifiersRed(ev, introduced, hash, recipe.ID); reason != "" {
		return blockOutput(reason), nil
	}
	if reason := engine.CheckCarriedGreen(ev, carried, hash, recipe.ID); reason != "" {
		return blockOutput(reason), nil
	}

	if out := buildReviewClean(root, recipe.ID); out != nil {
		return out, nil
	}

	prevPhase := recipe.Phase
	recipe.Phase = "finalize"
	recipe.Level = 3.0
	if err := state.SaveRecipeState(root, recipe); err != nil {
		return nil, fmt.Errorf("save recipe state: %w", err)
	}
	_ = metrics.Append(root, &metrics.MetricsEvent{
		Kind:          metrics.KindPhaseChange,
		RecipeID:      recipe.ID,
		Phase:         "finalize",
		PreviousPhase: prevPhase,
	})
	_ = state.LogPhaseChange(root, recipe.ID, prevPhase, "finalize")
	fmt.Fprintf(os.Stderr, "[bts] Build spec ready ✓ skeleton builds, %d falsifier(s) red, %d carried green. Next: /bts-implement %s\n", len(introduced), len(carried), recipe.ID)
	return &HookOutput{}, nil
}

// buildReviewClean is gate build_review_clean: the last verify round on
// final.md was a full pass with no critical or major finding, and final.md
// is unchanged since it was verified. One review, not a loop: minors are
// fixed or declared under Known Uncertainties, and nothing waits for a
// replicated clean pass.
func buildReviewClean(root, recipeID string) *HookOutput {
	e, err := state.LastVerifyEntryForDoc(root, recipeID, "final.md")
	if err != nil || e == nil || !strings.EqualFold(e.Doc, "final.md") {
		return blockOutput(fmt.Sprintf(
			"No review of final.md is recorded. Run /bts-verify on final.md once, and record it with "+
				"`bts recipe log %s --doc final.md --dimension verify --scope full --from-verification <verification.md>`.", recipeID))
	}
	if !e.FullPass {
		return blockOutput("The last review of final.md was a delta pass. Review it once at full scope.")
	}
	if e.Critical > 0 || e.Major > 0 {
		return blockOutput(fmt.Sprintf(
			"The review of final.md left %d critical and %d major. Fix each where it lives (the skeleton, a falsifier, or final.md), "+
				"rerun what the fix touched (`bts check build`, `bts falsifier check --red`), and review final.md again.",
			e.Critical, e.Major))
	}
	if dirty, derr := state.DirtyVerifiedDocs(root, recipeID); derr == nil {
		for _, d := range dirty {
			if strings.EqualFold(d, "final.md") {
				return blockOutput("final.md changed after its review. Review it again, or revert the change.")
			}
		}
	}
	return nil
}

// buildFalsifiersGreen is gate falsifiers_green at IMPLEMENT DONE: every
// invariant's falsifier passes on the code as it is now, carried ones
// included.
func buildFalsifiersGreen(root, recipeID string) *HookOutput {
	ev, err := state.LoadBuildEvidence(root, recipeID)
	if err != nil {
		return blockOutput(fmt.Sprintf("build-evidence.json is unreadable (%v). Run `bts falsifier check %s`.", err, recipeID))
	}
	ids, derr := engine.DomainInvariantIDs(filepath.Join(state.RecipeDir(root, recipeID), "domain.md"))
	if derr != nil {
		return blockOutput(fmt.Sprintf("domain.md invariants are unreadable (%v).", derr))
	}
	hash, herr := engine.CodeHash(root, recipeID)
	if herr != nil {
		fmt.Fprintf(os.Stderr, "[bts] warning: code fingerprint: %v (freshness not checked)\n", herr)
	}
	if reason := engine.CheckFalsifiersGreen(ev, ids, hash, recipeID); reason != "" {
		return blockOutput(reason)
	}
	return nil
}

// buildFalsifiersAbleToFail is gate falsifiers_able_to_fail at IMPLEMENT
// DONE: every invariant's falsifier has been seen failing, in its own test,
// on a copy of the current code with the invariant's guard broken, or the
// invariant is recorded as having no code guard (pstack's "able to fail").
func buildFalsifiersAbleToFail(root, recipeID string) *HookOutput {
	ev, err := state.LoadBuildEvidence(root, recipeID)
	if err != nil {
		return blockOutput(fmt.Sprintf("build-evidence.json is unreadable (%v). Run `bts falsifier mutate %s --rerun`.", err, recipeID))
	}
	// Only the invariants this recipe introduces: a carried one's guard is
	// existing code the change keeps, not one it wrote (trial problem #41).
	introduced, _, derr := engine.DomainInvariantKinds(filepath.Join(state.RecipeDir(root, recipeID), "domain.md"))
	if derr != nil {
		return blockOutput(fmt.Sprintf("domain.md invariants are unreadable (%v).", derr))
	}
	hash, herr := engine.CodeHash(root, recipeID)
	if herr != nil {
		fmt.Fprintf(os.Stderr, "[bts] warning: code fingerprint: %v (freshness not checked)\n", herr)
	}
	if reason := engine.CheckFalsifiersAbleToFail(ev, introduced, hash, recipeID); reason != "" {
		return blockOutput(reason)
	}
	return nil
}

// interrogateClean is gate interrogate_clean at IMPLEMENT DONE, for every
// recipe type: adversaries tried to break the change, bts reran each
// finding on a fresh copy, and every finding that failed again is fixed on
// the current code or waived with the user's reason (prove stage, P3).
func interrogateClean(root, recipeID string) *HookOutput {
	run, err := state.LoadInterrogateRun(root, recipeID)
	if err != nil {
		return blockOutput(fmt.Sprintf("interrogate.json is unreadable (%v).", err))
	}
	hash, herr := engine.CodeHash(root, recipeID)
	if herr != nil {
		fmt.Fprintf(os.Stderr, "[bts] warning: code fingerprint: %v (freshness not checked)\n", herr)
	}
	if reason := engine.CheckInterrogateClean(run, hash, recipeID); reason != "" {
		return blockOutput(reason)
	}
	return nil
}

// appProven is gate app_proven at IMPLEMENT DONE, for every recipe type:
// the user journeys the change touches were driven through the real app
// on the current code and left their evidence, or were deferred with a
// reason and an owner, or the change has no user-facing surface (prove
// stage, P2).
func appProven(root, recipeID string) *HookOutput {
	rec, err := state.LoadProveRecord(root, recipeID)
	if err != nil {
		return blockOutput(fmt.Sprintf("prove.json is unreadable (%v).", err))
	}
	hash, herr := engine.CodeHash(root, recipeID)
	if herr != nil {
		fmt.Fprintf(os.Stderr, "[bts] warning: code fingerprint: %v (freshness not checked)\n", herr)
	}
	if reason := engine.CheckAppProven(rec, hash, recipeID); reason != "" {
		return blockOutput(reason)
	}
	return nil
}
