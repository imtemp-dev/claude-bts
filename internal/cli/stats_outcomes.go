package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

// `bts stats --outcomes` correlates verification quality with
// implementation outcomes across recipes. This is the feedback loop the
// pipeline otherwise lacks: "critical=0 predicts smooth implementation"
// is a design hypothesis — this report measures it against what
// actually happened (build retries, test iterations, deviations).

// RecipeOutcome aggregates one recipe's verification and implementation
// signals from the files already on disk.
type RecipeOutcome struct {
	ID    string `json:"id"`
	Type  string `json:"type"`
	Phase string `json:"phase"`

	// Verification side
	VerifyIterations int    `json:"verify_iterations"`
	FirstCritical    int    `json:"first_critical"`
	FirstMajor       int    `json:"first_major"`
	FinalStatus      string `json:"final_status,omitempty"`
	SimulateRuns     int    `json:"simulate_runs"`
	// Overrides names the hard gates this recipe finalized past by
	// recorded operator decision. An outcome that reached "complete"
	// under an override is not the same data point as one that reached
	// it through the gates, and aggregating them together is how a tool
	// convinces itself its gates are working.
	Overrides []string `json:"overrides,omitempty"`

	// Implementation side
	Tasks          int    `json:"tasks"`
	ImplRetries    int    `json:"impl_retries"`
	BlockedTasks   int    `json:"blocked_tasks"`
	TestIterations int    `json:"test_iterations"`
	TestStatus     string `json:"test_status,omitempty"`
	TestRecordedBy string `json:"test_recorded_by,omitempty"`
	// Deviations counts top-level "- " list lines in deviation.md —
	// an approximation of deviation entries, labeled as such in output.
	Deviations   int  `json:"deviations"`
	HasImplement bool `json:"has_implement"`
}

// gatherOutcomes builds outcomes for every recipe. Missing or malformed
// files degrade to zero values — a partial recipe still reports what it
// has instead of failing the whole run.
func gatherOutcomes(root string) ([]RecipeOutcome, error) {
	recipes, err := state.ListRecipes(root)
	if err != nil {
		return nil, err
	}
	var outs []RecipeOutcome
	for _, r := range recipes {
		o := RecipeOutcome{ID: r.ID, Type: r.Type, Phase: r.Phase}

		if entries, err := state.ReadVerifyLog(root, r.ID); err == nil && len(entries) > 0 {
			o.VerifyIterations = len(entries)
			o.FirstCritical = entries[0].Critical
			o.FirstMajor = entries[0].Major
			o.FinalStatus = entries[len(entries)-1].Status
		}

		if recs, oerr := state.ReadOverrides(root, r.ID); oerr == nil {
			for _, o2 := range state.LiveOverrides(recs) {
				o.Overrides = append(o.Overrides, o2.Gate)
			}
		}

		if changelog, err := state.ReadChangelog(root, r.ID); err == nil {
			for _, e := range changelog {
				if e.Action == "simulate" {
					o.SimulateRuns++
				}
			}
		}

		if ts, err := state.LoadTaskState(root, r.ID); err == nil && len(ts.Tasks) > 0 {
			o.HasImplement = true
			o.Tasks = len(ts.Tasks)
			for _, task := range ts.Tasks {
				o.ImplRetries += task.RetryCount
				if task.Status == "blocked" {
					o.BlockedTasks++
				}
			}
		}

		if tr, err := state.LoadTestResults(root, r.ID); err == nil {
			o.TestIterations = tr.Iterations
			o.TestStatus = tr.Status
			o.TestRecordedBy = tr.RecordedBy
		}

		o.Deviations = countDeviationEntries(
			filepath.Join(state.RecipeDir(root, r.ID), "deviation.md"))

		outs = append(outs, o)
	}
	return outs, nil
}

// countDeviationEntries counts top-level "- " list lines in
// deviation.md. Approximate by design — deviation.md is prose.
func countDeviationEntries(path string) int {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	count := 0
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		if strings.HasPrefix(scanner.Text(), "- ") {
			count++
		}
	}
	return count
}

// renderOutcomes prints the per-recipe table plus grouped means. Small
// samples are labeled honestly instead of dressed up as statistics.
func renderOutcomes(outs []RecipeOutcome) string {
	var b strings.Builder
	if len(outs) == 0 {
		return "No recipes found.\n"
	}

	b.WriteString("Recipe outcomes — verification quality vs implementation results\n")
	b.WriteString("(deviations = top-level list entries in deviation.md, approximate)\n\n")

	w := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tType\tPhase\tVerifyIter\tFirst C/M\tSimRuns\tTasks\tRetries\tBlocked\tTestIter\tTest\tDeviations\tOverrides")
	for _, o := range outs {
		test := o.TestStatus
		if test != "" && o.TestRecordedBy != "bts" {
			test += " (hand-recorded)"
		}
		over := "-"
		if len(o.Overrides) > 0 {
			over = strings.Join(o.Overrides, ",")
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%d/%d\t%d\t%d\t%d\t%d\t%d\t%s\t%d\t%s\n",
			o.ID, o.Type, o.Phase, o.VerifyIterations, o.FirstCritical, o.FirstMajor,
			o.SimulateRuns, o.Tasks, o.ImplRetries, o.BlockedTasks,
			o.TestIterations, test, o.Deviations, over)
	}
	w.Flush()

	// A recipe that reached its phase past a gate is not the same data
	// point as one that reached it through the gates. Saying so here is
	// what keeps the correlation section below from quietly reporting
	// that the gates work.
	var overridden int
	for _, o := range outs {
		if len(o.Overrides) > 0 {
			overridden++
		}
	}
	if overridden > 0 {
		fmt.Fprintf(&b, "\n%d recipe(s) finalized past a hard gate under a recorded override — "+
			"their outcomes are not evidence the gates held. `bts recipe override list <id>` for the reasons.\n",
			overridden)
	}

	// Grouped means over recipes that actually reached implementation AND
	// got there through the gates. Naming the overridden recipes above
	// and then folding them into these means anyway is how a report ends
	// up saying the gates work using the recipes that went around them.
	var implemented []RecipeOutcome
	excluded := 0
	for _, o := range outs {
		switch {
		case !o.HasImplement:
		case len(o.Overrides) > 0:
			excluded++
		default:
			implemented = append(implemented, o)
		}
	}
	b.WriteString("\n")
	if len(implemented) == 0 {
		if excluded > 0 {
			fmt.Fprintf(&b, "No un-overridden recipes with implementation data yet (%d excluded for finalizing past a gate).\n", excluded)
			return b.String()
		}
		b.WriteString("No recipes with implementation data yet — correlation section will populate as recipes complete.\n")
		return b.String()
	}

	lowIter := groupMean(implemented, func(o RecipeOutcome) bool { return o.VerifyIterations <= 2 })
	highIter := groupMean(implemented, func(o RecipeOutcome) bool { return o.VerifyIterations >= 3 })
	withSim := groupMean(implemented, func(o RecipeOutcome) bool { return o.SimulateRuns > 0 })
	noSim := groupMean(implemented, func(o RecipeOutcome) bool { return o.SimulateRuns == 0 })

	if excluded > 0 {
		fmt.Fprintf(&b, "Grouped means (implemented recipes; %d overridden recipe(s) excluded):\n", excluded)
	} else {
		b.WriteString("Grouped means (implemented recipes):\n")
	}
	fmt.Fprintf(&b, "  verify ≤2 iterations: %s\n", lowIter)
	fmt.Fprintf(&b, "  verify ≥3 iterations: %s\n", highIter)
	fmt.Fprintf(&b, "  simulate ran:         %s\n", withSim)
	fmt.Fprintf(&b, "  simulate skipped:     %s\n", noSim)

	if len(implemented) < 5 {
		fmt.Fprintf(&b, "\n⚠ Only %d implemented recipe(s) — directional signal only, not statistics. The report gains meaning as recipes accumulate.\n", len(implemented))
	}
	return b.String()
}

// groupMean formats "n=K avg-retries=X.X avg-test-iter=Y.Y avg-deviations=Z.Z"
// for the subset matching pred, or "n=0" when empty.
func groupMean(outs []RecipeOutcome, pred func(RecipeOutcome) bool) string {
	n, retries, testIter, dev := 0, 0, 0, 0
	for _, o := range outs {
		if !pred(o) {
			continue
		}
		n++
		retries += o.ImplRetries
		testIter += o.TestIterations
		dev += o.Deviations
	}
	if n == 0 {
		return "n=0"
	}
	return fmt.Sprintf("n=%d avg-retries=%.1f avg-test-iter=%.1f avg-deviations=%.1f",
		n, float64(retries)/float64(n), float64(testIter)/float64(n), float64(dev)/float64(n))
}

// runStatsOutcomes is the --outcomes entry point.
func runStatsOutcomes(root string, jsonOutput bool) error {
	outs, err := gatherOutcomes(root)
	if err != nil {
		return fmt.Errorf("gather outcomes: %w", err)
	}
	if jsonOutput {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(outs)
	}
	fmt.Print(renderOutcomes(outs))
	return nil
}
