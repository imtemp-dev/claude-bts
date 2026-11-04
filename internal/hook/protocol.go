package hook

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// ReadInput reads and parses JSON from stdin.
func ReadInput() (*HookInput, error) {
	return ReadInputFor("")
}

// CaptureEnv names a directory. When it is set, every hook writes the raw
// payload Claude Code sent it there before parsing, so the fields bts
// reads can be checked against what Claude Code actually sends
// (internal/hook/testdata/claude-code/ holds captured fixtures).
const CaptureEnv = "BTS_HOOK_CAPTURE"

// ReadInputFor reads and parses the stdin payload of the named hook event,
// capturing it first when CaptureEnv is set.
func ReadInputFor(event string) (*HookInput, error) {
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return nil, fmt.Errorf("read stdin: %w", err)
	}
	if dir := os.Getenv(CaptureEnv); dir != "" {
		if cerr := captureInput(dir, event, data); cerr != nil {
			fmt.Fprintf(os.Stderr, "[bts] warning: %s: %v\n", CaptureEnv, cerr)
		}
	}
	return parseInput(data)
}

// captureInput writes one raw payload as <dir>/<event>-<nanos>-<pid>.json.
func captureInput(dir, event string, data []byte) error {
	if event == "" {
		event = "unknown"
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	name := fmt.Sprintf("%s-%d-%d.json", event, time.Now().UnixNano(), os.Getpid())
	return os.WriteFile(filepath.Join(dir, name), data, 0644)
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
