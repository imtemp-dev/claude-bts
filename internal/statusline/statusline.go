package statusline

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/imtemp-dev/claude-bts/internal/metrics"
	"github.com/imtemp-dev/claude-bts/internal/state"
	"github.com/imtemp-dev/claude-bts/pkg/version"
)

// StdinData is the JSON Claude Code sends to the statusline hook.
type StdinData struct {
	ContextWindow *ContextWindowInfo `json:"context_window"`
}

// ContextWindowInfo holds context window usage data.
type ContextWindowInfo struct {
	RemainingPercentage *float64      `json:"remaining_percentage"` // Claude Code banner uses this
	UsedPercentage      *float64      `json:"used_percentage"`       // input tokens only
	ContextWindowSize   int           `json:"context_window_size"`
	CurrentUsage        *CurrentUsage `json:"current_usage"`
	Used                int           `json:"used"`  // legacy
	Total               int           `json:"total"` // legacy
}

// CurrentUsage holds detailed token usage.
type CurrentUsage struct {
	InputTokens         int `json:"input_tokens"`
	CacheCreationTokens int `json:"cache_creation_input_tokens"`
	CacheReadTokens     int `json:"cache_read_input_tokens"`
	OutputTokens        int `json:"output_tokens"`
}

// Render reads Claude Code's stdin JSON and bts state, returns a 1-line statusline.
func Render(stdin io.Reader, root string) string {
	// Parse stdin
	var data StdinData
	if stdin != nil {
		raw, _ := io.ReadAll(stdin)
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &data)
			// Always save last payload for diagnosis (overwritten each call, low overhead)
			_ = os.WriteFile("/tmp/bts-sl-last.json", raw, 0644)
		}
	}

	var segments []string
	segments = append(segments, "bts"+version.GetVersion())

	// Recipe info
	if root != "" {
		recipeSegment := renderRecipeSegment(root)
		if recipeSegment != "" {
			segments = append(segments, recipeSegment)
		}
	}

	// Context bar (remaining%, matches Claude Code's "Context low" convention)
	remaining := getContextRemaining(&data)
	if remaining >= 0 {
		segments = append(segments, renderContextBar(remaining))
	}

	// Persist token snapshot to metrics (throttled, fire-and-forget)
	if root != "" && data.ContextWindow != nil && data.ContextWindow.CurrentUsage != nil {
		if metrics.ShouldEmitTokenSnapshot(root) {
			cu := data.ContextWindow.CurrentUsage
			event := &metrics.MetricsEvent{
				Kind: metrics.KindTokenSnapshot,
				Tokens: &metrics.TokenSnapshot{
					InputTokens:         cu.InputTokens,
					CacheCreationTokens: cu.CacheCreationTokens,
					CacheReadTokens:     cu.CacheReadTokens,
					OutputTokens:        cu.OutputTokens,
					ContextWindowSize:   data.ContextWindow.ContextWindowSize,
				},
			}
			if data.ContextWindow.UsedPercentage != nil {
				event.Tokens.UsedPercentage = *data.ContextWindow.UsedPercentage
			}
			_ = metrics.AppendGlobal(root, event)
			metrics.TouchTokenSentinel(root)
		}
	}

	return strings.Join(segments, " │ ")
}

// renderRecipeSegment returns "[🟡] phase detail" or empty string.
func renderRecipeSegment(root string) string {
	// Check if a subagent is running
	agentDot := ""
	agentFile := filepath.Join(state.LocalPath(root), "active-agent.json")
	if _, err := os.Stat(agentFile); err == nil {
		agentDot = "🟡 "
	}

	// Try work state first (richer info)
	ws, _ := state.LoadWorkState(root)
	if ws != nil && ws.RecipeID != "" {
		detail := renderPhaseFromWorkState(ws, root)
		return agentDot + blockedMarker(root, ws.RecipeID) + detail
	}

	// Fall back to recipe state
	recipe, _ := state.GetActiveRecipe(root)
	if recipe == nil {
		recipe, _ = state.GetFinalizedRecipe(root)
	}
	if recipe == nil {
		return ""
	}

	return agentDot + blockedMarker(root, recipe.ID) + recipe.Phase
}

// blockedMarker prefixes the phase when the recipe is waiting on a user
// decision. Without it "blocked on a person" and "working" render
// identically, which is the confusion the decision ledger exists to end.
func blockedMarker(root, recipeID string) string {
	if recipeID == "" {
		return ""
	}
	open, err := state.OpenDecisions(root, recipeID)
	if err != nil || len(open) == 0 {
		return ""
	}
	return fmt.Sprintf("⛔%d ", len(open))
}

// renderPhaseFromWorkState builds a detailed phase string.
func renderPhaseFromWorkState(ws *state.WorkState, root string) string {
	switch ws.Phase {
	case "scoping":
		status := "DRAFT"
		if ws.ScopeStatus == "CONFIRMED" {
			status = "CONFIRMED"
		}
		return fmt.Sprintf("scoping (%s)", status)

	case "implement":
		return renderImplementDetail(ws, root)

	case "test":
		return renderTestDetail(root, ws.RecipeID)

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
func renderImplementDetail(ws *state.WorkState, root string) string {
	if root == "" {
		return "implement"
	}

	ts, err := state.LoadTaskState(root, ws.RecipeID)
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
func renderTestDetail(root, recipeID string) string {
	if root == "" {
		return "test"
	}

	tr, err := state.LoadTestResults(root, recipeID)
	if err != nil || tr == nil {
		return "test"
	}

	return fmt.Sprintf("test %d/%d %s", tr.Passed, tr.Total, tr.Status)
}

// getContextRemaining extracts remaining context percentage from Claude Code's JSON.
// Returns remaining% to match Claude Code's own "Context low" banner convention.
func getContextRemaining(data *StdinData) float64 {
	if data.ContextWindow == nil {
		return -1
	}
	cw := data.ContextWindow

	// Priority 1: remaining_percentage — direct match with Claude Code's "Context low" banner
	if cw.RemainingPercentage != nil {
		return *cw.RemainingPercentage
	}

	// Priority 2: compute remaining from used_percentage
	if cw.UsedPercentage != nil {
		return 100 - *cw.UsedPercentage
	}

	// Priority 3: calculate remaining from all token counts
	if cw.CurrentUsage != nil && cw.ContextWindowSize > 0 {
		used := cw.CurrentUsage.InputTokens +
			cw.CurrentUsage.CacheCreationTokens +
			cw.CurrentUsage.CacheReadTokens +
			cw.CurrentUsage.OutputTokens
		return 100 - float64(used)/float64(cw.ContextWindowSize)*100
	}

	// Priority 4: legacy used/total
	if cw.Total > 0 {
		return 100 - float64(cw.Used)/float64(cw.Total)*100
	}

	return -1
}

// renderContextBar renders remaining context as a short label.
// Shows remaining% to match Claude Code's "Context low (X% remaining)" convention.
func renderContextBar(remaining float64) string {
	return fmt.Sprintf("ctx %d%%", int(remaining))
}
