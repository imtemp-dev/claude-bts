package hook

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

// Feature #132, step 2: an express build recipe has no review round on
// final.md.  Its DONE takes the mechanical `bts verify` of final.md in
// place of build_review_clean, and the hints a session reads say what
// express asks for, not the full recipe's next step.

// setupExpressBuildRoot is setupBuildRoot made express.
func setupExpressBuildRoot(t *testing.T) (root, id string) {
	t.Helper()
	root, id = setupBuildRoot(t)
	r, err := state.LoadRecipeState(root, id)
	if err != nil {
		t.Fatal(err)
	}
	r.Mode = state.ModeExpress
	if err := state.SaveRecipeState(root, r); err != nil {
		t.Fatal(err)
	}
	return root, id
}

// A final.md that cites a file nobody wrote: `bts verify` reports it as a
// critical.
const finalCitingAMissingFile = "# Build spec\n\n## 3. Boundary contracts\nThe relay lives in `src/gone.py`.\n"
const finalCitingTheSkeleton = "# Build spec\n\n## 3. Boundary contracts\nThe relay lives in `src/relay.py`.\n"

func TestBuildSpecDone_ExpressTakesTheMechanicalVerifyInPlaceOfTheReview(t *testing.T) {
	root, id := setupExpressBuildRoot(t)
	recordEvidence(t, root, id, 0, "INV-001", "INV-002")

	writeFinal(t, root, id, finalCitingAMissingFile)
	out := buildDone(t, root)
	if out.Decision != "block" || !strings.Contains(out.Reason, "bts verify") || !strings.Contains(out.Reason, "src/gone.py") {
		t.Fatalf("a critical from bts verify must hold express DONE, naming it and the command; got %q %q", out.Decision, out.Reason)
	}
	if strings.Contains(out.Reason, "/bts-verify") || strings.Contains(out.Reason, "No review of final.md") {
		t.Errorf("express has no review round, so DONE must not ask for one; got %q", out.Reason)
	}

	writeFinal(t, root, id, finalCitingTheSkeleton)
	if out := buildDone(t, root); out.Decision == "block" {
		t.Fatalf("a clean bts verify must let express DONE through without a review round: %s", out.Reason)
	}
	if r, _ := state.LoadRecipeState(root, id); r.Phase != "finalize" {
		t.Fatalf("phase must be finalize, got %q", r.Phase)
	}
}

// The full recipe keeps its review round, and an express recipe moved to
// the full recipe takes it from then on.
func TestBuildSpecDone_FullAndUpgradedRecipesKeepTheReview(t *testing.T) {
	for _, mode := range []string{"", state.ModeFull} {
		root, id := setupBuildRoot(t)
		r, _ := state.LoadRecipeState(root, id)
		r.Mode = mode
		if err := state.SaveRecipeState(root, r); err != nil {
			t.Fatal(err)
		}
		recordEvidence(t, root, id, 0, "INV-001", "INV-002")
		writeFinal(t, root, id, finalCitingTheSkeleton)
		if out := buildDone(t, root); out.Decision != "block" || !strings.Contains(out.Reason, "No review of final.md") {
			t.Errorf("mode %q: DONE must still ask for the review round; got %q %q", mode, out.Decision, out.Reason)
		}
	}
}

func TestNextStepHint_ExpressBuildTailNamesTheMechanicalVerify(t *testing.T) {
	root, id := setupExpressBuildRoot(t)
	recordEvidence(t, root, id, 0, "INV-001", "INV-002")

	_ = os.Remove(filepath.Join(state.RecipeDir(root, id), "final.md"))
	h := hintFor(t, root, id)
	if !strings.Contains(h, "Write final.md") || !strings.Contains(h, "bts verify") || strings.Contains(h, "review it once") {
		t.Fatalf("no final.md in express: write it, then bts verify it; got %q", h)
	}

	writeFinal(t, root, id, finalCitingAMissingFile)
	h = hintFor(t, root, id)
	if !strings.Contains(h, "build_verify_clean") || !strings.Contains(h, "src/gone.py") {
		t.Fatalf("express with a finding in final.md: the hint must name the gate and the finding; got %q", h)
	}

	writeFinal(t, root, id, finalCitingTheSkeleton)
	if h := hintFor(t, root, id); !strings.Contains(h, "<bts>DONE</bts>") {
		t.Fatalf("every express gate holds: DONE is next; got %q", h)
	}
}

// A session that resumes an express recipe (a new session, or after a
// compaction) must hear that it is express, or it follows the full
// recipe's steps: the architect's runners and debate, the review round.
func TestNextStepHint_ResumingAnExpressRecipeSaysItIsExpress(t *testing.T) {
	root, id := setupExpressBuildRoot(t)
	r, _ := state.LoadRecipeState(root, id)
	r.Phase = "domain-model"
	if err := state.SaveRecipeState(root, r); err != nil {
		t.Fatal(err)
	}
	if h := hintFor(t, root, id); !strings.Contains(h, "/bts-recipe-build") || !strings.Contains(h, "express") {
		t.Fatalf("the resume hint must say the recipe is express; got %q", h)
	}

	r.Mode = ""
	if err := state.SaveRecipeState(root, r); err != nil {
		t.Fatal(err)
	}
	if h := hintFor(t, root, id); strings.Contains(h, "express") {
		t.Fatalf("a full recipe's hint must not mention express; got %q", h)
	}
}
