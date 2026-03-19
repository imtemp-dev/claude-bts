package hook

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jlim/bts/internal/state"
)

type stopHandler struct{}

func NewStopHandler() Handler {
	return &stopHandler{}
}

func (h *stopHandler) EventType() EventType {
	return EventStop
}

func (h *stopHandler) Handle(input *HookInput) (*HookOutput, error) {
	btsRoot, err := state.FindBTSRoot(input.CWD)
	if err != nil {
		return &HookOutput{}, nil
	}

	recipe, err := state.GetActiveRecipe(btsRoot)
	if err != nil || recipe == nil {
		return &HookOutput{}, nil
	}

	// Check for implementation completion marker
	if strings.Contains(input.StopHookContent, "<bts>IMPLEMENT DONE</bts>") {
		return h.handleImplementDone(btsRoot, recipe)
	}

	// Check for spec completion marker
	if strings.Contains(input.StopHookContent, "<bts>DONE</bts>") {
		return h.handleSpecDone(btsRoot, recipe)
	}

	// No completion marker — remind about active recipe
	hint := "Continue or use /recipe cancel."
	if state.IsImplementPhase(recipe.Phase) {
		hint = fmt.Sprintf("Run /implement %s to continue, or /recipe cancel to abort.", recipe.ID)
	}
	return &HookOutput{
		HookSpecificOutput: &HookSpecificOutput{
			AdditionalContext: fmt.Sprintf(
				"[bts] Recipe \"%s\" is still active (Step: %s). %s",
				recipe.Topic, recipe.Phase, hint,
			),
		},
	}, nil
}

// handleSpecDone validates spec recipe completion via verify-log.
func (h *stopHandler) handleSpecDone(btsRoot string, recipe *state.RecipeState) (*HookOutput, error) {
	logPath := filepath.Join(state.RecipeDir(btsRoot, recipe.ID), "verify-log.jsonl")
	lastEntry, err := readLastVerifyEntry(logPath)
	if err != nil {
		return blockOutput("No verification log found. Run verification before completing."), nil
	}

	if lastEntry.Critical > 0 || lastEntry.Major > 0 {
		return blockOutput(fmt.Sprintf(
			"Verification not passed: %d critical, %d major errors remain. Fix and re-verify.",
			lastEntry.Critical, lastEntry.Major,
		)), nil
	}

	// All clear — allow completion
	recipe.Phase = "finalize"
	_ = state.SaveRecipeState(btsRoot, recipe)

	return &HookOutput{}, nil
}

// handleImplementDone validates implementation completion via tasks.json + test-results.json.
func (h *stopHandler) handleImplementDone(btsRoot string, recipe *state.RecipeState) (*HookOutput, error) {
	// 1. Check tasks.json
	tasks, err := state.LoadTaskState(btsRoot, recipe.ID)
	if err != nil {
		return blockOutput("No tasks.json found. Run /implement to decompose tasks before completing."), nil
	}

	var blocked, pending int
	for _, t := range tasks.Tasks {
		switch t.Status {
		case "blocked":
			blocked++
		case "pending", "in_progress":
			pending++
		}
	}

	if blocked > 0 {
		return blockOutput(fmt.Sprintf(
			"Implementation incomplete: %d task(s) blocked. Resolve blocked tasks before completing.",
			blocked,
		)), nil
	}

	if pending > 0 {
		return blockOutput(fmt.Sprintf(
			"Implementation incomplete: %d task(s) still pending. Complete all tasks before finishing.",
			pending,
		)), nil
	}

	// 2. Check test-results.json
	testResults, err := state.LoadTestResults(btsRoot, recipe.ID)
	if err != nil {
		return blockOutput("No test-results.json found. Run /test before completing implementation."), nil
	}

	if testResults.Status != "pass" {
		return blockOutput(fmt.Sprintf(
			"Tests not passing: %d failed out of %d. Fix failing tests before completing.",
			testResults.Failed, testResults.Total,
		)), nil
	}

	// 3. Check deviation.md for blocking issues
	deviationPath := filepath.Join(state.RecipeDir(btsRoot, recipe.ID), "deviation.md")
	if _, err := os.Stat(deviationPath); os.IsNotExist(err) {
		return blockOutput("No deviation.md found. Run /sync before completing implementation."), nil
	}
	// Check for "Not Implemented" items in deviation report
	deviationData, err := os.ReadFile(deviationPath)
	if err == nil {
		content := string(deviationData)
		notImplCount := countDeviationItems(content, "## Not Implemented")
		deviationCount := countDeviationItems(content, "## Deviations")
		if notImplCount > 0 {
			return blockOutput(fmt.Sprintf(
				"Sync report has %d not-implemented item(s). Implement them or update the spec before completing.",
				notImplCount,
			)), nil
		}
		if deviationCount > 0 {
			return blockOutput(fmt.Sprintf(
				"Sync report has %d unresolved deviation(s). Review and resolve before completing.",
				deviationCount,
			)), nil
		}
	}

	// All clear — mark as complete
	recipe.Phase = "complete"
	_ = state.SaveRecipeState(btsRoot, recipe)

	return &HookOutput{}, nil
}

// countDeviationItems counts non-header rows in a markdown table after the given section header.
func countDeviationItems(content, sectionHeader string) int {
	idx := strings.Index(content, sectionHeader)
	if idx < 0 {
		return 0
	}
	section := content[idx+len(sectionHeader):]
	// Find the next ## section or end of content
	nextSection := strings.Index(section[1:], "\n## ")
	if nextSection >= 0 {
		section = section[:nextSection+1]
	}
	count := 0
	for _, line := range strings.Split(section, "\n") {
		line = strings.TrimSpace(line)
		// Count table rows: starts with | but not header separator (|---|)
		if strings.HasPrefix(line, "|") && !strings.HasPrefix(line, "| Item") && !strings.HasPrefix(line, "|--") && !strings.HasPrefix(line, "| -") && len(line) > 3 {
			// Check it's not the header row
			if !strings.Contains(line, "File") || !strings.Contains(line, "Reason") {
				if strings.Count(line, "|") >= 3 {
					count++
				}
			}
		}
	}
	return count
}

func blockOutput(reason string) *HookOutput {
	return &HookOutput{
		Decision: "block",
		Reason:   reason,
	}
}

func readLastVerifyEntry(path string) (*state.VerifyLogEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var last state.VerifyLogEntry
	found := false
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		var entry state.VerifyLogEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		last = entry
		found = true
	}

	if !found {
		return nil, fmt.Errorf("empty verify log")
	}

	return &last, nil
}
