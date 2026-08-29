package engine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// RecipeStats bundles the numbers scripts/bts-monitor.ts reads for
// Phase 17's 14-indicator report. One struct per recipe; aggregate
// math stays in the TS layer where the report format lives.
type RecipeStats struct {
	TaskAnchorOrphans        int    `json:"task_anchor_orphans"`
	TaskAnchorTotal          int    `json:"task_anchor_total"`
	ModifyScopeViolations    int    `json:"modify_scope_violations"`
	ModifyScopeTasks         int    `json:"modify_scope_tasks"`
	StructureFindingsTotal   int    `json:"structure_findings_total"`
	CompletedTasks           int    `json:"completed_tasks"`
	MidrunInvocations        int    `json:"midrun_invocations"`
	MidrunExpected           int    `json:"midrun_expected"`
	DeviationRowsTotal       int    `json:"deviation_rows_total"`
	DeviationRowsNonCodeDiff int    `json:"deviation_rows_non_code_diff"`
	TestScenariosTotal       int    `json:"test_scenarios_total"`
	TestScenariosLinked      int    `json:"test_scenarios_linked"`
	TestScenariosLegacy      int    `json:"test_scenarios_legacy"`
	RetryLadderHistogram     [7]int `json:"retry_ladder_histogram"` // index 0 unused, 1..6 tiers
	LegacyModifyScopeTasks   int    `json:"legacy_modify_scope_tasks"`
}

// ComputeRecipeStats runs every Phase 9-16 checker once and records
// the counts the monitoring layer needs. No aggregation — the TS
// script averages across recipes.
func ComputeRecipeStats(projectRoot, recipeDir string) (*RecipeStats, error) {
	s := &RecipeStats{}

	// Task anchors (P9).
	finalPath := filepath.Join(recipeDir, "final.md")
	tasksPath := filepath.Join(recipeDir, "tasks.json")
	wireframePath := filepath.Join(recipeDir, "wireframe.md")
	// The denominator has to be the set CheckTaskAnchors judges, not the
	// union of both declaration sources — see TaskAnchorPopulation.
	s.TaskAnchorTotal = TaskAnchorPopulation(finalPath, wireframePath, tasksPath)
	if tasks, err := loadTasksForAnchor(tasksPath); err == nil {
		// Orphans are anchors with no task. CheckTaskAnchors emits them;
		// we count the issues of that category.
		for _, issue := range CheckTaskAnchors(finalPath, wireframePath, tasksPath) {
			if strings.HasPrefix(issue.Claim, "orphan_anchor") ||
				strings.HasPrefix(issue.Claim, "missing_anchor") {
				s.TaskAnchorOrphans++
			}
		}
		for _, t := range tasks {
			if t.Action == "modify" {
				s.ModifyScopeTasks++
			}
		}
	}

	// Modify scope (P14).
	for _, issue := range CheckModifyScope(finalPath, tasksPath, projectRoot) {
		if strings.Contains(issue.Claim, "scope_violation") ||
			strings.Contains(issue.Claim, "scope_symbol_missing") {
			s.ModifyScopeViolations++
		}
	}
	// Legacy placeholder count — authors owe us real symbols here.
	s.LegacyModifyScopeTasks = countLegacyModifyScope(tasksPath)

	// Structure findings (P10). Read them straight off tasks.json.
	if raw, err := os.ReadFile(tasksPath); err == nil {
		var payload struct {
			Tasks []struct {
				Status            string `json:"status"`
				StructureFindings []struct {
					Severity string `json:"severity"`
				} `json:"structure_findings"`
			} `json:"tasks"`
		}
		if json.Unmarshal(raw, &payload) == nil {
			for _, t := range payload.Tasks {
				if t.Status == "done" {
					s.CompletedTasks++
				}
				s.StructureFindingsTotal += len(t.StructureFindings)
			}
		}
	}

	// Mid-run review coverage (P11). Invocations = count of
	// `reviews/midrun-*.md`; expected = completed tasks ÷ configured
	// cadence. The expected number was missing from v0.5.0/v0.5.1,
	// which left midrun_review_coverage stuck at 1.0 in the monitor
	// script regardless of reality. Load settings so the denominator
	// respects per-project overrides.
	reviewsDir := filepath.Join(recipeDir, "reviews")
	if entries, err := os.ReadDir(reviewsDir); err == nil {
		for _, e := range entries {
			if !e.IsDir() && strings.HasPrefix(e.Name(), "midrun-") {
				s.MidrunInvocations++
			}
		}
	}
	// projectRoot is the argument the CLI passes through; stats.go's
	// caller resolves it to the BTS root, so LoadSettings works here.
	// If settings.yaml is absent LoadSettings returns DefaultSettings.
	if settings, err := LoadSettings(projectRoot); err == nil {
		every := settings.Implement.MidrunReviewEvery
		if every > 0 {
			s.MidrunExpected = s.CompletedTasks / every
		}
	}

	// Deviation driver distribution (P16).
	devPath := filepath.Join(recipeDir, "deviation.md")
	if rows, err := ParseDeviationMd(devPath); err == nil {
		s.DeviationRowsTotal = len(rows)
		for _, r := range rows {
			for _, d := range r.Drivers {
				if d != "code-diff" {
					s.DeviationRowsNonCodeDiff++
					break
				}
			}
		}
	}

	// Test scenarios (P13).
	if scenarios := collectSimulationScenarioIDs(recipeDir); len(scenarios.known) > 0 {
		s.TestScenariosTotal = len(scenarios.known)
		linked := map[string]bool{}
		if links, err := ExtractTestScenarioLinks(recipeDir); err == nil {
			for _, l := range links {
				linked[l.ScenarioID] = true
			}
		}
		if tr, _ := loadTestResultsForScenarios(recipeDir); tr != nil {
			for id, v := range tr.ScenarioCoverage {
				// Count "legacy" placeholders separately so adoption is
				// visible even when the gate passes.
				if len(v) == 1 && strings.EqualFold(v[0], "legacy") {
					s.TestScenariosLegacy++
				}
				linked[id] = true
			}
		}
		s.TestScenariosLinked = len(linked)
	}

	// Retry ladder histogram (P15).
	if raw, err := os.ReadFile(tasksPath); err == nil {
		var payload struct {
			Tasks []struct {
				RetryTier int    `json:"retry_tier"`
				Status    string `json:"status"`
			} `json:"tasks"`
		}
		if json.Unmarshal(raw, &payload) == nil {
			for _, t := range payload.Tasks {
				tier := t.RetryTier
				if tier < 0 {
					tier = 0
				}
				if tier > 6 {
					tier = 6
				}
				s.RetryLadderHistogram[tier]++
			}
		}
	}

	return s, nil
}

// countLegacyModifyScope counts tasks whose modify_scope is exactly
// `["legacy"]` — the Phase 14 migration placeholder.
func countLegacyModifyScope(tasksPath string) int {
	data, err := os.ReadFile(tasksPath)
	if err != nil {
		return 0
	}
	var payload struct {
		Tasks []struct {
			Action      string   `json:"action"`
			ModifyScope []string `json:"modify_scope"`
		} `json:"tasks"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return 0
	}
	count := 0
	for _, t := range payload.Tasks {
		if t.Action == "modify" && isLegacyScope(t.ModifyScope) {
			count++
		}
	}
	return count
}
