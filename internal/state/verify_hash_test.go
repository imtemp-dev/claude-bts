package state

import (
	"os"
	"path/filepath"
	"testing"
)

// worktreeRecipe builds a recipe directory holding ONLY tracked state —
// no .jig/local at all. This is exactly what `git worktree add` produces:
// .jig/specs/ comes across with the branch, .jig/local/ is gitignored and
// simply does not exist.
func worktreeRecipe(t *testing.T, docContent string) (root, recipeID string) {
	t.Helper()
	root = t.TempDir()
	recipeID = "r-001"
	dir := RecipeDir(root, recipeID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "draft.md"), []byte(docContent), 0644); err != nil {
		t.Fatal(err)
	}
	return root, recipeID
}

func logRound(t *testing.T, root, recipeID string, e VerifyLogEntry) {
	t.Helper()
	if err := AppendVerifyLog(root, recipeID, &e); err != nil {
		t.Fatal(err)
	}
}

// The regression this fix exists for: rule 3 was enforced by comparing
// against .jig/local/verify-snapshots/, which is gitignored. In a
// worktree that directory is absent, so DirtyVerifiedDocs returned nil
// and the gate was silently switched off — a doc edited after its
// verification passed every gate.
func TestDirtyVerifiedDocs_HashCatchesEditWithNoSnapshotDir(t *testing.T) {
	root, id := worktreeRecipe(t, "as verified\n")
	logRound(t, root, id, VerifyLogEntry{
		Iteration: 1, Doc: "draft.md", FullPass: true, Status: "converged",
		DocHash: ContentHash([]byte("as verified\n")),
	})

	if _, err := os.Stat(VerifySnapshotDir(root, id)); !os.IsNotExist(err) {
		t.Fatal("fixture must have no snapshot dir — that is the worktree case")
	}

	dirty, err := DirtyVerifiedDocs(root, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(dirty) != 0 {
		t.Fatalf("unmodified doc must be clean, got %v", dirty)
	}

	if err := os.WriteFile(filepath.Join(RecipeDir(root, id), "draft.md"),
		[]byte("edited after verify\n"), 0644); err != nil {
		t.Fatal(err)
	}
	dirty, err = DirtyVerifiedDocs(root, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(dirty) != 1 || dirty[0] != "draft.md" {
		t.Fatalf("rule 3 must hold without a snapshot dir, got %v", dirty)
	}
}

// A round logged before DocHash existed still has its snapshot. Dropping
// the fallback would switch the gate off for every in-flight recipe until
// its next full verification.
func TestDirtyVerifiedDocs_SnapshotFallbackForHashlessRound(t *testing.T) {
	root, id := worktreeRecipe(t, "edited after verify\n")
	logRound(t, root, id, VerifyLogEntry{
		Iteration: 1, Doc: "draft.md", FullPass: true, Status: "converged",
	}) // no DocHash: pre-upgrade entry

	snapDir := VerifySnapshotDir(root, id)
	if err := os.MkdirAll(snapDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(snapDir, "draft.md"), []byte("as verified\n"), 0644); err != nil {
		t.Fatal(err)
	}

	dirty, err := DirtyVerifiedDocs(root, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(dirty) != 1 || dirty[0] != "draft.md" {
		t.Fatalf("legacy snapshot must still be honoured, got %v", dirty)
	}
}

// The recorded hash is authoritative when both sources exist: a stale
// snapshot left over from before a re-verification must not resurrect a
// violation the log says is resolved. Without the precedence rule the
// same doc would also be reported twice.
func TestDirtyVerifiedDocs_HashWinsOverStaleSnapshot(t *testing.T) {
	root, id := worktreeRecipe(t, "revision 2\n")
	logRound(t, root, id, VerifyLogEntry{
		Iteration: 2, Doc: "draft.md", FullPass: true, Status: "converged",
		DocHash: ContentHash([]byte("revision 2\n")),
	})

	snapDir := VerifySnapshotDir(root, id)
	if err := os.MkdirAll(snapDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(snapDir, "draft.md"), []byte("revision 1\n"), 0644); err != nil {
		t.Fatal(err)
	}

	dirty, err := DirtyVerifiedDocs(root, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(dirty) != 0 {
		t.Fatalf("the log is authoritative over a stale snapshot, got %v", dirty)
	}
}

// Only full passes count as "this whole document was verified", matching
// what SaveVerifySnapshot records. A delta round verified a region, not
// the document.
func TestLastFullPassHashes_IgnoresDeltaAndHashlessRounds(t *testing.T) {
	hashes := LastFullPassHashes([]VerifyLogEntry{
		{Doc: "draft.md", FullPass: true, DocHash: "sha256:aaa"},
		{Doc: "draft.md", FullPass: false, DocHash: "sha256:bbb"},
		{Doc: "wireframe.md", FullPass: true},
		{Doc: "", FullPass: true, DocHash: "sha256:ccc"},
	})
	if len(hashes) != 1 || hashes["draft.md"] != "sha256:aaa" {
		t.Fatalf("expected only the full-pass hash for draft.md, got %v", hashes)
	}
}

func TestLastFullPassHashes_LatestFullPassWins(t *testing.T) {
	hashes := LastFullPassHashes([]VerifyLogEntry{
		{Doc: "draft.md", FullPass: true, DocHash: "sha256:old"},
		{Doc: "draft.md", FullPass: true, DocHash: "sha256:new"},
	})
	if hashes["draft.md"] != "sha256:new" {
		t.Fatalf("the latest full pass must win, got %v", hashes)
	}
}

// Whether a checkout materialises a file with LF or CRLF is a property of
// the checkout. Letting it change the hash would reintroduce the very
// cross-checkout false positive these hashes remove.
func TestContentHash_LineEndingsDoNotChangeIdentity(t *testing.T) {
	lf := ContentHash([]byte("# Title\n\nbody\n"))
	crlf := ContentHash([]byte("# Title\r\n\r\nbody\r\n"))
	if lf != crlf {
		t.Fatalf("line endings must not change document identity:\n  %s\n  %s", lf, crlf)
	}
	if lf == ContentHash([]byte("# Title\n\nbody changed\n")) {
		t.Fatal("different content must hash differently")
	}
}

func TestFileContentHash_MissingFileIsNotAnError(t *testing.T) {
	hash, ok, err := FileContentHash(filepath.Join(t.TempDir(), "nope.md"))
	if err != nil || ok || hash != "" {
		t.Fatalf("a missing file is a normal state: hash=%q ok=%v err=%v", hash, ok, err)
	}
}
