package engine

import (
	"regexp"
	"strings"
)

// The wireframe's File Structure table as a task-anchor source.
//
// bts-wireframe/SKILL.md § Step 4 has always said of that table: "This
// becomes the basis for task decomposition in /bts-implement." It was
// never wired. /bts-implement read final.md alone, so the blueprint had
// to carry a per-file section for every unit the implementation would
// touch — and that requirement, not any single instruction, is what made
// a blueprint a transcription of the code.
//
// One measured recipe shows the cost precisely: wireframe.md § 4 held 31
// rows of `File | Action | Depends On | Responsibility` in 47 lines, and
// draft.md § 2-4 re-expanded those same 31 rows into 1,322 lines of
// per-file prose. The verify loop then spent itself on the re-expansion:
// the three transcription sections drew over two hundred findings while
// the 25-line contract section drew eleven.
//
// Reading the table directly removes the reason to copy it. A row IS the
// anchor: it already carries exactly the (path, action) pair tasks.json
// is keyed on.

var fileStructureHeadingRe = regexp.MustCompile(`(?im)^#{1,6}[^\n]*\bfile structure\b`)

// tableRowSplitRe splits a markdown table row into cells.
var tableSeparatorRowRe = regexp.MustCompile(`^[\s|:-]+$`)

// cellCleanupRe strips the decoration specs put around a path or an
// action — backticks and bold/italic asterisks — so `**modify**` and
// modify are the same answer.
//
// Underscores are NOT stripped: they are markdown italics only at a
// cell's edges, and inside a path they are the path.
// `0026_community_bundle_cover_url.sql` became
// `0026communitybundlecoverurl.sql` when they were.
var cellCleanupRe = regexp.MustCompile("[`*]")

// cleanCell removes decoration without touching the content. Edge
// underscores go; interior ones stay.
func cleanCell(s string) string {
	return strings.Trim(strings.TrimSpace(cellCleanupRe.ReplaceAllString(s, "")), "_")
}

// anchorActions are the actions a task can carry. A File Structure row
// may legitimately name others — one measured wireframe marked a file
// `unchanged` to record that it needed no edit — and those are not
// tasks, so they are not anchors.
var anchorActions = map[string]bool{"create": true, "modify": true, "delete": true}

func splitTableRow(line string) []string {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "|") {
		return nil
	}
	line = strings.Trim(line, "|")
	cells := strings.Split(line, "|")
	for i := range cells {
		cells[i] = strings.TrimSpace(cells[i])
	}
	return cells
}

// columnIndex finds a header cell by any of the given names.
// Header order is not fixed: the template writes `File | Action |
// Depends On | Responsibility`, and a measured wireframe prefixed a `#`
// column, which would shift every positional read by one.
func columnIndex(header []string, names ...string) int {
	for i, h := range header {
		h = strings.ToLower(cleanCell(h))
		for _, want := range names {
			if h == want {
				return i
			}
		}
	}
	return -1
}

// ParseWireframeFileTable returns the task anchors declared by the File
// Structure table, in document order. Anything else in the wireframe is
// ignored, and a wireframe without the section returns nothing.
func ParseWireframeFileTable(content string) []TaskAnchorKey {
	loc := fileStructureHeadingRe.FindStringIndex(content)
	if loc == nil {
		return nil
	}
	section := content[loc[1]:]
	// Stop at the next heading of the same level or higher so a later
	// section's table cannot be read as this one's.
	if end := regexp.MustCompile(`(?m)^#{1,2}\s`).FindStringIndex(section); end != nil {
		section = section[:end[0]]
	}

	var header []string
	var fileCol, actionCol int
	var out []TaskAnchorKey
	seen := map[TaskAnchorKey]bool{}
	for _, line := range strings.Split(section, "\n") {
		cells := splitTableRow(line)
		if len(cells) == 0 {
			continue
		}
		if header == nil {
			header = cells
			fileCol = columnIndex(header, "file", "path", "파일")
			actionCol = columnIndex(header, "action", "동작", "액션")
			if fileCol < 0 || actionCol < 0 {
				// Not the table we are looking for; keep scanning in case
				// the section opens with an unrelated one.
				header = nil
			}
			continue
		}
		if tableSeparatorRowRe.MatchString(strings.Trim(line, " ")) {
			continue
		}
		if fileCol >= len(cells) || actionCol >= len(cells) {
			continue
		}
		path := cleanCell(cells[fileCol])
		action := strings.ToLower(cleanCell(cells[actionCol]))
		if path == "" || !anchorActions[action] {
			continue
		}
		key := TaskAnchorKey{Path: path, Action: action}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, key)
	}
	return out
}
