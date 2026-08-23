package hook

import (
	"fmt"
	"os"

	"github.com/imtemp-dev/jig/internal/metrics"
	"github.com/imtemp-dev/jig/internal/state"
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

	marker := &state.CompactMarker{SessionID: input.SessionID}

	// Save recipe state if active recipe exists
	recipe, _ := state.GetActiveRecipe(root)
	if recipe != nil {
		marker.RecipeID = recipe.ID
		marker.Phase = recipe.Phase
		if err := state.SaveRecipeState(root, recipe); err != nil {
			fmt.Fprintf(os.Stderr, "warning: save recipe state: %v\n", err)
		}
	}

	// Build and save work state snapshot (covers active + finalized recipes)
	ws, _ := state.BuildWorkState(root)
	if ws != nil {
		if err := state.SaveWorkState(root, ws); err != nil {
			fmt.Fprintf(os.Stderr, "warning: save work state: %v\n", err)
		}
	} else {
		// No recipe active — save non-recipe session snapshot if possible
		if ss, _ := state.BuildSessionState(root); ss != nil {
			if err := state.SaveSessionState(root, ss); err != nil {
				fmt.Fprintf(os.Stderr, "warning: save session state: %v\n", err)
			}
		}
	}

	// Always write the marker so SessionStart can detect compaction deterministically.
	if err := state.WriteCompactMarker(root, marker); err != nil {
		fmt.Fprintf(os.Stderr, "warning: write compact marker: %v\n", err)
	}

	recipeID, phase := "", ""
	if recipe != nil {
		recipeID = recipe.ID
		phase = recipe.Phase
	}
	_ = metrics.Append(root, &metrics.MetricsEvent{
		Kind:      metrics.KindCompact,
		SessionID: input.SessionID,
		RecipeID:  recipeID,
		Phase:     phase,
	})

	return &HookOutput{}, nil
}
