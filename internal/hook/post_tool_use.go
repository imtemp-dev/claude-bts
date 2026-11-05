package hook

import (
	"regexp"
	"strconv"

	"github.com/imtemp-dev/claude-bts/internal/metrics"
	"github.com/imtemp-dev/claude-bts/internal/state"
)

// Claude Code reports a tool call's outcome by which event it sends:
// PostToolUse after a success (its tool_response carries no exit code), and
// PostToolUseFailure after a failure, whose error text starts with
// "Exit code N" for Bash. A permission denial sends neither.

type postToolUseHandler struct{}

func NewPostToolUseHandler() Handler {
	return &postToolUseHandler{}
}

func (h *postToolUseHandler) EventType() EventType {
	return EventPostToolUse
}

func (h *postToolUseHandler) Handle(input *HookInput) (*HookOutput, error) {
	if root, err := input.ProjectRoot(); err == nil {
		bindOwnership(root, input)
	}
	recordToolUse(input, true, nil)
	return &HookOutput{}, nil
}

type postToolUseFailureHandler struct{}

// NewPostToolUseFailureHandler records a failed tool call.
func NewPostToolUseFailureHandler() Handler {
	return &postToolUseFailureHandler{}
}

func (h *postToolUseFailureHandler) EventType() EventType {
	return EventPostToolUseFailure
}

var exitCodeRe = regexp.MustCompile(`^Exit code (\d+)`)

func (h *postToolUseFailureHandler) Handle(input *HookInput) (*HookOutput, error) {
	var code *int
	if m := exitCodeRe.FindStringSubmatch(input.Error); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil {
			code = &n
		}
	}
	recordToolUse(input, false, code)
	return &HookOutput{}, nil
}

// recordToolUse appends a tool_use metrics event and, for tracked tools, a
// tool-trace breadcrumb for post-compact recovery.
func recordToolUse(input *HookInput, success bool, exitCode *int) {
	root, err := input.ProjectRoot()
	if err != nil {
		return
	}

	event := &metrics.MetricsEvent{
		Kind:      metrics.KindToolUse,
		SessionID: input.SessionID,
		ToolName:  input.ToolName,
		Success:   &success,
		ExitCode:  exitCode,
	}

	// Attach recipe context if available; another session's events stay
	// out of the recipe's metrics.
	recipe, _ := state.GetActiveRecipe(root)
	observer := isObserver(recipe, input.SessionID)
	switch {
	case observer:
		event.Observer = true
	case recipe != nil:
		event.RecipeID = recipe.ID
		event.Phase = recipe.Phase
	}

	// Extract file path from tool input
	if fp, ok := input.ToolInput["file_path"].(string); ok {
		event.ToolFile = fp
	} else if cmd, ok := input.ToolInput["command"].(string); ok {
		// Trim back to a separator rather than cutting mid-token: a
		// 100-byte slice of a shell line reliably produces entries like
		// `cd /Users/…/recipes/r-001 && ` — a command whose visible form
		// is a prefix with its verb removed. state.ClipCommand marks the
		// cut so a partial entry cannot be read as a whole one.
		event.ToolFile = state.ClipCommand(cmd)
	}

	_ = metrics.Append(root, event)

	if isTrackedTool(input.ToolName) && !observer {
		_ = state.AppendToolTrace(root, &state.ToolTraceEntry{
			Phase:    "post",
			ToolName: input.ToolName,
			File:     event.ToolFile,
			ExitCode: event.ExitCode,
		})
	}
}
