package hook

import "strings"

// EventType represents a Claude Code lifecycle event.
type EventType string

const (
	EventSessionStart EventType = "session-start"
	EventPreCompact   EventType = "pre-compact"
	EventStop         EventType = "stop"
	EventSessionEnd   EventType = "session-end"
	EventPreToolUse   EventType = "pre-tool-use"
	EventPostToolUse  EventType = "post-tool-use"
	// EventPostToolUseFailure fires instead of post-tool-use when a tool
	// fails (a Bash command exiting non-zero).
	EventPostToolUseFailure EventType = "post-tool-use-failure"
	EventSubagentStart      EventType = "subagent-start"
	EventSubagentStop       EventType = "subagent-stop"
)

// HookInput is the JSON received via stdin from Claude Code.
type HookInput struct {
	SessionID     string `json:"session_id"`
	CWD           string `json:"cwd"`
	HookEventName string `json:"hook_event_name"`

	// Session source (if Claude Code sends it)
	Source string `json:"source,omitempty"` // "startup", "resume", "clear", "compact"

	// Stop hook fields. Claude Code sends the turn's final assistant text as
	// last_assistant_message; "content" is kept only as a fallback for
	// callers that pipe a payload by hand. Read both through StopText.
	LastAssistantMessage string `json:"last_assistant_message,omitempty"`
	StopHookContent      string `json:"content,omitempty"`
	// StopHookActive is set by Claude Code on every stop that follows a
	// stop-hook continuation. It cannot distinguish the second block from
	// the tenth, so it is recorded for diagnostics but the actual loop
	// bound is state.ChargeStopBlock's budget.
	StopHookActive bool `json:"stop_hook_active,omitempty"`

	// Subagent fields
	AgentID string `json:"agent_id,omitempty"`

	// Tool fields
	ToolName     string                 `json:"tool_name,omitempty"`
	ToolInput    map[string]interface{} `json:"tool_input,omitempty"`
	ToolResponse map[string]interface{} `json:"tool_response,omitempty"`
	// PostToolUseFailure: the failure text ("Exit code 1\n..." for Bash).
	Error       string `json:"error,omitempty"`
	IsInterrupt bool   `json:"is_interrupt,omitempty"`

	// Extended fields (sent by Claude Code, previously ignored)
	Model          string `json:"model,omitempty"`
	TranscriptPath string `json:"transcript_path,omitempty"`
}

// StopText is the final assistant text of the turn that is stopping, where
// the completion markers (<bts>DONE</bts> and the others) are looked for.
func (in *HookInput) StopText() string {
	if in.LastAssistantMessage != "" {
		return in.LastAssistantMessage
	}
	return in.StopHookContent
}

// MarkerText is StopText with code removed: fenced blocks and inline code
// spans.  A completion marker counts only outside code, so a report that
// quotes `<bts>DONE</bts>` in backticks completes nothing, in the owning
// session or another one (trial problem #46).
func (in *HookInput) MarkerText() string {
	return stripCode(in.StopText())
}

// stripCode drops fenced code blocks (``` or ~~~) and inline code spans,
// keeping everything else.  An unmatched backtick run is kept as text.
func stripCode(s string) string {
	var out strings.Builder
	inFence := false
	for _, line := range strings.SplitAfter(s, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			inFence = !inFence
			continue
		}
		if !inFence {
			out.WriteString(stripInlineCode(line))
		}
	}
	return out.String()
}

func stripInlineCode(line string) string {
	var b strings.Builder
	for i := 0; i < len(line); {
		if line[i] != '`' {
			b.WriteByte(line[i])
			i++
			continue
		}
		j := i
		for j < len(line) && line[j] == '`' {
			j++
		}
		end := strings.Index(line[j:], line[i:j])
		if end < 0 {
			b.WriteString(line[i:])
			break
		}
		i = j + end + (j - i)
	}
	return b.String()
}

// HookOutput is the JSON written to stdout for Claude Code.
type HookOutput struct {
	// For Stop: block or allow
	Decision string `json:"decision,omitempty"` // "block" or empty
	Reason   string `json:"reason,omitempty"`

	// For hooks that support hookSpecificOutput (NOT Stop)
	HookSpecificOutput *HookSpecificOutput `json:"hookSpecificOutput,omitempty"`
}

// HookSpecificOutput contains data to inject into Claude's context.
// Requires hookEventName matching the event: SessionStart, PreToolUse, UserPromptSubmit, PostToolUse.
// PreCompact and SessionEnd do NOT use this — they save state via other means.
type HookSpecificOutput struct {
	HookEventName     string `json:"hookEventName,omitempty"`
	AdditionalContext string `json:"additionalContext,omitempty"`

	// PreToolUse only. "deny" refuses the call and hands the model
	// PermissionDecisionReason instead of the tool result; "allow" and
	// "ask" are accepted by the harness but unused here. Empty means the
	// hook expresses no opinion and the call proceeds.
	PermissionDecision       string `json:"permissionDecision,omitempty"`
	PermissionDecisionReason string `json:"permissionDecisionReason,omitempty"`
}

// Handler processes a hook event and returns output.
type Handler interface {
	EventType() EventType
	Handle(input *HookInput) (*HookOutput, error)
}
