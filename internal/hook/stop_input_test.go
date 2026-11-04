package hook

import (
	"encoding/json"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

// claudeCodeStopPayload is the Stop hook input as Claude Code sends it: the
// final assistant text arrives as last_assistant_message, and there is no
// "content" field (https://code.claude.com/docs/en/hooks, Stop input).
func claudeCodeStopPayload(t *testing.T, cwd, lastMessage string) []byte {
	t.Helper()
	data, err := json.Marshal(map[string]any{
		"session_id":             "00000000-0000-0000-0000-000000000000",
		"transcript_path":        "/tmp/transcript.jsonl",
		"cwd":                    cwd,
		"permission_mode":        "auto",
		"hook_event_name":        "Stop",
		"stop_hook_active":       false,
		"last_assistant_message": lastMessage,
	})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// A DONE marker in Claude Code's real Stop payload must reach the DONE path.
// The other stop tests set StopHookContent in Go and so never exercised the
// JSON field the marker actually arrives in.
func TestStopInput_DoneInLastAssistantMessageFinalizesABuildRecipe(t *testing.T) {
	root, id := setupBuildRoot(t)
	recordEvidence(t, root, id, 0, "INV-001", "INV-002")
	writeVerifyLog(t, root, id, []state.VerifyLogEntry{
		{Iteration: 1, Doc: "final.md", FullPass: true, Critical: 0, Major: 0, MinorResolvable: 4, Status: "continue"},
	})

	input, err := parseInput(claudeCodeStopPayload(t, root, "Review is clean.\n\n<bts>DONE</bts>"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	out, err := NewStopHandler().Handle(input)
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if out.Decision == "block" {
		t.Fatalf("a clean build recipe must finalize: %s", out.Reason)
	}
	if r, _ := state.LoadRecipeState(root, id); r.Phase != "finalize" {
		t.Fatalf("DONE in last_assistant_message must move the phase to finalize, got %q", r.Phase)
	}
}

// A completion marker counts only with its <bts> tags. Prose that names a
// marker (a report, an explanation of a block) must not run the handler:
// seen twice on the AGEN-35 trial, once in an owner session and once in
// the worker's, each time blocking a turn that completed nothing.
func TestStopInput_UntaggedMarkerNamesInProseDoNotRunTheHandlers(t *testing.T) {
	for _, prose := range []string{
		"The gate refused IMPLEMENT DONE because U-003 is unclassified.",
		"Waiting on the owner before FIX DONE.",
	} {
		root, id := setupImplementRoot(t)
		input, err := parseInput(claudeCodeStopPayload(t, root, prose))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := NewStopHandler().Handle(input); err != nil {
			t.Fatalf("handle: %v", err)
		}
		if r, _ := state.LoadRecipeState(root, id); r.Phase != "implement" {
			t.Fatalf("%q must not complete anything; phase is %q", prose, r.Phase)
		}
	}

	root, id := setupImplementRoot(t)
	input, _ := parseInput(claudeCodeStopPayload(t, root, "All gates met.\n\n<bts>IMPLEMENT DONE</bts>"))
	if out, err := NewStopHandler().Handle(input); err != nil || out.Decision == "block" {
		t.Fatalf("the tagged marker must still complete: %v %+v", err, out)
	}
	if r, _ := state.LoadRecipeState(root, id); r.Phase != "complete" {
		t.Fatalf("the tagged marker must complete the recipe, phase is %q", r.Phase)
	}
}

func TestStopInput_StopTextPrefersLastAssistantMessage(t *testing.T) {
	cases := []struct {
		name    string
		payload string
		want    string
	}{
		{"claude code", `{"hook_event_name":"Stop","last_assistant_message":"a <bts>DONE</bts>"}`, "a <bts>DONE</bts>"},
		{"legacy content", `{"hook_event_name":"Stop","content":"b <bts>DONE</bts>"}`, "b <bts>DONE</bts>"},
		{"both", `{"hook_event_name":"Stop","content":"old","last_assistant_message":"new"}`, "new"},
		{"neither", `{"hook_event_name":"Stop"}`, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			input, err := parseInput([]byte(c.payload))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if got := input.StopText(); got != c.want {
				t.Fatalf("StopText() = %q, want %q", got, c.want)
			}
		})
	}
}
