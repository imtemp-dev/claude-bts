package state

import (
	"os"
	"path/filepath"
	"testing"
)

// Trial problem #88: intent.md, scope.md and vision.md carry a status
// line that agents flipped by hand (sed -i, denied twice on true-shark and
// once on swift-eagle, accepted on neat-wolf), in two forms, and the
// readers disagreed: workstate.go matched only `### Status: CONFIRMED`,
// roadmap.go only a line starting `Status:`.  One reader and one writer.
func TestDocStatus_ReadsEveryForm(t *testing.T) {
	cases := map[string]string{
		"# Intent\n\nStatus: EXPLORING\n":          "EXPLORING",
		"# Scope\n\n### Status: CONFIRMED\n":       "CONFIRMED",
		"# Vision\n**Status:** DRAFT\n":            "DRAFT",
		"# Scope\n\nThe status quo stays.\n":       "",
		"# Scope\n\n### Status: confirmed\n":       "CONFIRMED",
		"# Intent\nStatus: CONFIRMED\nStatus: X\n": "CONFIRMED",
	}
	for content, want := range cases {
		if got := DocStatus(content); got != want {
			t.Errorf("DocStatus(%q) = %q, want %q", content, got, want)
		}
	}
}

func TestSetDocStatus_KeepsTheLinesForm(t *testing.T) {
	cases := map[string]string{
		"# Intent\n\nStatus: EXPLORING\nbody\n": "# Intent\n\nStatus: CONFIRMED\nbody\n",
		"# Scope\n\n### Status: DRAFT\nbody\n":  "# Scope\n\n### Status: CONFIRMED\nbody\n",
		"# Scope\n\nbody with no status line\n": "# Scope\n\n### Status: CONFIRMED\n\nbody with no status line\n",
	}
	for in, want := range cases {
		if got := SetDocStatus(in, "CONFIRMED"); got != want {
			t.Errorf("SetDocStatus(%q)\n got %q\nwant %q", in, got, want)
		}
	}
}

func TestBuildWorkState_ReadsAPlainScopeStatus(t *testing.T) {
	root := t.TempDir()
	r := &RecipeState{ID: "r-088", Type: "build", Phase: "research"}
	if err := SaveRecipeState(root, r); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(RecipeDir(root, "r-088"), "scope.md"), []byte("# Scope\n\nStatus: CONFIRMED\n"), 0644)
	ws, err := BuildWorkState(root)
	if err != nil || ws == nil {
		t.Fatalf("no work state built (%v)", err)
	}
	if ws.ScopeStatus != "CONFIRMED" {
		t.Errorf("a plain Status line is a confirmation too, got %q", ws.ScopeStatus)
	}
}
