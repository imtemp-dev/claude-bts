package metrics

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// `bts stats` had tokens only from the status line's snapshots of the
// main session's current context: no recipe total, nothing from the
// subagents.  The trial measurements were summed from Claude Code's
// transcripts by hand (trial problem #13, Multica AGEN-66; jongyoon chose
// token totals from transcripts, no cost).
//
// Claude Code writes one line per content block, each repeating the
// message's usage, so a message is counted once by its id.
func TestSumTranscriptTokens(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", home)
	root := "/work/My.Project"
	dir := TranscriptDir(root)
	if want := filepath.Join(home, "projects", "-work-My-Project"); dir != want {
		t.Fatalf("transcript dir = %s, want %s", dir, want)
	}
	line := func(ts, id string, in, out, cw, cr int) string {
		return `{"type":"assistant","timestamp":"` + ts + `","message":{"id":"` + id + `","usage":{"input_tokens":` +
			itoa(in) + `,"output_tokens":` + itoa(out) + `,"cache_creation_input_tokens":` + itoa(cw) +
			`,"cache_read_input_tokens":` + itoa(cr) + `}}}`
	}
	write := func(rel string, lines ...string) {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("s1.jsonl",
		`{"type":"user","timestamp":"2026-11-05T10:00:00Z","message":{"content":"go"}}`,
		line("2026-11-05T10:00:01Z", "m1", 10, 100, 1000, 5000),
		line("2026-11-05T10:00:02Z", "m1", 10, 100, 1000, 5000), // the same message's next block
		line("2026-11-05T10:05:00Z", "m2", 1, 20, 0, 6000),
		line("2026-11-05T09:00:00Z", "m0", 7, 7, 7, 7), // before the recipe started
		`not json`,
	)
	write("s1/subagents/agent-a1.jsonl", line("2026-11-05T10:02:00Z", "m3", 2, 50, 300, 900))
	write("s1/subagents/agent-a2.jsonl", line("2026-11-05T10:03:00Z", "m4", 3, 60, 400, 800))
	write("s2.jsonl", line("2026-11-05T10:01:00Z", "m9", 99, 99, 99, 99)) // another session

	from, _ := time.Parse(time.RFC3339, "2026-11-05T09:30:00Z")
	got, err := SumTranscriptTokens(dir, []string{"s1"}, from, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if want := (TokenCounts{Input: 11, Output: 120, CacheWrite: 1000, CacheRead: 11000}); got.Main != want {
		t.Errorf("main = %+v, want %+v", got.Main, want)
	}
	if want := (TokenCounts{Input: 5, Output: 110, CacheWrite: 700, CacheRead: 1700}); got.Subagents != want {
		t.Errorf("subagents = %+v, want %+v", got.Subagents, want)
	}
	if got.SubagentRuns != 2 || got.Messages != 4 {
		t.Errorf("2 subagent runs and 4 messages, got %d and %d", got.SubagentRuns, got.Messages)
	}

	all, err := SumTranscriptTokens(dir, nil, from, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if all.Main.Input != 11+99 || len(all.Sessions) != 2 {
		t.Errorf("with no owner session every session in the window counts: %+v", all)
	}
}

func itoa(n int) string {
	var b []byte
	if n == 0 {
		return "0"
	}
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
