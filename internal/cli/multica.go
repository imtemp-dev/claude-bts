package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/imtemp-dev/claude-bts/internal/feature"
	"github.com/imtemp-dev/claude-bts/internal/multica"
	"github.com/imtemp-dev/claude-bts/internal/state"
)

// Every bts command and hook runs in this binary, so the phase observers
// registered here see every recipe phase change.  The feature store goes
// first: it is local and cheap, while the Multica notifier may wait on the
// network, and the Stop hook has 10 s in all.
func init() {
	state.AddPhaseObserver(feature.OnPhaseChange)
	state.AddPhaseObserver(multica.OnPhaseChange)
}

// createFeatureRecipe takes an item of a feature in this worktree: it claims
// the next attempt in the store, creates the recipe bound to it, and gives
// the attempt back if the create fails.
func createFeatureRecipe(root, featureID, itemID, typeFlag, topic string) (string, error) {
	c, err := feature.BeginClaim(feature.ClaimRequest{FeatureID: featureID, ItemID: itemID, Root: root, Type: typeFlag})
	if err != nil {
		return "", err
	}
	ok := false
	defer func() {
		if !ok {
			c.Abort()
		}
	}()
	if err := c.RestoreAmended(root); err != nil {
		return "", err
	}
	if topic == "" {
		topic = c.Item.Title
	}
	issue := ""
	if tc := multica.FindTaskContext(root); tc != nil {
		issue = tc.IssueID
	}
	id, err := executeRecipeCreateWith(root, c.RecipeType, topic, c.Amends, "", c.Express, func(id string, r *state.RecipeState) error {
		if err := c.Write(root, id, issue); err != nil {
			return fmt.Errorf("record the claim: %w", err)
		}
		c.Bind(r)
		return nil
	})
	if err != nil {
		return "", err
	}
	ok = true
	fmt.Fprintf(os.Stderr, "[bts] %s is attempt a%d (%s) of item %s of %s; read its context first: bts feature context %s %s\n",
		id, c.Attempt, c.Kind, c.Item.ID, c.Feature.ID, c.Feature.ID, c.Item.ID)
	if s := strings.TrimSpace(c.Feature.Setup); s != "" {
		fmt.Fprintf(os.Stderr, "[bts] the feature's setup makes a fresh checkout runnable; run it here first: %s\n", s)
	}
	return id, nil
}
