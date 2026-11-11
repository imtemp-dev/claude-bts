package state

import (
	"os"
	"path/filepath"
	"testing"
)

// A Multica run starts Claude in the task's workdir, one level above the
// worktree that holds .bts/; the workdir's .bts-root pointer is how a hook
// started there finds the project.
func TestFindRootFollowsPointer(t *testing.T) {
	session := t.TempDir()
	repo := filepath.Join(session, "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".bts"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(session, PointerFile), []byte("repo\n"), 0644); err != nil {
		t.Fatal(err)
	}

	got, err := FindRoot(session)
	if err != nil || got != repo {
		t.Fatalf("FindRoot(session) = %q, %v; want %q", got, err, repo)
	}

	sub := filepath.Join(session, "notes")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}
	if got, err := FindRoot(sub); err != nil || got != repo {
		t.Fatalf("a directory below the session root must reach the pointer: got %q, %v", got, err)
	}

	// Inside the worktree its own .bts/ is found first.
	inner := filepath.Join(repo, "src")
	if err := os.MkdirAll(inner, 0755); err != nil {
		t.Fatal(err)
	}
	if got, err := FindRoot(inner); err != nil || got != repo {
		t.Fatalf("FindRoot(inside worktree) = %q, %v; want %q", got, err, repo)
	}
}

func TestFindRootPointerAbsoluteAndDangling(t *testing.T) {
	session := t.TempDir()
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".bts"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(session, PointerFile), []byte(repo), 0644); err != nil {
		t.Fatal(err)
	}
	if got, err := FindRoot(session); err != nil || got != repo {
		t.Fatalf("an absolute pointer: got %q, %v; want %q", got, err, repo)
	}

	// A pointer to a directory with no .bts/ is ignored, not followed.
	dangling := t.TempDir()
	if err := os.WriteFile(filepath.Join(dangling, PointerFile), []byte("missing"), 0644); err != nil {
		t.Fatal(err)
	}
	if got, err := FindRoot(dangling); err == nil && got == filepath.Join(dangling, "missing") {
		t.Fatalf("a dangling pointer must not resolve, got %q", got)
	}
}

func TestLogPhaseChangeTellsObserver(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(SpecsPath(root), "recipes", "r-001-x"), 0755); err != nil {
		t.Fatal(err)
	}
	var calls [][3]string
	t.Cleanup(setPhaseObserversForTest(func(_, id, from, to string) { calls = append(calls, [3]string{id, from, to}) }))

	_ = LogPhaseChange(root, "r-001-x", "", "research")
	_ = LogPhaseChange(root, "r-001-x", "research", "research") // no transition
	_ = LogPhaseChange(root, "r-001-x", "research", "draft")

	want := [][3]string{{"r-001-x", "", "research"}, {"r-001-x", "research", "draft"}}
	if len(calls) != len(want) || calls[0] != want[0] || calls[1] != want[1] {
		t.Fatalf("observer calls = %v, want %v", calls, want)
	}
}
