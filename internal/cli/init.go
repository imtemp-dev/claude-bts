package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jlim/bts/internal/template"
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
		filepath.Join(btsDir, "state", "recipes"),
		filepath.Join(btsDir, "state", "debates"),
	}
	for _, dir := range stateDirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
	}

	// Deploy templates
	created, err := template.Deploy(absRoot)
	if err != nil {
		return fmt.Errorf("deploy templates: %w", err)
	}

	// Merge statusline config into .claude/settings.local.json
	if err := mergeStatusLineSettings(absRoot); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not configure statusline: %v\n", err)
	}

	fmt.Printf("\nbts initialized successfully.\n")
	fmt.Printf("  Files created: %d\n", len(created))
	fmt.Printf("  Skills:        .claude/skills/bts/\n")
	fmt.Printf("  Agents:        .claude/agents/bts/\n")
	fmt.Printf("  Commands:      .claude/commands/bts/\n")
	fmt.Printf("  Rules:         .claude/rules/bts/\n")
	fmt.Printf("  Hooks:         .claude/hooks/bts/\n")
	fmt.Printf("  State:         .bts/\n")
	fmt.Printf("\nStart Claude Code and try: /recipe blueprint \"your feature\"\n")

	return nil
}

// mergeStatusLineSettings adds statusLine config to .claude/settings.local.json.
func mergeStatusLineSettings(projectRoot string) error {
	settingsPath := filepath.Join(projectRoot, ".claude", "settings.local.json")

	// Read existing settings or start empty
	var settings map[string]interface{}
	data, err := os.ReadFile(settingsPath)
	if err == nil {
		if err := json.Unmarshal(data, &settings); err != nil {
			settings = make(map[string]interface{})
		}
	} else {
		settings = make(map[string]interface{})
	}

	// Only add if not already configured
	if _, exists := settings["statusLine"]; exists {
		return nil
	}

	settings["statusLine"] = map[string]interface{}{
		"type":    "command",
		"command": ".bts/status_line.sh",
	}

	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(settingsPath), 0755); err != nil {
		return err
	}

	return os.WriteFile(settingsPath, out, 0644)
}
