package engine

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

func driftRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	driftGit(t, root, "init", "-q", "-b", "main")
	driftGit(t, root, "config", "user.email", "t@example.com")
	driftGit(t, root, "config", "user.name", "t")
	return root
}

func driftGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func driftWrite(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
}

func driftCommit(t *testing.T, root, msg string) string {
	t.Helper()
	driftGit(t, root, "add", "-A")
	driftGit(t, root, "commit", "-q", "-m", msg)
	return driftGit(t, root, "rev-parse", "HEAD")
}

// refined is what InterrogateDrift returns for a record, without a recipe.
func refined(t *testing.T, root string, seen *state.CodeSeen, files []string) state.CodeChange {
	t.Helper()
	raw, err := seen.Since(root, files)
	if err != nil {
		t.Fatal(err)
	}
	return refineDrift(root, seen, raw)
}

const validatePy = `"""Validate a bundle."""


def validate(bundle):
    errors = []
    if not bundle:
        errors.append("empty")
    return errors
`

// Trial problem #140: a fix round touched two files and changed only a
// comment in a third; the third is not a changed code file, and the code
// files carry the lines that changed.
func TestInterrogateDrift_CommentsDoNotCount(t *testing.T) {
	root := driftRepo(t)
	driftWrite(t, root, "reports/validate.py", validatePy)
	driftWrite(t, root, "reports/render.py", "def render(b):\n    return b\n")
	driftWrite(t, root, "reports/metrics.py", "class Data:\n    # Warnings the validator found.\n    warnings: list\n")
	driftCommit(t, root, "base")
	files := []string{"reports/validate.py", "reports/render.py", "reports/metrics.py"}
	seen, err := state.SeeFiles(root, files)
	if err != nil {
		t.Fatal(err)
	}
	if seen.Base == "" || len(seen.Blobs) != 3 {
		t.Fatalf("in git the record keeps HEAD and the content, got base %q blobs %v", seen.Base, seen.Blobs)
	}

	driftWrite(t, root, "reports/validate.py", strings.Replace(validatePy, `errors.append("empty")`, `errors.append("empty bundle")`, 1))
	driftWrite(t, root, "reports/render.py", "def render(b):\n    \"\"\"Render a bundle.\"\"\"\n    return b\n")
	driftWrite(t, root, "reports/metrics.py", "class Data:\n    # Warnings, in the order the\n    # validator found them.\n    warnings: list\n")
	c := refined(t, root, seen, files)
	if !reflect.DeepEqual(c.Changed, []string{"reports/validate.py"}) {
		t.Errorf("changed = %v, want only validate.py", c.Changed)
	}
	if want := []string{"reports/metrics.py", "reports/render.py"}; !reflect.DeepEqual(c.CommentOnly, want) {
		t.Errorf("comment only = %v, want %v", c.CommentOnly, want)
	}
	if got := c.Hunks["reports/validate.py"]; !reflect.DeepEqual(got, []state.LineRange{{Start: 7, End: 7}}) {
		t.Errorf("validate.py hunks = %v, want line 7", got)
	}
	if v := CheckInterrogateRescope(c, &state.InterrogateRun{Round: 1}, 2, "r-1"); v != (RescopeVerdict{}) {
		t.Errorf("one code file changed keeps the cheap path, got %+v", v)
	}
}

// Trial problem #139: after a rebase onto master, a file the recipe edits
// differs from what the adversaries saw by what master brought in; that is
// not the recipe's change.  The recipe's own later edit still counts, and
// its lines are the recipe's, not upstream's.
func TestInterrogateDrift_ARebaseIsNotTheRecipesChange(t *testing.T) {
	root := driftRepo(t)
	metrics := "class Data:\n    \"\"\"Report data.\"\"\"\n    rows: list\n"
	driftWrite(t, root, "reports/metrics.py", metrics)
	driftWrite(t, root, "reports/validate.py", validatePy)
	driftWrite(t, root, "reports/render.py", "def render(b):\n    return b\n")
	base := driftCommit(t, root, "base")
	driftGit(t, root, "update-ref", "refs/remotes/origin/main", base)

	// The recipe's branch: a docstring in metrics.py, code in validate.py.
	driftGit(t, root, "checkout", "-q", "-b", "feature")
	driftWrite(t, root, "reports/metrics.py", strings.Replace(metrics, "Report data.", "Report data, validated.", 1))
	driftWrite(t, root, "reports/validate.py", strings.Replace(validatePy, "    return errors\n", "    errors.sort()\n    return errors\n", 1))
	driftCommit(t, root, "recipe")
	files := []string{"reports/metrics.py", "reports/validate.py", "reports/render.py"}
	seen, err := state.SeeFiles(root, files)
	if err != nil {
		t.Fatal(err)
	}

	// Master merges another PR that rewrites metrics.py and render.py.
	driftGit(t, root, "checkout", "-q", "-b", "upstream", base)
	driftWrite(t, root, "reports/metrics.py", metrics+"\n\ndef other_status_count(rows):\n    return sum(1 for r in rows if r.other)\n")
	driftWrite(t, root, "reports/render.py", "def render(b, fmt='pdf'):\n    return b\n")
	up := driftCommit(t, root, "another PR")
	driftGit(t, root, "update-ref", "refs/remotes/origin/main", up)
	driftGit(t, root, "checkout", "-q", "feature")
	driftGit(t, root, "rebase", "-q", up)

	c := refined(t, root, seen, files)
	if len(c.Changed)+len(c.Added) != 0 {
		t.Fatalf("a rebase alone changes nothing of the recipe's, got %+v", c)
	}
	if want := []string{"reports/metrics.py", "reports/render.py"}; !reflect.DeepEqual(c.Upstream, want) {
		t.Errorf("upstream = %v, want %v", c.Upstream, want)
	}

	// The recipe then fixes validate.py again: that counts, at its lines.
	cur, _ := os.ReadFile(filepath.Join(root, "reports/validate.py"))
	driftWrite(t, root, "reports/validate.py", strings.Replace(string(cur), "errors.sort()", "errors.sort(key=str)", 1))
	c = refined(t, root, seen, files)
	if !reflect.DeepEqual(c.Changed, []string{"reports/validate.py"}) || !reflect.DeepEqual(c.Upstream, []string{"reports/metrics.py", "reports/render.py"}) {
		t.Fatalf("changed %v upstream %v, want validate.py changed and the rebased files aside", c.Changed, c.Upstream)
	}
	if got := c.Hunks["reports/validate.py"]; !reflect.DeepEqual(got, []state.LineRange{{Start: 8, End: 8}}) {
		t.Errorf("validate.py hunks = %v, want line 8", got)
	}

	// An edit on top of upstream's lines in a rebased file is the
	// recipe's, and only its lines are in scope.
	cur, _ = os.ReadFile(filepath.Join(root, "reports/metrics.py"))
	driftWrite(t, root, "reports/metrics.py", strings.Replace(string(cur), "if r.other", "if r.other and not r.hidden", 1))
	c = refined(t, root, seen, files)
	if !reflect.DeepEqual(c.Changed, []string{"reports/metrics.py", "reports/validate.py"}) {
		t.Fatalf("changed = %v, want metrics.py and validate.py", c.Changed)
	}
	if got := c.Hunks["reports/metrics.py"]; !reflect.DeepEqual(got, []state.LineRange{{Start: 7, End: 7}}) {
		t.Errorf("metrics.py hunks = %v, want line 7 only", got)
	}
}

// A recipe that names no code files is read off the tree: after a rebase
// the files master changed are upstream's, not the fix's.
func TestInterrogateDrift_TreeRebase(t *testing.T) {
	root := driftRepo(t)
	driftWrite(t, root, "app/login.py", "def login():\n    return 1\n")
	driftWrite(t, root, "app/other.py", "def other():\n    return 1\n")
	base := driftCommit(t, root, "base")
	driftGit(t, root, "update-ref", "refs/remotes/origin/main", base)
	driftGit(t, root, "checkout", "-q", "-b", "fix")
	driftWrite(t, root, "app/login.py", "def login():\n    return 2\n")
	seen, err := state.SeeTree(root)
	if err != nil || seen == nil {
		t.Fatalf("see tree: %v", err)
	}
	driftCommit(t, root, "the fix")

	driftGit(t, root, "checkout", "-q", "-b", "upstream", base)
	driftWrite(t, root, "app/other.py", "def other():\n    return 3\n")
	driftWrite(t, root, "app/added.py", "x = 1\n")
	up := driftCommit(t, root, "another PR")
	driftGit(t, root, "update-ref", "refs/remotes/origin/main", up)
	driftGit(t, root, "checkout", "-q", "fix")
	driftGit(t, root, "rebase", "-q", up)

	c := refined(t, root, seen, nil)
	if len(c.Changed)+len(c.Added) != 0 {
		t.Fatalf("the rebase brought upstream's files only, got %+v", c)
	}
	if want := []string{"app/added.py", "app/other.py"}; !reflect.DeepEqual(c.Upstream, want) {
		t.Errorf("upstream = %v, want %v", c.Upstream, want)
	}
}

// What bts cannot separate still counts: an upstream change that
// conflicts with the recipe's, and a record that kept no content.
func TestInterrogateDrift_WhatCannotBeSeparatedCounts(t *testing.T) {
	root := driftRepo(t)
	driftWrite(t, root, "m.py", "x = 1\n")
	base := driftCommit(t, root, "base")
	driftGit(t, root, "update-ref", "refs/remotes/origin/main", base)
	driftGit(t, root, "checkout", "-q", "-b", "feature")
	driftWrite(t, root, "m.py", "x = 2\n")
	driftCommit(t, root, "recipe")
	seen, err := state.SeeFiles(root, []string{"m.py"})
	if err != nil {
		t.Fatal(err)
	}
	legacy := &state.CodeSeen{Files: seen.Files}

	driftGit(t, root, "checkout", "-q", "-b", "upstream", base)
	driftWrite(t, root, "m.py", "x = 3\n")
	up := driftCommit(t, root, "conflicting")
	driftGit(t, root, "update-ref", "refs/remotes/origin/main", up)
	driftGit(t, root, "checkout", "-q", "feature")
	driftGit(t, root, "reset", "-q", "--hard", up) // as if rebased, resolved to upstream's
	driftWrite(t, root, "m.py", "x = 4\n")

	if c := refined(t, root, seen, []string{"m.py"}); !reflect.DeepEqual(c.Changed, []string{"m.py"}) {
		t.Errorf("a conflicting upstream change counts, got %+v", c)
	}
	if c := refined(t, root, legacy, []string{"m.py"}); !reflect.DeepEqual(c.Changed, []string{"m.py"}) || len(c.Hunks) != 0 {
		t.Errorf("a record without content is compared by fingerprint, got %+v", c)
	}
}
