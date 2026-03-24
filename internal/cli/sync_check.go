package cli

import (
	"fmt"
	"os"

	"github.com/imtemp-dev/claude-forge/internal/state"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(syncCheckCmd)
}

var syncCheckCmd = &cobra.Command{
	Use:     "sync-check [recipe-id]",
	Short:   "Verify all documents are in sync within a recipe",
	Args:    cobra.MaximumNArgs(1),
	GroupID: "tools",
	RunE:    runSyncCheck,
}

func runSyncCheck(cmd *cobra.Command, args []string) error {
	cwd, _ := os.Getwd()
	root, err := state.FindRoot(cwd)
	if err != nil {
		return fmt.Errorf("not a forge project: %w", err)
	}

	// Find recipe ID
	var recipeID string
	if len(args) > 0 {
		recipeID = args[0]
	} else {
		recipe, err := state.GetActiveRecipe(root)
		if err != nil || recipe == nil {
			return fmt.Errorf("no active recipe. Specify recipe ID: forge sync-check <id>")
		}
		recipeID = recipe.ID
	}

	manifest, err := state.LoadManifest(root, recipeID)
	if err != nil {
		return fmt.Errorf("load manifest: %w", err)
	}

	issues := 0

	// 1. Unverified drafts
	unverified := manifest.GetUnverifiedDrafts()
	if len(unverified) > 0 {
		fmt.Println("UNVERIFIED drafts:")
		for _, path := range unverified {
			fmt.Printf("  - %s\n", path)
		}
		issues += len(unverified)
	}

	// 2. Unincorporated debate conclusions
	debates := manifest.GetUnincorporatedDebates()
	if len(debates) > 0 {
		fmt.Println("OUT OF SYNC — debate conclusions not in current draft:")
		for _, path := range debates {
			fmt.Printf("  - %s\n", path)
		}
		issues += len(debates)
	}

	// 3. Unresolved simulation gaps
	gaps := manifest.GetUnresolvedGaps()
	if len(gaps) > 0 {
		fmt.Println("UNRESOLVED simulation gaps:")
		for _, path := range gaps {
			fmt.Printf("  - %s\n", path)
		}
		issues += len(gaps)
	}

	if issues == 0 {
		fmt.Println("All documents in sync.")
	} else {
		fmt.Printf("\n%d sync issue(s) found.\n", issues)
		os.Exit(1)
	}

	return nil
}
