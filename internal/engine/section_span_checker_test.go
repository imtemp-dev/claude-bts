package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func spanDoc(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "draft.md")
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return p
}

func body(n int) string { return strings.Repeat("text\n", n) }

func TestMeasureSectionSpans_IgnoresHeadingsInsideFences(t *testing.T) {
	spans := MeasureSectionSpans("## Real\n" + body(3) +
		"```sh\n## not a heading\n```\n" + body(2) + "## Second\n" + body(1))
	if len(spans) != 2 {
		t.Fatalf("got %d sections, want 2: %+v", len(spans), spans)
	}
	if spans[0].Title != "Real" || spans[1].Title != "Second" {
		t.Errorf("titles = %q, %q", spans[0].Title, spans[1].Title)
	}
	// heading + 3 text + 3 fence + 2 text = lines 1..9, with "## Second"
	// starting at line 10.
	if spans[0].Lines != 9 {
		t.Errorf("first section = %d lines, want 9", spans[0].Lines)
	}
	if spans[1].Line != 10 {
		t.Errorf("second heading at line %d, want 10", spans[1].Line)
	}
}

func TestCheckSectionSpan_ReportsOnlyOversizeSections(t *testing.T) {
	doc := spanDoc(t, "## Small\n"+body(10)+"## Sprawling\n"+body(60)+"## Also small\n"+body(5))
	issues := CheckSectionSpan(doc, 20, SeverityInfo)
	if len(issues) != 1 {
		t.Fatalf("got %d issues, want 1: %+v", len(issues), issues)
	}
	if !strings.Contains(issues[0].Claim, "Sprawling") {
		t.Errorf("claim should name the section, got %q", issues[0].Claim)
	}
	if issues[0].Severity != SeverityInfo {
		t.Errorf("severity = %q, want info by default", issues[0].Severity)
	}
	if !strings.Contains(issues[0].Detail, "verify.max_section_lines") {
		t.Errorf("detail should name the setting that produced it, got %q", issues[0].Detail)
	}
}

func TestCheckSectionSpan_SeverityIsConfigurable(t *testing.T) {
	doc := spanDoc(t, "## Sprawling\n"+body(60))
	issues := CheckSectionSpan(doc, 20, SeverityMajor)
	if len(issues) != 1 || issues[0].Severity != SeverityMajor {
		t.Fatalf("want one major issue, got %+v", issues)
	}
}

func TestCheckSectionSpan_ZeroThresholdDisablesTheCheck(t *testing.T) {
	doc := spanDoc(t, "## Sprawling\n"+body(600))
	if issues := CheckSectionSpan(doc, 0, SeverityMajor); len(issues) != 0 {
		t.Fatalf("max_section_lines=0 must disable the check, got %+v", issues)
	}
}

// A file ending in a newline splits to a trailing empty element that is
// not a line of the document. Counting it made the LAST section — the
// one most likely to be sitting near the threshold — read one line
// longer than it is.
func TestMeasureSectionSpans_TrailingNewlineIsNotALine(t *testing.T) {
	withNewline := MeasureSectionSpans("## One\n" + body(4))     // body ends in \n
	without := MeasureSectionSpans("## One\n" + body(4) + "end") // no trailing \n
	if len(withNewline) != 1 || len(without) != 1 {
		t.Fatalf("want one section each, got %d and %d", len(withNewline), len(without))
	}
	if withNewline[0].Lines != 5 {
		t.Errorf("heading + 4 body lines = 5, got %d", withNewline[0].Lines)
	}
	if without[0].Lines != 6 {
		t.Errorf("heading + 4 body + 1 unterminated = 6, got %d", without[0].Lines)
	}
}

// An unrecognised verify.section_span_severity used to pass straight
// through into the Issue, where nothing downstream counts it — so a typo
// turned the check off rather than changing how loudly it spoke.
func TestSettings_RejectsAnUnknownSectionSpanSeverity(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".jig", "config")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	write := func(sev string) *Settings {
		t.Helper()
		body := "verify:\n  section_span_severity: " + sev + "\n"
		if err := os.WriteFile(filepath.Join(dir, "settings.yaml"), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
		st, err := LoadSettings(root)
		if err != nil {
			t.Fatal(err)
		}
		return st
	}
	if got := write("banana").Verify.SectionSpanSeverity; got != SeverityInfo {
		t.Errorf("an unknown severity must fall back to the default, got %q", got)
	}
	if got := write("major").Verify.SectionSpanSeverity; got != SeverityMajor {
		t.Errorf("a valid severity must survive, got %q", got)
	}
}
