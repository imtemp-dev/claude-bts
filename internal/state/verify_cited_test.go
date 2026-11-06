package state

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// Trial problem #106: a build recipe's review reads final.md and the
// documents it points at, but only final.md's hash was checked.  On
// cool-crow and neat-wolf the last round's minors were fixed in domain.md
// seconds after the round, and DONE passed with that change unread.
func TestDirtyVerifiedDocs_ACitedDocumentChangedAfterTheReview(t *testing.T) {
	root := t.TempDir()
	id := "r-106"
	dir := RecipeDir(root, id)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("final.md", "# Build spec\n")
	write("domain.md", "# Domain\nINV-001 as reviewed\n")
	write("wireframe.md", "# Wireframe\n")
	logRound(t, root, id, VerifyLogEntry{
		Iteration: 1, Doc: "final.md", FullPass: true, Status: "continue",
		DocHash: ContentHash([]byte("# Build spec\n")),
		CitedHashes: map[string]string{
			"domain.md":    ContentHash([]byte("# Domain\nINV-001 as reviewed\n")),
			"wireframe.md": ContentHash([]byte("# Wireframe\n")),
		},
	})

	if dirty, _ := DirtyVerifiedDocs(root, id); len(dirty) != 0 {
		t.Fatalf("nothing changed yet, got %v", dirty)
	}

	write("domain.md", "# Domain\nINV-001 fixed after the review\n")
	dirty, err := DirtyVerifiedDocs(root, id)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(dirty, []string{"final.md"}) {
		t.Fatalf("final.md's review read the old domain.md, so final.md needs a new round: got %v", dirty)
	}
	if got := CitedDocsChanged(root, id, "final.md"); !reflect.DeepEqual(got, []string{"domain.md"}) {
		t.Fatalf("the change is named: got %v", got)
	}

	// A delta round that read the new domain.md absorbs it.
	logRound(t, root, id, VerifyLogEntry{
		Iteration: 2, Doc: "final.md", Status: "continue",
		DocHash: ContentHash([]byte("# Build spec\n")),
		CitedHashes: map[string]string{
			"domain.md":    ContentHash([]byte("# Domain\nINV-001 fixed after the review\n")),
			"wireframe.md": ContentHash([]byte("# Wireframe\n")),
		},
	})
	if dirty, _ := DirtyVerifiedDocs(root, id); len(dirty) != 0 {
		t.Fatalf("the newer round read the current domain.md, got %v", dirty)
	}
}
