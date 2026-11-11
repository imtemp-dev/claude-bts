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
	"strings"
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

// Runner runs the multica CLI with stdin and returns its standard output.
type Runner func(ctx context.Context, stdin string, args ...string) ([]byte, error)

// runner runs the multica CLI.  Tests replace it.  The CLI authenticates from
// the run's environment (MULTICA_TOKEN, MULTICA_SERVER_URL), which every
// command and hook in the run inherits; outside a run it uses the profile
// `multica login` set up.
var runner Runner = func(ctx context.Context, stdin string, args ...string) ([]byte, error) {
	path, err := exec.LookPath("multica")
	if err != nil {
		return nil, fmt.Errorf("multica CLI not on PATH")
	}
	cmd := exec.CommandContext(ctx, path, args...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := firstLine([]byte(stderr.String()))
		if msg == "" {
			msg = firstLine(out)
		}
		return out, fmt.Errorf("multica %s: %v: %s", strings.Join(args[:min(2, len(args))], " "), err, msg)
	}
	return out, nil
}

func runCLI(ctx context.Context, args ...string) error {
	_, err := runner(ctx, "", args...)
	return err
}

// Run runs the multica CLI and returns its standard output.
func Run(ctx context.Context, stdin string, args ...string) ([]byte, error) {
	return runner(ctx, stdin, args...)
}

// SetRunner replaces how the multica CLI is run and returns a function that
// restores the previous runner.  For tests in other packages, which must
// never reach a real Multica server.
func SetRunner(f Runner) (restore func()) {
	prev := runner
	runner = f
	return func() { runner = prev }
}

// SetCLIRunner is SetRunner for a fake that needs no stdin or output.
func SetCLIRunner(f func(ctx context.Context, args ...string) error) (restore func()) {
	return SetRunner(func(ctx context.Context, _ string, args ...string) ([]byte, error) {
		return nil, f(ctx, args...)
	})
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
