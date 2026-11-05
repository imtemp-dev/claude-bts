package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

// After /bts-sync, `bts doctor` warned that final.md changed after its
// last verification (rule 3) and that verification.md was not in the
// manifest (trial problem #84, Multica AGEN-70).  The protocol has sync's
// edits to final.md checked by `bts validate`, not a review round, and the
// stop hook already exempts the implement-side phases from rule 3; and a
// verify round's report is recorded in verify-log.jsonl through
// --from-verification, never in the manifest.
func TestDoctor_AfterSyncAgreesWithTheProtocol(t *testing.T) {
	root := t.TempDir()
	id := "r-084-sync"
	dir := filepath.Join(root, ".bts", "specs", "recipes", id)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	writeProjectFile(t, root, ".bts/specs/recipes/"+id+"/final.md", "verified")
	if err := state.SaveVerifySnapshot(root, id, filepath.Join(dir, "final.md")); err != nil {
		t.Fatal(err)
	}
	writeProjectFile(t, root, ".bts/specs/recipes/"+id+"/final.md", "synced with the code")

	for phase, wantWarning := range map[string]bool{"status": false, "sync": false, "complete": false, "draft": true} {
		if err := state.SaveRecipeState(root, &state.RecipeState{ID: id, Type: "blueprint", Phase: phase}); err != nil {
			t.Fatal(err)
		}
		got := len(checkDirtyVerifiedDocs(root, id)) > 0
		if got != wantWarning {
			t.Errorf("phase %s: rule-3 warning = %v, want %v", phase, got, wantWarning)
		}
	}

	writeProjectFile(t, root, ".bts/specs/recipes/"+id+"/verification.md", "the verify fork's report")
	manifest := &state.Manifest{Documents: map[string]state.DocumentEntry{}}
	warns := func() []string {
		var out []string
		for _, i := range checkManifestConsistency(dir, id, manifest) {
			if strings.Contains(i.message, "verification.md") {
				out = append(out, i.message)
			}
		}
		return out
	}
	if len(warns()) != 1 {
		t.Fatal("with no verify round recorded, an unexplained verification.md is still flagged")
	}
	writeProjectFile(t, root, ".bts/specs/recipes/"+id+"/verify-log.jsonl", `{"iteration":1,"doc":"final.md"}`+"\n")
	if got := warns(); len(got) != 0 {
		t.Fatalf("a verify round's report is accounted for by verify-log.jsonl: %v", got)
	}
}
