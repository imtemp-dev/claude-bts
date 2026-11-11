package cli

import (
	"fmt"
	"strings"

	"github.com/imtemp-dev/claude-bts/internal/state"
	"github.com/spf13/cobra"
)

func init() {
	recipeCmd.AddCommand(recipeModeCmd)
	recipeModeCmd.Flags().String("reason", "", "Why the recipe moves to the full recipe (for example: nine invariants, or a new boundary); recorded in its changelog")
}

// bts recipe mode <id> full: move an express build recipe to the full
// recipe (feature #132).  Express trims the fixed process a small change
// does not need; a recipe that has outgrown it moves to the full recipe,
// and the move is one way, so the trims cannot come back once a recipe is
// known to be large.  The changelog records the move and its reason.
var recipeModeCmd = &cobra.Command{
	Use:   "mode <recipe-id> full",
	Short: "Move an express build recipe to the full recipe (one way, recorded)",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		root, err := projectRoot()
		if err != nil {
			return err
		}
		reason, _ := cmd.Flags().GetString("reason")
		if err := executeRecipeMode(root, args[0], args[1], reason); err != nil {
			return err
		}
		fmt.Printf("%s now runs the full build recipe. The full recipe's steps and gates apply from the step it is on: "+
			"the architect's runners and debate if it has not passed the architect step, the review round on final.md before DONE, "+
			"and two interrogate adversaries.\n", args[0])
		return nil
	},
}

// executeRecipeMode moves recipe id to mode.  The only move is to full:
// a recipe is made express when it is created.
func executeRecipeMode(root, id, mode, reason string) error {
	switch mode {
	case state.ModeFull:
	case state.ModeExpress:
		return fmt.Errorf("a recipe is made express when it is created (bts recipe create --type build --express); the move to full is one way")
	default:
		return fmt.Errorf("unknown mode %q: a recipe moves only to full", mode)
	}
	r, err := state.LoadRecipeState(root, id)
	if err != nil {
		return fmt.Errorf("no recipe %s: %w", id, err)
	}
	switch {
	case r.Phase == "complete" || r.Phase == "cancelled":
		return fmt.Errorf("recipe %s is %s; there is nothing left to move", id, r.Phase)
	case !r.IsExpress():
		return fmt.Errorf("recipe %s already runs the full recipe", id)
	}
	r.Mode = state.ModeFull
	if err := state.SaveRecipeState(root, r); err != nil {
		return fmt.Errorf("save: %w", err)
	}
	result := "express → full"
	if reason = strings.TrimSpace(reason); reason != "" {
		result += ": " + state.TruncateRunes(reason, 300)
	}
	return state.AppendChangelog(root, id, &state.ChangelogEntry{Action: "mode", Result: result})
}
