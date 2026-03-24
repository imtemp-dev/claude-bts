package hook

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jlim/claude-forge/internal/state"
	"github.com/jlim/claude-forge/internal/template"
	"github.com/jlim/claude-forge/pkg/version"
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
					msg = fmt.Sprintf("[forge] Roadmap: %d/%d done. Next: %s\nRun /forge-recipe-blueprint to start.", done, total, nextItem)
				} else {
					msg = fmt.Sprintf("[forge] Roadmap complete: %d/%d done. Run /forge-recipe-blueprint to add new items or start a new vision.", done, total)
				}
				if updated {
					msg = fmt.Sprintf("[forge] Templates updated to %s\n%s", version.GetTemplateVersion(), msg)
				}
				return &HookOutput{
					HookSpecificOutput: &HookSpecificOutput{
						AdditionalContext: msg,
					},
				}, nil
			}
			// Check for incomplete vision (DRAFT without a recipe yet)
			if state.VisionExists(root) {
				visionData, _ := os.ReadFile(filepath.Join(state.StatePath(root), "vision.md"))
				if strings.Contains(string(visionData), "Status: DRAFT") {
					msg := "[forge] Vision document in progress (Status: DRAFT).\nRun /forge-recipe-blueprint to continue."
					if updated {
						msg = fmt.Sprintf("[forge] Templates updated to %s\n%s", version.GetTemplateVersion(), msg)
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
						AdditionalContext: fmt.Sprintf("[forge] Templates updated to %s", version.GetTemplateVersion()),
					},
				}, nil
			}
			return &HookOutput{}, nil
		}

		msg := fmt.Sprintf(
			"[forge] Recipe ready for implementation: %s \"%s\" (ID: %s)\nRun /forge-implement %s to start coding.",
			recipe.Type, recipe.Topic, recipe.ID, recipe.ID,
		)

		// Enrich with work state if resuming
		if ws != nil && (source == "compact" || source == "resume") {
			msg = fmt.Sprintf("[forge] Resuming. %s\nRun /forge-implement %s to start coding.", ws.Summary, recipe.ID)
		}

		if updated {
			msg = fmt.Sprintf("[forge] Templates updated to %s\n%s", version.GetTemplateVersion(), msg)
		}

		return &HookOutput{
			HookSpecificOutput: &HookSpecificOutput{
				AdditionalContext: msg,
			},
		}, nil
	}

	// Build hint based on phase and source
	var hint string
	if recipe.Phase == "discovery" {
		hint = fmt.Sprintf("Intent discovery in progress. Read .forge/state/recipes/%s/intent.md and continue conversation.", recipe.ID)
	} else if recipe.Phase == "scoping" {
		hint = fmt.Sprintf("Scope alignment in progress. Read .forge/state/recipes/%s/scope.md and confirm or adjust.", recipe.ID)
	} else if state.IsImplementPhase(recipe.Phase) {
		implCmd := fmt.Sprintf("/forge-implement %s", recipe.ID)
		if recipe.Type == "fix" {
			implCmd = fmt.Sprintf("/forge-recipe-fix %s", recipe.ID)
		}
		switch source {
		case "resume":
			hint = "Session restored. Continue where you left off."
		case "compact":
			if next := nextStepHint(root, recipe); next != "" {
				hint = fmt.Sprintf("Context compacted. NEXT: %s", next)
			} else {
				hint = fmt.Sprintf("Context compacted. Run %s to continue.", implCmd)
			}
		default:
			if next := nextStepHint(root, recipe); next != "" {
				hint = fmt.Sprintf("NEXT: %s", next)
			} else {
				hint = fmt.Sprintf("Run %s to continue, or /recipe cancel to abort.", implCmd)
			}
		}
	} else {
		switch source {
		case "resume":
			hint = "Session restored. Continue where you left off."
		case "compact":
			if next := nextStepHint(root, recipe); next != "" {
				hint = fmt.Sprintf("Context compacted. NEXT: %s", next)
			} else {
				hint = "Context compacted. Continue where you left off."
			}
		default:
			if next := nextStepHint(root, recipe); next != "" {
				hint = fmt.Sprintf("NEXT: %s", next)
			} else {
				hint = fmt.Sprintf("Run /forge-recipe-%s to re-enter the recipe, or /recipe cancel to abort.", recipe.Type)
			}
		}
	}

	// Build message
	var msg string
	switch source {
	case "resume":
		msg = fmt.Sprintf("[forge] Session restored. %s \"%s\" (Step: %s)\n%s",
			recipe.Type, recipe.Topic, recipe.Phase, hint)
		if ws != nil {
			msg = fmt.Sprintf("[forge] Session restored. %s\n%s", ws.Summary, hint)
		}
	case "compact":
		msg = fmt.Sprintf("[forge] Context compacted. %s \"%s\" (Step: %s)\n%s",
			recipe.Type, recipe.Topic, recipe.Phase, hint)
		if ws != nil {
			msg = fmt.Sprintf("[forge] Context compacted. %s\n%s", ws.Summary, hint)
		}
	default:
		msg = fmt.Sprintf(
			"[forge] Active recipe: %s \"%s\" (Step: %s, Iteration: %d)\n%s",
			recipe.Type, recipe.Topic, recipe.Phase, recipe.Iteration, hint,
		)
	}

	if updated {
		msg = fmt.Sprintf("[forge] Templates updated to %s\n%s", version.GetTemplateVersion(), msg)
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
	versionFile := filepath.Join(root, ".forge", "config", ".template-version")
	existing, _ := os.ReadFile(versionFile)

	current := version.GetTemplateVersion()

	if strings.TrimSpace(string(existing)) == current {
		return false // same version, skip
	}

	// Deploy templates (overwrite all except user config files)
	_, _ = template.DeployForce(root, []string{
		".forge/config/settings.yaml",
		".mcp.json",
	})

	// Record new version
	if err := os.WriteFile(versionFile, []byte(current), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "warning: save template version: %v\n", err)
	}
	return true
}

// nextStepHint returns a specific next-action hint based on recipe phase and state files.
// For implementation phases, always guides back to the orchestrator (/forge-implement)
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
	case !state.IsImplementPhase(recipe.Phase) && recipe.Phase != "finalize":
		return "Run /forge-assess on draft.md to determine next action."
	}

	// Implementation phases — always guide to orchestrator
	implCmd := fmt.Sprintf("/forge-implement %s", recipe.ID)
	if recipe.Type == "fix" {
		implCmd = fmt.Sprintf("/forge-recipe-fix %s", recipe.ID)
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
		if exists("test-results.json") {
			simsDir := filepath.Join(recipeDir, "simulations")
			if entries, err := os.ReadDir(simsDir); err == nil && len(entries) > 0 {
				detail = "proceed to code review"
			} else {
				detail = "run code simulation"
			}
		} else {
			detail = "run tests"
		}
	case "review":
		detail = "run code review"
	case "sync":
		detail = "sync spec with implementation"
	case "status":
		detail = "update project status"
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

