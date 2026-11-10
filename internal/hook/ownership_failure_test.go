package hook

import (
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

// failedBash is the PostToolUseFailure input for a Bash call that exited
// non-zero: Claude Code sends its output in error, with no tool_response.
func failedBash(root, sid, cmd, errText string) *HookInput {
	return &HookInput{
		CWD: root, SessionID: sid, HookEventName: "PostToolUseFailure", ToolName: "Bash",
		ToolInput: map[string]interface{}{"command": cmd},
		Error:     errText,
	}
}

// Trial problem #124: cool-crow created r-005 in one Bash call with a
// `sed` after it that failed (11-06 07:17Z).  The call exited 1, Claude
// Code sent PostToolUseFailure, which did not bind, and r-005 had no owner
// from then on, so every later session got its gates.
func TestOwnership_ACreateInACallThatFailsAfterwardsStillBinds(t *testing.T) {
	root, id := setupStopRoot(t)
	_ = state.WriteOwnerRequest(root, id, "create")
	cmd := "bts recipe create --type build --topic x 2>&1 | tail -3; sed -n '1,+20p' SKILL.md"
	if _, err := NewPostToolUseFailureHandler().Handle(failedBash(root, ownerSID, cmd,
		"Exit code 1\n"+id+"\nsed: 1: \",22p \": invalid command code ,")); err != nil {
		t.Fatal(err)
	}
	if got := owner(t, root, id); got != ownerSID {
		t.Fatalf("the create ran and left its request, so its session owns the recipe; owner=%q", got)
	}

	// A claim in a call that fails afterwards moves the recipe too.
	_ = state.WriteOwnerRequest(root, id, "claim")
	_, _ = NewPostToolUseFailureHandler().Handle(failedBash(root, observerSID, "bts recipe claim "+id+" && false", "Exit code 1\nclaimed"))
	if got := owner(t, root, id); got != observerSID {
		t.Fatalf("the claim ran and left its request, so it moves the recipe; owner=%q", got)
	}

	// A failed call that ran no bts left no request, and binds nothing.
	other, otherID := setupStopRoot(t)
	_, _ = NewPostToolUseFailureHandler().Handle(failedBash(other, ownerSID, "echo bts recipe create; false", "Exit code 1\n"+otherID))
	if got := owner(t, other, otherID); got != "" {
		t.Fatalf("no create ran, so nothing binds; owner=%q", got)
	}
}
