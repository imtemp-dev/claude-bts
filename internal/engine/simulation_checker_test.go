package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeSim(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "001-scenarios.md")
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

func TestCheckSimulationScenarios_MeetsCrossBoundaryRatio(t *testing.T) {
	// 2 out of 5 are cross-boundary = 40% > 30%.
	body := `# Scenarios

## Scenario 1 [single-axis: Drag]
Body.

## Scenario 2 [cross-boundary: axes=Drag,Arrangement]
Body.

## Scenario 3 [single-axis: Animation]
Body.

## Scenario 4 [cross-boundary: axes=Arrangement,Validation]
Body.

## Scenario 5 [single-axis: Drag]
Body.
`
	path := writeSim(t, body)
	issues := CheckSimulationScenarios(path, DefaultCrossBoundaryRatio)
	if len(issues) != 0 {
		t.Fatalf("expected no issues, got %d: %v", len(issues), issues)
	}
}

func TestCheckSimulationScenarios_BelowThreshold(t *testing.T) {
	// 1 out of 5 = 20% < 30%.
	body := `## Scenario 1 [cross-boundary: axes=A,B]
## Scenario 2 [single-axis: A]
## Scenario 3 [single-axis: A]
## Scenario 4 [single-axis: B]
## Scenario 5 [single-axis: C]
`
	path := writeSim(t, body)
	issues := CheckSimulationScenarios(path, DefaultCrossBoundaryRatio)
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d: %v", len(issues), issues)
	}
	if issues[0].Severity != "critical" {
		t.Errorf("want critical, got %s", issues[0].Severity)
	}
	if !strings.Contains(issues[0].Claim, "insufficient_cross_boundary_coverage") {
		t.Errorf("wrong claim: %s", issues[0].Claim)
	}
}

func TestCheckSimulationScenarios_UntaggedFlagged(t *testing.T) {
	body := `## Scenario 1 [cross-boundary: axes=A,B]
## Scenario 2
No tag here.
`
	path := writeSim(t, body)
	issues := CheckSimulationScenarios(path, DefaultCrossBoundaryRatio)
	// "untagged_scenarios" major expected. Depending on ratio, no critical.
	var untaggedSeen bool
	for _, i := range issues {
		if strings.Contains(i.Claim, "untagged_scenarios") {
			untaggedSeen = true
		}
	}
	if !untaggedSeen {
		t.Fatalf("expected untagged_scenarios major, got %v", issues)
	}
}

// Illegal-cell scenarios are themselves cross-boundary by definition;
// they count toward the ratio.
func TestCheckSimulationScenarios_IllegalCellCountsTowardRatio(t *testing.T) {
	body := `## Scenario 1 [single-axis: A]
## Scenario 2 [single-axis: A]
## Scenario 3 [illegal-cell: drag×slot=dropping,filled]
`
	path := writeSim(t, body)
	// 1 illegal / 3 = 33% > 30%
	issues := CheckSimulationScenarios(path, DefaultCrossBoundaryRatio)
	if len(issues) != 0 {
		t.Fatalf("expected no issues, got %v", issues)
	}
}

// Full integration: domain.md with 2 illegal cells, simulations cover 1.
func TestCheckIllegalCellCoverage_UncoveredCellCritical(t *testing.T) {
	dir := t.TempDir()
	domain := filepath.Join(dir, "domain.md")
	domainBody := `# Domain

## 4. Combinatorial State Space

| col1 | col2 |
|------|------|
| A    | ILLEGAL: double-fill |
| B    | ILLEGAL: mid-snap-back |
`
	if err := os.WriteFile(domain, []byte(domainBody), 0644); err != nil {
		t.Fatalf("write domain: %v", err)
	}
	simsDir := filepath.Join(dir, "simulations")
	if err := os.MkdirAll(simsDir, 0755); err != nil {
		t.Fatalf("mkdir sims: %v", err)
	}
	simBody := `## Scenario 1 [illegal-cell: double-fill]`
	if err := os.WriteFile(filepath.Join(simsDir, "001.md"), []byte(simBody), 0644); err != nil {
		t.Fatalf("write sim: %v", err)
	}

	issues := CheckIllegalCellCoverage(domain, dir)
	if len(issues) != 1 {
		t.Fatalf("want 1 issue (mid-snap-back uncovered), got %d: %v", len(issues), issues)
	}
	if issues[0].Severity != "critical" {
		t.Errorf("want critical, got %s", issues[0].Severity)
	}
	if !strings.Contains(issues[0].Claim, "mid-snap-back") {
		t.Errorf("claim should mention uncovered cell, got %s", issues[0].Claim)
	}
}

func TestCheckIllegalCellCoverage_AllCellsCoveredPasses(t *testing.T) {
	dir := t.TempDir()
	domain := filepath.Join(dir, "domain.md")
	_ = os.WriteFile(domain, []byte(`## 4. Combinatorial State Space

| x | ILLEGAL: A |
| y | ILLEGAL: B |
`), 0644)
	simsDir := filepath.Join(dir, "simulations")
	_ = os.MkdirAll(simsDir, 0755)
	_ = os.WriteFile(filepath.Join(simsDir, "001.md"),
		[]byte(`## S1 [illegal-cell: A]
## S2 [illegal-cell: B]
`), 0644)

	issues := CheckIllegalCellCoverage(domain, dir)
	if len(issues) != 0 {
		t.Fatalf("expected 0 issues, got %v", issues)
	}
}

// ===== Sprint 9 P19 — canonical format + parser flexibility =====

// Short-id heading form (r-017 style) must be recognized as a scenario
// line even though the heading does not contain the literal word
// "scenario".
func TestCheckSimulationScenarios_ShortIDHeading(t *testing.T) {
	body := `# R17 Simulation

### S01 — Happy path [single-axis: Auth]
body prose here.

### S02 — Edge case [cross-boundary: axes=Auth,Cache]
more body.

### S03 — Another one [single-axis: Auth]
`
	path := writeSim(t, body)
	issues := CheckSimulationScenarios(path, DefaultCrossBoundaryRatio)
	// 1 of 3 cross-boundary = 33% > 30% — must pass cleanly.
	if len(issues) != 0 {
		t.Fatalf("expected 0 issues for valid short-id file, got %v", issues)
	}
}

// Scenario Index table form (r-018 style). Each row's first cell
// carries the id; tag sits in a later cell. Parser must count each
// row as a scenario line.
func TestCheckSimulationScenarios_TableRowIndex(t *testing.T) {
	body := `## Scenario Index

| ID  | Title          | Result | Tag                               |
| --- | -------------- | ------ | --------------------------------- |
| S01 | Happy path     | PASS   | [single-axis: Auth]               |
| S02 | Key rotation   | PASS   | [cross-boundary: axes=Auth,Cache] |
| S03 | Cache miss     | PASS   | [single-axis: Cache]              |
`
	path := writeSim(t, body)
	issues := CheckSimulationScenarios(path, DefaultCrossBoundaryRatio)
	// 1 of 3 cross-boundary = 33% — passes.
	if len(issues) != 0 {
		t.Fatalf("expected 0 issues for valid table form, got %v", issues)
	}
}

// Markdown alignment rows `| --- | --- |` must never be counted as
// scenario lines. Header rows `| ID | Title |` likewise — first cell
// `ID` has no digit, so `S\d+` prefix regex rejects it.
// Test uses a cross-boundary scenario so the ratio passes with only
// one data row (1/1 = 100% ≥ 30%); the point of this test is
// parser discipline, not ratio logic.
func TestCheckSimulationScenarios_AlignmentRowIgnored(t *testing.T) {
	body := `## Scenario Index

| ID  | Title       | Tag                               |
| --- | ----------- | --------------------------------- |
| S01 | Only one    | [cross-boundary: axes=Auth,Cache] |
`
	path := writeSim(t, body)
	issues := CheckSimulationScenarios(path, DefaultCrossBoundaryRatio)
	if len(issues) != 0 {
		t.Fatalf("expected 0 issues (alignment + header rows must be ignored), got %v", issues)
	}
}

// Data tables with numeric first cells must not be misread as
// scenarios. This is the r-014 anti-pattern (`| # | 1 | Item | ... |`).
func TestCheckSimulationScenarios_DataTableNotScenario(t *testing.T) {
	body := `## Other Notes

| # | Item      | Count |
| - | --------- | ----- |
| 1 | foo       | 42    |
| 2 | bar       | 17    |

## Scenario Index

| ID  | Title    | Tag                              |
| --- | -------- | -------------------------------- |
| S01 | Happy    | [cross-boundary: axes=Auth,Cache] |
`
	path := writeSim(t, body)
	issues := CheckSimulationScenarios(path, DefaultCrossBoundaryRatio)
	// Only S01 should register as a scenario; untagged data rows
	// above must not trigger untagged_scenarios.
	if len(issues) != 0 {
		t.Fatalf("expected 0 issues — data rows must not register as scenarios, got %v", issues)
	}
}

// Markdown table present but no row matches the Scenario Index form
// (all rows have non-id first cells) should trigger the Phase 19
// hint so authors self-correct.
func TestCheckSimulationScenarios_TablePresentNoScenarioHint(t *testing.T) {
	body := `## Sections

| Section | Coverage |
| ------- | -------- |
| Intro   | done     |
| Body    | partial  |
`
	path := writeSim(t, body)
	issues := CheckSimulationScenarios(path, DefaultCrossBoundaryRatio)
	if len(issues) != 1 {
		t.Fatalf("want 1 hint, got %d: %v", len(issues), issues)
	}
	if !strings.Contains(issues[0].Claim, "no_scenarios_detected") {
		t.Errorf("expected no_scenarios_detected hint, got %s", issues[0].Claim)
	}
}

// Mixed form: heading + table in the same file. Ids must dedupe
// correctly so a single scenario appearing in both places is not
// double-counted by the ratio denominator.
func TestCheckSimulationScenarios_MixedForms(t *testing.T) {
	body := `## Scenario Index

| ID  | Title       | Tag                                |
| --- | ----------- | ---------------------------------- |
| S01 | Happy path  | [single-axis: Auth]                |
| S02 | Key rotate  | [cross-boundary: axes=Auth,Cache]  |

### S01 — Happy path (detail) [single-axis: Auth]
body prose
`
	path := writeSim(t, body)
	// The simulation checker counts scenario *lines*, so S01 appears
	// twice by design (once in table, once in heading). The ratio is
	// still 1 of 3 tagged = 33% > 30% — pass. Consumers that need
	// unique ids (test_scenario_map) dedupe by ID instead.
	issues := CheckSimulationScenarios(path, DefaultCrossBoundaryRatio)
	if len(issues) != 0 {
		t.Fatalf("mixed forms should pass when ratio holds, got %v", issues)
	}
}

func TestCheckScenarioBudget(t *testing.T) {
	write := func(t *testing.T, n int) string {
		t.Helper()
		var b strings.Builder
		b.WriteString("# Simulation\n\n")
		for i := 1; i <= n; i++ {
			fmt.Fprintf(&b, "### S%02d — case %d [single-axis: Auth]\n\nbody\n\n", i, i)
		}
		path := filepath.Join(t.TempDir(), "001-scenarios.md")
		if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	has := func(issues []Issue, claim string) bool {
		for _, i := range issues {
			if strings.Contains(i.Claim, claim) {
				return true
			}
		}
		return false
	}

	if got := CheckScenarioBudget(write(t, 8), 5, 12); len(got) != 0 {
		t.Fatalf("inside the budget must be silent, got %+v", got)
	}
	under := CheckScenarioBudget(write(t, 3), 5, 12)
	if !has(under, "scenario_floor_not_met") {
		t.Fatalf("below the floor must report, got %+v", under)
	}
	if under[0].Severity != SeverityMajor {
		t.Errorf("floor breach is a coverage gap: want major, got %s", under[0].Severity)
	}
	over := CheckScenarioBudget(write(t, 20), 5, 12)
	if !has(over, "scenario_budget_exceeded") {
		t.Fatalf("above the ceiling must report, got %+v", over)
	}
	if over[0].Severity != SeverityMinor {
		t.Errorf("ceiling breach is a cost overrun, not a wrong document: want minor, got %s", over[0].Severity)
	}
	// 0 disables either side, matching LoadSettings.
	if got := CheckScenarioBudget(write(t, 40), 0, 0); len(got) != 0 {
		t.Fatalf("zero bounds must disable the check, got %+v", got)
	}
}
