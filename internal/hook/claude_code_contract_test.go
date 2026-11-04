package hook

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/metrics"
)

// testdata/claude-code holds hook payloads captured from Claude Code itself
// (BTS_HOOK_CAPTURE, 2026-11-05; paths and ids scrubbed). Two field-name
// bugs lived for months because every hook test built HookInput in Go:
// the stop marker was read from "content" (Claude Code sends
// last_assistant_message) and the tool result from "tool_result" (it sends
// tool_response). These tests read what Claude Code actually sends.

func loadFixture(t *testing.T, name string) (*HookInput, map[string]any) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "claude-code", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	in, err := parseInput(data)
	if err != nil {
		t.Fatal(err)
	}
	return in, raw
}

// Every field HookInput reads must arrive in at least one real payload.
// A name Claude Code never sends would otherwise stay silently empty.
func TestClaudeCodeContract_EveryFieldBtsReadsIsSent(t *testing.T) {
	// "content" is the fallback for a payload piped by hand; Claude Code
	// itself sends last_assistant_message.
	notFromClaudeCode := map[string]bool{"content": true}

	sent := map[string]bool{}
	files, _ := filepath.Glob(filepath.Join("testdata", "claude-code", "*.json"))
	if len(files) == 0 {
		t.Fatal("no captured payloads")
	}
	for _, f := range files {
		data, _ := os.ReadFile(f)
		var raw map[string]any
		if err := json.Unmarshal(data, &raw); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		for k := range raw {
			sent[k] = true
		}
	}
	typ := reflect.TypeOf(HookInput{})
	for i := 0; i < typ.NumField(); i++ {
		name := strings.Split(typ.Field(i).Tag.Get("json"), ",")[0]
		if name == "" || name == "-" || notFromClaudeCode[name] {
			continue
		}
		if !sent[name] {
			t.Errorf("HookInput.%s reads %q, which no captured Claude Code payload carries", typ.Field(i).Name, name)
		}
	}
}

func TestClaudeCodeContract_EachEventCarriesWhatItsHandlerUses(t *testing.T) {
	cases := map[string]func(*HookInput) string{
		"stop":                       func(in *HookInput) string { return in.StopText() },
		"session-start-startup":      func(in *HookInput) string { return in.Source },
		"session-start-compact":      func(in *HookInput) string { return in.Source },
		"pre-tool-use-bash":          func(in *HookInput) string { s, _ := in.ToolInput["command"].(string); return s },
		"post-tool-use-bash":         func(in *HookInput) string { s, _ := in.ToolResponse["stdout"].(string); return s },
		"post-tool-use-failure-bash": func(in *HookInput) string { return in.Error },
		"subagent-start":             func(in *HookInput) string { return in.AgentID },
		"subagent-stop":              func(in *HookInput) string { return in.AgentID },
	}
	for name, get := range cases {
		in, _ := loadFixture(t, name)
		if in.SessionID == "" {
			t.Errorf("%s: no session_id", name)
		}
		if get(in) == "" {
			t.Errorf("%s: the field its handler reads came through empty", name)
		}
	}
}

// A PostToolUse means the tool succeeded; a failing command arrives as
// PostToolUseFailure, whose error text carries the exit code.
func TestClaudeCodeContract_ToolOutcomeReachesMetrics(t *testing.T) {
	root, _ := setupStopRoot(t)

	ok, _ := loadFixture(t, "post-tool-use-bash")
	ok.CWD = root
	if _, err := NewPostToolUseHandler().Handle(ok); err != nil {
		t.Fatal(err)
	}
	failed, _ := loadFixture(t, "post-tool-use-failure-bash")
	failed.CWD = root
	if _, err := NewPostToolUseFailureHandler().Handle(failed); err != nil {
		t.Fatal(err)
	}

	events, err := metrics.ReadAllEvents(root)
	if err != nil {
		t.Fatal(err)
	}
	var got []metrics.MetricsEvent
	for _, e := range events {
		if e.Kind == metrics.KindToolUse {
			got = append(got, e)
		}
	}
	if len(got) != 2 {
		t.Fatalf("want two tool_use events, got %d", len(got))
	}
	if got[0].Success == nil || !*got[0].Success {
		t.Errorf("a PostToolUse must record success, got %+v", got[0])
	}
	if got[1].Success == nil || *got[1].Success || got[1].ExitCode == nil || *got[1].ExitCode != 1 {
		t.Errorf("a PostToolUseFailure must record failure with exit code 1, got %+v", got[1])
	}
}
