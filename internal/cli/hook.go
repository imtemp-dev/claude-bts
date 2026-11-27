package cli

import (
	"fmt"
	"os"

	"github.com/imtemp-dev/claude-bts/internal/hook"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(hookCmd)
}

var hookCmd = &cobra.Command{
	Use:    "hook <event-name>",
	Short:  "Handle Claude Code lifecycle events (called by hooks)",
	Args:   cobra.ExactArgs(1),
	Hidden: true, // Not for direct user invocation
	RunE:   runHook,
}

func runHook(cmd *cobra.Command, args []string) error {
	input, err := hook.ReadInputFor(args[0])
	if err != nil {
		// Not silent: a payload Claude Code changed would otherwise switch
		// every hook off with no trace (plan N11, AGEN-78).  Exit 1 is a
		// non-blocking hook error, which Claude Code reports and goes on.
		fmt.Fprintln(os.Stderr, payloadErrorMessage(args[0], err))
		os.Exit(1)
		return nil
	}

	// Override event name from CLI arg (hook scripts pass it)
	input.HookEventName = args[0]

	registry := hook.NewRegistry(
		hook.NewSessionStartHandler(),
		hook.NewPreCompactHandler(),
		hook.NewPreToolUseHandler(),
		hook.NewPostToolUseHandler(),
		hook.NewPostToolUseFailureHandler(),
		hook.NewStopHandler(),
		hook.NewSessionEndHandler(),
		hook.NewSubagentStartHandler(),
		hook.NewSubagentStopHandler(),
		hook.NewUserPromptSubmitHandler(),
		// User-level (`bts pages setup`): runs in every session, bts
		// project or not.
		hook.NewArtifactGuardHandler(),
	)

	output, err := registry.Dispatch(input)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bts hook error: %v\n", err)
		os.Exit(0) // Always exit 0 to not break Claude Code
		return nil
	}

	if err := hook.WriteOutput(output); err != nil {
		os.Exit(0)
	}

	// Exit 2 if blocking
	if output.Decision == "block" {
		os.Exit(2)
	}

	return nil
}

// payloadErrorMessage is the stderr line for a hook payload bts could not
// read.
func payloadErrorMessage(event string, err error) string {
	return fmt.Sprintf("[bts] the %s hook payload could not be read (%v); bts did nothing for this event. "+
		"If Claude Code changed its hook payload, update bts.", event, err)
}
