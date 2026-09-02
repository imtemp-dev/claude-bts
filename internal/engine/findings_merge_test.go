package engine

import (
	"strings"
	"testing"
)

const mergePrimary = `# Verification

<bts-findings>
{
  "critical": 1,
  "major": 0,
  "minor_resolvable": 1,
  "minor_deferred": 0,
  "info": 0,
  "paths_total": 7,
  "paths_unspecified": 0,
  "findings": [
    {"severity": "critical", "title": "INV-001 has two owners", "anchor": "§2"},
    {"severity": "minor_resolvable", "title": "header says Level 2", "anchor": "§1"}
  ]
}
</bts-findings>

1. [CRITICAL] INV-001 has two owners
2. [MINOR resolvable] header says Level 2
`

const mergeAudit = `<bts-findings>
{
  "critical": 0,
  "major": 2,
  "minor_resolvable": 0,
  "minor_deferred": 1,
  "info": 0,
  "branches_total": 12,
  "findings": [
    {"severity": "major", "title": "rollback path unaddressed", "anchor": "§5"},
    {"severity": "major", "title": "no timeout on token refresh", "anchor": "§3"},
    {"severity": "minor_deferred", "title": "cold-start budget unmeasured", "anchor": "§8"}
  ]
}
</bts-findings>
`

const mergeSim = `# Simulation
<bts-findings>
{"critical": 0, "major": 1, "minor_resolvable": 0, "minor_deferred": 0, "info": 0,
 "findings": [{"severity": "major", "title": "restore path never reaches projection", "anchor": "S04"}]}
</bts-findings>
`

func TestMergeFindingsBlocks_UnionsCountsAndArray(t *testing.T) {
	out, counts, err := MergeFindingsBlocks([]byte(mergePrimary),
		[][]byte{[]byte(mergeAudit), []byte(mergeSim)}, []string{"audit.md", "simulations/001.md"})
	if err != nil {
		t.Fatal(err)
	}
	if counts.Critical != 1 || counts.Major != 3 || counts.MinorResolvable != 1 || counts.MinorDeferred != 1 || counts.Info != 0 {
		t.Errorf("counts = %+v", *counts)
	}
	if len(counts.Findings) != 6 {
		t.Errorf("findings = %d, want 6", len(counts.Findings))
	}

	// The rewritten file must parse back to the same union — this is
	// what `bts recipe log` reads next, and what `bts validate`
	// cross-checks against the verify-log entry.
	again, err := ParseFindingsBlock(out)
	if err != nil {
		t.Fatalf("merged block does not parse: %v\n%s", err, out)
	}
	if again.Total() != 6 || again.Major != 3 {
		t.Errorf("re-parsed = %+v", *again)
	}
	s := string(out)
	for _, keep := range []string{`"paths_total": 7`, "1. [CRITICAL] INV-001 has two owners", "<!-- bts-merged: audit.md, simulations/001.md -->"} {
		if !strings.Contains(s, keep) {
			t.Errorf("merged output lost %q", keep)
		}
	}
	if strings.Count(s, "<bts-findings>") != 1 {
		t.Errorf("merged output must hold exactly one block:\n%s", s)
	}
}

func TestMergeFindingsBlocks_RefusesSecondMerge(t *testing.T) {
	out, _, err := MergeFindingsBlocks([]byte(mergePrimary), [][]byte{[]byte(mergeSim)}, []string{"simulations/001.md"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := MergeFindingsBlocks(out, [][]byte{[]byte(mergeSim)}, []string{"simulations/001.md"}); err == nil {
		t.Fatal("a second merge must be refused, not double-counted")
	} else if !strings.Contains(err.Error(), "already merged") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestMergeFindingsBlocks_RefusesCountsWithoutArray(t *testing.T) {
	noArray := `<bts-findings>
{"critical": 0, "major": 2, "minor_resolvable": 0, "minor_deferred": 0, "info": 0}
</bts-findings>`
	if _, _, err := MergeFindingsBlocks([]byte(mergePrimary), [][]byte{[]byte(noArray)}, []string{"audit.md"}); err == nil {
		t.Fatal("counts without titles cannot enter the ledger; the merge must refuse")
	}
}

func TestMergeFindingsBlocks_ExtraMustBeValid(t *testing.T) {
	bad := `<bts-findings>{"critical": 1, "major": 0, "minor_resolvable": 0, "minor_deferred": 0,
  "findings": [{"severity": "major", "title": "mismatched severity"}]}</bts-findings>`
	_, _, err := MergeFindingsBlocks([]byte(mergePrimary), [][]byte{[]byte(bad)}, []string{"audit.md"})
	if err == nil || !strings.Contains(err.Error(), "audit.md") {
		t.Fatalf("an inconsistent extra must be refused and named, got %v", err)
	}
}

func TestMergeFindingsBlocks_NoExtrasIsIdentityOnCounts(t *testing.T) {
	_, counts, err := MergeFindingsBlocks([]byte(mergePrimary), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if counts.Total() != 2 {
		t.Errorf("no extras must leave the counts alone, got %+v", *counts)
	}
}
