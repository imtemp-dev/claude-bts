package hook

import (
	"github.com/imtemp-dev/jig/internal/metrics"
	"github.com/imtemp-dev/jig/internal/state"
)

type postToolUseHandler struct{}

func NewPostToolUseHandler() Handler {
	return &postToolUseHandler{}
}

func (h *postToolUseHandler) EventType() EventType {
	return EventPostToolUse
}

func (h *postToolUseHandler) Handle(input *HookInput) (*HookOutput, error) {
	root, err := state.FindRoot(input.CWD)
	if err != nil {
		return &HookOutput{}, nil
	}

	event := &metrics.MetricsEvent{
		Kind:      metrics.KindToolUse,
		SessionID: input.SessionID,
		ToolName:  input.ToolName,
	}

	// Attach recipe context if available
	recipe, _ := state.GetActiveRecipe(root)
	if recipe != nil {
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
		cmd = state.ClipCommand(cmd)
		event.ToolFile = cmd
	}

	// Extract exit code from tool result
	if input.ToolResult != nil {
		if ec, ok := input.ToolResult["exit_code"]; ok {
			if ecf, ok := ec.(float64); ok {
				code := int(ecf)
				event.ExitCode = &code
				success := code == 0
				event.Success = &success
			}
		}
	}

	_ = metrics.Append(root, event)

	// Tool-trace breadcrumb for post-compact recovery
	if isTrackedTool(input.ToolName) {
		entry := &state.ToolTraceEntry{
			Phase:    "post",
			ToolName: input.ToolName,
			File:     event.ToolFile,
			ExitCode: event.ExitCode,
		}
		_ = state.AppendToolTrace(root, entry)
	}

	return &HookOutput{}, nil
}
