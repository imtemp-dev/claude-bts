package engine

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/imtemp-dev/claude-bts/internal/mdtable"
	"github.com/imtemp-dev/claude-bts/internal/state"
)

// final.md § 2 maps each invariant to the test that falsifies it, and
// build-evidence.json already holds the same mapping, recorded one
// invariant at a time by `bts falsifier run`.  On swift-eagle the agent
// wrote the 30 rows of § 2 by hand, a script to fill them was denied, and
// the table could drift from the records (trial problem #97).  The table is
// now rendered from the records.

var (
	invIDRe       = regexp.MustCompile(`\bINV-\d+\b`)
	pytestNodeRe  = regexp.MustCompile(`[\w./-]+::[\w\[\]./:-]+`)
	pytestKExprRe = regexp.MustCompile(`(?:^|\s)-k\s+(?:'([^']+)'|"([^"]+)"|(\S+))`)
	filterExprRe  = regexp.MustCompile(`--filter\s+(?:'([^']+)'|"([^"]+)"|(\S+))`)
)

// RenderFalsifierTable renders final.md § 2's table from the recipe's
// falsifier records: one row per domain.md invariant in id order, tagged
// [carried] or [illegal-cell: <label>] as domain.md declares, with the test
// the command selects and the command itself.  An invariant with no record
// gets a row that says so, so the gap is visible.
func RenderFalsifierTable(ev *state.BuildEvidence, domainPath string) (string, error) {
	invs, err := domainInvariantRows(domainPath)
	if err != nil {
		return "", err
	}
	cellsByInv := illegalCellsByInvariant(domainPath)
	sort.SliceStable(invs, func(i, j int) bool { return invNumber(invs[i].ID) < invNumber(invs[j].ID) })

	var b strings.Builder
	b.WriteString("| Invariant | Test | Command |\n| --- | --- | --- |\n")
	for _, inv := range invs {
		label := inv.ID
		if inv.Carried {
			label += " [carried]"
		}
		for _, cell := range cellsByInv[inv.ID] {
			label += " [illegal-cell: " + cell + "]"
		}
		var f *state.Falsifier
		if ev != nil {
			f = ev.Falsifiers[inv.ID]
		}
		if f == nil || strings.TrimSpace(f.Command) == "" {
			fmt.Fprintf(&b, "| %s | (none recorded) | record it with `bts falsifier run` |\n", label)
			continue
		}
		test := testSelectedBy(f.Command)
		if test != "" {
			test = "`" + test + "`"
		}
		fmt.Fprintf(&b, "| %s | %s | `%s` |\n", label, escapePipes(test), escapePipes(f.Command))
	}
	return b.String(), nil
}

// testSelectedBy names the test a falsifier command selects: a pytest node
// id, a -k expression or a dotnet --filter, or "" when it names none.
func testSelectedBy(command string) string {
	if m := pytestNodeRe.FindString(command); m != "" {
		return strings.Trim(m, `'"`)
	}
	for _, re := range []*regexp.Regexp{pytestKExprRe, filterExprRe} {
		if m := re.FindStringSubmatch(command); m != nil {
			for _, g := range m[1:] {
				if g != "" {
					return g
				}
			}
		}
	}
	return ""
}

// illegalCellsByInvariant maps an invariant id to the labels of the
// domain.md § 4 ILLEGAL cells whose row names it ("prevented by ...
// (INV-004)").
func illegalCellsByInvariant(domainPath string) map[string][]string {
	out := map[string][]string{}
	data, err := os.ReadFile(domainPath)
	if err != nil {
		return out
	}
	inSection := false
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if !inSection {
			inSection = domainIllegalSectionRe.MatchString(trimmed)
			continue
		}
		if strings.HasPrefix(trimmed, "## ") {
			break
		}
		if !strings.HasPrefix(trimmed, "|") {
			continue
		}
		var labels []string
		for _, cell := range mdtable.SplitRow(trimmed) {
			if l := declaredCellLabel(cell); l != "" {
				labels = append(labels, l)
			}
		}
		if len(labels) == 0 {
			continue
		}
		for _, id := range invIDRe.FindAllString(trimmed, -1) {
			out[id] = append(out[id], labels...)
		}
	}
	return out
}

func invNumber(id string) int {
	n, _ := strconv.Atoi(strings.TrimPrefix(strings.ToUpper(id), "INV-"))
	return n
}

func escapePipes(s string) string { return strings.ReplaceAll(s, "|", `\|`) }

// ReplaceSectionTable puts table in place of the first table under the
// heading that starts with headingPrefix (for example "## 2."), or right
// under that heading when it has none.  It reports whether the heading
// was found.
func ReplaceSectionTable(content, headingPrefix, table string) (string, bool) {
	lines := strings.Split(content, "\n")
	start := -1
	for i, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), headingPrefix) {
			start = i
			break
		}
	}
	if start < 0 {
		return content, false
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "## ") {
			end = i
			break
		}
	}
	tableLines := strings.Split(strings.TrimRight(table, "\n"), "\n")
	first, last := -1, -1
	for i := start + 1; i < end; i++ {
		isRow := strings.HasPrefix(strings.TrimSpace(lines[i]), "|")
		if isRow && first < 0 {
			first = i
		}
		if first >= 0 {
			if !isRow {
				break
			}
			last = i
		}
	}
	var out []string
	if first < 0 {
		out = append(out, lines[:start+1]...)
		out = append(out, "")
		out = append(out, tableLines...)
		out = append(out, lines[start+1:]...)
	} else {
		out = append(out, lines[:first]...)
		out = append(out, tableLines...)
		out = append(out, lines[last+1:]...)
	}
	return strings.Join(out, "\n"), true
}
