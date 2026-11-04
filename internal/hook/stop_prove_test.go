package hook

import (
	"os"
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

// IMPLEMENT DONE needs the planned user journeys driven through the real
// app on the current code, or deferred, or a recorded no-surface reason
// (prove stage P2, every recipe type).
func TestImplementDone_NeedsTheAppProven(t *testing.T) {
	root, id := setupImplementRoot(t)
	_ = os.Remove(state.ProvePath(root, id))
	done := func() *HookOutput {
		t.Helper()
		out, err := NewStopHandler().Handle(&HookInput{CWD: root, LastAssistantMessage: "<bts>IMPLEMENT DONE</bts>"})
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	if out := done(); out.Decision != "block" || !strings.Contains(out.Reason, "No drive record") {
		t.Fatalf("no drive record must block, got %q %q", out.Decision, out.Reason)
	}
	_ = state.SaveProveRecord(root, id, &state.ProveRecord{Journeys: map[string]*state.Journey{"login": {}}})
	if out := done(); out.Decision != "block" || !strings.Contains(out.Reason, "login not driven") {
		t.Fatalf("an undriven journey must block, got %q", out.Reason)
	}
	_ = state.SaveProveRecord(root, id, &state.ProveRecord{Journeys: map[string]*state.Journey{
		"login": {Deferred: &state.Deferral{Reason: "needs the real IdP", Owner: "live checks"}},
	}})
	if out := done(); out.Decision == "block" {
		t.Fatalf("a deferred journey meets the gate: %s", out.Reason)
	}
}
