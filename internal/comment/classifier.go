package comment

import (
	"regexp"
	"strings"
)

// Classification adds metadata that the preview view shows BEFORE Claude
// runs the real analysis. Heuristic-only — Pass A in /bts-comment-apply
// supersedes this with structured findings.
type Classification struct {
	TargetSection string   `json:"target_section,omitempty"` // best-guess section heading the comment refers to
	LikelyImpact  []string `json:"likely_impact,omitempty"`  // other docs that may need cascade updates
	SeverityHint  string   `json:"severity_hint"`            // "low" | "medium" | "high"
}

// Cascade-impact heuristics: keywords in the comment body that suggest the
// edit will need to land in another doc too. Conservative — false positives
// here only pad the preview view; the real cascade detection happens in
// the skill's Pass A.
var cascadeRules = []struct {
	docs    []string
	pattern *regexp.Regexp
}{
	{[]string{"scope.md", "vision.md"}, regexp.MustCompile(`(?i)\b(scope|vision|boundary|out[- ]of[- ]scope|in[- ]scope|goal|non[- ]goal)\b`)},
	{[]string{"wireframe.md"}, regexp.MustCompile(`(?i)\b(endpoint|interface|signature|component|module|api|route)\b`)},
	{[]string{"domain.md"}, regexp.MustCompile(`(?i)\b(entity|invariant|state|owner|lifecycle|partition)\b`)},
	{[]string{"final.md"}, regexp.MustCompile(`(?i)\b(final|implementation|spec)\b`)},
}

var highSeverityRE = regexp.MustCompile(`(?i)\b(must|required|broken|wrong|incorrect|missing|critical|breaks?)\b`)

// Classify computes a heuristic classification for one comment.
// It does NOT consult Claude — that happens in /bts-comment-apply Pass A.
func Classify(c Comment) Classification {
	cls := Classification{}

	// Severity hint
	switch c.Kind {
	case KindBlock:
		cls.SeverityHint = "high"
	case KindQuestion:
		if highSeverityRE.MatchString(c.Body) {
			cls.SeverityHint = "medium"
		} else {
			cls.SeverityHint = "low"
		}
	case KindFreeForm:
		cls.SeverityHint = "low"
	default:
		if highSeverityRE.MatchString(c.Body) {
			cls.SeverityHint = "medium"
		} else {
			cls.SeverityHint = "low"
		}
	}

	// Target section: deepest heading in section path
	if len(c.SectionPath) > 0 {
		cls.TargetSection = c.SectionPath[len(c.SectionPath)-1]
	}

	// Cascade impact: any doc OTHER than the comment's own file
	seen := map[string]bool{c.File: true}
	for _, rule := range cascadeRules {
		if !rule.pattern.MatchString(c.Body) {
			continue
		}
		for _, d := range rule.docs {
			if seen[d] {
				continue
			}
			seen[d] = true
			cls.LikelyImpact = append(cls.LikelyImpact, d)
		}
	}

	return cls
}

// LooksHigh is a tiny helper for the renderer to color-code rows.
func (cls Classification) LooksHigh() bool {
	return strings.EqualFold(cls.SeverityHint, "high")
}
