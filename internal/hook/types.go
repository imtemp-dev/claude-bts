package hook

import (
	"path/filepath"
	"strings"
)

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
	// EventUserPromptSubmit fires when the user sends a prompt.
	EventUserPromptSubmit EventType = "user-prompt-submit"
)

// IsSubagent reports whether the hook runs inside a subagent rather than
// the main session: Claude Code names the agent (agent_id), or the
// transcript is a subagent's (…/subagents/agent-….jsonl).  The captured
// main-session SessionStart payloads carry neither.
func (in *HookInput) IsSubagent() bool {
	return in.AgentID != "" || strings.Contains(filepath.ToSlash(in.TranscriptPath), "/subagents/")
}

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
	AgentID   string `json:"agent_id,omitempty"`
	AgentType string `json:"agent_type,omitempty"`

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
//
// Fences follow CommonMark: a block opened by a run of n backticks or
// tildes closes only on a line holding a run of the same character at
// least n long and nothing else, and a backtick run whose line carries
// another backtick opens no fence (it is inline code).  Toggling on any
// line that started with three of either let a ``` line inside a ````
// block close it, so a marker quoted after it counted as a completion
// and a real one after the block was dropped (plan N3, AGEN-77).  An
// unclosed fence runs to the end.  Indentation is not limited, so a
// fence inside a list item is still a fence.
func stripCode(s string) string {
	var out strings.Builder
	var fenceChar byte
	fenceLen := 0
	for _, line := range strings.SplitAfter(s, "\n") {
		t := strings.TrimSpace(line)
		if fenceLen > 0 {
			if c, n := fenceRun(t); c == fenceChar && n >= fenceLen && strings.TrimSpace(t[n:]) == "" {
				fenceLen = 0
			}
			continue
		}
		if c, n := fenceRun(t); n >= 3 && (c == '~' || !strings.Contains(t[n:], "`")) {
			fenceChar, fenceLen = c, n
			continue
		}
		out.WriteString(stripInlineCode(line))
	}
	return out.String()
}

// fenceRun is the run of backticks or tildes a line starts with: its
// character and length, or 0 and 0.
func fenceRun(t string) (byte, int) {
	if t == "" || (t[0] != '`' && t[0] != '~') {
		return 0, 0
	}
	n := 0
	for n < len(t) && t[n] == t[0] {
		n++
	}
	return t[0], n
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
