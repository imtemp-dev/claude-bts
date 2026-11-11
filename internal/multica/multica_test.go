package multica

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

func writeContext(t *testing.T, workdir, body string) {
	t.Helper()
	p := filepath.Join(workdir, filepath.FromSlash(ContextFile))
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestFindTaskContextFromWorktree(t *testing.T) {
	workdir := t.TempDir()
	writeContext(t, workdir, `{"managed_by":"multica","agent_id":"a-1","issue_id":"i-1"}`)
	repo := filepath.Join(workdir, "Repo")
	if err := os.MkdirAll(repo, 0755); err != nil {
		t.Fatal(err)
	}

	tc := FindTaskContext(repo)
	if tc == nil || tc.IssueID != "i-1" || tc.AgentID != "a-1" || tc.Dir != workdir {
		t.Fatalf("FindTaskContext(worktree) = %+v; want issue i-1 in %s", tc, workdir)
	}
	if tc := FindTaskContext(workdir); tc == nil || tc.IssueID != "i-1" {
		t.Fatalf("FindTaskContext(workdir) = %+v", tc)
	}
}

func TestFindTaskContextOutsideMulticaAndChat(t *testing.T) {
	if tc := FindTaskContext(t.TempDir()); tc != nil {
		t.Fatalf("outside Multica: got %+v", tc)
	}
	chat := t.TempDir()
	writeContext(t, chat, `{"managed_by":"multica","agent_id":"a-1","chat_session_id":"c-1"}`)
	tc := FindTaskContext(chat)
	if tc == nil || tc.IssueID != "" || tc.ChatSessionID != "c-1" {
		t.Fatalf("a chat task: got %+v", tc)
	}
	bad := t.TempDir()
	writeContext(t, bad, `{not json`)
	if tc := FindTaskContext(bad); tc != nil {
		t.Fatalf("a malformed context file: got %+v", tc)
	}
}

func newRecipe(t *testing.T, issue string) (root, id string) {
	t.Helper()
	root = t.TempDir()
	id = "r-001-smoke"
	if err := os.MkdirAll(filepath.Join(state.SpecsPath(root), "recipes", id), 0755); err != nil {
		t.Fatal(err)
	}
	if err := state.SaveRecipeState(root, &state.RecipeState{ID: id, Type: "fix", Phase: "research", MulticaIssue: issue}); err != nil {
		t.Fatal(err)
	}
	return root, id
}

func TestOnPhaseChangeWritesMetadata(t *testing.T) {
	var got [][]string
	restore := SetCLIRunner(func(_ context.Context, args ...string) error {
		got = append(got, args)
		return nil
	})
	defer restore()

	root, id := newRecipe(t, "i-1")
	OnPhaseChange(root, id, "research", "draft")
	want := [][]string{
		{"issue", "metadata", "set", "i-1", "--key", "bts.recipe_id", "--value", id},
		{"issue", "metadata", "set", "i-1", "--key", "bts.phase", "--value", "draft"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("CLI calls = %v\nwant %v", got, want)
	}
}

func TestOnPhaseChangeOutsideMulticaAndOnFailure(t *testing.T) {
	calls := 0
	restore := SetCLIRunner(func(context.Context, ...string) error {
		calls++
		return errors.New("server down")
	})
	defer restore()

	root, id := newRecipe(t, "")
	OnPhaseChange(root, id, "research", "draft")
	if calls != 0 {
		t.Fatalf("a recipe with no Multica issue must not call the CLI, got %d calls", calls)
	}

	root, id = newRecipe(t, "i-1")
	OnPhaseChange(root, id, "research", "draft") // must not panic or fail
	if calls != 1 {
		t.Fatalf("after the first failure the rest is skipped: got %d calls, want 1", calls)
	}
}
