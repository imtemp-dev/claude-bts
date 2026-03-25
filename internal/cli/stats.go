package cli

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/imtemp-dev/claude-forge/internal/metrics"
	"github.com/imtemp-dev/claude-forge/internal/state"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(statsCmd)
	statsCmd.Flags().Bool("json", false, "Output as JSON")
	statsCmd.Flags().Bool("csv", false, "Output as CSV (one row per session)")
}

var statsCmd = &cobra.Command{
	Use:     "stats [recipe-id]",
	Short:   "Show metrics and performance statistics",
	GroupID: "tools",
	Args:    cobra.MaximumNArgs(1),
	RunE:    runStats,
}

func runStats(cmd *cobra.Command, args []string) error {
	cwd, _ := os.Getwd()
	root, err := state.FindRoot(cwd)
	if err != nil {
		return fmt.Errorf("not a forge project: %w", err)
	}

	jsonOutput, _ := cmd.Flags().GetBool("json")
	csvOutput, _ := cmd.Flags().GetBool("csv")

	if len(args) > 0 {
		if csvOutput {
			return showRecipeCSV(root, args[0])
		}
		return showRecipeStats(root, args[0], jsonOutput)
	}
	if csvOutput {
		return showProjectCSV(root)
	}
	return showProjectStats(root, jsonOutput)
}

func showProjectStats(root string, jsonOutput bool) error {
	stats, err := metrics.AggregateProject(root)
	if err != nil {
		return fmt.Errorf("aggregate: %w", err)
	}

	if jsonOutput {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(stats)
	}

	fmt.Println("Project Overview")
	fmt.Println(strings.Repeat("─", 40))
	fmt.Printf("  Recipes:     %d complete, %d active, %d total\n",
		stats.CompletedCount, stats.ActiveCount, stats.TotalRecipes)
	fmt.Printf("  Sessions:    %d total, %d compactions\n",
		stats.TotalSessions, stats.TotalCompacts)
	if len(stats.Models) > 0 {
		fmt.Printf("  Models:      %s\n", strings.Join(stats.Models, ", "))
	}

	if stats.TotalTokens.InputTokens > 0 || stats.TotalTokens.OutputTokens > 0 || stats.TotalTokens.CacheReadTokens > 0 {
		fmt.Println()
		fmt.Println("Context Window (latest snapshot)")
		fmt.Println(strings.Repeat("─", 40))
		if stats.TotalTokens.UsedPercentage > 0 {
			fmt.Printf("  Used:        %.0f%%\n", stats.TotalTokens.UsedPercentage)
		}
		if stats.TotalTokens.ContextWindowSize > 0 {
			fmt.Printf("  Window:      %s tokens\n", formatTokens(stats.TotalTokens.ContextWindowSize))
		}
		fmt.Printf("  Input:       %s\n", formatTokens(stats.TotalTokens.InputTokens))
		fmt.Printf("  Output:      %s\n", formatTokens(stats.TotalTokens.OutputTokens))
		fmt.Printf("  Cache Read:  %s\n", formatTokens(stats.TotalTokens.CacheReadTokens))
		fmt.Printf("  Cache Write: %s\n", formatTokens(stats.TotalTokens.CacheCreationTokens))
	}

	if stats.TotalCost.Total > 0 {
		fmt.Println()
		fmt.Println("Estimated Cost")
		fmt.Println(strings.Repeat("─", 40))
		fmt.Printf("  Total:       %s\n", metrics.FormatCost(stats.TotalCost.Total))
		fmt.Printf("  Input:       %s\n", metrics.FormatCost(stats.TotalCost.Input))
		fmt.Printf("  Output:      %s\n", metrics.FormatCost(stats.TotalCost.Output))
		fmt.Printf("  Cache Read:  %s\n", metrics.FormatCost(stats.TotalCost.CacheRead))
		fmt.Printf("  Cache Write: %s\n", metrics.FormatCost(stats.TotalCost.CacheWrite))
	}

	if len(stats.TopTools) > 0 {
		fmt.Println()
		fmt.Println("Tool Usage")
		fmt.Println(strings.Repeat("─", 40))
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		for _, t := range stats.TopTools {
			fmt.Fprintf(w, "  %s\t%d calls\t%.0f%% fail\n", t.Name, t.Count, t.FailRate)
		}
		w.Flush()
	}

	if len(stats.RecentSessions) > 0 {
		fmt.Println()
		fmt.Println("Recent Sessions")
		fmt.Println(strings.Repeat("─", 40))
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		for _, s := range stats.RecentSessions {
			dur := formatDuration(s.Duration)
			if s.Duration == 0 {
				dur = "-"
			}
			fmt.Fprintf(w, "  %s\t%s\t%s\t%s\t%s in / %s out\n",
				truncateID(s.SessionID, 12),
				formatModelShort(s.Model),
				dur,
				metrics.FormatCost(s.Cost.Total),
				formatTokens(s.Tokens.InputTokens),
				formatTokens(s.Tokens.OutputTokens),
			)
		}
		w.Flush()
	}

	if stats.TotalSessions == 0 {
		fmt.Println()
		fmt.Println("No metrics data yet. Metrics are collected during Claude Code sessions.")
	}

	return nil
}

func showRecipeStats(root, recipeID string, jsonOutput bool) error {
	// Load recipe info
	recipe, err := state.LoadRecipeState(root, recipeID)
	if err != nil {
		return fmt.Errorf("load recipe: %w", err)
	}

	// Read recipe-level metrics
	events, err := metrics.ReadRecipeEvents(root, recipeID)
	if err != nil {
		fmt.Printf("Recipe: %s \"%s\" (%s) — %s\n", recipe.ID, recipe.Topic, recipe.Type, recipe.Phase)
		fmt.Println("No metrics data for this recipe.")
		return nil
	}

	stats := metrics.AggregateRecipe(events)
	stats.Topic = recipe.Topic
	stats.Type = recipe.Type
	stats.Phase = recipe.Phase

	if jsonOutput {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(stats)
	}

	fmt.Printf("Recipe: %s \"%s\"\n", recipe.ID, recipe.Topic)
	fmt.Println(strings.Repeat("─", 40))
	fmt.Printf("  Type:        %s\n", recipe.Type)
	fmt.Printf("  Phase:       %s\n", recipe.Phase)
	fmt.Printf("  Sessions:    %d (%d compactions)\n", stats.TotalSessions, stats.TotalCompacts)
	if len(stats.Models) > 0 {
		fmt.Printf("  Models:      %s\n", strings.Join(stats.Models, ", "))
	}
	if stats.TotalDuration > 0 {
		fmt.Printf("  Duration:    %s\n", formatDuration(stats.TotalDuration))
	}
	if stats.TotalCost.Total > 0 {
		fmt.Printf("  Cost:        %s\n", metrics.FormatCost(stats.TotalCost.Total))
	}

	if len(stats.Phases) > 0 {
		fmt.Println()
		var totalPhaseDur time.Duration
		for _, p := range stats.Phases {
			totalPhaseDur += p.Duration
		}
		if totalPhaseDur > 0 {
			fmt.Printf("Phase Timeline (%s)\n", formatDuration(totalPhaseDur))
		} else {
			fmt.Println("Phase Timeline")
		}
		fmt.Println(strings.Repeat("─", 40))
		for _, p := range stats.Phases {
			bar := renderPhaseBar(p.Duration, totalPhaseDur)
			fmt.Printf("  %-14s %8s  %s\n", p.Phase, formatDuration(p.Duration), bar)
		}
	}

	if stats.TotalCost.Total > 0 {
		fmt.Println()
		fmt.Println("Estimated Cost")
		fmt.Println(strings.Repeat("─", 40))
		fmt.Printf("  Total:       %s\n", metrics.FormatCost(stats.TotalCost.Total))
		fmt.Printf("  Input:       %s\n", metrics.FormatCost(stats.TotalCost.Input))
		fmt.Printf("  Output:      %s\n", metrics.FormatCost(stats.TotalCost.Output))
		fmt.Printf("  Cache Read:  %s\n", metrics.FormatCost(stats.TotalCost.CacheRead))
		fmt.Printf("  Cache Write: %s\n", metrics.FormatCost(stats.TotalCost.CacheWrite))
		if len(stats.CostByModel) > 1 {
			fmt.Println()
			fmt.Println("  By Model:")
			for model, cost := range stats.CostByModel {
				fmt.Printf("    %-20s %s\n", formatModelShort(model), metrics.FormatCost(cost.Total))
			}
		}
	}

	if len(stats.ToolCounts) > 0 {
		fmt.Println()
		fmt.Println("Tool Usage")
		fmt.Println(strings.Repeat("─", 40))

		// Sort by count
		var tools []metrics.ToolStat
		for name, count := range stats.ToolCounts {
			failCount := stats.ToolFailures[name]
			failRate := 0.0
			if count > 0 {
				failRate = float64(failCount) / float64(count) * 100
			}
			tools = append(tools, metrics.ToolStat{Name: name, Count: count, FailCount: failCount, FailRate: failRate})
		}
		sortTools(tools)

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		for _, t := range tools {
			fmt.Fprintf(w, "  %s\t%d calls\t%.0f%% fail\n", t.Name, t.Count, t.FailRate)
		}
		w.Flush()
	}

	if len(stats.Sessions) > 0 {
		fmt.Println()
		fmt.Println("Sessions")
		fmt.Println(strings.Repeat("─", 40))
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		for _, s := range stats.Sessions {
			dur := formatDuration(s.Duration)
			if s.Duration == 0 {
				dur = "-"
			}
			fmt.Fprintf(w, "  %s\t%s\t%s\t%s\t%d tools\n",
				truncateID(s.SessionID, 12),
				formatModelShort(s.Model),
				dur,
				metrics.FormatCost(s.Cost.Total),
				s.ToolCount,
			)
		}
		w.Flush()
	}

	return nil
}

// showProjectCSV outputs all sessions as CSV.
func showProjectCSV(root string) error {
	events, err := metrics.ReadAllEvents(root)
	if err != nil {
		return fmt.Errorf("read metrics: %w", err)
	}

	sessions := metrics.AggregateSessions(events)
	w := csv.NewWriter(os.Stdout)
	defer w.Flush()

	_ = w.Write([]string{
		"session_id", "model", "source", "started_at", "duration_sec",
		"tool_count", "tool_fails", "compacts",
		"input_tokens", "output_tokens", "cache_read", "cache_write",
		"cost_usd",
	})

	for _, s := range sessions {
		startedAt := ""
		if !s.StartedAt.IsZero() {
			startedAt = s.StartedAt.Format(time.RFC3339)
		}
		_ = w.Write([]string{
			s.SessionID, s.Model, s.Source, startedAt,
			fmt.Sprintf("%.0f", s.Duration.Seconds()),
			fmt.Sprintf("%d", s.ToolCount),
			fmt.Sprintf("%d", s.ToolFails),
			fmt.Sprintf("%d", s.Compacts),
			fmt.Sprintf("%d", s.Tokens.InputTokens),
			fmt.Sprintf("%d", s.Tokens.OutputTokens),
			fmt.Sprintf("%d", s.Tokens.CacheReadTokens),
			fmt.Sprintf("%d", s.Tokens.CacheCreationTokens),
			fmt.Sprintf("%.4f", s.Cost.Total),
		})
	}
	return nil
}

// showRecipeCSV outputs recipe sessions as CSV.
func showRecipeCSV(root, recipeID string) error {
	recipe, err := state.LoadRecipeState(root, recipeID)
	if err != nil {
		return fmt.Errorf("load recipe: %w", err)
	}

	events, err := metrics.ReadRecipeEvents(root, recipeID)
	if err != nil {
		return fmt.Errorf("read metrics: %w", err)
	}

	sessions := metrics.AggregateSessions(events)
	w := csv.NewWriter(os.Stdout)
	defer w.Flush()

	_ = w.Write([]string{
		"recipe_id", "topic", "phase",
		"session_id", "model", "source", "started_at", "duration_sec",
		"tool_count", "tool_fails", "compacts",
		"input_tokens", "output_tokens", "cache_read", "cache_write",
		"cost_usd",
	})

	for _, s := range sessions {
		startedAt := ""
		if !s.StartedAt.IsZero() {
			startedAt = s.StartedAt.Format(time.RFC3339)
		}
		_ = w.Write([]string{
			recipe.ID, recipe.Topic, recipe.Phase,
			s.SessionID, s.Model, s.Source, startedAt,
			fmt.Sprintf("%.0f", s.Duration.Seconds()),
			fmt.Sprintf("%d", s.ToolCount),
			fmt.Sprintf("%d", s.ToolFails),
			fmt.Sprintf("%d", s.Compacts),
			fmt.Sprintf("%d", s.Tokens.InputTokens),
			fmt.Sprintf("%d", s.Tokens.OutputTokens),
			fmt.Sprintf("%d", s.Tokens.CacheReadTokens),
			fmt.Sprintf("%d", s.Tokens.CacheCreationTokens),
			fmt.Sprintf("%.4f", s.Cost.Total),
		})
	}
	return nil
}

// renderPhaseBar renders an ASCII bar for a phase duration.
func renderPhaseBar(phaseDur, totalDur time.Duration) string {
	const barWidth = 16
	if totalDur <= 0 {
		return ""
	}
	pct := float64(phaseDur) / float64(totalDur) * 100
	filled := int(float64(barWidth) * phaseDur.Seconds() / totalDur.Seconds())
	if filled < 1 && phaseDur > 0 {
		filled = 1
	}
	if filled > barWidth {
		filled = barWidth
	}
	bar := strings.Repeat("#", filled) + strings.Repeat(".", barWidth-filled)
	return fmt.Sprintf("%s  %2.0f%%", bar, pct)
}

func formatTokens(n int) string {
	if n >= 1_000_000 {
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	}
	if n >= 1_000 {
		return fmt.Sprintf("%.1fK", float64(n)/1_000)
	}
	return fmt.Sprintf("%d", n)
}

func formatDuration(d time.Duration) string {
	secs := d.Seconds()
	if secs < 60 {
		return fmt.Sprintf("%.0fs", secs)
	}
	if secs < 3600 {
		return fmt.Sprintf("%dm %ds", int(secs)/60, int(secs)%60)
	}
	return fmt.Sprintf("%dh %dm", int(secs)/3600, (int(secs)%3600)/60)
}

// formatModelShort trims the "claude-" prefix for compact display.
func formatModelShort(model string) string {
	if model == "" {
		return "-"
	}
	return strings.TrimPrefix(model, "claude-")
}

// truncateID shortens a session ID for display.
func truncateID(id string, maxLen int) string {
	if len(id) <= maxLen {
		return id
	}
	return id[:maxLen]
}

func sortTools(tools []metrics.ToolStat) {
	for i := 1; i < len(tools); i++ {
		for j := i; j > 0 && tools[j].Count > tools[j-1].Count; j-- {
			tools[j], tools[j-1] = tools[j-1], tools[j]
		}
	}
}
