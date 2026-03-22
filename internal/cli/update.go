package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jlim/bts/internal/state"
	"github.com/jlim/bts/internal/template"
	"github.com/jlim/bts/pkg/version"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(updateCmd)
}

var updateCmd = &cobra.Command{
	Use:     "update",
	Short:   "Update templates to match current binary version",
	GroupID: "project",
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, _ := os.Getwd()
		btsRoot, err := state.FindBTSRoot(cwd)
		if err != nil {
			return fmt.Errorf("not a bts project. Run 'bts init' first")
		}

		current := version.GetTemplateVersion()
		versionFile := filepath.Join(btsRoot, ".bts", "config", ".template-version")
		existing, _ := os.ReadFile(versionFile)
		oldVer := strings.TrimSpace(string(existing))

		if oldVer == current {
			fmt.Printf("Templates already up to date (%s)\n", current)
			return nil
		}

		// DeployForce (same skip list as auto-update and init --force)
		skipFiles := []string{".bts/config/settings.yaml", ".mcp.json"}
		updated, err := template.DeployForce(btsRoot, skipFiles)
		if err != nil {
			return fmt.Errorf("update templates: %w", err)
		}

		// Write new version
		_ = os.WriteFile(versionFile, []byte(current), 0644)

		// Merge statusline settings (same as init)
		if err := mergeStatusLineSettings(btsRoot); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not update statusline settings: %v\n", err)
		}

		if oldVer == "" {
			fmt.Printf("Templates initialized: %s\n", current)
		} else {
			fmt.Printf("Templates updated: %s → %s\n", oldVer, current)
		}
		fmt.Printf("Files updated: %d\n", len(updated))
		return nil
	},
}
