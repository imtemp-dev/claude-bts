package hook

import (
	"encoding/json"
	"strings"
	"testing"
)

// The guard's decision table: only a publish that would make a new
// claude.ai page from a file is denied, with the bts command that does it.
func TestArtifactGuard_DecisionTable(t *testing.T) {
	cases := []struct {
		name string
		in   map[string]interface{}
		want string // "deny", "note" or ""
	}{
		{"a new page", map[string]interface{}{"file_path": "/tmp/s/tron-flow.html", "icon": "chart"}, "deny"},
		{"a new page, action publish", map[string]interface{}{"action": "publish", "file_path": "page.html"}, "deny"},
		{"a new page from files only", map[string]interface{}{"file_path": "/tmp/s/p.html", "files": map[string]interface{}{"img/a.png": "/tmp/s/a.png"}}, "deny"},
		{"an update to an existing artifact", map[string]interface{}{"file_path": "/tmp/s/p.html", "url": "https://claude.ai/artifact/abc"}, ""},
		{"an asset upload", map[string]interface{}{"file_path": "/tmp/a.png", "url": "https://claude.ai/artifact/abc", "asset": true}, ""},
		{"a typed artifact", map[string]interface{}{"type_url": "https://claude.ai/artifact/type/slides", "title": "Deck"}, "note"},
		{"a runtime page", map[string]interface{}{"file_path": "/tmp/s/poll.html", "capabilities": map[string]interface{}{"db": map[string]interface{}{}}}, "note"},
		{"empty capabilities", map[string]interface{}{"file_path": "/tmp/s/p.html", "capabilities": map[string]interface{}{}}, "deny"},
		{"a read", map[string]interface{}{"action": "read", "url": "https://claude.ai/artifact/abc"}, ""},
		{"a list", map[string]interface{}{"action": "list"}, ""},
		{"a quickstart", map[string]interface{}{"action": "quickstart", "intent": "other"}, ""},
		{"a delete", map[string]interface{}{"action": "delete", "url": "https://claude.ai/artifact/abc"}, ""},
		{"a review page", map[string]interface{}{"file_path": "/p/.bts/local/recipes/r-001-x/review/page.html"}, ""},
		{"a relative review page", map[string]interface{}{"file_path": ".bts/local/features/f-002-y/review/page.html"}, ""},
		{"a page with the review kit", map[string]interface{}{"file_path": "/tmp/p.html", "files": map[string]interface{}{"bts-review-kit.js": "/p/.claude/skills/bts-review-page/kit/bts-review-kit.js"}}, ""},
		{"nothing to publish", map[string]interface{}{"title": "x"}, ""},
	}
	for _, c := range cases {
		out, err := NewArtifactGuardHandler().Handle(&HookInput{CWD: "/p", ToolName: "Artifact", ToolInput: c.in})
		if err != nil {
			t.Fatal(err)
		}
		got := ""
		if h := out.HookSpecificOutput; h != nil {
			if h.HookEventName != "PreToolUse" {
				t.Errorf("%s: event name %q", c.name, h.HookEventName)
			}
			switch {
			case h.PermissionDecision == "deny":
				got = "deny"
			case h.AdditionalContext != "":
				got = "note"
			}
		}
		if got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
	// Other tools never reach a decision.
	if out, _ := NewArtifactGuardHandler().Handle(&HookInput{ToolName: "Write", ToolInput: map[string]interface{}{"file_path": "/tmp/p.html"}}); out.HookSpecificOutput != nil {
		t.Error("the guard acted on another tool")
	}
}

func TestArtifactGuard_ReasonNamesTheCommand(t *testing.T) {
	deny, _ := artifactGuard(&HookInput{ToolName: "Artifact", ToolInput: map[string]interface{}{
		"file_path":   "/tmp/scratch dir/tron flow.html",
		"description": "How a run moves",
		"files": map[string]interface{}{
			"img/b.png": "/tmp/b.png",
			"img/a.png": map[string]interface{}{"from": "/tmp/a.png"},
		},
	}})
	want := "bts page publish '/tmp/scratch dir/tron flow.html' --file img/a.png=/tmp/a.png --file img/b.png=/tmp/b.png --description 'How a run moves'"
	if !strings.Contains(deny, want) {
		t.Errorf("reason lacks %q:\n%s", want, deny)
	}
	for _, s := range []string{"--page <id>", "bts page export", "url"} {
		if !strings.Contains(deny, s) {
			t.Errorf("reason lacks %q", s)
		}
	}
	if got := shellQuote("it's"); got != `'it'\''s'` {
		t.Errorf("quote %s", got)
	}
	md, _ := artifactGuard(&HookInput{ToolName: "Artifact", ToolInput: map[string]interface{}{"file_path": "/tmp/notes.md"}})
	if !strings.HasPrefix(md, "bts serves HTML pages only") {
		t.Errorf("a Markdown page's reason: %s", md)
	}
}

// The payload Claude Code sends for an Artifact publish reaches the guard
// through the same parsing every hook uses.
func TestArtifactGuard_FromAPayload(t *testing.T) {
	payload := `{"session_id":"s","cwd":"/Users/x/Workspace/Carepatron-App","hook_event_name":"PreToolUse","tool_name":"Artifact",` +
		`"tool_input":{"file_path":"/private/tmp/claude-501/x/scratchpad/tron-run.html","icon":"chart","description":"Tron run lifecycle"}}`
	in, err := parseInput([]byte(payload))
	if err != nil {
		t.Fatal(err)
	}
	in.HookEventName = string(EventArtifactGuard)
	out, err := NewRegistry(NewArtifactGuardHandler()).Dispatch(in)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(out)
	if !strings.Contains(string(b), `"permissionDecision":"deny"`) || !strings.Contains(string(b), "bts page publish /private/tmp/claude-501/x/scratchpad/tron-run.html") {
		t.Errorf("output %s", b)
	}
}
