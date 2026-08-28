package engine

import (
	"os"
	"regexp"
	"strings"
)

// wireframeNodeRe captures mermaid node labels in flowchart diagrams.
//
// Matches: `A["label"]`, `A("label")`, `A{"label"}`, `A[["label"]]`, etc.
// The captured group is the label text between the first pair of quotes.
// We deliberately match `"..."` quoted labels — unquoted mermaid labels
// cannot contain spaces, which means they cannot contain " and " and so
// pass the responsibility rule by construction.
var wireframeNodeRe = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*(?:\[{1,2}|\(|\{)"([^"\n]+)"(?:\]{1,2}|\)|\})`)

// conjunctionRe matches the banned conjunctions as whole-word occurrences
// in a responsibility line. Case-insensitive for "and"; Korean "및" and
// "&" are matched literally. Word boundaries prevent false hits on
// "android", "understand", etc.
var conjunctionRe = regexp.MustCompile(`(?i)(\band\b|\s&\s|\s및\s)`)

// CheckWireframeResponsibilities parses wireframe.md mermaid nodes and
// flags any node whose label (second line, responsibility) contains a
// banned conjunction. The label format is "name\n(responsibility)" per
// jig-wireframe SKILL.md Step 1.
//
// The checker is deliberately permissive about label format — if a node
// does not follow the "\n(responsibility)" convention we pass it through
// rather than fighting authoring style. The check targets the specific
// "X and Y" pattern that signals a two-job module.
func CheckWireframeResponsibilities(wireframePath string) []Issue {
	data, err := os.ReadFile(wireframePath)
	if err != nil {
		return nil
	}

	matches := wireframeNodeRe.FindAllStringSubmatch(string(data), -1)
	if len(matches) == 0 {
		return nil
	}

	seen := map[string]bool{} // dedupe by label
	var issues []Issue
	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		label := m[1]
		if seen[label] {
			continue
		}
		seen[label] = true

		responsibility := extractResponsibilityLine(label)
		if responsibility == "" {
			continue
		}
		if conjunctionRe.MatchString(responsibility) {
			issues = append(issues, Issue{
				Category: "wireframe_responsibility",
				Claim:    "multi_job_node: " + truncate(label, 60),
				Severity: "major",
				Detail:   "Node responsibility \"" + responsibility + "\" contains a conjunction (and / & / 및). Split the node into two: one job per module. See jig-wireframe SKILL.md Step 1.",
			})
		}
	}
	return issues
}

// extractResponsibilityLine isolates the text inside "(...)" on the
// second (or only) line of a mermaid label. Falls back to the full label
// when no parentheses appear — authors sometimes omit them.
func extractResponsibilityLine(label string) string {
	// mermaid uses literal "\n" two-char sequences inside quoted labels.
	// It also tolerates actual newlines.
	lines := strings.Split(strings.ReplaceAll(label, `\n`, "\n"), "\n")
	// Prefer an explicit responsibility line (any line with "(...)").
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if open := strings.Index(line, "("); open >= 0 {
			if close := strings.LastIndex(line, ")"); close > open {
				return strings.TrimSpace(line[open+1 : close])
			}
		}
	}
	// No "(...)" found. Treat the whole label as the responsibility only
	// when the label has multiple lines (otherwise we'd be checking the
	// module name itself, which commonly uses slashes).
	if len(lines) > 1 {
		return strings.TrimSpace(lines[len(lines)-1])
	}
	return ""
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
