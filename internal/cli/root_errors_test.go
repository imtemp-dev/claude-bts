package cli

import (
	"bytes"
	"strings"
	"testing"
)

// Trial problem #120: every failing bts command printed its error twice,
// once from cobra and once from ExitOnError, so a long error (the
// four-line --from-verification guidance on cool-crow, 11-07 21:20Z)
// reached the agent's context twice.  Cobra prints none now; ExitOnError
// prints it once.
func TestRootCommand_LeavesTheErrorToExitOnError(t *testing.T) {
	var out, errOut bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&errOut)
	rootCmd.SetArgs([]string{"graph", "paths", "--no-such-flag"})
	t.Cleanup(func() {
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
		rootCmd.SetArgs(nil)
	})
	err := rootCmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "unknown flag") {
		t.Fatalf("an unknown flag must fail, got %v", err)
	}
	if n := strings.Count(out.String()+errOut.String(), "Error:"); n != 0 {
		t.Fatalf("cobra printed the error itself (%d times); ExitOnError prints it too, so it shows twice:\n%s%s", n, out.String(), errOut.String())
	}
}
