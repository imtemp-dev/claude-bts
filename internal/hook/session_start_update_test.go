package hook

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/imtemp-dev/claude-bts/pkg/version"
)

// Trial problem #36: a project the SessionStart auto-update had already
// moved to the current version kept the retired simulate skill and agent,
// because the auto-update never ran `bts update`'s cleanup and `bts update`
// then stopped at "already up to date".  The session start must remove them.
func TestAutoUpdateTemplates_RemovesRetiredFilesAtTheCurrentVersion(t *testing.T) {
	root := t.TempDir()
	retired := []string{
		".claude/skills/bts-simulate/SKILL.md",
		".claude/agents/bts-simulator.md",
	}
	for _, rel := range append(retired, ".bts/config/.template-version") {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		body := "retired\n"
		if filepath.Base(rel) == ".template-version" {
			body = version.GetTemplateVersion()
		}
		if err := os.WriteFile(p, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}

	if autoUpdateTemplates(root) {
		t.Error("templates already at the current version must not be redeployed")
	}
	for _, rel := range retired {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); !os.IsNotExist(err) {
			t.Errorf("%s must be removed at session start (err=%v)", rel, err)
		}
	}
}
