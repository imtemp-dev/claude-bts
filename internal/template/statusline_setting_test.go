package template

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// bts used to register its own statusLine in every project's
// .claude/settings.local.json.  A project entry overrides the statusline
// a person configures in their own Claude Code settings, and Claude Code
// now offers statusline customisation itself, so bts's is off by default
// (jongyoon, 11-06): `statusline: true` in .bts/config/settings.yaml turns
// it back on.  An entry bts wrote earlier is removed on the next update or
// session start; a statusLine the person wrote is never touched.

func statuslineRoot(t *testing.T, settingsYAML string, local map[string]any) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".bts", "config"), 0755); err != nil {
		t.Fatal(err)
	}
	if settingsYAML != "" {
		if err := os.WriteFile(filepath.Join(root, ".bts", "config", "settings.yaml"), []byte(settingsYAML), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if local != nil {
		if err := os.MkdirAll(filepath.Join(root, ".claude"), 0755); err != nil {
			t.Fatal(err)
		}
		data, _ := json.Marshal(local)
		if err := os.WriteFile(filepath.Join(root, ".claude", "settings.local.json"), data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func localSettings(t *testing.T, root string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, ".claude", "settings.local.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestMergeHookSettings_NoStatusLineByDefault(t *testing.T) {
	root := statuslineRoot(t, "", nil)
	if err := MergeHookSettings(root); err != nil {
		t.Fatal(err)
	}
	m := localSettings(t, root)
	if _, ok := m["statusLine"]; ok {
		t.Fatalf("bts must not register a statusLine by default: %v", m["statusLine"])
	}
	if hooks, _ := m["hooks"].(map[string]any); len(hooks) == 0 {
		t.Fatal("the hooks are still registered")
	}
}

func TestMergeHookSettings_StatusLineWhenTurnedOn(t *testing.T) {
	root := statuslineRoot(t, "statusline: true\n", nil)
	if err := MergeHookSettings(root); err != nil {
		t.Fatal(err)
	}
	sl, _ := localSettings(t, root)["statusLine"].(map[string]any)
	if cmd, _ := sl["command"].(string); !strings.HasSuffix(cmd, filepath.Join(".bts", "status_line.sh")) {
		t.Fatalf("statusline: true registers bts's statusLine, got %v", sl)
	}
}

func TestCleanup_RemovesOnlyBtsStatusLineWhenOff(t *testing.T) {
	btsEntry := func(root string) map[string]any {
		return map[string]any{"statusLine": map[string]any{"type": "command", "command": filepath.Join(root, ".bts", "status_line.sh")}, "permissions": map[string]any{"allow": []any{"Bash(ls)"}}}
	}

	root := statuslineRoot(t, "", nil)
	data, _ := json.Marshal(btsEntry(root))
	_ = os.MkdirAll(filepath.Join(root, ".claude"), 0755)
	_ = os.WriteFile(filepath.Join(root, ".claude", "settings.local.json"), data, 0644)
	res := CleanupOnly(root)
	m := localSettings(t, root)
	if _, ok := m["statusLine"]; ok {
		t.Fatal("an entry bts wrote earlier must go when the setting is off")
	}
	if _, ok := m["permissions"]; !ok {
		t.Fatal("the rest of settings.local.json is kept")
	}
	if !strings.Contains(strings.Join(res.Notes, "\n"), "statusLine") {
		t.Errorf("the update says what it removed, got %v", res.Notes)
	}

	own := statuslineRoot(t, "", map[string]any{"statusLine": map[string]any{"type": "command", "command": "~/bin/my-status.sh"}})
	CleanupOnly(own)
	if sl, _ := localSettings(t, own)["statusLine"].(map[string]any); sl["command"] != "~/bin/my-status.sh" {
		t.Fatalf("a statusLine the person wrote is never touched, got %v", sl)
	}

	on := statuslineRoot(t, "statusline: true\n", nil)
	data, _ = json.Marshal(btsEntry(on))
	_ = os.MkdirAll(filepath.Join(on, ".claude"), 0755)
	_ = os.WriteFile(filepath.Join(on, ".claude", "settings.local.json"), data, 0644)
	CleanupOnly(on)
	if _, ok := localSettings(t, on)["statusLine"]; !ok {
		t.Fatal("with statusline: true the entry stays")
	}
}
