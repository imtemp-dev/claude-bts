package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/imtemp-dev/claude-jig/internal/engine"
	"github.com/imtemp-dev/claude-jig/internal/state"
	"github.com/spf13/cobra"
)

func init() {
	simDeviationsCmd.Flags().String("recipe", "", "Recipe ID (defaults to active recipe)")
	simDeviationsCmd.Flags().Bool("json", false, "Emit as JSON (otherwise tab-separated)")
	rootCmd.AddCommand(simDeviationsCmd)
}

var simDeviationsCmd = &cobra.Command{
	Use:   "sim-deviations",
	Short: "List DEVIATION entries parsed from simulations/*.md (Phase 12)",
	Long: `Walks the recipe's simulations/ directory and emits every DEVIATION
entry the simulate step produced. Used by /jig-sync Step 2.5 to
ingest simulate findings into deviation.md without rediscovering
them via file-by-file comparison.

Output format (default):
  <id>	<driver>	<severity>	<file>	<detail>

With --json, a JSON array is written.`,
	GroupID: "tools",
	RunE:    runSimDeviations,
}

func runSimDeviations(cmd *cobra.Command, args []string) error {
	cwd, _ := os.Getwd()
	root, err := state.FindRoot(cwd)
	if err != nil {
		return fmt.Errorf("not a jig project: %w", err)
	}

	recipeID, _ := cmd.Flags().GetString("recipe")
	if recipeID == "" && len(args) > 0 {
		recipeID = args[0]
	}
	if recipeID == "" {
		active, _ := state.GetActiveRecipe(root)
		if active == nil {
			return fmt.Errorf("no --recipe provided and no active recipe")
		}
		recipeID = active.ID
	}

	sims, err := engine.ExtractSimulationDeviations(state.RecipeDir(root, recipeID))
	if err != nil {
		return err
	}

	asJSON, _ := cmd.Flags().GetBool("json")
	if asJSON {
		data, err := json.MarshalIndent(sims, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	}

	for _, s := range sims {
		fmt.Printf("%s\t%s\t%s\t%s\t%s\n", s.ID, s.Driver, s.Severity, s.File, s.Detail)
	}
	return nil
}
