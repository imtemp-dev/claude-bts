package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/review"
	"github.com/imtemp-dev/claude-bts/internal/state"
)

// A confirmation made on the review page records the version and
// fingerprint the user pressed, and only that version.
func TestReviewConfirm_RecordsTheFingerprintedVersion(t *testing.T) {
	root := newRecipeFixture(t, "r-rv1", "scoping", 0, 0, nil)
	dir := state.RecipeDir(root, "r-rv1")
	scope := filepath.Join(dir, "scope.md")
	if err := os.WriteFile(scope, []byte("# Scope\n\n### Status: DRAFT\n\n## In Scope\n\n1. One.\n"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, k := range review.KitFiles {
		p := filepath.Join(root, k)
		_ = os.MkdirAll(filepath.Dir(p), 0755)
		_ = os.WriteFile(p, []byte("/* kit */"), 0644)
	}
	b, err := review.PageBrief(root, "r-rv1", "scope")
	if err != nil {
		t.Fatal(err)
	}
	hash := b.Docs[0].Hash

	if _, err := reviewConfirmAnswer(root, "r-rv1", "scope", "user", "000000000000", "yes"); err == nil {
		t.Error("a fingerprint that is not the file's is refused")
	}
	if _, err := reviewConfirmAnswer(root, "r-rv1", "scope", "manager", hash, "yes"); err == nil {
		t.Error("--via review is the user's own confirmation; --by manager does not combine")
	}
	if _, err := reviewConfirmAnswer(root, "r-rv1", "scope", "user", hash, " "); err == nil {
		t.Error("the user's words are still required")
	}
	answer, err := reviewConfirmAnswer(root, "r-rv1", "scope", "user", hash, "reads right")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(answer, "scope.md v1 ("+hash+")") || !strings.HasSuffix(answer, "reads right") {
		t.Errorf("the answer names the version and fingerprint confirmed, got %q", answer)
	}
	if _, err := executeRecipeConfirm(root, "r-rv1", "scope", answer); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(scope)
	if state.DocStatus(string(data)) != "CONFIRMED" {
		t.Errorf("scope.md reads CONFIRMED, got:\n%s", data)
	}
	if after, _ := review.FileFingerprint(root, "r-rv1", "scope.md"); after != hash {
		t.Errorf("confirming must not change the fingerprint: %s → %s", hash, after)
	}
}
