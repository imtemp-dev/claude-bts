package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/pkg/version"
)

// Trial problem #128: templates moved only at SessionStart or by `bts
// update`, so a session that outlived its last recipe created the next one
// on old skills.  cool-crow cancelled r-005 at 22:32Z and its session,
// running since 21:33Z, created r-006 at 00:37Z on templates 931fca8 under
// binary 0b8ec2f.  With no recipe holding them, create now brings the
// templates to the binary's version first; an active recipe still holds
// them (#55).
func TestRecipeCreate_BringsTemplatesUpWhenNoRecipeHoldsThem(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{".bts/config", ".bts/local", ".bts/specs/recipes"} {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(d)), 0755); err != nil {
			t.Fatal(err)
		}
	}
	versionFile := filepath.Join(root, ".bts", "config", ".template-version")
	if err := os.WriteFile(versionFile, []byte("v0.0.1-old\n"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", t.TempDir())
	t.Chdir(root)

	if err := runQuiet(t, recipeCreateCmd, "recipe", "create", "--type", "build", "--topic", "next roadmap item"); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(versionFile); strings.TrimSpace(string(got)) != version.GetTemplateVersion() {
		t.Fatalf("no recipe held the templates, so create must bring them to the binary's %q; still %q",
			version.GetTemplateVersion(), strings.TrimSpace(string(got)))
	}

	// The new recipe is active: a second create leaves the templates as they are.
	if err := os.WriteFile(versionFile, []byte("v0.0.1-old\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := runQuiet(t, recipeCreateCmd, "recipe", "create", "--type", "build", "--topic", "another item"); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(versionFile); strings.TrimSpace(string(got)) != "v0.0.1-old" {
		t.Fatalf("an active recipe holds the templates (#55), got %q", strings.TrimSpace(string(got)))
	}
}
