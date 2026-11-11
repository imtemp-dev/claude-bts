package cli

import (
	"fmt"
	"strings"

	"github.com/imtemp-dev/claude-bts/internal/state"
	"github.com/spf13/cobra"
)

func init() {
	recipeCmd.AddCommand(recipeModeCmd)
	recipeModeCmd.Flags().String("reason", "", "Why the recipe changes mode (for example: the user chose express at scoping, or nine invariants); recorded in its changelog")
}

// bts recipe mode <id> express|full (feature #132).  Express trims the
// fixed process a small change does not need.  A build recipe becomes
// express when it is created with --express, or later with the user's
// yes, but only before its architect step, so nothing express trims has
// run.  An express recipe that has outgrown it moves to the full recipe,
// and that move is one way: the trims cannot come back once a recipe is
// known to be large.  The changelog records each move and its reason.
var recipeModeCmd = &cobra.Command{
	Use:          "mode <recipe-id> express|full",
	Short:        "Make a build recipe express before its architect step, or move an express recipe to the full recipe (one way)",
	Args:         cobra.ExactArgs(2),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		root, err := projectRoot()
		if err != nil {
			return err
		}
		reason, _ := cmd.Flags().GetString("reason")
		if err := executeRecipeMode(root, args[0], args[1], reason); err != nil {
			return err
		}
		if args[1] == state.ModeExpress {
			fmt.Printf("%s now runs the express build recipe: follow bts-recipe-build § Express mode from the step it is on.\n", args[0])
			return nil
		}
		fmt.Printf("%s now runs the full build recipe. The full recipe's steps and gates apply from the step it is on: "+
			"the architect's runners and debate if it has not passed the architect step, the review round on final.md before DONE, "+
			"and two interrogate adversaries.\n", args[0])
		return nil
	},
}

// executeRecipeMode moves recipe id to mode, express or full.
func executeRecipeMode(root, id, mode, reason string) error {
	if mode != state.ModeExpress && mode != state.ModeFull {
		return fmt.Errorf("unknown mode %q: express or full", mode)
	}
	r, err := state.LoadRecipeState(root, id)
	if err != nil {
		return fmt.Errorf("no recipe %s: %w", id, err)
	}
	if r.Phase == "complete" || r.Phase == "cancelled" {
		return fmt.Errorf("recipe %s is %s; there is nothing left to move", id, r.Phase)
	}
	from := "full"
	switch mode {
	case state.ModeExpress:
		switch {
		case r.IsExpress():
			return fmt.Errorf("recipe %s is already express", id)
		case r.Mode == state.ModeFull:
			return fmt.Errorf("recipe %s was moved to the full recipe, and that move is one way", id)
		case r.Type != "build":
			return fmt.Errorf("express is a mode of the build recipe; %s is a %s recipe", id, r.Type)
		case !state.ExpressFitsPhase(r.Phase):
			return fmt.Errorf("recipe %s is in phase %s, past its architect step: express would trim steps that have run, so it stays on the full recipe", id, r.Phase)
		}
	case state.ModeFull:
		if !r.IsExpress() {
			return fmt.Errorf("recipe %s already runs the full recipe", id)
		}
		from = "express"
	}
	r.Mode = mode
	if err := state.SaveRecipeState(root, r); err != nil {
		return fmt.Errorf("save: %w", err)
	}
	result := from + " → " + mode
	if reason = strings.TrimSpace(reason); reason != "" {
		result += ": " + state.TruncateRunes(reason, 300)
	}
	return state.AppendChangelog(root, id, &state.ChangelogEntry{Action: "mode", Result: result})
}
