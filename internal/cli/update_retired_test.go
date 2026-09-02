package cli

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/template"
)

// DeployForce writes what the binary embeds and never looks at what an
// earlier binary left behind, so a template file removed from a release
// stayed in every project that had it. A retired agent file keeps
// describing a role the skills no longer give it, to a harness that
// still lists it.
func TestRemoveRetiredTemplates_DeletesOnlyTheRetiredFiles(t *testing.T) {
	root := t.TempDir()
	write := func(rel string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	for _, rel := range retiredTemplateFiles {
		write(rel)
	}
	keep := ".claude/agents/bts-simulator.md"
	write(keep)

	removeRetiredTemplates(root)

	for _, rel := range retiredTemplateFiles {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); !os.IsNotExist(err) {
			t.Errorf("%s should have been removed (err=%v)", rel, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(keep))); err != nil {
		t.Errorf("%s must survive: %v", keep, err)
	}

	// A project that never had them: no error, nothing to do.
	removeRetiredTemplates(t.TempDir())
}

// The list must never name a file the template still ships — that would
// delete a live file on every update.
func TestRetiredTemplateFiles_AreNotShippedAnyMore(t *testing.T) {
	tmpl, err := template.EmbeddedTemplates()
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range retiredTemplateFiles {
		if _, err := fs.Stat(tmpl, rel); err == nil {
			t.Errorf("%s is in retiredTemplateFiles but the template still ships it", rel)
		}
	}
}
