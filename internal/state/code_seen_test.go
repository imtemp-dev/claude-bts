package state

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// Trial problem #52: an interrogate round records the code its adversaries
// copied, so a gate can tell which files changed since, not only that
// something did.
func TestCodeSeen_FilesSince(t *testing.T) {
	root := t.TempDir()
	thWrite(t, root, "src/a.py", "a1\n")
	thWrite(t, root, "src/b.py", "b1\n")
	seen, err := SeeFiles(root, []string{"src/a.py", "src/b.py", "src/later.py"})
	if err != nil {
		t.Fatal(err)
	}
	if seen.Files["src/later.py"] != seenAbsent {
		t.Fatalf("a listed file that does not exist yet is recorded absent, got %q", seen.Files["src/later.py"])
	}
	c, err := seen.Since(root, []string{"src/a.py", "src/b.py", "src/later.py"})
	if err != nil || len(c.Files()) != 0 {
		t.Fatalf("nothing changed, got %+v %v", c, err)
	}

	thWrite(t, root, "src/a.py", "a2\n")
	_ = os.Remove(filepath.Join(root, "src", "b.py"))
	thWrite(t, root, "src/later.py", "now here\n")
	thWrite(t, root, "src/new.py", "a file the tasks name only now\n")
	c, err = seen.Since(root, []string{"src/a.py", "src/b.py", "src/later.py", "src/new.py"})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"src/later.py", "src/new.py"}; !reflect.DeepEqual(c.Added, want) {
		t.Errorf("added = %v, want %v", c.Added, want)
	}
	if want := []string{"src/a.py", "src/b.py"}; !reflect.DeepEqual(c.Changed, want) {
		t.Errorf("changed = %v, want %v (an edit and a delete)", c.Changed, want)
	}
}

// A recipe that names no code files (a fix recipe) is read off the git
// tree: HEAD plus the files that differ from it. A commit between the
// copy and the check is not a change; an edit, a new file and a delete
// are.
func TestCodeSeen_TreeSince(t *testing.T) {
	root := thRepo(t)
	thWrite(t, root, "app/login.py", "v1\n")
	thWrite(t, root, "app/util.py", "u1\n")
	thWrite(t, root, "app/gone.py", "g1\n")
	thGit(t, root, "add", "-A")
	thGit(t, root, "commit", "-q", "-m", "base")
	// The fix under way when the adversaries copy the project.
	thWrite(t, root, "app/login.py", "v2 fixed\n")
	thWrite(t, root, "app/helper.py", "h1\n")
	thWrite(t, root, ".bts/specs/recipes/r-1/interrogate.json", "{}\n")

	seen, err := SeeTree(root)
	if err != nil || seen == nil || !seen.Tree || seen.Base == "" {
		t.Fatalf("a repository with a commit records HEAD and an overlay, got %+v %v", seen, err)
	}
	if _, ok := seen.Overlay[".bts/specs/recipes/r-1/interrogate.json"]; ok {
		t.Fatal("bts state is not code")
	}
	c, err := seen.Since(root, nil)
	if err != nil || len(c.Files()) != 0 {
		t.Fatalf("nothing changed since the copy, got %+v %v", c, err)
	}

	// Committing the fix as it was copied changes nothing.
	thGit(t, root, "add", "-A")
	thGit(t, root, "commit", "-q", "-m", "fix")
	if c, err := seen.Since(root, nil); err != nil || len(c.Files()) != 0 {
		t.Fatalf("a commit is not a code change, got %+v %v", c, err)
	}

	thWrite(t, root, "app/login.py", "v3 reworked\n")
	thWrite(t, root, "app/util.py", "u2\n")
	_ = os.Remove(filepath.Join(root, "app", "gone.py"))
	thWrite(t, root, "app/brand_new.py", "n1\n")
	c, err = seen.Since(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"app/brand_new.py"}; !reflect.DeepEqual(c.Added, want) {
		t.Errorf("added = %v, want %v", c.Added, want)
	}
	if want := []string{"app/gone.py", "app/login.py", "app/util.py"}; !reflect.DeepEqual(c.Changed, want) {
		t.Errorf("changed = %v, want %v", c.Changed, want)
	}
}

// Outside a git repository a recipe with no code files has no record, as
// it has no fingerprint (TreeHash).
func TestCodeSeen_TreeOutsideGit(t *testing.T) {
	root := t.TempDir()
	thWrite(t, root, "main.go", "package main\n")
	if seen, err := SeeTree(root); err != nil || seen != nil {
		t.Fatalf("outside git want no record, got %+v %v", seen, err)
	}
}
