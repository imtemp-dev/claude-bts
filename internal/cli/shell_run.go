package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"syscall"
	"time"
)

// shellRun is the outcome of one command bts executed itself, so the
// result comes from an exit code rather than from an agent's reading of
// the output.
type shellRun struct {
	ExitCode int
	TimedOut bool
	Output   []byte
}

// runShellCommand runs cmdStr via `sh -c` at root, tees its output to sink,
// and returns the exit code. The command runs in its own process group so
// a timeout kills the whole tree, not just the shell; a timeout or a kill
// by signal reads as exit 137. An error means the command could not start.
func runShellCommand(root, cmdStr string, timeoutSec int, sink io.Writer) (*shellRun, error) {
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
		switch {
		case errors.As(runErr, &exitErr):
			exitCode = exitErr.ExitCode()
			if exitCode < 0 {
				exitCode = 137 // killed by signal, e.g. our timeout SIGKILL
			}
		case timedOut:
			exitCode = 137
		default:
			return nil, runErr
		}
	}
	if timedOut {
		fmt.Fprintf(&buf, "\n[bts] TIMEOUT: killed after %ds\n", timeoutSec)
		fmt.Fprintf(sink, "\n[bts] TIMEOUT: killed after %ds\n", timeoutSec)
	}
	return &shellRun{ExitCode: exitCode, TimedOut: timedOut, Output: buf.Bytes()}, nil
}
