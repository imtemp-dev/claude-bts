package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/imtemp-dev/claude-bts/internal/engine"
	"github.com/imtemp-dev/claude-bts/internal/state"
	"github.com/spf13/cobra"
)

// `bts falsifier table` renders final.md § 2 from the falsifier records, so
// the table is never transcribed by hand (trial problem #97).

func init() {
	falsifierCmd.AddCommand(falsifierTableCmd)
	falsifierTableCmd.Flags().Bool("write", false, "Replace the table under final.md's `## 2.` heading with the rendered one")
}

var falsifierTableCmd = &cobra.Command{
	Use:          "table <recipe-id>",
	Short:        "Render final.md § 2's falsifier table from build-evidence.json (--write puts it in final.md)",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		root, err := projectRoot()
		if err != nil {
			return err
		}
		write, _ := cmd.Flags().GetBool("write")
		table, err := executeFalsifierTable(root, args[0], write)
		if err != nil {
			return err
		}
		if write {
			fmt.Printf("[bts] final.md § 2 table written from build-evidence.json\n")
			return nil
		}
		fmt.Print(table)
		return nil
	},
}

func executeFalsifierTable(root, recipeID string, write bool) (string, error) {
	ev, err := state.LoadBuildEvidence(root, recipeID)
	if err != nil {
		return "", err
	}
	dir := state.RecipeDir(root, recipeID)
	table, err := engine.RenderFalsifierTable(ev, filepath.Join(dir, "domain.md"))
	if err != nil {
		return "", err
	}
	if !write {
		return table, nil
	}
	finalPath := filepath.Join(dir, "final.md")
	data, err := os.ReadFile(finalPath)
	if err != nil {
		return "", fmt.Errorf("read final.md: %w", err)
	}
	updated, ok := engine.ReplaceSectionTable(string(data), "## 2.", table)
	if !ok {
		return "", fmt.Errorf("final.md has no `## 2.` heading; write § 2's heading (Invariants and their falsifiers) first")
	}
	if err := os.WriteFile(finalPath, []byte(updated), 0644); err != nil {
		return "", err
	}
	return table, nil
}
