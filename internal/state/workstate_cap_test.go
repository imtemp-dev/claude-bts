package state

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// work-state.json is re-read at every session start and after every
// compaction, so its size is a recurring token cost. A measured project's
// file reached 37KB, one last_actions entry alone being 8,352 characters
// of changelog prose copied verbatim.
func TestSaveWorkStateCapsUnboundedFields(t *testing.T) {
	root := t.TempDir()
	long := strings.Repeat("round-33 propagated the fix and recorded the rationale. ", 200)
	ws := &WorkState{
		RecipeID:    "r-001",
		LastActions: []string{"improve draft.md (" + long + ")", "verify"},
		Summary:     long,
		RecentTools: []ToolTraceEntry{
			{ToolName: "Bash", Command: "cd /Users/someone/Workspace/project/.bts/specs/recipes/r-001-a-fairly-long-recipe-identifier && bts recipe log r-001 --action improve --output draft.md"},
		},
	}
	if err := SaveWorkState(root, ws); err != nil {
		t.Fatal(err)
	}
	back, err := LoadWorkState(root)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(back.LastActions[0]); n > maxActionChars+80 {
		t.Errorf("last action is %d chars, want it capped near %d", n, maxActionChars)
	}
	if !strings.Contains(back.LastActions[0], "truncated") {
		t.Error("a clipped entry must say so, or a reader cannot tell it from a short one")
	}
	if back.LastActions[1] != "verify" {
		t.Errorf("a short entry must pass through untouched, got %q", back.LastActions[1])
	}
	if n := len(back.Summary); n > maxSummaryChars+80 {
		t.Errorf("summary is %d chars, want it capped near %d", n, maxSummaryChars)
	}
}

// The hooks cut commands at 100 bytes mid-token, producing traces whose
// visible form is a shell prefix with the actual verb cut off.
func TestClipCommandDoesNotLeaveABareShellPrefix(t *testing.T) {
	got := ClipCommand("cd /Users/someone/Workspace/a-project-with-a-rather-long-name/.bts/specs/recipes/r-001-support-gpt-solterra && " +
		"bts recipe log r-001-support-gpt-solterra --from-verification verification.md --doc draft.md --scope full --dimension verify")
	if strings.HasSuffix(strings.TrimSpace(got), "&&") {
		t.Errorf("clipped command ends on a dangling separator: %q", got)
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("a clipped command must be marked as partial, got %q", got)
	}
	if len(got) > maxCommandChars+8 {
		t.Errorf("clipped command is %d chars, want near %d", len(got), maxCommandChars)
	}

	short := "bts recipe status"
	if got := ClipCommand(short); got != short {
		t.Errorf("a short command must pass through untouched, got %q", got)
	}
}

// Multi-byte titles must not be split mid-rune: bts specs are frequently
// Korean.
func TestClipIsRuneSafe(t *testing.T) {
	s := strings.Repeat("검증 라운드가 수렴하지 않았다. ", 100)
	got := clip(s, 100)
	if !strings.Contains(got, "truncated") {
		t.Fatal("expected a truncation marker")
	}
	for _, r := range got {
		if r == '�' {
			t.Fatal("clip split a multi-byte character")
		}
	}
}

// The helpers this replaces tested the budget in BYTES and then sliced
// RUNES, so the same call both let a long Korean string through and,
// when it did clip, appended a 43-character marker on top of a
// full-budget cut — clipping a 250-byte action produced 285 bytes.
func TestClipNeverGrowsAndBudgetsInRunes(t *testing.T) {
	ascii := strings.Repeat("x", 250)
	got := clip(ascii, 240)
	if len([]rune(got)) > 240 {
		t.Errorf("clip returned %d runes against a budget of 240", len([]rune(got)))
	}
	if len(got) >= len(ascii) {
		t.Errorf("clip grew the string: %d -> %d bytes", len(ascii), len(got))
	}

	// 1,645 Korean runes: well under 2,000 runes but far over 2,000 bytes.
	korean := strings.Repeat("검증 라운드가 수렴하지 않았다. ", 100)
	if n := len([]rune(clip(korean, 2000))); n > 2000 {
		t.Errorf("Korean summary kept %d runes against a budget of 2000", n)
	}
	long := strings.Repeat("검증 라운드가 수렴하지 않았다. ", 400)
	out := clip(long, 2000)
	if n := len([]rune(out)); n > 2000 {
		t.Errorf("clipped to %d runes, want <= 2000", n)
	}
	if len(out) >= len(long) {
		t.Errorf("clip grew the string: %d -> %d bytes", len(long), len(out))
	}
	if !strings.Contains(out, "truncated") {
		t.Error("a clipped entry must say so")
	}
}

// TruncateRunes replaced a helper that sliced raw bytes at n-1: it
// returned invalid UTF-8 on any multi-byte input and panicked outright
// at n <= 0.
func TestTruncateRunesIsSafeAtEveryBound(t *testing.T) {
	s := "검증 라운드가 수렴하지 않았다"
	for _, n := range []int{-5, 0, 1, 2, 3, 7, len([]rune(s)), len([]rune(s)) + 5, 1000} {
		got := TruncateRunes(s, n)
		if !utf8.ValidString(got) {
			t.Errorf("TruncateRunes(%q, %d) = %q, not valid UTF-8", s, n, got)
		}
		if n > 0 && len([]rune(got)) > n {
			t.Errorf("TruncateRunes(_, %d) returned %d runes", n, len([]rune(got)))
		}
		if len(got) > len(s) {
			t.Errorf("TruncateRunes(_, %d) grew the string", n)
		}
	}
	if got := TruncateRunes(s, 0); got != "" {
		t.Errorf("a zero budget must yield nothing, got %q", got)
	}
}

// ClipCommand compared a BYTE index from strings.LastIndexAny against a
// RUNE budget, so on a Korean command the "keeps most of it" guard
// passed at an offset worth a fraction of the allowance.
func TestClipCommandKeepsMostOfTheBudgetOnMultibyteInput(t *testing.T) {
	cmd := "bts recipe log r-001 --reason " + strings.Repeat("검증 라운드가 수렴하지 않았다 ", 40)
	got := ClipCommand(cmd)
	if !utf8.ValidString(got) {
		t.Fatalf("ClipCommand produced invalid UTF-8: %q", got)
	}
	n := len([]rune(got))
	if n > maxCommandChars {
		t.Errorf("kept %d runes against a budget of %d", n, maxCommandChars)
	}
	if n < maxCommandChars/2 {
		t.Errorf("kept only %d runes of a %d budget — the whitespace back-off over-trimmed", n, maxCommandChars)
	}
}
