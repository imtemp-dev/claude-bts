package hook

import (
	"github.com/jlim/claude-forge/internal/state"
)

type sessionEndHandler struct{}

func NewSessionEndHandler() Handler {
	return &sessionEndHandler{}
}

func (h *sessionEndHandler) EventType() EventType {
	return EventSessionEnd
}

func (h *sessionEndHandler) Handle(input *HookInput) (*HookOutput, error) {
	root, err := state.FindRoot(input.CWD)
	if err != nil {
		return &HookOutput{}, nil
	}

	// Save recipe state
	recipe, err := state.GetActiveRecipe(root)
	if err != nil || recipe == nil {
		return &HookOutput{}, nil
	}
	_ = state.SaveRecipeState(root, recipe)

	// Build and save work state for cross-session resume
	ws, err := state.BuildWorkState(root)
	if err == nil && ws != nil {
		_ = state.SaveWorkState(root, ws)
	}

	return &HookOutput{}, nil
}
