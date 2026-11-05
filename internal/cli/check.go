package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/imtemp-dev/claude-bts/internal/engine"
	"github.com/imtemp-dev/claude-bts/internal/state"
	"github.com/spf13/cobra"
)

func init() {
	checkCmd.AddCommand(checkTaskCmd)
	checkTaskCmd.Flags().String("recipe", "", "Recipe ID (defaults to active recipe)")
	checkTaskCmd.Flags().String("task", "", "Task ID to check (required)")
	checkTaskCmd.Flags().Bool("write", false, "Persist findings to tasks.json")
	checkTaskCmd.Flags().Bool("json", false, "Emit findings as JSON")

	rootCmd.AddCommand(checkCmd)
}

var checkCmd = &cobra.Command{
	Use:     "check",
	Short:   "Structural checks during implementation",
	GroupID: "tools",
}

var checkTaskCmd = &cobra.Command{
	Use:   "task",
	Short: "Run per-task MINI-CHECK (Phase 10): import drift, symbol presence, modify scope, owner delta",
	Long: `bts check task --recipe {id} --task {task-id}

Runs the deterministic per-task checks defined in Phase 10:
  - import_drift:    file imports modules not listed as neighbors in
                     wireframe.md's component diagram
  - symbol_missing:  a modify_scope symbol is no longer in the file
  - scope_violation: a modify task changed a public top-level symbol
                     outside its modify_scope, or a private helper that
                     something outside the scope uses (compared with the
                     file at git HEAD; Python, Go, TypeScript/JavaScript)
  - owner_drift:     the invariant-owner module identifier disappeared
                     from the file

Findings are advisory — they do not block the task. Pass --write to
persist them onto tasks.json for downstream review (mid-run review in
Phase 11, final bts-review at completion).`,
	RunE: runCheckTask,
}

func runCheckTask(cmd *cobra.Command, args []string) error {
	cwd, _ := os.Getwd()
	root, err := state.FindRoot(cwd)
	if err != nil {
		return fmt.Errorf("not a bts project: %w", err)
	}

	recipeID, _ := cmd.Flags().GetString("recipe")
	if recipeID == "" {
		active, _ := state.GetActiveRecipe(root)
		if active == nil {
			return fmt.Errorf("no --recipe given and no active recipe found")
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

	// Find the target task.
	taskIdx := -1
	for i := range tasks.Tasks {
		if tasks.Tasks[i].ID == taskID {
			taskIdx = i
			break
		}
	}
	if taskIdx < 0 {
		return fmt.Errorf("task %q not found in %s/tasks.json", taskID, recipeID)
	}

	recipeDir := state.RecipeDir(root, recipeID)
	findings := engine.CheckTaskStructure(root, recipeDir, &tasks.Tasks[taskIdx])

	asJSON, _ := cmd.Flags().GetBool("json")
	if asJSON {
		data, err := json.MarshalIndent(findings, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	} else if len(findings) == 0 {
		fmt.Printf("Task %s: no structural findings.\n", taskID)
	} else {
		fmt.Printf("Task %s: %d finding(s)\n", taskID, len(findings))
		for _, f := range findings {
			fmt.Printf("  [%s] %s — %s\n", f.Severity, f.Category, f.Detail)
		}
	}

	// Persist when requested so the mid-run review / final review can
	// cite findings without recomputing them.
	shouldWrite, _ := cmd.Flags().GetBool("write")
	if shouldWrite {
		tasks.Tasks[taskIdx].StructureFindings = findings
		if err := persistTasks(root, recipeID, tasks); err != nil {
			return fmt.Errorf("write tasks.json: %w", err)
		}
	}

	// Exit non-zero on critical so CI / the skill loop can detect
	// hard failures; majors and minors are advisory.
	for _, f := range findings {
		if f.Severity == "critical" {
			os.Exit(2)
		}
	}
	return nil
}

func persistTasks(root, recipeID string, tasks *state.TaskState) error {
	data, err := json.MarshalIndent(tasks, "", "  ")
	if err != nil {
		return err
	}
	path := state.RecipeDir(root, recipeID) + "/tasks.json"
	return os.WriteFile(path, append(data, '\n'), 0644)
}
