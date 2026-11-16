package hook

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

// Trial problem #136: rule 3's block offered "tell the user the doc is left
// unverified", which the hook cannot see, and blocked again after the
// session did.  The exit it offers now is a record the hook reads.

func writeRecipeFile(t *testing.T, root, recipeID, name, content string) string {
	t.Helper()
	p := filepath.Join(state.RecipeDir(root, recipeID), name)
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	h, ok, err := state.FileContentHash(p)
	if err != nil || !ok {
		t.Fatalf("hash %s: %v", name, err)
	}
	return h
}

// A full pass over final.md that read domain.md, both as written now.
func setupVerifiedFinal(t *testing.T) (root, recipeID string) {
	t.Helper()
	root, recipeID = setupStopRoot(t)
	finalHash := writeRecipeFile(t, root, recipeID, "final.md", "as verified")
	domainHash := writeRecipeFile(t, root, recipeID, "domain.md", "domain v1")
	writeVerifyLog(t, root, recipeID, []state.VerifyLogEntry{
		{Iteration: 1, Doc: "final.md", Status: "converged", Budget: 3, FullPass: true,
			DocHash: finalHash, CitedHashes: map[string]string{"domain.md": domainHash},
			Timestamp: time.Now().UTC().Format(time.RFC3339)},
	})
	recordVerification(t, root, recipeID)
	return root, recipeID
}

func TestBlindStop_LeftUnverifiedLetsTheTurnEnd(t *testing.T) {
	root, recipeID := setupVerifiedFinal(t)
	writeRecipeFile(t, root, recipeID, "final.md", "edited for review findings")

	out := blindStop(t, root)
	if out.Decision != "block" {
		t.Fatal("final.md changed after its verification: rule 3 must block")
	}
	if !strings.Contains(out.Reason, "bts recipe leave-unverified "+recipeID+" --doc final.md") {
		t.Errorf("the block must offer the exit it accepts, got: %s", out.Reason)
	}
	if strings.Contains(out.Reason, "tell the user the doc is left unverified —") {
		t.Errorf("the block must not offer an exit it cannot see, got: %s", out.Reason)
	}

	if _, err := state.RecordLeftUnverified(root, recipeID, "final.md", "the falsifier table is about to rewrite section 2", "s-1"); err != nil {
		t.Fatal(err)
	}
	if out := blindStop(t, root); out.Decision == "block" {
		t.Fatalf("final.md is recorded as left unverified and unchanged since: the turn may end, got: %s", out.Reason)
	}

	// Another edit re-arms rule 3.
	writeRecipeFile(t, root, recipeID, "final.md", "edited again")
	if out := blindStop(t, root); out.Decision != "block" {
		t.Fatal("final.md changed after it was left unverified: rule 3 must block again")
	}

	// So does a round logged for it.
	if _, err := state.RecordLeftUnverified(root, recipeID, "final.md", "waiting for the table", "s-1"); err != nil {
		t.Fatal(err)
	}
	if out := blindStop(t, root); out.Decision == "block" {
		t.Fatalf("recorded again: the turn may end, got: %s", out.Reason)
	}
	state.ClearLeftUnverified(root, recipeID, "final.md")
	if out := blindStop(t, root); out.Decision != "block" {
		t.Fatal("a logged round ends the record: rule 3 must block while final.md is still stale")
	}
}

func TestBlindStop_LeftUnverifiedRearmsWhenACitedDocChanges(t *testing.T) {
	root, recipeID := setupVerifiedFinal(t)
	writeRecipeFile(t, root, recipeID, "domain.md", "domain v2")
	if out := blindStop(t, root); out.Decision != "block" {
		t.Fatal("domain.md changed after final.md's review read it: rule 3 must block")
	}
	if _, err := state.RecordLeftUnverified(root, recipeID, "final.md", "domain.md is still being revised", "s-1"); err != nil {
		t.Fatal(err)
	}
	if out := blindStop(t, root); out.Decision == "block" {
		t.Fatalf("left unverified with nothing changed since: the turn may end, got: %s", out.Reason)
	}
	writeRecipeFile(t, root, recipeID, "domain.md", "domain v3")
	if out := blindStop(t, root); out.Decision != "block" {
		t.Fatal("domain.md changed again after the record: rule 3 must block")
	}
}

// Trial problem #135, the hook's side: an unrecorded verification in a
// build recipe names final.md, the document its spec loop verifies, in the
// recovery command, not draft.md, which a build recipe never writes.
func TestBlindStop_UnloggedVerificationOnABuildRecipeNamesFinal(t *testing.T) {
	root, recipeID := setupStopRoot(t)
	recipe, err := state.LoadRecipeState(root, recipeID)
	if err != nil {
		t.Fatal(err)
	}
	recipe.Type = "build"
	if err := state.SaveRecipeState(root, recipe); err != nil {
		t.Fatal(err)
	}
	out := blindStop(t, root)
	if out.Decision != "block" {
		t.Fatal("verification.md with no verify-log must block")
	}
	if !strings.Contains(out.Reason, "--doc final.md") {
		t.Errorf("the recovery command must name final.md, got: %s", out.Reason)
	}
}

// Only the stale document a record holds is let through; another stale
// one still blocks, named alone.
func TestBlindStop_LeftUnverifiedHoldsOnlyItsDocument(t *testing.T) {
	root, recipeID := setupVerifiedFinal(t)
	wireHash := writeRecipeFile(t, root, recipeID, "wireframe.md", "wireframe as verified")
	entries, err := state.ReadVerifyLog(root, recipeID)
	if err != nil {
		t.Fatal(err)
	}
	entries = append(entries, state.VerifyLogEntry{Iteration: 1, Doc: "wireframe.md", Status: "converged", Budget: 3,
		FullPass: true, DocHash: wireHash, VerificationHash: entries[0].VerificationHash,
		Timestamp: time.Now().UTC().Format(time.RFC3339)})
	writeVerifyLog(t, root, recipeID, entries)

	writeRecipeFile(t, root, recipeID, "final.md", "edited")
	writeRecipeFile(t, root, recipeID, "wireframe.md", "wireframe edited")
	if _, err := state.RecordLeftUnverified(root, recipeID, "final.md", "waiting for the table", "s-1"); err != nil {
		t.Fatal(err)
	}
	out := blindStop(t, root)
	if out.Decision != "block" {
		t.Fatal("wireframe.md is stale and not left unverified: rule 3 must block")
	}
	if !strings.Contains(out.Reason, "wireframe.md changed") || strings.Contains(out.Reason, "final.md") {
		t.Errorf("the block names only the document no record holds, got: %s", out.Reason)
	}
}
