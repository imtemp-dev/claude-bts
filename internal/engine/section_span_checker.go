package engine

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

// Section span — findings scale with section length, so length is the
// one lever on the loop's total cost that is knowable before the loop
// starts.
//
// Measured over one Level 3 draft's 21 H2 sections and the 453 findings
// anchored to them, section length and finding count correlate at
// r=+0.95, at a near-constant density of ~12.7 findings per 100 lines.
// The three sections over 400 lines were half the document and carried
// 56% of its findings, at a density 28% above the rest. A 3,593-line
// document is therefore not a document that happened to need 34 verify
// rounds; it is a document that had roughly 450 findings in it before
// anyone looked, and the completion gate asks for zero.
//
// The recipe's own retrospective reached the same place from the other
// direction: "the cause is span, not spec quality — one document had to
// hold a design whose seams touch 29 files, so correcting a statement in
// one place falsified a statement in another."
//
// This check is deterministic and cheap, so it runs on every `bts verify`
// and the pressure to split arrives while splitting is still easy —
// rather than at round 30, when the operator's only remaining options
// were an override or a rewrite.
//
// Default severity is info: `bts verify` reports it every run without
// affecting the exit code. It is a report to the operator and to the
// verifying agent, NOT a ledger entry — findings.jsonl and
// verify-log.jsonl are fed by the <bts-findings> array in
// verification.md, so a span report reaches them only if the round
// writes it there. Raising verify.section_span_severity to "major" makes
// `bts verify` exit non-zero on an oversize section.

var sectionSpanH2Re = regexp.MustCompile(`(?m)^##\s+(\S.*?)\s*$`)

// SectionSpan is one H2 section's extent.
type SectionSpan struct {
	Title string
	Line  int // 1-based line of the heading
	Lines int // lines from the heading to the next H2 (or EOF)
}

// MeasureSectionSpans returns every H2 section in a markdown document
// with its length in lines. Fenced code blocks are respected, so a `##`
// inside a shell snippet does not read as a heading.
func MeasureSectionSpans(content string) []SectionSpan {
	lines := strings.Split(content, "\n")
	// A file ending in a newline splits to a trailing empty element that
	// is not a line of the document; counting it made every final
	// section one line longer than it is, which is one line of slack on
	// the threshold for exactly the section most likely to be near it.
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1]
	}
	type mark struct {
		title string
		at    int
	}
	var marks []mark
	inFence := false
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		if m := sectionSpanH2Re.FindStringSubmatch(l); m != nil && strings.HasPrefix(l, "## ") {
			marks = append(marks, mark{title: m[1], at: i + 1})
		}
	}
	out := make([]SectionSpan, 0, len(marks))
	for i, mk := range marks {
		end := len(lines)
		if i+1 < len(marks) {
			end = marks[i+1].at - 1
		}
		out = append(out, SectionSpan{Title: mk.title, Line: mk.at, Lines: end - mk.at + 1})
	}
	return out
}

// CheckSectionSpan reports H2 sections longer than maxLines. maxLines <= 0
// disables the check. severity selects how the issues are classified;
// an empty value means info.
func CheckSectionSpan(docPath string, maxLines int, severity string) []Issue {
	if maxLines <= 0 {
		return nil
	}
	if severity == "" {
		severity = SeverityInfo
	}
	data, err := os.ReadFile(docPath)
	if err != nil {
		return nil
	}
	var issues []Issue
	for _, s := range MeasureSectionSpans(string(data)) {
		if s.Lines <= maxLines {
			continue
		}
		issues = append(issues, Issue{
			Category: "section_span",
			Claim:    fmt.Sprintf("## %s (line %d) runs %d lines", s.Title, s.Line, s.Lines),
			Severity: severity,
			Detail: fmt.Sprintf(
				"Sections over %d lines accumulate findings faster than edits close them, and an "+
					"anchor this wide cannot localise a fix — the same claim gets corrected in one "+
					"clause and left standing in another. Split it along the seam it already has, "+
					"or move the justification prose out and leave the normative text. "+
					"Threshold: verify.max_section_lines=%d.",
				maxLines, maxLines),
		})
	}
	return issues
}

// CheckDocumentSpan reports a document whose whole length exceeds the
// limit.
//
// The per-section limit does not bound a document — several sections at
// the limit exceed this one many times over, which is how a measured
// draft reached 2,184 lines while every individual section stayed
// arguable. Findings scale with span at r=+0.95, so the length is the
// finding count the completion gate will later be asked to drive to
// zero, decided before anyone reads a word.
func CheckDocumentSpan(docPath string, maxLines int, severity string) []Issue {
	if maxLines <= 0 {
		return nil
	}
	if severity == "" {
		severity = SeverityInfo
	}
	data, err := os.ReadFile(docPath)
	if err != nil {
		return nil
	}
	lines := strings.Split(string(data), "\n")
	// A file ending in a newline splits to a trailing empty element that
	// is not a line of the document.
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1]
	}
	if len(lines) <= maxLines {
		return nil
	}
	return []Issue{{
		Category: "document_span",
		Claim:    fmt.Sprintf("the document runs %d lines", len(lines)),
		Severity: severity,
		Detail: fmt.Sprintf(
			"A blueprint carries what code cannot cheaply falsify — invariants and their "+
				"owners, boundary contracts, irreversible order, falsifiers, open questions — "+
				"and that is short. Past %d lines something else has gotten in: signatures a "+
				"compiler produces, scaffolding, per-file walkthroughs, enumerated error cases, "+
				"test assertion values. Each is settled in seconds by a build or one test run, "+
				"and each costs a verify round here plus the prose written to settle it, which "+
				"the next round re-checks. Find that content and take it out, or move it to the "+
				"document that owns it (wireframe.md, domain.md). "+
				"Threshold: verify.max_document_lines=%d.",
			maxLines, maxLines),
	}}
}
