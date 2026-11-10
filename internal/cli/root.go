package cli

import (
	"fmt"
	"os"

	"github.com/imtemp-dev/claude-bts/pkg/version"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:     "bts",
	Short:   "bts — Verify before you code",
	Long:    "Structured AI development for Claude Code — catches spec errors before they become debugging sessions.",
	Version: version.GetVersion(),
	// ExitOnError prints a command's error.  Cobra printed it too, so every
	// failure reached the agent twice, guidance included (trial problem
	// #120).
	SilenceErrors: true,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("bts — Verify before you code")
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
