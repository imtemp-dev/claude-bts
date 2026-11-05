package hook

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

// writeTranscript writes a Claude Code transcript: each message as a
// thinking line and a text line sharing its id, as Claude Code writes them.
func writeTranscript(t *testing.T, dir string, texts ...string) string {
	t.Helper()
	var b strings.Builder
	je := json.NewEncoder(&b)
	je.SetEscapeHTML(false) // Claude Code writes "<bts>" as is
	enc := func(v any) { _ = je.Encode(v) }
	for i, text := range texts {
		id := "msg_" + string(rune('a'+i))
		enc(map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": "go on"}})
		enc(map[string]any{"type": "assistant", "isSidechain": false, "message": map[string]any{"id": id, "content": []any{map[string]any{"type": "thinking", "thinking": ""}}}})
		enc(map[string]any{"type": "assistant", "isSidechain": false, "message": map[string]any{"id": id, "content": []any{map[string]any{"type": "text", "text": text}}}})
	}
	p := filepath.Join(dir, "transcript.jsonl")
	if err := os.WriteFile(p, []byte(b.String()), 0644); err != nil {
		t.Fatal(err)
	}
	return p
}

// `<bts>IMPLEMENT DONE</bts>` did not apply on true-shark: the stop hook
// returned {} as for a turn with no marker, though the marker stood alone
// near the start of a 5,548-character message; the markers that worked
// ended short messages.  The payload's last message most likely carried
// only the tail (trial problem #85, Multica AGEN-69).  When the payload
// lacks a marker, the hook reads the turn's last assistant message from
// the transcript, and only when that message contains the payload's text.
func TestStop_FindsTheMarkerInTheTranscriptWhenThePayloadLacksIt(t *testing.T) {
	report := "All gates are green.\n\n" + strings.Repeat("A line of the closing report.\n", 200)
	full := "Done.\n\n<bts>IMPLEMENT DONE</bts>\n\n" + report
	tail := full[len(full)-600:]

	root, id := setupImplementRoot(t)
	setOwner(t, root, id, ownerSID)
	tr := writeTranscript(t, t.TempDir(), "an earlier answer", full)
	if out, _ := NewStopHandler().Handle(&HookInput{CWD: root, SessionID: ownerSID, LastAssistantMessage: tail, TranscriptPath: tr}); out.Decision == "block" {
		t.Fatalf("the marker in the turn's message must complete: %s", out.Reason)
	}
	if r, _ := state.LoadRecipeState(root, id); r.Phase == "implement" {
		t.Fatal("the marker the payload did not carry must still move the recipe on")
	}

	// A previous turn's marker never counts: the transcript's last message
	// must be the one the payload carries.
	root2, id2 := setupImplementRoot(t)
	setOwner(t, root2, id2, ownerSID)
	tr2 := writeTranscript(t, t.TempDir(), full)
	if out, _ := NewStopHandler().Handle(&HookInput{CWD: root2, SessionID: ownerSID, LastAssistantMessage: "Still working on the review fixes.", TranscriptPath: tr2}); out.Decision == "block" {
		t.Fatalf("no completion, so no gate: %s", out.Reason)
	}
	if r, _ := state.LoadRecipeState(root2, id2); r.Phase != "implement" {
		t.Fatalf("an earlier message's marker must complete nothing, phase is %q", r.Phase)
	}

	// A quoted marker in the transcript stays prose.
	root3, id3 := setupImplementRoot(t)
	setOwner(t, root3, id3, ownerSID)
	quoted := "The report quotes `<bts>IMPLEMENT DONE</bts>` only.\n\n" + report
	tr3 := writeTranscript(t, t.TempDir(), quoted)
	_, _ = NewStopHandler().Handle(&HookInput{CWD: root3, SessionID: ownerSID, LastAssistantMessage: quoted[len(quoted)-600:], TranscriptPath: tr3})
	if r, _ := state.LoadRecipeState(root3, id3); r.Phase != "implement" {
		t.Fatalf("a quoted marker must complete nothing, phase is %q", r.Phase)
	}
}
