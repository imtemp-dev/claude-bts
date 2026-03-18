package cli

import (
	"fmt"
	"os"

	"github.com/jlim/bts/pkg/version"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:     "bts",
	Short:   "bts — Bulletproof Technical Specification",
	Long:    "Verify and refine implementation specs until AI can generate code with high accuracy.",
	Version: version.GetVersion(),
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("bts — Bulletproof Technical Specification")
		fmt.Printf("Version: %s\n\n", version.GetFullVersion())
		_ = cmd.Help()
	},
}

func init() {
	rootCmd.SetVersionTemplate(fmt.Sprintf("bts %s\n", version.GetFullVersion()))

	rootCmd.AddGroup(
		&cobra.Group{ID: "project", Title: "Project Commands:"},
		&cobra.Group{ID: "recipe", Title: "Recipe Commands:"},
		&cobra.Group{ID: "tools", Title: "Tools:"},
	)
}

// Execute is the main entry point for the bts CLI.
func Execute() error {
	return rootCmd.Execute()
}

// ExitOnError runs Execute and exits with code 1 on error.
func ExitOnError() {
	if err := Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
