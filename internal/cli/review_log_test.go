package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

const provenReview = "# Code Review: auth\n\n" +
	`<bts-review-findings>{"findings":[{"id":"CRT-001","severity":"critical","perspective":"quality",` +
	`"title":"token never expires","evidence":{"kind":"run","command":"go test ./auth -run TestExpiry",` +
	`"observed":"--- FAIL: TestExpiry","required":"PASS"}}]}</bts-review-findings>` + "\n"

const unprovenReview = "# Code Review: auth\n\n" +
	`<bts-review-findings>{"findings":[{"id":"CRT-001","severity":"critical","perspective":"quality",` +
	`"title":"token never expires"}]}</bts-review-findings>` + "\n"

func writeReview(t *testing.T, root, recipeID, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(state.RecipeDir(root, recipeID), "review.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func reviewEntries(t *testing.T, root, recipeID string) int {
	t.Helper()
	entries, _ := state.ReadChangelog(root, recipeID)
	n := 0
	for _, e := range entries {
		if e.Action == "review" {
			n++
		}
	}
	return n
}

// review_criticals_proven, at the step that records the review: a critical
// with no evidence is refused and nothing is logged.
func TestRecipeLogReview_RefusesUnprovenCritical(t *testing.T) {
	root := newRecipeFixture(t, "r-rv1", "review", 0, 0, nil)
	writeReview(t, root, "r-rv1", unprovenReview)

	out := runRecipeLog(t, root, "r-rv1", "--action", "review", "--output", "review.md", "--result", "1 critical")
	if !strings.Contains(out, "CRT-001") || !strings.Contains(out, "downgrade") {
		t.Fatalf("want a refusal naming CRT-001 and the way out, got: %s", out)
	}
	if n := reviewEntries(t, root, "r-rv1"); n != 0 {
		t.Fatalf("a refused review must not be logged, got %d entries", n)
	}
}

func TestRecipeLogReview_RefusesMissingBlock(t *testing.T) {
	root := newRecipeFixture(t, "r-rv2", "review", 0, 0, nil)
	writeReview(t, root, "r-rv2", "# Code Review: auth\n\nprose only\n")

	out := runRecipeLog(t, root, "r-rv2", "--action", "review", "--output", "review.md")
	if !strings.Contains(out, "<bts-review-findings>") {
		t.Fatalf("want a refusal naming the missing block, got: %s", out)
	}
	if n := reviewEntries(t, root, "r-rv2"); n != 0 {
		t.Fatalf("a refused review must not be logged, got %d entries", n)
	}
}

func TestRecipeLogReview_AcceptsProvenCritical(t *testing.T) {
	root := newRecipeFixture(t, "r-rv3", "review", 0, 0, nil)
	writeReview(t, root, "r-rv3", provenReview)

	out := runRecipeLog(t, root, "r-rv3", "--action", "review", "--output", "review.md", "--result", "1 critical")
	if n := reviewEntries(t, root, "r-rv3"); n != 1 {
		t.Fatalf("a proven review must be logged once, got %d (output: %s)", n, out)
	}
}

func TestCheckReviewFindingsBlockDoctor(t *testing.T) {
	root := newRecipeFixture(t, "r-rv4", "review", 0, 0, nil)
	if issues := checkReviewFindingsBlock(root, "r-rv4"); len(issues) != 0 {
		t.Fatalf("no review.md must be clean: %v", issues)
	}

	writeReview(t, root, "r-rv4", "# Code Review\n\nlegacy prose\n")
	if issues := checkReviewFindingsBlock(root, "r-rv4"); len(issues) != 1 || issues[0].level != "warning" {
		t.Fatalf("a legacy review must warn, got %v", issues)
	}

	writeReview(t, root, "r-rv4", unprovenReview)
	if issues := checkReviewFindingsBlock(root, "r-rv4"); len(issues) != 1 || issues[0].level != "error" {
		t.Fatalf("an unproven critical must be an error, got %v", issues)
	}

	writeReview(t, root, "r-rv4", provenReview)
	if issues := checkReviewFindingsBlock(root, "r-rv4"); len(issues) != 0 {
		t.Fatalf("a proven review must be clean: %v", issues)
	}
}
