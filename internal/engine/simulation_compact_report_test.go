package engine

import (
	"os"
	"path/filepath"
	"testing"
)

// The compact report bts-simulate writes since the fork became the
// walker: a Scenario Index table as the spine, findings as `### [GAP-nnn]`
// headings with a `Where:` line, no per-scenario transcript. The
// validator must count exactly the table rows — not the finding
// headings, not the carried-forward list — or every compact report would
// raise untagged_scenarios.
const compactReport = `# Simulation: draft.md — round 2

Generated: 2026-09-02T12:00:00Z
Recipe: r-001
Mode: document
Scope: delta (re-walked S02, S05; carried 3)
Scenarios: 2 walked, 3 carried, 1 uncovered

<bts-findings>
{"critical": 0, "major": 1, "minor_resolvable": 0, "minor_deferred": 0, "info": 0,
 "findings": [{"severity": "major", "title": "restore path never reaches the projection", "anchor": "S02 / §2.1"}]}
</bts-findings>

## Scenario Index
| ID | Title | Tag | Result | Findings |
| --- | --- | --- | --- | --- |
| S01 | Happy path | [single-axis: Auth] | PASS | — |
| S02 | Key rotation mid-flight | [cross-boundary: axes=Auth,Cache] | GAP | GAP-001 |
| S03 | Reach C8 via legacy import | [illegal-cell: C8] | PASS | — |
| S04 | Offline publish | [single-axis: Network] | PASS | — |
| S05 | Restore then edit | [cross-boundary: axes=Store,Projection] | PASS | — |

## Findings
### [GAP-001] restore path never reaches the projection — major
Where: S02 step 3
Trigger: rotate the key while a fetch is in flight
Source says: nothing about the in-flight request
Consequence: one implementor retries, another fails the request

## Uncovered
- edge Cache→Evict — below the line after the illegal cells

## Carried forward
- S01 PASS (round 1) · S03 PASS (round 1) · S04 PASS (round 1)
`

func TestCompactReport_CountsOnlyIndexRows(t *testing.T) {
	stats := countSimulationTags(compactReport)
	if stats.Total != 5 {
		t.Fatalf("total = %d, want 5 index rows (finding headings and carried list must not count): %+v", stats.Total, stats)
	}
	if stats.Untagged != 0 {
		t.Errorf("untagged = %d, want 0: %+v", stats.Untagged, stats)
	}
	if stats.CrossBoundary != 2 || stats.IllegalCell != 1 || stats.SingleAxis != 2 {
		t.Errorf("tag split = %+v", stats)
	}

	dir := t.TempDir()
	p := filepath.Join(dir, "002-scenarios.md")
	if err := os.WriteFile(p, []byte(compactReport), 0o644); err != nil {
		t.Fatal(err)
	}
	if issues := CheckSimulationScenarios(p, DefaultCrossBoundaryRatio); len(issues) != 0 {
		t.Errorf("compact report must validate clean, got %+v", issues)
	}
	if issues := CheckScenarioBudget(p, 5, 12); len(issues) != 0 {
		t.Errorf("5 scenarios is on the floor, got %+v", issues)
	}
}
