package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/imtemp-dev/claude-bts/internal/state"
	"github.com/imtemp-dev/claude-bts/internal/template"
	"github.com/imtemp-dev/claude-bts/pkg/version"
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
		root, err := state.FindRoot(cwd)
		if err != nil {
			return fmt.Errorf("not a bts project. Run 'bts init' first")
		}

		current := version.GetTemplateVersion()
		versionFile := filepath.Join(root, ".bts", "config", ".template-version")
		existing, _ := os.ReadFile(versionFile)
		oldVer := strings.TrimSpace(string(existing))

		if oldVer == current {
			fmt.Printf("Templates already up to date (%s)\n", current)
			return nil
		}

		// DeployForce (same skip list as auto-update and init --force).
		// .gitignore is user-owned — merged via EnsureGitignore, never overwritten.
		skipFiles := []string{".bts/config/settings.yaml", ".mcp.json", ".gitignore"}
		updated, err := template.DeployForce(root, skipFiles)
		if err != nil {
			return fmt.Errorf("update templates: %w", err)
		}

		// Ensure .gitignore ignores bts local data without destroying existing rules.
		if err := template.EnsureGitignore(root); err != nil {
			fmt.Fprintf(os.Stderr, "warning: update .gitignore: %v\n", err)
		}

		// Write new version
		if err := os.WriteFile(versionFile, []byte(current), 0644); err != nil {
			fmt.Fprintf(os.Stderr, "warning: save template version: %v\n", err)
		}

		// Clean up legacy forge-* template files and settings
		cleanupLegacyForge(root)
		migrateHookSettings(root)

		// Merge statusline and hook settings (same as init)
		if err := template.MergeHookSettings(root); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not update hook settings: %v\n", err)
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

// migrateHookSettings replaces forge-handle-* with bts-handle-* in settings.local.json.
func migrateHookSettings(root string) {
	path := filepath.Join(root, ".claude", "settings.local.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	content := string(data)
	if !strings.Contains(content, "forge-handle-") {
		return
	}
	content = strings.ReplaceAll(content, "forge-handle-", "bts-handle-")
	content = strings.ReplaceAll(content, ".forge/status_line.sh", ".bts/status_line.sh")
	_ = os.WriteFile(path, []byte(content), 0644)
	fmt.Println("Migrated hook settings: forge → bts")
}

// cleanupLegacyForge removes old forge-* template files left from pre-rename versions.
func cleanupLegacyForge(root string) {
	claudeDir := filepath.Join(root, ".claude")
	dirs := []string{"skills", "agents", "rules", "hooks", "commands"}
	removed := 0

	for _, d := range dirs {
		base := filepath.Join(claudeDir, d)
		entries, err := os.ReadDir(base)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), "forge-") {
				path := filepath.Join(base, entry.Name())
				if err := os.RemoveAll(path); err == nil {
					removed++
				}
			}
		}
	}

	// Remove legacy .forge/ status_line.sh if .bts/ version exists
	oldStatus := filepath.Join(root, ".forge", "status_line.sh")
	if _, err := os.Stat(oldStatus); err == nil {
		_ = os.Remove(oldStatus)
		removed++
	}

	if removed > 0 {
		fmt.Printf("Cleaned up %d legacy forge files\n", removed)
	}
}
