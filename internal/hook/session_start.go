package hook

import (
	"fmt"
	"os"
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
			"[bts] Recipe ready for implementation: %s \"%s\" (ID: %s)\nRun /implement %s to start coding.",
			recipe.Type, recipe.Topic, recipe.ID, recipe.ID,
		)

		// Enrich with work state if resuming
		if ws != nil && (source == "compact" || source == "resume") {
			msg = fmt.Sprintf("[bts] Resuming. %s\nRun /implement %s to start coding.", ws.Summary, recipe.ID)
		}

		return &HookOutput{
			HookSpecificOutput: &HookSpecificOutput{
				AdditionalContext: msg,
			},
		}, nil
	}

	// Build hint based on phase
	var hint string
	if recipe.Phase == "scoping" {
		hint = fmt.Sprintf("Scope alignment in progress. Read .bts/state/recipes/%s/scope.md and confirm or adjust.", recipe.ID)
	} else if state.IsImplementPhase(recipe.Phase) {
		hint = fmt.Sprintf("Run /implement %s to continue, or /recipe cancel to abort.", recipe.ID)
	} else {
		hint = "Run /recipe resume to continue, or /recipe cancel to abort."
	}

	// Build message based on session source
	var msg string
	if ws != nil && (source == "compact" || source == "resume") {
		prefix := "[bts] Resuming after compaction. "
		if source == "resume" {
			prefix = "[bts] Resuming from previous session. "
		}
		msg = prefix + ws.Summary + "\n" + hint
	} else {
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

// init registers the Source field — ensure types.go has it
func init() {
	// Verify os import is used (for potential future stat calls)
	_ = os.IsNotExist
}
