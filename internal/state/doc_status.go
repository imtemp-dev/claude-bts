package state

import (
	"regexp"
	"strings"
)

// A recipe document's status line: `Status: EXPLORING` in intent.md,
// `### Status: CONFIRMED` in scope.md, `**Status:** DRAFT` elsewhere.  Every
// reader goes through DocStatus and every writer through SetDocStatus, so
// the forms can no longer disagree, as workstate.go (`### Status:` only)
// and roadmap.go (a line starting `Status:` only) did (trial problem #88).
var docStatusLineRe = regexp.MustCompile(`(?m)^([ \t]*(?:#{1,6}[ \t]*)?(?:\*\*)?Status:(?:\*\*)?[ \t]*)([A-Za-z_-]+)`)

// DocStatus returns the value of a document's first status line, upper
// case, or "" when it has none.
func DocStatus(content string) string {
	m := docStatusLineRe.FindStringSubmatch(content)
	if m == nil {
		return ""
	}
	return strings.ToUpper(m[2])
}

// SetDocStatus sets the first status line's value and keeps its form.  A
// document with none gets `### Status: <status>` after its title.
func SetDocStatus(content, status string) string {
	if loc := docStatusLineRe.FindStringSubmatchIndex(content); loc != nil {
		return content[:loc[4]] + status + content[loc[5]:]
	}
	lines := strings.SplitAfter(content, "\n")
	for i, l := range lines {
		if strings.HasPrefix(l, "# ") {
			rest := strings.Join(lines[i+1:], "")
			rest = strings.TrimLeft(rest, "\n")
			return strings.Join(lines[:i+1], "") + "\n### Status: " + status + "\n\n" + rest
		}
	}
	return "### Status: " + status + "\n\n" + content
}
