package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Default cross-boundary threshold when no settings override exists.
// Phase 6.1 picks 30% as a heuristic balance between "enough
// cross-axis coverage to expose interaction bugs" and "not so many
// scenarios that authoring becomes busywork".
const DefaultCrossBoundaryRatio = 0.30

// Tag-recognition regexes. Scenario-line recognition itself now lives
// in simulation_patterns.go (single source shared with test_scenario_map).
var (
	simCrossBoundaryRe = regexp.MustCompile(`(?i)\[cross-boundary(?::\s*axes\s*=\s*[^\]]+)?\]`)
	simSingleAxisRe    = regexp.MustCompile(`(?i)\[single-axis(?::\s*([^\]]+))?\]`)
	simIllegalCellRe   = regexp.MustCompile(`(?i)\[illegal-cell(?::\s*[^\]]+)?\]`)
)

// SimulationStats counts scenario tags in a simulation file. Legacy
// scenarios (marked by `jig migrate simulations`) do not count toward
// the ratio denominator — they represent coverage that was never
// cross-axis classified in the first place.
type SimulationStats struct {
	Total         int
	CrossBoundary int
	SingleAxis    int
	IllegalCell   int
	Legacy        int
	Untagged      int
}

// CheckSimulationScenarios enforces the Phase 6.1 / 6.2 requirements on a
// simulation file:
//   - At least `ratio` of tagged scenarios carry [cross-boundary: ...].
//   - Every simulation has at least one scenario (the existing
//     simulate.min_scenarios rule is authored in skills, not here).
//   - Untagged scenarios are flagged as major — authors must pick one.
//
// Illegal-cell coverage is validated against domain.md by the separate
// CheckIllegalCellCoverage helper, which takes the recipe directory.
func CheckSimulationScenarios(simPath string, ratio float64) []Issue {
	data, err := os.ReadFile(simPath)
	if err != nil {
		return nil
	}
	content := string(data)
	stats := countSimulationTags(content)

	var issues []Issue
	fileName := filepath.Base(simPath)

	if stats.Total == 0 {
		// The skill's author-time rule handles "no scenarios" separately;
		// we stay silent when there are zero headers to avoid duplicating
		// that finding.
		//
		// Exception (Phase 19 P19): if the file contains a markdown
		// table (alignment row `| --- | ---`) but IsSimulationScenarioLine
		// matched zero rows, the author likely tried Form C but
		// structured it incorrectly — emit a hint so they can self-correct.
		if strings.Contains(content, "| ---") {
			return []Issue{{
				Category: "simulation",
				Claim:    "no_scenarios_detected: " + fileName,
				Severity: "major",
				Detail:   "Simulation contains a markdown table but no row matched the Scenario Index form `| S\\d+ | ... |`. Ensure the first cell is an id like 'S01' or 'sim-foo'. See jig-simulate SKILL.md Step 3.5 Form C.",
			}}
		}
		return nil
	}
	if stats.Untagged > 0 {
		issues = append(issues, Issue{
			Category: "simulation",
			Claim:    "untagged_scenarios: " + fileName,
			Severity: "major",
			Detail:   "Simulation contains scenarios without [cross-boundary: ...], [single-axis: ...], or [illegal-cell: ...] tags. Per jig-simulate SKILL.md Step 3, every scenario header must carry exactly one tag so cross-boundary ratio can be measured.",
		})
	}

	// Ratio denominator excludes legacy-tagged scenarios: migrate-authored
	// tags represent "never classified", not "single-axis on purpose".
	// Including them would force every legacy recipe to re-simulate to
	// clear validate, which is stricter than the phase intends.
	tagged := stats.CrossBoundary + stats.SingleAxis + stats.IllegalCell
	if tagged == 0 {
		return issues
	}
	coverage := float64(stats.CrossBoundary+stats.IllegalCell) / float64(tagged)
	if coverage < ratio {
		issues = append(issues, Issue{
			Category: "simulation",
			Claim:    "insufficient_cross_boundary_coverage: " + fileName,
			Severity: "critical",
			Detail:   "Cross-boundary + illegal-cell scenarios are " + pct(coverage) + " of non-legacy tagged scenarios; threshold is " + pct(ratio) + ". Add scenarios that touch state axes from 2+ modules simultaneously (per jig-simulate SKILL.md Step 3 / Phase 6.1).",
		})
	}
	return issues
}

// CheckIllegalCellCoverage compares the ILLEGAL cells declared in
// domain.md § 4 with the `[illegal-cell: <label>]` tags in the
// simulation file(s) of a recipe. Returns one critical issue per cell
// that has no corresponding scenario.
//
// Parsing is line-scan based: we look for the H2 "## 4" heading, then
// collect any table row whose cells contain the literal "ILLEGAL".
// The cell label is derived from surrounding context — since authors
// format the table freely, we use a looser match and let the author
// include the same label in their scenario tag.
func CheckIllegalCellCoverage(domainPath, recipeDir string) []Issue {
	cells, err := parseIllegalCells(domainPath)
	if err != nil || len(cells) == 0 {
		return nil
	}

	covered := collectIllegalCellTags(recipeDir)

	var issues []Issue
	for _, cell := range cells {
		if !covered[strings.ToLower(cell)] {
			issues = append(issues, Issue{
				Category: "simulation",
				Claim:    "uncovered_illegal_cell: " + cell,
				Severity: "critical",
				Detail:   "domain.md §4 declares cell '" + cell + "' ILLEGAL but no simulation scenario carries `[illegal-cell: " + cell + "]`. Add a scenario that would reach this cell and documents the enforcement mechanism.",
			})
		}
	}
	return issues
}

// ---- parsing helpers --------------------------------------------------

func countSimulationTags(content string) SimulationStats {
	var stats SimulationStats

	// Walk line by line so we can distinguish scenario headers from body
	// prose that happens to repeat the tag text. IsSimulationScenarioLine
	// (simulation_patterns.go) is the single source of truth for what
	// qualifies as a scenario line — keep this delegate thin so any
	// grammar change requires touching only simulation_patterns.go.
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		if !IsSimulationScenarioLine(line) {
			continue
		}
		stats.Total++
		crossB := simCrossBoundaryRe.MatchString(line)
		singleMatch := simSingleAxisRe.FindStringSubmatch(line)
		singleA := singleMatch != nil
		illegal := simIllegalCellRe.MatchString(line)

		// Legacy tag — migrate-authored, no cross-axis classification.
		// Count separately so the ratio denominator can exclude them.
		isLegacy := singleA && len(singleMatch) >= 2 &&
			strings.EqualFold(strings.TrimSpace(singleMatch[1]), "legacy")

		switch {
		case crossB:
			stats.CrossBoundary++
		case illegal:
			stats.IllegalCell++
		case isLegacy:
			stats.Legacy++
		case singleA:
			stats.SingleAxis++
		default:
			stats.Untagged++
		}
	}
	return stats
}

var domainIllegalSectionRe = regexp.MustCompile(`(?i)^##\s*4\.?\s*Combinatorial\s+State\s+Space\b`)
var illegalCellLabelRe = regexp.MustCompile(`(?i)ILLEGAL[:\s]*([^\n|]+)`)

func parseIllegalCells(domainPath string) ([]string, error) {
	data, err := os.ReadFile(domainPath)
	if err != nil {
		return nil, err
	}
	content := string(data)
	lines := strings.Split(content, "\n")

	inSection := false
	var cells []string
	seen := map[string]bool{}
	for _, line := range lines {
		if !inSection {
			if domainIllegalSectionRe.MatchString(strings.TrimSpace(line)) {
				inSection = true
			}
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(line), "## ") &&
			!domainIllegalSectionRe.MatchString(strings.TrimSpace(line)) {
			break
		}
		// Match "ILLEGAL: label" segments anywhere on the line.
		matches := illegalCellLabelRe.FindAllStringSubmatch(line, -1)
		for _, m := range matches {
			if len(m) < 2 {
				continue
			}
			label := strings.TrimSpace(m[1])
			// Strip trailing punctuation and table junk.
			label = strings.TrimRight(label, " .—-*_`")
			label = strings.TrimSpace(label)
			if label == "" {
				continue
			}
			key := strings.ToLower(label)
			if seen[key] {
				continue
			}
			seen[key] = true
			cells = append(cells, label)
		}
	}
	return cells, nil
}

func collectIllegalCellTags(recipeDir string) map[string]bool {
	simsDir := filepath.Join(recipeDir, "simulations")
	entries, err := os.ReadDir(simsDir)
	if err != nil {
		return map[string]bool{}
	}
	covered := map[string]bool{}
	tagLabelRe := regexp.MustCompile(`(?i)\[illegal-cell:\s*([^\]]+)\]`)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(simsDir, e.Name()))
		if err != nil {
			continue
		}
		for _, m := range tagLabelRe.FindAllStringSubmatch(string(data), -1) {
			if len(m) < 2 {
				continue
			}
			label := strings.TrimSpace(m[1])
			covered[strings.ToLower(label)] = true
		}
	}
	return covered
}

// pct renders a ratio as an integer percent string (0.333 → "33%"). The
// checker messages only need one-decimal precision at most, but integer
// percent is clearer for the common 0.30 threshold case.
func pct(r float64) string {
	return fmt.Sprintf("%d%%", int(r*100+0.5))
}
