package statusline

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/jlim/bts/internal/state"
)

// StdinData is the JSON Claude Code sends to the statusline hook.
type StdinData struct {
	ContextWindow *ContextWindowInfo `json:"context_window"`
}

// ContextWindowInfo holds context window usage data.
type ContextWindowInfo struct {
	UsedPercentage    *float64      `json:"used_percentage"`
	ContextWindowSize int           `json:"context_window_size"`
	CurrentUsage      *CurrentUsage `json:"current_usage"`
	Used              int           `json:"used"`  // legacy
	Total             int           `json:"total"` // legacy
}

// CurrentUsage holds detailed token usage.
type CurrentUsage struct {
	InputTokens         int `json:"input_tokens"`
	CacheCreationTokens int `json:"cache_creation_input_tokens"`
	CacheReadTokens     int `json:"cache_read_input_tokens"`
	OutputTokens        int `json:"output_tokens"`
}

// Render reads Claude Code's stdin JSON and bts state, returns a 1-line statusline.
func Render(stdin io.Reader, btsRoot string) string {
	// Parse stdin
	var data StdinData
	if stdin != nil {
		raw, _ := io.ReadAll(stdin)
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &data)
		}
	}

	var segments []string
	segments = append(segments, "bts")

	// Recipe info
	if btsRoot != "" {
		recipeSegment := renderRecipeSegment(btsRoot)
		if recipeSegment != "" {
			segments = append(segments, recipeSegment)
		}
	}

	// Context bar
	pct := getContextPercentage(&data)
	if pct >= 0 {
		segments = append(segments, renderContextBar(pct))
	}

	return strings.Join(segments, " │ ")
}

// renderRecipeSegment returns "topic │ phase detail" or empty string.
func renderRecipeSegment(btsRoot string) string {
	// Try work state first (richer info)
	ws, _ := state.LoadWorkState(btsRoot)
	if ws != nil && ws.RecipeID != "" {
		topic := truncate(ws.Topic, 20)
		detail := renderPhaseFromWorkState(ws, btsRoot)
		return topic + " │ " + detail
	}

	// Fall back to recipe state
	recipe, _ := state.GetActiveRecipe(btsRoot)
	if recipe == nil {
		recipe, _ = state.GetFinalizedRecipe(btsRoot)
	}
	if recipe == nil {
		return ""
	}

	topic := truncate(recipe.Topic, 20)
	return topic + " │ " + recipe.Phase
}

// renderPhaseFromWorkState builds a detailed phase string.
func renderPhaseFromWorkState(ws *state.WorkState, btsRoot string) string {
	switch ws.Phase {
	case "scoping":
		status := "DRAFT"
		if ws.ScopeStatus == "CONFIRMED" {
			status = "CONFIRMED"
		}
		return fmt.Sprintf("scoping (%s)", status)

	case "implement":
		return renderImplementDetail(ws, btsRoot)

	case "test":
		return renderTestDetail(btsRoot, ws.RecipeID)

	case "finalize":
		return "finalize ✓"

	case "complete":
		return "complete ✓"

	case "debate":
		return "debate → adjudicate"

	default:
		return ws.Phase
	}
}

// renderImplementDetail shows task progress.
func renderImplementDetail(ws *state.WorkState, btsRoot string) string {
	if btsRoot == "" {
		return "implement"
	}

	ts, err := state.LoadTaskState(btsRoot, ws.RecipeID)
	if err != nil || ts == nil {
		return "implement"
	}

	done, total := 0, len(ts.Tasks)
	for _, t := range ts.Tasks {
		if t.Status == "done" || t.Status == "skipped" {
			done++
		}
	}

	// Show current in-progress task if available
	if ws.CurrentTask != nil && ws.CurrentTask.Status == "in_progress" {
		if ws.CurrentTask.RetryCount > 0 {
			return fmt.Sprintf("implement %s retry %d/5", ws.CurrentTask.ID, ws.CurrentTask.RetryCount)
		}
		return fmt.Sprintf("implement %s (%d/%d)", ws.CurrentTask.ID, done, total)
	}

	return fmt.Sprintf("implement %d/%d", done, total)
}

// renderTestDetail shows test progress.
func renderTestDetail(btsRoot, recipeID string) string {
	if btsRoot == "" {
		return "test"
	}

	tr, err := state.LoadTestResults(btsRoot, recipeID)
	if err != nil || tr == nil {
		return "test"
	}

	return fmt.Sprintf("test %d/%d %s", tr.Passed, tr.Total, tr.Status)
}

// getContextPercentage extracts context usage from Claude Code's JSON.
func getContextPercentage(data *StdinData) float64 {
	if data.ContextWindow == nil {
		return -1
	}
	cw := data.ContextWindow

	// Priority 1: pre-calculated percentage
	if cw.UsedPercentage != nil {
		return *cw.UsedPercentage
	}

	// Priority 2: current usage tokens / window size
	if cw.CurrentUsage != nil && cw.ContextWindowSize > 0 {
		used := cw.CurrentUsage.InputTokens +
			cw.CurrentUsage.CacheCreationTokens +
			cw.CurrentUsage.CacheReadTokens +
			cw.CurrentUsage.OutputTokens
		return float64(used) / float64(cw.ContextWindowSize) * 100
	}

	// Priority 3: legacy used/total
	if cw.Total > 0 {
		return float64(cw.Used) / float64(cw.Total) * 100
	}

	return -1
}

// renderContextBar renders a 10-segment progress bar with percentage.
func renderContextBar(pct float64) string {
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}

	filled := int(pct / 10)
	empty := 10 - filled

	bar := strings.Repeat("━", filled) + strings.Repeat("─", empty)
	return fmt.Sprintf("%s %d%%", bar, int(pct))
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}
