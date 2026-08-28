package cli

import (
	"fmt"
	"os"

	"github.com/imtemp-dev/jig/internal/state"
	"github.com/imtemp-dev/jig/pkg/version"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:     "jig",
	Short:   "jig — Verify before you code",
	Long:    "Structured AI development for Claude Code — catches spec errors before they become debugging sessions.",
	Version: version.GetVersion(),
	// Bare `jig` reports the active recipe. Status is by far the most-typed
	// command, and inside a project the banner is never what you wanted.
	// Outside one — or when status cannot run — fall back to the banner,
	// which is what a first-time user needs.
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, _ := os.Getwd()
		if _, err := state.FindRoot(cwd); err == nil {
			if err := recipeStatusCmd.RunE(cmd, args); err == nil {
				return nil
			}
		}
		fmt.Println("jig — Verify before you code")
		fmt.Printf("Version: %s\n\n", version.GetFullVersion())
		return cmd.Help()
	},
}

func init() {
	rootCmd.SetVersionTemplate(fmt.Sprintf("jig %s\n", version.GetFullVersion()))

	rootCmd.AddGroup(
		&cobra.Group{ID: "project", Title: "Project Commands:"},
		&cobra.Group{ID: "recipe", Title: "Recipe Commands:"},
		&cobra.Group{ID: "tools", Title: "Tools:"},
	)
}

// Execute is the main entry point for the jig CLI.
func Execute() error {
	registerShortcuts()
	return rootCmd.Execute()
}

// ExitOnError runs Execute and exits with code 1 on error.
func ExitOnError() {
	if err := Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
