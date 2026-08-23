package template

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// MergeHookSettings ensures all jig hooks and statusline are registered
// in .claude/settings.local.json. Uses absolute paths to avoid CWD
// resolution issues. Fixes stale entries (wrong paths, underscore naming)
// and adds missing hooks.
func MergeHookSettings(projectRoot string) error {
	absRoot, err := filepath.Abs(projectRoot)
	if err != nil {
		absRoot = projectRoot
	}

	settingsPath := filepath.Join(absRoot, ".claude", "settings.local.json")

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

	// StatusLine — use absolute path
	slPath := filepath.Join(absRoot, ".jig", "status_line.sh")
	sl, _ := settings["statusLine"].(map[string]interface{})
	if sl == nil || !isAbsolutePath(sl["command"]) {
		settings["statusLine"] = map[string]interface{}{
			"type":    "command",
			"command": slPath,
		}
		changed = true
	}

	// Hooks — all use absolute paths
	hooks, _ := settings["hooks"].(map[string]interface{})
	if hooks == nil {
		hooks = make(map[string]interface{})
	}

	type hookDef struct {
		event   string
		script  string
		timeout int
	}

	defs := []hookDef{
		{"SessionStart", filepath.Join(absRoot, ".claude", "hooks", "jig-handle-session-start.sh"), 10},
		{"PreCompact", filepath.Join(absRoot, ".claude", "hooks", "jig-handle-pre-compact.sh"), 5},
		{"Stop", filepath.Join(absRoot, ".claude", "hooks", "jig-handle-stop.sh"), 10},
		{"SessionEnd", filepath.Join(absRoot, ".claude", "hooks", "jig-handle-session-end.sh"), 5},
		{"SubagentStart", filepath.Join(absRoot, ".claude", "hooks", "jig-handle-subagent-start.sh"), 5},
		{"SubagentStop", filepath.Join(absRoot, ".claude", "hooks", "jig-handle-subagent-stop.sh"), 5},
		{"PreToolUse", filepath.Join(absRoot, ".claude", "hooks", "jig-handle-pre-tool-use.sh"), 5},
		{"PostToolUse", filepath.Join(absRoot, ".claude", "hooks", "jig-handle-post-tool-use.sh"), 5},
	}

	makeEntry := func(script string, timeout int) []interface{} {
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

	for _, def := range defs {
		existing, exists := hooks[def.event]
		if !exists {
			// Missing → add
			hooks[def.event] = makeEntry(def.script, def.timeout)
			changed = true
			continue
		}

		// Check if existing entry has a stale jig/forge hook path
		if fixStaleJigHook(existing, def.script, def.timeout) {
			changed = true
		}
	}

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

// fixStaleJigHook finds a jig/forge hook entry within a hook event array
// and fixes its command path if stale. Returns true if any fix was applied.
func fixStaleJigHook(eventValue interface{}, correctScript string, correctTimeout int) bool {
	groups, ok := eventValue.([]interface{})
	if !ok {
		return false
	}

	for _, group := range groups {
		gm, ok := group.(map[string]interface{})
		if !ok {
			continue
		}
		hookList, ok := gm["hooks"].([]interface{})
		if !ok {
			continue
		}
		for _, h := range hookList {
			hm, ok := h.(map[string]interface{})
			if !ok {
				continue
			}
			cmd, _ := hm["command"].(string)
			if !isJigHookCommand(cmd) {
				continue
			}
			// Found a jig/forge hook — fix if stale
			if cmd != correctScript {
				hm["command"] = correctScript
				hm["timeout"] = correctTimeout
				return true
			}
			return false // already correct
		}
	}
	return false
}

// isJigHookCommand returns true if the command path looks like a jig or forge hook.
func isJigHookCommand(cmd string) bool {
	return strings.Contains(cmd, "jig-handle-") ||
		strings.Contains(cmd, "jig_handle_") ||
		strings.Contains(cmd, "forge-handle-") ||
		strings.Contains(cmd, "forge_handle_")
}

// isAbsolutePath checks if a settings value is a string starting with /.
func isAbsolutePath(v interface{}) bool {
	s, ok := v.(string)
	return ok && strings.HasPrefix(s, "/")
}
