package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/feature"
)

func gitT(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)
	out, err := exec.Command("git", full...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// fakeGH stands in for the GitHub CLI: no pull request exists until
// `pr create`, which records its arguments.
type fakeGH struct {
	mu        sync.Mutex
	created   []string
	edits     []string
	byHead    map[string]string
	failEdits int // pr edit fails this many times
}

func (g *fakeGH) run(_ string, _ string, args ...string) ([]byte, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	switch {
	case len(args) >= 2 && args[0] == "pr" && args[1] == "create":
		head := ""
		for i := range args {
			if args[i] == "--head" {
				head = args[i+1]
			}
		}
		url := fmt.Sprintf("https://github.com/org/app/pull/%d", len(g.created)+1)
		g.byHead[head] = url
		g.created = append(g.created, strings.Join(args, " "))
		return []byte(url + "\n"), nil
	case len(args) >= 3 && args[0] == "pr" && args[1] == "view":
		if strings.HasPrefix(args[2], "https://") {
			b, _ := json.Marshal(map[string]interface{}{"number": 1, "state": "OPEN"})
			return b, nil
		}
		if url, ok := g.byHead[args[2]]; ok {
			b, _ := json.Marshal(map[string]interface{}{"url": url, "number": 1, "state": "OPEN"})
			return b, nil
		}
		return nil, fmt.Errorf("no pull requests found for branch %q", args[2])
	case len(args) >= 2 && args[0] == "pr" && args[1] == "edit":
		if g.failEdits > 0 {
			g.failEdits--
			return nil, fmt.Errorf("gh pr edit: HTTP 502")
		}
		g.edits = append(g.edits, strings.Join(args, " "))
		return nil, nil
	}
	return nil, fmt.Errorf("fake gh: unexpected %v", args)
}

// prSetup: a bare origin with master, a clone, a confirmed feature i1 ← i2
// with base master, and the store's branch prefix me/.
func prSetup(t *testing.T) (bare, main, fid, dir string, items []string, gh *fakeGH) {
	t.Helper()
	t.Setenv(feature.StoreEnv, t.TempDir())
	base := t.TempDir()
	bare = filepath.Join(base, "origin.git")
	if out, err := exec.Command("git", "init", "-q", "--bare", "-b", "master", bare).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	main = filepath.Join(base, "main")
	if out, err := exec.Command("git", "clone", "-q", bare, main).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	// The restack under test rebases, which needs a committer, and a CI
	// runner has no global identity.  Worktrees of main share this config.
	gitT(t, main, "config", "user.name", "t")
	gitT(t, main, "config", "user.email", "t@t")
	gitT(t, main, "checkout", "-q", "-B", "master")
	_ = os.WriteFile(filepath.Join(main, "app.txt"), []byte("base\n"), 0644)
	gitT(t, main, "add", ".")
	gitT(t, main, "commit", "-q", "-m", "base")
	gitT(t, main, "push", "-q", "origin", "master")

	repo, err := feature.RepoAt(main)
	if err != nil {
		t.Fatal(err)
	}
	dir, f, _, err := feature.Create(feature.CreateOptions{Title: "pr flow", Repo: repo, Base: "master"})
	if err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(dir, feature.IntentFile), []byte("# Intent\n\nx\n"), 0644)
	build := "build"
	door, turn := "door", "turn"
	i1, _ := feature.AddItem(dir, feature.ItemSpec{Title: &door, Type: &build})
	i2, _ := feature.AddItem(dir, feature.ItemSpec{Title: &turn, Type: &build, DependsOn: &[]string{i1}})
	if _, err := feature.Confirm(dir, "go"); err != nil {
		t.Fatal(err)
	}
	if err := feature.SaveMulticaConfig(&feature.MulticaConfig{BranchPrefix: "me/"}); err != nil {
		t.Fatal(err)
	}
	gh = &fakeGH{byHead: map[string]string{}}
	t.Cleanup(feature.SetGHRunner(gh.run))
	return bare, main, f.ID, dir, []string{i1, i2}, gh
}

// worktree adds a bts worktree of main on a Multica-style branch from ref.
func worktree(t *testing.T, main, branch, ref string) string {
	t.Helper()
	wt := filepath.Join(t.TempDir(), "wt")
	gitT(t, main, "worktree", "add", "-q", "-b", branch, wt, ref)
	if _, err := os.Stat(filepath.Join(main, ".git", "info")); err == nil {
		_ = os.WriteFile(filepath.Join(main, ".git", "info", "exclude"), []byte("/.bts/\n"), 0644)
	}
	for _, d := range []string{".bts/specs/recipes", ".bts/local"} {
		_ = os.MkdirAll(filepath.Join(wt, d), 0755)
	}
	return wt
}

func commitFile(t *testing.T, wt, name, body string) {
	t.Helper()
	_ = os.WriteFile(filepath.Join(wt, name), []byte(body), 0644)
	gitT(t, wt, "add", name)
	gitT(t, wt, "commit", "-q", "-m", "add "+name)
}

func TestOpenDraftPRAndRestackAfterSquashMerge(t *testing.T) {
	bare, main, fid, dir, items, gh := prSetup(t)
	i1, i2 := items[0], items[1]

	// i1 in a worktree on a branch Multica named.
	wa := worktree(t, main, "agent/bts-worker/aaa", "master")
	id1, err := createFeatureRecipe(wa, fid, i1, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := feature.OpenDraftPR(wa, fid, i1); err == nil || !strings.Contains(err.Error(), "completes") {
		t.Fatalf("before completion: %v", err)
	}
	complete(t, wa, id1)
	if _, _, err := feature.OpenDraftPR(wa, fid, i1); err == nil || !strings.Contains(err.Error(), "no commits") {
		t.Fatalf("with no commits: %v", err)
	}
	_ = os.WriteFile(filepath.Join(wa, "door.txt"), []byte("door\n"), 0644)
	if _, _, err := feature.OpenDraftPR(wa, fid, i1); err == nil || !strings.Contains(err.Error(), "uncommitted") {
		t.Fatalf("with uncommitted work: %v", err)
	}
	gitT(t, wa, "add", "door.txt")
	gitT(t, wa, "commit", "-q", "-m", "door")

	pr1, created, err := feature.OpenDraftPR(wa, fid, i1)
	if err != nil || !created {
		t.Fatalf("open: %+v %v %v", pr1, created, err)
	}
	if pr1.Branch != "me/NA-door" || pr1.Base != "master" {
		t.Fatalf("branch %q base %q", pr1.Branch, pr1.Base)
	}
	if got := gitT(t, wa, "branch", "--show-current"); got != "me/NA-door" {
		t.Fatalf("the worktree branch must be renamed: %s", got)
	}
	if gitT(t, bare, "rev-parse", "refs/heads/me/NA-door") != gitT(t, wa, "rev-parse", "HEAD") {
		t.Fatal("the branch must be pushed")
	}
	if len(gh.created) != 1 || !strings.Contains(gh.created[0], "--draft --base master --head me/NA-door --title [NA] door") {
		t.Fatalf("gh pr create: %v", gh.created)
	}
	if _, created, err := feature.OpenDraftPR(wa, fid, i1); err != nil || created || len(gh.created) != 1 {
		t.Fatalf("a second open pushes only: created=%v %v %v", created, err, gh.created)
	}

	// The manager stacked i2 on i1's branch.
	if _, err := feature.Mutate(dir, "test", func(f *feature.Feature) (string, error) {
		f.Multica = &feature.MulticaLink{Items: map[string]*feature.MulticaItem{
			i1: {Issue: "x1"}, i2: {Issue: "x2", Base: "me/NA-door", BaseItem: i1},
		}}
		return "stack", nil
	}); err != nil {
		t.Fatal(err)
	}
	gitT(t, main, "fetch", "-q", "origin")
	wb := worktree(t, main, "agent/bts-worker/bbb", "origin/me/NA-door")
	id2, err := createFeatureRecipe(wb, fid, i2, "", "")
	if err != nil {
		t.Fatal(err)
	}
	commitFile(t, wb, "turn.txt", "turn\n")
	complete(t, wb, id2)
	pr2, _, err := feature.OpenDraftPR(wb, fid, i2)
	if err != nil || pr2.Base != "me/NA-door" {
		t.Fatalf("i2's draft targets i1's branch: %+v %v", pr2, err)
	}

	// i1 gets a review fix, then is squash-merged into master.
	commitFile(t, wa, "door.txt", "door, reviewed\n")
	if _, _, err := feature.OpenDraftPR(wa, fid, i1); err != nil {
		t.Fatal(err)
	}
	gitT(t, main, "fetch", "-q", "origin")
	gitT(t, main, "checkout", "-q", "master")
	gitT(t, main, "merge", "-q", "--squash", "origin/me/NA-door")
	gitT(t, main, "commit", "-q", "-m", "door (#1)")
	gitT(t, main, "push", "-q", "origin", "master")

	if _, err := feature.Restack(wb, fid, i2); err == nil || !strings.Contains(err.Error(), "has not merged") {
		t.Fatalf("restack before the merge is recorded: %v", err)
	}
	if _, err := feature.Mutate(dir, "test", func(f *feature.Feature) (string, error) {
		f.Multica.Items[i1].Merged = &feature.MergeRecord{SHA: gitT(t, main, "rev-parse", "HEAD"), At: "now"}
		return "merged", nil
	}); err != nil {
		t.Fatal(err)
	}
	rs, err := feature.Restack(wb, fid, i2)
	if err != nil {
		t.Fatalf("restack: %v", err)
	}
	if rs.ToRef != "master" || len(gh.edits) != 1 || !strings.Contains(gh.edits[0], "--base master") {
		t.Fatalf("restack record %+v, gh edits %v", rs, gh.edits)
	}
	log := gitT(t, bare, "log", "--format=%s", "master..me/NA-turn")
	if log != "add turn.txt" {
		t.Fatalf("only i2's commit is left on top of master, got:\n%s", log)
	}
}
