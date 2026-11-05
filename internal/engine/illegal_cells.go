package engine

import (
	"os"
	"regexp"
	"strings"

	"github.com/imtemp-dev/claude-bts/internal/mdtable"
)

// CheckIllegalCellFalsifiers checks that every illegal cell domain.md § 4
// declares has a test that the transition is refused: a
// `[illegal-cell: <label>]` tag on that test's row in final.md § 2.
// Returns one critical issue per untagged cell.
func CheckIllegalCellFalsifiers(domainPath, finalPath string) []Issue {
	cells, err := parseIllegalCells(domainPath)
	if err != nil || len(cells) == 0 {
		return nil
	}
	data, err := os.ReadFile(finalPath)
	if err != nil {
		return nil
	}
	covered := map[string]bool{}
	for _, m := range illegalCellTagRe.FindAllStringSubmatch(string(data), -1) {
		covered[cellKey(m[1])] = true
	}
	var issues []Issue
	for _, cell := range cells {
		if !covered[cellKey(cell)] {
			issues = append(issues, Issue{
				Category: "falsify",
				Claim:    "untested_illegal_cell: " + cell,
				Severity: "critical",
				Detail:   "domain.md §4 declares cell '" + cell + "' ILLEGAL but no falsifier row in final.md § 2 carries `[illegal-cell: " + cell + "]`. Write a test that the transition is refused, tag its row, and (in a build recipe) record it with `bts falsifier run`.",
			})
		}
	}
	return issues
}

var illegalCellTagRe = regexp.MustCompile(`(?i)\[illegal-cell:\s*([^\]]+)\]`)

// parseIllegalCells reads the illegal cells domain.md § 4 declares.
//
// A cell is declared only in a table row of § 4, by the uppercase marker
// and its label: `ILLEGAL: <label>`, bold or not, or `**ILLEGAL** — <label>`.
// Prose in the section, a lowercase "illegal" and a bare ILLEGAL status
// cell declare nothing.  The label ends where its bold closes, at a spaced
// dash, or at the cell's end, and a trailing parenthetical is a note, not
// part of the name (cellLabel).  Reading the marker case-blind anywhere in
// the section, to the end of the cell, made prose sentences into cells and
// kept notes and preventions in labels (trial problem #68, AGEN-49).
func parseIllegalCells(domainPath string) ([]string, error) {
	data, err := os.ReadFile(domainPath)
	if err != nil {
		return nil, err
	}
	inSection := false
	var cells []string
	seen := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if !inSection {
			inSection = domainIllegalSectionRe.MatchString(trimmed)
			continue
		}
		if strings.HasPrefix(trimmed, "## ") && !domainIllegalSectionRe.MatchString(trimmed) {
			break
		}
		if !strings.HasPrefix(trimmed, "|") {
			continue
		}
		for _, cell := range mdtable.SplitRow(trimmed) {
			label := declaredCellLabel(cell)
			if label == "" || seen[cellKey(label)] {
				continue
			}
			seen[cellKey(label)] = true
			cells = append(cells, label)
		}
	}
	return cells, nil
}

// domainIllegalSectionRe finds domain.md § 4, where ILLEGAL cells live.
var domainIllegalSectionRe = regexp.MustCompile(`(?i)^##\s*4\.?\s*Combinatorial\s+State\s+Space\b`)

// illegalMarkerRe finds the marker that opens a cell's label: the
// uppercase word, optionally bold, then a colon or a dash.  Group 1 is a
// bold opened before the word, group 2 a bold closed right after it.
var illegalMarkerRe = regexp.MustCompile(`(\*\*)?\bILLEGAL\b(\*\*)?[ \t]*(?::|[—–]|--?[ \t])[ \t]*`)

// declaredCellLabel returns the label a table cell declares, or "".
func declaredCellLabel(cell string) string {
	loc := illegalMarkerRe.FindStringSubmatchIndex(cell)
	if loc == nil {
		return ""
	}
	rest := cell[loc[1]:]
	if loc[2] >= 0 && loc[4] < 0 { // `**ILLEGAL: label**`: the bold ends the label
		if i := strings.Index(rest, "**"); i >= 0 {
			rest = rest[:i]
		}
	}
	for _, dash := range []string{" — ", " – "} {
		if i := strings.Index(rest, dash); i >= 0 {
			rest = rest[:i]
		}
	}
	return cellLabel(rest)
}

// cellLabel normalises a cell label from either side of the comparison:
// whitespace collapsed, emphasis and trailing punctuation trimmed, and a
// trailing parenthetical note dropped, so `x (or the reverse)` names x.
func cellLabel(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	for {
		s = strings.Trim(s, " *_")
		s = strings.TrimRight(s, " .;,:—–-")
		if i := trailingParen(s); i > 0 {
			s = s[:i]
			continue
		}
		return s
	}
}

// cellKey is the comparison form of a label.
func cellKey(s string) string { return strings.ToLower(cellLabel(s)) }

// trailingParen returns where the parenthetical closing s opens, or -1.
func trailingParen(s string) int {
	if !strings.HasSuffix(s, ")") {
		return -1
	}
	depth := 0
	for i := len(s) - 1; i >= 0; i-- {
		switch s[i] {
		case ')':
			depth++
		case '(':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}
