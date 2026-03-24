package cli

import (
	"fmt"
	"os"

	"github.com/jlim/claude-forge/internal/state"
	"github.com/jlim/claude-forge/internal/statusline"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(statuslineCmd)
}

var statuslineCmd = &cobra.Command{
	Use:    "statusline",
	Short:  "Render statusline for Claude Code",
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, _ := os.Getwd()
		root, _ := state.FindRoot(cwd)
		result := statusline.Render(os.Stdin, root)
		fmt.Print(result)
		return nil
	},
}
