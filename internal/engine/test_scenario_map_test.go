package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupScenarioFixture(t *testing.T, sims, testFiles map[string]string, resultsJSON string) string {
	t.Helper()
	projectRoot := t.TempDir()
	recipeDir := filepath.Join(projectRoot, ".jig", "specs", "recipes", "r-001")
	if err := os.MkdirAll(filepath.Join(recipeDir, "simulations"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for name, body := range sims {
		_ = os.WriteFile(filepath.Join(recipeDir, "simulations", name), []byte(body), 0644)
	}
	for relPath, body := range testFiles {
		full := filepath.Join(projectRoot, relPath)
		_ = os.MkdirAll(filepath.Dir(full), 0755)
		_ = os.WriteFile(full, []byte(body), 0644)
	}
	if resultsJSON != "" {
		_ = os.WriteFile(filepath.Join(recipeDir, "test-results.json"), []byte(resultsJSON), 0644)
	}
	return recipeDir
}

// Happy path: each simulate scenario has a matching jig:scenario tag.
func TestCheckTestScenarioCoverage_AllLinked(t *testing.T) {
	sims := map[string]string{
		"001.md": `## Scenario sim-001.s1 [single-axis: A]
## Scenario sim-001.s2 [cross-boundary: axes=A,B]
`,
	}
	testFiles := map[string]string{
		"src/__tests__/a.test.ts": `// jig:scenario sim-001.s1
it("covers sim-001.s1", () => {});

// jig:scenario sim-001.s2
it("covers sim-001.s2", () => {});
`,
	}
	resultsJSON := `{"recipe_id":"r-001","status":"pass","test_files":["src/__tests__/a.test.ts"],"failures":[]}`
	recipeDir := setupScenarioFixture(t, sims, testFiles, resultsJSON)
	issues := CheckTestScenarioCoverage(recipeDir)
	if len(issues) != 0 {
		t.Fatalf("expected 0 issues, got %v", issues)
	}
}

// Unlinked cross-boundary scenario → critical; unlinked single-axis → major.
func TestCheckTestScenarioCoverage_UnlinkedSeverity(t *testing.T) {
	sims := map[string]string{
		"001.md": `## Scenario sim-001.s1 [single-axis: A]
## Scenario sim-001.s2 [cross-boundary: axes=A,B]
`,
	}
	resultsJSON := `{"recipe_id":"r-001","status":"pass","test_files":[],"failures":[]}`
	recipeDir := setupScenarioFixture(t, sims, map[string]string{}, resultsJSON)
	issues := CheckTestScenarioCoverage(recipeDir)
	if len(issues) != 2 {
		t.Fatalf("want 2 unlinked issues, got %d: %v", len(issues), issues)
	}
	var sawCritical, sawMajor bool
	for _, i := range issues {
		if i.Severity == "critical" && strings.Contains(i.Claim, "sim-001.s2") {
			sawCritical = true
		}
		if i.Severity == "major" && strings.Contains(i.Claim, "sim-001.s1") {
			sawMajor = true
		}
	}
	if !sawCritical || !sawMajor {
		t.Errorf("severity split wrong: %v", issues)
	}
}

// Orphan tag: test refers to a scenario id that doesn't exist.
func TestCheckTestScenarioCoverage_OrphanTag(t *testing.T) {
	sims := map[string]string{
		"001.md": `## Scenario sim-001.s1 [single-axis: A]`,
	}
	testFiles := map[string]string{
		"src/__tests__/a.test.ts": `// jig:scenario sim-001.s1
it("ok", () => {});

// jig:scenario sim-001.ghost
it("orphan", () => {});
`,
	}
	resultsJSON := `{"recipe_id":"r-001","status":"pass","test_files":["src/__tests__/a.test.ts"],"failures":[]}`
	recipeDir := setupScenarioFixture(t, sims, testFiles, resultsJSON)
	issues := CheckTestScenarioCoverage(recipeDir)
	var orphan *Issue
	for i := range issues {
		if strings.Contains(issues[i].Claim, "scenario_orphan") {
			orphan = &issues[i]
		}
	}
	if orphan == nil {
		t.Fatalf("expected orphan, got %v", issues)
	}
	if !strings.Contains(orphan.Claim, "sim-001.ghost") {
		t.Errorf("wrong id cited: %s", orphan.Claim)
	}
}

// Failure category gate: fail status + missing category → major.
func TestCheckTestScenarioCoverage_FailureCategoryMissing(t *testing.T) {
	sims := map[string]string{
		"001.md": `## Scenario sim-001.s1 [single-axis: A]`,
	}
	testFiles := map[string]string{
		"src/__tests__/a.test.ts": `// jig:scenario sim-001.s1
it("x", () => {});
`,
	}
	// failures[0] has no `category` field.
	resultsJSON := `{"recipe_id":"r-001","status":"fail","test_files":["src/__tests__/a.test.ts"],"failures":[{"test":"x","error":"nope"}]}`
	recipeDir := setupScenarioFixture(t, sims, testFiles, resultsJSON)
	issues := CheckTestScenarioCoverage(recipeDir)
	var cat *Issue
	for i := range issues {
		if strings.Contains(issues[i].Claim, "failure_category_missing") {
			cat = &issues[i]
		}
	}
	if cat == nil {
		t.Fatalf("expected failure_category_missing, got %v", issues)
	}
}

// Python test file with hash-comment tag.
func TestExtractTestScenarioLinks_HashComment(t *testing.T) {
	testFiles := map[string]string{
		"tests/test_a.py": `# jig:scenario sim-001.s1
def test_something():
    assert True
`,
	}
	resultsJSON := `{"recipe_id":"r-001","status":"pass","test_files":["tests/test_a.py"],"failures":[]}`
	recipeDir := setupScenarioFixture(t, map[string]string{}, testFiles, resultsJSON)
	links, err := ExtractTestScenarioLinks(recipeDir)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(links) != 1 {
		t.Fatalf("want 1 link, got %d", len(links))
	}
	if links[0].ScenarioID != "sim-001.s1" {
		t.Errorf("wrong id: %s", links[0].ScenarioID)
	}
	if links[0].TestName != "test_something" {
		t.Errorf("want test_something, got %s", links[0].TestName)
	}
}

func TestCheckTestScenarioCoverage_MissingResultsIsNil(t *testing.T) {
	recipeDir := t.TempDir()
	issues := CheckTestScenarioCoverage(recipeDir)
	if len(issues) != 0 {
		t.Errorf("missing test-results should not surface issues, got %v", issues)
	}
}
