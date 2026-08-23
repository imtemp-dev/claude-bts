package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-jig/internal/state"
)

func writeProjectFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestCheckConfigDrift_ActiveReviewerSecurity(t *testing.T) {
	root := t.TempDir()
	writeProjectFile(t, root, ".jig/config/settings.yaml", `
agents:
  # verifier: sonnet
  reviewer_security: sonnet
  # reviewer_arch: sonnet
`)
	issues := checkConfigDrift(root)
	if len(issues) != 1 || !strings.Contains(issues[0].message, "reviewer_security") {
		t.Fatalf("expected reviewer_security drift warning, got %v", issues)
	}
	if issues[0].level != "warning" {
		t.Errorf("must be warning (may be intentional), got %s", issues[0].level)
	}
}

func TestCheckConfigDrift_CommentedOverrideIsClean(t *testing.T) {
	root := t.TempDir()
	writeProjectFile(t, root, ".jig/config/settings.yaml", `
agents:
  # reviewer_security: sonnet
`)
	if issues := checkConfigDrift(root); len(issues) != 0 {
		t.Fatalf("commented override must not warn: %v", issues)
	}
}

func TestCheckConfigDrift_McpWithoutKeyPassthrough(t *testing.T) {
	root := t.TempDir()
	writeProjectFile(t, root, ".mcp.json",
		`{"mcpServers":{"context7":{"command":"/bin/bash","args":["-l","-c","exec npx -y @upstash/context7-mcp@latest"]}}}`)
	issues := checkConfigDrift(root)
	if len(issues) != 1 || !strings.Contains(issues[0].message, "CONTEXT7_API_KEY") {
		t.Fatalf("expected passthrough warning, got %v", issues)
	}
}

func TestCheckConfigDrift_McpWithPassthroughClean(t *testing.T) {
	root := t.TempDir()
	writeProjectFile(t, root, ".mcp.json",
		`{"mcpServers":{"context7":{"command":"/bin/bash","args":["-l","-c","exec npx -y @upstash/context7-mcp@latest ${CONTEXT7_API_KEY:+--api-key \"$CONTEXT7_API_KEY\"}"]}}}`)
	if issues := checkConfigDrift(root); len(issues) != 0 {
		t.Fatalf("passthrough present must not warn: %v", issues)
	}
}

func TestCheckConfigDrift_MissingFilesClean(t *testing.T) {
	root := t.TempDir()
	if issues := checkConfigDrift(root); len(issues) != 0 {
		t.Fatalf("missing config files must not warn: %v", issues)
	}
}

func TestCheckTestResultsProvenance(t *testing.T) {
	root := t.TempDir()
	id := "r-100-prov"
	dir := filepath.Join(root, ".jig", "specs", "recipes", id)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}

	// No file → clean.
	if issues := checkTestResultsProvenance(root, id); len(issues) != 0 {
		t.Fatalf("no file must be clean: %v", issues)
	}

	// Hand-recorded → warning.
	writeProjectFile(t, root, ".jig/specs/recipes/"+id+"/test-results.json",
		`{"recipe_id":"`+id+`","status":"pass","total":5,"passed":5}`)
	issues := checkTestResultsProvenance(root, id)
	if len(issues) != 1 || !strings.Contains(issues[0].message, "hand-recorded") {
		t.Fatalf("expected hand-recorded warning, got %v", issues)
	}

	// Machine-recorded → clean.
	writeProjectFile(t, root, ".jig/specs/recipes/"+id+"/test-results.json",
		`{"recipe_id":"`+id+`","status":"pass","recorded_by":"jig","exit_code":0}`)
	if issues := checkTestResultsProvenance(root, id); len(issues) != 0 {
		t.Fatalf("jig-recorded must be clean: %v", issues)
	}
}

func TestCheckDirtyVerifiedDocsDoctor(t *testing.T) {
	root := t.TempDir()
	id := "r-101-dirty"
	dir := filepath.Join(root, ".jig", "specs", "recipes", id)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	doc := filepath.Join(dir, "draft.md")
	writeProjectFile(t, root, ".jig/specs/recipes/"+id+"/draft.md", "verified")
	if err := state.SaveVerifySnapshot(root, id, doc); err != nil {
		t.Fatal(err)
	}

	// Clean while unchanged.
	if issues := checkDirtyVerifiedDocs(root, id); len(issues) != 0 {
		t.Fatalf("unchanged doc must be clean: %v", issues)
	}

	// Warn after modification.
	writeProjectFile(t, root, ".jig/specs/recipes/"+id+"/draft.md", "edited after verify")
	issues := checkDirtyVerifiedDocs(root, id)
	if len(issues) != 1 || !strings.Contains(issues[0].message, "draft.md") {
		t.Fatalf("expected dirty warning naming draft.md, got %v", issues)
	}
}

// A recipe verified before content hashes existed, then opened in a
// worktree or fresh clone, has no way to enforce rule 3 — and a gate
// that cannot fire looks exactly like a gate that passed. doctor is
// where that silence has to become visible.
func TestCheckUnenforceableRule3(t *testing.T) {
	root := t.TempDir()
	id := "r-102-legacy"
	writeProjectFile(t, root, ".jig/specs/recipes/"+id+"/draft.md", "whatever is here now")

	// Legacy round: full pass, no hash, and no local snapshot survived.
	if err := state.AppendVerifyLog(root, id, &state.VerifyLogEntry{
		Iteration: 1, Doc: "draft.md", FullPass: true, Status: "converged",
	}); err != nil {
		t.Fatal(err)
	}
	issues := checkUnenforceableRule3(root, id)
	if len(issues) != 1 || !strings.Contains(issues[0].message, "draft.md") {
		t.Fatalf("expected one warning naming draft.md, got %v", issues)
	}
	if !strings.Contains(issues[0].fix, "--scope full") {
		t.Errorf("the fix must say how to re-arm the gate, got: %s", issues[0].fix)
	}

	// One full pass with a hash re-arms it.
	if err := state.AppendVerifyLog(root, id, &state.VerifyLogEntry{
		Iteration: 2, Doc: "draft.md", FullPass: true, Status: "converged",
		DocHash: state.ContentHash([]byte("whatever is here now")),
	}); err != nil {
		t.Fatal(err)
	}
	if issues := checkUnenforceableRule3(root, id); len(issues) != 0 {
		t.Fatalf("a hashed full pass must clear the warning, got %v", issues)
	}
	if issues := checkDirtyVerifiedDocs(root, id); len(issues) != 0 {
		t.Fatalf("and the doc itself is clean, got %v", issues)
	}
}

// A surviving snapshot is enough on its own — no need to nag a project
// that is still working from the branch it verified on.
func TestCheckUnenforceableRule3_SnapshotCounts(t *testing.T) {
	root := t.TempDir()
	id := "r-103-snap"
	writeProjectFile(t, root, ".jig/specs/recipes/"+id+"/draft.md", "verified")
	if err := state.SaveVerifySnapshot(root, id,
		filepath.Join(root, ".jig", "specs", "recipes", id, "draft.md")); err != nil {
		t.Fatal(err)
	}
	if err := state.AppendVerifyLog(root, id, &state.VerifyLogEntry{
		Iteration: 1, Doc: "draft.md", FullPass: true, Status: "converged",
	}); err != nil {
		t.Fatal(err)
	}
	if issues := checkUnenforceableRule3(root, id); len(issues) != 0 {
		t.Fatalf("a live snapshot keeps the gate enforceable, got %v", issues)
	}
}

// unrecordedFixture writes a verify-log and returns the root and recipe id.
func unrecordedFixture(t *testing.T, entries []state.VerifyLogEntry) (string, string) {
	t.Helper()
	root := t.TempDir()
	id := "r-200-hashgap"
	writeProjectFile(t, root, ".jig/specs/recipes/"+id+"/draft.md", "body")
	for i := range entries {
		if err := state.AppendVerifyLog(root, id, &entries[i]); err != nil {
			t.Fatal(err)
		}
	}
	return root, id
}

// The measured gap: hashes recorded for the early rounds, none for the
// recent tail. checkUnenforceableRule3 stays quiet because the last full
// pass still carries a hash, so this check is what makes the silence
// visible.
func TestCheckUnrecordedRevisions_ReportsRecentTail(t *testing.T) {
	root, id := unrecordedFixture(t, []state.VerifyLogEntry{
		{Iteration: 26, Doc: "draft.md", FullPass: true, DocHash: "sha256:aaa", Status: "continue"},
		{Iteration: 27, Doc: "draft.md", FullPass: true, DocHash: "sha256:bbb", Status: "continue"},
		{Iteration: 28, Doc: "draft.md", FullPass: true, Status: "failed"},
		{Iteration: 29, Doc: "draft.md", Status: "failed"},
		{Iteration: 30, Doc: "draft.md", Status: "failed"},
	})
	if issues := checkUnenforceableRule3(root, id); len(issues) != 0 {
		t.Fatalf("precondition: the last-full-pass check should stay quiet here, got %v", issues)
	}
	issues := checkUnrecordedRevisions(root, id)
	if len(issues) != 1 {
		t.Fatalf("got %d issues, want 1: %+v", len(issues), issues)
	}
	if !strings.Contains(issues[0].message, "3 of the last 5") {
		t.Errorf("message should count the gap, got %q", issues[0].message)
	}
	if !strings.Contains(issues[0].message, "iteration 30") {
		t.Errorf("message should name the most recent gap, got %q", issues[0].message)
	}
	if issues[0].fix == "" {
		t.Error("a warning must say what to do about it")
	}
}

func TestCheckUnrecordedRevisions_QuietWhenAllRoundsRecordRevisions(t *testing.T) {
	root, id := unrecordedFixture(t, []state.VerifyLogEntry{
		{Iteration: 1, Doc: "draft.md", FullPass: true, DocHash: "sha256:aaa", Status: "continue"},
		{Iteration: 2, Doc: "draft.md", FullPass: true, DocHash: "sha256:bbb", Status: "converged"},
	})
	if issues := checkUnrecordedRevisions(root, id); len(issues) != 0 {
		t.Fatalf("want no issues, got %+v", issues)
	}
}

// Legacy unscoped rounds have no document to hash against and must not
// be reported as a gap.
func TestCheckUnrecordedRevisions_IgnoresUnscopedLegacyRounds(t *testing.T) {
	root, id := unrecordedFixture(t, []state.VerifyLogEntry{
		{Iteration: 1, Status: "continue"},
		{Iteration: 2, Status: "converged"},
	})
	if issues := checkUnrecordedRevisions(root, id); len(issues) != 0 {
		t.Fatalf("want no issues for a legacy unscoped log, got %+v", issues)
	}
}
