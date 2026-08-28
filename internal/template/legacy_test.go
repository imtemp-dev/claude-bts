package template

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Deployment writes the jig-* set but never deletes. If the retired files
// stay, Claude Code loads both sets: every skill and rule appears twice, and
// the stale copy points at a binary that is no longer installed.
func TestCleanupLegacyPrefixesRemovesRetiredTemplatesOnly(t *testing.T) {
	root := t.TempDir()
	claude := filepath.Join(root, ".claude")

	keep := []string{"skills/jig-spec", "agents/jig-verifier.md", "rules/jig-schema.md"}
	drop := []string{"skills/bts-recipe-blueprint", "agents/bts-verifier.md",
		"rules/bts-schema.md", "hooks/bts-handle-stop.sh", "commands/bts-recipe.md",
		"skills/forge-plan"}
	// A user's own file must survive: cleanup keys off the prefix, not the dir.
	keep = append(keep, "skills/my-own-skill", "commands/deploy.md")

	for _, p := range append(append([]string{}, keep...), drop...) {
		full := filepath.Join(claude, p)
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(p, ".md") || strings.HasSuffix(p, ".sh") {
			if err := os.WriteFile(full, []byte("x"), 0644); err != nil {
				t.Fatal(err)
			}
		} else if err := os.MkdirAll(full, 0755); err != nil {
			t.Fatal(err)
		}
	}

	if n := CleanupLegacyPrefixes(root); n != len(drop) {
		t.Errorf("removed %d, want %d", n, len(drop))
	}
	for _, p := range drop {
		if _, err := os.Stat(filepath.Join(claude, p)); !os.IsNotExist(err) {
			t.Errorf("%s survived cleanup", p)
		}
	}
	for _, p := range keep {
		if _, err := os.Stat(filepath.Join(claude, p)); err != nil {
			t.Errorf("%s was removed but should have been kept: %v", p, err)
		}
	}
}

// settings.local.json holds absolute paths to the hook scripts. A rebrand
// renames those scripts, and a hook that cannot run is a gate that fails open.
func TestMigrateHookSettingsRepointsRetiredPaths(t *testing.T) {
	root := t.TempDir()
	claude := filepath.Join(root, ".claude")
	if err := os.MkdirAll(claude, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(claude, "settings.local.json")
	body := `{"hooks":{"Stop":[{"hooks":[{"command":"` + root +
		`/.claude/hooks/bts-handle-stop.sh"}]}]},"statusLine":{"command":"` + root + `/.bts/status_line.sh"}}`
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}

	if from := MigrateHookSettings(root); from != "bts" {
		t.Errorf("migrated from %q, want \"bts\"", from)
	}
	got, _ := os.ReadFile(path)
	if strings.Contains(string(got), "bts-handle-") || strings.Contains(string(got), ".bts/status_line.sh") {
		t.Errorf("retired paths survived: %s", got)
	}
	if !strings.Contains(string(got), "jig-handle-stop.sh") ||
		!strings.Contains(string(got), ".jig/status_line.sh") {
		t.Errorf("paths not repointed at jig: %s", got)
	}

	// Idempotent: a second pass has nothing to do and must not report one.
	if from := MigrateHookSettings(root); from != "" {
		t.Errorf("second pass reported %q, want no-op", from)
	}
}
