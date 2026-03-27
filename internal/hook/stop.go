package hook

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/imtemp-dev/claude-bts/internal/metrics"
	"github.com/imtemp-dev/claude-bts/internal/state"
)

type stopHandler struct{}

func NewStopHandler() Handler {
	return &stopHandler{}
}

func (h *stopHandler) EventType() EventType {
	return EventStop
}

func (h *stopHandler) Handle(input *HookInput) (*HookOutput, error) {
	root, err := state.FindRoot(input.CWD)
	if err != nil {
		return &HookOutput{}, nil
	}

	recipe, err := state.GetActiveRecipe(root)
	if err != nil || recipe == nil {
		// Check for finalized recipe (ready for implementation)
		finalized, _ := state.GetFinalizedRecipe(root)
		if finalized != nil {
			fmt.Fprintf(os.Stderr, "[bts] Spec finalized. Run /bts-implement %s to start implementation.\n", finalized.ID)
		}
		return &HookOutput{}, nil
	}

	// Check for fix completion marker
	if strings.Contains(input.StopHookContent, "<bts>FIX DONE</bts>") ||
		strings.Contains(input.StopHookContent, "FIX DONE") {
		return h.handleFixDone(root, recipe)
	}

	// Check for implementation completion marker
	if strings.Contains(input.StopHookContent, "<bts>IMPLEMENT DONE</bts>") ||
		strings.Contains(input.StopHookContent, "IMPLEMENT DONE") {
		return h.handleImplementDone(root, recipe)
	}

	// Check for spec completion marker
	if strings.Contains(input.StopHookContent, "<bts>DONE</bts>") ||
		(strings.Contains(input.StopHookContent, "DONE") &&
			!strings.Contains(input.StopHookContent, "IMPLEMENT DONE") &&
			!strings.Contains(input.StopHookContent, "FIX DONE")) {
		return h.handleSpecDone(root, recipe)
	}

	// No completion marker — allow stop without blocking.
	// Print next-step hint to stderr so user sees it immediately.
	if next := nextStepHint(root, recipe); next != "" {
		fmt.Fprintf(os.Stderr, "[bts] %s\n", next)
	}
	return &HookOutput{}, nil
}

// handleSpecDone validates spec recipe completion via verify-log.
func (h *stopHandler) handleSpecDone(root string, recipe *state.RecipeState) (*HookOutput, error) {
	recipeDir := state.RecipeDir(root, recipe.ID)

	// 1. Check verification.md exists (proves /verify was actually run)
	verifyDocPath := filepath.Join(recipeDir, "verification.md")
	if _, err := os.Stat(verifyDocPath); os.IsNotExist(err) {
		return blockOutput("No verification.md found. Run /bts-verify on draft.md before completing."), nil
	}

	// 2. Check verify-log has passing entry
	logPath := filepath.Join(recipeDir, "verify-log.jsonl")
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
	prevPhase := recipe.Phase
	recipe.Phase = "finalize"
	if err := state.SaveRecipeState(root, recipe); err != nil {
		return nil, fmt.Errorf("save recipe state: %w", err)
	}
	_ = metrics.Append(root, &metrics.MetricsEvent{
		Kind:          metrics.KindPhaseChange,
		RecipeID:      recipe.ID,
		Phase:         "finalize",
		PreviousPhase: prevPhase,
	})

	return &HookOutput{}, nil
}

// handleImplementDone validates implementation completion via tasks.json + test-results.json.
func (h *stopHandler) handleImplementDone(root string, recipe *state.RecipeState) (*HookOutput, error) {
	implCmd := fmt.Sprintf("/bts-implement %s", recipe.ID)
	if recipe.Type == "fix" {
		implCmd = fmt.Sprintf("/bts-recipe-fix %s", recipe.ID)
	}

	// 1. Check tasks.json
	tasks, err := state.LoadTaskState(root, recipe.ID)
	if err != nil {
		return blockOutput(fmt.Sprintf("No tasks.json found. Run %s to decompose tasks.", implCmd)), nil
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
			"Implementation incomplete: %d task(s) still pending. Run %s to complete remaining tasks.",
			pending, implCmd,
		)), nil
	}

	// 2. Check test-results.json
	testResults, err := state.LoadTestResults(root, recipe.ID)
	if err != nil {
		return blockOutput(fmt.Sprintf("No test-results.json found. Run %s to run tests.", implCmd)), nil
	}

	if testResults.Status != "pass" {
		return blockOutput(fmt.Sprintf(
			"Tests not passing: %d failed out of %d. Run %s to fix and re-test.",
			testResults.Failed, testResults.Total, implCmd,
		)), nil
	}

	// 3. Check that review has run (review.md exists)
	reviewPath := filepath.Join(state.RecipeDir(root, recipe.ID), "review.md")
	if _, err := os.Stat(reviewPath); os.IsNotExist(err) {
		return blockOutput(fmt.Sprintf("No review.md found. Run %s to complete review.", implCmd)), nil
	}

	// 4. Check that sync has run (deviation.md exists)
	deviationPath := filepath.Join(state.RecipeDir(root, recipe.ID), "deviation.md")
	if _, err := os.Stat(deviationPath); os.IsNotExist(err) {
		return blockOutput(fmt.Sprintf("No deviation.md found. Run %s to sync spec with code.", implCmd)), nil
	}
	// deviation.md content is a REPORT, not a gate.
	// Deviations and not-implemented items become follow-up work,
	// not blockers for the current recipe's completion.

	// All clear — mark as complete
	prevPhase := recipe.Phase
	recipe.Phase = "complete"
	if err := state.SaveRecipeState(root, recipe); err != nil {
		return nil, fmt.Errorf("save recipe state: %w", err)
	}
	_ = metrics.Append(root, &metrics.MetricsEvent{
		Kind:          metrics.KindPhaseChange,
		RecipeID:      recipe.ID,
		Phase:         "complete",
		PreviousPhase: prevPhase,
	})

	if err := state.MarkRoadmapItemDone(root, recipe.ID); err != nil {
		fmt.Fprintf(os.Stderr, "warning: update roadmap: %v\n", err)
	}
	return roadmapHint(root, "Implementation complete."), nil
}

// handleFixDone validates fix recipe completion via fix-spec.md + test-results.json.
func (h *stopHandler) handleFixDone(root string, recipe *state.RecipeState) (*HookOutput, error) {
	// 1. Check fix-spec.md exists
	fixSpecPath := filepath.Join(state.RecipeDir(root, recipe.ID), "fix-spec.md")
	if _, err := os.Stat(fixSpecPath); os.IsNotExist(err) {
		return blockOutput("No fix-spec.md found. Create fix spec before completing."), nil
	}

	// 2. Check test-results.json
	testResults, err := state.LoadTestResults(root, recipe.ID)
	if err != nil {
		return blockOutput("No test-results.json found. Run tests before completing fix."), nil
	}

	if testResults.Status != "pass" {
		return blockOutput(fmt.Sprintf(
			"Tests not passing: %d failed out of %d. Fix and re-test.",
			testResults.Failed, testResults.Total,
		)), nil
	}

	// All clear — mark as complete
	prevPhase := recipe.Phase
	recipe.Phase = "complete"
	if err := state.SaveRecipeState(root, recipe); err != nil {
		return nil, fmt.Errorf("save recipe state: %w", err)
	}
	_ = metrics.Append(root, &metrics.MetricsEvent{
		Kind:          metrics.KindPhaseChange,
		RecipeID:      recipe.ID,
		Phase:         "complete",
		PreviousPhase: prevPhase,
	})

	if err := state.MarkRoadmapItemDone(root, recipe.ID); err != nil {
		fmt.Fprintf(os.Stderr, "warning: update roadmap: %v\n", err)
	}
	return roadmapHint(root, "Fix complete."), nil
}

// roadmapHint logs roadmap progress to stderr (Stop hook cannot use hookSpecificOutput).
func roadmapHint(root string, prefix string) *HookOutput {
	done, total, nextItem := state.RoadmapProgress(root)
	if total > 0 {
		hint := fmt.Sprintf("Roadmap: %d/%d done.", done, total)
		if nextItem != "" {
			hint += fmt.Sprintf(" Next: %s — run /bts-recipe-blueprint to start.", nextItem)
		}
		fmt.Fprintf(os.Stderr, "[bts] %s %s\n", prefix, hint)
	}
	return &HookOutput{}
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
