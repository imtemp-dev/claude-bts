package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

func TestLeaveUnverified_OnlyAStaleDocumentWithAReason(t *testing.T) {
	root, id := newTestRunFixture(t)
	final := filepath.Join(state.RecipeDir(root, id), "final.md")
	if err := os.WriteFile(final, []byte("as verified"), 0644); err != nil {
		t.Fatal(err)
	}
	hash, _, _ := state.FileContentHash(final)
	if err := state.AppendVerifyLog(root, id, &state.VerifyLogEntry{Iteration: 1, Doc: "final.md", Status: "converged",
		FullPass: true, DocHash: hash}); err != nil {
		t.Fatal(err)
	}

	if _, err := leaveUnverified(root, id, "final.md", "waiting", ""); err == nil || !strings.Contains(err.Error(), "has not changed") {
		t.Fatalf("final.md is verified as it is: want a refusal, got %v", err)
	}
	if err := os.WriteFile(final, []byte("edited"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := leaveUnverified(root, id, "final.md", "  ", ""); err == nil {
		t.Fatal("a record without a reason must be refused")
	}
	rec, err := leaveUnverified(root, id, ".bts/specs/recipes/"+id+"/final.md", "the falsifier table rewrites section 2", "s-9")
	if err != nil {
		t.Fatal(err)
	}
	if rec.Doc != "final.md" || rec.SessionID != "s-9" || rec.Fingerprint == "" {
		t.Fatalf("unexpected record: %+v", rec)
	}
	if held, open := state.HeldUnverified(root, id, []string{"final.md"}); len(held) != 1 || len(open) != 0 {
		t.Fatalf("the record must hold final.md: held=%v open=%v", held, open)
	}
}
