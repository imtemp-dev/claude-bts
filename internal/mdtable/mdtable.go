// Package mdtable splits GitHub-flavoured markdown table rows.  The spec
// checkers (domain.md, wireframe.md, deviation.md) and the migrations read
// tables through it, so a pipe escaped inside a cell cannot split the row
// in one of them and not in another.
package mdtable

import "strings"

// IsSeparatorRow reports whether line is a table's separator row: nothing
// but pipes, dashes, colons and spaces, with at least one pipe and one
// dash.  Three readers used to call any row containing "---" a separator,
// so an invariant whose statement held "---" vanished from domain.md's
// table (trial problem #112).
func IsSeparatorRow(line string) bool {
	line = strings.TrimSpace(line)
	if !strings.Contains(line, "|") || !strings.Contains(line, "-") {
		return false
	}
	for _, c := range line {
		switch c {
		case '|', '-', ':', ' ', '\t':
		default:
			return false
		}
	}
	return true
}

// SplitRow splits a table row into its cells, without trimming them.  As in
// GFM, a pipe escaped as `\|` belongs to the cell, inside a code span too,
// and comes back as `|`.  The empty segments the row's outer pipes leave
// are dropped.
func SplitRow(line string) []string {
	line = strings.TrimSpace(line)
	var cells []string
	var b strings.Builder
	for i := 0; i < len(line); i++ {
		c := line[i]
		if c == '\\' && i+1 < len(line) && line[i+1] == '|' {
			b.WriteByte('|')
			i++
			continue
		}
		if c == '|' {
			cells = append(cells, b.String())
			b.Reset()
			continue
		}
		b.WriteByte(c)
	}
	cells = append(cells, b.String())
	if len(cells) > 0 && strings.TrimSpace(cells[0]) == "" && strings.HasPrefix(line, "|") {
		cells = cells[1:]
	}
	if len(cells) > 0 && strings.TrimSpace(cells[len(cells)-1]) == "" &&
		strings.HasSuffix(line, "|") && !strings.HasSuffix(line, `\|`) {
		cells = cells[:len(cells)-1]
	}
	return cells
}
