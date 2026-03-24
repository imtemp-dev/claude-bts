package metrics

import (
	"path/filepath"
	"time"

	"github.com/jlim/claude-forge/internal/state"
)

// EventKind categorizes the metric event.
type EventKind string

const (
	KindSessionStart  EventKind = "session_start"
	KindSessionEnd    EventKind = "session_end"
	KindCompact       EventKind = "compact"
	KindPhaseChange   EventKind = "phase_change"
	KindToolUse       EventKind = "tool_use"
	KindTokenSnapshot EventKind = "token_snapshot"
	KindSubagentStart EventKind = "subagent_start"
	KindSubagentStop  EventKind = "subagent_stop"
)

// MetricsEvent is a single entry in metrics.jsonl.
type MetricsEvent struct {
	Timestamp string    `json:"ts"`
	Kind      EventKind `json:"kind"`
	SessionID string    `json:"session_id,omitempty"`
	RecipeID  string    `json:"recipe_id,omitempty"`
	Phase     string    `json:"phase,omitempty"`

	// Session fields
	Model  string `json:"model,omitempty"`
	Source string `json:"source,omitempty"`

	// Phase change fields
	PreviousPhase string `json:"prev_phase,omitempty"`

	// Tool fields
	ToolName string `json:"tool_name,omitempty"`
	ToolFile string `json:"tool_file,omitempty"`
	ExitCode *int   `json:"exit_code,omitempty"`
	Success  *bool  `json:"success,omitempty"`

	// Token fields
	Tokens *TokenSnapshot `json:"tokens,omitempty"`

	// Subagent fields
	AgentID string `json:"agent_id,omitempty"`
}

// TokenSnapshot holds token counts at a point in time.
type TokenSnapshot struct {
	InputTokens         int     `json:"input"`
	CacheCreationTokens int     `json:"cache_creation"`
	CacheReadTokens     int     `json:"cache_read"`
	OutputTokens        int     `json:"output"`
	ContextWindowSize   int     `json:"ctx_size,omitempty"`
	UsedPercentage      float64 `json:"used_pct,omitempty"`
}

// globalPath returns the project-wide metrics log path.
func globalPath(root string) string {
	return filepath.Join(state.StatePath(root), "metrics.jsonl")
}

// recipePath returns the per-recipe metrics log path.
func recipePath(root, recipeID string) string {
	return filepath.Join(state.RecipeDir(root, recipeID), "metrics.jsonl")
}

// Append writes a metric event to both global and per-recipe logs.
// If RecipeID is empty, only the global log is written.
// Errors are returned but callers should treat them as non-fatal.
func Append(root string, event *MetricsEvent) error {
	if event.Timestamp == "" {
		event.Timestamp = time.Now().UTC().Format(time.RFC3339)
	}

	if err := state.AppendJSONL(globalPath(root), event); err != nil {
		return err
	}

	if event.RecipeID != "" {
		// Best-effort write to recipe log; don't fail if recipe dir doesn't exist
		_ = state.AppendJSONL(recipePath(root, event.RecipeID), event)
	}

	return nil
}

// AppendGlobal writes a metric event to the global log only.
func AppendGlobal(root string, event *MetricsEvent) error {
	if event.Timestamp == "" {
		event.Timestamp = time.Now().UTC().Format(time.RFC3339)
	}
	return state.AppendJSONL(globalPath(root), event)
}
