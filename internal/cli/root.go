package cli

import (
	"fmt"
	"os"

	"github.com/imtemp-dev/claude-forge/pkg/version"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:     "forge",
	Short:   "forge — Bulletproof Technical Specification",
	Long:    "Verify and refine implementation specs until AI can generate code with high accuracy.",
	Version: version.GetVersion(),
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("forge — Bulletproof Technical Specification")
		fmt.Printf("Version: %s\n\n", version.GetFullVersion())
		_ = cmd.Help()
	},
}

func init() {
	rootCmd.SetVersionTemplate(fmt.Sprintf("forge %s\n", version.GetFullVersion()))

	rootCmd.AddGroup(
		&cobra.Group{ID: "project", Title: "Project Commands:"},
		&cobra.Group{ID: "recipe", Title: "Recipe Commands:"},
		&cobra.Group{ID: "tools", Title: "Tools:"},
	)
}

// Execute is the main entry point for the forge CLI.
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
