package hook

import (
	"fmt"

	"github.com/jlim/bts/internal/state"
)

type preCompactHandler struct{}

func NewPreCompactHandler() Handler {
	return &preCompactHandler{}
}

func (h *preCompactHandler) EventType() EventType {
	return EventPreCompact
}

func (h *preCompactHandler) Handle(input *HookInput) (*HookOutput, error) {
	btsRoot, err := state.FindBTSRoot(input.CWD)
	if err != nil {
		return &HookOutput{}, nil
	}

	// Save recipe state
	recipe, err := state.GetActiveRecipe(btsRoot)
	if err != nil || recipe == nil {
		return &HookOutput{}, nil
	}
	_ = state.SaveRecipeState(btsRoot, recipe)

	// Build and save work state snapshot
	ws, err := state.BuildWorkState(btsRoot)
	if err != nil || ws == nil {
		return &HookOutput{
			HookSpecificOutput: &HookSpecificOutput{
				AdditionalContext: "[bts] Recipe state saved before compaction.",
			},
		}, nil
	}
	_ = state.SaveWorkState(btsRoot, ws)

	// Include next-step hint for post-compaction context
	msg := fmt.Sprintf("[bts] Context snapshot saved. %s", ws.Summary)
	nextStep := nextStepHint(btsRoot, recipe)
	if nextStep != "" {
		msg += fmt.Sprintf("\nNEXT: %s", nextStep)
	}

	return &HookOutput{
		HookSpecificOutput: &HookSpecificOutput{
			AdditionalContext: msg,
		},
	}, nil
}
