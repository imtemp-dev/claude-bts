package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/imtemp-dev/claude-jig/internal/state"
	"github.com/imtemp-dev/claude-jig/pkg/version"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(versionCmd)
}

var versionCmd = &cobra.Command{
	Use:     "version",
	Short:   "Show binary and template versions",
	GroupID: "tools",
	RunE: func(cmd *cobra.Command, args []string) error {
		binVer := version.GetTemplateVersion()
		date := version.Date
		if len(date) >= 10 {
			date = date[:10]
		}
		fmt.Printf("Binary:    %s (%s)\n", binVer, date)

		cwd, _ := os.Getwd()
		root, err := state.FindRoot(cwd)
		if err != nil {
			fmt.Println("Templates: not initialized (run 'jig init')")
			return nil
		}

		versionFile := filepath.Join(root, ".jig", "config", ".template-version")
		existing, err := os.ReadFile(versionFile)
		if err != nil {
			fmt.Println("Templates: not initialized (run 'jig init')")
			return nil
		}

		tmplVer := strings.TrimSpace(string(existing))
		fmt.Printf("Templates: %s\n", tmplVer)

		if tmplVer == binVer {
			fmt.Println("Status:    up to date")
		} else {
			fmt.Println("Status:    outdated (run 'jig update' to refresh)")
		}
		return nil
	},
}
