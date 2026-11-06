package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Trial problem #103: swift-eagle's confirmed scope item 14 measured that
// unrelated turns "before and inside the topic" stay out of the packet,
// INV-007 keeps a contiguous span, and the wireframe's eval rows covered
// only turns before the topic.  Nothing recorded the narrowing, and six
// review rounds and DONE passed it.  final.md now maps every In Scope item
// to what holds it, or to the decision that changed it.

const testScope = "# Scope: x\n\n## In Scope\n\n### Server\n\n" +
	"1. **The offer.**  One new tool.\n" +
	"   - a nested note, not an item\n" +
	"2. **Grounding.**  References resolve.\n\n" +
	"### Eval\n\n" +
	"3. Scenarios with unrelated turns before and inside the topic.\n\n" +
	"## Out of Scope\n\n- the work item track\n"

func TestParseScopeItems_TopLevelListItemsUnderInScope(t *testing.T) {
	items := ParseScopeItems(testScope)
	if len(items) != 3 || !strings.Contains(items[0], "The offer") || !strings.Contains(items[2], "Scenarios") {
		t.Fatalf("want the three In Scope items, got %q", items)
	}
	bullets := ParseScopeItems("## In Scope\n\n- one\n- two\n  - nested\n\n## Out of Scope\n- three\n")
	if len(bullets) != 2 {
		t.Fatalf("bullets count too, nested ones and Out of Scope do not: got %q", bullets)
	}
}

func scopeRecipe(t *testing.T, recipeType, final string) (finalPath string) {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, ".bts", "specs", "recipes", "r-103")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(dir, "recipe.json"), []byte(`{"id":"r-103","type":"`+recipeType+`","phase":"falsify"}`), 0644)
	_ = os.WriteFile(filepath.Join(dir, "scope.md"), []byte(testScope), 0644)
	finalPath = filepath.Join(dir, "final.md")
	_ = os.WriteFile(finalPath, []byte(final), 0644)
	return finalPath
}

func claims(issues []Issue) string {
	var out []string
	for _, is := range issues {
		out = append(out, is.Claim+" ["+is.Severity+"]")
	}
	return strings.Join(out, "; ")
}

func TestCheckScopeCoverage(t *testing.T) {
	coverage := "# Build: x\n\n## 6. Scope coverage\n\n| Scope item | Held by |\n| --- | --- |\n" +
		"| 1 | INV-001, INV-002 |\n" +
		"| 3 | eval rows; narrowed to turns before the topic by decision scope-3-before-only |\n"

	got := claims(CheckScopeCoverage(scopeRecipe(t, "build", coverage)))
	if !strings.Contains(got, "scope_item_uncovered: 2") || !strings.Contains(got, "[major]") {
		t.Errorf("item 2 has no row: want a major, got %s", got)
	}
	if !strings.Contains(got, "scope_decision_unresolved: scope-3-before-only") {
		t.Errorf("a row citing a decision needs it resolved: got %s", got)
	}

	finalPath := scopeRecipe(t, "build", coverage+"| 2 | INV-004 |\n")
	_ = os.WriteFile(filepath.Join(filepath.Dir(finalPath), "decisions.jsonl"), []byte(
		`{"key":"scope-3-before-only","status":"open","question":"Narrow item 3?"}`+"\n"+
			`{"key":"scope-3-before-only","status":"resolved","answer":"yes, before only"}`+"\n"), 0644)
	if got := claims(CheckScopeCoverage(finalPath)); got != "" {
		t.Errorf("every item mapped and the decision resolved: want nothing, got %s", got)
	}

	if got := claims(CheckScopeCoverage(scopeRecipe(t, "build", "# Build: x\n\n## 1. What ships\n\nA thing.\n"))); !strings.Contains(got, "scope_coverage_missing") {
		t.Errorf("no section at all is one finding, got %s", got)
	}
	if got := claims(CheckScopeCoverage(scopeRecipe(t, "blueprint", "# Final\n"))); got != "" {
		t.Errorf("only a build recipe's final.md carries the table, got %s", got)
	}
}
