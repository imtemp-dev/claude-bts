package hook

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/imtemp-dev/claude-jig/internal/metrics"
	"github.com/imtemp-dev/claude-jig/internal/state"
	"github.com/imtemp-dev/claude-jig/internal/template"
	"github.com/imtemp-dev/claude-jig/pkg/version"
)

type sessionStartHandler struct{}

func NewSessionStartHandler() Handler {
	return &sessionStartHandler{}
}

func (h *sessionStartHandler) EventType() EventType {
	return EventSessionStart
}

func (h *sessionStartHandler) Handle(input *HookInput) (*HookOutput, error) {
	root, err := state.FindRoot(input.CWD)
	if err != nil {
		return &HookOutput{}, nil
	}

	// Auto-update templates if binary version changed
	updated := autoUpdateTemplates(root)

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
					msg = fmt.Sprintf("[jig] Roadmap: %d/%d done. Next: %s\nRun /jig-spec to start.", done, total, nextItem)
				} else {
					msg = fmt.Sprintf("[jig] Roadmap complete: %d/%d done. Run /jig-spec to add new items or start a new vision.", done, total)
				}
				if updated {
					msg = fmt.Sprintf("[jig] Templates updated to %s\n%s", version.GetTemplateVersion(), msg)
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
				if strings.Contains(string(visionData), "Status: DRAFT") {
					msg := "[jig] Vision document in progress (Status: DRAFT).\nRun /jig-spec to continue."
					if updated {
						msg = fmt.Sprintf("[jig] Templates updated to %s\n%s", version.GetTemplateVersion(), msg)
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
						AdditionalContext: fmt.Sprintf("[jig] Templates updated to %s", version.GetTemplateVersion()),
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
			"[jig] Recipe ready for implementation: %s \"%s\" (ID: %s)\nRun /jig-implement %s to start coding.",
			recipe.Type, recipe.Topic, recipe.ID, recipe.ID,
		)

		// Enrich with work state if resuming
		if ws != nil && (source == "compact" || source == "resume") {
			msg = fmt.Sprintf("[jig] Resuming. %s\nRun /jig-implement %s to start coding.", ws.Summary, recipe.ID)
		}

		msg += openDecisionNotice(root, recipe.ID)
		msg += dirtyDocWarning(root, recipe.ID)

		if updated {
			msg = fmt.Sprintf("[jig] Templates updated to %s\n%s", version.GetTemplateVersion(), msg)
		}

		return &HookOutput{
			HookSpecificOutput: &HookSpecificOutput{
				HookEventName:     "SessionStart",
				AdditionalContext: msg,
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
			msg = fmt.Sprintf("[jig] Session restored. %s\nNEXT: %s", ws.Summary, hint)
		} else {
			msg = fmt.Sprintf("[jig] Session restored. %s \"%s\" (Step: %s)\nNEXT: %s",
				recipe.Type, recipe.Topic, recipe.Phase, hint)
		}
	case "compact":
		if ws != nil {
			msg = fmt.Sprintf("[jig] Context compacted. %s\nNEXT: %s", ws.Summary, hint)
		} else {
			msg = fmt.Sprintf("[jig] Context compacted. %s \"%s\" (Step: %s)\nNEXT: %s",
				recipe.Type, recipe.Topic, recipe.Phase, hint)
		}
	default:
		msg = fmt.Sprintf(
			"[jig] Active recipe: %s \"%s\" (Step: %s, Iteration: %d)\nNEXT: %s",
			recipe.Type, recipe.Topic, recipe.Phase, recipe.Iteration, hint,
		)
	}

	msg += openDecisionNotice(root, recipe.ID)
	msg += dirtyDocWarning(root, recipe.ID)

	if updated {
		msg = fmt.Sprintf("[jig] Templates updated to %s\n%s", version.GetTemplateVersion(), msg)
	}

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
		fmt.Fprintf(&sb, "\n     record the answer: jig recipe decision resolve %s %s --answer \"...\"", recipeID, d.Key)
	}
	return sb.String()
}

// dirtyDocWarning returns a rule-3 warning when verified documents were
// modified after their last verification (per verify snapshots), ""
// otherwise. Best-effort: errors and legacy recipes (no snapshots)
// produce no warning.
func dirtyDocWarning(root, recipeID string) string {
	dirty, err := state.DirtyVerifiedDocs(root, recipeID)
	if err != nil || len(dirty) == 0 {
		return ""
	}
	return fmt.Sprintf(
		"\n⚠ %s modified after last verification — run /jig-verify before continuing (rule 3).",
		strings.Join(dirty, ", "))
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

	// 2. Sub-state guidance (debate/simulate in progress)
	if ws != nil && ws.SubState != nil {
		switch ws.SubState.Kind {
		case "debate":
			return fmt.Sprintf("Resume debate (%s). Run /jig-debate to continue.", ws.SubState.Position)
		case "simulate":
			return fmt.Sprintf("Resume simulation (%s). Run /jig-simulate to continue.", ws.SubState.Position)
		}
	}

	// 3. Phase-specific hints (existing logic)
	if recipe.Phase == "discovery" {
		return fmt.Sprintf("Read .jig/specs/recipes/%s/intent.md and continue intent discovery.", recipe.ID)
	}
	if recipe.Phase == "scoping" {
		return fmt.Sprintf("Read .jig/specs/recipes/%s/scope.md and confirm or adjust scope.", recipe.ID)
	}
	if next := nextStepHint(root, recipe); next != "" {
		return next
	}
	if state.IsImplementPhase(recipe.Phase) {
		implCmd := fmt.Sprintf("/jig-implement %s", recipe.ID)
		if recipe.Type == "fix" {
			implCmd = fmt.Sprintf("/jig-fix %s", recipe.ID)
		}
		return fmt.Sprintf("Run %s to continue.", implCmd)
	}
	return fmt.Sprintf("Run /jig-%s to re-enter the recipe, or `jig recipe cancel` to abort.", recipe.Type)
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
		fmt.Fprintf(&b, "[jig] Templates updated to %s\n", version.GetTemplateVersion())
	}
	b.WriteString("[jig] Context compacted (no active recipe).")
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

// autoUpdateTemplates checks if templates need updating and deploys if so.
// Returns true if templates were updated, false if skipped.
func autoUpdateTemplates(root string) bool {
	versionFile := filepath.Join(root, ".jig", "config", ".template-version")
	existing, _ := os.ReadFile(versionFile)

	current := version.GetTemplateVersion()

	if strings.TrimSpace(string(existing)) == current {
		return false // same version, skip
	}

	// Deploy templates (overwrite all except user-owned files).
	// .gitignore is user-owned — merged via EnsureGitignore below, never overwritten.
	_, _ = template.DeployForce(root, []string{
		".jig/config/settings.yaml",
		".mcp.json",
		".gitignore",
	})

	// Ensure .gitignore ignores jig local data without destroying existing rules.
	_ = template.EnsureGitignore(root)

	// Drop templates left under a retired name prefix and repoint hook paths
	// at the current scripts. The binary can be upgraded out from under a
	// project (brew, curl), so this auto-update path — not just an explicit
	// `jig update` — is where most users cross a rebrand.
	template.CleanupLegacyPrefixes(root)
	template.MigrateHookSettings(root)

	// Ensure hooks are registered in settings.local.json
	_ = template.MergeHookSettings(root)

	// Clean up legacy forge files if present
	cleanupLegacyTemplates(root)

	// Record new version
	if err := os.WriteFile(versionFile, []byte(current), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "warning: save template version: %v\n", err)
	}
	return true
}

// cleanupLegacyTemplates removes old forge-* files from .claude/ directories.
func cleanupLegacyTemplates(root string) {
	claudeDir := filepath.Join(root, ".claude")
	for _, d := range []string{"skills", "agents", "rules", "hooks", "commands"} {
		base := filepath.Join(claudeDir, d)
		entries, err := os.ReadDir(base)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), "forge-") {
				_ = os.RemoveAll(filepath.Join(base, entry.Name()))
			}
		}
	}

	// Migrate hook settings
	settingsPath := filepath.Join(claudeDir, "settings.local.json")
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		return
	}
	content := string(data)
	if strings.Contains(content, "forge-handle-") {
		content = strings.ReplaceAll(content, "forge-handle-", "jig-handle-")
		content = strings.ReplaceAll(content, ".forge/status_line.sh", ".jig/status_line.sh")
		_ = os.WriteFile(settingsPath, []byte(content), 0644)
	}
}

// nextStepHint returns a specific next-action hint based on recipe phase and state files.
// For implementation phases, always guides back to the orchestrator (/jig-implement)
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
		return "Run /jig-wireframe to design system structure."
	case recipe.Phase == "finalize":
		return fmt.Sprintf("Spec finalized. Run /jig-implement %s to start implementation.", recipe.ID)
	case !state.IsImplementPhase(recipe.Phase):
		return "Run /jig-assess on draft.md to determine next action."
	}

	// Implementation phases — always guide to orchestrator
	implCmd := fmt.Sprintf("/jig-implement %s", recipe.ID)
	if recipe.Type == "fix" {
		implCmd = fmt.Sprintf("/jig-fix %s", recipe.ID)
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
		simsDir := filepath.Join(recipeDir, "simulations")
		simsExist := false
		if entries, err := os.ReadDir(simsDir); err == nil && len(entries) > 0 {
			simsExist = true
		}
		if exists("test-results.json") && simsExist && exists("review.md") {
			detail = "sync spec with implementation"
		} else if exists("test-results.json") && simsExist {
			detail = "run code review"
		} else if exists("test-results.json") {
			detail = "run code simulation"
		} else {
			detail = "run tests"
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
