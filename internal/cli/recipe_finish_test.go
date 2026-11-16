package cli

import (
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

// finishStdout runs fn with stdout read into a string.
func finishStdout(t *testing.T, fn func()) string {
	t.Helper()
	saved := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	done := make(chan string)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	fn()
	_ = w.Close()
	os.Stdout = saved
	return <-done
}

// Trial problem #155: the command exits non-zero when the gate refuses, and
// reads the session from CLAUDE_CODE_SESSION_ID.
func TestRecipeFinishCommand(t *testing.T) {
	root, id := newTestRunFixture(t) // a blueprint in phase test, with no tasks.json
	t.Chdir(root)
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	if err := recipeFinishCmd.RunE(recipeFinishCmd, []string{id}); !errors.Is(err, errFinishBlocked) {
		t.Fatalf("a refused gate exits non-zero: %v", err)
	}

	r, _ := state.LoadRecipeState(root, id)
	r.OwnerSession = "aaaaaaaa-1111-4111-8111-111111111111"
	if err := state.SaveRecipeState(root, r); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_CODE_SESSION_ID", "bbbbbbbb-2222-4222-8222-222222222222")
	out := finishStdout(t, func() {
		if err := recipeFinishCmd.RunE(recipeFinishCmd, []string{id}); !errors.Is(err, errFinishBlocked) {
			t.Fatalf("another session's finish is refused: %v", err)
		}
	})
	if !strings.Contains(out, "does not own") {
		t.Fatalf("the refusal names the owner rule:\n%s", out)
	}
	if after, _ := state.LoadRecipeState(root, id); after.Phase != "test" {
		t.Fatalf("nothing changed: phase %s", after.Phase)
	}
}
