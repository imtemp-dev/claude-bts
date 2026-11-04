package hook

import (
	"os"
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

// IMPLEMENT DONE needs a verified interrogate run with nothing open, for
// every recipe type (prove stage, decision 5).
func TestImplementDone_NeedsACleanInterrogateRun(t *testing.T) {
	root, id := setupImplementRoot(t)
	removeInterrogate(t, root, id)

	out, err := NewStopHandler().Handle(&HookInput{CWD: root, LastAssistantMessage: "<bts>IMPLEMENT DONE</bts>"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Decision != "block" || !strings.Contains(out.Reason, "No interrogate run") {
		t.Fatalf("no interrogate run must block, got %q %q", out.Decision, out.Reason)
	}

	run := &state.InterrogateRun{PreparedAt: "t", VerifiedAt: "t", Findings: []*state.InterrogateFinding{
		{ID: "F-001", Status: state.InterrogateReproduced, Title: "x"},
	}}
	_ = state.SaveInterrogateRun(root, id, run)
	out, _ = NewStopHandler().Handle(&HookInput{CWD: root, LastAssistantMessage: "<bts>IMPLEMENT DONE</bts>"})
	if out.Decision != "block" || !strings.Contains(out.Reason, "F-001 reproduced and not fixed") {
		t.Fatalf("an open finding must block, got %q %q", out.Decision, out.Reason)
	}

	run.Findings[0].Status, run.Findings[0].WaiveReason = state.InterrogateWaived, "user accepted"
	_ = state.SaveInterrogateRun(root, id, run)
	out, _ = NewStopHandler().Handle(&HookInput{CWD: root, LastAssistantMessage: "<bts>IMPLEMENT DONE</bts>"})
	if out.Decision == "block" {
		t.Fatalf("a waived finding meets the gate: %s", out.Reason)
	}
}

func removeInterrogate(t *testing.T, root, id string) {
	t.Helper()
	_ = removeFile(state.InterrogatePath(root, id))
}

func removeFile(p string) error { return os.Remove(p) }
