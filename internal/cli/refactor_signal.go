package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/imtemp-dev/claude-bts/internal/engine"
	"github.com/imtemp-dev/claude-bts/internal/state"
	"github.com/spf13/cobra"
)

func init() {
	refactorSignalCmd.Flags().Bool("json", false, "Emit signals as JSON")
	rootCmd.AddCommand(refactorSignalCmd)
}

var refactorSignalCmd = &cobra.Command{
	Use:   "refactor-signal [recipe-id]",
	Short: "Surface patch-of-patches patterns in a recipe's history",
	Long: `Analyzes changelog and tasks.json for patterns that typically signal
the current decomposition is wrong:

  - test_fix_cascade: one test failure fanned out into 3+ module fixes
  - cross_module_churn: the same module is edited 4+ times, or 3+ modules
    each see 3+ edits

Signals are diagnostic, not blocking. Each carries a suggested next
step — typically to revisit domain.md invariant ownership or run
/bts-architect to consider an alternative decomposition.`,
	Args:    cobra.MaximumNArgs(1),
	GroupID: "tools",
	RunE:    runRefactorSignal,
}

func runRefactorSignal(cmd *cobra.Command, args []string) error {
	cwd, _ := os.Getwd()
	root, err := state.FindRoot(cwd)
	if err != nil {
		return fmt.Errorf("not a bts project: %w", err)
	}

	recipeID := ""
	if len(args) > 0 {
		recipeID = args[0]
	}
	if recipeID == "" {
		active, _ := state.GetActiveRecipe(root)
		if active == nil {
			return fmt.Errorf("no recipe id given and no active recipe found")
		}
		recipeID = active.ID
	}

	signals, err := engine.DetectRefactorSignals(state.RecipeDir(root, recipeID))
	if err != nil {
		return err
	}

	asJSON, _ := cmd.Flags().GetBool("json")
	if asJSON {
		data, err := json.MarshalIndent(signals, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	}

	if len(signals) == 0 {
		fmt.Printf("No refactor signals for %s.\n", recipeID)
		return nil
	}
	fmt.Printf("Refactor signals for %s:\n\n", recipeID)
	for i, s := range signals {
		fmt.Printf("%d. %s\n", i+1, s.Kind)
		for _, e := range s.Evidence {
			fmt.Printf("   - %s\n", e)
		}
		fmt.Printf("   Suggest: %s\n\n", s.Suggest)
	}
	return nil
}
