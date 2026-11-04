package engine

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFinal(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "final.md")
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

// No section → no entries, no unresolved. Uncertainty tracking is
// optional, so absence must pass silently.
func TestCheckKnownUncertainties_NoSection(t *testing.T) {
	path := writeFinal(t, "# Spec\n\nBody with no Known Uncertainties section.\n")
	all, unresolved, err := CheckKnownUncertainties(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(all) != 0 || len(unresolved) != 0 {
		t.Fatalf("expected no entries, got all=%v unresolved=%v", all, unresolved)
	}
}

func TestCheckKnownUncertainties_AllResolved(t *testing.T) {
	path := writeFinal(t, `# Spec

## Known Uncertainties

### U-001: scroll jitter on cold start
Why-deferred: only observable on real device.
Resolved: simulated via T-042 — no jitter reproduced.

### U-002: token refresh window
Why-deferred: depends on server clock.
Still-unknown: production monitoring required.
`)
	all, unresolved, err := CheckKnownUncertainties(path)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("want 2 entries, got %d", len(all))
	}
	if len(unresolved) != 0 {
		t.Fatalf("all entries carry a resolution marker, got unresolved=%v", unresolved)
	}
	if all[0].ID != "U-001" || all[0].Status != "resolved" {
		t.Errorf("U-001 status wrong: %+v", all[0])
	}
	if all[1].ID != "U-002" || all[1].Status != "still-unknown" {
		t.Errorf("U-002 status wrong: %+v", all[1])
	}
}

// The unresolved list is exactly the entries missing a marker.
func TestCheckKnownUncertainties_MixedUnresolved(t *testing.T) {
	path := writeFinal(t, `## Known Uncertainties

### U-001: resolved one
Resolved: handled.

### U-002: unresolved one
Why-deferred: lacks verification.

### U-003: diverged one
Diverged: actual behavior differs from spec assumption.

### U-004: another unresolved
No marker here either.
`)
	_, unresolved, err := CheckKnownUncertainties(path)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(unresolved) != 2 {
		t.Fatalf("want 2 unresolved, got %d: %+v", len(unresolved), unresolved)
	}
	ids := []string{unresolved[0].ID, unresolved[1].ID}
	if ids[0] != "U-002" || ids[1] != "U-004" {
		t.Errorf("unexpected unresolved ids: %v", ids)
	}
}

// Section is bounded by the next H2. Entries after the next `## `
// heading must not be parsed (they belong to a different section).
func TestCheckKnownUncertainties_StopsAtNextSection(t *testing.T) {
	path := writeFinal(t, `## Known Uncertainties

### U-001: inside
Resolved: yes.

## Appendix

### U-002: pretend this is not an uncertainty
`)
	all, _, err := CheckKnownUncertainties(path)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("expected 1 entry bounded by Appendix, got %d", len(all))
	}
	if all[0].ID != "U-001" {
		t.Errorf("want U-001, got %s", all[0].ID)
	}
}

// Case and colon variants must all match so authors aren't punished
// for stylistic choices.
func TestCheckKnownUncertainties_CaseAndColonVariants(t *testing.T) {
	path := writeFinal(t, `## KNOWN UNCERTAINTIES:

### U-001: upper-case with colon
resolved: lowercase marker — the check is case-insensitive.
`)
	_, unresolved, err := CheckKnownUncertainties(path)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(unresolved) != 0 {
		t.Fatalf("lowercase resolved should count, unresolved=%v", unresolved)
	}
}

// File missing → callers expect a clean nil,nil,nil (uncertainties
// are opt-in). Propagating an error would break recipes that don't
// use them.
func TestCheckKnownUncertainties_MissingFile(t *testing.T) {
	all, unresolved, err := CheckKnownUncertainties("/nope/does/not/exist.md")
	if err != nil {
		t.Fatalf("missing file should not error, got %v", err)
	}
	if len(all) != 0 || len(unresolved) != 0 {
		t.Errorf("missing file should return no entries")
	}
}

// An outcome that holds for part of an uncertainty is written with a
// qualifier, and a mixed outcome as one line per part. AGEN-35's r-002 was
// blocked at completion on "Resolved for reads (§ 3):", which the old
// pattern did not accept. When parts differ, the least settled one is the
// entry's status: still-unknown, then diverged, then resolved.
func TestCheckKnownUncertainties_QualifiedAndMixedOutcomes(t *testing.T) {
	path := writeFinal(t, `## Known Uncertainties

### U-001: qualifier with "for"
Resolved for reads (§ 3): issue search returns an object with issues.

### U-002: qualifier in parentheses
Diverged (writes): the CLI prints its notice on stderr.

### U-003: mixed outcome, one line per part
Resolved (reads): shapes recorded.
Still-unknown (writes): no observation yet.

### U-004: a sentence that only starts with the word
Resolved questions are listed elsewhere.
`)
	all, unresolved, err := CheckKnownUncertainties(path)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	want := map[string]string{"U-001": "resolved", "U-002": "diverged", "U-003": "still-unknown", "U-004": ""}
	for _, e := range all {
		if e.Status != want[e.ID] {
			t.Errorf("%s: status %q, want %q", e.ID, e.Status, want[e.ID])
		}
	}
	if len(unresolved) != 1 || unresolved[0].ID != "U-004" {
		t.Fatalf("only U-004 has no outcome line, unresolved=%v", unresolved)
	}
}
