package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/imtemp-dev/jig/internal/engine"
	"github.com/imtemp-dev/jig/internal/state"
	"github.com/spf13/cobra"
)

func init() {
	retryClassifyCmd.Flags().String("last-error-file", "", "Path to a file containing the last build error (reads stdin if empty)")
	retryClassifyCmd.Flags().String("language", "", "Language hint (go/ts/python/swift) — not currently used")

	retryNextCmd.Flags().String("recipe", "", "Recipe ID (defaults to active recipe)")
	retryNextCmd.Flags().String("task", "", "Task ID (required)")
	retryNextCmd.Flags().Bool("json", false, "Emit the decision as JSON")

	retryCmd.AddCommand(retryClassifyCmd)
	retryCmd.AddCommand(retryNextCmd)
	rootCmd.AddCommand(retryCmd)
}

var retryCmd = &cobra.Command{
	Use:     "retry",
	Short:   "Retry ladder controls (Phase 15)",
	GroupID: "tools",
}

var retryClassifyCmd = &cobra.Command{
	Use:   "classify",
	Short: "Classify a build/test error as syntactic | semantic | unknown",
	RunE: func(cmd *cobra.Command, args []string) error {
		path, _ := cmd.Flags().GetString("last-error-file")
		var data []byte
		var err error
		if path == "" {
			return fmt.Errorf("--last-error-file required (stdin-piping left for a follow-up)")
		}
		data, err = os.ReadFile(path)
		if err != nil {
			return err
		}
		lang, _ := cmd.Flags().GetString("language")
		class := engine.ClassifyBuildError(string(data), lang)
		fmt.Println(class)
		return nil
	},
}

var retryNextCmd = &cobra.Command{
	Use:   "next",
	Short: "Return the next retry action for a task",
	Long: `Reads the task's retry_tier + retry_count from tasks.json, classifies
task.last_error, and emits a RetryDecision indicating the next step:
  retry_inplace       — in-place fix (tier 1)
  strategy_switch     — try a different approach (tier 2)
  spec_escalate       — re-read final.md block + /jig-verify (tier 3)
  domain_escalate     — re-verify domain.md invariants (tier 4)
  architect_escalate  — re-enter /jig-architect (tier 5)
  block               — ladder exhausted; mark task blocked

Honors settings.implement.retry_ladder for per-project tuning.`,
	RunE: runRetryNext,
}

func runRetryNext(cmd *cobra.Command, args []string) error {
	cwd, _ := os.Getwd()
	root, err := state.FindRoot(cwd)
	if err != nil {
		return fmt.Errorf("not a jig project: %w", err)
	}
	recipeID, _ := cmd.Flags().GetString("recipe")
	if recipeID == "" {
		active, _ := state.GetActiveRecipe(root)
		if active == nil {
			return fmt.Errorf("no --recipe given and no active recipe")
		}
		recipeID = active.ID
	}
	taskID, _ := cmd.Flags().GetString("task")
	if taskID == "" {
		return fmt.Errorf("--task <task-id> is required")
	}

	tasks, err := state.LoadTaskState(root, recipeID)
	if err != nil {
		return fmt.Errorf("load tasks.json: %w", err)
	}
	var task *state.Task
	for i := range tasks.Tasks {
		if tasks.Tasks[i].ID == taskID {
			task = &tasks.Tasks[i]
			break
		}
	}
	if task == nil {
		return fmt.Errorf("task %q not found in %s/tasks.json", taskID, recipeID)
	}

	settings, err := engine.LoadSettings(root)
	if err != nil {
		return fmt.Errorf("load settings: %w", err)
	}
	cfg := settings.Implement.RetryLadder.LadderConfig()
	class := engine.ClassifyBuildError(task.LastError, "")
	tier := task.RetryTier
	if tier == 0 {
		tier = 1
	}
	// attempts_in_tier is the per-tier budget counter; the skill resets
	// it to 0 on every tier transition. Legacy tasks that predate the
	// field fall back to retry_count so old recipes keep working
	// (conservative: they escalate sooner rather than looping).
	attempts := task.AttemptsInTier
	if attempts == 0 && task.RetryTier == 0 && task.RetryCount > 0 {
		attempts = task.RetryCount
	}
	decision := engine.NextRetryDecision(tier, attempts, class, cfg)

	// The engine owns the reset decision so the skill never has to infer
	// it. Compare against the DEFAULTED `tier` (not task.RetryTier): on a
	// task's first failure the stored tier is 0 but the engine ran tier 1,
	// so next_tier==1 must read as "same tier, no reset". Comparing to the
	// raw 0 would spuriously reset attempts_in_tier and, with the default
	// cap of 8 exactly covering the 3+2+1+1+1 ladder, push architect
	// escalation (tier 5) past the hard cap — unreachable again.
	resetAttempts := decision.NextTier != tier

	asJSON, _ := cmd.Flags().GetBool("json")
	if asJSON {
		data, err := json.MarshalIndent(map[string]interface{}{
			"task_id":                task.ID,
			"error_class":            string(class),
			"current_tier":           tier,
			"next_tier":              decision.NextTier,
			"action":                 string(decision.Action),
			"rationale":              decision.Rationale,
			"reset_attempts_in_tier": resetAttempts,
		}, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	} else {
		fmt.Printf("task=%s error_class=%s current_tier=%d next_tier=%d action=%s reset_attempts_in_tier=%t\n",
			task.ID, class, tier, decision.NextTier, decision.Action, resetAttempts)
		fmt.Println("rationale:", decision.Rationale)
	}
	return nil
}
