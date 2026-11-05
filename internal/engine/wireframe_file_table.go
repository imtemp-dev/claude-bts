package engine

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/imtemp-dev/claude-bts/internal/mdtable"
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

// cleanCell removes decoration without touching the content.
//
// An underscore is markdown italics only when it wraps the WHOLE cell,
// so only a wrapping pair comes off. Trimming both edges instead renamed
// the file: `__init__.py` became `init__.py` and `_layout.tsx` became
// `layout.tsx`, and each produced a CRITICAL missing_anchor against a
// tasks.json that spells the name correctly — plus, on a recipe whose
// anchors come from the table, a CRITICAL orphan_anchor for the
// corrupted key alongside it.
func cleanCell(s string) string {
	s = strings.TrimSpace(cellCleanupRe.ReplaceAllString(s, ""))
	for len(s) > 2 && s[0] == '_' && s[len(s)-1] == '_' {
		s = s[1 : len(s)-1]
	}
	return s
}

// anchorActions are the actions a task can carry. A File Structure row
// may legitimately name others — one measured wireframe marked a file
// `unchanged` to record that it needed no edit — and those are not
// tasks, so they are not anchors. `generate` is one of those too: its
// file is written by a generator, never by a task (ParseWireframeGenerateRows).
var anchorActions = map[string]bool{"create": true, "modify": true, "delete": true}

func splitTableRow(line string) []string {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "|") {
		return nil
	}
	cells := mdtable.SplitRow(line)
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

// headingLevel counts the leading `#` of a matched heading. A match
// always starts at one, so the zero case is defensive only.
func headingLevel(heading string) int {
	n := 0
	for n < len(heading) && heading[n] == '#' {
		n++
	}
	if n == 0 {
		return 1
	}
	return n
}

// sectionEndRes[l] matches the next heading at level l or higher. There
// are only six levels, so they are compiled once.
var sectionEndRes = func() [7]*regexp.Regexp {
	var res [7]*regexp.Regexp
	for l := 1; l <= 6; l++ {
		res[l] = regexp.MustCompile(fmt.Sprintf(`(?m)^#{1,%d}\s`, l))
	}
	return res
}()

func sectionEndRe(level int) *regexp.Regexp {
	if level < 1 {
		level = 1
	}
	if level > 6 {
		level = 6
	}
	return sectionEndRes[level]
}

// ParseWireframeFileTable returns the task anchors declared by the File
// Structure table, in document order. Anything else in the wireframe is
// ignored, and a wireframe without the section returns nothing.
func ParseWireframeFileTable(content string) []TaskAnchorKey {
	var out []TaskAnchorKey
	seen := map[TaskAnchorKey]bool{}
	for _, r := range fileStructureRows(content) {
		if r.Path == "" || !anchorActions[r.Action] {
			continue
		}
		key := TaskAnchorKey{Path: r.Path, Action: r.Action}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, key)
	}
	return out
}

// GenerateRow is a File Structure row whose action is `generate`: a file a
// generator writes (an OpenAPI document, generated client types, generated
// headers), or a directory it writes into. It is never a task and never
// edited by hand, so the row carries the command that regenerates it
// instead. Without the action, true-shark marked three such rows
// `generated`, which passed as a non-task row and left nothing to say they
// had to be regenerated and checked (trial problem #66).
type GenerateRow struct {
	Path string
	// Command is the generator, written `generator: ` and the command in a
	// code span in the row's Responsibility cell; "" when the row names none.
	Command string
}

// generatorRe finds a generate row's command. The skill puts it in the
// Responsibility cell rather than a new column, so the table keeps the
// four columns every wireframe already has; the parse reads the whole row,
// so a table with translated headers needs no alias for that column.
var generatorRe = regexp.MustCompile("(?i)\\bgenerator:\\s*`([^`]+)`")

// ParseWireframeGenerateRows returns the File Structure table's `generate`
// rows in document order, one per path.
func ParseWireframeGenerateRows(content string) []GenerateRow {
	var out []GenerateRow
	seen := map[string]bool{}
	for _, r := range fileStructureRows(content) {
		if r.Path == "" || r.Action != "generate" || seen[r.Path] {
			continue
		}
		seen[r.Path] = true
		row := GenerateRow{Path: r.Path}
		// SplitRow turns an escaped `\|` back into the pipe it stands for.
		if m := generatorRe.FindStringSubmatch(strings.Join(r.Cells, " | ")); m != nil {
			row.Command = strings.TrimSpace(m[1])
		}
		out = append(out, row)
	}
	return out
}

// fileStructureRow is one data row of the File Structure table: the path
// and the lower-cased action with their decoration stripped, and every
// cell as written.
type fileStructureRow struct {
	Path, Action string
	Cells        []string
}

// fileStructureRows reads the File Structure table's data rows.
func fileStructureRows(content string) []fileStructureRow {
	loc := fileStructureHeadingRe.FindStringIndex(content)
	if loc == nil {
		return nil
	}
	section := content[loc[1]:]
	// Stop at the next heading of the same level or higher so a later
	// section's table cannot be read as this one's. The bound has to come
	// from the heading that was actually matched: hardcoding `^#{1,2}`
	// scanned straight past the siblings of a `### File Structure`, and
	// because the column indices are already locked to this table's
	// header by then, the next table's data rows were read as anchor
	// rows — a `### Rollback plan` listing the same files became two
	// critical orphan_anchor findings on a wireframe declaring one unit.
	if end := sectionEndRe(headingLevel(content[loc[0]:loc[1]])).FindStringIndex(section); end != nil {
		section = section[:end[0]]
	}

	var header []string
	var fileCol, actionCol int
	var out []fileStructureRow
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
		out = append(out, fileStructureRow{
			Path:   cleanCell(cells[fileCol]),
			Action: strings.ToLower(cleanCell(cells[actionCol])),
			Cells:  cells,
		})
	}
	return out
}
