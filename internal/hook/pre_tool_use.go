package hook

import (
	"fmt"
	"strings"

	"github.com/imtemp-dev/jig/internal/state"
)

type preToolUseHandler struct{}

func NewPreToolUseHandler() Handler {
	return &preToolUseHandler{}
}

func (h *preToolUseHandler) EventType() EventType {
	return EventPreToolUse
}

func (h *preToolUseHandler) Handle(input *HookInput) (*HookOutput, error) {
	root, err := state.FindRoot(input.CWD)
	if err != nil {
		return &HookOutput{}, nil
	}

	// Record a breadcrumb for any tracked tool — helps post-compact recovery.
	appendToolTraceBreadcrumb(root, "pre", input)

	// Spec-phase write protection: only care about Write and Edit tools
	if input.ToolName != "Write" && input.ToolName != "Edit" {
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

	// Allow writes to .jig/ and .claude/ directories (recipe documents, configs)
	if strings.Contains(filePath, ".jig/") || strings.Contains(filePath, ".claude/") {
		return &HookOutput{}, nil
	}

	// Source code file during spec phase → warn (not block)
	return &HookOutput{
		HookSpecificOutput: &HookSpecificOutput{
			HookEventName: "PreToolUse",
			AdditionalContext: fmt.Sprintf(
				"[jig] Writing source code during spec phase (%s). "+
					"Blueprint creates specs, not code. "+
					"Save code snippets in the spec document instead.",
				recipe.Phase,
			),
		},
	}, nil
}

// appendToolTraceBreadcrumb records a single tool-trace entry for a subset
// of tools that carry meaningful "what I was doing" context. Failures are
// silent — breadcrumbs are best-effort.
func appendToolTraceBreadcrumb(root, phase string, input *HookInput) {
	if !isTrackedTool(input.ToolName) {
		return
	}
	entry := &state.ToolTraceEntry{
		Phase:    phase,
		ToolName: input.ToolName,
	}
	if fp, ok := input.ToolInput["file_path"].(string); ok && fp != "" {
		entry.File = fp
	} else if pat, ok := input.ToolInput["pattern"].(string); ok && pat != "" {
		entry.File = pat
	}
	if cmd, ok := input.ToolInput["command"].(string); ok && cmd != "" {
		// Trim back to a separator rather than cutting mid-token: a
		// 100-byte slice of a shell line reliably produces entries like
		// `cd /Users/…/recipes/r-001 && ` — a command whose visible form
		// is a prefix with its verb removed. state.ClipCommand marks the
		// cut so a partial entry cannot be read as a whole one.
		cmd = state.ClipCommand(cmd)
		entry.Command = cmd
	}
	// Task delegation: capture subagent_type + short description so the
	// breadcrumb is actually useful for post-compact recovery. Task's
	// ToolInput has no file_path/command; without this the entry would
	// be an uninformative bare "Task".
	if input.ToolName == "Task" {
		if st, ok := input.ToolInput["subagent_type"].(string); ok && st != "" {
			entry.File = st
		}
		if desc, ok := input.ToolInput["description"].(string); ok && desc != "" {
			if len(desc) > 100 {
				desc = desc[:100]
			}
			entry.Summary = desc
		}
	}
	_ = state.AppendToolTrace(root, entry)
}

// isTrackedTool returns true for tools whose invocation reveals user intent
// worth replaying after compaction. Excludes noisy/trivial tools.
func isTrackedTool(name string) bool {
	switch name {
	case "Read", "Edit", "Write", "Bash", "Grep", "Glob", "Task", "NotebookEdit":
		return true
	}
	return false
}
