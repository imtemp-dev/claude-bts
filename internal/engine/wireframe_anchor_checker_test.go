package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writePair(t *testing.T, wireframe, draft string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	wfPath := filepath.Join(dir, "wireframe.md")
	drPath := filepath.Join(dir, "draft.md")
	if err := os.WriteFile(wfPath, []byte(wireframe), 0644); err != nil {
		t.Fatalf("write wireframe: %v", err)
	}
	if err := os.WriteFile(drPath, []byte(draft), 0644); err != nil {
		t.Fatalf("write draft: %v", err)
	}
	return drPath, wfPath
}

func TestCheckWireframeAnchors_OneToOne(t *testing.T) {
	draft, wireframe := writePair(t,
		`<!-- path-id: path-happy -->
<!-- path-id: path-timeout -->`,
		`## Paths

<!-- path: wireframe.md#path-happy -->
### happy detail
<!-- path: wireframe.md#path-timeout -->
### timeout detail`)

	issues := CheckWireframeAnchors(draft, wireframe)
	if len(issues) != 0 {
		t.Fatalf("expected 0 issues, got %d: %v", len(issues), issues)
	}
}

func TestCheckWireframeAnchors_UnmappedPath(t *testing.T) {
	draft, wireframe := writePair(t,
		`<!-- path-id: path-1 -->
<!-- path-id: path-2 -->`,
		`<!-- path: wireframe.md#path-1 -->`)

	issues := CheckWireframeAnchors(draft, wireframe)
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d: %v", len(issues), issues)
	}
	if issues[0].Severity != "critical" {
		t.Errorf("want critical, got %s", issues[0].Severity)
	}
	if !strings.Contains(issues[0].Claim, "unmapped_path: path-2") {
		t.Errorf("claim should mention unmapped_path: path-2, got %s", issues[0].Claim)
	}
}

func TestCheckWireframeAnchors_OrphanDraftAnchor(t *testing.T) {
	draft, wireframe := writePair(t,
		`<!-- path-id: path-1 -->`,
		`<!-- path: wireframe.md#path-1 -->
<!-- path: wireframe.md#path-ghost -->`)

	issues := CheckWireframeAnchors(draft, wireframe)
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(issues))
	}
	if issues[0].Severity != "major" {
		t.Errorf("want major, got %s", issues[0].Severity)
	}
	if !strings.Contains(issues[0].Claim, "orphan_draft_anchor: path-ghost") {
		t.Errorf("claim should mention orphan, got %s", issues[0].Claim)
	}
}

func TestCheckWireframeAnchors_DuplicateWireframeID(t *testing.T) {
	draft, wireframe := writePair(t,
		`<!-- path-id: path-1 -->
<!-- path-id: path-1 -->
<!-- path-id: path-2 -->`,
		`<!-- path: wireframe.md#path-1 -->
<!-- path: wireframe.md#path-2 -->`)

	issues := CheckWireframeAnchors(draft, wireframe)
	if len(issues) != 1 {
		t.Fatalf("expected 1 duplicate issue, got %d: %v", len(issues), issues)
	}
	if !strings.Contains(issues[0].Claim, "duplicate_path_id: path-1") {
		t.Errorf("want duplicate_path_id tag, got %s", issues[0].Claim)
	}
	if issues[0].Severity != "major" {
		t.Errorf("want major, got %s", issues[0].Severity)
	}
}

func TestCheckWireframeAnchors_MultipleDraftRefsToSamePath(t *testing.T) {
	// draft can reference the same wireframe path multiple times (e.g.,
	// split into sub-sections). That is not an error.
	draft, wireframe := writePair(t,
		`<!-- path-id: path-1 -->`,
		`<!-- path: wireframe.md#path-1 -->
section A
<!-- path: wireframe.md#path-1 -->
section B`)

	issues := CheckWireframeAnchors(draft, wireframe)
	if len(issues) != 0 {
		t.Fatalf("multiple draft refs to same path should not error, got %d: %v", len(issues), issues)
	}
}

func TestCheckWireframeAnchors_MissingFiles(t *testing.T) {
	issues := CheckWireframeAnchors("/nope/draft.md", "/nope/wireframe.md")
	if len(issues) != 0 {
		t.Fatalf("missing files should pass through silently; got %d issues", len(issues))
	}
}

// Integration: VerifyDocument on draft.md invokes the anchor check.
func TestVerifyDocument_ChecksDraftAnchors(t *testing.T) {
	draft, _ := writePair(t,
		`<!-- path-id: path-1 -->
<!-- path-id: path-2 -->`,
		`<!-- path: wireframe.md#path-1 -->`)

	result, err := VerifyDocument(draft, "", true)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if result.Summary.Critical < 1 {
		t.Fatalf("expected unmapped_path to be counted as critical; got %+v", result.Summary)
	}
}
