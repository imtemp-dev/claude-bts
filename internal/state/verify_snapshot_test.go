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

func TestDirtyVerifiedDocs_NoSnapshotDirIsClean(t *testing.T) {
	root := t.TempDir()
	dirty, err := DirtyVerifiedDocs(root, "r-001")
	if err != nil || dirty != nil {
		t.Fatalf("legacy recipe must be clean: dirty=%v err=%v", dirty, err)
	}
}

func TestDirtyVerifiedDocs_CleanMatchDirtyMismatch(t *testing.T) {
	root := t.TempDir()
	recipeDir := filepath.Join(root, ".bts", "specs", "recipes", "r-001")
	if err := os.MkdirAll(recipeDir, 0755); err != nil {
		t.Fatal(err)
	}
	doc := filepath.Join(recipeDir, "draft.md")
	if err := os.WriteFile(doc, []byte("v1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := SaveVerifySnapshot(root, "r-001", doc); err != nil {
		t.Fatal(err)
	}

	// Unmodified → clean.
	dirty, err := DirtyVerifiedDocs(root, "r-001")
	if err != nil || len(dirty) != 0 {
		t.Fatalf("expected clean, got dirty=%v err=%v", dirty, err)
	}

	// Modified after verification → dirty.
	if err := os.WriteFile(doc, []byte("v2 modified\n"), 0644); err != nil {
		t.Fatal(err)
	}
	dirty, err = DirtyVerifiedDocs(root, "r-001")
	if err != nil || len(dirty) != 1 || dirty[0] != "draft.md" {
		t.Fatalf("expected [draft.md], got dirty=%v err=%v", dirty, err)
	}

	// Re-snapshot (re-verification) → clean again.
	if err := SaveVerifySnapshot(root, "r-001", doc); err != nil {
		t.Fatal(err)
	}
	dirty, _ = DirtyVerifiedDocs(root, "r-001")
	if len(dirty) != 0 {
		t.Fatalf("expected clean after re-snapshot, got %v", dirty)
	}
}

func TestDirtyVerifiedDocs_MultipleDocsSortedAndPartial(t *testing.T) {
	root := t.TempDir()
	recipeDir := filepath.Join(root, ".bts", "specs", "recipes", "r-001")
	if err := os.MkdirAll(recipeDir, 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"draft.md", "fix-spec.md", "wireframe.md"} {
		p := filepath.Join(recipeDir, name)
		if err := os.WriteFile(p, []byte("v1 "+name), 0644); err != nil {
			t.Fatal(err)
		}
		if err := SaveVerifySnapshot(root, "r-001", p); err != nil {
			t.Fatal(err)
		}
	}
	// Modify two of three, in non-alphabetical order.
	if err := os.WriteFile(filepath.Join(recipeDir, "wireframe.md"), []byte("changed"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(recipeDir, "draft.md"), []byte("changed"), 0644); err != nil {
		t.Fatal(err)
	}
	dirty, err := DirtyVerifiedDocs(root, "r-001")
	if err != nil {
		t.Fatal(err)
	}
	if len(dirty) != 2 || dirty[0] != "draft.md" || dirty[1] != "wireframe.md" {
		t.Fatalf("expected sorted [draft.md wireframe.md], got %v", dirty)
	}
}

func TestDirtyVerifiedDocs_MissingCurrentDocSkipped(t *testing.T) {
	root := t.TempDir()
	recipeDir := filepath.Join(root, ".bts", "specs", "recipes", "r-001")
	if err := os.MkdirAll(recipeDir, 0755); err != nil {
		t.Fatal(err)
	}
	doc := filepath.Join(recipeDir, "draft.md")
	if err := os.WriteFile(doc, []byte("v1"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := SaveVerifySnapshot(root, "r-001", doc); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(doc); err != nil {
		t.Fatal(err)
	}
	dirty, err := DirtyVerifiedDocs(root, "r-001")
	if err != nil || len(dirty) != 0 {
		t.Fatalf("deleted doc must be skipped (other gates own it): dirty=%v err=%v", dirty, err)
	}
}

func TestDirtyVerifiedDocs_TmpLeftoverIgnored(t *testing.T) {
	root := t.TempDir()
	recipeDir := filepath.Join(root, ".bts", "specs", "recipes", "r-001")
	if err := os.MkdirAll(recipeDir, 0755); err != nil {
		t.Fatal(err)
	}
	doc := filepath.Join(recipeDir, "draft.md")
	if err := os.WriteFile(doc, []byte("v1"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := SaveVerifySnapshot(root, "r-001", doc); err != nil {
		t.Fatal(err)
	}
	// Simulate a crashed atomic write leaving a .tmp behind.
	tmp := VerifySnapshotPath(root, "r-001", "draft.md") + ".tmp"
	if err := os.WriteFile(tmp, []byte("partial"), 0644); err != nil {
		t.Fatal(err)
	}
	dirty, err := DirtyVerifiedDocs(root, "r-001")
	if err != nil || len(dirty) != 0 {
		t.Fatalf(".tmp leftover must be ignored: dirty=%v err=%v", dirty, err)
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

// A --doc that legitimately resolves outside the recipe directory got a
// doc_hash recorded and was then never re-checked: the dirty check
// looked the BASENAME up under the recipe directory, FileContentHash
// returned ok=false on a path that does not exist, and the loop skipped
// it. Rule 3 silently exempted every such document.
func TestDirtyVerifiedDocs_FollowsTheRecordedDocPath(t *testing.T) {
	root := t.TempDir()
	recipeID := "r-001-test"
	if err := os.MkdirAll(RecipeDir(root, recipeID), 0755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(root, "docs")
	if err := os.MkdirAll(outside, 0755); err != nil {
		t.Fatal(err)
	}
	docPath := filepath.Join(outside, "api-spec.md")
	if err := os.WriteFile(docPath, []byte("# api spec\n"), 0644); err != nil {
		t.Fatal(err)
	}
	hash, ok, err := FileContentHash(docPath)
	if err != nil || !ok {
		t.Fatalf("hash: %v ok=%v", err, ok)
	}
	if err := AppendVerifyLog(root, recipeID, &VerifyLogEntry{
		Iteration: 1, Doc: "api-spec.md", DocPath: "docs/api-spec.md",
		DocHash: hash, FullPass: true, Status: "converged",
	}); err != nil {
		t.Fatal(err)
	}

	dirty, err := DirtyVerifiedDocs(root, recipeID)
	if err != nil {
		t.Fatal(err)
	}
	if len(dirty) != 0 {
		t.Errorf("unchanged document reported dirty: %v", dirty)
	}

	if err := os.WriteFile(docPath, []byte("# api spec, revised\n"), 0644); err != nil {
		t.Fatal(err)
	}
	dirty, err = DirtyVerifiedDocs(root, recipeID)
	if err != nil {
		t.Fatal(err)
	}
	if len(dirty) != 1 || dirty[0] != "api-spec.md" {
		t.Errorf("dirty = %v, want [api-spec.md] — rule 3 must reach documents outside the recipe directory", dirty)
	}
}

// Trial problem #45: only a build recipe's final.md lets a clean delta
// round move its reviewed revision. Every other document keeps rule 3:
// only a full pass advances it.
func TestDirtyVerifiedDocs_ACleanDeltaCountsOnlyForABuildFinal(t *testing.T) {
	root := t.TempDir()
	recipeID := "r-045"
	dir := RecipeDir(root, recipeID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) string {
		t.Helper()
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
		h, _, _ := FileContentHash(p)
		return h
	}
	full := write("final.md", "v1\n")
	delta := write("final.md", "v2\n")
	for _, e := range []VerifyLogEntry{
		{Iteration: 1, Doc: "final.md", FullPass: true, Major: 1, DocHash: full},
		{Iteration: 2, Doc: "final.md", FullPass: false, DocHash: delta},
	} {
		e := e
		if err := AppendVerifyLog(root, recipeID, &e); err != nil {
			t.Fatal(err)
		}
	}

	for _, tc := range []struct {
		typ       string
		wantDirty bool
	}{{"blueprint", true}, {"build", false}} {
		if err := SaveRecipeState(root, &RecipeState{ID: recipeID, Type: tc.typ, Phase: "falsify"}); err != nil {
			t.Fatal(err)
		}
		dirty, err := DirtyVerifiedDocs(root, recipeID)
		if err != nil {
			t.Fatal(err)
		}
		if got := len(dirty) == 1 && dirty[0] == "final.md"; got != tc.wantDirty {
			t.Errorf("%s recipe: dirty=%v, want final.md dirty=%v", tc.typ, dirty, tc.wantDirty)
		}
	}

	// A delta that found a major does not move the revision, even for a
	// build recipe.
	if err := AppendVerifyLog(root, recipeID, &VerifyLogEntry{Iteration: 3, Doc: "final.md", Critical: 1, DocHash: delta}); err != nil {
		t.Fatal(err)
	}
	if dirty, _ := DirtyVerifiedDocs(root, recipeID); len(dirty) != 1 {
		t.Errorf("a delta chain with a critical must leave final.md dirty, got %v", dirty)
	}
}
