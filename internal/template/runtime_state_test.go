package template

import (
	"io/fs"
	"strings"
	"testing"
)

// templates/.bts/ fills itself whenever a bts command runs with a working
// directory inside the template tree — state.FindRoot walks upward for a
// `.bts/` and finds that one — and `//go:embed all:` picks dot-directories
// up. DeployForce overwrites, so a leaked file there replaces a user's own
// state on `bts update`.
func TestRuntimeStateIsNeverDeployed(t *testing.T) {
	cases := map[string]bool{
		".bts/local":                         true,
		".bts/local/metrics.jsonl":           true,
		".bts/local/recipes/r-1/x.json":      true,
		".bts/specs":                         true,
		".bts/specs/recipes/r-001/draft.md":  true,
		".bts/specs/debates/d-1/round-1.md":  true,
		".bts/config/settings.yaml":          false,
		".bts/status_line.sh":                false,
		".claude/skills/bts-verify/SKILL.md": false,
		".bts":                               false, // must be walked into
	}
	for path, want := range cases {
		if got := isRuntimeState(path); got != want {
			t.Errorf("isRuntimeState(%q) = %v, want %v", path, got, want)
		}
	}
}

// The allowlist and the embedded tree must agree in both directions: an
// unlisted name means state leaked in, and a listed name that no longer
// exists means the list has drifted. The earlier denylist named
// .bts/local/ alone, which left .bts/specs/ — a developer's own recipes
// and drafts, and not gitignored either — one stray command away from
// being compiled into a release binary.
func TestEmbeddedTemplatesCarryOnlyTemplates(t *testing.T) {
	fsys, err := EmbeddedTemplates()
	if err != nil {
		t.Fatal(err)
	}
	entries, err := fs.ReadDir(fsys, ".bts")
	if err != nil {
		t.Fatalf("read .bts from the embedded FS: %v", err)
	}
	seen := map[string]bool{}
	for _, e := range entries {
		seen[e.Name()] = true
		if !templatePathsUnderBTS[e.Name()] {
			t.Errorf("templates/.bts/%s is embedded but is not a template — "+
				"it is runtime state that leaked into the tree; delete it "+
				"(or add it to templatePathsUnderBTS if it really is one)", e.Name())
		}
	}
	for name := range templatePathsUnderBTS {
		if !seen[name] {
			t.Errorf("templatePathsUnderBTS lists %q, which the embedded tree no longer has", name)
		}
	}
}

// Whatever the allowlist says, nothing runtime-shaped may be walked for
// deployment.
func TestDeployWalkSkipsRuntimeState(t *testing.T) {
	fsys, err := EmbeddedTemplates()
	if err != nil {
		t.Fatal(err)
	}
	err = fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if isRuntimeState(path) {
			t.Errorf("%s would be skipped at deploy time, so it must not be in the tree at all", path)
		}
		if strings.HasPrefix(path, ".bts/local") || strings.HasPrefix(path, ".bts/specs") {
			t.Errorf("%s is runtime state compiled into the binary", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
