package hook

// EventType represents a Claude Code lifecycle event.
type EventType string

const (
	EventSessionStart  EventType = "session-start"
	EventPreCompact    EventType = "pre-compact"
	EventStop          EventType = "stop"
	EventSessionEnd    EventType = "session-end"
	EventPreToolUse    EventType = "pre-tool-use"
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

	// Subagent fields
	AgentID string `json:"agent_id,omitempty"`

	// Tool fields (for future use)
	ToolName  string                 `json:"tool_name,omitempty"`
	ToolInput map[string]interface{} `json:"tool_input,omitempty"`
}

// HookOutput is the JSON written to stdout for Claude Code.
type HookOutput struct {
	// For SessionStart: inject context into Claude
	HookSpecificOutput *HookSpecificOutput `json:"hookSpecificOutput,omitempty"`

	// For Stop: block or allow
	Decision string `json:"decision,omitempty"` // "block" or empty
	Reason   string `json:"reason,omitempty"`
}

// HookSpecificOutput contains data to inject into Claude's context.
type HookSpecificOutput struct {
	AdditionalContext string `json:"additionalContext,omitempty"`
}

// Handler processes a hook event and returns output.
type Handler interface {
	EventType() EventType
	Handle(input *HookInput) (*HookOutput, error)
}
