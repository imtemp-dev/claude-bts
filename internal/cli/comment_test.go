package cli

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// CRITICAL #1 — `compact` previously cast each rune to byte(r), which
// silently truncated anything above U+00FF and produced mojibake on
// Korean/Japanese/emoji bodies. After the fix, multibyte runes round-trip
// correctly through whitespace collapse.
func TestCompact_PreservesNonASCIIRunes(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"hello world", "hello world"},
		{"hello   world", "hello world"},
		{"hello\tworld", "hello world"},
		{"hello\n\nworld", "hello world"},
		{"안녕  세상", "안녕 세상"},
		{"fix this 🔥 issue", "fix this 🔥 issue"},
		{"  leading and trailing  ", " leading and trailing "}, // outer whitespace collapsed but kept
	}
	for _, tc := range cases {
		got := compact(tc.in)
		if got != tc.want {
			t.Errorf("compact(%q):\n  got  %q\n  want %q", tc.in, got, tc.want)
		}
		if !utf8.ValidString(got) {
			t.Errorf("compact(%q) produced invalid UTF-8: %q", tc.in, got)
		}
	}
}

// CRITICAL #1/#2 — singleLine in cli must rune-truncate for non-ASCII bodies.
func TestSingleLine_NonASCIIRuneTruncation(t *testing.T) {
	long := strings.Repeat("한", 200) // 200 Korean syllables, ~600 bytes
	out := singleLine(long)
	if !utf8.ValidString(out) {
		t.Errorf("invalid UTF-8: %q", out)
	}
	// budget is 80 runes; 79 + ellipsis = 80
	if utf8.RuneCountInString(out) != 80 {
		t.Errorf("rune count: want 80, got %d", utf8.RuneCountInString(out))
	}
	if !strings.HasSuffix(out, "…") {
		t.Errorf("should end with ellipsis, got tail: %q", out[len(out)-6:])
	}
}

// Regression: short strings must not be truncated.
func TestSingleLine_ShortPassesThrough(t *testing.T) {
	in := "안녕 세상"
	out := singleLine(in)
	if out != in {
		t.Errorf("short string mangled: %q -> %q", in, out)
	}
}
