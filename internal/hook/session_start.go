package hook

import (
	"fmt"
	"time"

	"github.com/jlim/bts/internal/state"
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
		switch source {
		case "resume":
			hint = "Session restored. Continue where you left off."
		case "compact":
			hint = fmt.Sprintf("Context compacted. Run /bts-implement %s to continue.", recipe.ID)
		default:
			hint = fmt.Sprintf("Run /bts-implement %s to continue, or /recipe cancel to abort.", recipe.ID)
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

