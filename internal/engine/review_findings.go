package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// ReviewFindingsBlock is the machine-readable record /bts-review appends to
// review.md as <bts-review-findings>{...}</bts-review-findings>. The prose
// above it stays for people; the block exists so one rule can be checked
// instead of asked for: a critical code-review finding carries the run
// that shows it (or, for a security finding, the concrete scenario).
//
// It is separate from <bts-findings> on purpose. That block's severity
// vocabulary (minor_resolvable, minor_deferred) and count rules belong to
// spec verification rounds; a code review has neither.
type ReviewFindingsBlock struct {
	Findings []ReviewFinding `json:"findings"`
}

// ReviewFinding is one consolidated finding after deduplication.
type ReviewFinding struct {
	ID          string `json:"id"`          // CRT-001, MAJ-002, …
	Severity    string `json:"severity"`    // critical | major | minor | info
	Perspective string `json:"perspective"` // quality | security | architecture | other
	Title       string `json:"title"`
	Anchor      string `json:"anchor,omitempty"` // file:line
	// Level is the confidence the finding reached (prove stage P4): L1
	// said, L2 pointed (file:line), L3 walked, L4 ran, L5 drove the app.
	Level    string          `json:"level,omitempty"`
	Evidence *ReviewEvidence `json:"evidence,omitempty"`
}

// ReviewEvidence is what makes a critical finding more than a claim.
//
// kind "run": the orchestrator ran Command once and recorded what it
// printed (Observed) beside what correct code would print (Required).
// kind "scenario": security findings only — the input, the path through
// the code, and the outcome. Security review is recall-bound, and the
// flaws it exists to catch are the hardest to show with one command.
type ReviewEvidence struct {
	Kind     string `json:"kind"`
	Command  string `json:"command,omitempty"`
	Observed string `json:"observed,omitempty"`
	Required string `json:"required,omitempty"`
	Input    string `json:"input,omitempty"`
	Path     string `json:"path,omitempty"`
	Outcome  string `json:"outcome,omitempty"`
}

var reviewFindingsBlockRe = regexp.MustCompile(`(?s)<bts-review-findings>\s*(\{.*?\})\s*</bts-review-findings>`)

// ErrNoReviewFindingsBlock is returned when review.md carries no block —
// a review written before the block existed, or one that skipped it.
var ErrNoReviewFindingsBlock = errors.New("review.md has no <bts-review-findings> block")

var validReviewSeverities = map[string]bool{
	"critical": true, "major": true, "minor": true, "info": true,
}

// ParseReviewFindingsBlock extracts the block from review.md and checks
// it. It returns ErrNoReviewFindingsBlock when there is none, and an
// error naming every problem when the block is malformed or a critical
// finding lacks its evidence. The block is returned alongside a
// validation error so callers can still report on it.
func ParseReviewFindingsBlock(data []byte) (*ReviewFindingsBlock, error) {
	matches := reviewFindingsBlockRe.FindAllSubmatch(data, -1)
	if len(matches) == 0 {
		return nil, ErrNoReviewFindingsBlock
	}
	if len(matches) > 1 {
		return nil, fmt.Errorf("review.md has %d <bts-review-findings> blocks; exactly one is allowed", len(matches))
	}
	var block ReviewFindingsBlock
	if err := json.Unmarshal(matches[0][1], &block); err != nil {
		return nil, fmt.Errorf("invalid JSON in <bts-review-findings>: %w", err)
	}

	var problems []string
	for i, f := range block.Findings {
		label := strings.TrimSpace(f.ID)
		if label == "" {
			label = fmt.Sprintf("findings[%d]", i)
			problems = append(problems, label+": missing id")
		}
		if strings.TrimSpace(f.Title) == "" {
			problems = append(problems, label+": missing title")
		}
		if !validReviewSeverities[f.Severity] {
			problems = append(problems, fmt.Sprintf("%s: severity %q is not one of critical, major, minor, info", label, f.Severity))
			continue
		}
		if f.Severity == "critical" {
			problems = append(problems, criticalEvidenceProblems(label, f)...)
		}
		problems = append(problems, levelProblems(label, f)...)
	}
	if len(problems) > 0 {
		return &block, errors.New(strings.Join(problems, "; "))
	}
	return &block, nil
}

func criticalEvidenceProblems(label string, f ReviewFinding) []string {
	e := f.Evidence
	if e == nil {
		return []string{label + ": critical with no evidence (run its falsifier, or downgrade it to major)"}
	}
	blank := func(s string) bool { return strings.TrimSpace(s) == "" }
	switch e.Kind {
	case "run":
		var missing []string
		if blank(e.Command) {
			missing = append(missing, "command")
		}
		if blank(e.Observed) {
			missing = append(missing, "observed")
		}
		if blank(e.Required) {
			missing = append(missing, "required")
		}
		if len(missing) > 0 {
			return []string{fmt.Sprintf("%s: run evidence lacks %s", label, strings.Join(missing, ", "))}
		}
	case "scenario":
		if f.Perspective != "security" {
			return []string{label + ": scenario evidence is for security findings only; others need a run"}
		}
		if blank(e.Input) || blank(e.Path) || blank(e.Outcome) {
			return []string{label + ": scenario evidence needs input, path and outcome"}
		}
	default:
		return []string{fmt.Sprintf("%s: evidence kind %q is not run or scenario", label, e.Kind)}
	}
	return nil
}

var reviewLevelRe = regexp.MustCompile(`^L([1-5])$`)

// declaredLevel returns the finding's stated level (1-5), or 0 when it
// states none or an invalid one.
func declaredLevel(f ReviewFinding) int {
	m := reviewLevelRe.FindStringSubmatch(strings.TrimSpace(f.Level))
	if m == nil {
		return 0
	}
	return int(m[1][0] - '0')
}

// evidenceLevel is the level the finding's evidence shows: a run is L4, a
// security scenario walked through the code is L3, an anchor is L2, a
// bare claim is L1.
func evidenceLevel(f ReviewFinding) int {
	switch {
	case f.Evidence != nil && f.Evidence.Kind == "run":
		return 4
	case f.Evidence != nil && f.Evidence.Kind == "scenario":
		return 3
	case strings.TrimSpace(f.Anchor) != "":
		return 2
	}
	return 1
}

// EffectiveLevel is the finding's confidence level (prove stage P4): the
// stated level, or, for a finding that states none, the level its
// evidence shows.
func EffectiveLevel(f ReviewFinding) int {
	if n := declaredLevel(f); n > 0 {
		return n
	}
	return evidenceLevel(f)
}

// levelProblems rejects a level that is not L1-L5, and one that claims a
// run (L4 ran, L5 drove the app) without run evidence: a level may not
// say more than the evidence shows.
func levelProblems(label string, f ReviewFinding) []string {
	if strings.TrimSpace(f.Level) == "" {
		return nil
	}
	n := declaredLevel(f)
	if n == 0 {
		return []string{fmt.Sprintf("%s: level %q is not one of L1 to L5", label, f.Level)}
	}
	if n >= 4 && (f.Evidence == nil || f.Evidence.Kind != "run") {
		return []string{fmt.Sprintf("%s: level L%d claims a run, but the finding has no run evidence (command, observed, required)", label, n)}
	}
	return nil
}
