package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/imtemp-dev/jig/internal/state"
	"github.com/spf13/cobra"
)

// `jig test run` executes the project's test command itself and derives
// status from the ACTUAL exit code. Before this, test-results.json was
// hand-written by the orchestrator from its reading of framework output
// — the IMPLEMENT/FIX DONE gates trusted an LLM transcription. Status
// recorded here cannot be overridden by flags; only the descriptive
// counts can be supplemented.

func init() {
	rootCmd.AddCommand(testCmd)
	testCmd.AddCommand(testRunCmd)
	testRunCmd.Flags().String("cmd", "", "Test command, executed via sh -c at the project root (required)")
	_ = testRunCmd.MarkFlagRequired("cmd")
	testRunCmd.Flags().String("framework", "", "Framework label recorded in test-results.json")
	testRunCmd.Flags().Int("timeout", 600, "Seconds before the run is killed (kill → status=fail)")
	testRunCmd.Flags().Int("total", 0, "Optional: total test count parsed from output (informational — status always comes from the exit code)")
	testRunCmd.Flags().Int("passed", 0, "Optional: passed count (informational)")
	testRunCmd.Flags().Int("failed", 0, "Optional: failed count (informational)")
	testRunCmd.Flags().Int("skipped", 0, "Optional: skipped count (informational)")
}

var testCmd = &cobra.Command{
	Use:     "test",
	Short:   "Test execution helpers",
	GroupID: "tools",
}

var testRunCmd = &cobra.Command{
	Use:   "run <recipe-id>",
	Short: "Run the test command and record machine-truthful results (exit code → status)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, _ := os.Getwd()
		root, err := state.FindRoot(cwd)
		if err != nil {
			return fmt.Errorf("not a jig project: %w", err)
		}
		cmdStr, _ := cmd.Flags().GetString("cmd")
		framework, _ := cmd.Flags().GetString("framework")
		timeout, _ := cmd.Flags().GetInt("timeout")
		counts := testCounts{}
		counts.Total, _ = cmd.Flags().GetInt("total")
		counts.Passed, _ = cmd.Flags().GetInt("passed")
		counts.Failed, _ = cmd.Flags().GetInt("failed")
		counts.Skipped, _ = cmd.Flags().GetInt("skipped")

		tr, err := executeTestRun(root, args[0], cmdStr, framework, timeout, counts, os.Stdout)
		if err != nil {
			return err
		}
		fmt.Printf("\n[jig] test run recorded: status=%s exit=%d iteration=%d → test-results.json\n",
			tr.Status, tr.ExitCode, tr.Iterations)
		if tr.Status != "pass" {
			// Propagate failure so callers (and the implement/test loop)
			// see a non-zero exit without parsing output.
			os.Exit(1)
		}
		return nil
	},
}

type testCounts struct {
	Total, Passed, Failed, Skipped int
}

// executeTestRun runs cmdStr via sh -c at root, tees output to sink and
// to .jig/local/recipes/<id>/test-output.log, and writes
// test-results.json with status derived SOLELY from the exit code.
func executeTestRun(root, recipeID, cmdStr, framework string, timeoutSec int, counts testCounts, sink io.Writer) (*state.TestResults, error) {
	if _, err := state.LoadRecipeState(root, recipeID); err != nil {
		return nil, fmt.Errorf("load recipe %s: %w", recipeID, err)
	}
	if cmdStr == "" {
		return nil, errors.New("--cmd is required")
	}
	if timeoutSec <= 0 {
		timeoutSec = 600
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSec)*time.Second)
	defer cancel()

	var buf bytes.Buffer
	out := io.MultiWriter(sink, &buf)

	cmd := exec.CommandContext(ctx, "sh", "-c", cmdStr)
	cmd.Dir = root
	cmd.Stdout = out
	cmd.Stderr = out
	// Run in its own process group so a timeout kills the whole tree,
	// not just the shell.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = 5 * time.Second

	runErr := cmd.Run()
	timedOut := ctx.Err() == context.DeadlineExceeded

	exitCode := 0
	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			exitCode = exitErr.ExitCode()
			if exitCode < 0 {
				// Killed by signal (e.g. our timeout SIGKILL).
				exitCode = 137
			}
		} else if timedOut {
			exitCode = 137
		} else {
			// Failed to even start (sh missing etc.) — surface as error.
			return nil, fmt.Errorf("run test command: %w", runErr)
		}
	}
	if timedOut {
		fmt.Fprintf(&buf, "\n[jig] TIMEOUT: killed after %ds\n", timeoutSec)
		fmt.Fprintf(sink, "\n[jig] TIMEOUT: killed after %ds\n", timeoutSec)
	}

	status := "fail"
	if exitCode == 0 && !timedOut {
		status = "pass"
	}

	// Persist the full output for diagnosis (local/, never committed).
	logDir := filepath.Join(state.LocalPath(root), "recipes", recipeID)
	if err := os.MkdirAll(logDir, 0755); err == nil {
		_ = os.WriteFile(filepath.Join(logDir, "test-output.log"), buf.Bytes(), 0644)
	}

	iterations := 1
	if prev, err := state.LoadTestResults(root, recipeID); err == nil && prev.Iterations > 0 {
		iterations = prev.Iterations + 1
	}

	tr := &state.TestResults{
		RecipeID:   recipeID,
		RunAt:      time.Now().UTC().Format(time.RFC3339),
		Framework:  framework,
		Iterations: iterations,
		Status:     status,
		Total:      counts.Total,
		Passed:     counts.Passed,
		Failed:     counts.Failed,
		Skipped:    counts.Skipped,
		ExitCode:   exitCode,
		Command:    cmdStr,
		RecordedBy: "jig",
	}
	if err := state.SaveTestResults(root, recipeID, tr); err != nil {
		return nil, fmt.Errorf("save test results: %w", err)
	}

	if err := state.AppendChangelog(root, recipeID, &state.ChangelogEntry{
		Action: "test",
		Result: fmt.Sprintf("exit=%d status=%s iteration=%d", exitCode, status, iterations),
	}); err != nil {
		fmt.Fprintf(os.Stderr, "warning: append changelog: %v\n", err)
	}
	return tr, nil
}
