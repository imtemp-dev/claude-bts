package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/engine"
	"github.com/imtemp-dev/claude-bts/internal/state"
)

const mergeTestVerify = `# Verification

<bts-findings>
{
  "critical": 1, "major": 0, "minor_resolvable": 0, "minor_deferred": 0, "info": 0,
  "paths_total": 3, "paths_unspecified": 0,
  "findings": [
    {"severity": "critical", "title": "INV-001 has two owners", "anchor": "§2"}
  ]
}
</bts-findings>

1. [CRITICAL] INV-001 has two owners
`

const mergeTestAudit = `<bts-findings>
{
  "critical": 0, "major": 1, "minor_resolvable": 0, "minor_deferred": 0, "info": 0,
  "branches_total": 4,
  "findings": [
    {"severity": "major", "title": "rollback path unaddressed", "anchor": "§5"}
  ]
}
</bts-findings>
`

const mergeTestSim = `# Simulation
<bts-findings>
{"critical": 0, "major": 0, "minor_resolvable": 1, "minor_deferred": 0, "info": 0,
 "findings": [{"severity": "minor_resolvable", "title": "S03 copy not localized", "anchor": "S03"}]}
</bts-findings>
`

func writeRecipeFile(t *testing.T, root, recipeID, rel, content string) string {
	t.Helper()
	p := filepath.Join(root, ".bts", "specs", "recipes", recipeID, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return filepath.ToSlash(filepath.Join(".bts", "specs", "recipes", recipeID, rel))
}

// One concurrent verify+audit+simulate batch is ONE round. A measured
// recipe logged its batch as three single-dimension rounds — three of
// six cap slots, and no two rounds of one class to judge convergence
// by — because joining three findings blocks was left to the
// orchestrator. --merge does the join.
func TestRecipeLog_MergeRecordsOneRoundFromThreeBlocks(t *testing.T) {
	root := newRecipeFixture(t, "r-m01", "draft", 0, 0, nil)
	writeRecipeFile(t, root, "r-m01", "draft.md", "# Draft\n\n## 1. What ships\n\nA thing.\n")
	v := writeRecipeFile(t, root, "r-m01", "verification.md", mergeTestVerify)
	a := writeRecipeFile(t, root, "r-m01", "audit.md", mergeTestAudit)
	s := writeRecipeFile(t, root, "r-m01", "simulations/001-scenarios.md", mergeTestSim)

	args := []string{"r-m01",
		"--from-verification", v, "--merge", a, "--merge", s,
		"--doc", "draft.md", "--scope", "full",
		"--dimension", "verify", "--dimension", "audit", "--dimension", "simulate"}
	out := runRecipeLog(t, root, args...)
	if !strings.Contains(out, "merged 2 block(s)") {
		t.Errorf("expected a merge note on stderr, got:\n%s", out)
	}

	entries, err := state.ReadVerifyLog(root, "r-m01")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("one batch must be one round, got %d entries", len(entries))
	}
	e := entries[0]
	if e.Critical != 1 || e.Major != 1 || e.MinorResolvable != 1 {
		t.Errorf("counts = %d/%d/%d, want 1/1/1 (one from each instrument)", e.Critical, e.Major, e.MinorResolvable)
	}
	if !e.HasAllDimensions() {
		t.Errorf("dimensions = %v, want all three", e.Dimensions)
	}
	if e.DocHash == "" || e.VerificationHash == "" {
		t.Errorf("merged round must still record doc_hash and verification_hash: %+v", e)
	}

	// verification.md now carries the merged block, so bts validate's
	// cross-check and the recorded verification_hash agree with the entry.
	data, err := os.ReadFile(filepath.Join(root, v))
	if err != nil {
		t.Fatal(err)
	}
	counts, err := engine.ParseFindingsBlock(data)
	if err != nil {
		t.Fatalf("rewritten verification.md does not parse: %v", err)
	}
	if counts.Total() != 3 {
		t.Errorf("merged block total = %d, want 3", counts.Total())
	}
	if !strings.Contains(string(data), "<!-- bts-merged: ") {
		t.Errorf("merged file must carry the merge marker")
	}

	// All three findings entered the ledger under one round.
	states, err := state.LoadFindings(root, "r-m01", "draft.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 3 {
		t.Errorf("ledger holds %d findings, want 3", len(states))
	}

	// Re-running the same merge must not double-count.
	out = runRecipeLog(t, root, args...)
	if !strings.Contains(out, "already merged") {
		t.Errorf("a second --merge of the same file must be refused, got:\n%s", out)
	}
	entries, _ = state.ReadVerifyLog(root, "r-m01")
	if len(entries) != 1 {
		t.Errorf("refused merge must not record a round, got %d entries", len(entries))
	}
}

func TestRecipeLog_MergeNeedsFromVerification(t *testing.T) {
	root := newRecipeFixture(t, "r-m02", "draft", 0, 0, nil)
	a := writeRecipeFile(t, root, "r-m02", "audit.md", mergeTestAudit)
	out := runRecipeLog(t, root, "r-m02", "--merge", a, "--doc", "draft.md")
	if !strings.Contains(out, "--merge needs --from-verification") {
		t.Errorf("expected a usage error, got:\n%s", out)
	}
}
