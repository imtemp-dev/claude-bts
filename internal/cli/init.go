package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/imtemp-dev/jig/internal/template"
	"github.com/imtemp-dev/jig/pkg/version"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(initCmd)
	initCmd.Flags().Bool("force", false, "Reinitialize (overwrites existing jig files)")
}

var initCmd = &cobra.Command{
	Use:     "init [directory]",
	Short:   "Initialize jig in a project",
	Long:    "Deploy skills, agents, hooks, and rules to .claude/ and create .jig/ for state management.",
	Args:    cobra.MaximumNArgs(1),
	GroupID: "project",
	RunE:    runInit,
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
	jigDir := filepath.Join(absRoot, ".jig")
	force, _ := cmd.Flags().GetBool("force")
	if _, err := os.Stat(jigDir); err == nil && !force {
		return fmt.Errorf(".jig/ already exists. Use --force to reinitialize")
	}

	fmt.Println("Initializing jig...")

	// Create .jig directories
	stateDirs := []string{
		filepath.Join(jigDir, "config"),
		filepath.Join(jigDir, "specs", "recipes"),
		filepath.Join(jigDir, "specs", "debates"),
		filepath.Join(jigDir, "local"),
	}
	for _, dir := range stateDirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
	}

	// Deploy templates. .gitignore is user-owned — never deploy/overwrite it;
	// EnsureGitignore below merges the jig rule in without clobbering user rules.
	skipFiles := []string{".jig/config/settings.yaml", ".mcp.json", ".gitignore"}
	var created []string
	if force {
		created, err = template.DeployForce(absRoot, skipFiles)
	} else {
		created, err = template.Deploy(absRoot)
	}
	if err != nil {
		return fmt.Errorf("deploy templates: %w", err)
	}

	// Ensure .gitignore ignores jig local data without destroying existing rules.
	if err := template.EnsureGitignore(absRoot); err != nil {
		fmt.Fprintf(os.Stderr, "warning: update .gitignore: %v\n", err)
	}

	// Record template version
	tv := version.GetVersion()
	if version.Commit != "none" && len(version.Commit) >= 7 {
		tv += "-" + version.Commit[:7]
	}
	if err := os.WriteFile(filepath.Join(absRoot, ".jig", "config", ".template-version"), []byte(tv), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "warning: save template version: %v\n", err)
	}

	// Merge statusline and hook configs into .claude/settings.local.json
	if err := template.MergeHookSettings(absRoot); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not configure hooks: %v\n", err)
	}

	fmt.Printf("\njig initialized successfully.\n")
	fmt.Printf("  Files created: %d\n", len(created))
	fmt.Printf("  Skills:        .claude/skills/jig-*/\n")
	fmt.Printf("  Agents:        .claude/agents/jig-*/\n")
	fmt.Printf("  Commands:      .claude/commands/jig-*/\n")
	fmt.Printf("  Rules:         .claude/rules/jig-*/\n")
	fmt.Printf("  Hooks:         .claude/hooks/jig-*/\n")
	fmt.Printf("  State:         .jig/\n")
	fmt.Printf("  VS Code:       .vscode/markdown.code-snippets (type jigc/jigcb/jigcq + Tab in any .md)\n")
	fmt.Printf("\nStart Claude Code and try: /jig-spec \"your feature\"\n")

	return nil
}
