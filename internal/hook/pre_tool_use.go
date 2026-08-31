package hook

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/imtemp-dev/claude-bts/internal/engine"

	"github.com/imtemp-dev/claude-bts/internal/state"
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

	// Fan-out budget: the only place a batch size can be observed.
	if input.ToolName == "Task" {
		if note := overBatchNotice(root, input); note != "" {
			return &HookOutput{
				HookSpecificOutput: &HookSpecificOutput{
					HookEventName:     "PreToolUse",
					AdditionalContext: note,
				},
			}, nil
		}
		return &HookOutput{}, nil
	}

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

	// Allow writes to .bts/ and .claude/ directories (recipe documents, configs)
	if strings.Contains(filePath, ".bts/") || strings.Contains(filePath, ".claude/") {
		return &HookOutput{}, nil
	}

	// Source code file during spec phase → warn (not block)
	return &HookOutput{
		HookSpecificOutput: &HookSpecificOutput{
			HookEventName: "PreToolUse",
			AdditionalContext: fmt.Sprintf(
				"[bts] Writing source code during spec phase (%s). "+
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

// Fan-out budget — the batch sizes are the one part of simulate's
// settings that no document can be checked against afterwards.
//
// simulate.scenario_batch and simulate.finding_batch say how much work
// one agent takes. Nothing they govern survives into an artefact: the
// simulation report records what was found, never how many agents split
// the finding. So `bts validate` cannot check them the way it checks
// the scenario budget, and for a while that was read as "unverifiable".
//
// It is unverifiable AFTER the fact. This hook sees the spawn itself,
// and the prompt names its own contents in bts's own ID vocabulary —
// [GAP-001], [ISS-002] for findings, S01 for scenarios. Counting those
// is counting the batch.
//
// It warns rather than blocks. An over-large batch produces a worse
// round, not a wrong one, and the measured failure was recoverable: 59
// findings split three ways ran three validators past the 64K
// output-token limit, they were abandoned, and the round re-ran them in
// smaller batches nineteen minutes later. Refusing the spawn would
// stop a round that can still finish; saying so lets the orchestrator
// split before it pays.
func overBatchNotice(root string, input *HookInput) string {
	agentType, _ := input.ToolInput["subagent_type"].(string)
	prompt, _ := input.ToolInput["prompt"].(string)
	if prompt == "" {
		return ""
	}

	cfg, err := engine.LoadSettings(root)
	if err != nil {
		return ""
	}

	var limit, count int
	var unit, setting string
	switch agentType {
	case "simulator":
		limit, unit, setting = cfg.Simulate.ScenarioBatch, "scenario", "simulate.scenario_batch"
		count = countUnique(scenarioIDRe, prompt)
	case "simulator-validator", "simulator-rebuttal":
		limit, unit, setting = cfg.Simulate.FindingBatch, "finding", "simulate.finding_batch"
		count = countUnique(findingIDRe, prompt)
	default:
		return ""
	}
	if limit <= 0 || count <= limit {
		return ""
	}

	return fmt.Sprintf(
		"[bts] This %s agent is being handed %d %ss; %s is %d. "+
			"Nothing downstream can see a batch size — the report records what was "+
			"found, not how it was split — so this is the only point at which it can "+
			"be said. Split into batches of %d and spawn them in one message. "+
			"Measured: 59 findings split three ways put all three validators past the "+
			"64K output-token limit, they were abandoned mid-reply, and the round lost "+
			"nineteen minutes re-running them in sixes.",
		agentType, count, unit, setting, limit, limit)
}

// countUnique counts distinct matches, because a prompt legitimately
// mentions the same id more than once — a findings list names it in the
// header and again in the body.
func countUnique(re *regexp.Regexp, s string) int {
	seen := map[string]bool{}
	for _, m := range re.FindAllString(s, -1) {
		seen[strings.ToUpper(m)] = true
	}
	return len(seen)
}

var (
	// Finding ids as bts-simulate assigns them in Step 5.
	findingIDRe = regexp.MustCompile(`\[(?:GAP|ISS|COV)-\d+\]`)
	// Scenario ids in either canonical form: S01 or sim-001.s1.
	scenarioIDRe = regexp.MustCompile(`\bS\d{2,}\b|\bsim-\d+\.s\d+\b`)
)
