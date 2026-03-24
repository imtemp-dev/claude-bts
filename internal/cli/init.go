package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/imtemp-dev/claude-forge/internal/template"
	"github.com/imtemp-dev/claude-forge/pkg/version"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(initCmd)
	initCmd.Flags().Bool("force", false, "Reinitialize (overwrites existing forge files)")
}

var initCmd = &cobra.Command{
	Use:   "init [directory]",
	Short: "Initialize forge in a project",
	Long:  "Deploy skills, agents, hooks, and rules to .claude/ and create .forge/ for state management.",
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
	forgeDir := filepath.Join(absRoot, ".forge")
	force, _ := cmd.Flags().GetBool("force")
	if _, err := os.Stat(forgeDir); err == nil && !force {
		return fmt.Errorf(".forge/ already exists. Use --force to reinitialize")
	}

	fmt.Println("Initializing forge...")

	// Create .forge directories
	stateDirs := []string{
		filepath.Join(forgeDir, "config"),
		filepath.Join(forgeDir, "state", "recipes"),
		filepath.Join(forgeDir, "state", "debates"),
	}
	for _, dir := range stateDirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
	}

	// Deploy templates
	skipFiles := []string{".forge/config/settings.yaml", ".mcp.json"}
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
	if err := os.WriteFile(filepath.Join(absRoot, ".forge", "config", ".template-version"), []byte(tv), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "warning: save template version: %v\n", err)
	}

	// Merge statusline config into .claude/settings.local.json
	if err := mergeStatusLineSettings(absRoot); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not configure statusline: %v\n", err)
	}

	fmt.Printf("\nforge initialized successfully.\n")
	fmt.Printf("  Files created: %d\n", len(created))
	fmt.Printf("  Skills:        .claude/skills/forge/\n")
	fmt.Printf("  Agents:        .claude/agents/forge/\n")
	fmt.Printf("  Commands:      .claude/commands/forge/\n")
	fmt.Printf("  Rules:         .claude/rules/forge/\n")
	fmt.Printf("  Hooks:         .claude/hooks/forge/\n")
	fmt.Printf("  State:         .forge/\n")
	fmt.Printf("\nStart Claude Code and try: /recipe blueprint \"your feature\"\n")

	return nil
}

// mergeStatusLineSettings adds statusLine and hook configs to .claude/settings.local.json.
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

	changed := false

	// StatusLine
	if _, exists := settings["statusLine"]; !exists {
		settings["statusLine"] = map[string]interface{}{
			"type":    "command",
			"command": ".forge/status_line.sh",
		}
		changed = true
	}

	// Hooks for SubagentStart/Stop
	hooks, _ := settings["hooks"].(map[string]interface{})
	if hooks == nil {
		hooks = make(map[string]interface{})
	}

	hookEntry := func(script string, timeout int) []interface{} {
		return []interface{}{
			map[string]interface{}{
				"hooks": []interface{}{
					map[string]interface{}{
						"type":    "command",
						"command": script,
						"timeout": timeout,
					},
				},
			},
		}
	}

	register := func(event, script string, timeout int) {
		if _, exists := hooks[event]; !exists {
			hooks[event] = hookEntry(script, timeout)
			changed = true
		}
	}

	register("SessionStart", ".claude/hooks/forge-handle-session-start.sh", 10)
	register("PreCompact", ".claude/hooks/forge-handle-pre-compact.sh", 5)
	register("Stop", ".claude/hooks/forge-handle-stop.sh", 10)
	register("SessionEnd", ".claude/hooks/forge-handle-session-end.sh", 5)
	register("SubagentStart", ".claude/hooks/forge-handle-subagent-start.sh", 5)
	register("SubagentStop", ".claude/hooks/forge-handle-subagent-stop.sh", 5)
	register("PreToolUse", ".claude/hooks/forge-handle-pre-tool-use.sh", 5)
	register("PostToolUse", ".claude/hooks/forge-handle-post-tool-use.sh", 5)

	if len(hooks) > 0 {
		settings["hooks"] = hooks
	}

	if !changed {
		return nil
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
