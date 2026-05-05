package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/imtemp-dev/claude-bts/internal/template"
	"github.com/imtemp-dev/claude-bts/pkg/version"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(initCmd)
	initCmd.Flags().Bool("force", false, "Reinitialize (overwrites existing bts files)")
}

var initCmd = &cobra.Command{
	Use:   "init [directory]",
	Short: "Initialize bts in a project",
	Long:  "Deploy skills, agents, hooks, and rules to .claude/ and create .bts/ for state management.",
	Args:  cobra.MaximumNArgs(1),
	GroupID: "project",
	RunE:  runInit,
}

func runInit(cmd *cobra.Command, args []string) error {
	// Determine project root
	projectRoot := "."
	if len(args) > 0 {
		projectRoot = args[0]
	}

	absRoot, err := filepath.Abs(projectRoot)
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}

	// Check if already initialized
	btsDir := filepath.Join(absRoot, ".bts")
	force, _ := cmd.Flags().GetBool("force")
	if _, err := os.Stat(btsDir); err == nil && !force {
		return fmt.Errorf(".bts/ already exists. Use --force to reinitialize")
	}

	fmt.Println("Initializing bts...")

	// Create .bts directories
	stateDirs := []string{
		filepath.Join(btsDir, "config"),
		filepath.Join(btsDir, "specs", "recipes"),
		filepath.Join(btsDir, "specs", "debates"),
		filepath.Join(btsDir, "local"),
	}
	for _, dir := range stateDirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
	}

	// Deploy templates
	skipFiles := []string{".bts/config/settings.yaml", ".mcp.json"}
	var created []string
	if force {
		created, err = template.DeployForce(absRoot, skipFiles)
	} else {
		created, err = template.Deploy(absRoot)
	}
	if err != nil {
		return fmt.Errorf("deploy templates: %w", err)
	}

	// Record template version
	tv := version.GetVersion()
	if version.Commit != "none" && len(version.Commit) >= 7 {
		tv += "-" + version.Commit[:7]
	}
	if err := os.WriteFile(filepath.Join(absRoot, ".bts", "config", ".template-version"), []byte(tv), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "warning: save template version: %v\n", err)
	}

	// Merge statusline and hook configs into .claude/settings.local.json
	if err := template.MergeHookSettings(absRoot); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not configure hooks: %v\n", err)
	}

	fmt.Printf("\nbts initialized successfully.\n")
	fmt.Printf("  Files created: %d\n", len(created))
	fmt.Printf("  Skills:        .claude/skills/bts-*/\n")
	fmt.Printf("  Agents:        .claude/agents/bts-*/\n")
	fmt.Printf("  Commands:      .claude/commands/bts-*/\n")
	fmt.Printf("  Rules:         .claude/rules/bts-*/\n")
	fmt.Printf("  Hooks:         .claude/hooks/bts-*/\n")
	fmt.Printf("  State:         .bts/\n")
	fmt.Printf("  VS Code:       .vscode/markdown.code-snippets (type btsc/btscb/btscq + Tab in any .md)\n")
	fmt.Printf("\nStart Claude Code and try: /bts-recipe-blueprint \"your feature\"\n")

	return nil
}
