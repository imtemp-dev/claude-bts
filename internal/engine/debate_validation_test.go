package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// debateFixture lays out a project with a recipe and returns its recipe dir.
func debateFixture(t *testing.T) (root, recipeDir string) {
	t.Helper()
	root = t.TempDir()
	recipeDir = filepath.Join(root, ".bts", "specs", "recipes", "r-001-test")
	if err := os.MkdirAll(recipeDir, 0755); err != nil {
		t.Fatal(err)
	}
	return root, recipeDir
}

func writeDebate(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
}

const goodDebateJSON = `{"id":"001-x","topic":"t","rounds":3,"decided":true,"conclusion":"c",
"started_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"}`

// The normal arrangement: `bts debate log` writes machine state to the
// project tree, /bts-debate writes round markdown beside the recipe.
// Validating the directories independently reports that as a missing
// state file, which is a false positive on every real project.
func TestValidateDebates_StateInProjectTreeRoundsInRecipeTree(t *testing.T) {
	root, recipeDir := debateFixture(t)
	writeDebate(t, filepath.Join(recipeDir, "debates", "001-x"), map[string]string{
		"round-1.md": "positions", "round-2.md": "rebuttals",
	})
	writeDebate(t, filepath.Join(root, ".bts", "specs", "debates", "001-x"), map[string]string{
		"debate.json": goodDebateJSON, "round-1.md": "positions",
	})
	if errs := validateDebates(recipeDir); len(errs) != 0 {
		t.Fatalf("state in either tree satisfies the gate, got %+v", errs)
	}
}

// The gate's actual purpose: rounds recorded, conclusion never written.
func TestValidateDebates_RoundsWithNoStateAnywhere(t *testing.T) {
	_, recipeDir := debateFixture(t)
	writeDebate(t, filepath.Join(recipeDir, "debates", "001-x"), map[string]string{
		"round-1.md": "positions",
	})
	errs := validateDebates(recipeDir)
	if len(errs) != 1 {
		t.Fatalf("got %d errors, want 1: %+v", len(errs), errs)
	}
	if !strings.Contains(errs[0].Message, "no state file in any debate tree") {
		t.Errorf("message = %q", errs[0].Message)
	}
}

// The name the CLI actually writes must be accepted — the old validator
// looked for meta.json, which nothing has ever produced.
func TestValidateDebates_AcceptsDebateJSONAndLegacyMetaJSON(t *testing.T) {
	for _, name := range []string{"debate.json", "meta.json"} {
		t.Run(name, func(t *testing.T) {
			_, recipeDir := debateFixture(t)
			writeDebate(t, filepath.Join(recipeDir, "debates", "001-x"), map[string]string{
				name: goodDebateJSON, "round-1.md": "positions",
			})
			if errs := validateDebates(recipeDir); len(errs) != 0 {
				t.Fatalf("%s must be accepted, got %+v", name, errs)
			}
		})
	}
}

// A malformed state file is still reported, wherever it lives.
func TestValidateDebates_ReportsMissingFieldsInTheStateFile(t *testing.T) {
	_, recipeDir := debateFixture(t)
	writeDebate(t, filepath.Join(recipeDir, "debates", "001-x"), map[string]string{
		"debate.json": `{"id":"001-x","topic":"t"}`, "round-1.md": "r",
	})
	errs := validateDebates(recipeDir)
	if len(errs) == 0 {
		t.Fatal("a state file missing decided/rounds must still be reported")
	}
	var sawDecided bool
	for _, e := range errs {
		if e.Field == "decided" {
			sawDecided = true
		}
		if strings.HasSuffix(e.File, "/meta.json") {
			t.Errorf("error labels the file that exists, not meta.json: %q", e.File)
		}
	}
	if !sawDecided {
		t.Errorf("expected a 'decided' error, got %+v", errs)
	}
}

func TestValidateDebates_NoDebatesIsQuiet(t *testing.T) {
	_, recipeDir := debateFixture(t)
	if errs := validateDebates(recipeDir); len(errs) != 0 {
		t.Fatalf("a recipe with no debates must be quiet, got %+v", errs)
	}
}
