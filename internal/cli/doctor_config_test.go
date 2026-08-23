package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
}

const jigHookJSON = `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"/p/.claude/hooks/jig-handle-stop.sh"}]}],
"PostToolUse":[{"hooks":[{"type":"command","command":"/p/.claude/hooks/jig-handle-post-tool-use.sh"}]}]}}`

// Claude Code merges hook configuration across scopes, so a jig hook in
// two scopes runs twice per event. A measured project's metrics.jsonl
// held 11,394 lines of which 5,991 were distinct.
func TestCheckDuplicateHookRegistration(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".claude", "settings.local.json"), jigHookJSON)
	if issues := duplicateHookRegistration(root, ""); len(issues) != 0 {
		t.Fatalf("one scope is the normal case, got %v", issues)
	}

	writeFile(t, filepath.Join(root, ".claude", "settings.json"), jigHookJSON)
	issues := duplicateHookRegistration(root, "")
	if len(issues) != 1 {
		t.Fatalf("got %d issues, want 1: %+v", len(issues), issues)
	}
	for _, want := range []string{"Stop", "PostToolUse", "twice"} {
		if !strings.Contains(issues[0].message, want) {
			t.Errorf("message should mention %q, got %q", want, issues[0].message)
		}
	}
}

// A hook that is not jig's own must not count — other tools register
// hooks in the same file.
func TestCheckDuplicateHookRegistration_IgnoresForeignHooks(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".claude", "settings.local.json"), jigHookJSON)
	writeFile(t, filepath.Join(root, ".claude", "settings.json"),
		`{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"/usr/local/bin/some-other-tool"}]}]}}`)
	if issues := duplicateHookRegistration(root, ""); len(issues) != 0 {
		t.Fatalf("a foreign hook is not a duplicate jig registration, got %v", issues)
	}
}

// A setting that looks authoritative and changes nothing is worse than
// an absent one, because it is consulted and believed. The measured
// project set verify.convergence.* and context.clear_threshold: 80, and
// its sessions ran to 95%.
func TestCheckUnreadSettings(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".jig", "config", "settings.yaml"), `
verify:
  max_iterations: 3
  confirm_passes: 2
  convergence:
    require_zero_critical: true
    allow_minor: true
context:
  clear_threshold: 80
agents:
  verifier: sonnet
`)
	issues := checkUnreadSettings(root)
	if len(issues) != 1 {
		t.Fatalf("got %d issues, want 1: %+v", len(issues), issues)
	}
	msg := issues[0].message
	for _, want := range []string{
		"verify.convergence.require_zero_critical",
		"verify.convergence.allow_minor",
		"context.clear_threshold",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("should report %q, got %q", want, msg)
		}
	}
	for _, unwanted := range []string{"verify.max_iterations", "verify.confirm_passes", "agents"} {
		if strings.Contains(msg, unwanted) {
			t.Errorf("%q IS read and must not be reported, got %q", unwanted, msg)
		}
	}
}

func TestCheckUnreadSettings_CleanFileIsQuiet(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".jig", "config", "settings.yaml"), `
verify:
  max_iterations: 3
simulate:
  min_scenarios: 5
`)
	if issues := checkUnreadSettings(root); len(issues) != 0 {
		t.Fatalf("want no issues, got %+v", issues)
	}
}

// The user scope is a real scope and must be counted — but as an
// argument, not by reading whatever machine the tests happen to run on.
func TestDuplicateHookRegistration_CountsTheUserScope(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	writeFile(t, filepath.Join(root, ".claude", "settings.local.json"), jigHookJSON)

	if issues := duplicateHookRegistration(root, home); len(issues) != 0 {
		t.Fatalf("a home with no jig hooks adds no scope, got %v", issues)
	}

	writeFile(t, filepath.Join(home, ".claude", "settings.json"), jigHookJSON)
	issues := duplicateHookRegistration(root, home)
	if len(issues) != 1 {
		t.Fatalf("got %d issues, want 1: %+v", len(issues), issues)
	}
	if !strings.Contains(issues[0].message, "user ~/.claude/settings.json") {
		t.Errorf("the report must name the user scope, got %q", issues[0].message)
	}
}

// One settings file registering several jig handlers for one event is
// one scope, not two. Counting each handler separately made the check
// report a duplicate against a single file.
func TestDuplicateHookRegistration_ManyHandlersInOneFileIsOneScope(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".claude", "settings.json"), `{
  "hooks": {
    "PostToolUse": [
      {"hooks": [{"command": ".claude/hooks/jig-handle-post-tool-use.sh"}]},
      {"hooks": [{"command": ".claude/hooks/jig-handle-metrics.sh"}]}
    ]
  }
}`)
	if issues := duplicateHookRegistration(root, ""); len(issues) != 0 {
		t.Errorf("two handlers in one file are one registration scope, got %v", issues)
	}
}
