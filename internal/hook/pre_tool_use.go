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
	if isAgentSpawnTool(input.ToolName) {
		if note := overBatchNotice(root, input); note != "" {
			return &HookOutput{
				HookSpecificOutput: &HookSpecificOutput{
					HookEventName:            "PreToolUse",
					PermissionDecision:       "deny",
					PermissionDecisionReason: note,
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
	// Agent delegation: capture subagent_type + short description so the
	// breadcrumb is actually useful for post-compact recovery. The spawn
	// tool's ToolInput has no file_path/command; without this the entry
	// would be an uninformative bare tool name.
	if isAgentSpawnTool(input.ToolName) {
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
	case "Read", "Edit", "Write", "Bash", "Grep", "Glob", "NotebookEdit":
		return true
	}
	return isAgentSpawnTool(name)
}

// isAgentSpawnTool reports whether a tool call spawns a subagent.
//
// The harness renamed the tool from "Task" to "Agent". Every check in
// this file matched the old name only, so for the whole life of the
// batch budget the notice below never fired once: a measured project's
// hook log holds 67 `Agent` spawns and zero `Task`, and validators were
// handed 10, 13, 15, 16, 17, 19, 23 and 28 findings against a batch of
// 6 without a word. The breadcrumb for post-compact recovery went
// silent the same way. Both names are accepted so the next rename does
// not repeat it unnoticed.
func isAgentSpawnTool(name string) bool {
	return name == "Task" || name == "Agent"
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
// It refuses the spawn rather than warning. The first form of this
// check only warned, on the reasoning that an over-large batch makes a
// worse round rather than a wrong one — and it never fired, because it
// matched a tool name the harness had stopped using (isAgentSpawnTool).
// While it was silent, every measured round chose its own batch size
// and none chose the configured one. A warning the model can read and
// ignore is the same mechanism as the prose in the skill, which was
// already being ignored; a refusal costs one retry with a split prompt
// and nothing else, because the split IS the fix.
//
// Scope: this guards the Agent-tool path only. /bts-defend runs as a
// Skill fork and draws its batch from `bts recipe findings defend-batch`,
// which caps it in Go (state.DefendBatch) — no prompt carries that batch
// for this hook to count. The two together mean a batch is bounded
// whichever way an agent is started.
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
	case "simulator-validator", "simulator-rebuttal", "defender":
		limit, unit, setting = cfg.Simulate.FindingBatch, "finding", "simulate.finding_batch"
		count = countUnique(findingIDRe, prompt)
	default:
		return ""
	}
	if limit <= 0 || count <= limit {
		return ""
	}

	return fmt.Sprintf(
		"[bts] Refused: this %s agent was being handed %d %ss; %s is %d. "+
			"Nothing downstream can see a batch size — the report records what was "+
			"found, not how it was split — so this is the only point at which it can "+
			"be enforced. Split into batches of at most %d and spawn every batch in ONE "+
			"message. Measured: 59 findings split three ways put all three validators "+
			"past the 64K output-token limit, they were abandoned mid-reply, and the "+
			"round lost nineteen minutes re-running them in sixes.",
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
	// Finding ids in either vocabulary: the walk's provisional
	// [GAP-001]/[ISS-002]/[COV-003], or the ledger's F-xxxxxxxx once a
	// round has been logged (bts-defend hands those to the defender).
	findingIDRe = regexp.MustCompile(`\[(?:GAP|ISS|COV)-\d+\]|\bF-[0-9a-f]{8}\b`)
	// Scenario ids in the canonical forms (S01, sim-001.s1) and in the
	// loose forms measured walker prompts actually used — "scenario 4",
	// "scenarios 1,2,3,7", "시나리오 5" — which the canonical-only
	// pattern counted as zero.
	scenarioIDRe = regexp.MustCompile(`(?i)\bS\d{2,}\b|\bsim-\d+\.s\d+\b|\bscenarios?\s*#?\s*\d+|시나리오\s*\d+`)
)
