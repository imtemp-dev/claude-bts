package cli

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/feature"
	"github.com/imtemp-dev/claude-bts/internal/state"
)

// Regression tests from the feature store review of 2026-11-16 (bts #142,
// #143, #148).

type stackFixture struct {
	bare, main, fid, dir string
	i1, i2               string
	wa, wb               string
	gh                   *fakeGH
}

// stackedPair: i1's draft from worktree wa (branch me/NA-door), i2 stacked
// on it in wb with commits turn-x and turn-y and its own draft (me/NA-turn).
func stackedPair(t *testing.T) *stackFixture {
	t.Helper()
	bare, main, fid, dir, items, gh := prSetup(t)
	s := &stackFixture{bare: bare, main: main, fid: fid, dir: dir, i1: items[0], i2: items[1], gh: gh}
	s.wa = worktree(t, main, "agent/bts-worker/aaa", "master")
	id1, err := createFeatureRecipe(s.wa, fid, s.i1, "", "")
	if err != nil {
		t.Fatal(err)
	}
	commitFile(t, s.wa, "door.txt", "door\n")
	complete(t, s.wa, id1)
	if _, _, err := feature.OpenDraftPR(s.wa, fid, s.i1); err != nil {
		t.Fatal(err)
	}
	if _, err := feature.Mutate(dir, "test", func(f *feature.Feature) (string, error) {
		f.Multica = &feature.MulticaLink{Items: map[string]*feature.MulticaItem{
			s.i1: {Issue: "x1"}, s.i2: {Issue: "x2", Base: "me/NA-door", BaseItem: s.i1},
		}}
		return "stack", nil
	}); err != nil {
		t.Fatal(err)
	}
	gitT(t, main, "fetch", "-q", "origin")
	s.wb = worktree(t, main, "agent/bts-worker/bbb", "origin/me/NA-door")
	id2, err := createFeatureRecipe(s.wb, fid, s.i2, "", "")
	if err != nil {
		t.Fatal(err)
	}
	commitFile(t, s.wb, "turn-x.txt", "x\n")
	commitFile(t, s.wb, "turn-y.txt", "y\n")
	complete(t, s.wb, id2)
	if pr, _, err := feature.OpenDraftPR(s.wb, fid, s.i2); err != nil || pr.Branch != "me/NA-turn" || pr.Base != "me/NA-door" {
		t.Fatalf("i2's draft: %+v %v", pr, err)
	}
	return s
}

// mergeDoor squash-merges i1's branch into master and records the merge as
// dispatch would.
func (s *stackFixture) mergeDoor(t *testing.T) {
	t.Helper()
	gitT(t, s.main, "fetch", "-q", "origin")
	gitT(t, s.main, "checkout", "-q", "master")
	gitT(t, s.main, "merge", "-q", "--squash", "origin/me/NA-door")
	gitT(t, s.main, "commit", "-q", "-m", "door (#1)")
	gitT(t, s.main, "push", "-q", "origin", "master")
	sha := gitT(t, s.main, "rev-parse", "HEAD")
	if _, err := feature.Mutate(s.dir, "test", func(f *feature.Feature) (string, error) {
		f.Multica.Items[s.i1].Merged = &feature.MergeRecord{SHA: sha, At: "now"}
		return "merged", nil
	}); err != nil {
		t.Fatal(err)
	}
}

func (s *stackFixture) remoteLog(t *testing.T) string {
	return gitT(t, s.bare, "log", "--format=%s", "master..me/NA-turn")
}

// bts #142: a fix-forward pushes to the pull request a1 opened; the restack
// after the merge must replay every commit on the branch, a1's included.
func TestRestackAfterFixForwardKeepsEveryAttempt(t *testing.T) {
	s := stackedPair(t)
	idFix, err := createFeatureRecipe(s.wb, s.fid, s.i2, "fix", "")
	if err != nil {
		t.Fatal(err)
	}
	commitFile(t, s.wb, "turn-z.txt", "z\n")
	complete(t, s.wb, idFix)
	if pr, created, err := feature.OpenDraftPR(s.wb, s.fid, s.i2); err != nil || created || pr.Branch != "me/NA-turn" {
		t.Fatalf("the fix-forward pushes to the open pull request: %+v %v %v", pr, created, err)
	}
	s.mergeDoor(t)
	if _, err := feature.Restack(s.wb, s.fid, s.i2); err != nil {
		t.Fatalf("restack: %v", err)
	}
	if got, want := s.remoteLog(t), "add turn-z.txt\nadd turn-y.txt\nadd turn-x.txt"; got != want {
		t.Fatalf("the restacked branch must keep a1's and a2's commits on master:\ngot\n%s\nwant\n%s", got, want)
	}
}

// bts #148 (L1): a restack whose retarget fails has already pushed; it is
// recorded, and the retry only retargets.
func TestRestackRecordsThePushBeforeTheRetarget(t *testing.T) {
	s := stackedPair(t)
	s.mergeDoor(t)
	s.gh.failEdits = 1
	if _, err := feature.Restack(s.wb, s.fid, s.i2); err == nil || !strings.Contains(err.Error(), "retarget") {
		t.Fatalf("the failed retarget must be reported: %v", err)
	}
	ad := filepath.Join(s.dir, feature.ItemsDir, s.i2, "a1")
	var list []feature.RestackRecord
	if err := state.ReadJSON(filepath.Join(ad, feature.RestackFile), &list); err != nil || len(list) != 1 {
		t.Fatalf("the push must be recorded before the retarget: %+v %v", list, err)
	}
	if head := gitT(t, s.bare, "rev-parse", "refs/heads/me/NA-turn"); list[0].HeadSHA != head {
		t.Fatalf("recorded head %s, pushed %s", list[0].HeadSHA, head)
	}
	if _, err := feature.Restack(s.wb, s.fid, s.i2); err != nil {
		t.Fatalf("the retry: %v", err)
	}
	list = nil
	_ = state.ReadJSON(filepath.Join(ad, feature.RestackFile), &list)
	if len(list) != 1 || len(s.gh.edits) != 1 || !strings.Contains(s.gh.edits[0], "--base master") {
		t.Fatalf("the retry only retargets: records %d, edits %v", len(list), s.gh.edits)
	}
	if got := s.remoteLog(t); got != "add turn-y.txt\nadd turn-x.txt" {
		t.Fatalf("branch after the retry:\n%s", got)
	}
	var pr feature.PRRecord
	_ = state.ReadJSON(filepath.Join(ad, feature.PRFile), &pr)
	if pr.Base != "master" {
		t.Fatalf("the pull request record's base: %s", pr.Base)
	}
}

// bts #148 (L3): restack acts on the worktree's HEAD, which must be on the
// pull request's branch.
func TestRestackRunsOnThePRBranch(t *testing.T) {
	s := stackedPair(t)
	s.mergeDoor(t)
	gitT(t, s.wb, "switch", "-q", "-c", "elsewhere")
	if _, err := feature.Restack(s.wb, s.fid, s.i2); err == nil || !strings.Contains(err.Error(), "me/NA-turn") {
		t.Fatalf("restack on another branch must refuse: %v", err)
	}
	if got := gitT(t, s.wb, "branch", "--show-current"); got != "elsewhere" {
		t.Fatalf("restack must not switch the worktree's branch: %s", got)
	}
}

// bts #148 (L3): restack never pushes a copy that lacks the pull request's
// head.
func TestRestackRefusesAStaleCopy(t *testing.T) {
	s := stackedPair(t)
	s.mergeDoor(t)
	pushed := gitT(t, s.bare, "rev-parse", "refs/heads/me/NA-turn")
	gitT(t, s.wb, "reset", "-q", "--hard", "HEAD~1")
	if _, err := feature.Restack(s.wb, s.fid, s.i2); err == nil || !strings.Contains(err.Error(), "does not contain") {
		t.Fatalf("a copy without the pull request's head must refuse: %v", err)
	}
	if got := gitT(t, s.bare, "rev-parse", "refs/heads/me/NA-turn"); got != pushed {
		t.Fatalf("the pull request's branch must be untouched: %s, was %s", got, pushed)
	}
}

// bts #143: a commit alone puts a feature-bound recipe's snapshot behind,
// and the next sync records the new head.
func TestCommitPutsTheSnapshotBehind(t *testing.T) {
	_, main, fid, _, items, _ := prSetup(t)
	wa := worktree(t, main, "agent/bts-worker/aaa", "master")
	id, err := createFeatureRecipe(wa, fid, items[0], "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := feature.Sync(wa, id); err != nil {
		t.Fatal(err)
	}
	r, _ := state.LoadRecipeState(wa, id)
	if feature.Behind(wa, r) {
		t.Fatal("just synced")
	}
	commitFile(t, wa, "door.txt", "door\n")
	if !feature.Behind(wa, r) {
		t.Fatal("a new commit must put the snapshot behind")
	}
	if synced, err := feature.SyncPending(wa); err != nil || len(synced) != 1 {
		t.Fatalf("sync pending: %v %v", synced, err)
	}
	st := itemState(t, fid, items[0])
	if st.Latest == nil || st.Latest.HeadSHA != gitT(t, wa, "rev-parse", "HEAD") {
		t.Fatalf("the snapshot must carry the new head: %+v", st.Latest)
	}
}
