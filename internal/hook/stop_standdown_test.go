package hook

import (
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

// stopAs runs the Stop hook with no completion marker for one session.
func stopAs(t *testing.T, root, session string) *HookOutput {
	t.Helper()
	out, err := NewStopHandler().Handle(&HookInput{CWD: root, SessionID: session})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	return out
}

// Trial problem #118: the stand-down cleared the budget, so every later
// turn of the session met the same two blocks and another stand-down.  On
// cool-crow (r-005, 11-07 19:46Z-20:29Z) four turns of a conversation about
// other things, with final.md left dirty by an earlier session and no way
// to run bts, gave 8 blocks and about 12 messages restating that final.md
// was unverified.  A session the gate stood down for is not blocked again
// on the same reason; another session, another reason, or an allowed stop
// arms it again.
func TestStopBlockBudget_AStoodDownReasonDoesNotBlockTheSessionAgain(t *testing.T) {
	root, _ := setupStopRoot(t)
	for i := 1; i < state.DefaultStopBlockBudget; i++ {
		if out := stopAs(t, root, "s-1"); out.Decision != "block" {
			t.Fatalf("block %d: expected block, got allow", i)
		}
	}
	if out := stopAs(t, root, "s-1"); out.Decision == "block" {
		t.Fatal("the budget ran out: the gate must stand down")
	}
	for turn := 1; turn <= 3; turn++ {
		if out := stopAs(t, root, "s-1"); out.Decision == "block" {
			t.Fatalf("turn %d after the stand-down: blocked again on the same reason: %s", turn, out.Reason)
		}
	}
	if out := stopAs(t, root, "s-2"); out.Decision != "block" {
		t.Fatal("a session that has not heard the reason must still be blocked")
	}
}
