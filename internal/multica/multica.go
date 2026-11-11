// Package multica connects bts to a Multica run: it finds the task the run
// belongs to and mirrors a recipe's progress onto the Multica issue.
//
// A Multica daemon runs each task in its own directory and writes
// workdir/.multica/daemon_task_context.json there; Claude starts in workdir/,
// and the repository is a git worktree one level below it.  No environment
// variable names the issue, so the context file is the only source.
package multica

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

// ContextFile is where the Multica daemon describes a task, relative to the
// task's workdir.
const ContextFile = ".multica/daemon_task_context.json"

// contextSearchDepth is how many directories FindTaskContext climbs from a
// bts root: the root itself (a run in local-directory mode starts in the
// repository) and the workdir above a checked-out worktree, with one to spare.
const contextSearchDepth = 3

// TaskContext is the part of daemon_task_context.json bts reads.  An issue
// task has IssueID; a chat task has ChatSessionID instead.
type TaskContext struct {
	IssueID       string `json:"issue_id"`
	AgentID       string `json:"agent_id"`
	ChatSessionID string `json:"chat_session_id"`
	ManagedBy     string `json:"managed_by"`
	// Dir is the workdir that holds .multica/.
	Dir string `json:"-"`
}

// FindTaskContext returns the Multica task the directory dir belongs to,
// looking in dir and the directories above it, or nil outside Multica.
func FindTaskContext(dir string) *TaskContext {
	d, err := filepath.Abs(dir)
	if err != nil {
		return nil
	}
	for i := 0; i < contextSearchDepth; i++ {
		data, err := os.ReadFile(filepath.Join(d, filepath.FromSlash(ContextFile)))
		if err == nil {
			var tc TaskContext
			if json.Unmarshal(data, &tc) == nil {
				tc.Dir = d
				return &tc
			}
			return nil
		}
		parent := filepath.Dir(d)
		if parent == d {
			break
		}
		d = parent
	}
	return nil
}

// notifyBudget bounds every metadata write a phase change makes.  The Stop
// hook, which finalizes and completes recipes, has 10 s in all.
const notifyBudget = 4 * time.Second

// runCLI runs the multica CLI.  Tests replace it.  The CLI authenticates from
// the run's environment (MULTICA_TOKEN, MULTICA_SERVER_URL), which every
// command and hook in the run inherits.
var runCLI = func(ctx context.Context, args ...string) error {
	path, err := exec.LookPath("multica")
	if err != nil {
		return fmt.Errorf("multica CLI not on PATH")
	}
	out, err := exec.CommandContext(ctx, path, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("multica %s: %v: %s", args[0], err, firstLine(out))
	}
	return nil
}

// SetCLIRunner replaces how the multica CLI is run and returns a function that
// restores the previous runner.  For tests in other packages, which must
// never reach a real Multica server.
func SetCLIRunner(f func(ctx context.Context, args ...string) error) (restore func()) {
	prev := runCLI
	runCLI = f
	return func() { runCLI = prev }
}

func firstLine(b []byte) string {
	s := string(b)
	for i, r := range s {
		if r == '\n' {
			return s[:i]
		}
	}
	return s
}

// SetMetadata writes one key on an issue's metadata.
func SetMetadata(ctx context.Context, issueID, key, value string) error {
	return runCLI(ctx, "issue", "metadata", "set", issueID, "--key", key, "--value", value)
}

// OnPhaseChange mirrors a recipe's phase onto its Multica issue: bts.recipe_id
// and bts.phase.  It is state.PhaseObserver.  A recipe with no MulticaIssue
// is left alone, and a failure is a warning on stderr, never an error: the
// issue shows progress, it does not gate it.
func OnPhaseChange(root, recipeID, from, to string) {
	r, err := state.LoadRecipeState(root, recipeID)
	if err != nil || r == nil || r.MulticaIssue == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), notifyBudget)
	defer cancel()
	for _, kv := range [][2]string{{"bts.recipe_id", recipeID}, {"bts.phase", to}} {
		if err := SetMetadata(ctx, r.MulticaIssue, kv[0], kv[1]); err != nil {
			fmt.Fprintf(os.Stderr, "[bts] warning: Multica issue %s not updated (%s=%s): %v\n", r.MulticaIssue, kv[0], kv[1], err)
			return
		}
	}
}
