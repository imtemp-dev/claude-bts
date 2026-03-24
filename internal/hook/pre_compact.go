package hook

import (
	"fmt"

	"github.com/jlim/claude-forge/internal/state"
)

type preCompactHandler struct{}

func NewPreCompactHandler() Handler {
	return &preCompactHandler{}
}

func (h *preCompactHandler) EventType() EventType {
	return EventPreCompact
}

func (h *preCompactHandler) Handle(input *HookInput) (*HookOutput, error) {
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

	// Build and save work state snapshot
	ws, err := state.BuildWorkState(root)
	if err != nil || ws == nil {
		return &HookOutput{
			HookSpecificOutput: &HookSpecificOutput{
				AdditionalContext: "[forge] Recipe state saved before compaction.",
			},
		}, nil
	}
	_ = state.SaveWorkState(root, ws)

	// Include next-step hint for post-compaction context
	msg := fmt.Sprintf("[forge] Context snapshot saved. %s", ws.Summary)
	nextStep := nextStepHint(root, recipe)
	if nextStep != "" {
		msg += fmt.Sprintf("\nNEXT: %s", nextStep)
	}

	return &HookOutput{
		HookSpecificOutput: &HookSpecificOutput{
			AdditionalContext: msg,
		},
	}, nil
}
