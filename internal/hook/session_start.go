package hook

import (
	"fmt"

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
		// No .bts/ — not a bts project, silently pass
		return &HookOutput{}, nil
	}

	recipe, err := state.GetActiveRecipe(btsRoot)
	if err != nil || recipe == nil {
		// Check for finalized recipes ready for implementation
		recipe, err = state.GetFinalizedRecipe(btsRoot)
		if err != nil || recipe == nil {
			return &HookOutput{}, nil
		}
		msg := fmt.Sprintf(
			"[bts] Recipe ready for implementation: %s \"%s\" (ID: %s)\n"+
				"Run /implement %s to start coding.",
			recipe.Type, recipe.Topic, recipe.ID, recipe.ID,
		)
		return &HookOutput{
			HookSpecificOutput: &HookSpecificOutput{
				AdditionalContext: msg,
			},
		}, nil
	}

	var hint string
	if recipe.Phase == "scoping" {
		hint = fmt.Sprintf("Scope alignment in progress. Read .bts/state/recipes/%s/scope.md and confirm or adjust.", recipe.ID)
	} else if state.IsImplementPhase(recipe.Phase) {
		hint = fmt.Sprintf("Run /implement %s to continue, or /recipe cancel to abort.", recipe.ID)
	} else {
		hint = "Run /recipe resume to continue, or /recipe cancel to abort."
	}

	msg := fmt.Sprintf(
		"[bts] Active recipe: %s \"%s\" (Step: %s, Iteration: %d)\n%s",
		recipe.Type, recipe.Topic, recipe.Phase, recipe.Iteration, hint,
	)

	return &HookOutput{
		HookSpecificOutput: &HookSpecificOutput{
			AdditionalContext: msg,
		},
	}, nil
}
