package cli

import (
	"fmt"

	"github.com/imtemp-dev/claude-bts/internal/state"
	"github.com/spf13/cobra"
)

// bts recipe claim <id>: make the Claude Code session that runs it the
// recipe's owner. The CLI never sees a session id; the PostToolUse hook
// does, and binds the session whose Bash call ran this command
// (internal/hook/ownership.go). Run outside Claude Code, it changes nothing.
var recipeClaimCmd = &cobra.Command{
	Use:   "claim <recipe-id>",
	Short: "Make the Claude Code session that runs this the recipe's owner (bound by the bts hook)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		root, err := projectRoot()
		if err != nil {
			return err
		}
		recipe, err := checkClaimable(root, args[0])
		if err != nil {
			return err
		}
		prev := recipe.OwnerSession
		if prev == "" {
			prev = "none"
		}
		fmt.Printf("Claiming %s (owner session now: %s). The bts PostToolUse hook binds the Claude Code session that ran this command; "+
			"outside Claude Code nothing changes. Check with `bts recipe status`.\n", recipe.ID, prev)
		return nil
	},
}

// checkClaimable returns the recipe when it exists and is still open.
func checkClaimable(root, id string) (*state.RecipeState, error) {
	recipe, err := state.LoadRecipeState(root, id)
	if err != nil {
		return nil, fmt.Errorf("no recipe %s: %w", id, err)
	}
	switch recipe.Phase {
	case "complete", "cancelled":
		return nil, fmt.Errorf("recipe %s is %s; there is nothing to claim", id, recipe.Phase)
	}
	return recipe, nil
}
