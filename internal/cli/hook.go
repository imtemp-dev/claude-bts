package cli

import (
	"fmt"
	"os"

	"github.com/jlim/claude-forge/internal/hook"
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
	input, err := hook.ReadInput()
	if err != nil {
		// Silent exit on parse failure (don't break Claude Code)
		os.Exit(0)
		return nil
	}

	// Override event name from CLI arg (hook scripts pass it)
	input.HookEventName = args[0]

	registry := hook.NewRegistry(
		hook.NewSessionStartHandler(),
		hook.NewPreCompactHandler(),
		hook.NewPreToolUseHandler(),
		hook.NewPostToolUseHandler(),
		hook.NewStopHandler(),
		hook.NewSessionEndHandler(),
		hook.NewSubagentStartHandler(),
		hook.NewSubagentStopHandler(),
	)

	output, err := registry.Dispatch(input)
	if err != nil {
		fmt.Fprintf(os.Stderr, "forge hook error: %v\n", err)
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
