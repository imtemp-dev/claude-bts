package template

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
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
	keep := ".claude/agents/bts-defender.md" // a live agent: must not be touched
	write(keep)

	var warnings []string
	if note := removeRetiredTemplates(root, &warnings); note == "" || len(warnings) != 0 {
		t.Errorf("want a removal note and no warning, got %q %v", note, warnings)
	}

	for _, rel := range retiredTemplateFiles {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); !os.IsNotExist(err) {
			t.Errorf("%s should have been removed (err=%v)", rel, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(keep))); err != nil {
		t.Errorf("%s must survive: %v", keep, err)
	}

	// A project that never had them: no error, nothing to do.
	if note := removeRetiredTemplates(t.TempDir(), &warnings); note != "" {
		t.Errorf("nothing to remove must give no note, got %q", note)
	}
}

// The list must never name a file the template still ships — that would
// delete a live file on every update.
func TestRetiredTemplateFiles_AreNotShippedAnyMore(t *testing.T) {
	tmpl, err := EmbeddedTemplates()
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range retiredTemplateFiles {
		if _, err := fs.Stat(tmpl, rel); err == nil {
			t.Errorf("%s is in retiredTemplateFiles but the template still ships it", rel)
		}
	}
}
