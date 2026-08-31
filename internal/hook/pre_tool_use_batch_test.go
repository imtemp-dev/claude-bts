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

func taskInput(root, agentType, prompt string) *HookInput {
	return &HookInput{
		CWD:      root,
		ToolName: "Task",
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

// The measured failure: 59 findings split three ways ran three
// validators past the 64K output-token limit. Nothing downstream can
// see a batch size, so the spawn is the only place it can be said.
func TestPreToolUse_WarnsOnOversizedFindingBatch(t *testing.T) {
	root := batchRoot(t, "simulate:\n  finding_batch: 6\n")
	h := NewPreToolUseHandler()

	out, err := h.Handle(taskInput(root, "simulator-validator", findingsPrompt(20)))
	if err != nil {
		t.Fatal(err)
	}
	if out.HookSpecificOutput == nil {
		t.Fatal("20 findings against a batch of 6 must produce a notice")
	}
	got := out.HookSpecificOutput.AdditionalContext
	for _, want := range []string{"20 finding", "simulate.finding_batch", "6"} {
		if !strings.Contains(got, want) {
			t.Errorf("notice missing %q:\n%s", want, got)
		}
	}

	// At the limit, silence.
	out, err = h.Handle(taskInput(root, "simulator-validator", findingsPrompt(6)))
	if err != nil {
		t.Fatal(err)
	}
	if out.HookSpecificOutput != nil {
		t.Errorf("a batch at the limit must be silent, got: %s",
			out.HookSpecificOutput.AdditionalContext)
	}
}

func TestPreToolUse_WarnsOnOversizedScenarioBatch(t *testing.T) {
	root := batchRoot(t, "simulate:\n  scenario_batch: 3\n")
	h := NewPreToolUseHandler()

	out, err := h.Handle(taskInput(root, "simulator",
		"Walk THESE scenarios: S01, S02, S03, S04, S05, S06, S07"))
	if err != nil {
		t.Fatal(err)
	}
	if out.HookSpecificOutput == nil {
		t.Fatal("7 scenarios against a batch of 3 must produce a notice")
	}
	if !strings.Contains(out.HookSpecificOutput.AdditionalContext, "7 scenario") {
		t.Errorf("notice must name the count: %s", out.HookSpecificOutput.AdditionalContext)
	}
}

// 0 means "one agent for everything" and must not be read as a limit of
// zero, which would make every spawn a violation.
func TestPreToolUse_ZeroBatchDisablesTheCheck(t *testing.T) {
	root := batchRoot(t, "simulate:\n  finding_batch: 0\n")
	h := NewPreToolUseHandler()
	out, err := h.Handle(taskInput(root, "simulator-validator", findingsPrompt(50)))
	if err != nil {
		t.Fatal(err)
	}
	if out.HookSpecificOutput != nil {
		t.Errorf("0 disables the batch, got: %s", out.HookSpecificOutput.AdditionalContext)
	}
}

// Other agents are none of this check's business.
func TestPreToolUse_IgnoresUnrelatedAgents(t *testing.T) {
	root := batchRoot(t, "simulate:\n  finding_batch: 6\n")
	h := NewPreToolUseHandler()
	out, err := h.Handle(taskInput(root, "verifier", findingsPrompt(30)))
	if err != nil {
		t.Fatal(err)
	}
	if out.HookSpecificOutput != nil {
		t.Errorf("only the simulate agents are budgeted, got: %s",
			out.HookSpecificOutput.AdditionalContext)
	}
}
