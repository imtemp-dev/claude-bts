package metrics

import (
	"sort"
	"time"

	"github.com/jlim/claude-forge/internal/state"
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

// RecipeStats holds aggregated metrics for one recipe.
type RecipeStats struct {
	RecipeID      string         `json:"recipe_id"`
	Topic         string         `json:"topic"`
	Type          string         `json:"type"`
	Phase         string         `json:"phase"`
	TotalSessions int            `json:"total_sessions"`
	TotalCompacts int            `json:"total_compacts"`
	Models        []string       `json:"models"`
	Phases        []PhaseSpan    `json:"phases"`
	ToolCounts    map[string]int `json:"tool_counts"`
	ToolFailures  map[string]int `json:"tool_failures"`
	TokensTotal   TokenSnapshot  `json:"tokens_total"`
	TotalDuration time.Duration  `json:"total_duration"`
}

// ProjectStats holds cross-recipe aggregation.
type ProjectStats struct {
	TotalRecipes   int           `json:"total_recipes"`
	CompletedCount int           `json:"completed_count"`
	ActiveCount    int           `json:"active_count"`
	TotalSessions  int           `json:"total_sessions"`
	TotalCompacts  int           `json:"total_compacts"`
	TotalTokens    TokenSnapshot `json:"total_tokens"`
	TopTools       []ToolStat    `json:"top_tools"`
	Models         []string      `json:"models"`
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
			if e.Tokens != nil {
				// Keep the latest snapshot as cumulative reference
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
			if e.Tokens != nil {
				stats.TotalTokens.InputTokens += e.Tokens.InputTokens
				stats.TotalTokens.OutputTokens += e.Tokens.OutputTokens
				stats.TotalTokens.CacheCreationTokens += e.Tokens.CacheCreationTokens
				stats.TotalTokens.CacheReadTokens += e.Tokens.CacheReadTokens
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

	return stats, nil
}
