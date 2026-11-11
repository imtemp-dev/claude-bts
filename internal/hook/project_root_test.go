package hook

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

// TestMain clears CLAUDE_PROJECT_DIR, so no test resolves a real project
// from the environment it happens to run in; the tests that need it set
// it themselves.
func TestMain(m *testing.M) {
	os.Unsetenv(ProjectEnv)
	os.Exit(m.Run())
}

// Every hook found its project from the payload's cwd, which follows the
// session's `cd`.  On true-shark the session worked in its scratchpad, so
// 63 hook runs resolved no project and did nothing, among them the Stop
// that carried <bts>IMPLEMENT DONE</bts> (trial problem #85, Multica
// AGEN-69).  Claude Code sets CLAUDE_PROJECT_DIR in every hook's
// environment to the project the session started in; the hooks act on it.
func TestStop_ACwdOutsideTheProjectStillCompletes(t *testing.T) {
	root, id := setupImplementRoot(t)
	setOwner(t, root, id, ownerSID)
	t.Setenv(ProjectEnv, root)
	scratch := t.TempDir() // the session's cwd, outside the project

	out, err := NewStopHandler().Handle(&HookInput{CWD: scratch, SessionID: ownerSID, LastAssistantMessage: "All green.\n<bts>IMPLEMENT DONE</bts>"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Decision == "block" {
		t.Fatalf("the marker must complete, not block: %s", out.Reason)
	}
	if r, _ := state.LoadRecipeState(root, id); r.Phase == "implement" {
		t.Fatal("the hook must act on the session's project although the cwd is outside it")
	}
}

func TestProjectRoot_TheSessionsProjectBeforeTheCwd(t *testing.T) {
	mk := func() string {
		d := t.TempDir()
		if err := os.MkdirAll(filepath.Join(d, ".bts"), 0755); err != nil {
			t.Fatal(err)
		}
		return d
	}
	project, other, plain := mk(), mk(), t.TempDir()
	sub := filepath.Join(project, "agent_server")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}

	for _, c := range []struct {
		name, env, cwd, want string
	}{
		{"cwd outside every project", project, plain, project},
		{"cwd in another bts project", project, other, project},
		{"cwd below the project", project, sub, project},
		{"no CLAUDE_PROJECT_DIR (a payload piped by hand)", "", sub, project},
		{"CLAUDE_PROJECT_DIR without bts", plain, sub, project},
	} {
		t.Setenv(ProjectEnv, c.env)
		got, err := (&HookInput{CWD: c.cwd}).ProjectRoot()
		if err != nil || got != c.want {
			t.Errorf("%s: root = %q, %v; want %q", c.name, got, err, c.want)
		}
	}
	t.Setenv(ProjectEnv, "")
	if _, err := (&HookInput{CWD: plain}).ProjectRoot(); err == nil {
		t.Error("no project anywhere must be an error, as before")
	}
}

// A Multica run starts Claude in the task's workdir; the repository worktree
// holding .bts/ is below it, and the workdir's .bts-root pointer names it.
// The hooks' CLAUDE_PROJECT_DIR is the workdir, so they must follow the
// pointer, whether the session has cd'd into the worktree or not.
func TestProjectRoot_FollowsTheSessionRootPointer(t *testing.T) {
	workdir := t.TempDir()
	repo := filepath.Join(workdir, "Repo")
	if err := os.MkdirAll(filepath.Join(repo, ".bts"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workdir, state.PointerFile), []byte("Repo\n"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(ProjectEnv, workdir)
	for _, cwd := range []string{workdir, repo} {
		got, err := (&HookInput{CWD: cwd}).ProjectRoot()
		if err != nil || got != repo {
			t.Errorf("cwd %s: root = %q, %v; want %q", cwd, got, err, repo)
		}
	}
}
