package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFinalAndTasks(t *testing.T, finalBody, tasksBody string) (finalPath, tasksPath string) {
	t.Helper()
	dir := t.TempDir()
	finalPath = filepath.Join(dir, "final.md")
	tasksPath = filepath.Join(dir, "tasks.json")
	if err := os.WriteFile(finalPath, []byte(finalBody), 0644); err != nil {
		t.Fatalf("write final: %v", err)
	}
	if err := os.WriteFile(tasksPath, []byte(tasksBody), 0644); err != nil {
		t.Fatalf("write tasks: %v", err)
	}
	return finalPath, tasksPath
}

// wireframePathFor names the sibling wireframe of a recipe fixture. Most
// tests here anchor in final.md, so the file does not exist and the
// wireframe source contributes nothing — which is the legacy-recipe case
// the union has to keep working.
func wireframePathFor(finalPath string) string {
	return filepath.Join(filepath.Dir(finalPath), "wireframe.md")
}

// Happy path: anchors in final.md 1:1 match Task.Anchor in tasks.json.
func TestCheckTaskAnchors_OneToOne(t *testing.T) {
	finalMd := `## Components

<!-- task-anchor: src/auth/oauth.ts create -->
### src/auth/oauth.ts

<!-- task-anchor: src/app.ts modify -->
### src/app.ts
`
	tasksJson := `{
  "recipe_id": "r-1",
  "tasks": [
    {"id": "t-001", "file": "src/auth/oauth.ts", "action": "create", "status": "done", "description": "x", "anchor": "src/auth/oauth.ts create"},
    {"id": "t-002", "file": "src/app.ts", "action": "modify", "status": "done", "description": "y", "anchor": "src/app.ts modify"}
  ]
}`
	finalPath, tasksPath := writeFinalAndTasks(t, finalMd, tasksJson)
	issues := CheckTaskAnchors(finalPath, wireframePathFor(finalPath), tasksPath)
	if len(issues) != 0 {
		t.Fatalf("expected 0 issues, got %d: %v", len(issues), issues)
	}
}

// tasks.json has a task with no matching anchor in final.md → critical.
func TestCheckTaskAnchors_MissingAnchor(t *testing.T) {
	finalMd := `<!-- task-anchor: src/a.ts create -->`
	tasksJson := `{
  "recipe_id": "r-1",
  "tasks": [
    {"id": "t-001", "file": "src/a.ts", "action": "create", "status": "done", "description": "x", "anchor": "src/a.ts create"},
    {"id": "t-002", "file": "src/b.ts", "action": "create", "status": "done", "description": "y", "anchor": "src/b.ts create"}
  ]
}`
	finalPath, tasksPath := writeFinalAndTasks(t, finalMd, tasksJson)
	issues := CheckTaskAnchors(finalPath, wireframePathFor(finalPath), tasksPath)
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d: %v", len(issues), issues)
	}
	if issues[0].Severity != "critical" {
		t.Errorf("want critical, got %s", issues[0].Severity)
	}
	if !strings.Contains(issues[0].Claim, "missing_anchor: src/b.ts create") {
		t.Errorf("wrong claim: %s", issues[0].Claim)
	}
}

// final.md has an anchor with no matching task → critical orphan.
func TestCheckTaskAnchors_OrphanAnchor(t *testing.T) {
	finalMd := `<!-- task-anchor: src/a.ts create -->
<!-- task-anchor: src/ghost.ts create -->`
	tasksJson := `{
  "recipe_id": "r-1",
  "tasks": [
    {"id": "t-001", "file": "src/a.ts", "action": "create", "status": "done", "description": "x", "anchor": "src/a.ts create"}
  ]
}`
	finalPath, tasksPath := writeFinalAndTasks(t, finalMd, tasksJson)
	issues := CheckTaskAnchors(finalPath, wireframePathFor(finalPath), tasksPath)
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(issues))
	}
	if !strings.Contains(issues[0].Claim, "orphan_anchor: src/ghost.ts create") {
		t.Errorf("wrong claim: %s", issues[0].Claim)
	}
}

func TestCheckTaskAnchors_DuplicateAnchor(t *testing.T) {
	finalMd := `<!-- task-anchor: src/a.ts create -->
<!-- task-anchor: src/a.ts create -->`
	tasksJson := `{
  "recipe_id": "r-1",
  "tasks": [
    {"id": "t-001", "file": "src/a.ts", "action": "create", "status": "done", "description": "x", "anchor": "src/a.ts create"}
  ]
}`
	finalPath, tasksPath := writeFinalAndTasks(t, finalMd, tasksJson)
	issues := CheckTaskAnchors(finalPath, wireframePathFor(finalPath), tasksPath)
	if len(issues) != 1 {
		t.Fatalf("expected 1 duplicate, got %d", len(issues))
	}
	if issues[0].Severity != "major" || !strings.Contains(issues[0].Claim, "duplicate_anchor") {
		t.Errorf("unexpected: %+v", issues[0])
	}
}

func TestCheckTaskAnchors_ActionMismatch(t *testing.T) {
	finalMd := `<!-- task-anchor: src/a.ts modify -->`
	tasksJson := `{
  "recipe_id": "r-1",
  "tasks": [
    {"id": "t-001", "file": "src/a.ts", "action": "modify", "status": "done", "description": "x", "anchor": "src/a.ts create"}
  ]
}`
	finalPath, tasksPath := writeFinalAndTasks(t, finalMd, tasksJson)
	issues := CheckTaskAnchors(finalPath, wireframePathFor(finalPath), tasksPath)

	// Expect: action_mismatch (anchor claims create but task is modify) AND
	// missing_anchor (anchor "src/a.ts create" doesn't exist in final.md).
	// Validator reports both so the user sees the whole picture.
	var gotMismatch, gotMissing bool
	for _, i := range issues {
		if strings.Contains(i.Claim, "action_mismatch") {
			gotMismatch = true
		}
		if strings.Contains(i.Claim, "missing_anchor") {
			gotMissing = true
		}
	}
	if !gotMismatch || !gotMissing {
		t.Fatalf("want mismatch+missing, got %+v", issues)
	}
}

// Legacy recipes have Task.Anchor empty. The checker falls back to
// File+Action so migration can happen lazily.
func TestCheckTaskAnchors_LegacyAnchorEmpty(t *testing.T) {
	finalMd := `<!-- task-anchor: src/a.ts create -->`
	tasksJson := `{
  "recipe_id": "r-1",
  "tasks": [
    {"id": "t-001", "file": "src/a.ts", "action": "create", "status": "done", "description": "x"}
  ]
}`
	finalPath, tasksPath := writeFinalAndTasks(t, finalMd, tasksJson)
	issues := CheckTaskAnchors(finalPath, wireframePathFor(finalPath), tasksPath)
	if len(issues) != 0 {
		t.Fatalf("legacy empty-anchor task should match via File+Action, got %v", issues)
	}
}

// Empty or malformed anchor grammar should be ignored by the parser
// rather than silently matching something surprising.
func TestCheckTaskAnchors_MalformedAnchorIgnored(t *testing.T) {
	finalMd := `<!-- task-anchor -->
<!-- task-anchor: just-a-path -->
<!-- task-anchor: src/ok.ts create -->`
	tasksJson := `{
  "recipe_id": "r-1",
  "tasks": [
    {"id": "t-001", "file": "src/ok.ts", "action": "create", "status": "done", "description": "x", "anchor": "src/ok.ts create"}
  ]
}`
	finalPath, tasksPath := writeFinalAndTasks(t, finalMd, tasksJson)
	issues := CheckTaskAnchors(finalPath, wireframePathFor(finalPath), tasksPath)
	if len(issues) != 0 {
		t.Fatalf("malformed anchors should be ignored, got %v", issues)
	}
}

func writeSiblingWireframe(t *testing.T, finalPath, body string) {
	t.Helper()
	if err := os.WriteFile(wireframePathFor(finalPath), []byte(body), 0644); err != nil {
		t.Fatalf("write wireframe: %v", err)
	}
}

// The wireframe's File Structure table is a task-anchor source, so a
// blueprint no longer needs a per-file section for every unit the
// implementation touches — the requirement that turned blueprints into
// transcriptions.
func TestCheckTaskAnchors_WireframeTableIsASource(t *testing.T) {
	tasksJson := `{
  "recipe_id": "r-1",
  "tasks": [
    {"id": "t-001", "file": "backend/db/0026_cover_url.sql", "action": "create", "status": "pending", "description": "x", "anchor": "backend/db/0026_cover_url.sql create"},
    {"id": "t-002", "file": "ios/App/CoverView.swift", "action": "modify", "status": "pending", "description": "y", "anchor": "ios/App/CoverView.swift modify"}
  ]
}`
	// final.md carries no anchors at all — the whole point.
	finalPath, tasksPath := writeFinalAndTasks(t, "# Blueprint\n\nNo per-file sections here.\n", tasksJson)
	writeSiblingWireframe(t, finalPath, `# Wireframe

## Step 4: File Structure

| # | File | Action | Depends On | Responsibility |
|---|------|--------|------------|----------------|
| 1 | `+"`backend/db/0026_cover_url.sql`"+` | create | — | declares the column shape |
| 2 | `+"`ios/App/CoverView.swift`"+` | **modify** | 1 | draws the cover |
| 3 | `+"`ios/project.yml`"+` | **unchanged** | — | globs the directory already |

## Step 5: Execution Path Enumeration
`)
	if issues := CheckTaskAnchors(finalPath, wireframePathFor(finalPath), tasksPath); len(issues) != 0 {
		t.Fatalf("expected 0 issues from wireframe-sourced anchors, got %d: %v", len(issues), issues)
	}
}

// An `unchanged` row records that a file needs no edit. It is not a task,
// so it must not read as an orphan anchor.
func TestCheckTaskAnchors_NonTaskActionsAreNotAnchors(t *testing.T) {
	tasksJson := `{"recipe_id": "r-1", "tasks": []}`
	finalPath, tasksPath := writeFinalAndTasks(t, "# Blueprint\n", tasksJson)
	writeSiblingWireframe(t, finalPath, "# Wireframe\n\n## File Structure\n\n"+
		"| File | Action | Responsibility |\n|---|---|---|\n"+
		"| `ios/project.yml` | **unchanged** | globs already |\n")
	if issues := CheckTaskAnchors(finalPath, wireframePathFor(finalPath), tasksPath); len(issues) != 0 {
		t.Fatalf("an unchanged row must not be an anchor, got %v", issues)
	}
}

func TestParseWireframeFileTable(t *testing.T) {
	// A leading `#` column shifts every positional read by one, which is
	// why columns are located by header name.
	body := "## 4. File Structure\n\n" +
		"| # | File | Action | Depends On |\n|---|---|---|---|\n" +
		"| 1 | `a/b_c.ts` | modify | — |\n" +
		"| 2 | `d/e.sql` | create | 1 |\n\n" +
		"## 5. Execution Paths\n\n" +
		"| File | Action |\n|---|---|\n| `not/a/unit.go` | create |\n"
	got := ParseWireframeFileTable(body)
	if len(got) != 2 {
		t.Fatalf("want 2 anchors, got %d: %v", len(got), got)
	}
	// Underscores are the path, not markdown italics.
	if got[0].Path != "a/b_c.ts" || got[0].Action != "modify" {
		t.Errorf("first anchor = %v, want a/b_c.ts modify", got[0])
	}
	// The scan stops at the next section, so a later table is not read
	// as this one's continuation.
	for _, k := range got {
		if k.Path == "not/a/unit.go" {
			t.Error("a table in a later section was read as File Structure")
		}
	}
	if got := ParseWireframeFileTable("# Wireframe\n\nno such section\n"); got != nil {
		t.Errorf("a wireframe without the section must declare nothing, got %v", got)
	}
}

// A recipe anchored in final.md keeps working untouched — the promise
// CheckTaskAnchors makes in place of a migration. Every wireframe
// written under the old contract has a File Structure table, and it is
// routinely a superset of tasks.json: files later dropped, rows spelled
// with a different relative path. Enforcing those rows as orphans would
// open one critical per extra row on the next `bts verify`.
func TestCheckTaskAnchors_WireframeRowsAreNotOrphansWhenFinalAnchors(t *testing.T) {
	finalMd := `<!-- task-anchor: src/a.ts create -->`
	tasksJson := `{
  "recipe_id": "r-1",
  "tasks": [
    {"id": "t-001", "file": "src/a.ts", "action": "create", "status": "done", "description": "x", "anchor": "src/a.ts create"}
  ]
}`
	finalPath, tasksPath := writeFinalAndTasks(t, finalMd, tasksJson)
	writeSiblingWireframe(t, finalPath, "# Wireframe\n\n## File Structure\n\n"+
		"| File | Action |\n|---|---|\n"+
		"| `src/a.ts` | create |\n"+
		"| `src/dropped.ts` | create |\n"+
		"| `pkg/types.go` | modify |\n")
	if issues := CheckTaskAnchors(finalPath, wireframePathFor(finalPath), tasksPath); len(issues) != 0 {
		t.Fatalf("a final.md-anchored recipe must not be judged against wireframe rows, got %d: %v", len(issues), issues)
	}
}

// With no final.md anchor the table IS the decomposition, so the reverse
// direction is enforced against it.
func TestCheckTaskAnchors_WireframeRowsAreOrphansWhenSoleSource(t *testing.T) {
	tasksJson := `{"recipe_id": "r-1", "tasks": []}`
	finalPath, tasksPath := writeFinalAndTasks(t, "# Blueprint\n", tasksJson)
	writeSiblingWireframe(t, finalPath, "# Wireframe\n\n## File Structure\n\n"+
		"| File | Action |\n|---|---|\n| `src/a.ts` | create |\n")
	issues := CheckTaskAnchors(finalPath, wireframePathFor(finalPath), tasksPath)
	if len(issues) != 1 || !strings.HasPrefix(issues[0].Claim, "orphan_anchor") {
		t.Fatalf("want 1 orphan_anchor, got %v", issues)
	}
}

// The section bound has to come from the heading that matched. A
// hardcoded `^#{1,2}` scanned past the siblings of a `### File
// Structure`, and since the column indices are already locked to this
// table's header, the next table's rows were read as anchor rows.
func TestParseWireframeFileTable_SubsectionStopsAtItsSibling(t *testing.T) {
	body := "## Step 4\n\n### File Structure\n\n" +
		"| File | Action |\n|---|---|\n| `a/b.ts` | create |\n\n" +
		"### Rollback plan\n\n" +
		"| File | Action |\n|---|---|\n| `a/b.ts` | delete |\n| `legacy/old.ts` | modify |\n"
	got := ParseWireframeFileTable(body)
	if len(got) != 1 || got[0].Path != "a/b.ts" || got[0].Action != "create" {
		t.Fatalf("want only the File Structure row, got %v", got)
	}
}

// The anchor check used to run only when final.md existed, so a
// wireframe-anchored recipe — the shape this change exists to enable —
// skipped 1:1 validation entirely, while engine/stats.go went on
// counting its orphans. The two disagreed about the same tree.
func TestValidateTasksJSON_RunsForWireframeOnlyRecipe(t *testing.T) {
	dir := t.TempDir()
	tasksPath := filepath.Join(dir, "tasks.json")
	if err := os.WriteFile(tasksPath, []byte(`{
  "recipe_id": "r-1",
  "tasks": [
    {"id": "t-001", "file": "src/nowhere.ts", "action": "create", "status": "pending", "description": "x", "anchor": "src/nowhere.ts create"}
  ]
}`), 0644); err != nil {
		t.Fatalf("write tasks: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "wireframe.md"),
		[]byte("# Wireframe\n\n## File Structure\n\n| File | Action |\n|---|---|\n| `src/a.ts` | create |\n"),
		0644); err != nil {
		t.Fatalf("write wireframe: %v", err)
	}

	var found bool
	for _, e := range validateTasksJSON(tasksPath) {
		if strings.Contains(e.Message, "missing_anchor") {
			found = true
		}
	}
	if !found {
		t.Error("a wireframe-anchored recipe with no final.md must still be checked against its table")
	}

	// A loose tasks.json with neither document is still skipped, so test
	// fixtures that are not recipes keep validating.
	loose := filepath.Join(t.TempDir(), "tasks.json")
	if err := os.WriteFile(loose, []byte(`{"recipe_id": "r-2", "tasks": []}`), 0644); err != nil {
		t.Fatalf("write loose tasks: %v", err)
	}
	for _, e := range validateTasksJSON(loose) {
		if strings.Contains(e.File, "final.md") {
			t.Errorf("a standalone tasks.json must not be anchor-checked, got %v", e)
		}
	}
}

// cleanCell trimmed underscores from both edges, which renames the file:
// `__init__.py` became `init__.py` and `_layout.tsx` became
// `layout.tsx`. Each produced a CRITICAL missing_anchor against a
// tasks.json that spells the name correctly, and — on a recipe whose
// anchors come from the table — a CRITICAL orphan_anchor for the
// corrupted key beside it. Italics wrap the WHOLE cell; a leading
// underscore in a name does not.
func TestParseWireframeFileTable_KeepsNameUnderscores(t *testing.T) {
	got := ParseWireframeFileTable("# W\n\n## File Structure\n\n" +
		"| File | Action |\n|---|---|\n" +
		"| `pkg/__init__.py` | create |\n" +
		"| `app/_layout.tsx` | _modify_ |\n")
	want := []TaskAnchorKey{
		{Path: "pkg/__init__.py", Action: "create"},
		{Path: "app/_layout.tsx", Action: "modify"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d = %v, want %v", i, got[i], want[i])
		}
	}
}

// task_anchor_orphan_rate divides the orphan and missing counts by
// TaskAnchorTotal. Counting the final∪wireframe union there while the
// orphan direction enforces final.md alone put rows in the denominator
// that can never reach the numerator: the measured recipe's 31 rows
// against 12 enforced anchors improved the indicator threefold with
// nothing in the project having changed.
func TestTaskAnchorPopulationCountsWhatIsJudged(t *testing.T) {
	finalMd := `<!-- task-anchor: src/a.ts create -->`
	tasksJSON := `{
  "recipe_id": "r-1",
  "tasks": [
    {"id": "t-001", "file": "src/a.ts", "action": "create", "status": "done", "description": "x", "anchor": "src/a.ts create"}
  ]
}`
	finalPath, tasksPath := writeFinalAndTasks(t, finalMd, tasksJSON)
	writeSiblingWireframe(t, finalPath, "# Wireframe\n\n## File Structure\n\n"+
		"| File | Action |\n|---|---|\n"+
		"| `src/a.ts` | create |\n"+
		"| `src/dropped.ts` | create |\n"+
		"| `pkg/types.go` | modify |\n")
	if got := TaskAnchorPopulation(finalPath, wireframePathFor(finalPath), tasksPath); got != 1 {
		t.Errorf("population = %d, want 1 — unenforced wireframe rows are not in the denominator", got)
	}

	// With the table as the only source its rows ARE judged, so they
	// belong in the denominator.
	finalPath2, tasksPath2 := writeFinalAndTasks(t, "# Blueprint\n", tasksJSON)
	writeSiblingWireframe(t, finalPath2, "# Wireframe\n\n## File Structure\n\n"+
		"| File | Action |\n|---|---|\n"+
		"| `src/a.ts` | create |\n"+
		"| `src/b.ts` | create |\n")
	if got := TaskAnchorPopulation(finalPath2, wireframePathFor(finalPath2), tasksPath2); got != 2 {
		t.Errorf("population = %d, want 2 when the table is the enforced source", got)
	}
}
