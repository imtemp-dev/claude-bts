package engine

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

// Trial problems #137 and #133: a modify scope declares the names a task
// removes as -name, and __doc__ stands for the module docstring.

const stagingBefore = `"""Report staging."""


def make_rows(rows):
    return [r for r in rows]


def stage_report_data(rows):
    return make_rows(rows)


class StagedReport:
    pass


def expand_recurring(rows):
    return rows
`

func findingsOf(fs []state.StructureFinding, category string) []string {
	var out []string
	for _, f := range fs {
		if f.Category == category {
			out = append(out, f.Severity+": "+f.Detail)
		}
	}
	return out
}

// #137: removing the declared names, and a public helper only they used,
// is the task done, not a missing symbol or a change outside the scope.
func TestCheckTaskStructure_DeclaredRemovalsPass(t *testing.T) {
	root, recipeDir := scopeRepo(t, "reports/staging.py", stagingBefore)
	task := &state.Task{ID: "t-004", File: "reports/staging.py", Action: "modify",
		ModifyScope: []string{"expand_recurring", "-stage_report_data", "-StagedReport"}}
	after := `"""Report staging."""


def expand_recurring(rows):
    return [r for r in rows if r]
`
	writeScopeFile(t, root, "reports/staging.py", after)
	fs := CheckTaskStructure(root, recipeDir, task)
	if got := findingsOf(fs, "symbol_missing"); len(got) != 0 {
		t.Fatalf("a declared removal is not missing: %v", got)
	}
	if got := scopeViolations(t, fs); len(got) != 0 {
		t.Fatalf("make_rows served only removed code, so its removal is in scope: %v", got)
	}
	if got := findingsOf(fs, "removal_pending"); len(got) != 0 {
		t.Fatalf("nothing is pending: %v", got)
	}

	// Another file still names the helper: its removal is outside the scope.
	writeScopeFile(t, root, "reports/build.py", "from reports.staging import make_rows\n")
	gitAdd(t, root, "reports/build.py")
	if got := scopeViolations(t, CheckTaskStructure(root, recipeDir, task)); strings.Join(got, ",") != "make_rows" {
		t.Fatalf("a helper another file imports is not the task's to remove: %v", got)
	}
}

func gitAdd(t *testing.T, root, rel string) {
	t.Helper()
	if out, err := exec.Command("git", "-C", root, "add", rel).CombinedOutput(); err != nil {
		t.Fatalf("git add: %v %s", err, out)
	}
}

// A declared removal still in the file is reported, as a major.
func TestCheckTaskStructure_RemovalPendingIsReported(t *testing.T) {
	root, recipeDir := scopeRepo(t, "reports/staging.py", stagingBefore)
	task := &state.Task{ID: "t-004", File: "reports/staging.py", Action: "modify",
		ModifyScope: []string{"expand_recurring", "-StagedReport"}}
	writeScopeFile(t, root, "reports/staging.py", strings.Replace(stagingBefore, "return rows", "return rows[:]", 1)+"\n# StagedReport went away in t-004\n")
	got := findingsOf(CheckTaskStructure(root, recipeDir, task), "removal_pending")
	if len(got) != 1 || !strings.HasPrefix(got[0], "major: ") || !strings.Contains(got[0], "'StagedReport'") {
		t.Fatalf("one major for the class still declared: %v", got)
	}
}

// A mention in a comment is not a declaration: the removal is done.
func TestCheckTaskStructure_RemovalNamedInACommentIsDone(t *testing.T) {
	root, recipeDir := scopeRepo(t, "reports/staging.py", stagingBefore)
	task := &state.Task{ID: "t-004", File: "reports/staging.py", Action: "modify",
		ModifyScope: []string{"-StagedReport"}}
	after := strings.Replace(stagingBefore, "class StagedReport:\n    pass\n", "# StagedReport moved to reports/model.py\n", 1)
	writeScopeFile(t, root, "reports/staging.py", after)
	if got := findingsOf(CheckTaskStructure(root, recipeDir, task), "removal_pending"); len(got) != 0 {
		t.Fatalf("a comment does not declare the class: %v", got)
	}
}

// #133: a docstring-only change, scoped __doc__.
func TestCheckTaskStructure_DocScope(t *testing.T) {
	root, recipeDir := scopeRepo(t, "reports/__init__.py", "\"\"\"Reports.\"\"\"\n\nVERSION = 1\n")
	task := &state.Task{ID: "t-007", File: "reports/__init__.py", Action: "modify", ModifyScope: []string{"__doc__"}}
	writeScopeFile(t, root, "reports/__init__.py", "\"\"\"Reports: staging, rendering and delivery.\"\"\"\n\nVERSION = 1\n")
	fs := CheckTaskStructure(root, recipeDir, task)
	if got := findingsOf(fs, "symbol_missing"); len(got) != 0 {
		t.Fatalf("__doc__ is the module docstring, not a symbol to find: %v", got)
	}
	if got := scopeViolations(t, fs); len(got) != 0 {
		t.Fatalf("a docstring edit changes no symbol: %v", got)
	}
}

// The IMPLEMENT DONE gate reads the same scope: declared removals and
// __doc__ are not missing symbols.
func TestCheckModifyScope_RemovalsAndDocAreNotMissing(t *testing.T) {
	finalMd := "<!-- task-anchor: reports/staging.py modify scope=-stage_report_data,expand_recurring -->\n" +
		"<!-- task-anchor: reports/__init__.py modify scope=__doc__ -->\n"
	tasksJSON := `{"tasks": [
  {"id": "t-004", "file": "reports/staging.py", "action": "modify", "status": "done", "description": "x",
   "anchor": "reports/staging.py modify", "modify_scope": ["-stage_report_data", "expand_recurring"]},
  {"id": "t-007", "file": "reports/__init__.py", "action": "modify", "status": "done", "description": "x",
   "anchor": "reports/__init__.py modify", "modify_scope": ["__doc__"]}
]}`
	finalPath, tasksPath, root := setupScope(t, finalMd, tasksJSON, "reports/staging.py", "def expand_recurring(rows):\n    return rows\n")
	writeScopeFile(t, root, "reports/__init__.py", "\"\"\"Reports.\"\"\"\n")
	for _, is := range CheckModifyScope(finalPath, tasksPath, root) {
		t.Errorf("no issue expected: %s — %s", is.Claim, is.Detail)
	}
}

// A scope may open with a removal.
func TestScopeSuffixReadsALeadingRemoval(t *testing.T) {
	m := scopeSuffixRe.FindStringSubmatch("modify scope=-StagedReport,expand_recurring")
	if len(m) < 2 || strings.Join(splitScopeList(m[1]), "|") != "-StagedReport|expand_recurring" {
		t.Fatalf("parsed %q", m)
	}
}
