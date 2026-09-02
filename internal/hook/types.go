package hook

// EventType represents a Claude Code lifecycle event.
type EventType string

const (
	EventSessionStart  EventType = "session-start"
	EventPreCompact    EventType = "pre-compact"
	EventStop          EventType = "stop"
	EventSessionEnd    EventType = "session-end"
	EventPreToolUse    EventType = "pre-tool-use"
	EventPostToolUse   EventType = "post-tool-use"
	EventSubagentStart EventType = "subagent-start"
	EventSubagentStop  EventType = "subagent-stop"
)

// HookInput is the JSON received via stdin from Claude Code.
type HookInput struct {
	SessionID     string `json:"session_id"`
	CWD           string `json:"cwd"`
	HookEventName string `json:"hook_event_name"`

	// Session source (if Claude Code sends it)
	Source string `json:"source,omitempty"` // "startup", "resume", "clear", "compact"

	// Stop hook fields
	StopHookContent string `json:"content,omitempty"`
	// StopHookActive is set by Claude Code on every stop that follows a
	// stop-hook continuation. It cannot distinguish the second block from
	// the tenth, so it is recorded for diagnostics but the actual loop
	// bound is state.ChargeStopBlock's budget.
	StopHookActive bool `json:"stop_hook_active,omitempty"`

	// Subagent fields
	AgentID string `json:"agent_id,omitempty"`

	// Tool fields
	ToolName   string                 `json:"tool_name,omitempty"`
	ToolInput  map[string]interface{} `json:"tool_input,omitempty"`
	ToolResult map[string]interface{} `json:"tool_result,omitempty"`

	// Extended fields (sent by Claude Code, previously ignored)
	Model          string `json:"model,omitempty"`
	TranscriptPath string `json:"transcript_path,omitempty"`
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
