package hook

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// ReadInput reads and parses JSON from stdin.
func ReadInput() (*HookInput, error) {
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return nil, fmt.Errorf("read stdin: %w", err)
	}

	return parseInput(data)
}

// parseInput decodes the stdin payload. Tests feed it Claude Code's own
// payload shapes, so a field Claude Code renames or never sends shows up as
// a failing test rather than as a hook that silently sees nothing.
func parseInput(data []byte) (*HookInput, error) {
	if len(data) == 0 {
		return &HookInput{}, nil
	}

	var input HookInput
	if err := json.Unmarshal(data, &input); err != nil {
		return nil, fmt.Errorf("parse input: %w", err)
	}

	return &input, nil
}

// WriteOutput writes JSON to stdout.
func WriteOutput(output *HookOutput) error {
	if output == nil {
		output = &HookOutput{}
	}

	data, err := json.Marshal(output)
	if err != nil {
		return fmt.Errorf("marshal output: %w", err)
	}

	_, err = fmt.Fprintln(os.Stdout, string(data))
	return err
}
