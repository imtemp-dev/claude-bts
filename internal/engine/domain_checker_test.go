package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeDomainMd(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "domain.md")
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

// Single owner per invariant — the happy path. No issues.
func TestCheckInvariantOwnership_SingleOwners(t *testing.T) {
	path := writeDomainMd(t, `# Domain

## 1. Entities
…

## 2. Invariants

| ID      | Statement                               | Owner       | Enforcement |
|---------|-----------------------------------------|-------------|-------------|
| INV-001 | A word occupies at most one slot        | Arrangement | place()     |
| INV-002 | At most one drag is active              | DragGesture | begin()     |

## 3. State Partitioning
`)
	issues := CheckInvariantOwnership(path)
	if len(issues) != 0 {
		t.Fatalf("expected 0 issues, got %d: %v", len(issues), issues)
	}
}

// Same statement with two different owners — the Duolingo failure mode.
// Must be flagged critical.
func TestCheckInvariantOwnership_DuplicateStatementDifferentOwners(t *testing.T) {
	path := writeDomainMd(t, `## 2. Invariants

| ID      | Statement                        | Owner       | Enforcement |
|---------|----------------------------------|-------------|-------------|
| INV-001 | Canonical word order             | Arrangement | reorder()   |
| INV-002 | Canonical word order             | Card        | renderOrder |
`)
	issues := CheckInvariantOwnership(path)
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d: %v", len(issues), issues)
	}
	if issues[0].Severity != "critical" {
		t.Errorf("want critical, got %s", issues[0].Severity)
	}
	if !strings.Contains(issues[0].Claim, "invariant_multiple_owners") {
		t.Errorf("claim should tag invariant_multiple_owners, got %s", issues[0].Claim)
	}
}

// One row listing multiple owners ("Arrangement, Card") is itself a
// violation — the splitter detects both owners and flags the row.
func TestCheckInvariantOwnership_CommaSeparatedOwners(t *testing.T) {
	path := writeDomainMd(t, `## 2. Invariants

| ID      | Statement             | Owner              | Enforcement |
|---------|-----------------------|--------------------|-------------|
| INV-001 | Slot occupancy        | Arrangement, Card  | —           |
`)
	issues := CheckInvariantOwnership(path)
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(issues))
	}
}

// Whitespace/case differences in the statement must NOT hide the duplicate.
func TestCheckInvariantOwnership_NormalizesStatement(t *testing.T) {
	path := writeDomainMd(t, `## 2. Invariants

| ID      | Statement              | Owner       | Enforcement |
|---------|------------------------|-------------|-------------|
| INV-001 | Canonical   word order | Arrangement | reorder()   |
| INV-002 | CANONICAL WORD order   | Card        | renderOrder |
`)
	issues := CheckInvariantOwnership(path)
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue via normalization, got %d", len(issues))
	}
}

// The invariants section is bounded by the next H2 heading; content
// after it must not be parsed as invariant rows.
func TestCheckInvariantOwnership_StopsAtNextSection(t *testing.T) {
	path := writeDomainMd(t, `## 2. Invariants

| ID      | Statement | Owner       |
|---------|-----------|-------------|
| INV-001 | Foo       | Arrangement |

## 3. State Partitioning

| ID      | Statement | Owner    |
|---------|-----------|----------|
| DEC-001 | Foo       | RedHerring |
`)
	issues := CheckInvariantOwnership(path)
	if len(issues) != 0 {
		t.Fatalf("expected 0 issues (rows after § 2 must be ignored), got %d: %v", len(issues), issues)
	}
}

// Missing invariants section — returns no issues; the textual verifier
// flags "missing section" separately.
func TestCheckInvariantOwnership_NoSection(t *testing.T) {
	path := writeDomainMd(t, "## 1. Entities\n\n| A | B |\n")
	issues := CheckInvariantOwnership(path)
	if len(issues) != 0 {
		t.Fatalf("expected 0 issues for missing section, got %d", len(issues))
	}
}

// Integration: VerifyDocument wires CheckInvariantOwnership in when the
// target is named domain.md.
func TestVerifyDocument_InvokesDomainCheckers(t *testing.T) {
	path := writeDomainMd(t, `## 2. Invariants

| ID      | Statement | Owner       |
|---------|-----------|-------------|
| INV-001 | X         | Arrangement |
| INV-002 | X         | Card        |
`)
	result, err := VerifyDocument(path, "", true)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if result.Summary.Critical < 1 {
		t.Fatalf("expected domain critical issue to be counted, got %+v", result.Summary)
	}
}

// Trial problem #40: r-003's INV-014 row carried a GFM-escaped pipe inside a
// code span.  Splitting on every pipe moved the statement's tail into the
// Owner column and raised a false invariant_multiple_owners critical.
func TestCheckInvariantOwnership_EscapedPipeInACell(t *testing.T) {
	path := writeDomainMd(t, "# Domain\n\n## 2. Invariants\n\n"+
		"| ID | Statement | Owner | Enforcement |\n"+
		"|----|-----------|-------|-------------|\n"+
		"| INV-014 | `<!subteam^S0BA1QR2YQZ\\|team-ai>`, a usergroup element or a direct mention counts as addressed | Rules | screen() |\n"+
		"| INV-015 | A bad group id is a ConfigError | Settings | from_env() |\n\n"+
		"## 3. State Partitioning\n")
	if issues := CheckInvariantOwnership(path); len(issues) != 0 {
		t.Fatalf("an escaped pipe must stay in its cell, got %d issues: %v", len(issues), issues)
	}
}

// Trial problem #63: an Owner cell with a parenthesised note split on the
// comma inside it and read as two owners.
func TestSplitOwners_KeepsNotesAndCodeSpansWhole(t *testing.T) {
	cases := map[string][]string{
		"Rules (screen, decide)":         {"Rules"},
		"`Work.Module/Domain` (rule)":    {"`Work.Module/Domain`"},
		"Arrangement, Card":              {"Arrangement", "Card"},
		"WorkController / WorkItemQuery": {"WorkController", "WorkItemQuery"},
	}
	for in, want := range cases {
		got := splitOwners(in)
		if strings.Join(got, "|") != strings.Join(want, "|") {
			t.Errorf("splitOwners(%q) = %q, want %q", in, got, want)
		}
	}
}
