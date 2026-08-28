package engine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// TestScenarioLink records one `jig:scenario {id}` annotation found in
// a test source file. Phase 13 requires every test to tag which
// simulation scenario it exercises; simulate scenarios without a
// linked test surface as coverage gaps.
type TestScenarioLink struct {
	TestFile   string
	TestName   string
	ScenarioID string
	LineNumber int
}

// Three comment forms — cover the test languages jig recipes actually
// use today. `jig:scenario` is the canonical tag; spacing is flexible.
var (
	tagCommentSlashRe = regexp.MustCompile(`(?m)^\s*//\s*jig:scenario\s+(\S+)\s*$`)
	tagCommentHashRe  = regexp.MustCompile(`(?m)^\s*#\s*jig:scenario\s+(\S+)\s*$`)
	tagCommentBlockRe = regexp.MustCompile(`(?m)^\s*/\*\s*jig:scenario\s+(\S+)\s*\*/\s*$`)

	// Test name matchers — the comment tag typically sits on the line
	// directly above a `test(`/`it(`/`def test_*` declaration.
	testNameJSRe     = regexp.MustCompile(`(?m)^\s*(?:it|test)\s*\(\s*['"\x60]([^'"\x60]+)['"\x60]`)
	testNameGoRe     = regexp.MustCompile(`(?m)^\s*func\s+(Test\w+)`)
	testNamePyRe     = regexp.MustCompile(`(?m)^\s*def\s+(test_\w+)`)
	testNameSwiftRe  = regexp.MustCompile(`(?m)^\s*func\s+(test\w+)\s*\(`)
)

// ExtractTestScenarioLinks walks the test files listed in
// test-results.json (via its `test_files` array) and collects every
// `jig:scenario {id}` annotation it finds. The test name reported is a
// best-effort match on the language's test declaration that follows
// the tag; when none can be inferred, the returned TestName is empty.
func ExtractTestScenarioLinks(recipeDir string) ([]TestScenarioLink, error) {
	testResults, err := loadTestResultsForScenarios(recipeDir)
	if err != nil {
		return nil, err
	}
	if testResults == nil {
		return nil, nil
	}

	var links []TestScenarioLink
	// Sort the file list so the output order is stable (tests help
	// downstream determinism).
	files := append([]string(nil), testResults.TestFiles...)
	sort.Strings(files)
	for _, rel := range files {
		path := rel
		if !filepath.IsAbs(path) {
			// test_files entries are project-relative; resolve against
			// the recipe's project root (two levels up from recipeDir:
			// .jig/specs/recipes/{id}).
			path = filepath.Join(projectRootFromRecipeDir(recipeDir), rel)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		links = append(links, parseTestFileLinks(rel, string(data))...)
	}
	return links, nil
}

// parseTestFileLinks finds jig:scenario comments and pairs each with
// the next test declaration in the file. Supports JS/TS/Go/Python/Swift
// naming conventions; if no declaration follows within 5 lines, the
// TestName is left empty (the link still counts for coverage purposes).
func parseTestFileLinks(fileLabel, content string) []TestScenarioLink {
	var links []TestScenarioLink
	lines := strings.Split(content, "\n")

	seenAtLine := map[int]string{} // line number → scenario id

	// Collect every comment tag's line + id.
	for _, re := range []*regexp.Regexp{tagCommentSlashRe, tagCommentHashRe, tagCommentBlockRe} {
		for _, loc := range re.FindAllStringSubmatchIndex(content, -1) {
			if len(loc) < 4 {
				continue
			}
			idStart, idEnd := loc[2], loc[3]
			id := strings.TrimSpace(content[idStart:idEnd])
			lineNum := strings.Count(content[:loc[0]], "\n") + 1
			seenAtLine[lineNum] = id
		}
	}

	// For each tag line, look at the next 5 lines for a test name match.
	for lineNum, id := range seenAtLine {
		name := ""
		windowEnd := lineNum + 5
		if windowEnd > len(lines) {
			windowEnd = len(lines)
		}
		window := strings.Join(lines[lineNum:windowEnd], "\n")
		for _, nameRe := range []*regexp.Regexp{testNameJSRe, testNameGoRe, testNamePyRe, testNameSwiftRe} {
			if m := nameRe.FindStringSubmatch(window); len(m) >= 2 {
				name = m[1]
				break
			}
		}
		links = append(links, TestScenarioLink{
			TestFile:   fileLabel,
			TestName:   name,
			ScenarioID: id,
			LineNumber: lineNum,
		})
	}
	sort.Slice(links, func(i, j int) bool {
		if links[i].TestFile != links[j].TestFile {
			return links[i].TestFile < links[j].TestFile
		}
		return links[i].LineNumber < links[j].LineNumber
	})
	return links
}

// CheckTestScenarioCoverage reports three issue categories:
//
//   - scenario_unlinked — a scenario defined in simulations/*.md has no
//     jig:scenario tag referencing it anywhere in the project's tests.
//     Cross-boundary or illegal-cell scenarios with zero linked tests
//     are CRITICAL; single-axis scenarios are MAJOR.
//   - scenario_orphan — a test tags `jig:scenario X` but X does not
//     appear as a scenario in simulations/*.md. MAJOR.
//   - failure_category_missing — test-results.json has status=fail but
//     one or more failures lack the `category` field (Phase 13 requires
//     each failure to be classified as test/implementation/spec). MAJOR.
func CheckTestScenarioCoverage(recipeDir string) []Issue {
	links, err := ExtractTestScenarioLinks(recipeDir)
	if err != nil {
		return nil
	}
	scenarios := collectSimulationScenarioIDs(recipeDir)

	var issues []Issue

	// Orphan check: every test tag must map to a known scenario.
	known := map[string]bool{}
	for _, id := range scenarios.known {
		known[id] = true
	}
	for _, link := range links {
		if !known[link.ScenarioID] {
			issues = append(issues, Issue{
				Category: "test_scenario",
				Claim:    "scenario_orphan: " + link.ScenarioID,
				Severity: "major",
				Detail:   link.TestFile + ":" + itoa(link.LineNumber) + " tags `jig:scenario " + link.ScenarioID + "` but no matching scenario exists in simulations/*.md.",
			})
		}
	}

	// Unlinked check: every scenario must have at least one linked test
	// OR an entry in test-results.json `scenario_coverage` (the
	// migration path — authors who cannot re-run tests yet declare
	// coverage explicitly there).
	linkedIDs := map[string]bool{}
	for _, link := range links {
		linkedIDs[link.ScenarioID] = true
	}
	// Fold scenario_coverage keys into the linked set. An empty value
	// list is treated as "acknowledged but not yet covered" — the
	// legacy-migration sentinel `["legacy"]` falls through here too.
	if tr, _ := loadTestResultsForScenarios(recipeDir); tr != nil {
		for id := range tr.ScenarioCoverage {
			linkedIDs[id] = true
		}
	}
	for _, sc := range scenarios.all {
		if linkedIDs[sc.id] {
			continue
		}
		sev := "major"
		if sc.crossBoundary || sc.illegalCell {
			sev = "critical"
		}
		issues = append(issues, Issue{
			Category: "test_scenario",
			Claim:    "scenario_unlinked: " + sc.id,
			Severity: sev,
			Detail:   "simulations/" + sc.sourceFile + " declares scenario '" + sc.id + "' but no test carries a `jig:scenario " + sc.id + "` tag. (Add a test with the tag, or record an explicit scenario_coverage entry in test-results.json.)",
		})
	}

	// Failure classification check.
	if tr, _ := loadTestResultsForScenarios(recipeDir); tr != nil && tr.Status == "fail" {
		for i, f := range tr.Failures {
			if strings.TrimSpace(f.Category) == "" {
				issues = append(issues, Issue{
					Category: "test_scenario",
					Claim:    "failure_category_missing: failures[" + itoa(i) + "]",
					Severity: "major",
					Detail:   "test-results.json failures[" + itoa(i) + "] has no category. Phase 13 requires each failure to classify as 'test' | 'implementation' | 'spec'.",
				})
			}
		}
	}
	return issues
}

type simScenarioRef struct {
	id             string
	sourceFile     string
	crossBoundary  bool
	illegalCell    bool
}

type simScenarioSet struct {
	all   []simScenarioRef
	known []string
}

// CollectSimulationScenarioIDsForMigration exposes the scenario-id
// collector as a stable public helper so jig migrate test-scenarios
// can seed scenario_coverage with exactly the ids the checker counts.
// Returns a sorted slice for deterministic migration output.
func CollectSimulationScenarioIDsForMigration(recipeDir string) []string {
	set := collectSimulationScenarioIDs(recipeDir)
	sort.Strings(set.known)
	return set.known
}

// collectSimulationScenarioIDs parses simulations/*.md and returns
// every scenario header it finds along with flags for cross-boundary
// and illegal-cell tags. Uses the same regexes simulation_checker.go
// does so the two agree on what counts as a scenario.
func collectSimulationScenarioIDs(recipeDir string) simScenarioSet {
	set := simScenarioSet{}
	simsDir := filepath.Join(recipeDir, "simulations")
	entries, err := os.ReadDir(simsDir)
	if err != nil {
		return set
	}

	// Scenario-line recognition + id extraction live in
	// simulation_patterns.go so this function, simulation_checker.go,
	// and the migrate tool all agree on what "a scenario" is.
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(simsDir, e.Name()))
		if err != nil {
			continue
		}
		content := string(data)
		simBase := strings.TrimSuffix(e.Name(), ".md")
		autoCounter := 0

		for _, line := range strings.Split(content, "\n") {
			if !IsSimulationScenarioLine(line) {
				continue
			}
			id := ExtractScenarioID(line)
			if id == "" {
				autoCounter++
				id = "sim-" + simBase + ".s" + itoa(autoCounter)
			}
			cross := simCrossBoundaryRe.MatchString(line)
			illegal := simIllegalCellRe.MatchString(line)
			set.all = append(set.all, simScenarioRef{
				id: id, sourceFile: e.Name(),
				crossBoundary: cross, illegalCell: illegal,
			})
			set.known = append(set.known, id)
		}
	}
	return set
}

// ---- test-results.json helpers ---------------------------------------

type testResultsForScenarios struct {
	RecipeID          string                     `json:"recipe_id"`
	Status            string                     `json:"status"`
	TestFiles         []string                   `json:"test_files"`
	Failures          []testFailureForScenarios  `json:"failures"`
	ScenarioCoverage  map[string][]string        `json:"scenario_coverage,omitempty"`
}

type testFailureForScenarios struct {
	Test     string `json:"test"`
	Error    string `json:"error"`
	Category string `json:"category"`
}

func loadTestResultsForScenarios(recipeDir string) (*testResultsForScenarios, error) {
	path := filepath.Join(recipeDir, "test-results.json")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var tr testResultsForScenarios
	if err := json.Unmarshal(data, &tr); err != nil {
		return nil, err
	}
	return &tr, nil
}

// projectRootFromRecipeDir reverses RecipeDir: given
// "/root/.jig/specs/recipes/{id}" return "/root". Any other shape
// returns the input unchanged so callers fall back to recipe-local
// paths rather than walking up into filesystem surprises.
func projectRootFromRecipeDir(recipeDir string) string {
	expected := filepath.Join(".jig", "specs", "recipes")
	if idx := strings.Index(recipeDir, expected); idx > 0 {
		return recipeDir[:idx-1] // strip trailing separator
	}
	// Fallback — walk up three levels.
	return filepath.Clean(filepath.Join(recipeDir, "..", "..", "..", ".."))
}
