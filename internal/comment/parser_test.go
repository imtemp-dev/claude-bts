package comment

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func writeMd(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0644); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
	return p
}

func TestParseFile_SingleComment(t *testing.T) {
	dir := t.TempDir()
	p := writeMd(t, dir, "draft.md", `# Draft

Some prose.

> [!BTS-COMMENT]
> Make this section concrete.

More prose.
`)
	cs, err := ParseFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 1 {
		t.Fatalf("want 1, got %d", len(cs))
	}
	c := cs[0]
	if c.Kind != KindComment {
		t.Errorf("kind: want comment, got %s", c.Kind)
	}
	if c.Body != "Make this section concrete." {
		t.Errorf("body: %q", c.Body)
	}
	if c.Line != 5 || c.EndLine != 6 {
		t.Errorf("lines: want 5..6, got %d..%d", c.Line, c.EndLine)
	}
	if c.AnchorBefore != "Some prose." {
		t.Errorf("anchor before: %q", c.AnchorBefore)
	}
	if c.AnchorAfter != "More prose." {
		t.Errorf("anchor after: %q", c.AnchorAfter)
	}
	if len(c.SectionPath) != 1 || c.SectionPath[0] != "# Draft" {
		t.Errorf("section path: %v", c.SectionPath)
	}
}

func TestParseFile_MultiLineBody(t *testing.T) {
	dir := t.TempDir()
	p := writeMd(t, dir, "draft.md", `> [!BTS-BLOCK]
> First line.
> Second line.
> Third line.

trailing prose
`)
	cs, _ := ParseFile(p)
	if len(cs) != 1 {
		t.Fatalf("want 1, got %d", len(cs))
	}
	if cs[0].Kind != KindBlock {
		t.Errorf("kind: %s", cs[0].Kind)
	}
	if cs[0].Body != "First line.\nSecond line.\nThird line." {
		t.Errorf("body: %q", cs[0].Body)
	}
}

func TestParseFile_BackToBackCallouts(t *testing.T) {
	dir := t.TempDir()
	p := writeMd(t, dir, "draft.md", `> [!BTS-COMMENT]
> first
> [!BTS-Q]
> second
`)
	cs, _ := ParseFile(p)
	if len(cs) != 2 {
		t.Fatalf("want 2 callouts, got %d", len(cs))
	}
	if cs[0].Body != "first" {
		t.Errorf("first body: %q", cs[0].Body)
	}
	if cs[1].Body != "second" || cs[1].Kind != KindQuestion {
		t.Errorf("second: kind=%s body=%q", cs[1].Kind, cs[1].Body)
	}
}

func TestParseFile_SectionPathStack(t *testing.T) {
	dir := t.TempDir()
	p := writeMd(t, dir, "draft.md", `# Top

## A
content

### A1
content

## B
content

> [!BTS-COMMENT]
> on B
`)
	cs, _ := ParseFile(p)
	if len(cs) != 1 {
		t.Fatalf("want 1, got %d", len(cs))
	}
	want := []string{"# Top", "## B"}
	if len(cs[0].SectionPath) != len(want) {
		t.Fatalf("section path len: %v", cs[0].SectionPath)
	}
	for i, w := range want {
		if cs[0].SectionPath[i] != w {
			t.Errorf("section[%d]: want %q got %q", i, w, cs[0].SectionPath[i])
		}
	}
}

func TestParseFile_CalloutAtEOF(t *testing.T) {
	dir := t.TempDir()
	p := writeMd(t, dir, "draft.md", "# X\n\n> [!BTS-COMMENT]\n> tail")
	cs, _ := ParseFile(p)
	if len(cs) != 1 {
		t.Fatalf("want 1, got %d", len(cs))
	}
	if cs[0].Body != "tail" {
		t.Errorf("body: %q", cs[0].Body)
	}
}

func TestParseFile_IgnoresCalloutInsideCodeFence(t *testing.T) {
	dir := t.TempDir()
	p := writeMd(t, dir, "draft.md", "# X\n\n```markdown\n> [!BTS-COMMENT]\n> not a real comment\n```\n")
	cs, _ := ParseFile(p)
	if len(cs) != 0 {
		t.Fatalf("want 0 (inside fence), got %d", len(cs))
	}
}

func TestParseFile_IgnoresPlainBlockquote(t *testing.T) {
	dir := t.TempDir()
	p := writeMd(t, dir, "draft.md", "> a quote\n> not a BTS callout\n")
	cs, _ := ParseFile(p)
	if len(cs) != 0 {
		t.Fatalf("want 0, got %d", len(cs))
	}
}

func TestStableID_SameBodySameID(t *testing.T) {
	a := stableID("draft.md", "Hello world", 5)
	b := stableID("draft.md", "Hello   world", 5)
	if a != b {
		t.Errorf("whitespace-only diff should be same ID: %s vs %s", a, b)
	}
}

func TestStableID_DifferentBody(t *testing.T) {
	a := stableID("draft.md", "Hello", 5)
	b := stableID("draft.md", "Goodbye", 5)
	if a == b {
		t.Errorf("different body should differ")
	}
}

func TestStableID_DifferentFile(t *testing.T) {
	a := stableID("draft.md", "X", 5)
	b := stableID("scope.md", "X", 5)
	if a == b {
		t.Errorf("different file should differ")
	}
}

// CRITICAL #3 — duplicate body in the same file at different lines must
// get different IDs, otherwise the skill's conflict resolution can't
// distinguish them.
func TestStableID_DuplicateBodyDifferentLineDiffers(t *testing.T) {
	a := stableID("draft.md", "clarify this", 12)
	b := stableID("draft.md", "clarify this", 47)
	if a == b {
		t.Errorf("duplicate body at different lines must differ: %s == %s", a, b)
	}
}

func TestParseFile_DuplicateBodyGetsDistinctIDs(t *testing.T) {
	dir := t.TempDir()
	p := writeMd(t, dir, "draft.md", `# X

> [!BTS-COMMENT]
> same body

intermediate prose

> [!BTS-COMMENT]
> same body
`)
	cs, _ := ParseFile(p)
	if len(cs) != 2 {
		t.Fatalf("want 2, got %d", len(cs))
	}
	if cs[0].ID == cs[1].ID {
		t.Errorf("duplicate-body callouts must get distinct IDs: both %s", cs[0].ID)
	}
}

func TestParseRecipe_AcrossMultipleDocs(t *testing.T) {
	dir := t.TempDir()
	writeMd(t, dir, "draft.md", "> [!BTS-COMMENT]\n> a\n")
	writeMd(t, dir, "scope.md", "> [!BTS-BLOCK]\n> b\n")
	writeMd(t, dir, "manifest.json", `{"x":1}`) // non-md, skipped
	// nested dir should be ignored (recipes are flat)
	if err := os.Mkdir(filepath.Join(dir, "nested"), 0755); err != nil {
		t.Fatal(err)
	}
	writeMd(t, dir, filepath.Join("nested", "buried.md"), "> [!BTS-COMMENT]\n> nope\n")

	cs, err := ParseRecipe(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 2 {
		t.Fatalf("want 2 (draft+scope only), got %d: %+v", len(cs), cs)
	}
	files := map[string]bool{cs[0].File: true, cs[1].File: true}
	if !files["draft.md"] || !files["scope.md"] {
		t.Errorf("files: %v", files)
	}
}

func TestParseFile_AllThreeKinds(t *testing.T) {
	dir := t.TempDir()
	p := writeMd(t, dir, "x.md", `> [!BTS-COMMENT]
> a

> [!BTS-BLOCK]
> b

> [!BTS-Q]
> c
`)
	cs, _ := ParseFile(p)
	if len(cs) != 3 {
		t.Fatalf("want 3, got %d", len(cs))
	}
	if cs[0].Kind != KindComment || cs[1].Kind != KindBlock || cs[2].Kind != KindQuestion {
		t.Errorf("kinds: %s %s %s", cs[0].Kind, cs[1].Kind, cs[2].Kind)
	}
}

func TestParseFile_AnchorTruncated(t *testing.T) {
	long := strings.Repeat("x", 200)
	dir := t.TempDir()
	p := writeMd(t, dir, "x.md", long+"\n\n> [!BTS-COMMENT]\n> body\n")
	cs, _ := ParseFile(p)
	if len(cs) != 1 {
		t.Fatalf("want 1, got %d", len(cs))
	}
	if len(cs[0].AnchorBefore) != anchorMaxLen {
		t.Errorf("anchor before len: want %d got %d", anchorMaxLen, len(cs[0].AnchorBefore))
	}
}

func TestParseDiffFreeForm_ExtractsAddedLines(t *testing.T) {
	diff := `diff --git a/specs/draft.md b/specs/draft.md
index abc..def 100644
--- a/specs/draft.md
+++ b/specs/draft.md
@@ -10,0 +11,2 @@
+please add error handling here
+also clarify retry behavior
`
	out := parseDiffFreeForm(diff, "specs")
	if len(out) != 1 {
		t.Fatalf("want 1, got %d", len(out))
	}
	if !strings.Contains(out[0].Body, "please add error handling") {
		t.Errorf("body: %q", out[0].Body)
	}
	if out[0].File != "draft.md" {
		t.Errorf("file: %s", out[0].File)
	}
	if out[0].Kind != KindFreeForm {
		t.Errorf("kind: %s", out[0].Kind)
	}
}

func TestParseDiffFreeForm_SkipsCalloutLines(t *testing.T) {
	diff := `diff --git a/specs/draft.md b/specs/draft.md
--- a/specs/draft.md
+++ b/specs/draft.md
@@ -10,0 +11,2 @@
+> [!BTS-COMMENT]
+> a real callout
`
	out := parseDiffFreeForm(diff, "specs")
	if len(out) != 0 {
		t.Fatalf("want 0 (callout owned by ParseFile), got %d", len(out))
	}
}

// MAJOR #8 — `git config diff.noprefix true` strips the b/ prefix.
// Parser must accept `+++ <path>` as well as `+++ b/<path>`.
func TestParseDiffFreeForm_NoPrefix(t *testing.T) {
	diff := `--- specs/draft.md
+++ specs/draft.md
@@ -10,0 +11,1 @@
+please clarify
`
	out := parseDiffFreeForm(diff, "specs")
	if len(out) != 1 {
		t.Fatalf("want 1 freeform with no-prefix diff, got %d", len(out))
	}
	if out[0].File != "draft.md" {
		t.Errorf("file: %q", out[0].File)
	}
}

// MAJOR #8 — `+++ /dev/null` (deletion sentinel) must be ignored.
func TestParseDiffFreeForm_DevNullSentinelIgnored(t *testing.T) {
	diff := `--- a/specs/draft.md
+++ /dev/null
@@ -1,2 +0,0 @@
-line gone
-line gone
`
	out := parseDiffFreeForm(diff, "specs")
	if len(out) != 0 {
		t.Fatalf("want 0 (pure deletion), got %d: %+v", len(out), out)
	}
}

// CRITICAL #4 — File path convention must match between ParseRecipe (basename)
// and ExtractFreeFormFromDiff so `--doc=draft.md` filters consistently.
func TestParseDiffFreeForm_FileFieldIsBasename(t *testing.T) {
	diff := `+++ b/specs/draft.md
@@ -10,0 +11,1 @@
+freeform addition
`
	out := parseDiffFreeForm(diff, "specs")
	if len(out) != 1 {
		t.Fatalf("want 1, got %d", len(out))
	}
	if out[0].File != "draft.md" {
		t.Errorf("File should be basename 'draft.md', got %q", out[0].File)
	}
}

// CRITICAL #4 — Nested paths (recipe is flat) must be dropped, not appear
// with subdir/ prefix that would never match a basename --doc filter.
func TestParseDiffFreeForm_NestedPathsDropped(t *testing.T) {
	diff := `+++ b/specs/sub/buried.md
@@ -10,0 +11,1 @@
+nope
`
	out := parseDiffFreeForm(diff, "specs")
	if len(out) != 0 {
		t.Fatalf("nested paths should be dropped (recipes are flat), got %d", len(out))
	}
}

// MINOR #16 — Empty-body callouts (marker with nothing after) must not
// pollute output. They carry no actionable content.
func TestParseFile_EmptyBodyCalloutSkipped(t *testing.T) {
	dir := t.TempDir()
	p := writeMd(t, dir, "x.md", `# X

> [!BTS-COMMENT]

> [!BTS-BLOCK]
> not empty
`)
	cs, _ := ParseFile(p)
	if len(cs) != 1 {
		t.Fatalf("want 1 (empty-body skipped), got %d: %+v", len(cs), cs)
	}
	if cs[0].Kind != KindBlock {
		t.Errorf("expected the BLOCK to survive, got %s", cs[0].Kind)
	}
}

// MINOR #11/#12 — A callout marker inside a 4-space indented code block
// is documentation, not a real callout.
func TestParseFile_IndentedCodeBlockIgnoresMarker(t *testing.T) {
	dir := t.TempDir()
	p := writeMd(t, dir, "x.md", "# Doc\n\n    > [!BTS-COMMENT]\n    > example syntax\n\nreal prose\n")
	cs, _ := ParseFile(p)
	if len(cs) != 0 {
		t.Fatalf("want 0 (marker is in indented code), got %d", len(cs))
	}
}

// CRITICAL #2 — Anchor truncation must rune-truncate, not byte-truncate.
// Korean text is 3 bytes/rune; byte truncation produces invalid UTF-8.
func TestParseFile_AnchorTruncationIsRuneSafe(t *testing.T) {
	// 100 Korean syllables ≈ 300 bytes. anchorMaxLen=80 runes.
	long := strings.Repeat("가", 100)
	dir := t.TempDir()
	p := writeMd(t, dir, "x.md", long+"\n\n> [!BTS-COMMENT]\n> body\n")
	cs, _ := ParseFile(p)
	if len(cs) != 1 {
		t.Fatalf("want 1, got %d", len(cs))
	}
	anchor := cs[0].AnchorBefore
	if !utf8.ValidString(anchor) {
		t.Errorf("anchor is invalid UTF-8: %q", anchor)
	}
	if utf8.RuneCountInString(anchor) != anchorMaxLen {
		t.Errorf("anchor rune count: want %d got %d", anchorMaxLen, utf8.RuneCountInString(anchor))
	}
}

// CRITICAL #1/#2 — singleLine in render.go must rune-truncate too.
func TestSingleLine_NonASCIIRuneSafe(t *testing.T) {
	long := strings.Repeat("한", 100)
	out := singleLine(long, 60)
	if !utf8.ValidString(out) {
		t.Errorf("singleLine produced invalid UTF-8: %q", out)
	}
	// 59 runes + "…" = 60 runes total
	if utf8.RuneCountInString(out) != 60 {
		t.Errorf("rune count: want 60, got %d", utf8.RuneCountInString(out))
	}
}

// truncRunesEllipsis must place the ellipsis where the rune budget runs out,
// never inside a multi-byte sequence.
func TestTruncRunesEllipsis_RuneBoundary(t *testing.T) {
	in := "abc🔥def🔥ghi"
	out := truncRunesEllipsis(in, 6)
	if !utf8.ValidString(out) {
		t.Errorf("invalid UTF-8: %q", out)
	}
	// 5 runes + "…" = 6 runes
	if utf8.RuneCountInString(out) != 6 {
		t.Errorf("rune count: want 6, got %d", utf8.RuneCountInString(out))
	}
}
