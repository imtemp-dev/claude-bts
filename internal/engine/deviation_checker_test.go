package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeDeviation(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "deviation.md")
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

// Well-formed deviation.md → no issues.
func TestCheckDeviationSchema_Valid(t *testing.T) {
	body := `# Deviation Report

## Not Implemented
| ID | Item | File | Driver | Severity | Reason |
|----|------|------|--------|----------|--------|
| D-001 | x | src/a.ts | code-diff | major | missing |

## Spec Additions
| ID | Item | File | Driver | Severity | Description |
|----|------|------|--------|----------|-------------|
| D-002 | helper | src/b.ts | code-diff | minor | added during build |

## Deviations
| ID | Item | Spec Says | Code Has | Driver | Severity | Resolution |
|----|------|-----------|----------|--------|----------|------------|
| D-003 | y | a | b | code-diff,review:MAJ-005 | major | updated spec |
`
	path := writeDeviation(t, body)
	issues := CheckDeviationSchema(path)
	if len(issues) != 0 {
		t.Fatalf("expected no issues, got %v", issues)
	}
}

func TestCheckDeviationSchema_MissingDriver(t *testing.T) {
	body := `## Deviations
| ID | Item | Spec Says | Code Has | Driver | Severity | Resolution |
|----|------|-----------|----------|--------|----------|------------|
| D-001 | y | a | b |  | major | pending |
`
	issues := CheckDeviationSchema(writeDeviation(t, body))
	var driver *Issue
	for i := range issues {
		if strings.Contains(issues[i].Claim, "driver_missing") {
			driver = &issues[i]
		}
	}
	if driver == nil {
		t.Fatalf("expected driver_missing, got %v", issues)
	}
	if driver.Severity != "critical" {
		t.Errorf("want critical, got %s", driver.Severity)
	}
}

func TestCheckDeviationSchema_InvalidDriver(t *testing.T) {
	body := `## Deviations
| ID | Item | Spec Says | Code Has | Driver | Severity | Resolution |
|----|------|-----------|----------|--------|----------|------------|
| D-001 | y | a | b | manual | major | pending |
`
	issues := CheckDeviationSchema(writeDeviation(t, body))
	var inv *Issue
	for i := range issues {
		if strings.Contains(issues[i].Claim, "driver_invalid") {
			inv = &issues[i]
		}
	}
	if inv == nil {
		t.Fatalf("expected driver_invalid, got %v", issues)
	}
}

func TestCheckDeviationSchema_DuplicateID(t *testing.T) {
	body := `## Not Implemented
| ID | Item | File | Driver | Severity | Reason |
|----|------|------|--------|----------|--------|
| D-001 | x | src/a | code-diff | major | missing |

## Deviations
| ID | Item | Spec Says | Code Has | Driver | Severity | Resolution |
|----|------|-----------|----------|--------|----------|------------|
| D-001 | y | a | b | code-diff | major | conflict |
`
	issues := CheckDeviationSchema(writeDeviation(t, body))
	var dup *Issue
	for i := range issues {
		if strings.Contains(issues[i].Claim, "id_duplicate") {
			dup = &issues[i]
		}
	}
	if dup == nil {
		t.Fatalf("expected id_duplicate, got %v", issues)
	}
}

func TestCheckDeviationSchema_MissingID(t *testing.T) {
	body := `## Deviations
| ID | Item | Spec Says | Code Has | Driver | Severity | Resolution |
|----|------|-----------|----------|--------|----------|------------|
|  | y | a | b | code-diff | major | pending |
`
	issues := CheckDeviationSchema(writeDeviation(t, body))
	if len(issues) == 0 || !strings.Contains(issues[0].Claim, "id_missing") {
		t.Fatalf("expected id_missing, got %v", issues)
	}
	if issues[0].Severity != "critical" {
		t.Errorf("want critical, got %s", issues[0].Severity)
	}
}

func TestCheckDeviationSchema_InvalidSeverity(t *testing.T) {
	body := `## Deviations
| ID | Item | Spec Says | Code Has | Driver | Severity | Resolution |
|----|------|-----------|----------|--------|----------|------------|
| D-001 | y | a | b | code-diff | fatal | pending |
`
	issues := CheckDeviationSchema(writeDeviation(t, body))
	var sev *Issue
	for i := range issues {
		if strings.Contains(issues[i].Claim, "severity_invalid") {
			sev = &issues[i]
		}
	}
	if sev == nil {
		t.Fatalf("expected severity_invalid, got %v", issues)
	}
}

// Placeholder rows (dashes) and missing sections should not register
// phantom findings.
func TestCheckDeviationSchema_PlaceholderRowsIgnored(t *testing.T) {
	body := `## Not Implemented
| ID | Item | File | Driver | Severity | Reason |
|----|------|------|--------|----------|--------|
| — | — | — | — | — | — |
`
	issues := CheckDeviationSchema(writeDeviation(t, body))
	if len(issues) != 0 {
		t.Fatalf("placeholder row should not surface findings, got %v", issues)
	}
}

func TestCheckDeviationSchema_FileMissingIsNil(t *testing.T) {
	issues := CheckDeviationSchema("/nope/does/not/exist.md")
	if len(issues) != 0 {
		t.Errorf("missing file should be silent, got %v", issues)
	}
}

// The prove stage feeds sync: a fixed interrogate finding and a driven
// journey are drivers, as simulate:<id> was before simulation was removed
// (which stays accepted so old deviation.md files still validate).
func TestCheckDeviationSchema_ProveStageDrivers(t *testing.T) {
	body := `# Deviation Report

## Deviations
| ID | Item | Spec Says | Code Has | Driver | Severity | Resolution |
|----|------|-----------|----------|--------|----------|------------|
| D-001 | link parse | any tail | resolved path | interrogate:F-001 | major | updated spec |
| D-002 | boot | a | b | prove:dry-run-boot | minor | updated spec |
| D-003 | old row | a | b | simulate:S04 | minor | legacy |
`
	if issues := CheckDeviationSchema(writeDeviation(t, body)); len(issues) != 0 {
		t.Fatalf("interrogate:, prove: and the legacy simulate: drivers must validate, got %v", issues)
	}
}
