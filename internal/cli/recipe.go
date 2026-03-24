package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/imtemp-dev/claude-forge/internal/metrics"
	"github.com/imtemp-dev/claude-forge/internal/state"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(recipeCmd)
	recipeCmd.AddCommand(recipeStatusCmd, recipeListCmd, recipeLogCmd, recipeCancelCmd)
}

var recipeCmd = &cobra.Command{
	Use:     "recipe",
	Short:   "Manage recipe execution state",
	GroupID: "recipe",
}

var recipeStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show active recipe status",
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, _ := os.Getwd()
		root, err := state.FindRoot(cwd)
		if err != nil {
			return fmt.Errorf("not a forge project: %w", err)
		}

		recipe, err := state.GetActiveRecipe(root)
		if err != nil {
			return fmt.Errorf("read state: %w", err)
		}

		if recipe == nil {
			fmt.Println("No active recipe.")
			return nil
		}

		fmt.Printf("Active recipe: %s\n", recipe.ID)
		fmt.Printf("  Type:         %s\n", recipe.Type)
		fmt.Printf("  Topic:        %s\n", recipe.Topic)
		fmt.Printf("  Phase:        %s\n", recipe.Phase)
		fmt.Printf("  Iteration:    %d\n", recipe.Iteration)
		if recipe.DraftVersion > 0 {
			fmt.Printf("  Draft:        v%d\n", recipe.DraftVersion)
		}
		fmt.Printf("  Level:        %.1f\n", recipe.Level)
		fmt.Printf("  Started:      %s\n", recipe.StartedAt)
		fmt.Printf("  Updated:      %s\n", recipe.UpdatedAt)
		return nil
	},
}

var recipeListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all recipes",
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, _ := os.Getwd()
		root, err := state.FindRoot(cwd)
		if err != nil {
			return fmt.Errorf("not a forge project: %w", err)
		}

		recipes, err := state.ListRecipes(root)
		if err != nil {
			return fmt.Errorf("list: %w", err)
		}

		if len(recipes) == 0 {
			fmt.Println("No recipes found.")
			return nil
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tType\tTopic\tPhase\tIteration\tUpdated")
		for _, r := range recipes {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d\t%s\n",
				r.ID, r.Type, truncate(r.Topic, 30), r.Phase, r.Iteration, r.UpdatedAt)
		}
		w.Flush()
		return nil
	},
}

var recipeLogCmd = &cobra.Command{
	Use:   "log <recipe-id>",
	Short: "Record an action or verify iteration (called by skills via Bash)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, _ := os.Getwd()
		root, err := state.FindRoot(cwd)
		if err != nil {
			return fmt.Errorf("not a forge project: %w", err)
		}

		recipeID := args[0]
		action, _ := cmd.Flags().GetString("action")
		phase, _ := cmd.Flags().GetString("phase")

		// Update phase if specified (independent of action/iteration mode)
		if phase != "" {
			recipe, err := state.LoadRecipeState(root, recipeID)
			if err != nil {
				return fmt.Errorf("load recipe: %w", err)
			}

			// Pre-condition checks for phase transition
			if err := checkPhasePreConditions(root, recipe, phase); err != nil {
				return err
			}

			previousPhase := recipe.Phase
			recipe.Phase = phase
			if err := state.SaveRecipeState(root, recipe); err != nil {
				return fmt.Errorf("save recipe: %w", err)
			}
			_ = metrics.Append(root, &metrics.MetricsEvent{
				Kind:          metrics.KindPhaseChange,
				RecipeID:      recipeID,
				Phase:         phase,
				PreviousPhase: previousPhase,
			})
			fmt.Printf("Phase → %s\n", phase)
		}

		if action != "" {
			// Changelog mode: log an action
			output, _ := cmd.Flags().GetString("output")
			basedOn, _ := cmd.Flags().GetString("based-on")
			docType, _ := cmd.Flags().GetString("doc-type")
			result, _ := cmd.Flags().GetString("result")
			gaps, _ := cmd.Flags().GetInt("gaps")

			entry := &state.ChangelogEntry{
				Action: action,
				Output: output,
				Result: result,
			}
			if basedOn != "" {
				entry.BasedOn = []string{basedOn}
			}
			if gaps > 0 {
				entry.Result = fmt.Sprintf("%d gaps found", gaps)
			}

			if err := state.AppendChangelog(root, recipeID, entry); err != nil {
				return fmt.Errorf("changelog: %w", err)
			}

			// Update manifest if output specified
			if output != "" {
				manifest, _ := state.LoadManifest(root, recipeID)
				var deps []string
				if basedOn != "" {
					deps = []string{basedOn}
				}
				// Use explicit doc-type if given, otherwise infer from action
				manifestType := docType
				if manifestType == "" {
					manifestType = actionToDocType(action)
				}
				manifest.AddDocument(output, manifestType, deps)
				if err := state.SaveManifest(root, recipeID, manifest); err != nil {
					fmt.Fprintf(os.Stderr, "warning: save manifest: %v\n", err)
				}
			}

			fmt.Printf("Logged action: %s → %s\n", action, output)
		} else if phase == "" {
			// Verify-log mode: log an iteration result (backward compatible)
			iteration, _ := cmd.Flags().GetInt("iteration")
			critical, _ := cmd.Flags().GetInt("critical")
			major, _ := cmd.Flags().GetInt("major")
			minor, _ := cmd.Flags().GetInt("minor")

			status := "continue"
			if critical == 0 && major == 0 {
				status = "converged"
			}

			entry := &state.VerifyLogEntry{
				Iteration: iteration,
				Critical:  critical,
				Major:     major,
				Minor:     minor,
				Status:    status,
			}

			if err := state.AppendVerifyLog(root, recipeID, entry); err != nil {
				return fmt.Errorf("log: %w", err)
			}

			// Also log to changelog
			if err := state.AppendChangelog(root, recipeID, &state.ChangelogEntry{
				Action: "verify",
				Result: fmt.Sprintf("critical=%d major=%d minor=%d → %s", critical, major, minor, status),
			}); err != nil {
				fmt.Fprintf(os.Stderr, "warning: append changelog: %v\n", err)
			}

			fmt.Printf("Logged iteration %d: critical=%d major=%d minor=%d → %s\n",
				iteration, critical, major, minor, status)
		}

		return nil
	},
}

var recipeCancelCmd = &cobra.Command{
	Use:   "cancel",
	Short: "Cancel the active recipe",
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, _ := os.Getwd()
		root, err := state.FindRoot(cwd)
		if err != nil {
			return fmt.Errorf("not a forge project: %w", err)
		}

		recipe, err := state.GetActiveRecipe(root)
		if err != nil || recipe == nil {
			fmt.Println("No active recipe to cancel.")
			return nil
		}

		recipe.Phase = "cancelled"
		if err := state.SaveRecipeState(root, recipe); err != nil {
			return fmt.Errorf("save: %w", err)
		}

		fmt.Printf("Recipe %s cancelled.\n", recipe.ID)
		return nil
	},
}

func init() {
	// Verify-log flags (backward compatible)
	recipeLogCmd.Flags().Int("iteration", 0, "Iteration number")
	recipeLogCmd.Flags().Int("critical", 0, "Critical error count")
	recipeLogCmd.Flags().Int("major", 0, "Major error count")
	recipeLogCmd.Flags().Int("minor", 0, "Minor error count")
	// Changelog flags
	recipeLogCmd.Flags().String("action", "", "Action type (research, improve, verify, debate, simulate, audit, assess, implement, test, sync, status)")
	recipeLogCmd.Flags().String("output", "", "Output file path")
	recipeLogCmd.Flags().String("based-on", "", "Dependency document path")
	recipeLogCmd.Flags().String("doc-type", "", "Manifest document type (overrides auto-detection from action)")
	recipeLogCmd.Flags().String("result", "", "Summary of outcome")
	recipeLogCmd.Flags().Int("gaps", 0, "Number of gaps found (for simulate)")
	// Phase flag
	recipeLogCmd.Flags().String("phase", "", "Update recipe phase (implement, test, sync, status, etc.)")
}

// actionToDocType maps changelog action names to manifest document types.
func actionToDocType(action string) string {
	switch action {
	case "research":
		return "research"
	case "draft", "improve":
		return "draft"
	case "debate":
		return "debate"
	case "simulate":
		return "simulation"
	case "verify", "audit", "assess", "sync-check":
		return "verification"
	case "implement":
		return "implementation"
	case "test":
		return "test-result"
	case "sync":
		return "deviation"
	case "adjudicate":
		return "verification"
	default:
		return action
	}
}

// checkPhasePreConditions warns about missing prerequisites for a phase transition.
// Warnings go to stderr; phase transition always proceeds (warn, not block).
func checkPhasePreConditions(root string, recipe *state.RecipeState, newPhase string) error {
	recipeDir := state.RecipeDir(root, recipe.ID)
	exists := func(name string) bool {
		_, err := os.Stat(filepath.Join(recipeDir, name))
		return err == nil
	}
	stateExists := func(name string) bool {
		_, err := os.Stat(filepath.Join(state.StatePath(root), name))
		return err == nil
	}
	warn := func(msg string) {
		fmt.Fprintf(os.Stderr, "⚠ %s\n", msg)
	}

	switch newPhase {
	case "complete", "finalize":
		fmt.Fprintf(os.Stderr, "✗ Phase '%s' is protected — set automatically by completion gates.\n", newPhase)
		fmt.Fprintf(os.Stderr, "  Output <forge>DONE</forge>, <forge>IMPLEMENT DONE</forge>, or <forge>FIX DONE</forge> to complete.\n")
		return fmt.Errorf("phase '%s' is protected", newPhase)

	case "research":
		if recipe.Type == "blueprint" && !stateExists("project-map.md") {
			warn("project-map.md not found — scan codebase to create it")
		}

	case "implement":
		if !exists("final.md") {
			warn("final.md not found — complete spec before implementing")
		}

	case "test":
		if recipe.Type != "fix" && !exists("tasks.json") {
			warn("tasks.json not found — run /forge-implement to decompose tasks")
		}

	case "review":
		if exists("test-results.json") {
			data, _ := os.ReadFile(filepath.Join(recipeDir, "test-results.json"))
			var tr state.TestResults
			if json.Unmarshal(data, &tr) == nil && tr.Status != "pass" {
				warn("tests not passing — fix before review")
			}
		}
		simsDir := filepath.Join(recipeDir, "simulations")
		if entries, err := os.ReadDir(simsDir); err != nil || countNonHidden(entries) == 0 {
			warn("no code simulation found — run /forge-simulate code first")
		}

	case "sync":
		if !exists("review.md") {
			warn("review.md not found — run /forge-review first")
		}

	case "status":
		if !exists("deviation.md") {
			warn("deviation.md not found — run /forge-sync first")
		}
	}

	return nil
}

func countNonHidden(entries []os.DirEntry) int {
	count := 0
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), ".") {
			count++
		}
	}
	return count
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}
