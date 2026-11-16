package cli

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// Plan N11 (AGEN-78): a hook payload that does not parse made runHook exit
// 0 with no trace, which would switch every hook off unseen if Claude Code
// changed the payload.  It now says so on stderr and exits 1, a
// non-blocking hook error.
func TestRunHook_UnparseablePayloadIsReported(t *testing.T) {
	if os.Getenv("BTS_TEST_HOOK_CHILD") == "1" {
		_ = runHook(hookCmd, []string{"Stop"})
		os.Exit(0) // not reached when runHook exits
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestRunHook_UnparseablePayloadIsReported$")
	cmd.Env = append(os.Environ(), "BTS_TEST_HOOK_CHILD=1")
	cmd.Stdin = strings.NewReader(`{"session_id": "s-1", "hook_event_name": "Stop",`)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 {
		t.Fatalf("want exit 1 for an unparseable payload, got %v (stderr: %s)", err, stderr.String())
	}
	if !strings.Contains(stderr.String(), "the Stop hook payload could not be read") {
		t.Errorf("stderr must say what failed, got: %s", stderr.String())
	}
}
