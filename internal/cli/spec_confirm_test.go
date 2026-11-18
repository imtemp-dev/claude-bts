package cli

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/review"
	"github.com/imtemp-dev/claude-bts/internal/state"
)

// specFixture is a recipe of the given type and phase holding its spec
// document, with the review kit deployed.
func specFixture(t *testing.T, id, typ, phase string) (root, specPath string) {
	t.Helper()
	root = editGoFixture(t, id, typ, phase, "")
	specPath = filepath.Join(state.RecipeDir(root, id), review.SpecDoc(typ))
	if err := os.WriteFile(specPath, []byte("# Spec\n\n## Components\n\n- One part.\n"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, k := range review.KitFiles {
		p := filepath.Join(root, k)
		_ = os.MkdirAll(filepath.Dir(p), 0755)
		_ = os.WriteFile(p, []byte("/* kit */"), 0644)
	}
	return root, specPath
}

// The user confirmed intent and scope, and then nothing put the document
// the code is built from in front of them.  Every recipe type that
// implements waits at the move into implement for a resolved spec-confirm;
// --force does not pass it, and the refusal names the spec document and
// the command that records the confirmation.
func TestSpecGate_HoldsImplementUntilTheUserConfirmsTheSpec(t *testing.T) {
	for _, typ := range []string{"blueprint", "build", "debug", "fix"} {
		t.Run(typ, func(t *testing.T) {
			id := "r-spec-" + typ
			root, _ := specFixture(t, id, typ, "finalize")
			doc := review.SpecDoc(typ)

			out := runRecipeLog(t, root, id, "--phase", "implement")
			if !strings.Contains(out, doc+" of "+id+" is not confirmed") ||
				!strings.Contains(out, "bts recipe confirm "+id+" spec") ||
				!strings.Contains(out, "/bts-review-page "+id+" spec") {
				t.Fatalf("the refusal must name %s, the review page and the confirm command, got:\n%s", doc, out)
			}
			if got := phaseOf(t, root, id); got != "finalize" {
				t.Fatalf("the phase must not move without the confirmation, now %q", got)
			}
			if _ = runRecipeLog(t, root, id, "--phase", "implement", "--force"); phaseOf(t, root, id) != "finalize" {
				t.Fatal("--force must not pass the user's confirmation")
			}
			// Skipping implement for a later implement phase is the same move.
			if _ = runRecipeLog(t, root, id, "--phase", "test"); phaseOf(t, root, id) != "finalize" {
				t.Fatal("entering a later implement phase must be held too")
			}

			// Held but not answered is still no confirmation.
			if _, err := state.HoldDecision(root, id, &state.DecisionEvent{Key: "spec-confirm", Question: "Confirm " + doc + " as written?"}); err != nil {
				t.Fatal(err)
			}
			if out := runRecipeLog(t, root, id, "--phase", "implement"); !strings.Contains(out, "the decision spec-confirm is open") {
				t.Fatalf("an open decision must be refused as open, got:\n%s", out)
			}

			if _, err := executeRecipeConfirm(root, id, "spec", "reads right"); err != nil {
				t.Fatal(err)
			}
			_ = runRecipeLog(t, root, id, "--phase", "implement")
			if got := phaseOf(t, root, id); got != "implement" {
				t.Fatalf("a confirmed spec must let implement start, phase %q", got)
			}
		})
	}
}

// Only the move into the implement phases waits, and only for the types
// that implement.
func TestSpecGate_HoldsOnlyTheMoveIntoImplement(t *testing.T) {
	root, _ := specFixture(t, "r-spec-moving", "blueprint", "implement")
	_ = runRecipeLog(t, root, "r-spec-moving", "--phase", "test")
	if got := phaseOf(t, root, "r-spec-moving"); got != "test" {
		t.Fatalf("a recipe already implementing is not held again, phase %q", got)
	}
	design, _ := specFixture(t, "r-spec-design", "design", "finalize")
	_ = runRecipeLog(t, design, "r-spec-design", "--phase", "implement")
	if got := phaseOf(t, design, "r-spec-design"); got != "implement" {
		t.Fatalf("a type with no spec to implement is not held, phase %q", got)
	}
}

// The spec's confirmation is a decision and a changelog line.  The spec
// itself is not touched: verify-log pins its exact content, so a status
// line written into it would read as an edit after the review.
func TestRecipeConfirm_SpecLeavesTheSpecUntouched(t *testing.T) {
	id := "r-spec-confirm"
	root, spec := specFixture(t, id, "build", "finalize")
	before, _ := os.ReadFile(spec)

	key, err := executeRecipeConfirm(root, id, "spec", "the parts read right")
	if err != nil || key != "spec-confirm" {
		t.Fatalf("key %q, err %v", key, err)
	}
	if after, _ := os.ReadFile(spec); string(after) != string(before) {
		t.Errorf("confirming must leave final.md as it was, got:\n%s", after)
	}
	// The page's confirm row names the document; that name works too, and
	// a revised spec confirmed again gets a new key.
	if key, err := executeRecipeConfirm(root, id, "final", "the revision reads right"); err != nil || key != "spec-confirm-2" {
		t.Fatalf("a second confirmation gets its own key, got %q, err %v", key, err)
	}
	if _, err := executeRecipeConfirm(root, id, "fix-spec", "yes"); err == nil || !strings.Contains(err.Error(), "is final.md") {
		t.Errorf("a build recipe's spec is final.md; fix-spec must be refused, got %v", err)
	}

	fix := "r-spec-confirm-fix"
	froot, _ := specFixture(t, fix, "fix", "draft")
	if _, err := executeRecipeConfirm(froot, fix, "final", "yes"); err == nil || !strings.Contains(err.Error(), "is fix-spec.md") {
		t.Errorf("a fix recipe's spec is fix-spec.md; final must be refused, got %v", err)
	}
	if key, err := executeRecipeConfirm(froot, fix, "fix-spec", "yes"); err != nil || key != "spec-confirm" {
		t.Errorf("fix-spec confirms a fix recipe's spec, got %q, err %v", key, err)
	}
}

// The spec point draws on the page the earlier points built: intent and
// scope sections stay when their documents exist, the spec's shape follows
// the recipe, and the only confirm row is the spec's.  A confirmation on
// that row is checked against the spec's fingerprint like any other.
func TestReviewPage_SpecPoint(t *testing.T) {
	id := "r-spec-page"
	root, _ := specFixture(t, id, "build", "finalize")
	dir := state.RecipeDir(root, id)
	_ = os.WriteFile(filepath.Join(dir, "intent.md"), []byte("# Intent\n\n### Status: CONFIRMED\n\nWhy.\n"), 0644)
	_ = os.WriteFile(filepath.Join(dir, "scope.md"), []byte("# Scope\n\n### Status: CONFIRMED\n\n## In Scope\n\n1. One.\n"), 0644)

	b, err := review.PageBrief(root, id, "spec")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(b.Config["confirm"], []string{"final"}) {
		t.Errorf("the spec page confirms final.md only, got %v", b.Config["confirm"])
	}
	all := strings.Join(b.Sections, "\n")
	for _, want := range []string{"The main scenario", "Lanes:", "Each invariant pinned to its owner", "Boundary contracts"} {
		if !strings.Contains(all, want) {
			t.Errorf("the spec page keeps the intent and scope sections and adds the spec's; %q missing from:\n%s", want, all)
		}
	}
	if last := b.Sections[len(b.Sections)-1]; last != "A confirm row for final.md" {
		t.Errorf("the last section is the spec's confirm row, got %q", last)
	}
	var hash string
	for _, d := range b.Docs {
		if d.File == "final.md" {
			hash = d.Hash
		}
	}
	if hash == "" {
		t.Fatalf("the brief carries final.md's version, got %+v", b.Docs)
	}
	if _, err := reviewConfirmAnswer(root, id, "final.md", "user", "000000000000", "yes"); err == nil {
		t.Error("a fingerprint that is not final.md's is refused")
	}
	answer, err := reviewConfirmAnswer(root, id, "final.md", "user", hash, "reads right")
	if err != nil || !strings.Contains(answer, "final.md v1 ("+hash+")") {
		t.Fatalf("answer %q, err %v", answer, err)
	}

	// A fix recipe has no intent or scope: its page starts at the spec,
	// drawn as a failing path.
	fix := "r-spec-page-fix"
	froot, _ := specFixture(t, fix, "fix", "draft")
	fb, err := review.PageBrief(froot, fix, "spec")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(fb.Sections[0], "The failing path today") {
		t.Errorf("a fix recipe's page starts with the failing path, got %q", fb.Sections[0])
	}
	if fb.Sections[len(fb.Sections)-1] != "A confirm row for fix-spec.md" {
		t.Errorf("a fix recipe confirms fix-spec.md, got %q", fb.Sections[len(fb.Sections)-1])
	}

	// An express build draws its trimmed spec in one picture.
	ex := "r-spec-page-express"
	eroot, _ := specFixture(t, ex, "build", "finalize")
	r, _ := state.LoadRecipeState(eroot, ex)
	r.Mode = state.ModeExpress
	if err := state.SaveRecipeState(eroot, r); err != nil {
		t.Fatal(err)
	}
	eb, err := review.PageBrief(eroot, ex, "spec")
	if err != nil {
		t.Fatal(err)
	}
	if len(eb.Sections) != 3 || !strings.HasPrefix(eb.Sections[0], "The parts and files this change touches") {
		t.Errorf("an express spec page has one drawing, its uncertainties and the confirm row, got %q", eb.Sections)
	}

	// No spec yet, no spec page.
	early := "r-spec-page-early"
	eroot2 := editGoFixture(t, early, "blueprint", "draft", "")
	if _, err := review.PageBrief(eroot2, early, "spec"); err == nil || !strings.Contains(err.Error(), "final.md does not exist yet") {
		t.Errorf("a spec page before final.md exists must be refused, got %v", err)
	}
}
