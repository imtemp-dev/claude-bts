package hook

import (
	"fmt"
	"os"

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
	if err := state.SaveRecipeState(root, recipe); err != nil {
		fmt.Fprintf(os.Stderr, "warning: save recipe state: %v\n", err)
	}

	// Build and save work state for cross-session resume
	ws, err := state.BuildWorkState(root)
	if err == nil && ws != nil {
		if err := state.SaveWorkState(root, ws); err != nil {
			fmt.Fprintf(os.Stderr, "warning: save work state: %v\n", err)
		}
	}

	return &HookOutput{}, nil
}
