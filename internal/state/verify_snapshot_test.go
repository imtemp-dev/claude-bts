package state

import (
	"os"
	"path/filepath"
	"testing"
)

func TestVerifySnapshot_SaveLoadRoundtrip(t *testing.T) {
	root := t.TempDir()
	recipeDir := filepath.Join(root, ".bts", "specs", "recipes", "r-001")
	if err := os.MkdirAll(recipeDir, 0755); err != nil {
		t.Fatal(err)
	}
	doc := filepath.Join(recipeDir, "draft.md")
	if err := os.WriteFile(doc, []byte("v1 content\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if _, ok, err := LoadVerifySnapshot(root, "r-001", "draft.md"); err != nil || ok {
		t.Fatalf("expected no snapshot yet, ok=%v err=%v", ok, err)
	}

	if err := SaveVerifySnapshot(root, "r-001", doc); err != nil {
		t.Fatalf("save: %v", err)
	}
	data, ok, err := LoadVerifySnapshot(root, "r-001", "draft.md")
	if err != nil || !ok {
		t.Fatalf("load: ok=%v err=%v", ok, err)
	}
	if string(data) != "v1 content\n" {
		t.Errorf("content: %q", data)
	}

	// Overwrite with a new revision.
	if err := os.WriteFile(doc, []byte("v2 content\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := SaveVerifySnapshot(root, "r-001", doc); err != nil {
		t.Fatalf("re-save: %v", err)
	}
	data, _, _ = LoadVerifySnapshot(root, "r-001", "draft.md")
	if string(data) != "v2 content\n" {
		t.Errorf("after overwrite: %q", data)
	}
}

func TestVerifySnapshot_SaveMissingDoc(t *testing.T) {
	root := t.TempDir()
	if err := SaveVerifySnapshot(root, "r-001", filepath.Join(root, "nope.md")); err == nil {
		t.Fatal("expected error for missing doc")
	}
}

func TestRecipeIDFromDocPath(t *testing.T) {
	cases := []struct {
		path, want string
	}{
		{".bts/specs/recipes/r-019/draft.md", "r-019"},
		{"/abs/proj/.bts/specs/recipes/r-2-auth/final.md", "r-2-auth"},
		{".bts/specs/recipes/r-019", ""}, // recipe dir itself, no doc
		{"docs/readme.md", ""},
		{"recipes/x/notes/deep.md", "x"},
	}
	for _, c := range cases {
		if got := RecipeIDFromDocPath(c.path); got != c.want {
			t.Errorf("RecipeIDFromDocPath(%q) = %q, want %q", c.path, got, c.want)
		}
	}
}
