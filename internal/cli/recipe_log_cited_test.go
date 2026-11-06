package cli

import (
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

// A build recipe's final.md review reads domain.md and the wireframe too,
// so its round records their hashes (trial problem #106); a review of any
// other document, or in another recipe type, records none.
func TestRecipeLog_BuildReviewOfFinalRecordsTheCitedDocuments(t *testing.T) {
	root := newRecipeFixture(t, "r-c106", "falsify", 0, 0, nil)
	r, err := state.LoadRecipeState(root, "r-c106")
	if err != nil {
		t.Fatal(err)
	}
	r.Type = "build"
	if err := state.SaveRecipeState(root, r); err != nil {
		t.Fatal(err)
	}
	writeRecipeFile(t, root, "r-c106", "final.md", "# Build spec\n")
	writeRecipeFile(t, root, "r-c106", "domain.md", "# Domain\n")
	writeRecipeFile(t, root, "r-c106", "wireframe.md", "# Wireframe\n")
	v := writeRecipeFile(t, root, "r-c106", "verification.md", mergeTestVerify)

	runRecipeLog(t, root, "r-c106", "--from-verification", v, "--doc", "final.md",
		"--scope", "full", "--dimension", "verify")

	entries, err := state.ReadVerifyLog(root, "r-c106")
	if err != nil || len(entries) != 1 {
		t.Fatalf("want one round, got %d (%v)", len(entries), err)
	}
	got := entries[0].CitedHashes
	if got["domain.md"] != state.ContentHash([]byte("# Domain\n")) ||
		got["wireframe.md"] != state.ContentHash([]byte("# Wireframe\n")) {
		t.Fatalf("the round must record what it read besides final.md, got %v", got)
	}
}
