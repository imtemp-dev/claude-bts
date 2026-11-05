package cli

import (
	"fmt"
	"os"

	"github.com/imtemp-dev/claude-bts/internal/state"
	"github.com/imtemp-dev/claude-bts/internal/template"
	"github.com/imtemp-dev/claude-bts/pkg/version"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(updateCmd)
}

// updateCmd is template.Update, the routine the SessionStart auto-update
// also runs, with its result printed.  Its cleanup runs even when the
// templates are already current, so a project an earlier auto-update left
// with retired files is cleaned here.
var updateCmd = &cobra.Command{
	Use:     "update",
	Short:   "Update templates to match current binary version",
	GroupID: "project",
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, _ := os.Getwd()
		root, err := state.FindRoot(cwd)
		if err != nil {
			return fmt.Errorf("not a bts project. Run 'bts init' first")
		}

		current := version.GetTemplateVersion()
		res, err := template.Update(root, current)
		for _, w := range res.Warnings {
			fmt.Fprintf(os.Stderr, "warning: %s\n", w)
		}
		if err != nil {
			return err
		}

		switch {
		case !res.Deployed:
			fmt.Printf("Templates already up to date (%s)\n", current)
		case res.OldVersion == "":
			fmt.Printf("Templates initialized: %s\n", current)
		default:
			fmt.Printf("Templates updated: %s → %s\n", res.OldVersion, current)
		}
		if res.Deployed {
			fmt.Printf("Files updated: %d\n", len(res.Files))
		}
		for _, n := range res.Notes {
			fmt.Println(n)
		}
		return nil
	},
}
