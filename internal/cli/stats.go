package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/jlim/claude-forge/internal/metrics"
	"github.com/jlim/claude-forge/internal/state"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(statsCmd)
	statsCmd.Flags().Bool("json", false, "Output as JSON")
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

	if len(args) > 0 {
		return showRecipeStats(root, args[0], jsonOutput)
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

	if stats.TotalTokens.InputTokens > 0 || stats.TotalTokens.OutputTokens > 0 {
		fmt.Println()
		fmt.Println("Token Usage")
		fmt.Println(strings.Repeat("─", 40))
		fmt.Printf("  Input:       %s\n", formatTokens(stats.TotalTokens.InputTokens))
		fmt.Printf("  Output:      %s\n", formatTokens(stats.TotalTokens.OutputTokens))
		fmt.Printf("  Cache Read:  %s\n", formatTokens(stats.TotalTokens.CacheReadTokens))
		fmt.Printf("  Cache Write: %s\n", formatTokens(stats.TotalTokens.CacheCreationTokens))
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

	if len(stats.Phases) > 0 {
		fmt.Println()
		fmt.Println("Phase Timeline")
		fmt.Println(strings.Repeat("─", 40))
		for _, p := range stats.Phases {
			fmt.Printf("  %-14s %s\n", p.Phase, formatDuration(p.Duration))
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

	return nil
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

func formatDuration(d interface{}) string {
	switch v := d.(type) {
	case int64:
		return formatDurationFromNano(v)
	default:
		// time.Duration
		dur, ok := d.(interface{ Seconds() float64 })
		if !ok {
			return "?"
		}
		secs := dur.Seconds()
		if secs < 60 {
			return fmt.Sprintf("%.0fs", secs)
		}
		if secs < 3600 {
			return fmt.Sprintf("%dm %ds", int(secs)/60, int(secs)%60)
		}
		return fmt.Sprintf("%dh %dm", int(secs)/3600, (int(secs)%3600)/60)
	}
}

func formatDurationFromNano(ns int64) string {
	secs := float64(ns) / 1e9
	if secs < 60 {
		return fmt.Sprintf("%.0fs", secs)
	}
	if secs < 3600 {
		return fmt.Sprintf("%dm %ds", int(secs)/60, int(secs)%60)
	}
	return fmt.Sprintf("%dh %dm", int(secs)/3600, (int(secs)%3600)/60)
}

func sortTools(tools []metrics.ToolStat) {
	for i := 1; i < len(tools); i++ {
		for j := i; j > 0 && tools[j].Count > tools[j-1].Count; j-- {
			tools[j], tools[j-1] = tools[j-1], tools[j]
		}
	}
}
