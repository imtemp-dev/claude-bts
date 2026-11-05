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

// CheckUnlabelledIllegalCells reports each domain.md § 4 table row that
// marks a cell ILLEGAL without naming it.  Such a row declares no cell, so
// no falsifier could be asked for or tied to it: relay r-001 had three,
// tagged by row id or description in final.md and read by nothing (trial
// problem #74, AGEN-51).  It needs only domain.md, so the author hears of
// it while writing § 4, not once final.md exists.
func CheckUnlabelledIllegalCells(domainPath string) []Issue {
	_, unlabelled, err := readIllegalSection(domainPath)
	if err != nil {
		return nil
	}
	var issues []Issue
	for _, row := range unlabelled {
		issues = append(issues, Issue{
			Category: "domain",
			Claim:    "unlabelled_illegal_cell: " + row,
			Severity: "critical",
			Detail:   "domain.md §4 marks this row ILLEGAL without a label, so no falsifier can be tied to it. Name the cell in the row as `ILLEGAL: <label>`, then tag its test's row in final.md § 2 `[illegal-cell: <label>]`.",
		})
	}
	return issues
}

// parseIllegalCells reads the labels of the illegal cells domain.md § 4
// declares.
func parseIllegalCells(domainPath string) ([]string, error) {
	cells, _, err := readIllegalSection(domainPath)
	return cells, err
}

// readIllegalSection reads domain.md § 4: the labels of the cells it
// declares, and the rows it marks ILLEGAL without one.
//
// A cell is declared only in a table row of § 4, by the uppercase marker
// and its label: `ILLEGAL: <label>`, bold or not, or `**ILLEGAL** — <label>`.
// Prose in the section, a lowercase "illegal" and a bare ILLEGAL status
// cell declare nothing.  The label ends where its bold closes, at a spaced
// dash, or at the cell's end, and a trailing parenthetical is a note, not
// part of the name (cellLabel).  Reading the marker case-blind anywhere in
// the section, to the end of the cell, made prose sentences into cells and
// kept notes and preventions in labels (trial problem #68, AGEN-49).
//
// A row with the marker and no label in any cell is unlabelled; a row
// whose label sits in one cell and whose status column says ILLEGAL is not.
func readIllegalSection(domainPath string) (cells, unlabelled []string, err error) {
	data, err := os.ReadFile(domainPath)
	if err != nil {
		return nil, nil, err
	}
	inSection := false
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
		labelled, marked := false, false
		for _, cell := range mdtable.SplitRow(trimmed) {
			label := declaredCellLabel(cell)
			if label == "" {
				marked = marked || bareIllegalRe.MatchString(cell)
				continue
			}
			labelled = true
			if !seen[cellKey(label)] {
				seen[cellKey(label)] = true
				cells = append(cells, label)
			}
		}
		if marked && !labelled {
			unlabelled = append(unlabelled, rowRef(trimmed))
		}
	}
	return cells, unlabelled, nil
}

// bareIllegalRe finds the uppercase marker without a label.
var bareIllegalRe = regexp.MustCompile(`\bILLEGAL\b`)

// rowRef is a table row as an error names it: whitespace collapsed, and
// cut at 100 characters.
func rowRef(row string) string {
	r := []rune(strings.Join(strings.Fields(row), " "))
	if len(r) > 100 {
		return string(r[:100]) + "…"
	}
	return string(r)
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
