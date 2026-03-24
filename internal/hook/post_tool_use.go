package hook

import (
	"github.com/jlim/claude-forge/internal/metrics"
	"github.com/jlim/claude-forge/internal/state"
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
		if len(cmd) > 100 {
			cmd = cmd[:100]
		}
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
	return &HookOutput{}, nil
}
