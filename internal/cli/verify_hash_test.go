package cli

import (
	"path/filepath"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

// The gates are only as good as what gets written. `bts recipe log` is
// the sole writer of verify-log entries, so if it does not stamp the
// hashes, both rule-3 gates fall back to their legacy behaviour forever
// and the fix ships inert.
func TestRecipeLog_StampsContentHashes(t *testing.T) {
	root := newRecipeFixture(t, "r-h01", "verify", 0, 0, nil)
	writeProjectFile(t, root, ".bts/specs/recipes/r-h01/draft.md", "draft body v1\n")
	writeProjectFile(t, root, ".bts/specs/recipes/r-h01/verification.md",
		"# Verification\nround 1\n")

	runRecipeLog(t, root, "r-h01", "--iteration", "1", "--critical", "0",
		"--doc", filepath.Join(".bts", "specs", "recipes", "r-h01", "draft.md"),
		"--scope", "full")

	entries, err := state.ReadVerifyLog(root, "r-h01")
	if err != nil || len(entries) != 1 {
		t.Fatalf("read verify-log: %d entries, err=%v", len(entries), err)
	}
	got := entries[0]
	if want := state.ContentHash([]byte("draft body v1\n")); got.DocHash != want {
		t.Errorf("doc_hash: got %q want %q", got.DocHash, want)
	}
	if want := state.ContentHash([]byte("# Verification\nround 1\n")); got.VerificationHash != want {
		t.Errorf("verification_hash: got %q want %q", got.VerificationHash, want)
	}

	// And the stamped hash is what the rule-3 gate reads back.
	if dirty, derr := state.DirtyVerifiedDocs(root, "r-h01"); derr != nil || len(dirty) != 0 {
		t.Fatalf("just-verified doc must be clean: dirty=%v err=%v", dirty, derr)
	}
	writeProjectFile(t, root, ".bts/specs/recipes/r-h01/draft.md", "draft body v2\n")
	dirty, derr := state.DirtyVerifiedDocs(root, "r-h01")
	if derr != nil || len(dirty) != 1 || dirty[0] != "draft.md" {
		t.Fatalf("edited doc must be dirty: dirty=%v err=%v", dirty, derr)
	}
}

// A round logged with no --doc still records which verification.md was
// accounted for; otherwise the unrecorded-verification gate would have
// nothing to compare against after an unscoped round.
func TestRecipeLog_StampsVerificationHashWithoutDoc(t *testing.T) {
	root := newRecipeFixture(t, "r-h02", "verify", 0, 0, nil)
	writeProjectFile(t, root, ".bts/specs/recipes/r-h02/verification.md", "# V\n")

	runRecipeLog(t, root, "r-h02", "--iteration", "1", "--critical", "1")

	entries, err := state.ReadVerifyLog(root, "r-h02")
	if err != nil || len(entries) != 1 {
		t.Fatalf("read verify-log: %d entries, err=%v", len(entries), err)
	}
	if entries[0].VerificationHash != state.ContentHash([]byte("# V\n")) {
		t.Errorf("verification_hash not stamped: %q", entries[0].VerificationHash)
	}
	if entries[0].DocHash != "" {
		t.Errorf("no --doc means no doc hash, got %q", entries[0].DocHash)
	}
}
