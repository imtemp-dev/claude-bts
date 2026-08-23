package engine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeConsistencyPair lays down verification.md + verify-log.jsonl in
// a tmp dir and returns both paths so the cross-check can run directly.
// All tests share this helper so the fixture shape stays uniform.
func writeConsistencyPair(t *testing.T, verificationBody string, logEntries []map[string]interface{}) (string, string) {
	t.Helper()
	dir := t.TempDir()
	vPath := filepath.Join(dir, "verification.md")
	lPath := filepath.Join(dir, "verify-log.jsonl")
	if err := os.WriteFile(vPath, []byte(verificationBody), 0644); err != nil {
		t.Fatalf("write verification.md: %v", err)
	}
	f, err := os.Create(lPath)
	if err != nil {
		t.Fatalf("create log: %v", err)
	}
	enc := json.NewEncoder(f)
	for i := range logEntries {
		if err := enc.Encode(logEntries[i]); err != nil {
			t.Fatalf("encode: %v", err)
		}
	}
	_ = f.Close()
	return vPath, lPath
}

func findingsBody(counts map[string]interface{}) string {
	data, _ := json.Marshal(counts)
	return "# Report\n\n<jig-findings>\n" + string(data) + "\n</jig-findings>\n"
}

// Counts match → no errors.
func TestValidateVerificationLogConsistency_Match(t *testing.T) {
	vPath, lPath := writeConsistencyPair(t,
		findingsBody(map[string]interface{}{
			"critical":         0,
			"major":            0,
			"minor_resolvable": 2,
			"minor_deferred":   1,
		}),
		[]map[string]interface{}{
			{"iteration": 1, "critical": 1, "major": 2, "status": "continue"},
			{"iteration": 3, "critical": 0, "major": 0, "minor_resolvable": 2, "minor_deferred": 1, "status": "converged"},
		},
	)
	errs := validateVerificationLogConsistency(vPath, lPath)
	if len(errs) != 0 {
		t.Fatalf("expected 0 errors, got %v", errs)
	}
}

// verification.md claims critical=1 but log says critical=0 → 1 error.
func TestValidateVerificationLogConsistency_CriticalMismatch(t *testing.T) {
	vPath, lPath := writeConsistencyPair(t,
		findingsBody(map[string]interface{}{
			"critical":         1,
			"major":            0,
			"minor_resolvable": 0,
			"minor_deferred":   0,
		}),
		[]map[string]interface{}{
			{"iteration": 3, "critical": 0, "major": 0, "status": "converged"},
		},
	)
	errs := validateVerificationLogConsistency(vPath, lPath)
	if len(errs) != 1 {
		t.Fatalf("want 1 error, got %d: %v", len(errs), errs)
	}
	if !strings.Contains(errs[0].Field, "critical") {
		t.Errorf("field should cite critical, got %s", errs[0].Field)
	}
	if !strings.Contains(errs[0].Message, "says critical=1") ||
		!strings.Contains(errs[0].Message, "says critical=0") {
		t.Errorf("message should show both sides: %s", errs[0].Message)
	}
}

// Two mismatches → two errors, one per field.
func TestValidateVerificationLogConsistency_MultipleMismatches(t *testing.T) {
	vPath, lPath := writeConsistencyPair(t,
		findingsBody(map[string]interface{}{
			"critical":         1,
			"major":            2,
			"minor_resolvable": 0,
			"minor_deferred":   0,
		}),
		[]map[string]interface{}{
			{"iteration": 2, "critical": 0, "major": 0, "status": "converged"},
		},
	)
	errs := validateVerificationLogConsistency(vPath, lPath)
	if len(errs) != 2 {
		t.Fatalf("want 2 errors, got %d: %v", len(errs), errs)
	}
}

// Migrate-seeded blocks (source key) still flag mismatches but with a
// `[migrate-seeded]` label so operators know the origin.
func TestValidateVerificationLogConsistency_MigrateSeeded_Labeled(t *testing.T) {
	vPath, lPath := writeConsistencyPair(t,
		findingsBody(map[string]interface{}{
			"critical":         1, // drift
			"major":            0,
			"minor_resolvable": 0,
			"minor_deferred":   0,
			"source":           "migrated-from-verify-log",
		}),
		[]map[string]interface{}{
			{"iteration": 1, "critical": 0, "major": 0, "status": "converged"},
		},
	)
	errs := validateVerificationLogConsistency(vPath, lPath)
	if len(errs) != 1 {
		t.Fatalf("want 1 error, got %d", len(errs))
	}
	if !strings.Contains(errs[0].Message, "[migrate-seeded]") {
		t.Errorf("message should carry [migrate-seeded] label, got %s", errs[0].Message)
	}
}

// Legacy log entries have only `minor` (pre-Phase-2.4 split). When the
// verification.md block reports `minor_resolvable=N`, the cross-check
// should accept N==log.minor via the effectiveResolvable fallback.
func TestValidateVerificationLogConsistency_LegacyMinorFallback(t *testing.T) {
	vPath, lPath := writeConsistencyPair(t,
		findingsBody(map[string]interface{}{
			"critical":         0,
			"major":            0,
			"minor_resolvable": 3,
			"minor_deferred":   0,
		}),
		[]map[string]interface{}{
			// Legacy entry — only `minor`, no split fields.
			{"iteration": 1, "critical": 0, "major": 0, "minor": 3, "status": "continue"},
		},
	)
	errs := validateVerificationLogConsistency(vPath, lPath)
	if len(errs) != 0 {
		t.Fatalf("legacy minor=3 should map to resolvable=3, got %v", errs)
	}
}

// Missing verify-log → vacuous. No errors.
func TestValidateVerificationLogConsistency_MissingLog_Vacuous(t *testing.T) {
	dir := t.TempDir()
	vPath := filepath.Join(dir, "verification.md")
	_ = os.WriteFile(vPath, []byte(findingsBody(map[string]interface{}{
		"critical": 0, "major": 0, "minor_resolvable": 0, "minor_deferred": 0,
	})), 0644)

	errs := validateVerificationLogConsistency(vPath, filepath.Join(dir, "nonexistent.jsonl"))
	if len(errs) != 0 {
		t.Errorf("missing log → no errors, got %v", errs)
	}
}

// Missing <jig-findings> block → we skip the cross-check (the other
// validator already emits the missing-block error).
func TestValidateVerificationLogConsistency_MissingBlock_Skipped(t *testing.T) {
	dir := t.TempDir()
	vPath := filepath.Join(dir, "verification.md")
	_ = os.WriteFile(vPath, []byte("# No findings block here\n"), 0644)
	lPath := filepath.Join(dir, "verify-log.jsonl")
	_ = os.WriteFile(lPath, []byte(`{"iteration":1,"critical":5,"major":0,"status":"continue"}`+"\n"), 0644)

	errs := validateVerificationLogConsistency(vPath, lPath)
	if len(errs) != 0 {
		t.Errorf("missing block → skipped, got %v", errs)
	}
}
