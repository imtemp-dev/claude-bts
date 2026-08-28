package engine

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

// Falsifier coverage — every load-bearing claim names what would prove
// it false.
//
// A claim in a document about code that does not exist yet has no truth
// value until something executes. Three verification agents reading the
// same paragraph and agreeing is not a measurement of the claim; it is
// three readings of the same prose. The completion gate calls that
// outcome "bulletproof", and on one measured recipe it would have
// certified a spec in which nobody had checked whether Postgres bracket
// ranges expand over the collating sequence, whether the synthesized
// Encodable omits nil optionals, or whether the DTO regex and the DB
// CHECK accept the same set. Nine of that recipe's seventeen CRITICAL
// findings were questions a single execution answers.
//
// So the blueprint owes each invariant the name of the thing that would
// go red. Names only: what the assertion should contain is decided while
// writing the test, where a compiler and a run settle it in seconds
// rather than over four verify rounds.
//
// This is the deterministic half of `bts-level-criteria.md § Level 3`'s
// falsifiers_assigned criterion. The level score reports it; this raises
// it as a major so `bts verify` exits non-zero and the stop hook can
// block on it.

// UncoveredInvariant is one invariant declared without a falsifier.
type UncoveredInvariant struct {
	ID     string
	LineNo int // 1-based line where the invariant was first declared
}

// FalsifierCoverage returns the invariants a spec declares that never
// appear on a line naming a test, spec, probe or observation.
//
// A document that declares no invariants returns nothing: invariants are
// domain.md's to introduce, and a spec that legitimately has none must
// not be told to invent one. Coverage is only a question once there is
// something to cover.
func FalsifierCoverage(content string) []UncoveredInvariant {
	// Keyed on the digits without leading zeros so INV-007 and INV-7 are
	// one invariant; the ID is reported as the document writes it.
	declaredAt := map[string]int{}
	writtenAs := map[string]string{}
	covered := map[string]bool{}
	for i, line := range strings.Split(content, "\n") {
		matches := invariantIDRe.FindAllStringSubmatch(line, -1)
		if len(matches) == 0 {
			continue
		}
		hasFalsifier := lineNamesFalsifier(line)
		for _, m := range matches {
			key := m[2]
			if _, seen := declaredAt[key]; !seen {
				declaredAt[key] = i + 1
				writtenAs[key] = m[1]
			}
			if hasFalsifier {
				covered[key] = true
			}
		}
	}

	var out []UncoveredInvariant
	for key, line := range declaredAt {
		if covered[key] {
			continue
		}
		out = append(out, UncoveredInvariant{ID: writtenAs[key], LineNo: line})
	}
	sort.Slice(out, func(a, b int) bool { return out[a].LineNo < out[b].LineNo })
	return out
}

// CheckFalsifierCoverage reads a spec document and raises one major per
// invariant that names no falsifier.
//
// One issue per invariant rather than one for the document: the fix is
// per-invariant, and a single aggregate finding gets "addressed" by
// covering the easy ones.
func CheckFalsifierCoverage(specPath string) []Issue {
	data, err := os.ReadFile(specPath)
	if err != nil {
		return nil
	}
	uncovered := FalsifierCoverage(string(data))
	if len(uncovered) == 0 {
		return nil
	}
	issues := make([]Issue, 0, len(uncovered))
	for _, u := range uncovered {
		issues = append(issues, Issue{
			Category: "falsifier_coverage",
			Severity: SeverityMajor,
			Claim: fmt.Sprintf("%s (line %d) names no falsifier",
				u.ID, u.LineNo),
			Detail: "Add a row naming the test, spec, probe or observation that " +
				"would go red if this invariant were violated — the name only, " +
				"not what it asserts. An invariant nothing can falsify is not " +
				"verified by agreement about its prose; it is unopened, and it " +
				"stays unopened through completion. See " +
				"bts-level-criteria.md § Level 3 falsifiers_assigned.",
		})
	}
	return issues
}
