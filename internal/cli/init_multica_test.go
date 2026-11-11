package cli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/multica"
	"github.com/imtemp-dev/claude-bts/internal/state"
	"github.com/spf13/cobra"
)

// newInitCmd is initCmd with fresh flags, so a test's flags do not leak.
func newInitCmd() *cobra.Command {
	c := &cobra.Command{Use: "init", RunE: runInit}
	c.Flags().Bool("force", false, "")
	c.Flags().Bool("exclude", false, "")
	c.Flags().String("session-root", "", "")
	c.Flags().Bool("multica", false, "")
	return c
}

func runInitWith(t *testing.T, args []string, flags map[string]string) error {
	t.Helper()
	c := newInitCmd()
	for k, v := range flags {
		if err := c.Flags().Set(k, v); err != nil {
			t.Fatal(err)
		}
	}
	return runInit(c, args)
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)
	out, err := exec.Command("git", full...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// multicaTask lays out a task the way the Multica daemon does: workdir/ with
// .multica/daemon_task_context.json, and a repository checked out below it.
func multicaTask(t *testing.T) (workdir, repo string) {
	t.Helper()
	workdir = t.TempDir()
	if err := os.MkdirAll(filepath.Join(workdir, ".multica"), 0755); err != nil {
		t.Fatal(err)
	}
	ctx := `{"managed_by":"multica","agent_id":"a-1","issue_id":"issue-1"}`
	if err := os.WriteFile(filepath.Join(workdir, filepath.FromSlash(multica.ContextFile)), []byte(ctx), 0644); err != nil {
		t.Fatal(err)
	}
	repo = filepath.Join(workdir, "Repo")
	if out, err := exec.Command("git", "init", "-q", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	// A tracked .gitignore and .mcp.json, as Carepatron-App has: neither may change.
	if err := os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("node_modules/\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".mcp.json"), []byte("{}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-q", "-m", "init")
	return workdir, repo
}

func TestInitMulticaLeavesTheWorktreeClean(t *testing.T) {
	workdir, repo := multicaTask(t)
	t.Chdir(workdir)

	if err := runInitWith(t, nil, map[string]string{"multica": "true"}); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(repo, ".bts", "config", "settings.yaml")); err != nil {
		t.Fatal("bts was not initialized in the worktree")
	}
	if st := git(t, repo, "status", "--porcelain"); strings.TrimSpace(st) != "" {
		t.Fatalf("the worktree must stay clean, git status:\n%s", st)
	}
	if _, err := os.Stat(filepath.Join(repo, ".vscode")); !os.IsNotExist(err) {
		t.Fatal("--exclude must not deploy .vscode/")
	}
	if fi, err := os.Lstat(filepath.Join(workdir, ".claude", "skills", "bts-recipe-fix")); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the workdir must link bts-recipe-fix: %v", err)
	}
	if _, err := os.Stat(filepath.Join(workdir, ".claude", "settings.local.json")); err != nil {
		t.Fatal("the workdir must register bts's hooks")
	}
	if got, err := state.FindRoot(workdir); err != nil || got != repo {
		t.Fatalf("FindRoot(workdir) = %q, %v; want %q", got, err, repo)
	}

	// A later run on the same task: no error, still clean.
	if err := runInitWith(t, nil, map[string]string{"multica": "true"}); err != nil {
		t.Fatalf("a second bts init --multica must succeed: %v", err)
	}
	if st := git(t, repo, "status", "--porcelain"); strings.TrimSpace(st) != "" {
		t.Fatalf("after a second run, git status:\n%s", st)
	}
}

func TestInitMulticaRefusesOutsideATask(t *testing.T) {
	t.Chdir(t.TempDir())
	err := runInitWith(t, nil, map[string]string{"multica": "true"})
	if err == nil || !strings.Contains(err.Error(), "no Multica task") {
		t.Fatalf("outside a task: %v", err)
	}
}

func TestInitMulticaNeedsOneRepository(t *testing.T) {
	workdir, _ := multicaTask(t)
	t.Chdir(workdir)
	if out, err := exec.Command("git", "init", "-q", filepath.Join(workdir, "Other")).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	err := runInitWith(t, nil, map[string]string{"multica": "true"})
	if err == nil || !strings.Contains(err.Error(), "several repositories") {
		t.Fatalf("two repositories: %v", err)
	}
	if err := runInitWith(t, []string{"Repo"}, map[string]string{"multica": "true"}); err != nil {
		t.Fatalf("naming one: %v", err)
	}
}

// A recipe created inside a run belongs to the run's issue, and its phase
// changes reach that issue.
func TestRecipeCreateInMulticaRecordsIssueAndNotifies(t *testing.T) {
	var calls []string
	restore := multica.SetCLIRunner(func(_ context.Context, args ...string) error {
		calls = append(calls, strings.Join(args, " "))
		return nil
	})
	defer restore()

	workdir, repo := multicaTask(t)
	t.Chdir(workdir)
	if err := runInitWith(t, nil, map[string]string{"multica": "true"}); err != nil {
		t.Fatal(err)
	}
	id, err := executeRecipeCreate(repo, "fix", "multica smoke", "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	r, err := state.LoadRecipeState(repo, id)
	if err != nil || r.MulticaIssue != "issue-1" {
		t.Fatalf("multica_issue = %q, %v; want issue-1", r.MulticaIssue, err)
	}
	want := []string{
		"issue metadata set issue-1 --key bts.recipe_id --value " + id,
		"issue metadata set issue-1 --key bts.phase --value research",
	}
	if strings.Join(calls, "\n") != strings.Join(want, "\n") {
		t.Fatalf("CLI calls:\n%s\nwant:\n%s", strings.Join(calls, "\n"), strings.Join(want, "\n"))
	}
}

func TestInitExcludeWithoutMultica(t *testing.T) {
	_, repo := multicaTask(t)
	if err := runInitWith(t, []string{repo}, map[string]string{"exclude": "true"}); err != nil {
		t.Fatal(err)
	}
	if st := git(t, repo, "status", "--porcelain"); strings.TrimSpace(st) != "" {
		t.Fatalf("--exclude must leave the repository clean:\n%s", st)
	}
	if data, _ := os.ReadFile(filepath.Join(repo, ".mcp.json")); string(data) != "{}\n" {
		t.Fatalf("the tracked .mcp.json changed: %q", data)
	}
}
