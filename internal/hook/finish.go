package hook

import (
	"fmt"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

// FinishResult is what `bts recipe finish` did: the marker it judged, and
// the recipe's phase before and after, or the gate's block reason.
type FinishResult struct {
	Marker   string
	From, To string
	Blocked  string
}

// FinishMarker is the completion marker a recipe would print now: FIX DONE
// for a fix recipe, IMPLEMENT DONE in an implement phase, DONE in the spec
// loop.
func FinishMarker(r *state.RecipeState) string {
	switch {
	case r.Type == "fix":
		return "<bts>FIX DONE</bts>"
	case state.IsImplementPhase(r.Phase):
		return "<bts>IMPLEMENT DONE</bts>"
	}
	return "<bts>DONE</bts>"
}

// Finish runs, now, the completion gate the Stop hook runs when a turn ends
// with the recipe's marker: the same checks, the same phase change via the
// gate, the same ownership rule.  It is for a session that must go on
// after the recipe completes, as a Multica run does, which ends at its
// stop (trial problem #155).  The stop-block budget does not apply: that
// bounds a hook repeating one block, and a command reports and returns.
func Finish(root, recipeID, sessionID string) (*FinishResult, error) {
	r, err := state.GetActiveRecipe(root)
	if err != nil {
		return nil, err
	}
	if r == nil || r.ID != recipeID {
		active := "none"
		if r != nil {
			active = r.ID
		}
		return nil, fmt.Errorf("%s is not the active recipe (active: %s); finish completes the recipe this worktree is working", recipeID, active)
	}
	res := &FinishResult{Marker: FinishMarker(r), From: r.Phase}
	out, err := (&stopHandler{}).decide(root, &HookInput{
		SessionID: sessionID, CWD: root, HookEventName: "Stop", LastAssistantMessage: res.Marker,
	})
	if err != nil {
		return nil, err
	}
	if out != nil && out.Decision == "block" {
		res.Blocked = out.Reason
	}
	if after, err := state.LoadRecipeState(root, recipeID); err == nil {
		res.To = after.Phase
	}
	return res, nil
}
