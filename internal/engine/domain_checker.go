package engine

import (
	"bufio"
	"os"
	"regexp"
	"strings"
)

// Invariant represents one row parsed from domain.md § 2 Invariants.
type Invariant struct {
	ID        string
	Statement string
	Owner     string
	LineNo    int
}

// CheckInvariantOwnership parses domain.md § 2 and returns an Issue for
// each invariant statement that has two or more distinct owners. This is
// the structural test the Duolingo word-sort failure mode would have
// caught: "canonical word order" ending up owned by both Card and
// Arrangement meant the truth was duplicated, and patches propagated.
//
// Parse rules:
//   - The section begins at a heading line matching "## 2. Invariants"
//     (case-insensitive, dot tolerated).
//   - Table rows have the form `| ID | Statement | Owner | ... |`.
//   - Header and separator rows (those containing "---") are skipped.
//   - Ownership comparison normalizes whitespace/case on Statement and
//     splits Owner on commas (so "A, B" counts as two owners even on a
//     single row — which itself is a violation).
//
// Returns nil (no issues) for any parsing failure that yields zero
// invariants — callers rely on the companion text-level verify to flag
// a missing section.
func CheckInvariantOwnership(domainPath string) []Issue {
	invariants, err := parseInvariantsTable(domainPath)
	if err != nil || len(invariants) == 0 {
		return nil
	}

	// Group by normalized statement.
	byStatement := map[string]map[string][]Invariant{}
	for _, inv := range invariants {
		stmt := normalizeStatement(inv.Statement)
		if stmt == "" {
			continue
		}
		owners := splitOwners(inv.Owner)
		for _, o := range owners {
			if byStatement[stmt] == nil {
				byStatement[stmt] = map[string][]Invariant{}
			}
			byStatement[stmt][o] = append(byStatement[stmt][o], inv)
		}
	}

	var issues []Issue
	for stmt, ownerMap := range byStatement {
		if len(ownerMap) <= 1 {
			continue
		}
		owners := make([]string, 0, len(ownerMap))
		for o := range ownerMap {
			owners = append(owners, o)
		}
		// Use the first invariant's ID for a stable claim reference.
		var firstID string
		for _, invs := range ownerMap {
			for _, inv := range invs {
				if firstID == "" || inv.ID < firstID {
					firstID = inv.ID
				}
			}
		}
		issues = append(issues, Issue{
			Category: "domain",
			Claim:    "invariant_multiple_owners: " + firstID + " — " + stmt,
			Severity: "critical",
			Detail:   "Invariant has multiple owners (" + strings.Join(owners, ", ") + "). Per jig-domain-model, each invariant must have exactly one owner — re-partition the decomposition so truth lives in one place.",
		})
	}
	return issues
}

var invariantsHeadingRe = regexp.MustCompile(`(?i)^##\s*2\.?\s*Invariants\b`)

// parseInvariantsTable extracts invariant rows. Uses a streaming line scan
// so domain.md size is bounded by the reader buffer, not by memory.
func parseInvariantsTable(path string) ([]Invariant, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	inSection := false
	lineNo := 0
	var invariants []Invariant
	for scanner.Scan() {
		lineNo++
		line := scanner.Text()
		if !inSection {
			if invariantsHeadingRe.MatchString(line) {
				inSection = true
			}
			continue
		}
		// Exit when the next H2 heading begins.
		if strings.HasPrefix(strings.TrimSpace(line), "## ") &&
			!invariantsHeadingRe.MatchString(line) {
			break
		}
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "|") {
			continue
		}
		// Skip the separator row (|---|---|---|).
		if strings.Contains(trimmed, "---") {
			continue
		}
		cells := parseTableRow(trimmed)
		if len(cells) < 3 {
			continue
		}
		// Heuristic: header row has "ID" literal in the first non-empty cell.
		if strings.EqualFold(strings.TrimSpace(cells[0]), "ID") ||
			strings.EqualFold(strings.TrimSpace(cells[0]), "Invariant ID") {
			continue
		}
		inv := Invariant{
			ID:        strings.TrimSpace(cells[0]),
			Statement: strings.TrimSpace(cells[1]),
			Owner:     strings.TrimSpace(cells[2]),
			LineNo:    lineNo,
		}
		// Skip blank / template rows.
		if inv.ID == "" && inv.Statement == "" && inv.Owner == "" {
			continue
		}
		invariants = append(invariants, inv)
	}
	return invariants, scanner.Err()
}

// parseTableRow splits "| a | b | c |" into ["a", "b", "c"]. Leading
// and trailing pipes produce empty segments that are dropped, but
// INTERNAL empty cells (e.g. "| a |  | c |") must be preserved so
// schema checkers can report "missing field" correctly.
func parseTableRow(line string) []string {
	line = strings.TrimSpace(line)
	parts := strings.Split(line, "|")
	// strings.Split on "|a|b|" yields ["", "a", "b", ""]. Drop the
	// leading and trailing empty segments (from the outer pipes)
	// without touching internal ones.
	if len(parts) > 0 && strings.TrimSpace(parts[0]) == "" && strings.HasPrefix(line, "|") {
		parts = parts[1:]
	}
	if len(parts) > 0 && strings.TrimSpace(parts[len(parts)-1]) == "" && strings.HasSuffix(line, "|") {
		parts = parts[:len(parts)-1]
	}
	return parts
}

// normalizeStatement collapses whitespace and lowercases so minor
// formatting differences do not hide duplication.
func normalizeStatement(s string) string {
	s = strings.ToLower(s)
	s = strings.Join(strings.Fields(s), " ")
	return s
}

// splitOwners accepts "Arrangement", "Arrangement, Card", "Arrangement / Card".
// Comma and slash both indicate a shared-ownership row — which is itself
// an invariant-owner violation, so the splitter exposes all candidates.
func splitOwners(owner string) []string {
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return nil
	}
	// Normalize separators to comma before split.
	owner = strings.ReplaceAll(owner, " / ", ",")
	owner = strings.ReplaceAll(owner, "/", ",")
	owner = strings.ReplaceAll(owner, " and ", ",")
	parts := strings.Split(owner, ",")
	out := make([]string, 0, len(parts))
	seen := map[string]bool{}
	for _, p := range parts {
		o := strings.TrimSpace(p)
		if o == "" || seen[strings.ToLower(o)] {
			continue
		}
		seen[strings.ToLower(o)] = true
		out = append(out, o)
	}
	return out
}
