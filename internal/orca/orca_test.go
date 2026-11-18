package orca

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

// fake answers one command with a canned reply and records its arguments.
func fake(t *testing.T, reply string) *[]string {
	t.Helper()
	var got []string
	t.Cleanup(SetRunner(func(_ context.Context, _ string, args ...string) ([]byte, error) {
		got = args
		return []byte(reply), nil
	}))
	return &got
}

func TestGetStatus_ReadsTheEnvelope(t *testing.T) {
	args := fake(t, `{"ok":true,"result":{"app":{"running":true},"runtime":{"reachable":true,"appVersion":"1.4.262"}}}`)
	st, err := GetStatus(context.Background(), "/p")
	if err != nil {
		t.Fatal(err)
	}
	if !st.Running || !st.Reachable || st.Version != "1.4.262" {
		t.Errorf("status %+v", st)
	}
	if !reflect.DeepEqual(*args, []string{"status", "--json"}) {
		t.Errorf("args %q", *args)
	}
}

// A refusal comes back in the envelope, with Orca's code and message.
func TestCall_ReportsARefusal(t *testing.T) {
	fake(t, `{"ok":false,"error":{"code":"runtime_unavailable","message":"Orca is restarting"}}`)
	_, err := GetStatus(context.Background(), "/p")
	var oe *Error
	if !errors.As(err, &oe) || oe.Code != "runtime_unavailable" || oe.Message != "Orca is restarting" {
		t.Fatalf("want the envelope's error, got %v", err)
	}
	fake(t, `not json`)
	if _, err := GetStatus(context.Background(), "/p"); err == nil {
		t.Error("an unreadable reply must be an error")
	}
}

// The launch asks for a terminal agent, passes --model only when one is
// named, and reads where the agent started.
func TestLaunchAgent(t *testing.T) {
	reply := `{"ok":true,"result":{"outcome":{"kind":"terminal","handle":"term_1","paneKey":"pk:1"},"worktreeId":"repo::/p","operationId":"op-9"}}`
	args := fake(t, reply)
	l, err := LaunchAgent(context.Background(), "/p", "claude", "", "hello")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"agent", "launch", "--agent", "claude", "--surface", "terminal", "--prompt", "hello", "--json"}
	if !reflect.DeepEqual(*args, want) {
		t.Errorf("args %q, want %q", *args, want)
	}
	if l.Terminal != "term_1" || l.PaneKey != "pk:1" || l.WorktreeID != "repo::/p" || l.OperationID != "op-9" || l.Kind != "terminal" {
		t.Errorf("launch %+v", l)
	}
	args = fake(t, reply)
	if _, err := LaunchAgent(context.Background(), "/p", "claude", "claude-sonnet-5-5", "hi"); err != nil {
		t.Fatal(err)
	}
	if (*args)[len(*args)-3] != "--model" || (*args)[len(*args)-2] != "claude-sonnet-5-5" {
		t.Errorf("--model must be passed when named, args %q", *args)
	}
}

func TestBinary_PrefersORCA_CLI_COMMAND(t *testing.T) {
	t.Setenv("ORCA_CLI_COMMAND", "/opt/orca-dev")
	if b, err := Binary(); err != nil || b != "/opt/orca-dev" {
		t.Errorf("Binary() = %q, %v", b, err)
	}
}

func TestInOrca(t *testing.T) {
	t.Setenv("ORCA_PANE_KEY", "")
	t.Setenv("ORCA_AGENT_PANE", "")
	if InOrca() {
		t.Error("no Orca variable, not in Orca")
	}
	t.Setenv("ORCA_PANE_KEY", "abc:def")
	if !InOrca() {
		t.Error("ORCA_PANE_KEY set, in Orca")
	}
}
