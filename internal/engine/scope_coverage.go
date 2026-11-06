package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/imtemp-dev/claude-bts/internal/mdtable"
	"github.com/imtemp-dev/claude-bts/internal/state"
)

// A build recipe's final.md maps every In Scope item of scope.md to what
// holds it, or to the resolved decision that changed it.  On swift-eagle a
// confirmed item (unrelated turns "before and inside the topic" kept out
// of the packet) was narrowed by the invariants and the eval rows without
// any record, and six review rounds and DONE passed it (trial problem
// #103).  The table makes each item's fate a line someone wrote, and the
// check makes a missing line visible.

var (
	inScopeHeadingRe       = regexp.MustCompile(`(?i)^##\s+in scope\b`)
	topLevelScopeItemRe    = regexp.MustCompile(`^(?:\d+\.|[-*])\s+(.*\S)`)
	scopeCoverageHeadingRe = regexp.MustCompile(`(?i)^#{2,3}\s*(?:\d+\.\s*)?scope coverage\b`)
	// A decision key is kebab-case (code-edit-go, scope-14-before-only), so
	// "the architect decision about X" cites nothing.
	scopeDecisionRefRe = regexp.MustCompile("(?i)\\bdecision\\s+`?([a-z0-9]+(?:-[a-z0-9]+)+)`?")
	firstNumberRe      = regexp.MustCompile(`\d+`)
)

// ParseScopeItems returns the top-level list items, numbered or bulleted,
// under scope.md's `## In Scope`, across its `###` subsections, in order.
// Item n of the coverage table is the n-th of these.
func ParseScopeItems(content string) []string {
	var items []string
	in, fence := false, false
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			fence = !fence
			continue
		}
		if fence {
			continue
		}
		if strings.HasPrefix(line, "## ") {
			in = inScopeHeadingRe.MatchString(line)
			continue
		}
		if !in {
			continue
		}
		if m := topLevelScopeItemRe.FindStringSubmatch(line); m != nil {
			items = append(items, m[1])
		}
	}
	return items
}

// parseScopeCoverage reads final.md's Scope coverage table: item number to
// the rest of its row.  found reports whether the section exists.
func parseScopeCoverage(content string) (rows map[int]string, found bool) {
	rows = map[int]string{}
	level := 0
	for _, line := range strings.Split(content, "\n") {
		if !found {
			if scopeCoverageHeadingRe.MatchString(line) {
				found, level = true, lineHeadingLevel(line)
			}
			continue
		}
		if h := lineHeadingLevel(line); h > 0 && h <= level {
			break
		}
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "|") || mdtable.IsSeparatorRow(trimmed) {
			continue
		}
		cells := mdtable.SplitRow(trimmed)
		if len(cells) < 2 {
			continue
		}
		num := firstNumberRe.FindString(cells[0])
		if num == "" {
			continue // the header row
		}
		n, err := strconv.Atoi(num)
		if err != nil {
			continue
		}
		rest := make([]string, 0, len(cells)-1)
		for _, c := range cells[1:] {
			rest = append(rest, strings.TrimSpace(c))
		}
		rows[n] = strings.Join(rest, " | ")
	}
	return rows, found
}

// CheckScopeCoverage reports, for a build recipe's final.md, the In Scope
// items with no row in its Scope coverage table, and the decisions a row
// cites that are not resolved.  Other documents and recipe types return
// nothing.
func CheckScopeCoverage(finalPath string) []Issue {
	dir := filepath.Dir(finalPath)
	if !strings.EqualFold(filepath.Base(finalPath), "final.md") || recipeTypeOf(dir) != "build" {
		return nil
	}
	scopeData, err := os.ReadFile(filepath.Join(dir, "scope.md"))
	if err != nil {
		return nil
	}
	items := ParseScopeItems(string(scopeData))
	if len(items) == 0 {
		return nil
	}
	finalData, err := os.ReadFile(finalPath)
	if err != nil {
		return nil
	}
	rows, found := parseScopeCoverage(string(finalData))
	if !found {
		return []Issue{{
			Category: "scope_coverage",
			Claim:    "scope_coverage_missing",
			Severity: SeverityMajor,
			Detail:   fmt.Sprintf("final.md has no Scope coverage section. %s", scopeCoverageFormat(len(items))),
		}}
	}
	var issues []Issue
	for i, text := range items {
		if _, ok := rows[i+1]; ok {
			continue
		}
		issues = append(issues, Issue{
			Category: "scope_coverage",
			Claim:    fmt.Sprintf("scope_item_uncovered: %d", i+1),
			Severity: SeverityMajor,
			Detail: fmt.Sprintf("scope.md In Scope item %d (%q) has no row in final.md's Scope coverage: name what holds it, "+
				"or the resolved decision that changed it.", i+1, state.TruncateRunes(text, 80)),
		})
	}
	resolved := resolvedDecisionKeys(dir)
	nums := make([]int, 0, len(rows))
	for n := range rows {
		nums = append(nums, n)
	}
	sort.Ints(nums)
	for _, n := range nums {
		for _, m := range scopeDecisionRefRe.FindAllStringSubmatch(rows[n], -1) {
			if resolved[m[1]] {
				continue
			}
			issues = append(issues, Issue{
				Category: "scope_coverage",
				Claim:    "scope_decision_unresolved: " + m[1],
				Severity: SeverityMajor,
				Detail: fmt.Sprintf("Scope coverage row %d cites decision %s, which is not resolved. A narrowed scope item stands "+
					"on the user's answer: hold the decision, ask, and resolve it with their words.", n, m[1]),
			})
		}
	}
	return issues
}

// scopeCoverageFormat says what the section holds, for a block message
// read by an agent whose skill text may predate it.
func scopeCoverageFormat(items int) string {
	return fmt.Sprintf("Add `## 6. Scope coverage` with a table `| Scope item | Held by |` and one row per In Scope item of "+
		"scope.md (%d, numbered in order, bullets too), naming the invariants, falsifiers or eval that hold it, or "+
		"`decision <key>` for a resolved decision that changed it.", items)
}

// resolvedDecisionKeys returns the recipe's resolved decision keys.  dir is
// the recipe directory, `<root>/.bts/specs/recipes/<id>`.
func resolvedDecisionKeys(dir string) map[string]bool {
	out := map[string]bool{}
	root := filepath.Clean(filepath.Join(dir, "..", "..", "..", ".."))
	decisions, err := state.LoadDecisions(root, filepath.Base(dir))
	if err != nil {
		return out
	}
	for _, d := range decisions {
		if d.Status == state.DecisionResolved {
			out[d.Key] = true
		}
	}
	return out
}
