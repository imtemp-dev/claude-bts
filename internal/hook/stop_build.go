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
//     no critical or major finding, and final.md has not changed since, or
//     a clean delta review of what changed after it covers final.md now.
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

	// Every In Scope item has a line in final.md saying what holds it, or
	// which resolved decision changed it (trial problem #103).  Checked
	// before the review, since adding the section is an edit to review.
	if issues := engine.CheckScopeCoverage(finalPath); len(issues) > 0 {
		details := make([]string, 0, len(issues))
		for i, is := range issues {
			if i == 5 {
				details = append(details, fmt.Sprintf("and %d more", len(issues)-5))
				break
			}
			details = append(details, is.Detail)
		}
		return blockOutput("Scope coverage: " + strings.Join(details, " ")), nil
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

// buildReviewClean is gate build_review_clean: final.md had one full
// review, and either that pass left no critical or major and final.md is
// unchanged since, or every delta review after it was clean and the last
// one read the final.md on disk. One review, not a loop: minors are fixed
// or declared under Known Uncertainties, and nothing waits for a
// replicated clean pass.
//
// The delta path is trial problem #45: any edit to final.md after the
// review, a minor's fix included, used to need another full pass, and the
// fix text bred new minors in it. A delta review of the changed sections
// now absorbs a fix; a delta that finds a critical or major does not, and
// sends final.md back to a full pass (state.DeltaChain).
func buildReviewClean(root, recipeID string) *HookOutput {
	e, err := state.LastVerifyEntryForDoc(root, recipeID, "final.md")
	if err != nil || e == nil || !strings.EqualFold(e.Doc, "final.md") {
		return blockOutput(fmt.Sprintf(
			"No review of final.md is recorded. Run /bts-verify on final.md once, and record it with "+
				"`bts recipe log %s --doc final.md --dimension verify --scope full --from-verification <verification.md>`.", recipeID))
	}
	entries, err := state.ReadVerifyLog(root, recipeID)
	if err != nil {
		return blockOutput(fmt.Sprintf("verify-log.jsonl is unreadable (%v).", err))
	}
	chain := state.LastDeltaChain(state.VerifyEntriesForDoc(entries, "final.md"))
	if chain.Full == nil {
		return blockOutput("final.md has only delta reviews. Review it once at full scope: a delta pass counts only after a full one.")
	}
	if b := chain.Breach(); b != nil {
		return blockOutput(fmt.Sprintf(
			"The delta review of final.md (round %d) found %d critical and %d major, so the fix reached past the sections it changed. "+
				"Fix each where it lives, rerun what the fix touched, and review final.md again at full scope.",
			b.Iteration, b.Critical, b.Major))
	}
	if len(chain.Deltas) == 0 && (chain.Full.Critical > 0 || chain.Full.Major > 0) {
		return blockOutput(fmt.Sprintf(
			"The review of final.md left %d critical and %d major. Fix each where it lives (the skeleton, a falsifier, or final.md), "+
				"rerun what the fix touched (`bts check build`, `bts falsifier check --red`), then review the changes with /bts-verify "+
				"at delta scope and record it with `bts recipe log %s --doc final.md --dimension verify --scope delta --from-verification <verification.md>`.",
			chain.Full.Critical, chain.Full.Major, recipeID))
	}
	if n := len(chain.Deltas); n > 0 && chain.Deltas[n-1].DocHash == "" {
		return blockOutput(fmt.Sprintf(
			"The delta review of final.md recorded no revision, so nothing shows it read the final.md on disk. "+
				"Record it again with a --doc path that resolves: `bts recipe log %s --doc final.md --scope delta ...`.", recipeID))
	}
	if dirty, derr := state.DirtyVerifiedDocs(root, recipeID); derr == nil {
		for _, d := range dirty {
			if strings.EqualFold(d, "final.md") {
				// The review also read the documents final.md points at; an
				// edit there leaves it as stale as an edit to final.md
				// (trial problem #106).
				if cited := state.CitedDocsChanged(root, recipeID, "final.md"); len(cited) > 0 {
					return blockOutput(fmt.Sprintf("final.md's review read %s, which changed after it. "+
						"Review the change with /bts-verify on final.md at delta scope (`--scope delta`), or revert it.",
						strings.Join(cited, ", ")))
				}
				return blockOutput("final.md changed after its review. Review the changed sections with /bts-verify at delta scope " +
					"(`--scope delta`), or revert the change.")
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

// generatedCurrent is gate generated_current at IMPLEMENT DONE, for every
// recipe whose wireframe has `generate` rows: the generators ran on the
// current code and their files are still what they wrote. Nothing held
// generated files to the code before (trial problem #66).
func generatedCurrent(root, recipeID string) *HookOutput {
	rows := engine.WireframeGenerateRows(root, recipeID)
	if len(rows) == 0 {
		return nil
	}
	ev, err := state.LoadBuildEvidence(root, recipeID)
	if err != nil {
		return blockOutput(fmt.Sprintf("build-evidence.json is unreadable (%v). Run `bts check generated %s`.", err, recipeID))
	}
	hash, herr := engine.CodeHash(root, recipeID)
	if herr != nil {
		fmt.Fprintf(os.Stderr, "[bts] warning: code fingerprint: %v (freshness not checked)\n", herr)
	}
	_, filesHash, ferr := engine.GeneratedFiles(root, engine.GenerateRowPaths(rows))
	if ferr != nil {
		return blockOutput(fmt.Sprintf("The generated files are unreadable (%v).", ferr))
	}
	if reason := engine.CheckGeneratedCurrent(ev.Generated, rows, hash, filesHash, recipeID); reason != "" {
		return blockOutput(reason)
	}
	return nil
}

// interrogateClean is gate interrogate_clean at IMPLEMENT DONE, for every
// recipe type: adversaries tried to break the change, bts reran each
// finding on a fresh copy, and every finding that failed again is fixed on
// the current code or waived with the user's reason (prove stage, P3).
// When the code changed beyond the rescope rule since the adversaries'
// copies, a scoped round must attack the change first (trial problem #52).
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
	change, ok, derr := engine.InterrogateDrift(root, recipeID, run)
	if derr != nil {
		fmt.Fprintf(os.Stderr, "[bts] warning: interrogate drift: %v (rescope not checked)\n", derr)
		return nil
	}
	if ok {
		if reason := engine.CheckInterrogateRescope(change, recipeID); reason != "" {
			return blockOutput(reason)
		}
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
	// An invariant guarded by prompt text holds only when the model follows
	// it, so a driven journey must name it (trial problem #111).
	ev, everr := state.LoadBuildEvidence(root, recipeID)
	if everr != nil {
		return blockOutput(fmt.Sprintf("build-evidence.json is unreadable (%v), so prompt-guarded invariants cannot be checked.", everr))
	}
	if reason := engine.CheckBehaviourObserved(rec, ev, recipeID); reason != "" {
		return blockOutput(reason)
	}
	return nil
}
