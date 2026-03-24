package hook

import (
	"fmt"
	"strings"

	"github.com/jlim/claude-forge/internal/state"
)

type preToolUseHandler struct{}

func NewPreToolUseHandler() Handler {
	return &preToolUseHandler{}
}

func (h *preToolUseHandler) EventType() EventType {
	return EventPreToolUse
}

func (h *preToolUseHandler) Handle(input *HookInput) (*HookOutput, error) {
	// Only care about Write and Edit tools
	if input.ToolName != "Write" && input.ToolName != "Edit" {
		return &HookOutput{}, nil
	}

	root, err := state.FindRoot(input.CWD)
	if err != nil {
		return &HookOutput{}, nil
	}

	recipe, err := state.GetActiveRecipe(root)
	if err != nil || recipe == nil {
		return &HookOutput{}, nil
	}

	// Only protect during spec phases (not implement/finalize/complete)
	if state.IsImplementPhase(recipe.Phase) || recipe.Phase == "finalize" || recipe.Phase == "complete" || recipe.Phase == "cancelled" {
		return &HookOutput{}, nil
	}

	// Extract file path from tool input
	filePath, _ := input.ToolInput["file_path"].(string)
	if filePath == "" {
		return &HookOutput{}, nil
	}

	// Allow writes to .forge/ and .claude/ directories (recipe documents, configs)
	if strings.Contains(filePath, ".forge/") || strings.Contains(filePath, ".claude/") {
		return &HookOutput{}, nil
	}

	// Source code file during spec phase → warn (not block)
	return &HookOutput{
		HookSpecificOutput: &HookSpecificOutput{
			AdditionalContext: fmt.Sprintf(
				"[forge] Writing source code during spec phase (%s). "+
					"Blueprint creates specs, not code. "+
					"Save code snippets in the spec document instead.",
				recipe.Phase,
			),
		},
	}, nil
}
