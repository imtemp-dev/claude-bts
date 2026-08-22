package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeWireframe(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "wireframe.md")
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

func TestCheckWireframeResponsibilities_SingleJobPasses(t *testing.T) {
	path := writeWireframe(t, "```mermaid\nflowchart TD\n    A[\"module-a\\n(owns word order)\"]\n    B[\"module-b\\n(handles pointer events)\"]\n```")
	issues := CheckWireframeResponsibilities(path)
	if len(issues) != 0 {
		t.Fatalf("expected 0 issues, got %d: %v", len(issues), issues)
	}
}

func TestCheckWireframeResponsibilities_FlagsAnd(t *testing.T) {
	path := writeWireframe(t, "```mermaid\nflowchart TD\n    A[\"Card\\n(renders view and owns placement)\"]\n```")
	issues := CheckWireframeResponsibilities(path)
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d: %v", len(issues), issues)
	}
	if issues[0].Severity != "major" {
		t.Errorf("want major, got %s", issues[0].Severity)
	}
	if !strings.Contains(issues[0].Claim, "multi_job_node") {
		t.Errorf("claim should mention multi_job_node, got %s", issues[0].Claim)
	}
}

func TestCheckWireframeResponsibilities_FlagsAmpersand(t *testing.T) {
	path := writeWireframe(t, "```mermaid\nflowchart TD\n    A[\"X\\n(reads & writes)\"]\n```")
	issues := CheckWireframeResponsibilities(path)
	if len(issues) != 1 {
		t.Fatalf("want 1 issue for &, got %d", len(issues))
	}
}

func TestCheckWireframeResponsibilities_FlagsKoreanConjunction(t *testing.T) {
	path := writeWireframe(t, "```mermaid\nflowchart TD\n    A[\"X\\n(읽기 및 쓰기)\"]\n```")
	issues := CheckWireframeResponsibilities(path)
	if len(issues) != 1 {
		t.Fatalf("want 1 issue for 및, got %d", len(issues))
	}
}

// The checker must not trip on "and" embedded in a word (android,
// understand) — that would be a false positive.
func TestCheckWireframeResponsibilities_NoFalsePositives(t *testing.T) {
	path := writeWireframe(t, "```mermaid\nflowchart TD\n    A[\"android-adapter\\n(understands protocol)\"]\n```")
	issues := CheckWireframeResponsibilities(path)
	if len(issues) != 0 {
		t.Fatalf("false positive on android/understand, got %v", issues)
	}
}

// Node without parentheses — unusual authoring style, we pass through
// rather than fighting it.
func TestCheckWireframeResponsibilities_NoParensPassesThrough(t *testing.T) {
	path := writeWireframe(t, "```mermaid\nflowchart TD\n    A[\"module-a\"]\n```")
	issues := CheckWireframeResponsibilities(path)
	if len(issues) != 0 {
		t.Fatalf("no-parens label should pass, got %v", issues)
	}
}

// VerifyDocument wires the checker in for wireframe.md.
func TestVerifyDocument_ChecksWireframeResponsibilities(t *testing.T) {
	path := writeWireframe(t, "```mermaid\nflowchart TD\n    A[\"Card\\n(renders and owns data)\"]\n```")
	result, err := VerifyDocument(path, "", true)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if result.Summary.Major < 1 {
		t.Fatalf("expected major issue for conjunction, got %+v", result.Summary)
	}
}
