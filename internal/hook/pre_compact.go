package hook

import (
	"fmt"
	"os"

	"github.com/imtemp-dev/claude-forge/internal/metrics"
	"github.com/imtemp-dev/claude-forge/internal/state"
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
	if err := state.SaveRecipeState(root, recipe); err != nil {
		fmt.Fprintf(os.Stderr, "warning: save recipe state: %v\n", err)
	}

	// Build and save work state snapshot
	ws, err := state.BuildWorkState(root)
	if err != nil || ws == nil {
		return &HookOutput{
			HookSpecificOutput: &HookSpecificOutput{
				AdditionalContext: "[forge] Recipe state saved before compaction.",
			},
		}, nil
	}
	if err := state.SaveWorkState(root, ws); err != nil {
		fmt.Fprintf(os.Stderr, "warning: save work state: %v\n", err)
	}

	_ = metrics.Append(root, &metrics.MetricsEvent{
		Kind:      metrics.KindCompact,
		SessionID: input.SessionID,
		RecipeID:  recipe.ID,
		Phase:     recipe.Phase,
	})

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
