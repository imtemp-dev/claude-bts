package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeArchitectWireframe(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "wireframe.md")
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

func TestCheckArchitectDecisionHeader_Valid(t *testing.T) {
	path := writeArchitectWireframe(t, `<!-- architect-decision -->
Selected: arrangement-centric
Basis: custom: keeps word-order truth in one module
Rationale: owns word order as a single source of truth.
Rejected:
  - card-centric: duplicates placement across cards
<!-- /architect-decision -->

## Step 1: Components
…`)
	issues := CheckArchitectDecisionHeader(path)
	if len(issues) != 0 {
		t.Fatalf("expected 0 issues, got %d: %v", len(issues), issues)
	}
}

func TestCheckArchitectDecisionHeader_Missing(t *testing.T) {
	path := writeArchitectWireframe(t, "# Wireframe\n\n(no block here)\n")
	issues := CheckArchitectDecisionHeader(path)
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(issues))
	}
	if !strings.Contains(issues[0].Claim, "missing_architect_decision_block") {
		t.Errorf("wrong claim: %s", issues[0].Claim)
	}
	if issues[0].Severity != "major" {
		t.Errorf("want major, got %s", issues[0].Severity)
	}
}

func TestCheckArchitectDecisionHeader_MissingSelected(t *testing.T) {
	path := writeArchitectWireframe(t, `<!-- architect-decision -->
Basis: custom: placeholder
Rationale: no Selected line present
Rejected: none
<!-- /architect-decision -->`)
	issues := CheckArchitectDecisionHeader(path)
	if len(issues) != 1 {
		t.Fatalf("want 1 issue, got %d: %v", len(issues), issues)
	}
	if !strings.Contains(issues[0].Claim, "missing_selected") {
		t.Errorf("wrong claim: %s", issues[0].Claim)
	}
}

func TestCheckArchitectDecisionHeader_MissingBasis(t *testing.T) {
	path := writeArchitectWireframe(t, `<!-- architect-decision -->
Selected: pipeline
Rationale: fits the flow.
<!-- /architect-decision -->`)
	issues := CheckArchitectDecisionHeader(path)
	if len(issues) != 1 || !strings.Contains(issues[0].Claim, "missing_basis") {
		t.Fatalf("want 1 missing_basis issue, got %v", issues)
	}
	if issues[0].Severity != "major" {
		t.Errorf("want major, got %s", issues[0].Severity)
	}
}

func TestCheckArchitectDecisionHeader_OfficialNeedsURL(t *testing.T) {
	unsourced := writeArchitectWireframe(t, `<!-- architect-decision -->
Selected: udf-layers
Basis: official: Android app architecture
<!-- /architect-decision -->`)
	issues := CheckArchitectDecisionHeader(unsourced)
	if len(issues) != 1 || !strings.Contains(issues[0].Claim, "official_unsourced") {
		t.Fatalf("want official_unsourced, got %v", issues)
	}

	sourced := writeArchitectWireframe(t, `<!-- architect-decision -->
Selected: udf-layers
Basis: official: Android app architecture for compose@1 (Source: https://developer.android.com/topic/architecture)
<!-- /architect-decision -->`)
	if issues := CheckArchitectDecisionHeader(sourced); len(issues) != 0 {
		t.Fatalf("sourced official should pass, got %v", issues)
	}
}

func TestCheckArchitectDecisionHeader_BasisFormat(t *testing.T) {
	path := writeArchitectWireframe(t, `<!-- architect-decision -->
Selected: pipeline
Basis: because it seemed reasonable
<!-- /architect-decision -->`)
	issues := CheckArchitectDecisionHeader(path)
	if len(issues) != 1 || !strings.Contains(issues[0].Claim, "basis_format") {
		t.Fatalf("want basis_format, got %v", issues)
	}
}

func TestCheckArchitectDecisionHeader_DuplicateBlocks(t *testing.T) {
	path := writeArchitectWireframe(t, `<!-- architect-decision -->
Selected: first
<!-- /architect-decision -->

<!-- architect-decision -->
Selected: second
<!-- /architect-decision -->`)
	issues := CheckArchitectDecisionHeader(path)
	if len(issues) != 1 {
		t.Fatalf("want 1 issue for duplicate, got %d", len(issues))
	}
	if !strings.Contains(issues[0].Claim, "duplicate") {
		t.Errorf("wrong claim: %s", issues[0].Claim)
	}
}

// Integration: VerifyDocument on wireframe.md should surface this.
func TestVerifyDocument_ChecksArchitectDecision(t *testing.T) {
	path := writeArchitectWireframe(t, "# empty wireframe")
	result, err := VerifyDocument(path, "")
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if result.Summary.Major < 1 {
		t.Fatalf("expected major for missing architect-decision; got %+v", result.Summary)
	}
}

// --- Invariant coverage cross-check (Phase 5.3c) ---

func writeArchitectPair(t *testing.T, wireframe, domain string) string {
	t.Helper()
	dir := t.TempDir()
	wf := filepath.Join(dir, "wireframe.md")
	if err := os.WriteFile(wf, []byte(wireframe), 0644); err != nil {
		t.Fatalf("write wireframe: %v", err)
	}
	if domain != "" {
		if err := os.WriteFile(filepath.Join(dir, "domain.md"), []byte(domain), 0644); err != nil {
			t.Fatalf("write domain: %v", err)
		}
	}
	return wf
}

const testDomainTwoInvariants = `# Domain

## 2. Invariants

| ID | Statement | Owner |
|---|---|---|
| INV-001 | Word order has one source of truth | arrangement |
| INV-002 | Cards never overlap | layout |
`

func TestInvariantCoverage_FullMappingPasses(t *testing.T) {
	wf := writeArchitectPair(t, `<!-- architect-decision -->
Selected: arrangement-centric
Basis: custom: single truth module
Rejected:
  - card-centric: duplicates placement
Invariant ownership:
  - INV-001: arrangement
  - INV-002: layout
<!-- /architect-decision -->`, testDomainTwoInvariants)
	if issues := CheckArchitectInvariantCoverage(wf); len(issues) != 0 {
		t.Fatalf("expected 0 issues, got %v", issues)
	}
}

func TestInvariantCoverage_UnmappedInvariant(t *testing.T) {
	wf := writeArchitectPair(t, `<!-- architect-decision -->
Selected: arrangement-centric
Basis: custom: single truth module
Invariant ownership:
  - INV-001: arrangement
<!-- /architect-decision -->`, testDomainTwoInvariants)
	issues := CheckArchitectInvariantCoverage(wf)
	if len(issues) != 1 || !strings.Contains(issues[0].Claim, "invariant_unmapped") {
		t.Fatalf("want invariant_unmapped, got %v", issues)
	}
	if !strings.Contains(issues[0].Detail, "INV-002") {
		t.Errorf("detail should name INV-002: %s", issues[0].Detail)
	}
	if issues[0].Severity != "major" {
		t.Errorf("want major, got %s", issues[0].Severity)
	}
}

func TestInvariantCoverage_UnknownMappedID(t *testing.T) {
	wf := writeArchitectPair(t, `<!-- architect-decision -->
Selected: arrangement-centric
Basis: custom: single truth module
Invariant ownership:
  - INV-001: arrangement
  - INV-002: layout
  - INV-999: ghost
<!-- /architect-decision -->`, testDomainTwoInvariants)
	issues := CheckArchitectInvariantCoverage(wf)
	if len(issues) != 1 || !strings.Contains(issues[0].Claim, "invariant_unknown") {
		t.Fatalf("want invariant_unknown, got %v", issues)
	}
	if issues[0].Severity != "minor" {
		t.Errorf("want minor, got %s", issues[0].Severity)
	}
}

func TestInvariantCoverage_MissingOwnershipSection(t *testing.T) {
	wf := writeArchitectPair(t, `<!-- architect-decision -->
Selected: arrangement-centric
Basis: custom: single truth module
Rejected:
  - card-centric: duplicates placement
<!-- /architect-decision -->`, testDomainTwoInvariants)
	issues := CheckArchitectInvariantCoverage(wf)
	if len(issues) != 1 || !strings.Contains(issues[0].Claim, "missing_invariant_ownership") {
		t.Fatalf("want missing_invariant_ownership, got %v", issues)
	}
}

func TestInvariantCoverage_RejectedListNotMiscounted(t *testing.T) {
	// Rejected entries share the "- name: reason" shape; they must not
	// be parsed as ownership entries even when ownership comes first.
	wf := writeArchitectPair(t, `<!-- architect-decision -->
Selected: arrangement-centric
Basis: custom: single truth module
Invariant ownership:
  - INV-001: arrangement
  - INV-002: layout
Rationale: ownership section ends at the non-entry line above.
Rejected:
  - card-centric: duplicates placement
<!-- /architect-decision -->`, testDomainTwoInvariants)
	if issues := CheckArchitectInvariantCoverage(wf); len(issues) != 0 {
		t.Fatalf("rejected-list entries leaked into ownership parsing: %v", issues)
	}
}

func TestInvariantCoverage_NoDomainFileSkips(t *testing.T) {
	wf := writeArchitectPair(t, `<!-- architect-decision -->
Selected: pipeline
Basis: custom: no domain contract yet
<!-- /architect-decision -->`, "")
	if issues := CheckArchitectInvariantCoverage(wf); issues != nil {
		t.Fatalf("expected nil without domain.md, got %v", issues)
	}
}
