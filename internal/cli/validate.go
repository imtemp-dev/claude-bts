package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/imtemp-dev/claude-forge/internal/engine"
	"github.com/imtemp-dev/claude-forge/internal/state"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(validateCmd)
}

var validateCmd = &cobra.Command{
	Use:     "validate [recipe-id]",
	Short:   "Validate JSON file schemas in a recipe",
	Args:    cobra.MaximumNArgs(1),
	GroupID: "tools",
	RunE:    runValidate,
}

func runValidate(cmd *cobra.Command, args []string) error {
	cwd, _ := os.Getwd()
	root, err := state.FindRoot(cwd)
	if err != nil {
		return fmt.Errorf("not a forge project: %w", err)
	}

	var recipeDir string
	if len(args) > 0 {
		recipeDir = filepath.Join(state.StatePath(root), "recipes", args[0])
	} else {
		// Find active recipe
		recipe, err := state.GetActiveRecipe(root)
		if err != nil || recipe == nil {
			// Try to find any recipe directory
			recipesDir := filepath.Join(state.StatePath(root), "recipes")
			entries, err := os.ReadDir(recipesDir)
			if err != nil || len(entries) == 0 {
				return fmt.Errorf("no recipes found. Specify recipe ID: forge validate <id>")
			}
			// Use first directory found
			for _, entry := range entries {
				if entry.IsDir() {
					recipeDir = filepath.Join(recipesDir, entry.Name())
					break
				}
			}
			if recipeDir == "" {
				return fmt.Errorf("no recipe directory found")
			}
		} else {
			recipeDir = state.RecipeDir(root, recipe.ID)
		}
	}

	errors, err := engine.ValidateRecipeDir(recipeDir)
	if err != nil {
		return fmt.Errorf("validate: %w", err)
	}

	if len(errors) == 0 {
		fmt.Println("All files valid.")
		return nil
	}

	fmt.Printf("%d validation error(s):\n\n", len(errors))
	for _, e := range errors {
		fmt.Printf("  %s\n", e.String())
	}

	os.Exit(1)
	return nil
}
