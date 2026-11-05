package state

import (
	"os"
	"path/filepath"
	"testing"
)

func writeImplFile(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func mustImplHash(t *testing.T, root string, tasks []Task) string {
	t.Helper()
	h, err := ImplHash(root, tasks)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func TestImplHash_OrderAndDuplicatesDoNotMatter(t *testing.T) {
	root := t.TempDir()
	writeImplFile(t, root, "src/a.go", "package a\n")
	writeImplFile(t, root, "src/b.go", "package b\n")

	one := mustImplHash(t, root, []Task{{File: "src/a.go"}, {File: "src/b.go"}})
	two := mustImplHash(t, root, []Task{{File: "src/b.go"}, {File: "./src/a.go"}, {File: "src/b.go"}})
	if one == "" || one != two {
		t.Fatalf("same files in another order must hash the same: %q vs %q", one, two)
	}
}

func TestImplHash_ChangesWhenATaskFileChanges(t *testing.T) {
	root := t.TempDir()
	writeImplFile(t, root, "src/a.go", "package a\n")
	tasks := []Task{{File: "src/a.go"}}
	before := mustImplHash(t, root, tasks)

	writeImplFile(t, root, "src/a.go", "package a\n\nfunc F() {}\n")
	if after := mustImplHash(t, root, tasks); after == before {
		t.Fatal("editing a task file must change the hash")
	}
}

func TestImplHash_IgnoresFilesNoTaskNames(t *testing.T) {
	root := t.TempDir()
	writeImplFile(t, root, "src/a.go", "package a\n")
	tasks := []Task{{File: "src/a.go"}}
	before := mustImplHash(t, root, tasks)

	writeImplFile(t, root, "notes.md", "unrelated\n")
	writeImplFile(t, root, ".bts/local/metrics.jsonl", "{}\n")
	if after := mustImplHash(t, root, tasks); after != before {
		t.Fatal("files outside the recipe must not read as drift")
	}
}

func TestImplHash_CRLFIsNotAChange(t *testing.T) {
	root := t.TempDir()
	writeImplFile(t, root, "src/a.go", "line one\nline two\n")
	tasks := []Task{{File: "src/a.go"}}
	before := mustImplHash(t, root, tasks)

	writeImplFile(t, root, "src/a.go", "line one\r\nline two\r\n")
	if after := mustImplHash(t, root, tasks); after != before {
		t.Fatal("line endings are a checkout property, not content")
	}
}

func TestImplHash_DeletedDiffersFromEmpty(t *testing.T) {
	root := t.TempDir()
	tasks := []Task{{File: "src/gone.go", Action: "delete"}}
	absent := mustImplHash(t, root, tasks)

	writeImplFile(t, root, "src/gone.go", "")
	if empty := mustImplHash(t, root, tasks); empty == absent {
		t.Fatal("a file that exists empty must not hash like a deleted one")
	}
}

func TestImplHash_NoFilesIsEmpty(t *testing.T) {
	if h := mustImplHash(t, t.TempDir(), []Task{{ID: "t-1"}}); h != "" {
		t.Fatalf("no task files must give an empty hash, got %q", h)
	}
}

// Outside a git repository a recipe without tasks.json has no fingerprint;
// inside one it falls back to the tree (TestRecipeImplHash_FallsBackToTheTree).
func TestRecipeImplHash_NoTasksJSON(t *testing.T) {
	h, err := RecipeImplHash(t.TempDir(), "r-001-none")
	if err != nil || h != "" {
		t.Fatalf("outside git a recipe without tasks.json must give (\"\", nil), got (%q, %v)", h, err)
	}
}
