package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imtemp-dev/jig/internal/state"
)

// setupPerTask creates a project root + recipe dir with optional
// wireframe/domain/source files, and returns both paths so tests can
// drive CheckTaskStructure directly.
func setupPerTask(t *testing.T, wireframe, domain, sourceFile, sourceBody string) (projectRoot, recipeDir string) {
	t.Helper()
	projectRoot = t.TempDir()
	recipeDir = filepath.Join(projectRoot, ".jig", "specs", "recipes", "r-001")
	if err := os.MkdirAll(recipeDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if wireframe != "" {
		_ = os.WriteFile(filepath.Join(recipeDir, "wireframe.md"), []byte(wireframe), 0644)
	}
	if domain != "" {
		_ = os.WriteFile(filepath.Join(recipeDir, "domain.md"), []byte(domain), 0644)
	}
	if sourceFile != "" {
		full := filepath.Join(projectRoot, sourceFile)
		_ = os.MkdirAll(filepath.Dir(full), 0755)
		_ = os.WriteFile(full, []byte(sourceBody), 0644)
	}
	return projectRoot, recipeDir
}

// Import inside wireframe's allowed neighbors → no findings.
func TestCheckTaskStructure_AllowedImport(t *testing.T) {
	wireframe := "```mermaid\nflowchart TD\n    A[\"src/auth/oauth.ts\\n(authenticates)\"]\n    B[\"src/types/user.ts\\n(definitions)\"]\n```\n"
	source := `import { User } from "src/types/user";
export function validate() {}
`
	root, recipeDir := setupPerTask(t, wireframe, "", "src/auth/oauth.ts", source)
	task := &state.Task{ID: "t-001", File: "src/auth/oauth.ts", Action: "create"}
	findings := CheckTaskStructure(root, recipeDir, task)
	for _, f := range findings {
		if f.Category == "import_drift" {
			t.Errorf("allowed import should not drift: %+v", f)
		}
	}
}

// Import NOT in wireframe allowed set → import_drift major.
func TestCheckTaskStructure_ImportDrift(t *testing.T) {
	wireframe := "```mermaid\nflowchart TD\n    A[\"src/auth/oauth.ts\\n(authenticates)\"]\n```\n"
	source := `import { Thing } from "src/unauthorized/module";
export function x() {}
`
	root, recipeDir := setupPerTask(t, wireframe, "", "src/auth/oauth.ts", source)
	task := &state.Task{ID: "t-001", File: "src/auth/oauth.ts", Action: "create"}
	findings := CheckTaskStructure(root, recipeDir, task)
	var drift *state.StructureFinding
	for i := range findings {
		if findings[i].Category == "import_drift" {
			drift = &findings[i]
		}
	}
	if drift == nil {
		t.Fatalf("expected import_drift finding, got %+v", findings)
	}
	if drift.Severity != "major" {
		t.Errorf("want major, got %s", drift.Severity)
	}
	if !strings.Contains(drift.Detail, "src/unauthorized/module") {
		t.Errorf("detail should cite the drifted import, got %s", drift.Detail)
	}
}

// Modify task with scope symbol missing from the file → symbol_missing critical.
func TestCheckTaskStructure_SymbolMissing(t *testing.T) {
	source := `function stillHere() {}
`
	root, recipeDir := setupPerTask(t, "", "", "src/a.ts", source)
	task := &state.Task{
		ID:          "t-002",
		File:        "src/a.ts",
		Action:      "modify",
		ModifyScope: []string{"stillHere", "wasDeletedOrRenamed"},
	}
	findings := CheckTaskStructure(root, recipeDir, task)
	var critical *state.StructureFinding
	for i := range findings {
		if findings[i].Category == "symbol_missing" {
			critical = &findings[i]
		}
	}
	if critical == nil {
		t.Fatalf("expected symbol_missing, got %+v", findings)
	}
	if critical.Severity != "critical" {
		t.Errorf("want critical, got %s", critical.Severity)
	}
	if !strings.Contains(critical.Detail, "wasDeletedOrRenamed") {
		t.Errorf("detail should name the missing symbol: %s", critical.Detail)
	}
}

// Legacy scope skips the symbol check.
func TestCheckTaskStructure_LegacyScopeSkipped(t *testing.T) {
	source := `function anything() {}`
	root, recipeDir := setupPerTask(t, "", "", "src/a.ts", source)
	task := &state.Task{
		ID:          "t-002",
		File:        "src/a.ts",
		Action:      "modify",
		ModifyScope: []string{"legacy"},
	}
	findings := CheckTaskStructure(root, recipeDir, task)
	for _, f := range findings {
		if f.Category == "symbol_missing" {
			t.Errorf("legacy scope should skip symbol check, got %+v", f)
		}
	}
}

// Owner drift — domain.md names a module as invariant owner, and the
// module's file no longer references that identifier.
func TestCheckTaskStructure_OwnerDrift(t *testing.T) {
	domain := `## 2. Invariants

| ID | Statement | Owner | Enforcement point |
|----|-----------|-------|-------------------|
| INV-001 | stored order is canonical | Arrangement | Arrangement.place() |
`
	// File is Arrangement.ts but contains no "Arrangement" identifier.
	source := `function place() {}
function unrelated() {}
`
	root, recipeDir := setupPerTask(t, "", domain, "src/Arrangement.ts", source)
	task := &state.Task{ID: "t-003", File: "src/Arrangement.ts", Action: "modify", ModifyScope: []string{"place"}}
	findings := CheckTaskStructure(root, recipeDir, task)
	var drift *state.StructureFinding
	for i := range findings {
		if findings[i].Category == "owner_drift" {
			drift = &findings[i]
		}
	}
	if drift == nil {
		t.Fatalf("expected owner_drift, got %+v", findings)
	}
	if drift.Severity != "critical" {
		t.Errorf("want critical, got %s", drift.Severity)
	}
}

// Missing file → no findings (task hasn't written code yet).
func TestCheckTaskStructure_MissingFileNoop(t *testing.T) {
	root, recipeDir := setupPerTask(t, "", "", "", "")
	task := &state.Task{ID: "t-004", File: "src/nonexistent.ts", Action: "create"}
	findings := CheckTaskStructure(root, recipeDir, task)
	if len(findings) != 0 {
		t.Errorf("missing file should produce no findings, got %+v", findings)
	}
}

// Nil task → nil findings.
func TestCheckTaskStructure_NilTask(t *testing.T) {
	findings := CheckTaskStructure("/", "/", nil)
	if findings != nil {
		t.Errorf("nil task must produce nil findings, got %+v", findings)
	}
}
