package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/imtemp-dev/claude-bts/internal/engine"
	"github.com/imtemp-dev/claude-bts/internal/state"
)

// bts asks the user about the recipe's mode at the two points where the
// size of the change shows (feature #132).  It holds a decision rather
// than switching: the mode decides how much process the user pays for,
// and part of what decides it (a new external integration, a migration,
// a new boundary) only a reader can see.  Each question is asked once;
// the user's answer stands.

// Decision keys bts holds about the mode.
const (
	expressModeKey = "express-mode"
	fullModeKey    = "full-mode"
)

// decisionExists reports whether a decision with key was ever held for the
// recipe, whatever became of it.
func decisionExists(root, recipeID, key string) (bool, error) {
	ds, err := state.LoadDecisions(root, recipeID)
	if err != nil {
		return false, err
	}
	for _, d := range ds {
		if d.Key == key {
			return true, nil
		}
	}
	return false, nil
}

// suggestExpress holds decision express-mode when a full build recipe,
// before its architect step, has a confirmed scope of at most
// ExpressMaxInScope In Scope items.  It returns the question it held, or "".
func suggestExpress(root, recipeID string) (string, error) {
	r, err := state.LoadRecipeState(root, recipeID)
	if err != nil || r.Type != "build" || r.Mode != "" || !state.ExpressFitsPhase(r.Phase) {
		return "", nil
	}
	data, err := os.ReadFile(filepath.Join(state.RecipeDir(root, recipeID), "scope.md"))
	if err != nil {
		return "", nil
	}
	items := engine.ParseScopeItems(string(data))
	if len(items) == 0 || len(items) > state.ExpressMaxInScope {
		return "", nil
	}
	if asked, err := decisionExists(root, recipeID, expressModeKey); err != nil || asked {
		return "", err
	}
	q := fmt.Sprintf("The confirmed scope has %d In Scope item(s). If the change adds no new external integration and no "+
		"migration or irreversible step, it fits an express build recipe: no architect runners or debate, no review round "+
		"on final.md, one interrogate adversary (bts-recipe-build § Express mode). Run %s as express, or keep the full recipe?",
		len(items), recipeID)
	if _, err := state.HoldDecision(root, recipeID, &state.DecisionEvent{
		Key: expressModeKey, Doc: "scope.md", Question: q, Options: []string{"express", "full"},
	}); err != nil {
		return "", err
	}
	return q, nil
}

// suggestFull holds decision full-mode when an express recipe's domain.md
// introduces more than ExpressMaxInvariants invariants.  It returns the
// question it held, or "".
func suggestFull(root string, r *state.RecipeState) (string, error) {
	if !r.IsExpress() {
		return "", nil
	}
	introduced, _, err := engine.DomainInvariantKinds(filepath.Join(state.RecipeDir(root, r.ID), "domain.md"))
	if err != nil || len(introduced) <= state.ExpressMaxInvariants {
		return "", nil
	}
	if asked, err := decisionExists(root, r.ID, fullModeKey); err != nil || asked {
		return "", err
	}
	q := fmt.Sprintf("domain.md introduces %d invariants, more than the %d an express recipe is for. Move %s to the full "+
		"recipe (the architect's runners and debate if it has not passed that step, the review round on final.md, two "+
		"interrogate adversaries), or keep it express?", len(introduced), state.ExpressMaxInvariants, r.ID)
	if _, err := state.HoldDecision(root, r.ID, &state.DecisionEvent{
		Key: fullModeKey, Doc: "domain.md", Question: q, Options: []string{"full", "express"},
	}); err != nil {
		return "", err
	}
	return q, nil
}

// printModeQuestion tells the orchestrator what bts held and how the
// user's answer is carried out.
func printModeQuestion(recipeID, key, question string) {
	fmt.Printf("[bts] held decision %s: %s\n", key, question)
	switch key {
	case expressModeKey:
		fmt.Printf("      Ask the user. On express: `bts recipe mode %s express --reason \"<their words>\"`. "+
			"Either way, record the answer with `bts recipe decision resolve %s %s --answer \"<their words>\"`.\n",
			recipeID, recipeID, key)
	case fullModeKey:
		fmt.Printf("      Ask the user. On full: `bts recipe mode %s full --reason \"<their words>\"`. "+
			"Either way, record the answer with `bts recipe decision resolve %s %s --answer \"<their words>\"`.\n",
			recipeID, recipeID, key)
	}
}
