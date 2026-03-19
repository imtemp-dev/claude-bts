package cli

import (
	"fmt"
	"os"

	"github.com/jlim/bts/internal/state"
	"github.com/jlim/bts/internal/statusline"
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
		btsRoot, _ := state.FindBTSRoot(cwd)
		result := statusline.Render(os.Stdin, btsRoot)
		fmt.Print(result)
		return nil
	},
}
