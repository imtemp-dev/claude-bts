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

	out, err := h.decide(root, input)
	if err != nil {
		return out, err
	}
	if out == nil || out.Decision != "block" {
		// Any allowed stop ends the episode: a recipe that recovered gets
		// a full budget for whatever it hits next.
		state.ClearStopBudget(root)
		return out, nil
	}

	// Bound the block loop. The identity is the reason text itself: when
	// the model makes progress the message changes (different counts,
	// different gate) and the count restarts, so only a genuinely
	// unchanging complaint burns the budget.
	count, exhausted := state.ChargeStopBlock(root, input.SessionID, out.Reason, state.DefaultStopBlockBudget)
	if exhausted {
		state.ClearStopBudget(root)
		fmt.Fprintf(os.Stderr,
			"[bts] The completion gate blocked %d times on the same issue and is standing down.\n"+
				"      Unresolved: %s\n"+
				"      Nothing was marked complete. Resolve it with the user, or run `bts doctor` for the full state.\n",
			count, out.Reason)
		return &HookOutput{}, nil
	}
	return out, nil
}

// decide runs the gates and returns the raw decision, before the block
// budget is applied.
func (h *stopHandler) decide(root string, input *HookInput) (*HookOutput, error) {
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

	// No completion marker. A marker-only gate has a trivial bypass: a
	// turn that simply never says DONE ends with the recipe mid-loop and
	// nothing is checked at all. The backstop below catches the states
	// where ending here would silently mislead the next session.
	return h.handleBlindStop(root, recipe)
}

// handleBlindStop is the state-based backstop for a turn that ends with no
// completion marker.
//
// It deliberately does NOT block merely because a recipe has open findings
// — that is the normal, expected mid-loop state, and blocking on it would
// make it impossible to stop for the day. It blocks only where ending the
// turn leaves the recipe's own records inconsistent, so that the next
// session (or `bts doctor`) would read a state that is not true:
//
//	A. a verification ran but was never logged — the findings ledger,
//	   convergence budget, and completion gate all read verify-log.jsonl,
//	   so an unlogged round is work that silently did not happen;
//	B. a verified document was modified after its verification — rule 3;
//	C. the convergence budget was exhausted — the loop gave up, and
//	   ending quietly means the next session resumes as if it had not.
//
// Scope is the spec loop. Implement-side phases run their own gates and
// stop mid-task constantly by design; /bts-sync legitimately rewrites
// final.md there, so condition B would fire on normal work.
//
// Every check fails open: a tooling error is not a veto (same policy as
// the DONE-path gates).
func (h *stopHandler) handleBlindStop(root string, recipe *state.RecipeState) (*HookOutput, error) {
	hint := nextStepHint(root, recipe)
	allow := func() (*HookOutput, error) {
		if hint != "" {
			fmt.Fprintf(os.Stderr, "[bts] %s\n", hint)
		}
		return &HookOutput{}, nil
	}

	if state.IsImplementPhase(recipe.Phase) ||
		recipe.Phase == "finalize" || recipe.Phase == "complete" || recipe.Phase == "cancelled" {
		return allow()
	}

	recipeDir := state.RecipeDir(root, recipe.ID)

	// An open decision is a legitimate reason for the turn to end — the
	// recipe is waiting on a person, and blocking would make it
	// impossible to hand the question over. Surface it and allow.
	if open, derr := state.OpenDecisions(root, recipe.ID); derr == nil && len(open) > 0 {
		fmt.Fprintf(os.Stderr,
			"[bts] %s is blocked on %d decision(s) awaiting you: %s\n",
			recipe.ID, len(open), decisionSummary(open))
		return &HookOutput{}, nil
	}

	// C. Convergence budget exhausted. Checked first: it is the most
	// consequential state to leave unannounced, and it is terminal for
	// the loop rather than a step the model can just redo.
	if last, err := readLastVerifyEntry(filepath.Join(recipeDir, "verify-log.jsonl")); err == nil && last.Status == "failed" {
		budget := "the convergence budget"
		if last.Budget > 0 {
			budget = fmt.Sprintf("verify.max_iterations=%d", last.Budget)
		}
		return blockOutput(fmt.Sprintf(
			"The verify loop for %s gave up: %s was exhausted with %d critical, %d major, %d minor [resolvable] still open. "+
				"Do not end the turn silently. Tell the user the loop stopped converging (`bts recipe findings list --open %s`), "+
				"and record the question you need answered with `bts recipe decision hold %s --key <key> --question \"...\"` "+
				"so it survives this session.",
			lastVerifyLabel(last), budget, last.Critical, last.Major, last.EffectiveResolvable(), recipe.ID, recipe.ID,
		)), nil
	}

	// A. Verification produced but never recorded.
	if unlogged, doc := unloggedVerification(root, recipe.ID); unlogged {
		return blockOutput(fmt.Sprintf(
			".bts/specs/recipes/%s/verification.md holds a verification the log does not account for. The findings "+
				"ledger, convergence budget, and completion gate all read verify-log.jsonl, so this verification did "+
				"not happen as far as bts is concerned. Record it with `bts recipe log %s --from-verification "+
				".bts/specs/recipes/%s/verification.md --doc %s --scope <full|delta>` before ending the turn.",
			recipe.ID, recipe.ID, recipe.ID, doc,
		)), nil
	}

	// B. Verified document edited after its verification.
	if dirty, derr := state.DirtyVerifiedDocs(root, recipe.ID); derr != nil {
		fmt.Fprintf(os.Stderr,
			"[bts] warning: dirty-doc check failed: %v (proceeding without check)\n", derr)
	} else if len(dirty) > 0 {
		return blockOutput(fmt.Sprintf(
			"%s changed after its last verification. Rule 3: every modification requires /bts-verify. "+
				"Either re-verify and record it, or tell the user the doc is left unverified — do not end the turn as if it were still verified.",
			strings.Join(dirty, ", "),
		)), nil
	}

	return allow()
}

// decisionSummary renders open decision keys for a one-line message.
func decisionSummary(open []state.DecisionState) string {
	keys := make([]string, 0, len(open))
	for _, d := range open {
		keys = append(keys, d.Key)
	}
	return strings.Join(keys, ", ")
}

// lastVerifyLabel names the document a verify entry belongs to, falling
// back to the recipe-wide stream for unscoped legacy entries.
func lastVerifyLabel(e *state.VerifyLogEntry) string {
	if e.Doc != "" {
		return e.Doc
	}
	return "this recipe"
}

// unloggedVerification reports whether the verification.md on disk is a
// different document from the one the last verify-log entry recorded —
// i.e. a verification round was produced but never logged. Returns the
// doc basename the round most likely belongs to, for the recovery
// command.
//
// Comparison is by content hash, not mtime. mtime is a property of the
// checkout, not of the file's history: `git checkout` stamps every file
// it materialises with the checkout time, so an mtime comparison
// reported "newer than the last round" for every recipe carried into a
// fresh worktree, and blocked turns that had done nothing wrong. The
// recorded hash travels with the branch and says what was actually
// verified.
//
// Fails closed toward "logged" whenever the evidence is missing or
// unreadable — an absent hash (a round logged before the field existed)
// must not manufacture a block.
func unloggedVerification(root, recipeID string) (bool, string) {
	recipeDir := state.RecipeDir(root, recipeID)
	current, ok, err := state.FileContentHash(filepath.Join(recipeDir, "verification.md"))
	if err != nil || !ok {
		return false, ""
	}
	last, err := readLastVerifyEntry(filepath.Join(recipeDir, "verify-log.jsonl"))
	if err != nil {
		// verification.md exists with no log at all — the round was never
		// recorded. Name draft.md, the document the spec loop verifies.
		return true, "draft.md"
	}
	doc := last.Doc
	if doc == "" {
		doc = "draft.md"
	}
	if last.VerificationHash == "" {
		return false, doc
	}
	return current != last.VerificationHash, doc
}

// handleSpecDone validates spec recipe completion via verify-log.
func (h *stopHandler) handleSpecDone(root string, recipe *state.RecipeState) (*HookOutput, error) {
	recipeDir := state.RecipeDir(root, recipe.ID)

	// 0. A spec cannot finalize while a question that shaped it is still
	// unanswered. Fail-open on a read error, like the other gates.
	if open, derr := state.OpenDecisions(root, recipe.ID); derr == nil && len(open) > 0 {
		return blockOutput(fmt.Sprintf(
			"%d decision(s) still waiting on the user: %s. Finalizing now would bake in an answer nobody gave. "+
				"Get the answer and record it with `bts recipe decision resolve %s <key> --answer \"...\"`, "+
				"or retire the question with `bts recipe decision drop`.",
			len(open), decisionSummary(open), recipe.ID,
		)), nil
	}

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
		if out, blocked := gateBlock(root, recipe.ID, "verification_not_passed",
			lastEntry.Doc, lastEntry.DocHash, fmt.Sprintf(
				"Verification not passed for %s: %d critical, %d major, %d minor [resolvable] remain. Fix and re-verify. Deferred minors are runtime watch-items and do not block here.",
				lastEntry.Doc, lastEntry.Critical, lastEntry.Major, resolvable,
			)); blocked {
			return out, nil
		}
	}

	if lastEntry.Status == "failed" {
		budget := "unrecorded"
		if lastEntry.Budget > 0 {
			budget = fmt.Sprintf("verify.max_iterations=%d", lastEntry.Budget)
		}
		if out, blocked := gateBlock(root, recipe.ID, "convergence_budget",
			lastEntry.Doc, lastEntry.DocHash, fmt.Sprintf(
				"Last verification round is marked failed (convergence budget exhausted under %s). "+
					"The loop stopped making progress — resolve with the user rather than re-emitting DONE. "+
					"See `bts recipe findings list --open` for the findings that would not clear.",
				budget,
			)); blocked {
			return out, nil
		}
	}

	// 2a. Completion evidence: the clean result must come from a strong
	// enough measurement, repeated on one revision. This subsumes the
	// old full-pass-only rule and adds the two conditions that rule was
	// missing — every dimension, and replication. See
	// engine/completion_evidence.go for the measurements behind it.
	//
	// Entries written before the scope flag existed carry FullPass=false
	// with no Doc; only enforce when the log is doc-scoped (v0.10+).
	if lastEntry.Doc != "" {
		confirmPasses := 2
		if s, serr := engine.LoadSettings(root); serr == nil {
			confirmPasses = s.Verify.ConfirmPasses
		}
		history, herr := state.ReadVerifyLog(root, recipe.ID)
		if herr != nil {
			fmt.Fprintf(os.Stderr, "[bts] warning: read verify-log: %v\n", herr)
		}
		scoped := state.VerifyEntriesForDoc(history, lastEntry.Doc)
		if ev := engine.EvaluateCompletionEvidence(scoped, confirmPasses); !ev.Confirmed {
			// EVERY unmet condition has to be excused, not just the one
			// the evaluator names first. Reporting one at a time meant a
			// grant of `verification_not_passed` — the condition an
			// unclean round fails before any other is even looked at —
			// carried full_pass, dimensions and replication with it.
			for _, f := range ev.Failures {
				if out, blocked := gateBlock(root, recipe.ID, f.Gate,
					lastEntry.Doc, lastEntry.DocHash, fmt.Sprintf(
						"%s cannot be finalized on the evidence recorded: %s.\n%s",
						lastEntry.Doc, f.Reason, f.Remedy,
					)); blocked {
					return out, nil
				}
			}
		}
	}

	// 2a-bis. The findings ledger has to agree that nothing is still
	// owed. `absence_is_not_closure` was in the gate registry as a hard
	// gate but blocked nothing: the completion path reads verify-log
	// TOTALS, and a round's totals come from the same <bts-findings>
	// block the verifier writes. So a verifier that simply stopped
	// reporting eight majors produced a clean round, the ledger demoted
	// all eight to `unreported`, two such rounds confirmed each other,
	// and DONE went through with eight findings nobody had resolved.
	//
	// The ledger is the one record that survives a round going quiet, so
	// it is the one that can say so. This is satisfiable by construction:
	// with two clean rounds required, whatever goes `unreported` on the
	// first is confirmed `fixed` on the second, because a clean round
	// leaves every anchor quiet.
	if lastEntry.Doc != "" {
		if states, ferr := state.LoadFindings(root, recipe.ID, lastEntry.Doc); ferr == nil {
			var owed []string
			for _, st := range states {
				if state.NotClosed(st.Status) {
					owed = append(owed, fmt.Sprintf("%s [%s] %s", st.ID, st.Status, firstLine(st.Title)))
				}
			}
			if len(owed) > 0 {
				shown := owed
				if len(shown) > 5 {
					shown = shown[:5]
				}
				body := fmt.Sprintf(
					"%d finding(s) in the ledger for %s are still owed — neither confirmed fixed nor dismissed:\n  %s",
					len(owed), lastEntry.Doc, strings.Join(shown, "\n  "))
				if len(owed) > len(shown) {
					body += fmt.Sprintf("\n  ... and %d more", len(owed)-len(shown))
				}
				body += "\n\nA finding that stopped being reported is `unreported`, not fixed: absence is equally the " +
					"signature of a repair and of a verifier rewording the same defect. Resolve them and re-verify, or " +
					"dismiss the ones that are not defects with `bts recipe findings dismiss`. " +
					"See `bts recipe findings list " + recipe.ID + " --doc " + lastEntry.Doc + " --open`."
				if out, blocked := gateBlock(root, recipe.ID, "absence_is_not_closure",
					lastEntry.Doc, lastEntry.DocHash, body); blocked {
					return out, nil
				}
			}
		}
	}

	// 2b. Rule 3 hard gate: verified documents must be UNCHANGED since
	// their last verification. Mandatory verification was prompt-level
	// until v0.9.x; the content hash recorded by `recipe log --doc` makes
	// it machine-checkable, and because that hash lives in the tracked
	// verify-log it holds in a worktree too. No recorded revision →
	// nothing enforceable (legacy recipes) — gates 1-2 still apply.
	// Fail-open on read errors: a tooling failure is not a verification
	// veto (same policy as the BTS-BLOCK count below).
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
			if out, blocked := gateBlock(root, recipe.ID, "deferred_minors_declared",
				lastEntry.Doc, lastEntry.DocHash, fmt.Sprintf(
					"%d minor [deferred] finding(s) recorded but %s has no '## Known Uncertainties' entries (### U-NNN form). Per blueprint rule 3b, append each deferred minor with its Why-deferred: line, re-run /bts-verify (the append is a modification — rule 3), then re-emit DONE.",
					lastEntry.MinorDeferred, filepath.Base(specPath),
				)); blocked {
				return out, nil
			}
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
			if out, blocked := gateBlock(root, recipe.ID, "simulate_at_least_once",
				lastEntry.Doc, lastEntry.DocHash,
				"No simulate action in changelog. Rule 5: run /bts-simulate (5+ scenarios) at least once before declaring Level 3, then /bts-sync-check, then re-emit DONE."); blocked {
				return out, nil
			}
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
	// a verified revision was recorded; legacy fix recipes pass through.)
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

// overrideAllows reports whether a recorded operator override lets this
// turn proceed past `gate`, and the note to print when it does.
//
// An override is deliberately noisy rather than silent. The point is not
// to make the gate stop mattering — it is to make the bypass leave a
// mark, on this turn's stderr and in the recipe's tracked state, instead
// of happening outside the system where nothing can see it.
//
// A stale override — one granted on a different revision — does NOT
// apply. The operator weighed a specific text; an edit since then is
// exactly when that judgement has to be made again.
// gateBlock renders a hard-gate block, first honouring any recorded
// override for that gate. It returns (nil, false) when an override
// applies, so the caller falls through to the next gate.
//
// Every overridable gate goes through here so the footer names the grant
// invocation the CLI will actually accept. Before this, one hard-coded
// footer served all of them and named `--finding <F-...>` for gates that
// fire on rounds with no findings at all.
func gateBlock(root, recipeID, gate, doc, docHash, body string) (*HookOutput, bool) {
	if ok, note := overrideAllows(root, recipeID, gate, doc, docHash); ok {
		fmt.Fprintf(os.Stderr, "[bts] %s\n", note)
		return nil, false
	}
	if footer := overrideFooter(recipeID, gate, doc); footer != "" {
		body += "\n\n" + footer
	}
	return blockOutput(body), true
}

func overrideFooter(recipeID, gate, doc string) string {
	if !engine.IsOverridableGate(gate) {
		return ""
	}
	selector := "--no-findings"
	if engine.GateExcusesFindings(gate) {
		selector = "--finding <F-...>"
	}
	docArg := ""
	if doc != "" {
		docArg = " --doc " + doc
	}
	return fmt.Sprintf(
		"If proceeding is the right call, record it rather than working around it:\n"+
			"  bts recipe override grant %s --gate %s%s \\\n"+
			"      %s --reason \"<why this is acceptable>\"\n"+
			"The recipe then reports as overridden in status, doctor and stats — which an "+
			"undocumented bypass does not.",
		recipeID, gate, docArg, selector)
}

func overrideAllows(root, recipeID, gate, docBase, docHash string) (bool, string) {
	if gate == "" {
		return false, ""
	}
	// When the ROUND recorded no revision, fall back to what the document
	// hashes to right now. Without this, `revision_recorded_before_final`
	// was unoverridable in principle: `override grant` refuses to record
	// an unpinned override, and every pinned one reads as stale against
	// an empty round hash — so the block message told the operator to run
	// a command whose result could never apply. Hashing the file answers
	// the question the pin actually asks: is the text in front of us the
	// text that was judged.
	if docHash == "" && docBase != "" {
		if h, ok, err := state.FileContentHash(
			filepath.Join(state.RecipeDir(root, recipeID), docBase)); err == nil && ok {
			docHash = h
		}
	}
	// The CLI refuses to grant an override for a non-overridable gate,
	// but overrides.jsonl is a plain file in the repo. Re-check here so a
	// hand-written record cannot excuse a gate that protects the
	// integrity of the record rather than a judgement call.
	if !engine.IsOverridableGate(gate) {
		return false, ""
	}
	records, err := state.ReadOverrides(root, recipeID)
	if err != nil || len(records) == 0 {
		return false, ""
	}
	st := state.ActiveOverride(records, gate, docBase, docHash)
	switch {
	case st.Active:
		note := fmt.Sprintf("proceeding past %s under a recorded override", gate)
		if n := len(st.Record.Findings); n > 0 {
			note += fmt.Sprintf(" excusing %d finding(s)", n)
		}
		note += ": " + firstLine(st.Record.Reason)
		return true, note
	case st.Stale:
		on := "an unrecorded revision"
		if docHash != "" {
			on = "revision " + shortRev(docHash)
		}
		fmt.Fprintf(os.Stderr,
			"[bts] the %s override was granted on revision %s of %s but this round is on %s, so it no longer applies — "+
				"re-grant it against the current text if the judgement still holds\n",
			gate, shortRev(st.Granted), docBase, on)
	}
	return false, ""
}

// shortRev trims a digest to something a hook line can carry.
func shortRev(h string) string {
	h = strings.TrimPrefix(h, "sha256:")
	if len(h) > 12 {
		return h[:12]
	}
	return h
}

// firstLine trims a multi-line reason to something a hook line can carry.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return state.TruncateRunes(strings.TrimSpace(s), 160)
}
