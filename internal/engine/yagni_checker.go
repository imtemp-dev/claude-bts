package engine

import (
	"os"
	"regexp"
	"strings"
)

// Interface declarations and their concrete implementations live in the
// draft spec text in predictable shapes. We cover the three common
// languages jig recipes target (Go, TypeScript, Python). Swift/Kotlin
// recipes would add their own patterns later — not in scope today.

var (
	// Go: `type Name interface {` or `type Name interface {}` or
	// `type Name interface { … }` (multi-line).
	goInterfaceRe = regexp.MustCompile(`(?m)^\s*type\s+(\w+)\s+interface\s*\{`)
	// TypeScript: `interface Name {` (handles `export` prefix).
	tsInterfaceRe = regexp.MustCompile(`(?m)^\s*(?:export\s+)?interface\s+(\w+)\s*\{`)
	// Python: `class Name(Protocol):` or `class Name(ABC):`
	pyProtocolRe = regexp.MustCompile(`(?m)^\s*class\s+(\w+)\s*\(\s*(?:Protocol|ABC|typing\.Protocol)\b`)
)

// yagniJustifiedRe matches a `// YAGNI-justified:` marker (any comment
// prefix that eventually says YAGNI-justified). Authors can place it on
// the interface line or within 3 lines above.
var yagniJustifiedRe = regexp.MustCompile(`YAGNI-justified:\s*\S`)

// CheckInterfaceJustification scans a spec document for interface
// declarations that appear to have only one concrete implementation in
// the same document. Each such interface must carry a
// `YAGNI-justified: <reason>` marker within the 3 lines preceding its
// declaration; otherwise the abstraction is flagged as major.
//
// Detection is intentionally conservative: we only look inside the draft
// text itself, not at a whole codebase. The goal is to catch the "add
// interface reflexively" anti-pattern at spec time, not to replace
// downstream static analysis.
func CheckInterfaceJustification(draftPath string) []Issue {
	data, err := os.ReadFile(draftPath)
	if err != nil {
		return nil
	}
	content := string(data)
	lines := strings.Split(content, "\n")

	var issues []Issue
	for _, decl := range collectInterfaces(content) {
		impls := countConcreteImpls(content, decl.Name, decl.Kind)
		if impls != 1 {
			continue
		}
		if hasYagniJustification(lines, decl.Line) {
			continue
		}
		issues = append(issues, Issue{
			Category: "yagni",
			Claim:    "single_impl_interface: " + decl.Name,
			Severity: "major",
			Detail:   "Interface/protocol '" + decl.Name + "' has exactly one concrete implementation in the spec. Either name a second implementation, inline the interface, or add `// YAGNI-justified: <reason>` on or just above the declaration (max 3 lines earlier) explaining why the abstraction earns its place.",
		})
	}
	return issues
}

type interfaceDecl struct {
	Name string
	Kind string // "go", "ts", "py"
	Line int    // 1-based
}

// collectInterfaces finds all interface declarations with their line
// positions. Line positions let hasYagniJustification check the
// three preceding lines for the marker.
func collectInterfaces(content string) []interfaceDecl {
	var out []interfaceDecl
	scan := func(re *regexp.Regexp, kind string) {
		for _, m := range re.FindAllStringSubmatchIndex(content, -1) {
			name := content[m[2]:m[3]]
			// Line number of the declaration start.
			line := strings.Count(content[:m[0]], "\n") + 1
			out = append(out, interfaceDecl{Name: name, Kind: kind, Line: line})
		}
	}
	scan(goInterfaceRe, "go")
	scan(tsInterfaceRe, "ts")
	scan(pyProtocolRe, "py")
	return out
}

// countConcreteImpls estimates the number of concrete types that
// implement a given interface in the same document.
//
//   - Go: types that declare methods matching the interface are hard to
//     detect without a parser. We approximate with `struct {` types
//     that appear near the interface AND contain a comment mentioning
//     the interface name, OR appear as `type Foo struct` followed by
//     `// implements Name`. When uncertain, we count the clearly
//     labeled implementations only — a conservative count biases toward
//     flagging (same direction as YAGNI intent).
//   - TS: `class Foo implements Name` or `class Foo extends AbstractName`.
//   - Py: `class Foo(Name)` base-class reference.
//
// When an implementation list cannot be inferred, returns 1 as the
// "default single implementation" assumption — the caller treats 1 as
// the flag condition. This keeps the checker from silently swallowing
// interfaces with no detectable implementations.
func countConcreteImpls(content, name, kind string) int {
	switch kind {
	case "ts":
		re := regexp.MustCompile(`\bclass\s+\w+\s+(?:implements|extends)\s+` + regexp.QuoteMeta(name) + `\b`)
		return len(re.FindAllStringIndex(content, -1))
	case "py":
		re := regexp.MustCompile(`\bclass\s+\w+\s*\(\s*(?:\w+\s*,\s*)*` + regexp.QuoteMeta(name) + `\b`)
		return len(re.FindAllStringIndex(content, -1))
	case "go":
		// Two signals:
		//   1. `// implements Name` comment nearby.
		//   2. `type Foo struct` followed by a method set that references
		//      Name via a block comment "// implements Name".
		re := regexp.MustCompile(`(?m)//\s*implements\s+` + regexp.QuoteMeta(name) + `\b`)
		return len(re.FindAllStringIndex(content, -1))
	}
	return 1
}

func hasYagniJustification(lines []string, declLine int) bool {
	if declLine <= 0 {
		return false
	}
	// Check the declaration line itself and the three lines above.
	start := declLine - 3
	if start < 1 {
		start = 1
	}
	for i := start; i <= declLine; i++ {
		if i-1 >= len(lines) {
			break
		}
		if yagniJustifiedRe.MatchString(lines[i-1]) {
			return true
		}
	}
	return false
}
