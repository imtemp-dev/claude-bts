package metrics

import (
	"sort"
	"time"

	"github.com/imtemp-dev/claude-forge/internal/state"
)

// PhaseSpan records time spent in a phase.
type PhaseSpan struct {
	Phase     string        `json:"phase"`
	EnteredAt time.Time     `json:"entered_at"`
	ExitedAt  time.Time     `json:"exited_at,omitempty"`
	Duration  time.Duration `json:"duration"`
}

// ToolStat holds usage stats for a single tool.
type ToolStat struct {
	Name      string  `json:"name"`
	Count     int     `json:"count"`
	FailCount int     `json:"fail_count"`
	FailRate  float64 `json:"fail_rate"`
}

// SessionStats holds aggregated metrics for a single Claude Code session.
type SessionStats struct {
	SessionID string        `json:"session_id"`
	Model     string        `json:"model"`
	Source    string        `json:"source"`
	StartedAt time.Time    `json:"started_at"`
	EndedAt   time.Time    `json:"ended_at,omitempty"`
	Duration  time.Duration `json:"duration"`
	ToolCount int           `json:"tool_count"`
	ToolFails int           `json:"tool_fails"`
	Compacts  int           `json:"compacts"`
	Tokens    TokenSnapshot `json:"tokens"`
	Cost      CostBreakdown `json:"cost"`
}

// RecipeStats holds aggregated metrics for one recipe.
type RecipeStats struct {
	RecipeID      string                  `json:"recipe_id"`
	Topic         string                  `json:"topic"`
	Type          string                  `json:"type"`
	Phase         string                  `json:"phase"`
	TotalSessions int                     `json:"total_sessions"`
	TotalCompacts int                     `json:"total_compacts"`
	Models        []string                `json:"models"`
	Phases        []PhaseSpan             `json:"phases"`
	ToolCounts    map[string]int          `json:"tool_counts"`
	ToolFailures  map[string]int          `json:"tool_failures"`
	TokensTotal   TokenSnapshot           `json:"tokens_total"`
	TotalDuration time.Duration           `json:"total_duration"`
	Sessions      []SessionStats          `json:"sessions,omitempty"`
	CostByModel   map[string]CostBreakdown `json:"cost_by_model,omitempty"`
	TotalCost     CostBreakdown           `json:"total_cost"`
}

// ProjectStats holds cross-recipe aggregation.
type ProjectStats struct {
	TotalRecipes   int            `json:"total_recipes"`
	CompletedCount int            `json:"completed_count"`
	ActiveCount    int            `json:"active_count"`
	TotalSessions  int            `json:"total_sessions"`
	TotalCompacts  int            `json:"total_compacts"`
	TotalTokens    TokenSnapshot  `json:"total_tokens"`
	TopTools       []ToolStat     `json:"top_tools"`
	Models         []string       `json:"models"`
	RecentSessions []SessionStats `json:"recent_sessions,omitempty"`
	TotalCost      CostBreakdown  `json:"total_cost"`
}

// AggregateRecipe computes stats from a slice of events for a single recipe.
func AggregateRecipe(events []MetricsEvent) *RecipeStats {
	stats := &RecipeStats{
		ToolCounts:   make(map[string]int),
		ToolFailures: make(map[string]int),
	}

	modelSet := make(map[string]bool)
	var phaseEnteredAt time.Time
	var currentPhase string

	for _, e := range events {
		switch e.Kind {
		case KindSessionStart:
			stats.TotalSessions++
			if e.Model != "" {
				modelSet[e.Model] = true
			}
			if stats.RecipeID == "" {
				stats.RecipeID = e.RecipeID
			}

		case KindCompact:
			stats.TotalCompacts++

		case KindPhaseChange:
			ts, _ := time.Parse(time.RFC3339, e.Timestamp)

			// Close previous phase span
			if currentPhase != "" && !phaseEnteredAt.IsZero() && !ts.IsZero() {
				stats.Phases = append(stats.Phases, PhaseSpan{
					Phase:     currentPhase,
					EnteredAt: phaseEnteredAt,
					ExitedAt:  ts,
					Duration:  ts.Sub(phaseEnteredAt),
				})
			}
			currentPhase = e.Phase
			phaseEnteredAt = ts

		case KindToolUse:
			if e.ToolName != "" {
				stats.ToolCounts[e.ToolName]++
				if e.Success != nil && !*e.Success {
					stats.ToolFailures[e.ToolName]++
				}
			}

		case KindTokenSnapshot:
			// Token snapshots are cumulative within a session.
			// Keep only the latest snapshot overall (highest values).
			if e.Tokens != nil {
				stats.TokensTotal = *e.Tokens
			}
		}
	}

	for m := range modelSet {
		stats.Models = append(stats.Models, m)
	}
	sort.Strings(stats.Models)

	// Calculate total duration from first to last event
	if len(events) > 1 {
		first, _ := time.Parse(time.RFC3339, events[0].Timestamp)
		last, _ := time.Parse(time.RFC3339, events[len(events)-1].Timestamp)
		if !first.IsZero() && !last.IsZero() {
			stats.TotalDuration = last.Sub(first)
		}
	}

	// Session-level aggregation with cost
	stats.Sessions = AggregateSessions(events)
	stats.CostByModel = make(map[string]CostBreakdown)
	for _, s := range stats.Sessions {
		key := s.Model
		if key == "" {
			key = "unknown"
		}
		stats.CostByModel[key] = AddCost(stats.CostByModel[key], s.Cost)
		stats.TotalCost = AddCost(stats.TotalCost, s.Cost)
	}

	return stats
}

// AggregateProject computes project-wide stats.
func AggregateProject(root string) (*ProjectStats, error) {
	recipes, err := state.ListRecipes(root)
	if err != nil {
		return nil, err
	}

	stats := &ProjectStats{}
	toolCounts := make(map[string]int)
	toolFailures := make(map[string]int)
	modelSet := make(map[string]bool)

	stats.TotalRecipes = len(recipes)
	for _, r := range recipes {
		if r.Phase == "complete" {
			stats.CompletedCount++
		} else if r.Phase != "cancelled" {
			stats.ActiveCount++
		}
	}

	// Read global metrics
	events, err := ReadAllEvents(root)
	if err != nil {
		// No metrics yet — return basic recipe stats
		return stats, nil
	}

	for _, e := range events {
		switch e.Kind {
		case KindSessionStart:
			stats.TotalSessions++
			if e.Model != "" {
				modelSet[e.Model] = true
			}

		case KindCompact:
			stats.TotalCompacts++

		case KindToolUse:
			if e.ToolName != "" {
				toolCounts[e.ToolName]++
				if e.Success != nil && !*e.Success {
					toolFailures[e.ToolName]++
				}
			}

		case KindTokenSnapshot:
			// Token snapshots are cumulative within a session.
			// Keep only the latest snapshot (highest values).
			if e.Tokens != nil {
				stats.TotalTokens = *e.Tokens
			}
		}
	}

	for m := range modelSet {
		stats.Models = append(stats.Models, m)
	}
	sort.Strings(stats.Models)

	// Build top tools
	for name, count := range toolCounts {
		failCount := toolFailures[name]
		failRate := 0.0
		if count > 0 {
			failRate = float64(failCount) / float64(count) * 100
		}
		stats.TopTools = append(stats.TopTools, ToolStat{
			Name:      name,
			Count:     count,
			FailCount: failCount,
			FailRate:  failRate,
		})
	}
	sort.Slice(stats.TopTools, func(i, j int) bool {
		return stats.TopTools[i].Count > stats.TopTools[j].Count
	})
	if len(stats.TopTools) > 10 {
		stats.TopTools = stats.TopTools[:10]
	}

	// Session-level aggregation with cost
	allSessions := AggregateSessions(events)
	for _, s := range allSessions {
		stats.TotalCost = AddCost(stats.TotalCost, s.Cost)
	}
	// Keep most recent 10
	if len(allSessions) > 10 {
		stats.RecentSessions = allSessions[len(allSessions)-10:]
	} else {
		stats.RecentSessions = allSessions
	}

	return stats, nil
}

// sessionBuilder accumulates events for a single session.
type sessionBuilder struct {
	id        string
	model     string
	source    string
	startedAt time.Time
	endedAt   time.Time
	toolCount int
	toolFails int
	compacts  int
	tokens    TokenSnapshot
}

// AggregateSessions groups events by session and computes per-session stats.
// Token snapshots without a SessionID are attributed to the most recently started session.
func AggregateSessions(events []MetricsEvent) []SessionStats {
	builders := make(map[string]*sessionBuilder)
	var order []string // insertion order
	var lastSessionID string

	for _, e := range events {
		switch e.Kind {
		case KindSessionStart:
			if e.SessionID == "" {
				continue
			}
			lastSessionID = e.SessionID
			if _, exists := builders[e.SessionID]; !exists {
				builders[e.SessionID] = &sessionBuilder{id: e.SessionID}
				order = append(order, e.SessionID)
			}
			b := builders[e.SessionID]
			b.model = e.Model
			b.source = e.Source
			b.startedAt, _ = time.Parse(time.RFC3339, e.Timestamp)

		case KindSessionEnd:
			sid := e.SessionID
			if sid == "" {
				sid = lastSessionID
			}
			if b, ok := builders[sid]; ok {
				b.endedAt, _ = time.Parse(time.RFC3339, e.Timestamp)
			}

		case KindToolUse:
			sid := e.SessionID
			if sid == "" {
				sid = lastSessionID
			}
			if b, ok := builders[sid]; ok {
				b.toolCount++
				if e.Success != nil && !*e.Success {
					b.toolFails++
				}
			}

		case KindCompact:
			sid := e.SessionID
			if sid == "" {
				sid = lastSessionID
			}
			if b, ok := builders[sid]; ok {
				b.compacts++
			}

		case KindTokenSnapshot:
			sid := e.SessionID
			if sid == "" {
				sid = lastSessionID
			}
			if sid == "" {
				continue
			}
			// Auto-create builder for orphan snapshots
			if _, exists := builders[sid]; !exists {
				builders[sid] = &sessionBuilder{id: sid}
				order = append(order, sid)
			}
			if e.Tokens != nil {
				builders[sid].tokens = *e.Tokens
			}
		}
	}

	// Build results in insertion order
	var result []SessionStats
	for _, sid := range order {
		b := builders[sid]
		var dur time.Duration
		if !b.startedAt.IsZero() && !b.endedAt.IsZero() {
			dur = b.endedAt.Sub(b.startedAt)
		}
		ss := SessionStats{
			SessionID: b.id,
			Model:     b.model,
			Source:    b.source,
			StartedAt: b.startedAt,
			EndedAt:   b.endedAt,
			Duration:  dur,
			ToolCount: b.toolCount,
			ToolFails: b.toolFails,
			Compacts:  b.compacts,
			Tokens:    b.tokens,
			Cost:      CalculateCost(b.tokens, b.model),
		}
		result = append(result, ss)
	}

	return result
}
