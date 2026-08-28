package engine

import (
	"regexp"
	"strings"
)

// Simulation scenario recognition — single source of truth.
//
// Historically `simulation_checker.go` and `test_scenario_map.go` each
// defined their own heading regex. They drifted and the stricter one
// silently no-op'd on common author styles (r-017 `### S01`, r-018
// Scenario Index table). This file centralizes everything so the
// checker, the test-scenario mapper, and the migration tool agree on
// what counts as a "scenario line".
//
// Three canonical forms (see jig-simulate SKILL.md §Step 3.5):
//
//   1. Prose heading      — "### Scenario sim-001.s1 [single-axis: A]"
//   2. Short-id heading   — "### S01 — Happy path [single-axis: Auth]"
//   3. Scenario-index row — "| S01 | Happy path | ... | [single-axis: A] |"
//
// Plus two legacy tolerances (bullet list and bare "Scenario:" prefix)
// so recipes authored before Phase 19 still parse.

var (
	// simHeadingProseRe — `#`-heading whose "Scenario" keyword is
	// followed by an identifier (a number, `sim-...`, or `S\d+` label).
	// This intentionally EXCLUDES meta headings like `## Scenario Index`,
	// `## Scenario Summary`, `## Scenarios Overview` — those carry no
	// tag and would produce false untagged_scenarios findings if
	// counted. Each real scenario heading pairs "Scenario" with an id
	// token per jig-simulate SKILL.md §Step 3.5 Form A.
	simHeadingProseRe = regexp.MustCompile(
		`(?mi)^#{1,6}\s+.*?\bscenario\s+(?:\d+|sim-[A-Za-z0-9_.\-]+|S\d+[A-Za-z0-9_.\-]*)\b[^\n]*$`,
	)

	// simHeadingShortRe — `### S01` style. `S` must be followed by a
	// digit so words like "Scenario", "Setup", "Security" do not
	// accidentally match.
	simHeadingShortRe = regexp.MustCompile(
		`(?mi)^#{1,6}\s+S\d+[A-Za-z0-9_.\-]*\b[^\n]*$`,
	)

	// simHeadingBulletRe — legacy bullet list form.
	simHeadingBulletRe = regexp.MustCompile(
		`(?mi)^\s*-\s+scenario\s+\d+[^\n]*$`,
	)

	// simHeadingBareRe — bare "Scenario: trigger" prefix.
	simHeadingBareRe = regexp.MustCompile(
		`(?mi)^scenario:[^\n]*$`,
	)

	// simTableRowRe — Scenario Index table row. Deliberately strict:
	//   - leading `|`
	//   - first cell starts with `S\d+` or `sim-<label>` (exclusive
	//     filter that skips alignment rows `| --- |`, header rows
	//     `| ID |`, and data rows with numeric-only first cells like
	//     `| 1 | Name |`)
	//   - trailing `|` at end of line (anchored to avoid mid-line matches)
	simTableRowRe = regexp.MustCompile(
		`(?m)^\|\s*(S\d+[A-Za-z0-9_.\-]*|sim-[A-Za-z0-9_.\-]+)\s*\|[^\n]*\|\s*$`,
	)

	// explicitScenarioIDRe — extracts an id from a heading line. Used
	// when a heading matches one of simHeading* but we need the id
	// token for downstream linking. Mirrors simTableRowRe's first-cell
	// constraint so the two ID definitions agree.
	explicitScenarioIDRe = regexp.MustCompile(
		`\b(sim-[A-Za-z0-9_.\-]+|S\d+[A-Za-z0-9_.\-]*)\b`,
	)
)

// IsSimulationScenarioLine returns true iff the line is a scenario
// header OR a Scenario Index table row. Callers walking a file
// line-by-line use this to decide whether to count tags on that line.
//
// The function is pure and cheap (five regex tests), suitable for
// line-scan loops over simulation files.
func IsSimulationScenarioLine(line string) bool {
	return simHeadingProseRe.MatchString(line) ||
		simHeadingShortRe.MatchString(line) ||
		simHeadingBulletRe.MatchString(line) ||
		simHeadingBareRe.MatchString(line) ||
		simTableRowRe.MatchString(line)
}

// ExtractScenarioID returns the canonical id embedded in a scenario
// line. Empty string when no id is present — callers that need a
// guaranteed id should fall back to a synthesized one.
//
// For a table row, the id is the first cell (already constrained by
// simTableRowRe). For a heading, the id is the first `sim-…` or
// `S\d+` token found in the line.
func ExtractScenarioID(line string) string {
	if m := simTableRowRe.FindStringSubmatch(line); len(m) >= 2 {
		return strings.TrimSpace(m[1])
	}
	if m := explicitScenarioIDRe.FindStringSubmatch(line); len(m) >= 2 {
		return strings.TrimSpace(m[1])
	}
	return ""
}
