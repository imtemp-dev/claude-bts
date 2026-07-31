package hook

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/imtemp-dev/claude-bts/internal/comment"
	"github.com/imtemp-dev/claude-bts/internal/engine"
	"github.com/imtemp-dev/claude-bts/internal/metrics"
	"github.com/imtemp-dev/claude-bts/internal/state"
)

type stopHandler struct{}

func NewStopHandler() Handler {
	return &stopHandler{}
}

func (h *stopHandler) EventType() EventType {
	return EventStop
}

func (h *stopHandler) Handle(input *HookInput) (*HookOutput, error) {
	root, err := state.FindRoot(input.CWD)
	if err != nil {
		return &HookOutput{}, nil
	}

	recipe, err := state.GetActiveRecipe(root)
	if err != nil || recipe == nil {
		// Check for finalized recipe (ready for implementation)
		finalized, _ := state.GetFinalizedRecipe(root)
		if finalized != nil {
			fmt.Fprintf(os.Stderr, "[bts] Spec finalized. Run /bts-implement %s to start implementation.\n", finalized.ID)
		}
		return &HookOutput{}, nil
	}

	// Check for fix completion marker
	if strings.Contains(input.StopHookContent, "<bts>FIX DONE</bts>") ||
		strings.Contains(input.StopHookContent, "FIX DONE") {
		return h.handleFixDone(root, recipe)
	}

	// Check for implementation completion marker
	if strings.Contains(input.StopHookContent, "<bts>IMPLEMENT DONE</bts>") ||
		strings.Contains(input.StopHookContent, "IMPLEMENT DONE") {
		return h.handleImplementDone(root, recipe)
	}

	// Check for spec completion marker (tagged only — "DONE" alone is too common)
	if strings.Contains(input.StopHookContent, "<bts>DONE</bts>") {
		return h.handleSpecDone(root, recipe)
	}

	// No completion marker — allow stop without blocking.
	// Print next-step hint to stderr so user sees it immediately.
	if next := nextStepHint(root, recipe); next != "" {
		fmt.Fprintf(os.Stderr, "[bts] %s\n", next)
	}
	return &HookOutput{}, nil
}

// handleSpecDone validates spec recipe completion via verify-log.
func (h *stopHandler) handleSpecDone(root string, recipe *state.RecipeState) (*HookOutput, error) {
	recipeDir := state.RecipeDir(root, recipe.ID)

	// 1. Check verification.md exists (proves /verify was actually run)
	verifyDocPath := filepath.Join(recipeDir, "verification.md")
	if _, err := os.Stat(verifyDocPath); os.IsNotExist(err) {
		return blockOutput("No verification.md found. Run /bts-verify on draft.md before completing."), nil
	}

	// 2. Check verify-log has passing entry.
	//
	// Prefer the spec document's OWN verification state. Before v0.10 the
	// log was undifferentiated, so a wireframe.md round could satisfy this
	// gate for draft.md — the counts checked belonged to a different
	// document. state.VerifyEntriesForDoc falls back to the whole stream
	// for legacy logs that record no doc, so old recipes are unaffected.
	logPath := filepath.Join(recipeDir, "verify-log.jsonl")
	lastEntry, err := readLastVerifyEntry(logPath)
	if err != nil {
		return blockOutput("No verification log found. Run verification before completing."), nil
	}
	gateDoc := "draft.md"
	if e, derr := state.LastVerifyEntryForDoc(root, recipe.ID, gateDoc); derr == nil && e != nil {
		lastEntry = e
	}

	resolvable := lastEntry.EffectiveResolvable()
	if lastEntry.Critical > 0 || lastEntry.Major > 0 || resolvable > 0 {
		return blockOutput(fmt.Sprintf(
			"Verification not passed for %s: %d critical, %d major, %d minor [resolvable] remain. Fix and re-verify. Deferred minors are runtime watch-items and do not block here.",
			lastEntry.Doc, lastEntry.Critical, lastEntry.Major, resolvable,
		)), nil
	}

	if lastEntry.Status == "failed" {
		return blockOutput(
			"Last verification round is marked failed (convergence budget exhausted). " +
				"The loop stopped making progress — resolve with the user rather than re-emitting DONE. " +
				"See `bts recipe findings list --open` for the findings that would not clear.",
		), nil
	}

	// 2a. Only a FULL pass may satisfy completion. Scoped delta rounds
	// verify the changed sections plus their reference closure, which
	// keeps iteration cheap, but finalizing on one would ship a spec
	// whose untouched sections were never re-checked against the edits.
	// Entries written before the scope flag existed carry FullPass=false
	// with no Doc; only enforce when the log is doc-scoped (v0.10+).
	if lastEntry.Doc != "" && !lastEntry.FullPass {
		return blockOutput(fmt.Sprintf(
			"%s is clean but its last verification was a scoped delta pass. Run one full pass "+
				"(`/bts-verify` then `bts recipe log %s --from-verification .bts/specs/recipes/%s/verification.md --doc %s --scope full`) before completing.",
			lastEntry.Doc, recipe.ID, recipe.ID, lastEntry.Doc,
		)), nil
	}

	// 2b. Rule 3 hard gate: verified documents must be UNCHANGED since
	// their last verification. Mandatory verification was prompt-level
	// until v0.9.x; the verify snapshots (`recipe log --doc`) make it
	// machine-checkable. No snapshots → nothing enforceable (legacy
	// recipes) — gates 1-2 still apply. Fail-open on read errors: a
	// tooling failure is not a verification veto (same policy as the
	// BTS-BLOCK count below).
	if dirty, derr := state.DirtyVerifiedDocs(root, recipe.ID); derr != nil {
		fmt.Fprintf(os.Stderr,
			"[bts] warning: dirty-doc check failed: %v (proceeding without check)\n", derr)
	} else if len(dirty) > 0 {
		return blockOutput(fmt.Sprintf(
			"%s modified after last verification. Rule 3: every modification requires /bts-verify. Re-verify the modified doc(s), record with `bts recipe log %s --from-verification .bts/specs/recipes/%s/verification.md --doc <doc-path>`, then re-emit DONE.",
			strings.Join(dirty, ", "), recipe.ID, recipe.ID,
		)), nil
	}

	// 2c. Deferred minors must be declared as Known Uncertainties entries
	// (### U-NNN) so /bts-implement inherits the watch-list (blueprint
	// rule 3b). Without this, deferred findings silently vanish between
	// spec and implementation.
	if lastEntry.MinorDeferred > 0 {
		specPath := filepath.Join(recipeDir, "final.md")
		if _, statErr := os.Stat(specPath); os.IsNotExist(statErr) {
			specPath = filepath.Join(recipeDir, "draft.md")
		}
		if all, _, uerr := engine.CheckKnownUncertainties(specPath); uerr == nil && len(all) == 0 {
			return blockOutput(fmt.Sprintf(
				"%d minor [deferred] finding(s) recorded but %s has no '## Known Uncertainties' entries (### U-NNN form). Per blueprint rule 3b, append each deferred minor with its Why-deferred: line, re-run /bts-verify (the append is a modification — rule 3), then re-emit DONE.",
				lastEntry.MinorDeferred, filepath.Base(specPath),
			)), nil
		}
	}

	// 2d. Blueprint-only changelog gates: simulate-at-least-once (rule 5)
	// and sync-check-after-last-modification (rule 8). Both rules are
	// tagged {gate: hard} — before Sprint 10 neither was actually
	// machine-enforced (sync-check never touched verify-log; simulate
	// only produced a warn on the implement-side phase transition).
	if recipe.Type == "blueprint" {
		entries, cerr := state.ReadChangelog(root, recipe.ID)
		if cerr != nil {
			return blockOutput(fmt.Sprintf(
				"Cannot read changelog.jsonl (%v). Rule 4 requires every action logged; the simulate and sync-check completion gates need the log.",
				cerr,
			)), nil
		}
		simulated := false
		lastModify := -1
		lastSyncCheckPass := -1
		for i, e := range entries {
			switch e.Action {
			case "simulate":
				simulated = true
			case "draft", "improve", "comment-apply":
				lastModify = i
			case "sync-check":
				if strings.HasPrefix(e.Result, "pass") {
					lastSyncCheckPass = i
				}
			}
		}
		if !simulated {
			return blockOutput(
				"No simulate action in changelog. Rule 5: run /bts-simulate (5+ scenarios) at least once before declaring Level 3, then /bts-sync-check, then re-emit DONE."), nil
		}
		if lastSyncCheckPass == -1 || lastSyncCheckPass < lastModify {
			return blockOutput(
				"sync-check has not passed since the last draft modification. Rule 8: run /bts-sync-check (it logs a pass entry via `bts sync-check`), then re-emit DONE."), nil
		}
	}

	// 3. Block on unresolved [!BTS-BLOCK] callouts. These represent
	// reviewer-flagged spec issues that must be addressed (or downgraded
	// to a non-blocking comment) before the spec can finalize. The check
	// re-parses the source markdown — manifest counts may be stale if
	// callouts were edited without running `bts comment apply --finalize`.
	//
	// On parse error (recipe dir missing, file unreadable), surface the
	// failure to stderr but DO NOT block — a parse failure is a tooling
	// problem, not a reviewer veto. Conservative fail-open here is fine
	// because gates 1+2 already enforce verification soundness.
	blocking, err := comment.CountBlockingComments(recipeDir)
	if err != nil {
		fmt.Fprintf(os.Stderr,
			"[bts] warning: could not count BTS-BLOCK comments in %s: %v (proceeding without check)\n",
			recipeDir, err)
	} else if blocking > 0 {
		return blockOutput(fmt.Sprintf(
			"%d BTS-BLOCK comment(s) unresolved. Run `bts comment list %s` to see them, then `/bts-comment-apply %s` to incorporate.",
			blocking, recipe.ID, recipe.ID,
		)), nil
	}

	// All clear — allow completion. Sprint 9 P21: normalize level and
	// iteration from the authoritative verify-log entry we just read.
	// handleSpecDone is only reached when the last entry is converged
	// (critical=0, major=0, minor_resolvable=0) — that IS Level 3.0 by
	// definition, so we set it explicitly to prevent recipe.json from
	// drifting (e.g. r-018 was phase=simulate, level=0 because finalize
	// was never emitted). Iteration stays monotonic — only raises.
	prevPhase := recipe.Phase
	recipe.Phase = "finalize"
	recipe.Level = 3.0
	if lastEntry.Iteration > recipe.Iteration {
		recipe.Iteration = lastEntry.Iteration
	}
	if err := state.SaveRecipeState(root, recipe); err != nil {
		return nil, fmt.Errorf("save recipe state: %w", err)
	}
	_ = metrics.Append(root, &metrics.MetricsEvent{
		Kind:          metrics.KindPhaseChange,
		RecipeID:      recipe.ID,
		Phase:         "finalize",
		PreviousPhase: prevPhase,
	})

	fmt.Fprintf(os.Stderr, "[bts] Spec verified ✓ Next: /bts-implement %s\n", recipe.ID)
	return &HookOutput{}, nil
}

// handleImplementDone validates implementation completion via tasks.json + test-results.json.
func (h *stopHandler) handleImplementDone(root string, recipe *state.RecipeState) (*HookOutput, error) {
	implCmd := fmt.Sprintf("/bts-implement %s", recipe.ID)
	if recipe.Type == "fix" {
		implCmd = fmt.Sprintf("/bts-recipe-fix %s", recipe.ID)
	}

	// 1. Check tasks.json
	tasks, err := state.LoadTaskState(root, recipe.ID)
	if err != nil {
		return blockOutput(fmt.Sprintf("No tasks.json found. Run %s to decompose tasks.", implCmd)), nil
	}

	var blocked, pending int
	for _, t := range tasks.Tasks {
		switch t.Status {
		case "blocked":
			blocked++
		case "pending", "in_progress":
			pending++
		}
	}

	if blocked > 0 {
		return blockOutput(fmt.Sprintf(
			"Implementation incomplete: %d task(s) blocked. Resolve blocked tasks before completing.",
			blocked,
		)), nil
	}

	if pending > 0 {
		return blockOutput(fmt.Sprintf(
			"Implementation incomplete: %d task(s) still pending. Run %s to complete remaining tasks.",
			pending, implCmd,
		)), nil
	}

	// 2. Check test-results.json
	testResults, err := state.LoadTestResults(root, recipe.ID)
	if err != nil {
		return blockOutput(fmt.Sprintf("No test-results.json found. Run %s to run tests.", implCmd)), nil
	}

	if testResults.Status != "pass" {
		return blockOutput(fmt.Sprintf(
			"Tests not passing: %d failed out of %d. Run %s to fix and re-test.",
			testResults.Failed, testResults.Total, implCmd,
		)), nil
	}

	// 3. Check that review has run (review.md exists)
	reviewPath := filepath.Join(state.RecipeDir(root, recipe.ID), "review.md")
	if _, err := os.Stat(reviewPath); os.IsNotExist(err) {
		return blockOutput(fmt.Sprintf("No review.md found. Run %s to complete review.", implCmd)), nil
	}

	// 4. Check that sync has run (deviation.md exists)
	deviationPath := filepath.Join(state.RecipeDir(root, recipe.ID), "deviation.md")
	if _, err := os.Stat(deviationPath); os.IsNotExist(err) {
		return blockOutput(fmt.Sprintf("No deviation.md found. Run %s to sync spec with code.", implCmd)), nil
	}
	// deviation.md content is a REPORT, not a gate.
	// Deviations and not-implemented items become follow-up work,
	// not blockers for the current recipe's completion.

	// 5. Known Uncertainties gate (Phase 8.2): every entry in final.md's
	// "## Known Uncertainties" section must carry Resolved:/Diverged:/
	// Still-unknown:. The skill promises this at Step 5.7; the hook
	// now enforces it.
	finalPath := filepath.Join(state.RecipeDir(root, recipe.ID), "final.md")
	if _, unresolved, err := engine.CheckKnownUncertainties(finalPath); err == nil && len(unresolved) > 0 {
		ids := make([]string, 0, len(unresolved))
		for _, u := range unresolved {
			ids = append(ids, u.ID)
		}
		return blockOutput(fmt.Sprintf(
			"Known Uncertainties unresolved (%d entr(y|ies): %s). Re-run Step 5.7 of %s to classify each as Resolved:/Diverged:/Still-unknown:.",
			len(unresolved), strings.Join(ids, ", "), implCmd,
		)), nil
	}

	// 5b. Deviation schema gate (Phase 16): every row in deviation.md
	// must carry an ID, at least one Driver from the vocabulary, and a
	// valid Severity. Critical-level findings (missing ID / missing
	// driver) block completion; lower severities surface via
	// `bts validate`.
	for _, issue := range engine.CheckDeviationSchema(deviationPath) {
		if issue.Severity == "critical" {
			return blockOutput(fmt.Sprintf(
				"deviation.md schema failure: %s — %s",
				issue.Claim, issue.Detail,
			)), nil
		}
	}

	// 6. Modify scope gate (Phase 14): for Action=="modify" tasks,
	// CheckModifyScope with the real project root runs the
	// scope_symbol_missing check that the static validator cannot do
	// (it needs filesystem access to the target file). critical
	// findings block; lower severities are already caught by
	// `bts validate`.
	tasksPath := filepath.Join(state.RecipeDir(root, recipe.ID), "tasks.json")
	for _, issue := range engine.CheckModifyScope(finalPath, tasksPath, root) {
		if issue.Severity == "critical" {
			return blockOutput(fmt.Sprintf(
				"%s — %s. Resolve modify_scope violations before completing.",
				issue.Claim, issue.Detail,
			)), nil
		}
	}

	// All clear — mark as complete
	prevPhase := recipe.Phase
	recipe.Phase = "complete"
	if err := state.SaveRecipeState(root, recipe); err != nil {
		return nil, fmt.Errorf("save recipe state: %w", err)
	}
	_ = metrics.Append(root, &metrics.MetricsEvent{
		Kind:          metrics.KindPhaseChange,
		RecipeID:      recipe.ID,
		Phase:         "complete",
		PreviousPhase: prevPhase,
	})

	if err := state.MarkRoadmapItemDone(root, recipe.ID); err != nil {
		fmt.Fprintf(os.Stderr, "warning: update roadmap: %v\n", err)
	}
	return roadmapHint(root, "Implementation complete."), nil
}

// handleFixDone validates fix recipe completion via fix-spec.md + test-results.json.
func (h *stopHandler) handleFixDone(root string, recipe *state.RecipeState) (*HookOutput, error) {
	// 1. Check fix-spec.md exists
	fixSpecPath := filepath.Join(state.RecipeDir(root, recipe.ID), "fix-spec.md")
	if _, err := os.Stat(fixSpecPath); os.IsNotExist(err) {
		return blockOutput("No fix-spec.md found. Create fix spec before completing."), nil
	}

	// 1b. Rule 3 hard gate — same as spec DONE: the verified fix-spec
	// must be unchanged since its last verification. (Applies only when
	// snapshots exist; legacy fix recipes pass through.)
	if dirty, derr := state.DirtyVerifiedDocs(root, recipe.ID); derr != nil {
		fmt.Fprintf(os.Stderr,
			"[bts] warning: dirty-doc check failed: %v (proceeding without check)\n", derr)
	} else if len(dirty) > 0 {
		return blockOutput(fmt.Sprintf(
			"%s modified after last verification. Rule 3: re-verify the modified doc(s), record with `bts recipe log %s --from-verification ... --doc <doc-path>`, then re-emit FIX DONE.",
			strings.Join(dirty, ", "), recipe.ID,
		)), nil
	}

	// 2. Check test-results.json
	testResults, err := state.LoadTestResults(root, recipe.ID)
	if err != nil {
		return blockOutput("No test-results.json found. Run tests before completing fix."), nil
	}

	if testResults.Status != "pass" {
		return blockOutput(fmt.Sprintf(
			"Tests not passing: %d failed out of %d. Fix and re-test.",
			testResults.Failed, testResults.Total,
		)), nil
	}

	// All clear — mark as complete
	prevPhase := recipe.Phase
	recipe.Phase = "complete"
	if err := state.SaveRecipeState(root, recipe); err != nil {
		return nil, fmt.Errorf("save recipe state: %w", err)
	}
	_ = metrics.Append(root, &metrics.MetricsEvent{
		Kind:          metrics.KindPhaseChange,
		RecipeID:      recipe.ID,
		Phase:         "complete",
		PreviousPhase: prevPhase,
	})

	if err := state.MarkRoadmapItemDone(root, recipe.ID); err != nil {
		fmt.Fprintf(os.Stderr, "warning: update roadmap: %v\n", err)
	}
	return roadmapHint(root, "Fix complete."), nil
}

// roadmapHint logs roadmap progress to stderr (Stop hook cannot use hookSpecificOutput).
func roadmapHint(root string, prefix string) *HookOutput {
	done, total, nextItem := state.RoadmapProgress(root)
	if total > 0 {
		hint := fmt.Sprintf("Roadmap: %d/%d done.", done, total)
		if nextItem != "" {
			hint += fmt.Sprintf(" Next: %s — run /bts-recipe-blueprint to start.", nextItem)
		}
		fmt.Fprintf(os.Stderr, "[bts] %s %s\n", prefix, hint)
	}
	return &HookOutput{}
}

func blockOutput(reason string) *HookOutput {
	return &HookOutput{
		Decision: "block",
		Reason:   reason,
	}
}

func readLastVerifyEntry(path string) (*state.VerifyLogEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var last state.VerifyLogEntry
	found := false
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		var entry state.VerifyLogEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		last = entry
		found = true
	}

	if !found {
		return nil, fmt.Errorf("empty verify log")
	}

	return &last, nil
}
