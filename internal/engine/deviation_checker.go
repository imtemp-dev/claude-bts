package engine

import (
	"bufio"
	"os"
	"regexp"
	"strings"
)

// DeviationRow is a single row parsed from deviation.md's tables.
// Section identifies which table the row came from so the caller can
// render targeted error messages.
type DeviationRow struct {
	ID       string
	Section  string // "not_implemented" | "spec_additions" | "deviations"
	Item     string
	Drivers  []string
	Severity string
	LineNo   int
}

// Driver vocabulary — exact tokens the validator accepts. Anything
// outside this set is reported as driver_invalid. Keep in sync with
// the list in jig-sync/SKILL.md.
var validDriverRe = regexp.MustCompile(
	`^(?:code-diff|sync-check|simulate:[\w\.\-]+|review:[\w\.\-]+|test:[^,|]+|midrun:[\w\.\-]+)$`,
)

// validSeverity is the set of severities acceptable in the Severity
// column. info is included for completeness even though it is unusual
// in a deviation context.
var validSeverity = map[string]bool{
	"critical": true, "major": true, "minor": true, "info": true,
}

// sectionHeaderRe matches H2 headings. The section name in the claim
// comes from the preceding H2 — we recognize three: Not Implemented,
// Spec Additions, Deviations (case-insensitive).
var sectionHeaderRe = regexp.MustCompile(`(?mi)^##\s+(.+?)\s*$`)

// ParseDeviationMd reads deviation.md and returns all parsed rows in
// document order. Rows are drawn from the three known sections; the
// parser ignores other tables (e.g. Summary) so adding documentation
// rows alongside the data tables does not produce spurious rows.
func ParseDeviationMd(path string) ([]DeviationRow, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var rows []DeviationRow
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	section := ""
	inTable := false
	seenHeader := false
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := scanner.Text()

		// Section detection — update context even inside tables.
		if m := sectionHeaderRe.FindStringSubmatch(line); len(m) >= 2 {
			section = normalizeSection(m[1])
			inTable = false
			seenHeader = false
			continue
		}

		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "|") {
			// Blank line or non-table content — reset table state so
			// the next "|" line starts fresh (headers must follow).
			inTable = false
			seenHeader = false
			continue
		}
		if strings.Contains(trimmed, "---") {
			// Separator row. Marks that we are inside a table after a
			// header was seen.
			if seenHeader {
				inTable = true
			}
			continue
		}
		if !inTable {
			// This is the header line for a new table.
			seenHeader = true
			continue
		}

		// Section filter — only capture rows from the three deviation tables.
		if section != "not_implemented" && section != "spec_additions" && section != "deviations" {
			continue
		}

		cells := parseTableRow(trimmed)
		if len(cells) == 0 {
			continue
		}
		// Trim whitespace on every cell.
		for i := range cells {
			cells[i] = strings.TrimSpace(cells[i])
		}
		// Skip placeholder rows used to show empty sections.
		if isPlaceholderRow(cells) {
			continue
		}

		row := rowFromCells(section, cells, lineNo)
		// Rows missing an ID are still recorded (as blank) so the
		// validator can raise id_missing with a precise line number.
		rows = append(rows, row)
	}
	return rows, scanner.Err()
}

// rowFromCells maps a parsed table row to a DeviationRow. Column
// positions depend on section: each table has a known schema (see
// jig-sync/SKILL.md Step 5 canonical layout).
//
// Layouts:
//   not_implemented: | ID | Item | File   | Driver | Severity | Reason |
//   spec_additions:  | ID | Item | File   | Driver | Severity | Description |
//   deviations:      | ID | Item | Spec Says | Code Has | Driver | Severity | Resolution |
func rowFromCells(section string, cells []string, lineNo int) DeviationRow {
	r := DeviationRow{Section: section, LineNo: lineNo}
	switch section {
	case "not_implemented", "spec_additions":
		if len(cells) >= 5 {
			r.ID = cells[0]
			r.Item = cells[1]
			r.Drivers = splitDriverList(cells[3])
			r.Severity = cells[4]
		}
	case "deviations":
		if len(cells) >= 6 {
			r.ID = cells[0]
			r.Item = cells[1]
			r.Drivers = splitDriverList(cells[4])
			r.Severity = cells[5]
		}
	}
	return r
}

func splitDriverList(s string) []string {
	raw := strings.Split(s, ",")
	out := make([]string, 0, len(raw))
	for _, r := range raw {
		r = strings.TrimSpace(r)
		if r != "" {
			out = append(out, r)
		}
	}
	return out
}

func isPlaceholderRow(cells []string) bool {
	for _, c := range cells {
		if c != "" && c != "—" && c != "-" {
			return false
		}
	}
	return true
}

func normalizeSection(name string) string {
	n := strings.ToLower(strings.TrimSpace(name))
	switch n {
	case "not implemented":
		return "not_implemented"
	case "spec additions":
		return "spec_additions"
	case "deviations":
		return "deviations"
	}
	return ""
}

// CheckDeviationSchema validates the structure Phase 16 requires:
//
//   - Every row has a non-empty ID (critical when missing).
//   - IDs are unique across all three sections (major on duplicate).
//   - Every row has at least one Driver from validDriverRe (critical
//     on miss, major on invalid token).
//   - Severity is one of {critical, major, minor, info} (major on miss
//     or invalid).
//
// When deviation.md is absent the function returns nil — the caller
// decides whether absence is itself an error (stop hook does).
func CheckDeviationSchema(path string) []Issue {
	rows, err := ParseDeviationMd(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return []Issue{{Category: "deviation", Claim: "parse_error", Severity: "major", Detail: err.Error()}}
	}

	var issues []Issue
	seenID := map[string]int{}
	for _, r := range rows {
		if r.ID == "" {
			issues = append(issues, Issue{
				Category: "deviation",
				Claim:    "id_missing",
				Severity: "critical",
				Detail:   "deviation.md (" + r.Section + ", line " + itoa(r.LineNo) + "): row has no ID. Every row must carry a unique D-NNN identifier.",
			})
			continue
		}
		seenID[r.ID]++
		if seenID[r.ID] == 2 {
			issues = append(issues, Issue{
				Category: "deviation",
				Claim:    "id_duplicate: " + r.ID,
				Severity: "major",
				Detail:   "deviation.md: ID " + r.ID + " appears more than once across sections.",
			})
		}

		if len(r.Drivers) == 0 {
			issues = append(issues, Issue{
				Category: "deviation",
				Claim:    "driver_missing: " + r.ID,
				Severity: "critical",
				Detail:   "deviation.md: " + r.ID + " (" + r.Section + ") has no Driver. Each row must cite code-diff | sync-check | simulate:{id} | review:{id} | test:{name} | midrun:{window}.",
			})
		} else {
			for _, d := range r.Drivers {
				if !validDriverRe.MatchString(d) {
					issues = append(issues, Issue{
						Category: "deviation",
						Claim:    "driver_invalid: " + r.ID + ":" + d,
						Severity: "major",
						Detail:   "deviation.md: " + r.ID + " driver '" + d + "' is outside the vocabulary (code-diff|sync-check|simulate:...|review:...|test:...|midrun:...).",
					})
				}
			}
		}

		if r.Severity == "" {
			issues = append(issues, Issue{
				Category: "deviation",
				Claim:    "severity_missing: " + r.ID,
				Severity: "major",
				Detail:   "deviation.md: " + r.ID + " has no Severity. Use critical/major/minor/info.",
			})
		} else if !validSeverity[strings.ToLower(r.Severity)] {
			issues = append(issues, Issue{
				Category: "deviation",
				Claim:    "severity_invalid: " + r.ID,
				Severity: "major",
				Detail:   "deviation.md: " + r.ID + " severity '" + r.Severity + "' not in {critical, major, minor, info}.",
			})
		}
	}
	return issues
}

// itoa avoids importing strconv for a single integer-to-string in the
// small number of error messages this checker emits.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	buf := make([]byte, 0, 10)
	for n > 0 {
		buf = append([]byte{byte('0' + n%10)}, buf...)
		n /= 10
	}
	if neg {
		buf = append([]byte{'-'}, buf...)
	}
	return string(buf)
}
