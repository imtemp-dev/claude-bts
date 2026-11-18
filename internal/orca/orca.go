// Package orca drives the CLI of Orca (Stably AI's desktop app for coding
// agents, https://github.com/stablyai/orca) so bts can start a Claude Code
// session in one of Orca's terminals.  bts keeps the recipe's state and
// gates; Orca keeps the sessions, terminals and worktrees
// (docs/research/orca-orchestration-design.md).
package orca

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Runner runs the orca CLI in dir and returns its standard output.
type Runner func(ctx context.Context, dir string, args ...string) ([]byte, error)

var runner Runner = runCLI

// SetRunner replaces how the orca CLI is run and returns a function that
// restores the previous runner.  Tests never reach a real Orca.
func SetRunner(f Runner) (restore func()) {
	prev := runner
	runner = f
	return func() { runner = prev }
}

// appBundles are where the macOS app keeps its CLI, which is not on a login
// shell's PATH outside Orca's own terminals.
var appBundles = []string{
	"/Applications/Orca.app/Contents/Resources/bin/orca",
	"~/Applications/Orca.app/Contents/Resources/bin/orca",
}

// InOrca reports whether this process runs in a terminal Orca manages:
// Orca sets ORCA_PANE_KEY (or the older ORCA_AGENT_PANE) there, and hooks
// inherit it from the Claude Code session they run in.
func InOrca() bool {
	return os.Getenv("ORCA_PANE_KEY") != "" || os.Getenv("ORCA_AGENT_PANE") != ""
}

// Binary finds the orca CLI the way Orca's own guide does: ORCA_CLI_COMMAND
// when Orca set it; on Linux `orca-ide`, or `orca` only inside an Orca
// terminal, because outside one `orca` is the GNOME screen reader; `orca`
// on PATH elsewhere; then the macOS app bundle.
func Binary() (string, error) {
	if c := strings.TrimSpace(os.Getenv("ORCA_CLI_COMMAND")); c != "" {
		return c, nil
	}
	if runtime.GOOS == "linux" {
		if p, err := exec.LookPath("orca-ide"); err == nil {
			return p, nil
		}
		if !InOrca() {
			return "", errors.New("the Orca CLI was not found: on Linux bts runs `orca-ide`, or `orca` inside an Orca terminal")
		}
	}
	if p, err := exec.LookPath("orca"); err == nil {
		return p, nil
	}
	home, _ := os.UserHomeDir()
	for _, p := range appBundles {
		if strings.HasPrefix(p, "~/") {
			p = filepath.Join(home, p[2:])
		}
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p, nil
		}
	}
	return "", errors.New("the Orca CLI was not found: install Orca (https://github.com/stablyai/orca) or set ORCA_CLI_COMMAND")
}

func runCLI(ctx context.Context, dir string, args ...string) ([]byte, error) {
	bin, err := Binary()
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = dir
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil && len(out) == 0 {
		return nil, fmt.Errorf("orca %s: %v: %s", strings.Join(args[:min(2, len(args))], " "), err, strings.TrimSpace(stderr.String()))
	}
	// A refused command still prints its JSON envelope, which names the
	// error better than the exit status does.
	return out, nil
}

// Error is a refusal Orca reported in its JSON envelope.
type Error struct {
	Code    string
	Message string
}

func (e *Error) Error() string { return fmt.Sprintf("orca refused (%s): %s", e.Code, e.Message) }

// call runs a command with --json and returns the envelope's result.
func call(ctx context.Context, dir string, args ...string) (json.RawMessage, error) {
	out, err := runner(ctx, dir, append(args, "--json")...)
	if err != nil {
		return nil, err
	}
	var env struct {
		OK     bool            `json:"ok"`
		Result json.RawMessage `json:"result"`
		Error  *Error          `json:"error"`
	}
	if err := json.Unmarshal(out, &env); err != nil {
		return nil, fmt.Errorf("orca %s: unreadable reply: %v", args[0], err)
	}
	if !env.OK {
		if env.Error != nil {
			return nil, env.Error
		}
		return nil, fmt.Errorf("orca %s: refused without a reason", args[0])
	}
	return env.Result, nil
}

// Status is what `orca status` says about the app and its runtime.
type Status struct {
	Running   bool
	Reachable bool
	Version   string
}

// GetStatus asks whether Orca is running and its runtime reachable.
func GetStatus(ctx context.Context, dir string) (*Status, error) {
	res, err := call(ctx, dir, "status")
	if err != nil {
		return nil, err
	}
	var r struct {
		App struct {
			Running bool `json:"running"`
		} `json:"app"`
		Runtime struct {
			Reachable  bool   `json:"reachable"`
			AppVersion string `json:"appVersion"`
		} `json:"runtime"`
	}
	if err := json.Unmarshal(res, &r); err != nil {
		return nil, fmt.Errorf("orca status: %v", err)
	}
	return &Status{Running: r.App.Running, Reachable: r.Runtime.Reachable, Version: r.Runtime.AppVersion}, nil
}

// Launch is the agent session `orca agent launch` started.
type Launch struct {
	Kind        string // "terminal" or "chat"
	Terminal    string // the terminal handle, for a terminal agent
	PaneKey     string
	WorktreeID  string
	OperationID string // what `--retry-launch` takes if the reply is lost
}

// WorkerSpec is one supervised worker to start in a child worktree of the
// caller's: `orca orchestration worker-start --worktree new-child`.
type WorkerSpec struct {
	SpecFile string // the task the worker gets, self-contained
	Title    string // the task's title
	Name     string // the child worktree's name
	Base     string // the branch the child worktree starts from
	Agent    string
	Model    string // "" for the person's own settings
	Run      string // the Run to join; "" makes Orca bind a new one to the caller
}

// Worker is what worker-start reported.
type Worker struct {
	Dispatch   string
	Task       string
	Run        string
	RunCreated bool
	State      string // "ready" once started; "outcome_unknown" when it was typed in but not seen starting
	Terminal   string
	Workspace  string // Orca's id of the worker's worktree
	Next       string // what Orca says to do next, if anything
}

// WorkerStart starts a supervised worker.  It must run from the
// coordinator's own Orca terminal: Orca binds the Run, and the worker's
// questions and reports, to the session that started it.
func WorkerStart(ctx context.Context, dir string, w WorkerSpec) (*Worker, error) {
	args := []string{"orchestration", "worker-start", "--spec-file", w.SpecFile, "--worktree", "new-child",
		"--name", w.Name, "--agent", w.Agent}
	if w.Base != "" {
		args = append(args, "--base-branch", w.Base)
	}
	if w.Title != "" {
		args = append(args, "--task-title", w.Title)
	}
	if w.Model != "" {
		args = append(args, "--model", w.Model)
	}
	if w.Run != "" {
		args = append(args, "--run", w.Run)
	}
	res, err := call(ctx, dir, args...)
	if err != nil {
		return nil, err
	}
	var r struct {
		DispatchID string `json:"dispatchId"`
		TaskID     string `json:"taskId"`
		RunID      string `json:"runId"`
		Run        *struct {
			ID      string `json:"id"`
			Created bool   `json:"created"`
		} `json:"run"`
		RunCreated  bool             `json:"runCreated"`
		State       string           `json:"state"`
		Terminal    string           `json:"terminal"`
		Workspace   string           `json:"workspace"`
		Effects     []map[string]any `json:"effects"`
		FailedStage string           `json:"failedStage"`
		LastError   string           `json:"lastError"`
		Next        string           `json:"next"`
	}
	if err := json.Unmarshal(res, &r); err != nil {
		return nil, fmt.Errorf("orca orchestration worker-start: %v", err)
	}
	if r.DispatchID == "" {
		return nil, fmt.Errorf("orca orchestration worker-start: no dispatch in the reply (state %q, failed stage %q: %s)", r.State, r.FailedStage, r.LastError)
	}
	out := &Worker{Dispatch: r.DispatchID, Task: r.TaskID, Run: r.RunID, RunCreated: r.RunCreated,
		State: r.State, Terminal: r.Terminal, Workspace: r.Workspace, Next: r.Next}
	if r.Run != nil {
		if out.Run == "" {
			out.Run = r.Run.ID
		}
		out.RunCreated = out.RunCreated || r.Run.Created
	}
	for _, e := range r.Effects {
		id, _ := e["id"].(string)
		switch e["kind"] {
		case "worktree":
			if out.Workspace == "" {
				out.Workspace = id
			}
		case "terminal":
			if out.Terminal == "" {
				out.Terminal = id
			}
		}
	}
	if r.FailedStage != "" && r.State != "ready" && r.State != "outcome_unknown" {
		return out, fmt.Errorf("orca worker %s failed at %s: %s", r.DispatchID, r.FailedStage, r.LastError)
	}
	return out, nil
}

// Send mails a message to one worker (dispatch id).  A working worker gets
// it when its turn ends; a finished one gets it as a follow-up task.
func Send(ctx context.Context, dir, dispatch, subject, body string) error {
	_, err := call(ctx, dir, "orchestration", "send", "--to", "dispatch:"+dispatch, "--subject", subject, "--body", body)
	return err
}

// LaunchAgent starts agent in a new terminal of the Orca worktree that
// holds dir, and submits prompt once the agent is ready.  model is passed
// only when set; otherwise the agent starts with the person's own settings.
func LaunchAgent(ctx context.Context, dir, agent, model, prompt string) (*Launch, error) {
	args := []string{"agent", "launch", "--agent", agent, "--surface", "terminal", "--prompt", prompt}
	if model != "" {
		args = append(args, "--model", model)
	}
	res, err := call(ctx, dir, args...)
	if err != nil {
		return nil, err
	}
	var r struct {
		Outcome struct {
			Kind    string `json:"kind"`
			Handle  string `json:"handle"`
			PaneKey string `json:"paneKey"`
		} `json:"outcome"`
		WorktreeID  string `json:"worktreeId"`
		OperationID string `json:"operationId"`
	}
	if err := json.Unmarshal(res, &r); err != nil {
		return nil, fmt.Errorf("orca agent launch: %v", err)
	}
	return &Launch{Kind: r.Outcome.Kind, Terminal: r.Outcome.Handle, PaneKey: r.Outcome.PaneKey,
		WorktreeID: r.WorktreeID, OperationID: r.OperationID}, nil
}
