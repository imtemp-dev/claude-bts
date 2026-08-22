package hook

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

func setupToolUseRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".bts", "local"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".bts", "specs", "recipes"), 0755); err != nil {
		t.Fatalf("mkdir specs: %v", err)
	}
	return root
}

func TestPreToolUse_WritesBreadcrumb(t *testing.T) {
	root := setupToolUseRoot(t)

	h := NewPreToolUseHandler()
	input := &HookInput{
		CWD:       root,
		ToolName:  "Read",
		ToolInput: map[string]interface{}{"file_path": "/tmp/foo.go"},
	}
	if _, err := h.Handle(input); err != nil {
		t.Fatalf("handle: %v", err)
	}
	tail, err := state.TailToolTrace(root, 5)
	if err != nil {
		t.Fatalf("tail: %v", err)
	}
	if len(tail) != 1 || tail[0].ToolName != "Read" || tail[0].File != "/tmp/foo.go" || tail[0].Phase != "pre" {
		t.Errorf("unexpected trace: %+v", tail)
	}
}

func TestPreToolUse_SkipsUntrackedTool(t *testing.T) {
	root := setupToolUseRoot(t)
	h := NewPreToolUseHandler()
	input := &HookInput{
		CWD:       root,
		ToolName:  "SomeRandomTool",
		ToolInput: map[string]interface{}{"file_path": "/x"},
	}
	if _, err := h.Handle(input); err != nil {
		t.Fatalf("handle: %v", err)
	}
	tail, _ := state.TailToolTrace(root, 5)
	if len(tail) != 0 {
		t.Errorf("expected no breadcrumb for untracked tool, got %+v", tail)
	}
}

func TestPostToolUse_WritesBreadcrumbWithExitCode(t *testing.T) {
	root := setupToolUseRoot(t)

	h := NewPostToolUseHandler()
	input := &HookInput{
		CWD:        root,
		ToolName:   "Bash",
		ToolInput:  map[string]interface{}{"command": "ls -la /tmp"},
		ToolResult: map[string]interface{}{"exit_code": float64(0)},
	}
	if _, err := h.Handle(input); err != nil {
		t.Fatalf("handle: %v", err)
	}
	tail, _ := state.TailToolTrace(root, 5)
	if len(tail) != 1 {
		t.Fatalf("expected 1 breadcrumb, got %d", len(tail))
	}
	if tail[0].ToolName != "Bash" || tail[0].Phase != "post" {
		t.Errorf("unexpected: %+v", tail[0])
	}
	if tail[0].ExitCode == nil || *tail[0].ExitCode != 0 {
		t.Errorf("ExitCode should be 0, got %v", tail[0].ExitCode)
	}
}

func TestPreToolUse_TaskCapturesSubagentType(t *testing.T) {
	root := setupToolUseRoot(t)
	h := NewPreToolUseHandler()
	input := &HookInput{
		CWD:      root,
		ToolName: "Task",
		ToolInput: map[string]interface{}{
			"subagent_type": "Explore",
			"description":   "Find API endpoints",
			"prompt":        "long prompt content here that should not bleed",
		},
	}
	if _, err := h.Handle(input); err != nil {
		t.Fatalf("handle: %v", err)
	}
	tail, _ := state.TailToolTrace(root, 1)
	if len(tail) != 1 {
		t.Fatalf("want 1 entry")
	}
	if tail[0].ToolName != "Task" {
		t.Errorf("ToolName: %q", tail[0].ToolName)
	}
	if tail[0].File != "Explore" {
		t.Errorf("subagent_type should land in File field, got %q", tail[0].File)
	}
	if tail[0].Summary != "Find API endpoints" {
		t.Errorf("Summary: %q", tail[0].Summary)
	}
}

func TestPreToolUse_BashCommandTruncation(t *testing.T) {
	root := setupToolUseRoot(t)

	// A realistic long command rather than 200 x's: the point of the cap
	// is that a partial entry must not read as a whole one, and a run of
	// identical bytes cannot show that.
	longCmd := "cd /Users/someone/Workspace/a-project/.bts/specs/recipes/r-001-support-gpt-solterra && " +
		"bts recipe log r-001-support-gpt-solterra --from-verification verification.md --doc draft.md --scope full --dimension verify --dimension audit"
	h := NewPreToolUseHandler()
	input := &HookInput{
		CWD:       root,
		ToolName:  "Bash",
		ToolInput: map[string]interface{}{"command": longCmd},
	}
	_, _ = h.Handle(input)

	tail, _ := state.TailToolTrace(root, 1)
	if len(tail) != 1 {
		t.Fatalf("expected 1 entry")
	}
	got := tail[0].Command
	if len(got) >= len(longCmd) {
		t.Errorf("Command should be bounded, got %d chars", len(got))
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("a clipped command must be marked as partial, got %q", got)
	}
	if strings.HasSuffix(strings.TrimSpace(strings.TrimSuffix(got, "…")), "&&") {
		t.Errorf("clipped command left a dangling shell separator: %q", got)
	}
}
