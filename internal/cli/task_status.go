package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

// bts task status moves an implementation task through its statuses.
//
//   bts task status <recipe-id> <task-id> <status> [--error "<text>"] [--reset-retries]
//
// The implement skill used to have the agent edit tasks.json by hand, and
// `bts check task` rewrites the file after each run, so every hand edit
// needed a fresh read first (trial problem #75, AGEN-57).

var taskStatuses = []string{"pending", "in_progress", "done", "blocked", "skipped"}

func init() {
	rootCmd.AddCommand(taskCmd)
	taskCmd.AddCommand(taskStatusCmd)
	taskStatusCmd.Flags().String("error", "", "Record why, as the task's last_error (with pending, in_progress, blocked or skipped)")
	taskStatusCmd.Flags().Bool("reset-retries", false, "Clear the retry budget (retry_count, attempts_in_tier, retry_tier), as when retrying a blocked task")
}

var taskCmd = &cobra.Command{
	Use:     "task",
	Short:   "Move implementation tasks through their statuses (tasks.json)",
	GroupID: "tools",
}

var taskStatusCmd = &cobra.Command{
	Use:   "status <recipe-id> <task-id> <" + strings.Join(taskStatuses, "|") + ">",
	Short: "Set a task's status in tasks.json; done clears its last error",
	Args:  cobra.ExactArgs(3),
	RunE: func(cmd *cobra.Command, args []string) error {
		root, err := projectRoot()
		if err != nil {
			return err
		}
		errText, _ := cmd.Flags().GetString("error")
		reset, _ := cmd.Flags().GetBool("reset-retries")
		from, err := executeTaskStatus(root, args[0], args[1], args[2], errText, reset)
		if err != nil {
			return err
		}
		fmt.Printf("[bts] %s: %s → %s\n", args[1], from, args[2])
		return nil
	},
}

// executeTaskStatus sets one task's status and returns the one it had.
// `done` clears last_error and takes no --error; the other statuses keep
// the error unless --error replaces it.  A refused move writes nothing.
func executeTaskStatus(root, recipeID, taskID, status, lastError string, resetRetries bool) (string, error) {
	valid := false
	for _, s := range taskStatuses {
		valid = valid || s == status
	}
	if !valid {
		return "", fmt.Errorf("unknown status %q: want one of %s", status, strings.Join(taskStatuses, ", "))
	}
	lastError = strings.TrimSpace(lastError)
	if status == "done" && lastError != "" {
		return "", fmt.Errorf("--error goes with a task that is not done; done clears the last error")
	}
	tasks, err := state.LoadTaskState(root, recipeID)
	if err != nil {
		return "", fmt.Errorf("load tasks.json: %w", err)
	}
	var ids []string
	for i := range tasks.Tasks {
		t := &tasks.Tasks[i]
		ids = append(ids, t.ID)
		if t.ID != taskID {
			continue
		}
		from := t.Status
		t.Status = status
		switch {
		case status == "done":
			t.LastError = ""
		case lastError != "":
			t.LastError = lastError
		}
		if resetRetries {
			t.RetryCount, t.AttemptsInTier, t.RetryTier = 0, 0, 0
		}
		tasks.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		if err := persistTasks(root, recipeID, tasks); err != nil {
			return "", fmt.Errorf("write tasks.json: %w", err)
		}
		return from, nil
	}
	return "", fmt.Errorf("task %q not found in %s/tasks.json (tasks: %s)", taskID, recipeID, strings.Join(ids, ", "))
}
