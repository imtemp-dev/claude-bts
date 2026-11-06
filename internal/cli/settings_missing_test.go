package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Trial problem #98: the relay's settings.yaml, written before v0.18.0,
// lacked implement.require_edit_go and implement.format_cmd.  SessionStart
// and `bts update` refresh templates, not settings.yaml, and nothing said
// keys were missing, so the knobs were invisible.  `bts doctor` now lists
// them and names `bts migrate settings`.
func TestCheckMissingSettingsKeys(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".bts", "config")
	_ = os.MkdirAll(dir, 0755)
	write := func(s string) {
		if err := os.WriteFile(filepath.Join(dir, "settings.yaml"), []byte(s), 0644); err != nil {
			t.Fatal(err)
		}
	}

	write("implement:\n  max_build_retries: 8\n  # require_edit_go: true\n")
	issues := checkMissingSettingsKeys(root)
	if len(issues) != 1 {
		t.Fatalf("want one doctor issue listing the missing keys, got %v", issues)
	}
	msg := issues[0].message + " " + issues[0].fix
	if !strings.Contains(msg, "implement.format_cmd") || !strings.Contains(msg, "bts migrate settings") {
		t.Errorf("the issue names a missing key and the command that adds it: %s", msg)
	}
	if strings.Contains(msg, "implement.require_edit_go") {
		t.Errorf("a key commented out on purpose is not missing: %s", msg)
	}

	var full strings.Builder
	parents := map[string][]settingsInsertion{}
	var order []string
	for _, ins := range settingsInsertions {
		if _, ok := parents[ins.Parent]; !ok {
			order = append(order, ins.Parent)
		}
		parents[ins.Parent] = append(parents[ins.Parent], ins)
	}
	for _, p := range order {
		full.WriteString(p + ":\n")
		for _, ins := range parents[p] {
			full.WriteString("  " + ins.Key + ": " + ins.Value + "\n")
		}
	}
	write(full.String())
	if issues := checkMissingSettingsKeys(root); len(issues) != 0 {
		t.Errorf("every key present: want nothing, got %v", issues)
	}
}
