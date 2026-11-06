package hook

import (
	"strings"
	"testing"
)

// Trial problem #114: swift-eagle's fork "Create and prove the verify
// skill" compacted, and SessionStart gave it the recipe's resume context
// with the orchestrator's "NEXT: Run /bts-implement ... drive the app".
// The fork kept to its task that time; a subagent gets no next step now.
func TestSessionStart_SubagentCompactionGetsNoNextStep(t *testing.T) {
	root, _ := setupBuildRoot(t)
	for name, in := range map[string]*HookInput{
		"agent_id":            {CWD: root, HookEventName: "SessionStart", Source: "compact", AgentID: "afe804d1", AgentType: "fork"},
		"subagent transcript": {CWD: root, HookEventName: "SessionStart", Source: "compact", TranscriptPath: "/Users/x/.claude/projects/p/30a5a082/subagents/agent-afe804d1.jsonl"},
	} {
		out, err := NewSessionStartHandler().Handle(in)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		ctx := ""
		if out.HookSpecificOutput != nil {
			ctx = out.HookSpecificOutput.AdditionalContext
		}
		if strings.Contains(ctx, "NEXT") || strings.Contains(ctx, "/bts-") {
			t.Errorf("%s: a subagent gets no recipe next step, got %q", name, ctx)
		}
		if !strings.Contains(ctx, "subagent") {
			t.Errorf("%s: it is told the next step belongs to the main session, got %q", name, ctx)
		}
	}

	main, err := NewSessionStartHandler().Handle(&HookInput{CWD: root, HookEventName: "SessionStart", Source: "compact"})
	if err != nil {
		t.Fatal(err)
	}
	if main.HookSpecificOutput == nil || !strings.Contains(main.HookSpecificOutput.AdditionalContext, "NEXT") {
		t.Errorf("the main session still gets its resume context, got %+v", main.HookSpecificOutput)
	}
}
