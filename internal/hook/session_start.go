package hook

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/imtemp-dev/claude-bts/internal/metrics"
	"github.com/imtemp-dev/claude-bts/internal/state"
	"github.com/imtemp-dev/claude-bts/internal/template"
	"github.com/imtemp-dev/claude-bts/pkg/version"
)

type sessionStartHandler struct{}

func NewSessionStartHandler() Handler {
	return &sessionStartHandler{}
}

func (h *sessionStartHandler) EventType() EventType {
	return EventSessionStart
}

func (h *sessionStartHandler) Handle(input *HookInput) (*HookOutput, error) {
	root, err := input.ProjectRoot()
	if err != nil {
		return &HookOutput{}, nil
	}

	// A subagent's compaction (a fork, an adversary) gets no recipe resume
	// context: its task is the one it was given, and the orchestrator's
	// next step injected here could pull it off that task.  It is not a
	// session either, so no template update and no session metric (trial
	// problem #114).
	if input.IsSubagent() {
		return &HookOutput{
			HookSpecificOutput: &HookSpecificOutput{
				HookEventName: "SessionStart",
				AdditionalContext: "[bts] Context compacted inside a subagent. Keep to the task you were given; " +
					"the recipe's next step belongs to the main session.",
			},
		}, nil
	}

	// Auto-update templates if binary version changed; held is the line
	// that replaces the update while a recipe is active.
	updated, held := autoUpdateTemplates(root)

	// Try to load work state for rich context recovery.
	// ws may reference a recipe that has since been cancelled/deleted; we
	// validate below after loading the actual active recipe and discard
	// ws if it is stale so it never taints the recovery message.
	ws, _ := state.LoadWorkState(root)
	source := detectSource(root, input, ws)

	// Emit session_start metric (fire-and-forget)
	metricsEvent := &metrics.MetricsEvent{
		Kind:      metrics.KindSessionStart,
		SessionID: input.SessionID,
		Model:     input.Model,
		Source:    source,
	}
	defer func() { _ = metrics.Append(root, metricsEvent) }()

	recipe, err := state.GetActiveRecipe(root)
	if err != nil || recipe == nil {
		// Check for finalized recipes ready for implementation
		recipe, err = state.GetFinalizedRecipe(root)
		if err != nil || recipe == nil {
			// No recipe at all — handle non-recipe post-compact recovery first
			if source == "compact" {
				if msg := buildNonRecipeCompactMsg(root, updated); msg != "" {
					return &HookOutput{
						HookSpecificOutput: &HookSpecificOutput{
							HookEventName:     "SessionStart",
							AdditionalContext: msg,
						},
					}, nil
				}
			}

			// Check roadmap for next-item hint
			done, total, nextItem := state.RoadmapProgress(root)
			if total > 0 {
				var msg string
				if nextItem != "" {
					msg = fmt.Sprintf("[bts] Roadmap: %d/%d done. Next: %s\nRun /bts-recipe-blueprint to start.", done, total, nextItem)
				} else {
					msg = fmt.Sprintf("[bts] Roadmap complete: %d/%d done. Run /bts-recipe-blueprint to add new items or start a new vision.", done, total)
				}
				if updated {
					msg = fmt.Sprintf("[bts] Templates updated to %s\n%s", version.GetTemplateVersion(), msg)
				}
				return &HookOutput{
					HookSpecificOutput: &HookSpecificOutput{
						HookEventName:     "SessionStart",
						AdditionalContext: msg,
					},
				}, nil
			}
			// Check for incomplete vision (DRAFT without a recipe yet)
			if state.VisionExists(root) {
				visionData, _ := os.ReadFile(filepath.Join(state.SpecsPath(root), "vision.md"))
				if state.DocStatus(string(visionData)) == "DRAFT" {
					msg := "[bts] Vision document in progress (Status: DRAFT).\nRun /bts-recipe-blueprint to continue."
					if updated {
						msg = fmt.Sprintf("[bts] Templates updated to %s\n%s", version.GetTemplateVersion(), msg)
					}
					return &HookOutput{
						HookSpecificOutput: &HookSpecificOutput{
							HookEventName:     "SessionStart",
							AdditionalContext: msg,
						},
					}, nil
				}
			}
			if updated {
				return &HookOutput{
					HookSpecificOutput: &HookSpecificOutput{
						HookEventName:     "SessionStart",
						AdditionalContext: fmt.Sprintf("[bts] Templates updated to %s", version.GetTemplateVersion()),
					},
				}, nil
			}
			return &HookOutput{}, nil
		}

		metricsEvent.RecipeID = recipe.ID
		metricsEvent.Phase = recipe.Phase

		// Discard ws if it points to a different recipe than the one we
		// just found — otherwise ws.Summary would describe the wrong one.
		if ws != nil && ws.RecipeID != recipe.ID {
			ws = nil
		}

		msg := fmt.Sprintf(
			"[bts] Recipe ready for implementation: %s \"%s\" (ID: %s)\nRun /bts-implement %s to start coding.",
			recipe.Type, recipe.Topic, recipe.ID, recipe.ID,
		)

		// Enrich with work state if resuming
		if ws != nil && (source == "compact" || source == "resume") {
			msg = fmt.Sprintf("[bts] Resuming. %s\nRun /bts-implement %s to start coding.", ws.Summary, recipe.ID)
		}

		msg += openDecisionNotice(root, recipe.ID)
		msg += dirtyDocWarning(root, recipe)

		if updated {
			msg = fmt.Sprintf("[bts] Templates updated to %s\n%s", version.GetTemplateVersion(), msg)
		}

		return &HookOutput{
			HookSpecificOutput: &HookSpecificOutput{
				HookEventName:     "SessionStart",
				AdditionalContext: msg,
			},
		}, nil
	}

	// Another session in the project: one line, no resume hints, and its
	// events stay out of the recipe's metrics.
	if isObserver(recipe, input.SessionID) {
		metricsEvent.Observer = true
		return &HookOutput{
			HookSpecificOutput: &HookSpecificOutput{
				HookEventName:     "SessionStart",
				AdditionalContext: held + observerNotice(recipe),
			},
		}, nil
	}

	metricsEvent.RecipeID = recipe.ID
	metricsEvent.Phase = recipe.Phase

	// Discard ws if it points to a different recipe than the active one —
	// e.g. a previous recipe was cancelled and a new one started. The ws
	// snapshot is frozen at PreCompact time and would otherwise show the
	// old recipe's summary alongside the new recipe's hint.
	if ws != nil && ws.RecipeID != recipe.ID {
		ws = nil
	}

	// Build hint — actionable content only, no source prefix.
	// All sources (startup, resume, compact) get the same hint quality.
	hint := buildHint(root, recipe, ws)

	// Build message — source prefix in msg, hint appended with NEXT:
	var msg string
	switch source {
	case "resume":
		if ws != nil {
			msg = fmt.Sprintf("[bts] Session restored. %s\nNEXT: %s", ws.Summary, hint)
		} else {
			msg = fmt.Sprintf("[bts] Session restored. %s \"%s\" (Step: %s)\nNEXT: %s",
				recipe.Type, recipe.Topic, recipe.Phase, hint)
		}
	case "compact":
		if ws != nil {
			msg = fmt.Sprintf("[bts] Context compacted. %s\nNEXT: %s", ws.Summary, hint)
		} else {
			msg = fmt.Sprintf("[bts] Context compacted. %s \"%s\" (Step: %s)\nNEXT: %s",
				recipe.Type, recipe.Topic, recipe.Phase, hint)
		}
	default:
		msg = fmt.Sprintf(
			"[bts] Active recipe: %s \"%s\" (Step: %s, Iteration: %d)\nNEXT: %s",
			recipe.Type, recipe.Topic, recipe.Phase, recipe.Iteration, hint,
		)
	}

	msg += openDecisionNotice(root, recipe.ID)
	msg += dirtyDocWarning(root, recipe)

	if updated {
		msg = fmt.Sprintf("[bts] Templates updated to %s\n%s", version.GetTemplateVersion(), msg)
	}
	msg = held + msg

	return &HookOutput{
		HookSpecificOutput: &HookSpecificOutput{
			HookEventName:     "SessionStart",
			AdditionalContext: msg,
		},
	}, nil
}

// openDecisionNotice surfaces questions the recipe is waiting on. A new
// session has no memory of the conversation that raised them, so without
// this the recipe looks like ordinary in-progress work and the loop
// resumes as if nobody owed an answer. Best-effort: a read error produces
// no notice.
func openDecisionNotice(root, recipeID string) string {
	open, err := state.OpenDecisions(root, recipeID)
	if err != nil || len(open) == 0 {
		return ""
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "\n⛔ BLOCKED on %d decision(s) awaiting you — do not resume the loop until they are answered:", len(open))
	for _, d := range open {
		fmt.Fprintf(&sb, "\n   • [%s] %s", d.Key, strings.Join(strings.Fields(d.Question), " "))
		if len(d.Options) > 0 {
			fmt.Fprintf(&sb, "\n     options: %s", strings.Join(d.Options, " | "))
		}
		fmt.Fprintf(&sb, "\n     record the answer: bts recipe decision resolve %s %s --answer \"...\"", recipeID, d.Key)
	}
	return sb.String()
}

// dirtyDocWarning returns a rule-3 warning when verified documents were
// modified after their last verification (per verify snapshots), ""
// otherwise. Best-effort: errors and legacy recipes (no snapshots)
// produce no warning.
//
// Rule 3 is a spec-phase rule. After implementation starts, sync edits
// final.md to record what the tested code does; the code and its runs are
// the truth then, and the stop hook's blind-stop backstop already lets
// these phases end (handleBlindStop). Asking for /bts-verify there sends
// the session back into model review rounds on a record of tested code.
func dirtyDocWarning(root string, recipe *state.RecipeState) string {
	if state.IsImplementPhase(recipe.Phase) {
		return ""
	}
	dirty, err := state.DirtyVerifiedDocs(root, recipe.ID)
	if err != nil || len(dirty) == 0 {
		return ""
	}
	return fmt.Sprintf(
		"\n⚠ %s modified after last verification — run /bts-verify before continuing (rule 3).",
		state.DescribeDirty(root, recipe.ID, dirty))
}

// buildHint chooses the most actionable next-step description.
// Priority: fresh assess.json → cached ws.NextAction → sub-state guidance → phase heuristics.
func buildHint(root string, recipe *state.RecipeState, ws *state.WorkState) string {
	// 1a. Re-read assess.json for freshness — it may have been updated
	// after the PreCompact snapshot was taken.
	if fresh := state.LoadAssessNextAction(root, recipe.ID); fresh != "" {
		return fresh
	}
	// 1b. Fallback to cached value if somehow fresh read fails but ws had it.
	if ws != nil && ws.NextAction != "" {
		return ws.NextAction
	}

	// 2. Sub-state guidance (a debate in progress)
	if ws != nil && ws.SubState != nil && ws.SubState.Kind == "debate" {
		return fmt.Sprintf("Resume debate (%s). Run /bts-debate to continue.", ws.SubState.Position)
	}

	// 3. Phase-specific hints (existing logic)
	if recipe.Phase == "discovery" {
		return fmt.Sprintf("Read .bts/specs/recipes/%s/intent.md and continue intent discovery.", recipe.ID)
	}
	if recipe.Phase == "scoping" {
		return fmt.Sprintf("Read .bts/specs/recipes/%s/scope.md and confirm or adjust scope.", recipe.ID)
	}
	if next := nextStepHint(root, recipe); next != "" {
		return next
	}
	if state.IsImplementPhase(recipe.Phase) {
		implCmd := fmt.Sprintf("/bts-implement %s", recipe.ID)
		if recipe.Type == "fix" {
			implCmd = fmt.Sprintf("/bts-recipe-fix %s", recipe.ID)
		}
		return fmt.Sprintf("Run %s to continue.", implCmd)
	}
	return fmt.Sprintf("Run /bts-recipe-%s to re-enter the recipe, or /recipe cancel to abort.", recipe.Type)
}

// buildNonRecipeCompactMsg returns a recovery message for compactions that
// happened with no active or finalized recipe. Returns empty string if
// there is nothing useful to say (falls through to the default branches).
func buildNonRecipeCompactMsg(root string, templatesUpdated bool) string {
	ss, err := state.LoadSessionState(root)
	if err != nil || ss == nil {
		return ""
	}
	if len(ss.RecentTools) == 0 && ss.PendingPlan == "" {
		return ""
	}
	var b strings.Builder
	if templatesUpdated {
		fmt.Fprintf(&b, "[bts] Templates updated to %s\n", version.GetTemplateVersion())
	}
	b.WriteString("[bts] Context compacted (no active recipe).")
	if len(ss.OpenFiles) > 0 {
		b.WriteString("\nOpen files: ")
		b.WriteString(strings.Join(ss.OpenFiles, ", "))
		b.WriteString(".")
	}
	if len(ss.RecentTools) > 0 {
		last := ss.RecentTools[len(ss.RecentTools)-1]
		detail := last.ToolName
		if last.File != "" {
			detail += "(" + last.File + ")"
		}
		b.WriteString("\nLast tool: ")
		b.WriteString(detail)
		b.WriteString(".")
	}
	if ss.PendingPlan != "" {
		b.WriteString("\nPending plan: ")
		b.WriteString(ss.PendingPlan)
	}
	b.WriteString("\nContinue from where you were.")
	return b.String()
}

// autoUpdateTemplates runs template.Update, the same routine as `bts
// update`, so the deploy and the cleanup of retired files can no longer
// diverge between the two paths.  It reports true when templates were
// deployed.  Cleanup notes and warnings go to stderr: this hook's stdout
// is the JSON the harness parses.
//
// While a recipe is active it deploys nothing (trial problem #55): a
// second worker's session start moved r-003's templates from 944b3e7 to
// 8bfa2c8 mid-recipe, and true-shark's moved at its architect step, so one
// recipe ran its steps under two sets of skills with no record of the
// switch.  Only the idempotent cleanup runs then, and held is one line of
// session-start context ("" when the templates are current) telling the
// user to move them with `bts update` between steps.  A finalized recipe
// does not hold them: finalize is where analyze, design and debug recipes
// stop for good, and implementation starting from it is itself a step
// boundary.
func autoUpdateTemplates(root string) (updated bool, held string) {
	current := version.GetTemplateVersion()
	if active, _ := state.GetActiveRecipe(root); active != nil {
		res := template.CleanupOnly(root)
		reportTemplateResult(res, nil)
		if res.OldVersion != current {
			old := res.OldVersion
			if old == "" {
				old = "an unrecorded version"
			}
			held = fmt.Sprintf("[bts] Templates are at %s and the bts binary is at %s; recipe %s is active, so they were not updated. "+
				"Run `bts update` between steps to move them.\n", old, current, active.ID)
		}
		return false, held
	}
	res, err := template.Update(root, current)
	reportTemplateResult(res, err)
	return res.Deployed, ""
}

// reportTemplateResult writes an update's warnings and notes to stderr.
func reportTemplateResult(res *template.UpdateResult, err error) {
	for _, w := range res.Warnings {
		fmt.Fprintf(os.Stderr, "[bts] warning: %s\n", w)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "[bts] warning: %v\n", err)
	}
	for _, n := range res.Notes {
		fmt.Fprintf(os.Stderr, "[bts] %s\n", n)
	}
}

// interrogateVerified reports whether `bts interrogate verify` has read
// the adversaries' reports for the current round.
func interrogateVerified(root, recipeID string) bool {
	run, err := state.LoadInterrogateRun(root, recipeID)
	return err == nil && run != nil && strings.TrimSpace(run.VerifiedAt) != ""
}

// nextStepHint returns a specific next-action hint based on recipe phase and state files.
// For implementation phases, always guides back to the orchestrator (/bts-implement)
// to maintain flow continuity, with a description of what step comes next.
func nextStepHint(root string, recipe *state.RecipeState) string {
	recipeDir := state.RecipeDir(root, recipe.ID)
	exists := func(name string) bool {
		_, err := os.Stat(filepath.Join(recipeDir, name))
		return err == nil
	}

	// Spec phases — guide to specific commands
	switch {
	case recipe.Phase == "discovery":
		return "Continue intent discovery — read intent.md and resume conversation."
	case recipe.Phase == "scoping":
		return "Read scope.md and confirm or adjust scope."
	case recipe.Phase == "wireframe":
		return "Run /bts-wireframe to design system structure."
	case recipe.Phase == "finalize":
		return fmt.Sprintf("Spec finalized. Run /bts-implement %s to start implementation.", recipe.ID)
	case recipe.Phase == "skeleton":
		return fmt.Sprintf("Run /bts-skeleton %s: write the decomposition as code that compiles, then `bts check build`.", recipe.ID)
	case recipe.Phase == "falsify" && recipe.Type == "build":
		return buildTailHint(root, recipe)
	case recipe.Phase == "falsify":
		return fmt.Sprintf("Run /bts-falsify %s: one failing test per invariant, recorded with `bts falsifier run`.", recipe.ID)
	case recipe.Type == "build" && !state.IsImplementPhase(recipe.Phase):
		return fmt.Sprintf("Resume /bts-recipe-build %s from its phase.", recipe.ID)
	case !state.IsImplementPhase(recipe.Phase):
		return "Run /bts-assess on draft.md to determine next action."
	}

	// Implementation phases — always guide to orchestrator
	implCmd := fmt.Sprintf("/bts-implement %s", recipe.ID)
	if recipe.Type == "fix" {
		implCmd = fmt.Sprintf("/bts-recipe-fix %s", recipe.ID)
	}

	var detail string
	switch recipe.Phase {
	case "implement":
		if !exists("tasks.json") {
			detail = "decompose tasks from spec"
		} else {
			detail = "continue task implementation"
		}
	case "test":
		// The order of the bts-implement resume table.  The hint used to
		// skip Step 5.2 and to send an agent to drive the app as soon as
		// `bts interrogate prepare` had written interrogate.json, before
		// any adversary reported (trial problem #101).
		switch {
		case !exists("test-results.json"):
			detail = "run tests"
		case recipe.Type == "build" && buildUnmutatedInvariants(root, recipe) > 0:
			detail = "show each falsifier can fail (Step 5.2): break each introduced invariant's guard and record it with `bts falsifier mutate`"
		case exists("prove.json") && exists("review.md"):
			detail = "sync spec with implementation"
		case exists("prove.json"):
			detail = "run code review"
		case interrogateVerified(root, recipe.ID):
			detail = "drive the app (/bts-prove)"
		case exists("interrogate.json"):
			detail = "finish the interrogate round: read each adversary's report and record it with `bts interrogate verify`"
		default:
			detail = "run the prove stage: /bts-interrogate, then /bts-prove"
		}
	case "review":
		if exists("review.md") {
			detail = "sync spec with implementation"
		} else {
			detail = "run code review"
		}
	case "sync":
		detail = "sync spec with implementation"
	case "status":
		if exists("tasks.json") && exists("test-results.json") &&
			exists("review.md") && exists("deviation.md") {
			detail = "complete recipe"
		} else {
			detail = "update project status"
		}
	default:
		return ""
	}

	return fmt.Sprintf("Run %s to continue (next: %s).", implCmd, detail)
}

// detectSource determines the session source.
// Order: explicit input.Source → compact marker → SavedAt heuristic.
func detectSource(root string, input *HookInput, ws *state.WorkState) string {
	// Use explicit source if Claude Code provides it
	if input.Source != "" {
		return input.Source
	}

	// Deterministic marker from PreCompact — consume it so we don't
	// misidentify a subsequent startup as another compaction.
	if m, err := state.ConsumeCompactMarker(root); err == nil && m != nil {
		return "compact"
	}

	// Fallback heuristic for older Claude Code versions that didn't run
	// PreCompact or that drop the source field.
	if ws == nil {
		return "startup"
	}

	savedAt, err := time.Parse(time.RFC3339, ws.SavedAt)
	if err != nil {
		return "resume"
	}

	// If saved within 120 seconds, likely a compaction or clear
	if time.Since(savedAt) < 120*time.Second {
		return "compact"
	}

	return "resume"
}
