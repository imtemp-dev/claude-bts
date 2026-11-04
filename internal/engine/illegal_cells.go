package engine

import (
	"os"
	"regexp"
	"strings"
)

// CheckIllegalCellFalsifiers is the build recipe's form of
// CheckIllegalCellCoverage. A build recipe proves an illegal cell with a
// test that the transition is refused, not with a simulated scenario, so
// coverage is a `[illegal-cell: <label>]` tag on that test's row in
// final.md § 2. Returns one critical issue per untagged cell.
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
		covered[strings.ToLower(strings.TrimSpace(m[1]))] = true
	}
	var issues []Issue
	for _, cell := range cells {
		if !covered[strings.ToLower(cell)] {
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

func parseIllegalCells(domainPath string) ([]string, error) {
	data, err := os.ReadFile(domainPath)
	if err != nil {
		return nil, err
	}
	content := string(data)
	lines := strings.Split(content, "\n")

	inSection := false
	var cells []string
	seen := map[string]bool{}
	for _, line := range lines {
		if !inSection {
			if domainIllegalSectionRe.MatchString(strings.TrimSpace(line)) {
				inSection = true
			}
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(line), "## ") &&
			!domainIllegalSectionRe.MatchString(strings.TrimSpace(line)) {
			break
		}
		// Match "ILLEGAL: label" segments anywhere on the line.
		matches := illegalCellLabelRe.FindAllStringSubmatch(line, -1)
		for _, m := range matches {
			if len(m) < 2 {
				continue
			}
			label := strings.TrimSpace(m[1])
			// Strip trailing punctuation and table junk.
			label = strings.TrimRight(label, " .—-*_`")
			label = strings.TrimSpace(label)
			if label == "" {
				continue
			}
			key := strings.ToLower(label)
			if seen[key] {
				continue
			}
			seen[key] = true
			cells = append(cells, label)
		}
	}
	return cells, nil
}

// domainIllegalSectionRe finds domain.md § 4, where ILLEGAL cells live.
var domainIllegalSectionRe = regexp.MustCompile(`(?i)^##\s*4\.?\s*Combinatorial\s+State\s+Space\b`)

// illegalCellLabelRe takes the label after ILLEGAL in a § 4 row.
var illegalCellLabelRe = regexp.MustCompile(`(?i)ILLEGAL[:\s]*([^\n|]+)`)
