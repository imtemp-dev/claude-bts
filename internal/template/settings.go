package template

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/imtemp-dev/claude-bts/internal/engine"
)

// statuslineEnabled reports whether the project turned bts's statusLine on
// (`statusline: true` in .bts/config/settings.yaml).  Off by default.
func statuslineEnabled(root string) bool {
	s, err := engine.LoadSettings(root)
	return err == nil && s.Statusline
}

// isBtsStatusLine reports whether a statusLine entry is the one bts writes:
// a command running .bts/status_line.sh (or the pre-rename .forge one).
func isBtsStatusLine(v interface{}) bool {
	sl, _ := v.(map[string]interface{})
	cmd, _ := sl["command"].(string)
	cmd = filepath.ToSlash(strings.TrimSpace(cmd))
	for _, script := range []string{".bts/status_line.sh", ".forge/status_line.sh"} {
		if cmd == script || strings.HasSuffix(cmd, "/"+script) {
			return true
		}
	}
	return false
}

// dropBtsStatusLine removes bts's own statusLine entry from parsed settings
// and reports whether it did; any other statusLine stays.
func dropBtsStatusLine(settings map[string]interface{}) bool {
	if !isBtsStatusLine(settings["statusLine"]) {
		return false
	}
	delete(settings, "statusLine")
	return true
}

// removeBtsStatusLine is the cleanup step that drops bts's statusLine from
// .claude/settings.local.json when the project has not turned it on.  It
// runs on every update and session start, so a project mid-recipe, whose
// template deploy is held, loses it too.
func removeBtsStatusLine(root string, warnings *[]string) string {
	if statuslineEnabled(root) {
		return ""
	}
	path := filepath.Join(root, ".claude", "settings.local.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var settings map[string]interface{}
	if json.Unmarshal(data, &settings) != nil || !dropBtsStatusLine(settings) {
		return ""
	}
	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		*warnings = append(*warnings, fmt.Sprintf("remove bts statusLine: %v", err))
		return ""
	}
	if err := os.WriteFile(path, append(out, '\n'), 0644); err != nil {
		*warnings = append(*warnings, fmt.Sprintf("remove bts statusLine: %v", err))
		return ""
	}
	return "Removed bts's statusLine from .claude/settings.local.json (statusline is off by default; set statusline: true in .bts/config/settings.yaml to keep it)"
}

// MergeHookSettings ensures all bts hooks are registered in
// .claude/settings.local.json, and bts's statusLine only when the project
// turned it on. Uses absolute paths to avoid CWD resolution issues. Fixes
// stale entries (wrong paths, underscore naming) and adds missing hooks.
func MergeHookSettings(projectRoot string) error {
	return mergeHookSettings(projectRoot, projectRoot)
}

// MergeSessionHookSettings registers the hooks of the bts project at btsRoot
// in sessionRoot's .claude/settings.local.json.  A Multica run starts Claude
// in its task's workdir, one level above the repository worktree that holds
// .bts/, and Claude reads settings only from the directory it started in.
// The script paths are absolute, so they still run the worktree's hooks.
func MergeSessionHookSettings(sessionRoot, btsRoot string) error {
	return mergeHookSettings(sessionRoot, btsRoot)
}

// mergeHookSettings writes into settingsRoot's settings.local.json the hooks
// and statusLine of the bts project at btsRoot.
func mergeHookSettings(settingsRoot, btsRoot string) error {
	absRoot, err := filepath.Abs(btsRoot)
	if err != nil {
		absRoot = btsRoot
	}
	absSettingsRoot, err := filepath.Abs(settingsRoot)
	if err != nil {
		absSettingsRoot = settingsRoot
	}

	settingsPath := filepath.Join(absSettingsRoot, ".claude", "settings.local.json")

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

	// StatusLine — only when the project turns it on (statusline: true in
	// .bts/config/settings.yaml); otherwise an entry bts wrote earlier is
	// dropped, and a person's own statusLine is left alone.
	if statuslineEnabled(absRoot) {
		slPath := filepath.Join(absRoot, ".bts", "status_line.sh")
		sl, _ := settings["statusLine"].(map[string]interface{})
		if sl == nil || !isAbsolutePath(sl["command"]) {
			settings["statusLine"] = map[string]interface{}{
				"type":    "command",
				"command": slPath,
			}
			changed = true
		}
	} else if dropBtsStatusLine(settings) {
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
		{"SessionStart", filepath.Join(absRoot, ".claude", "hooks", "bts-handle-session-start.sh"), 10},
		{"PreCompact", filepath.Join(absRoot, ".claude", "hooks", "bts-handle-pre-compact.sh"), 5},
		{"Stop", filepath.Join(absRoot, ".claude", "hooks", "bts-handle-stop.sh"), 10},
		{"SessionEnd", filepath.Join(absRoot, ".claude", "hooks", "bts-handle-session-end.sh"), 5},
		{"SubagentStart", filepath.Join(absRoot, ".claude", "hooks", "bts-handle-subagent-start.sh"), 5},
		{"SubagentStop", filepath.Join(absRoot, ".claude", "hooks", "bts-handle-subagent-stop.sh"), 5},
		{"PreToolUse", filepath.Join(absRoot, ".claude", "hooks", "bts-handle-pre-tool-use.sh"), 5},
		{"PostToolUse", filepath.Join(absRoot, ".claude", "hooks", "bts-handle-post-tool-use.sh"), 5},
		// A failing tool call (a Bash command exiting non-zero) sends this
		// instead of PostToolUse; without it, failures leave no metrics.
		{"PostToolUseFailure", filepath.Join(absRoot, ".claude", "hooks", "bts-handle-post-tool-use-failure.sh"), 5},
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

		// Check if existing entry has a stale bts/forge hook path
		if fixStaleBtsHook(existing, def.script, def.timeout) {
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

// fixStaleBtsHook finds a bts/forge hook entry within a hook event array
// and fixes its command path if stale. Returns true if any fix was applied.
func fixStaleBtsHook(eventValue interface{}, correctScript string, correctTimeout int) bool {
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
			if !isBtsHookCommand(cmd) {
				continue
			}
			// Found a bts/forge hook — fix if stale
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

// isBtsHookCommand returns true if the command path looks like a bts or forge hook.
func isBtsHookCommand(cmd string) bool {
	return strings.Contains(cmd, "bts-handle-") ||
		strings.Contains(cmd, "bts_handle_") ||
		strings.Contains(cmd, "forge-handle-") ||
		strings.Contains(cmd, "forge_handle_")
}

// isAbsolutePath checks if a settings value is a string starting with /.
func isAbsolutePath(v interface{}) bool {
	s, ok := v.(string)
	return ok && strings.HasPrefix(s, "/")
}
