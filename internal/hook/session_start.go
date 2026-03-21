package hook

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jlim/bts/internal/state"
	"github.com/jlim/bts/internal/template"
	"github.com/jlim/bts/pkg/version"
)

type sessionStartHandler struct{}

func NewSessionStartHandler() Handler {
	return &sessionStartHandler{}
}

func (h *sessionStartHandler) EventType() EventType {
	return EventSessionStart
}

func (h *sessionStartHandler) Handle(input *HookInput) (*HookOutput, error) {
	btsRoot, err := state.FindBTSRoot(input.CWD)
	if err != nil {
		return &HookOutput{}, nil
	}

	// Auto-update templates if binary version changed
	autoUpdateTemplates(btsRoot)

	// Try to load work state for rich context recovery
	ws, _ := state.LoadWorkState(btsRoot)
	source := detectSource(input, ws)

	recipe, err := state.GetActiveRecipe(btsRoot)
	if err != nil || recipe == nil {
		// Check for finalized recipes ready for implementation
		recipe, err = state.GetFinalizedRecipe(btsRoot)
		if err != nil || recipe == nil {
			return &HookOutput{}, nil
		}

		msg := fmt.Sprintf(
			"[bts] Recipe ready for implementation: %s \"%s\" (ID: %s)\nRun /bts-implement %s to start coding.",
			recipe.Type, recipe.Topic, recipe.ID, recipe.ID,
		)

		// Enrich with work state if resuming
		if ws != nil && (source == "compact" || source == "resume") {
			msg = fmt.Sprintf("[bts] Resuming. %s\nRun /bts-implement %s to start coding.", ws.Summary, recipe.ID)
		}

		return &HookOutput{
			HookSpecificOutput: &HookSpecificOutput{
				AdditionalContext: msg,
			},
		}, nil
	}

	// Build hint based on phase and source
	var hint string
	if recipe.Phase == "scoping" {
		hint = fmt.Sprintf("Scope alignment in progress. Read .bts/state/recipes/%s/scope.md and confirm or adjust.", recipe.ID)
	} else if state.IsImplementPhase(recipe.Phase) {
		// Fix recipes use /bts-recipe-fix, not /bts-implement
		implCmd := fmt.Sprintf("/bts-implement %s", recipe.ID)
		if recipe.Type == "fix" {
			implCmd = fmt.Sprintf("/bts-recipe-fix %s", recipe.ID)
		}
		switch source {
		case "resume":
			hint = "Session restored. Continue where you left off."
		case "compact":
			hint = fmt.Sprintf("Context compacted. Run %s to continue.", implCmd)
		default:
			hint = fmt.Sprintf("Run %s to continue, or /recipe cancel to abort.", implCmd)
		}
	} else {
		// Source-aware hints for spec phases (research, draft, verify, debate, etc.)
		switch source {
		case "resume":
			hint = "Session restored. Continue where you left off."
		case "compact":
			hint = "Context compacted. Continue where you left off."
		default:
			// startup or clear — no context, need to re-invoke skill
			hint = fmt.Sprintf("Run /bts-recipe-%s to re-enter the recipe, or /recipe cancel to abort.", recipe.Type)
		}
	}

	// Build message
	var msg string
	switch source {
	case "resume":
		msg = fmt.Sprintf("[bts] Session restored. %s \"%s\" (Step: %s)\n%s",
			recipe.Type, recipe.Topic, recipe.Phase, hint)
		if ws != nil {
			msg = fmt.Sprintf("[bts] Session restored. %s\n%s", ws.Summary, hint)
		}
	case "compact":
		msg = fmt.Sprintf("[bts] Context compacted. %s \"%s\" (Step: %s)\n%s",
			recipe.Type, recipe.Topic, recipe.Phase, hint)
		if ws != nil {
			msg = fmt.Sprintf("[bts] Context compacted. %s\n%s", ws.Summary, hint)
		}
	default:
		msg = fmt.Sprintf(
			"[bts] Active recipe: %s \"%s\" (Step: %s, Iteration: %d)\n%s",
			recipe.Type, recipe.Topic, recipe.Phase, recipe.Iteration, hint,
		)
	}

	return &HookOutput{
		HookSpecificOutput: &HookSpecificOutput{
			AdditionalContext: msg,
		},
	}, nil
}

// autoUpdateTemplates checks if templates need updating and deploys if so.
func autoUpdateTemplates(btsRoot string) {
	versionFile := filepath.Join(btsRoot, ".bts", "config", ".template-version")
	existing, _ := os.ReadFile(versionFile)

	current := templateVersionString()

	if strings.TrimSpace(string(existing)) == current {
		return // same version, skip
	}

	// Deploy templates (overwrite all except user config files)
	_, _ = template.DeployForce(btsRoot, []string{
		".bts/config/settings.yaml",
		".mcp.json",
	})

	// Record new version
	_ = os.WriteFile(versionFile, []byte(current), 0644)
}

// templateVersionString builds a version identifier for template tracking.
func templateVersionString() string {
	v := version.GetVersion()
	c := version.Commit
	if c != "none" && len(c) >= 7 {
		return v + "-" + c[:7]
	}
	return v
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

