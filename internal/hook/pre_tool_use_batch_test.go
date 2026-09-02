package hook

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func batchRoot(t *testing.T, settings string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".bts", "config"), 0o755); err != nil {
		t.Fatal(err)
	}
	if settings != "" {
		if err := os.WriteFile(filepath.Join(root, ".bts", "config", "settings.yaml"),
			[]byte(settings), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// spawnInput builds the hook input for a subagent spawn under the tool
// name the harness uses today ("Agent"); spawnInputNamed lets a test
// cover the legacy "Task" name too.
func spawnInput(root, agentType, prompt string) *HookInput {
	return spawnInputNamed(root, "Agent", agentType, prompt)
}

func spawnInputNamed(root, tool, agentType, prompt string) *HookInput {
	return &HookInput{
		CWD:      root,
		ToolName: tool,
		ToolInput: map[string]interface{}{
			"subagent_type": agentType,
			"prompt":        prompt,
		},
	}
}

func findingsPrompt(n int) string {
	var b strings.Builder
	b.WriteString("Review the following simulation findings.\n\n## Findings\n\n")
	for i := 1; i <= n; i++ {
		// Each id appears twice, as a real prompt does: header and body.
		b.WriteString("[GAP-")
		b.WriteString(strings.Repeat("0", 3-len(itoa(i))))
		b.WriteString(itoa(i))
		b.WriteString("] title\n   see [GAP-")
		b.WriteString(strings.Repeat("0", 3-len(itoa(i))))
		b.WriteString(itoa(i))
		b.WriteString("] above\n")
	}
	return b.String()
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var d []byte
	for i > 0 {
		d = append([]byte{byte('0' + i%10)}, d...)
		i /= 10
	}
	return string(d)
}

// denied returns the refusal reason, or "" when the spawn went through.
func denied(out *HookOutput) string {
	if out == nil || out.HookSpecificOutput == nil {
		return ""
	}
	if out.HookSpecificOutput.PermissionDecision != "deny" {
		return ""
	}
	return out.HookSpecificOutput.PermissionDecisionReason
}

// The measured failure: 59 findings split three ways ran three
// validators past the 64K output-token limit. Nothing downstream can
// see a batch size, so the spawn is the only place it can be enforced.
func TestPreToolUse_RefusesOversizedFindingBatch(t *testing.T) {
	root := batchRoot(t, "simulate:\n  finding_batch: 6\n")
	h := NewPreToolUseHandler()

	out, err := h.Handle(spawnInput(root, "simulator-validator", findingsPrompt(20)))
	if err != nil {
		t.Fatal(err)
	}
	got := denied(out)
	if got == "" {
		t.Fatalf("20 findings against a batch of 6 must be refused, got %+v", out)
	}
	for _, want := range []string{"20 finding", "simulate.finding_batch", "6"} {
		if !strings.Contains(got, want) {
			t.Errorf("reason missing %q:\n%s", want, got)
		}
	}
	if out.HookSpecificOutput.AdditionalContext != "" {
		t.Errorf("a refusal carries its reason in permissionDecisionReason, not additionalContext")
	}

	// At the limit, silence.
	out, err = h.Handle(spawnInput(root, "simulator-validator", findingsPrompt(6)))
	if err != nil {
		t.Fatal(err)
	}
	if out.HookSpecificOutput != nil {
		t.Errorf("a batch at the limit must pass silently, got: %+v", out.HookSpecificOutput)
	}
}

// The tool was renamed from Task to Agent and the check matched only the
// old name, so it never fired on a real spawn. Both names must count.
func TestPreToolUse_BatchCheckMatchesBothSpawnToolNames(t *testing.T) {
	root := batchRoot(t, "simulate:\n  finding_batch: 6\n")
	h := NewPreToolUseHandler()

	for _, tool := range []string{"Agent", "Task"} {
		out, err := h.Handle(spawnInputNamed(root, tool, "defender", findingsPrompt(9)))
		if err != nil {
			t.Fatal(err)
		}
		if denied(out) == "" {
			t.Errorf("tool %q: 9 findings against a batch of 6 must be refused", tool)
		}
	}
}

// bts-defend hands the defender ledger ids (F-xxxxxxxx), not the walk's
// provisional [GAP-nnn] ids. Both vocabularies are one batch.
func TestPreToolUse_CountsLedgerFindingIDs(t *testing.T) {
	root := batchRoot(t, "simulate:\n  finding_batch: 6\n")
	h := NewPreToolUseHandler()

	var b strings.Builder
	b.WriteString("Defend these findings:\n")
	for _, id := range []string{"F-8e79a246", "F-140cd8f7", "F-a1b2c3d4", "F-00ff00ff",
		"F-deadbeef", "F-cafebabe", "F-12345678"} {
		b.WriteString("- " + id + " some title (" + id + ")\n")
	}
	out, err := h.Handle(spawnInput(root, "defender", b.String()))
	if err != nil {
		t.Fatal(err)
	}
	if got := denied(out); !strings.Contains(got, "7 finding") {
		t.Errorf("7 ledger ids must count as 7 findings, got: %q", got)
	}
}

func TestPreToolUse_RefusesOversizedScenarioBatch(t *testing.T) {
	root := batchRoot(t, "simulate:\n  scenario_batch: 3\n")
	h := NewPreToolUseHandler()

	out, err := h.Handle(spawnInput(root, "simulator",
		"Walk THESE scenarios: S01, S02, S03, S04, S05, S06, S07"))
	if err != nil {
		t.Fatal(err)
	}
	if got := denied(out); !strings.Contains(got, "7 scenario") {
		t.Errorf("7 scenarios against a batch of 3 must be refused and name the count: %q", got)
	}

	// Measured walker prompts named scenarios in prose, not by S-id, and
	// the canonical-only pattern counted them as zero.
	out, err = h.Handle(spawnInput(root, "simulator",
		"Walk scenario 1, scenario 2, scenario 3 and scenario 4, then 시나리오 5."))
	if err != nil {
		t.Fatal(err)
	}
	if got := denied(out); !strings.Contains(got, "5 scenario") {
		t.Errorf("prose scenario references must count, got: %q", got)
	}
}

// 0 means "one agent for everything" and must not be read as a limit of
// zero, which would make every spawn a violation.
func TestPreToolUse_ZeroBatchDisablesTheCheck(t *testing.T) {
	root := batchRoot(t, "simulate:\n  finding_batch: 0\n")
	h := NewPreToolUseHandler()
	out, err := h.Handle(spawnInput(root, "simulator-validator", findingsPrompt(50)))
	if err != nil {
		t.Fatal(err)
	}
	if out.HookSpecificOutput != nil {
		t.Errorf("0 disables the batch, got: %+v", out.HookSpecificOutput)
	}
}

// Other agents are none of this check's business.
func TestPreToolUse_IgnoresUnrelatedAgents(t *testing.T) {
	root := batchRoot(t, "simulate:\n  finding_batch: 6\n")
	h := NewPreToolUseHandler()
	out, err := h.Handle(spawnInput(root, "verifier", findingsPrompt(30)))
	if err != nil {
		t.Fatal(err)
	}
	if out.HookSpecificOutput != nil {
		t.Errorf("only the simulate agents are budgeted, got: %+v", out.HookSpecificOutput)
	}
}

// A spawn under the old tool name still leaves a breadcrumb, and so
// does one under the new name — the trace is what post-compact recovery
// replays, and it went blank when the name changed.
func TestPreToolUse_SpawnBreadcrumbUnderBothNames(t *testing.T) {
	for _, tool := range []string{"Agent", "Task"} {
		if !isTrackedTool(tool) {
			t.Errorf("%s spawns must be tracked", tool)
		}
	}
}
