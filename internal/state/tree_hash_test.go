package state

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func thRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	thGit(t, root, "init", "-q")
	thGit(t, root, "config", "user.email", "t@example.com")
	thGit(t, root, "config", "user.name", "t")
	return root
}

func thGit(t *testing.T, root string, args ...string) {
	t.Helper()
	if out, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
}

func thWrite(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
}

func thHash(t *testing.T, root string) string {
	t.Helper()
	h, err := TreeHash(root)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// Outside a git repository there is nothing to list: no fingerprint, the
// behaviour every recipe without tasks.json had before.
func TestTreeHash_NotARepository(t *testing.T) {
	root := t.TempDir()
	thWrite(t, root, "main.go", "package main\n")
	if h := thHash(t, root); h != "" {
		t.Fatalf("outside git want no fingerprint, got %q", h)
	}
}

// An edit, a new file and a delete each read as a change; staging,
// committing and touching do not, so the fingerprint names content only.
func TestTreeHash_ChangesWithContentNotWithGit(t *testing.T) {
	root := thRepo(t)
	thWrite(t, root, ".gitignore", "build/\n")
	thWrite(t, root, "src/app.py", "x = 1\n")
	thWrite(t, root, "tests/test_app.py", "def test(): pass\n")
	thGit(t, root, "add", "-A")
	thGit(t, root, "commit", "-qm", "base")
	base := thHash(t, root)
	if base == "" {
		t.Fatal("a repository with files must have a fingerprint")
	}

	thWrite(t, root, "src/app.py", "x = 2\n")
	edited := thHash(t, root)
	if edited == base {
		t.Fatal("an unstaged edit must change the fingerprint")
	}
	thGit(t, root, "add", "src/app.py")
	if h := thHash(t, root); h != edited {
		t.Fatal("staging the edit must not change the fingerprint")
	}
	thGit(t, root, "commit", "-qm", "edit")
	if h := thHash(t, root); h != edited {
		t.Fatal("committing the edit must not change the fingerprint")
	}
	later := time.Now().Add(time.Hour)
	_ = os.Chtimes(filepath.Join(root, "src", "app.py"), later, later)
	if h := thHash(t, root); h != edited {
		t.Fatal("touching a file must not change the fingerprint")
	}

	thWrite(t, root, "src/new.py", "y = 1\n")
	added := thHash(t, root)
	if added == edited {
		t.Fatal("a new untracked file must change the fingerprint")
	}
	thWrite(t, root, "tests/test_app.py", "def test(): assert True\n")
	if h := thHash(t, root); h == added {
		t.Fatal("a test edit must change the fingerprint: a fix recipe's tests are part of what ran")
	}
	_ = os.Remove(filepath.Join(root, "tests", "test_app.py"))
	if h := thHash(t, root); h == added {
		t.Fatal("deleting a tracked file must change the fingerprint")
	}
}

// bts state, the agent's skills, drive evidence and ignored output are not
// code: writing them must not make a recorded run stale.
func TestTreeHash_IgnoresStateEvidenceAndIgnoredFiles(t *testing.T) {
	root := thRepo(t)
	thWrite(t, root, ".gitignore", "build/\n")
	thWrite(t, root, "src/app.py", "x = 1\n")
	thWrite(t, root, ".claude/skills/verify-app/SKILL.md", "v1\n")
	thGit(t, root, "add", "-A")
	thGit(t, root, "commit", "-qm", "base")
	base := thHash(t, root)

	thWrite(t, root, ".bts/specs/recipes/r-001/test-results.json", "{}\n")
	thWrite(t, root, ".verify/login/summary.txt", "PASS\n")
	thWrite(t, root, ".claude/skills/verify-app/SKILL.md", "v2\n")
	thWrite(t, root, "build/app.bin", "binary\n")
	if h := thHash(t, root); h != base {
		t.Fatal("writes under .bts/, .verify/, .claude/ or an ignored path must not change the fingerprint")
	}
}

// An untracked repository inside the project is listed by git as a
// directory; it is skipped rather than failing the whole fingerprint.
func TestTreeHash_SkipsANestedRepository(t *testing.T) {
	root := thRepo(t)
	thWrite(t, root, "src/app.py", "x = 1\n")
	thWrite(t, root, "vendor/lib/a.txt", "a\n")
	thGit(t, filepath.Join(root, "vendor", "lib"), "init", "-q")
	if _, err := TreeHash(root); err != nil {
		t.Fatalf("a nested repository must not break the fingerprint: %v", err)
	}
}

// A fix recipe has no tasks.json, so RecipeImplHash falls back to the tree.
func TestRecipeImplHash_FallsBackToTheTree(t *testing.T) {
	root := thRepo(t)
	thWrite(t, root, "src/app.py", "x = 1\n")
	h, err := RecipeImplHash(root, "r-001-fix")
	if err != nil || h == "" || h != thHash(t, root) {
		t.Fatalf("a recipe with no tasks.json must use the tree fingerprint, got %q %v", h, err)
	}
}
