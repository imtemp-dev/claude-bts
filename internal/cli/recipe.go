package cli

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/jlim/bts/internal/state"
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
		btsRoot, err := state.FindBTSRoot(cwd)
		if err != nil {
			return fmt.Errorf("not a bts project: %w", err)
		}

		recipe, err := state.GetActiveRecipe(btsRoot)
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
		fmt.Printf("  Draft:        v%d\n", recipe.DraftVersion)
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
		btsRoot, err := state.FindBTSRoot(cwd)
		if err != nil {
			return fmt.Errorf("not a bts project: %w", err)
		}

		recipes, err := state.ListRecipes(btsRoot)
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
		btsRoot, err := state.FindBTSRoot(cwd)
		if err != nil {
			return fmt.Errorf("not a bts project: %w", err)
		}

		recipeID := args[0]
		action, _ := cmd.Flags().GetString("action")
		phase, _ := cmd.Flags().GetString("phase")

		// Update phase if specified (independent of action/iteration mode)
		if phase != "" {
			recipe, err := state.LoadRecipeState(btsRoot, recipeID)
			if err != nil {
				return fmt.Errorf("load recipe: %w", err)
			}
			recipe.Phase = phase
			if err := state.SaveRecipeState(btsRoot, recipe); err != nil {
				return fmt.Errorf("save recipe: %w", err)
			}
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

			if err := state.AppendChangelog(btsRoot, recipeID, entry); err != nil {
				return fmt.Errorf("changelog: %w", err)
			}

			// Update manifest if output specified
			if output != "" {
				manifest, _ := state.LoadManifest(btsRoot, recipeID)
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
				_ = state.SaveManifest(btsRoot, recipeID, manifest)
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

			if err := state.AppendVerifyLog(btsRoot, recipeID, entry); err != nil {
				return fmt.Errorf("log: %w", err)
			}

			// Also log to changelog
			_ = state.AppendChangelog(btsRoot, recipeID, &state.ChangelogEntry{
				Action: "verify",
				Result: fmt.Sprintf("critical=%d major=%d minor=%d → %s", critical, major, minor, status),
			})

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
		btsRoot, err := state.FindBTSRoot(cwd)
		if err != nil {
			return fmt.Errorf("not a bts project: %w", err)
		}

		recipe, err := state.GetActiveRecipe(btsRoot)
		if err != nil || recipe == nil {
			fmt.Println("No active recipe to cancel.")
			return nil
		}

		recipe.Phase = "cancelled"
		if err := state.SaveRecipeState(btsRoot, recipe); err != nil {
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

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}
