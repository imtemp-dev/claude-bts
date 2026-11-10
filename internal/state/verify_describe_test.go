package state

import (
	"os"
	"path/filepath"
	"testing"
)

// Trial problem #122: DescribeDirty gave "final.md (domain.md changed
// after its review read it)", and every message went on "changed after
// its last verification", which said final.md changed when only domain.md
// had (cool-crow, r-005, 11-07 21:27Z).  It now gives the whole clause.
func TestDescribeDirty_SaysWhatChanged(t *testing.T) {
	root := t.TempDir()
	id := "r-122"
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
	write("domain.md", "# Domain\n")
	write("scope.md", "# Scope\n")
	logRound(t, root, id, VerifyLogEntry{
		Iteration: 1, Doc: "final.md", FullPass: true, Status: "continue",
		DocHash:     ContentHash([]byte("# Build spec\n")),
		CitedHashes: map[string]string{"domain.md": ContentHash([]byte("# Domain\n"))},
	})
	logRound(t, root, id, VerifyLogEntry{
		Iteration: 1, Doc: "scope.md", FullPass: true, Status: "continue",
		DocHash: ContentHash([]byte("# Scope\n")),
	})

	write("domain.md", "# Domain\nfixed after the review\n")
	if got, want := DescribeDirty(root, id, []string{"final.md"}),
		"final.md's review is out of date: domain.md changed after the review read it"; got != want {
		t.Errorf("only a cited document changed:\n got %q\nwant %q", got, want)
	}

	write("final.md", "# Build spec\nedited\n")
	write("scope.md", "# Scope\nedited\n")
	if got, want := DescribeDirty(root, id, []string{"final.md", "scope.md"}),
		"final.md, scope.md changed after their last verification"; got != want {
		t.Errorf("the documents themselves changed:\n got %q\nwant %q", got, want)
	}
	if got, want := DescribeDirty(root, id, []string{"scope.md"}),
		"scope.md changed after its last verification"; got != want {
		t.Errorf("one document changed:\n got %q\nwant %q", got, want)
	}
}
