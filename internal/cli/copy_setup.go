package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/imtemp-dev/claude-bts/internal/engine"
	"github.com/imtemp-dev/claude-bts/internal/feature"
	"github.com/imtemp-dev/claude-bts/internal/state"
)

// copySetup is the command that makes a fresh copy of the project runnable:
// the --setup flag when one is given, else implement.setup_cmd, else the
// setup of the feature the recipe is bound to.  A copy holds what git
// lists, so an ignored virtualenv or node_modules is not in it (trial
// problems #141, #156).
func copySetup(root, recipeID, flag string) string {
	if s := strings.TrimSpace(flag); s != "" {
		return s
	}
	if cfg, err := engine.LoadSettings(root); err == nil {
		if s := strings.TrimSpace(cfg.Implement.SetupCmd); s != "" {
			return s
		}
	}
	if r, err := state.LoadRecipeState(root, recipeID); err == nil {
		return feature.SetupFor(r)
	}
	return ""
}

// runCopySetup runs setup in a copy.  An error means nothing run in the
// copy would describe the code, so the caller runs nothing there.
func runCopySetup(dir, setup string, timeoutSec int, sink io.Writer) error {
	if setup == "" {
		return nil
	}
	s, err := runShellCommand(dir, setup, timeoutSec, sink)
	if err != nil {
		return fmt.Errorf("setup %q: %w", setup, err)
	}
	if s.ExitCode != 0 || s.TimedOut {
		return fmt.Errorf("setup %q failed in the copy (exit=%d): %s", setup, s.ExitCode, lastLines(s.Output, 3))
	}
	return nil
}

// lastLines is the end of a run's output, on one line.
func lastLines(out []byte, n int) string {
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, " | ")
}

// suspectRun is a finding whose rerun stopped on a missing dependency.
type suspectRun struct {
	id, why string
}

// controlCopyRun runs the recipe's own tests, as test-results.json recorded
// them, in a fresh copy with the same setup.  It tells a copy that lacks
// the project's environment (the tests fail there too) from a finding whose
// own scenario needs what is missing (the tests pass).  ok is false when
// there is no recorded test command to run.
func controlCopyRun(root, recipeID, setup string, timeoutSec int) (passed bool, detail string, ok bool) {
	tr, err := state.LoadTestResults(root, recipeID)
	if err != nil || tr == nil || strings.TrimSpace(tr.Command) == "" {
		return false, "", false
	}
	dir, err := os.MkdirTemp("", "bts-control-*")
	if err != nil {
		return false, err.Error(), true
	}
	defer os.RemoveAll(dir)
	if _, err := copyProjectTo(root, dir); err != nil {
		return false, "copy project: " + err.Error(), true
	}
	if err := runCopySetup(dir, setup, timeoutSec, io.Discard); err != nil {
		return false, err.Error(), true
	}
	run, err := runShellCommand(dir, tr.Command, timeoutSec, io.Discard)
	if err != nil {
		return false, err.Error(), true
	}
	if run.ExitCode == 0 && !run.TimedOut {
		return true, "", true
	}
	detail = fmt.Sprintf("`%s` exits %d there", tr.Command, run.ExitCode)
	if why := engine.MissingDependency(run.Output, run.ExitCode); why != "" {
		detail += " (" + why + ")"
	}
	return false, detail, true
}

// couldNotRunError names findings whose reruns could not run in a copy, and
// how to give the copies the project's environment.
func couldNotRunError(recipeID, setup, control string, suspects []suspectRun, recheck bool) error {
	var b strings.Builder
	fmt.Fprintf(&b, "%d finding(s) could not run in a fresh copy of the project:", len(suspects))
	for _, s := range suspects {
		fmt.Fprintf(&b, "\n  %s: %s", s.id, s.why)
	}
	if control != "" {
		fmt.Fprintf(&b, "\nThe recipe's own tests fail in such a copy too: %s.", control)
	}
	if setup == "" {
		b.WriteString("\nThe copies ran no setup, and a copy holds only what git lists (no ignored virtualenv or node_modules).  " +
			"Give them one: implement.setup_cmd in .bts/config/settings.yaml, the feature's (bts feature setup <feature> \"<command>\"), or --setup here")
	} else {
		fmt.Fprintf(&b, "\nThe copies ran setup %q, which did not provide it; fix the setup", setup)
	}
	if recheck {
		fmt.Fprintf(&b, ", then run `bts interrogate verify %s --recheck` again.  The other findings' reruns were recorded.", recipeID)
	} else {
		fmt.Fprintf(&b, ", then run `bts interrogate verify %s` again.  Nothing was recorded.", recipeID)
	}
	return fmt.Errorf("%s", b.String())
}
