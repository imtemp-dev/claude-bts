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
//     a clean delta review of what changed after it covers final.md now;
//   - build_verify_clean, in its place for an express recipe (feature
//     #132): the mechanical `bts verify` of final.md as it is now finds no
//     critical or major.
//
// None is overridable. Each remedy is to run something again, which is
// cheap; an override could only mean "trust the prose instead".
func (h *stopHandler) handleBuildSpecDone(root string, recipe *state.RecipeState) (*HookOutput, error) {
	gate, reason, introduced, carried := buildSpecGate(root, recipe)
	if gate != "" {
		return blockOutput(reason), nil
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
	_ = state.LogPhaseChangeVia(root, recipe.ID, prevPhase, "finalize", state.PhaseViaGate)
	fmt.Fprintf(os.Stderr, "[bts] Build spec ready ✓ skeleton builds, %d falsifier(s) red, %d carried green. Next: /bts-implement %s\n", introduced, carried, recipe.ID)
	return &HookOutput{}, nil
}

// buildSpecGate runs the build recipe's DONE gates in order and returns
// the first one not met, by name, with its message; gate is "" when all
// hold, and introduced and carried count the invariants for the success
// line.  The next-step hint reads the same sequence, so it names what DONE
// would block on rather than guessing from the phase (trial problem #101).
func buildSpecGate(root string, recipe *state.RecipeState) (gate, reason string, introduced, carried int) {
	recipeDir := state.RecipeDir(root, recipe.ID)

	if open, derr := state.OpenDecisions(root, recipe.ID); derr == nil && len(open) > 0 {
		return "decisions", fmt.Sprintf(
			"%d decision(s) still waiting on the user: %s. Get the answer and record it with "+
				"`bts recipe decision resolve %s <key> --answer \"...\"`, or retire the question with `bts recipe decision drop`.",
			len(open), decisionSummary(open), recipe.ID), 0, 0
	}

	finalPath := filepath.Join(recipeDir, "final.md")
	if _, err := os.Stat(finalPath); os.IsNotExist(err) {
		if recipe.IsExpress() {
			return "final_md", fmt.Sprintf("No final.md. An express build recipe finalizes a short build spec (bts-recipe-build, Build spec); "+
				"write it, check it with `bts verify %s`, then complete.", finalRel(recipe.ID)), 0, 0
		}
		return "final_md", "No final.md. A build recipe finalizes a short build spec (bts-recipe-build, Build spec); write it, review it once, then complete.", 0, 0
	}

	ev, err := state.LoadBuildEvidence(root, recipe.ID)
	if err != nil {
		return "evidence", fmt.Sprintf("build-evidence.json is unreadable (%v). Re-record it with `bts check build` and `bts falsifier check --red`.", err), 0, 0
	}
	hash, herr := engine.CodeHash(root, recipe.ID)
	if herr != nil {
		fmt.Fprintf(os.Stderr, "[bts] warning: code fingerprint: %v (freshness not checked)\n", herr)
	}

	if reason := engine.CheckSkeletonBuilds(ev, hash, recipe.ID); reason != "" {
		return "skeleton_builds", reason, 0, 0
	}

	intro, carr, derr := engine.DomainInvariantKinds(filepath.Join(recipeDir, "domain.md"))
	if derr != nil {
		return "domain", fmt.Sprintf("domain.md invariants are unreadable (%v). The build recipe falsifies each invariant in domain.md § 2.", derr), 0, 0
	}
	if reason := engine.CheckFalsifiersRed(ev, intro, hash, recipe.ID); reason != "" {
		return "falsifiers_red", reason, len(intro), len(carr)
	}
	if reason := engine.CheckCarriedGreen(ev, carr, hash, recipe.ID); reason != "" {
		return "carried_green", reason, len(intro), len(carr)
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
		return "scope_coverage", "Scope coverage: " + strings.Join(details, " "), len(intro), len(carr)
	}

	if recipe.IsExpress() {
		if reason := buildVerifyClean(root, recipe.ID); reason != "" {
			return "build_verify_clean", reason, len(intro), len(carr)
		}
		return "", "", len(intro), len(carr)
	}
	if out := buildReviewClean(root, recipe.ID); out != nil {
		return "build_review_clean", out.Reason, len(intro), len(carr)
	}
	return "", "", len(intro), len(carr)
}

// finalRel is a recipe's final.md from the project root, as a command
// takes it.
func finalRel(recipeID string) string {
	return filepath.ToSlash(filepath.Join(".bts", "specs", "recipes", recipeID, "final.md"))
}

// buildVerifyClean is gate build_verify_clean, an express recipe's DONE
// gate in place of build_review_clean (feature #132): the mechanical
// `bts verify` of final.md, run here on final.md as it is now, finds no
// critical or major.  Express has no review round on final.md, whose
// review loop cost 19-121 active minutes per recipe; the code review at
// the end reads final.md with the code.  "" when it holds.
func buildVerifyClean(root, recipeID string) string {
	res, err := engine.VerifyDocument(filepath.Join(state.RecipeDir(root, recipeID), "final.md"), root, true)
	if err != nil {
		return fmt.Sprintf("final.md could not be checked (%v).", err)
	}
	if res.Summary.Critical == 0 && res.Summary.Major == 0 {
		return ""
	}
	var found []string
	for _, is := range res.Issues {
		if is.Severity != engine.SeverityCritical && is.Severity != engine.SeverityMajor {
			continue
		}
		found = append(found, fmt.Sprintf("%s %s %s: %s", is.Severity, is.Category, is.Claim, state.TruncateRunes(is.Detail, 200)))
	}
	if len(found) > 5 {
		found = append(found[:5], fmt.Sprintf("and %d more", len(found)-5))
	}
	return fmt.Sprintf("`bts verify` finds %d critical and %d major in final.md: %s. "+
		"Fix each where it lives (final.md, the skeleton, or a falsifier), rerun what the fix touched, and check again with "+
		"`bts verify %s`. An express recipe has no review round on final.md; the code review at the end reads it with the code.",
		res.Summary.Critical, res.Summary.Major, strings.Join(found, "; "), finalRel(recipeID))
}

// buildFalsifiersRecorded reports whether falsify's own work is done:
// every introduced invariant red and every carried one green on the
// current code.
func buildFalsifiersRecorded(root string, recipe *state.RecipeState) bool {
	ev, err := state.LoadBuildEvidence(root, recipe.ID)
	if err != nil {
		return false
	}
	intro, carr, derr := engine.DomainInvariantKinds(filepath.Join(state.RecipeDir(root, recipe.ID), "domain.md"))
	if derr != nil || len(intro)+len(carr) == 0 {
		return false
	}
	hash, _ := engine.CodeHash(root, recipe.ID)
	return engine.CheckFalsifiersRed(ev, intro, hash, recipe.ID) == "" && engine.CheckCarriedGreen(ev, carr, hash, recipe.ID) == ""
}

// buildTailHint is the next step for a build recipe in phase falsify,
// which covers steps 8 to 13: falsify, the empirical questions, final.md,
// its one review and DONE.  Keyed to the phase alone, the hint said "Run
// /bts-falsify" through all of them, four times on swift-eagle after its
// falsifiers were recorded, once at SessionStart after a compaction (trial
// problem #101).
func buildTailHint(root string, recipe *state.RecipeState) string {
	if !buildFalsifiersRecorded(root, recipe) {
		return fmt.Sprintf("Run /bts-falsify %s: one failing test per invariant, recorded with `bts falsifier run`.", recipe.ID)
	}
	gate, reason, _, _ := buildSpecGate(root, recipe)
	switch gate {
	case "":
		return fmt.Sprintf("Every build gate holds for %s: output <bts>DONE</bts> (bts-recipe-build step 13).", recipe.ID)
	case "final_md":
		if recipe.IsExpress() {
			return fmt.Sprintf("Falsifiers are recorded for %s. Write final.md (bts-recipe-build step 10, the short express form), "+
				"then check it with `bts verify %s` (express step 11).", recipe.ID, finalRel(recipe.ID))
		}
		return fmt.Sprintf("Falsifiers are recorded for %s. Write final.md (bts-recipe-build step 10), then review it once (step 11).", recipe.ID)
	default:
		// The whole reason, not its first sentence: a gate's message
		// carries its instruction for a recipe whose held templates
		// predate the gate, and the cut left cool-crow's SessionStart with
		// "final.md has no Scope coverage section." alone (trial problem
		// #123).
		return fmt.Sprintf("Next for %s (DONE gate %s): %s", recipe.ID, gate, state.TruncateRunes(reason, 900))
	}
}

// firstSentence cuts a gate message at its first sentence, for a hint.
func firstSentence(s string) string {
	if i := strings.Index(s, ". "); i > 0 {
		s = s[:i+1]
	}
	return state.TruncateRunes(s, 300)
}

// buildUnmutatedInvariants counts the introduced invariants with neither a
// mutation run nor a no-guard reason: Step 5.2's work still to do.
func buildUnmutatedInvariants(root string, recipe *state.RecipeState) int {
	ev, err := state.LoadBuildEvidence(root, recipe.ID)
	if err != nil {
		return 0
	}
	intro, _, derr := engine.DomainInvariantKinds(filepath.Join(state.RecipeDir(root, recipe.ID), "domain.md"))
	if derr != nil {
		return 0
	}
	n := 0
	for _, id := range intro {
		f := ev.Falsifiers[id]
		if f == nil || (f.Mutation == nil && strings.TrimSpace(f.NotMutable) == "") {
			n++
		}
	}
	return n
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
