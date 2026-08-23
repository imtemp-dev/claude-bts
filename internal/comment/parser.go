// Package comment parses jig review callouts embedded in markdown docs.
//
// A callout is a GitHub-flavored alert blockquote with a jig marker:
//
//	> [!JIG-COMMENT]
//	> A general suggestion.
//
//	> [!JIG-BLOCK]
//	> Must resolve before recipe can finalize.
//
//	> [!JIG-Q]
//	> A question that needs an answer.
//
// Callouts may span multiple lines (every continuation line starts with `>`).
// They sit inline in the doc; the surrounding section heading and the
// adjacent prose lines act as anchors so /jig-comment-apply can place
// changes accurately even after the doc is edited.
package comment

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Kind classifies a callout's intent.
type Kind string

const (
	KindComment  Kind = "comment"  // [!JIG-COMMENT] — general suggestion
	KindBlock    Kind = "block"    // [!JIG-BLOCK]   — finalize-blocking
	KindQuestion Kind = "question" // [!JIG-Q]       — needs answer
	KindFreeForm Kind = "freeform" // diff fallback (no marker)

	// KindCascade is a synthetic kind emitted by /jig-comment-apply Pass A
	// (meta-analysis) for changes that must land in a doc OTHER than the
	// one carrying the originating callout. The Go-side parser never emits
	// this — only the skill does, when constructing resolved-comments.json.
	KindCascade Kind = "cascade"
)

// Comment is one parsed callout.
type Comment struct {
	ID           string   `json:"id"`
	File         string   `json:"file"`
	Kind         Kind     `json:"kind"`
	Line         int      `json:"line"`     // 1-based line of the marker
	EndLine      int      `json:"end_line"` // 1-based last line of the callout block
	Body         string   `json:"body"`
	SectionPath  []string `json:"section_path,omitempty"`  // ["# Title", "## Section", ...]
	AnchorBefore string   `json:"anchor_before,omitempty"` // up to 80 chars of preceding context
	AnchorAfter  string   `json:"anchor_after,omitempty"`  // up to 80 chars of following context
}

const anchorMaxLen = 80

var (
	// The legacy BTS- spelling is still accepted: a reviewer's comments live
	// in the recipe's own docs, and a rebrand must not make comments already
	// written there invisible — an unresolved [!BTS-BLOCK] has to keep
	// blocking finalize exactly as it did before.
	calloutOpenRE = regexp.MustCompile(`^>\s*\[!(?:JIG|BTS)-(COMMENT|BLOCK|Q)\]\s*$`)
	headingRE     = regexp.MustCompile(`^(#{1,6})\s+(.+?)\s*#*\s*$`)
	// Fence detection: leading 0–3 spaces (per CommonMark) then ``` or ~~~.
	// Matched against the raw line, not a trimmed copy, so deeply indented
	// runs like a 4-space code block don't get mistaken for a fence.
	fenceRE      = regexp.MustCompile("^[ ]{0,3}(`{3,}|~{3,})")
	blockquoteRE = regexp.MustCompile(`^>\s?(.*)$`)
)

func kindFromMarker(s string) Kind {
	switch s {
	case "COMMENT":
		return KindComment
	case "BLOCK":
		return KindBlock
	case "Q":
		return KindQuestion
	}
	return KindComment
}

// truncRunes returns s truncated to at most maxRunes runes (NOT bytes), so
// multi-byte UTF-8 sequences (Korean, Japanese, emoji) are never split mid-rune.
func truncRunes(s string, maxRunes int) string {
	if utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	rs := []rune(s)
	return string(rs[:maxRunes])
}

// truncRunesEllipsis is truncRunes with a trailing "…" when truncation occurred.
// maxRunes counts the ellipsis — so a budget of 60 leaves 59 runes + "…".
func truncRunesEllipsis(s string, maxRunes int) string {
	if utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	rs := []rune(s)
	return string(rs[:maxRunes-1]) + "…"
}

func snippet(s string) string {
	return truncRunes(strings.TrimSpace(s), anchorMaxLen)
}

// stableID hashes filename + line + normalized body so the ID survives
// whitespace-only edits to the body but discriminates between callouts
// that share identical body text in different locations within the same
// file. Including line is the right tradeoff: line shifts within a single
// /jig-comment-apply invocation are impossible (Pass B runs after parsing
// completes), and cross-invocation identity is not load-bearing — applied
// callouts are removed, not re-tracked.
func stableID(file, body string, line int) string {
	norm := strings.Join(strings.Fields(body), " ")
	key := fmt.Sprintf("%s\x00%d\x00%s", filepath.Base(file), line, norm)
	h := sha256.Sum256([]byte(key))
	return "c-" + hex.EncodeToString(h[:])[:8]
}

// ParseFile extracts all jig callouts from one markdown file.
// Comment.File is set to the basename of `path` — callers that need
// repository-relative paths should override after.
func ParseFile(path string) ([]Comment, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseLines(strings.Split(string(data), "\n"), filepath.Base(path)), nil
}

// ParseRecipe extracts all jig callouts from every *.md file in recipeDir
// (non-recursive — recipes are flat). Comment.File is set to the filename
// relative to recipeDir.
func ParseRecipe(recipeDir string) ([]Comment, error) {
	var out []Comment
	entries, err := os.ReadDir(recipeDir)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(strings.ToLower(name), ".md") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(recipeDir, name))
		if err != nil {
			return nil, err
		}
		out = append(out, parseLines(strings.Split(string(data), "\n"), name)...)
	}
	return out, nil
}

type heading struct {
	level int
	text  string
}

// indentedCodeBlock returns true when line is a CommonMark indented code
// line (4+ leading spaces, no tab normalization). Used to skip callout/
// heading detection inside literal code examples — without this, a doc
// that documents the jig callout syntax inside a 4-space code block
// would self-trigger.
func indentedCodeBlock(line string) bool {
	return len(line) >= 4 && line[:4] == "    "
}

func parseLines(lines []string, relFile string) []Comment {
	var out []Comment
	var stack []heading
	inFence := false

	for i := 0; i < len(lines); i++ {
		line := lines[i]

		if fenceRE.MatchString(line) {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		if indentedCodeBlock(line) {
			continue
		}

		// Track headings to compute section_path.
		if m := headingRE.FindStringSubmatch(line); m != nil {
			level := len(m[1])
			text := strings.TrimSpace(m[2])
			for len(stack) > 0 && stack[len(stack)-1].level >= level {
				stack = stack[:len(stack)-1]
			}
			stack = append(stack, heading{level: level, text: m[1] + " " + text})
			continue
		}

		// Detect callout opening line.
		trimmed := strings.TrimSpace(line)
		m := calloutOpenRE.FindStringSubmatch(trimmed)
		if m == nil {
			continue
		}

		startLine := i + 1
		kind := kindFromMarker(m[1])

		// Collect body lines: continue while line is a blockquote AND
		// not the start of another callout.
		var bodyLines []string
		j := i + 1
		for ; j < len(lines); j++ {
			t := strings.TrimSpace(lines[j])
			if calloutOpenRE.MatchString(t) {
				break
			}
			bm := blockquoteRE.FindStringSubmatch(lines[j])
			if bm == nil {
				break
			}
			bodyLines = append(bodyLines, bm[1])
		}

		body := strings.TrimSpace(strings.Join(bodyLines, "\n"))

		// Anchor: nearest non-blank, non-blockquote prose line above and below.
		before := ""
		for k := i - 1; k >= 0; k-- {
			t := strings.TrimSpace(lines[k])
			if t == "" || strings.HasPrefix(t, ">") {
				continue
			}
			before = t
			break
		}
		after := ""
		for k := j; k < len(lines); k++ {
			t := strings.TrimSpace(lines[k])
			if t == "" || strings.HasPrefix(t, ">") {
				continue
			}
			after = t
			break
		}

		sec := make([]string, len(stack))
		for k, h := range stack {
			sec[k] = h.text
		}

		// Skip empty-body callouts — they carry no actionable content
		// and would clutter every preview/list view. Still advance past
		// the consumed marker line so we don't re-enter the loop on it.
		if body == "" {
			i = j - 1
			continue
		}

		out = append(out, Comment{
			ID:           stableID(relFile, body, startLine),
			File:         relFile,
			Kind:         kind,
			Line:         startLine,
			EndLine:      j, // 1-based last callout line == j (j is 0-based first non-callout)
			Body:         body,
			SectionPath:  sec,
			AnchorBefore: snippet(before),
			AnchorAfter:  snippet(after),
		})

		i = j - 1 // skip past consumed body
	}

	return out
}

// ExtractFreeFormFromDiff scans `git diff HEAD` over recipeDir for added
// lines that are NOT part of a `> [!jig-...]` callout block, and returns
// them as pseudo-Comments with Kind=KindFreeForm.
//
// Behavior:
//   - Files with no prior commit (untracked or new) emit nothing — we
//     can't distinguish "added comment" from "newly drafted body".
//   - Lines inside a callout block are skipped (the callout parser owns those).
//   - Consecutive added lines are grouped into one freeform comment.
//
// This is intentionally simple — see /jig-comment-apply for how freeform
// comments are surfaced to the user (gated behind --include-freeform).
func ExtractFreeFormFromDiff(recipeDir string) ([]Comment, error) {
	cmd := exec.Command("git", "diff", "HEAD", "--unified=0", "--", recipeDir)
	out, err := cmd.Output()
	if err != nil {
		// No HEAD, not a git repo, or no diff — return empty without error.
		return nil, nil
	}
	return parseDiffFreeForm(string(out), recipeDir), nil
}

var (
	// `+++ b/<path>` is the default git diff format; `+++ <path>` (no
	// prefix) is what `git config diff.noprefix true` produces — common
	// enough that we accept both. `+++ /dev/null` (deletion) is handled
	// separately as a sentinel.
	diffFileRE = regexp.MustCompile(`^\+\+\+ (?:b/)?(.+)$`)
	diffHunkRE = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,\d+)? @@`)
)

func parseDiffFreeForm(diff, recipeDir string) []Comment {
	var out []Comment
	scanner := bufio.NewScanner(strings.NewReader(diff))
	scanner.Buffer(make([]byte, 1024*1024), 8*1024*1024)

	var (
		curFile  string // path relative to repo root, from `+++ b/<path>`
		curLine  int    // current line number in the new file
		bufLines []string
		bufStart int
	)

	flush := func() {
		defer func() { bufLines = nil }()
		if len(bufLines) == 0 || curFile == "" {
			return
		}
		// Drop hunks whose first line is a callout marker — owned by ParseFile.
		first := strings.TrimSpace(bufLines[0])
		if calloutOpenRE.MatchString(first) || strings.HasPrefix(first, ">") {
			return
		}
		body := strings.TrimSpace(strings.Join(bufLines, "\n"))
		if body == "" {
			return
		}
		// Normalize the path to a basename matching ParseRecipe's convention.
		// Recipes are flat (one level deep), so a freeform hunk in any nested
		// path is out-of-scope and dropped. This guarantees `--doc=draft.md`
		// matches both callouts AND freeform hunks for the same file.
		absRecipe, _ := filepath.Abs(recipeDir)
		absFile, _ := filepath.Abs(curFile)
		rel, err := filepath.Rel(absRecipe, absFile)
		if err != nil || strings.HasPrefix(rel, "..") {
			return
		}
		if strings.ContainsRune(rel, filepath.Separator) {
			return // nested — recipes are flat
		}
		out = append(out, Comment{
			ID:      stableID(rel, body, bufStart),
			File:    rel,
			Kind:    KindFreeForm,
			Line:    bufStart,
			EndLine: bufStart + len(bufLines) - 1,
			Body:    body,
		})
	}

	for scanner.Scan() {
		line := scanner.Text()
		if m := diffFileRE.FindStringSubmatch(line); m != nil {
			flush()
			path := m[1]
			if path == "/dev/null" {
				curFile = "" // deletion sentinel — no new content to surface
				continue
			}
			curFile = path
			continue
		}
		if m := diffHunkRE.FindStringSubmatch(line); m != nil {
			flush()
			// Hunk header is regex-matched as `\d+`, so this conversion
			// cannot fail. Discard the err to satisfy errcheck.
			if _, err := fmt.Sscanf(m[1], "%d", &curLine); err != nil {
				continue
			}
			bufStart = curLine
			continue
		}
		if strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---") {
			continue
		}
		if strings.HasPrefix(line, "+") {
			content := strings.TrimPrefix(line, "+")
			if len(bufLines) == 0 {
				bufStart = curLine
			}
			bufLines = append(bufLines, content)
			curLine++
			continue
		}
		// Context (' ') or removal ('-') breaks the run.
		flush()
	}
	flush()
	return out
}

// WalkMarkdownFiles is a small helper for callers (CLI, manifest) that need
// to enumerate the same file set ParseRecipe parses. Returned paths are
// absolute.
func WalkMarkdownFiles(recipeDir string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(recipeDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != recipeDir {
				return fs.SkipDir // recipes are flat
			}
			return nil
		}
		if strings.HasSuffix(strings.ToLower(p), ".md") {
			out = append(out, p)
		}
		return nil
	})
	return out, err
}
