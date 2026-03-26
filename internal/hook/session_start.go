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
	root, err := state.FindRoot(input.CWD)
	if err != nil {
		return &HookOutput{}, nil
	}

	// Auto-update templates if binary version changed
	updated := autoUpdateTemplates(root)

	// Try to load work state for rich context recovery
	ws, _ := state.LoadWorkState(root)
	source := detectSource(input, ws)

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
						AdditionalContext: msg,
					},
				}, nil
			}
			// Check for incomplete vision (DRAFT without a recipe yet)
			if state.VisionExists(root) {
				visionData, _ := os.ReadFile(filepath.Join(state.SpecsPath(root), "vision.md"))
				if strings.Contains(string(visionData), "Status: DRAFT") {
					msg := "[bts] Vision document in progress (Status: DRAFT).\nRun /bts-recipe-blueprint to continue."
					if updated {
						msg = fmt.Sprintf("[bts] Templates updated to %s\n%s", version.GetTemplateVersion(), msg)
					}
					return &HookOutput{
						HookSpecificOutput: &HookSpecificOutput{
							AdditionalContext: msg,
						},
					}, nil
				}
			}
			if updated {
				return &HookOutput{
					HookSpecificOutput: &HookSpecificOutput{
						AdditionalContext: fmt.Sprintf("[bts] Templates updated to %s", version.GetTemplateVersion()),
					},
				}, nil
			}
			return &HookOutput{}, nil
		}

		metricsEvent.RecipeID = recipe.ID
		metricsEvent.Phase = recipe.Phase

		msg := fmt.Sprintf(
			"[bts] Recipe ready for implementation: %s \"%s\" (ID: %s)\nRun /bts-implement %s to start coding.",
			recipe.Type, recipe.Topic, recipe.ID, recipe.ID,
		)

		// Enrich with work state if resuming
		if ws != nil && (source == "compact" || source == "resume") {
			msg = fmt.Sprintf("[bts] Resuming. %s\nRun /bts-implement %s to start coding.", ws.Summary, recipe.ID)
		}

		if updated {
			msg = fmt.Sprintf("[bts] Templates updated to %s\n%s", version.GetTemplateVersion(), msg)
		}

		return &HookOutput{
			HookSpecificOutput: &HookSpecificOutput{
				AdditionalContext: msg,
			},
		}, nil
	}

	metricsEvent.RecipeID = recipe.ID
	metricsEvent.Phase = recipe.Phase

	// Build hint — actionable content only, no source prefix.
	// All sources (startup, resume, compact) get the same hint quality.
	var hint string
	if recipe.Phase == "discovery" {
		hint = fmt.Sprintf("Read .bts/specs/recipes/%s/intent.md and continue intent discovery.", recipe.ID)
	} else if recipe.Phase == "scoping" {
		hint = fmt.Sprintf("Read .bts/specs/recipes/%s/scope.md and confirm or adjust scope.", recipe.ID)
	} else if next := nextStepHint(root, recipe); next != "" {
		hint = next
	} else if state.IsImplementPhase(recipe.Phase) {
		implCmd := fmt.Sprintf("/bts-implement %s", recipe.ID)
		if recipe.Type == "fix" {
			implCmd = fmt.Sprintf("/bts-recipe-fix %s", recipe.ID)
		}
		hint = fmt.Sprintf("Run %s to continue.", implCmd)
	} else {
		hint = fmt.Sprintf("Run /bts-recipe-%s to re-enter the recipe, or /recipe cancel to abort.", recipe.Type)
	}

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

	if updated {
		msg = fmt.Sprintf("[bts] Templates updated to %s\n%s", version.GetTemplateVersion(), msg)
	}

	return &HookOutput{
		HookSpecificOutput: &HookSpecificOutput{
			AdditionalContext: msg,
		},
	}, nil
}

// autoUpdateTemplates checks if templates need updating and deploys if so.
// Returns true if templates were updated, false if skipped.
func autoUpdateTemplates(root string) bool {
	versionFile := filepath.Join(root, ".bts", "config", ".template-version")
	existing, _ := os.ReadFile(versionFile)

	current := version.GetTemplateVersion()

	if strings.TrimSpace(string(existing)) == current {
		return false // same version, skip
	}

	// Deploy templates (overwrite all except user config files)
	_, _ = template.DeployForce(root, []string{
		".bts/config/settings.yaml",
		".mcp.json",
	})

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
		content = strings.ReplaceAll(content, "forge-handle-", "bts-handle-")
		content = strings.ReplaceAll(content, ".forge/status_line.sh", ".bts/status_line.sh")
		_ = os.WriteFile(settingsPath, []byte(content), 0644)
	}
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
func detectSource(input *HookInput, ws *state.WorkState) string {
	// Use explicit source if Claude Code provides it
	if input.Source != "" {
		return input.Source
	}

	// Infer from work state freshness
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

