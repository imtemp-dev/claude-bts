package template

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

func gitInit(t *testing.T, dir string) {
	t.Helper()
	for _, args := range [][]string{
		{"init", "-q", dir},
		{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "init"},
	} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

func TestEnsureExcludeWritesOnceAndIgnores(t *testing.T) {
	repo := t.TempDir()
	gitInit(t, repo)

	wrote, err := EnsureExclude(repo)
	if err != nil || !wrote {
		t.Fatalf("first EnsureExclude = %v, %v; want a write", wrote, err)
	}
	if wrote, err := EnsureExclude(repo); err != nil || wrote {
		t.Fatalf("second EnsureExclude = %v, %v; want no write", wrote, err)
	}
	data, _ := os.ReadFile(filepath.Join(repo, ".git", "info", "exclude"))
	if n := strings.Count(string(data), excludeMarker); n != 1 {
		t.Fatalf("the block appears %d times, want once:\n%s", n, data)
	}
	for _, p := range []string{".bts/local/x", ".claude/skills/bts-verify/SKILL.md", ".claude/settings.local.json"} {
		if exec.Command("git", "-C", repo, "check-ignore", "-q", p).Run() != nil {
			t.Errorf("%s is not ignored after EnsureExclude", p)
		}
	}
	if _, err := os.Stat(filepath.Join(repo, ".gitignore")); !os.IsNotExist(err) {
		t.Fatalf("EnsureExclude must not create .gitignore")
	}
}

// The maintainer's own checkout already carries a hand-written block that
// opens with "/.bts/"; it must be recognised, not doubled.
func TestEnsureExcludeRespectsExistingRule(t *testing.T) {
	repo := t.TempDir()
	gitInit(t, repo)
	p := filepath.Join(repo, ".git", "info", "exclude")
	if err := os.WriteFile(p, []byte("# mine\n/.bts/\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if wrote, err := EnsureExclude(repo); err != nil || wrote {
		t.Fatalf("EnsureExclude with an existing /.bts/ rule = %v, %v; want no write", wrote, err)
	}
}

// A Multica task's repository is a linked worktree of the daemon's bare
// cache; the block belongs in the shared info/exclude, which git reads for
// every worktree.
func TestEnsureExcludeLinkedWorktree(t *testing.T) {
	main := t.TempDir()
	gitInit(t, main)
	wt := filepath.Join(t.TempDir(), "wt")
	if out, err := exec.Command("git", "-C", main, "worktree", "add", "-q", "-b", "task", wt).CombinedOutput(); err != nil {
		t.Fatalf("worktree add: %v\n%s", err, out)
	}
	if _, err := EnsureExclude(wt); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(main, ".git", "info", "exclude"))
	if !strings.Contains(string(data), excludeMarker) {
		t.Fatalf("the block must go to the common info/exclude, got:\n%s", data)
	}
	if exec.Command("git", "-C", wt, "check-ignore", "-q", ".bts/local/x").Run() != nil {
		t.Fatal(".bts/ is not ignored in the linked worktree")
	}
}

func TestEnsureExcludeOutsideGit(t *testing.T) {
	if _, err := EnsureExclude(t.TempDir()); err == nil {
		t.Fatal("EnsureExclude outside a repository must fail")
	}
}

func TestSkipInExcludeMode(t *testing.T) {
	for path, want := range map[string]bool{
		".mcp.json":                      true,
		".vscode":                        true,
		".vscode/markdown.code-snippets": true,
		".claude/skills/bts-verify":      false,
		".bts/config/settings.yaml":      false,
		".vscodex":                       false,
	} {
		if got := SkipInExcludeMode(path); got != want {
			t.Errorf("SkipInExcludeMode(%q) = %v, want %v", path, got, want)
		}
	}
}

func mkfile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestLinkSession(t *testing.T) {
	session := t.TempDir()
	repo := filepath.Join(session, "Repo")
	mkfile(t, filepath.Join(repo, ".bts", "config", "settings.yaml"))
	mkfile(t, filepath.Join(repo, ".claude", "skills", "bts-verify", "SKILL.md"))
	mkfile(t, filepath.Join(repo, ".claude", "skills", "team-skill", "SKILL.md"))
	mkfile(t, filepath.Join(repo, ".claude", "agents", "bts-verifier.md"))
	mkfile(t, filepath.Join(repo, ".claude", "rules", "bts-schema.md"))
	// Multica's own skill in the session root must survive.
	mkfile(t, filepath.Join(session, ".claude", "skills", "multica-platform", "SKILL.md"))

	res, err := LinkSession(session, repo)
	if err != nil {
		t.Fatal(err)
	}
	if res.Links != 3 {
		t.Fatalf("links = %d, want 3 (team-skill is not bts's)", res.Links)
	}
	target, err := os.Readlink(filepath.Join(session, ".claude", "skills", "bts-verify"))
	if err != nil || target != filepath.Join(repo, ".claude", "skills", "bts-verify") {
		t.Fatalf("bts-verify link = %q, %v", target, err)
	}
	if _, err := os.Lstat(filepath.Join(session, ".claude", "skills", "team-skill")); !os.IsNotExist(err) {
		t.Fatal("a non-bts skill must not be linked")
	}
	if _, err := os.Stat(filepath.Join(session, ".claude", "skills", "multica-platform", "SKILL.md")); err != nil {
		t.Fatal("the session's own skills must be left alone")
	}

	ptr, _ := os.ReadFile(filepath.Join(session, state.PointerFile))
	if strings.TrimSpace(string(ptr)) != "Repo" {
		t.Fatalf("pointer = %q, want the relative path Repo", ptr)
	}
	if got, err := state.FindRoot(session); err != nil || got != repo {
		t.Fatalf("FindRoot(session) = %q, %v; want %q", got, err, repo)
	}

	var settings map[string]interface{}
	data, err := os.ReadFile(filepath.Join(session, ".claude", "settings.local.json"))
	if err != nil || json.Unmarshal(data, &settings) != nil {
		t.Fatalf("session settings: %v", err)
	}
	if !strings.Contains(string(data), filepath.Join(repo, ".claude", "hooks", "bts-handle-stop.sh")) {
		t.Fatalf("the session's Stop hook must run the worktree's script:\n%s", data)
	}
	if _, err := os.Stat(filepath.Join(repo, ".claude", "settings.local.json")); !os.IsNotExist(err) {
		t.Fatal("LinkSession must not write the worktree's settings")
	}

	// Again: links are refreshed, nothing is doubled.
	res, err = LinkSession(session, repo)
	if err != nil || res.Links != 3 || len(res.Skipped) != 0 {
		t.Fatalf("second LinkSession = %+v, %v", res, err)
	}

	// A real file where a link would go is left alone and reported.
	clash := filepath.Join(session, ".claude", "rules", "bts-schema.md")
	_ = os.Remove(clash)
	mkfile(t, clash)
	res, err = LinkSession(session, repo)
	if err != nil || len(res.Skipped) != 1 || res.Skipped[0] != clash {
		t.Fatalf("a clashing file: %+v, %v", res, err)
	}
}

func TestLinkSessionRefusals(t *testing.T) {
	repo := t.TempDir()
	mkfile(t, filepath.Join(repo, ".bts", "config", "settings.yaml"))
	if _, err := LinkSession(repo, repo); err == nil {
		t.Fatal("linking a project to itself must fail")
	}
	if _, err := LinkSession(t.TempDir(), t.TempDir()); err == nil {
		t.Fatal("linking to a directory without .bts/ must fail")
	}
}
