package feature

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

func TestRepoKey(t *testing.T) {
	want := "github.com/carepatron/carepatron-app"
	for _, u := range []string{
		"git@github.com:Carepatron/Carepatron-App.git",
		"git@github.com:Carepatron/Carepatron-App",
		"ssh://git@github.com/Carepatron/Carepatron-App.git",
		"ssh://git@github.com:22/Carepatron/Carepatron-App.git",
		"https://github.com/Carepatron/Carepatron-App.git",
		"https://user@github.com/Carepatron/Carepatron-App/",
		"HTTPS://GitHub.com/Carepatron/Carepatron-App",
	} {
		if got := RepoKey(u); got != want {
			t.Errorf("RepoKey(%q) = %q, want %q", u, got, want)
		}
	}
	for _, u := range []string{"", "/srv/git/repo.git", "./repo", "file:///srv/git/repo.git"} {
		if got := RepoKey(u); got != "" {
			t.Errorf("RepoKey(%q) = %q, want empty (not a remote)", u, got)
		}
	}
}

func run(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v: %v\n%s", args, err, out)
	}
}

func gitRepo(t *testing.T, origin string) string {
	t.Helper()
	dir := t.TempDir()
	run(t, dir, "git", "init", "-q")
	run(t, dir, "git", "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "init")
	if origin != "" {
		run(t, dir, "git", "remote", "add", "origin", origin)
	}
	return dir
}

func TestRepoAtSSHAndHTTPSAgreeAndNoOriginFallsBack(t *testing.T) {
	a, err := RepoAt(gitRepo(t, "git@github.com:Org/App.git"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := RepoAt(gitRepo(t, "https://github.com/org/app"))
	if err != nil {
		t.Fatal(err)
	}
	if a.Key != b.Key || a.Key != "github.com/org/app" {
		t.Fatalf("keys %q and %q must both be github.com/org/app", a.Key, b.Key)
	}
	local := gitRepo(t, "")
	c, err := RepoAt(local)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(c.Key, "local/") {
		t.Fatalf("no origin: key %q, want local/…", c.Key)
	}
	wt := filepath.Join(t.TempDir(), "wt")
	run(t, local, "git", "worktree", "add", "-q", "-b", "w", wt)
	d, err := RepoAt(wt)
	if err != nil || d.Key != c.Key {
		t.Fatalf("a worktree of the same clone must share the key: %q vs %q (%v)", d.Key, c.Key, err)
	}
	if _, err := RepoAt(t.TempDir()); err == nil {
		t.Fatal("RepoAt outside a repository must fail")
	}
}

// newFeature makes a store and a feature with intent written.
func newFeature(t *testing.T) (dir string, f *Feature) {
	t.Helper()
	t.Setenv(StoreEnv, t.TempDir())
	dir, f, _, err := Create(CreateOptions{Title: "Global ask", Repo: &Repo{Key: "github.com/org/app"}, Base: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, IntentFile), []byte("# Intent\n\nAsk anything from one place.\n"), 0644); err != nil {
		t.Fatal(err)
	}
	return dir, f
}

func ptr[T any](v T) *T { return &v }

func TestCreateFindAndTicket(t *testing.T) {
	dir, f := newFeature(t)
	if !strings.HasPrefix(f.ID, "f-001-") {
		t.Fatalf("id = %q", f.ID)
	}
	if got, err := Find("f-001"); err != nil || got != dir {
		t.Fatalf("Find(f-001) = %q, %v", got, err)
	}
	if got, err := Find(f.ID); err != nil || got != dir {
		t.Fatalf("Find(full id) = %q, %v", got, err)
	}
	if _, err := Find("f-099"); err == nil {
		t.Fatal("an unknown id must fail")
	}
	_, g, _, err := Create(CreateOptions{Title: "Other", Ticket: "CU-1", Repo: &Repo{Key: "local/x-1"}})
	if err != nil || !strings.HasPrefix(g.ID, "f-002-") {
		t.Fatalf("second feature: %+v %v", g, err)
	}
	_, h, existed, err := Create(CreateOptions{Title: "Other again", Ticket: "CU-1", Repo: &Repo{Key: "local/x-1"}})
	if err != nil || !existed || h.ID != g.ID {
		t.Fatalf("same ticket must return the open feature: %+v existed=%v %v", h, existed, err)
	}
}

func TestConfirmChecksPinsAndPends(t *testing.T) {
	dir, _ := newFeature(t)
	if _, err := Confirm(dir, "yes"); err == nil || !strings.Contains(err.Error(), "no items") {
		t.Fatalf("an empty plan: %v", err)
	}
	i1, _ := AddItem(dir, ItemSpec{Title: ptr("Door"), Type: ptr("build")})
	i2, _ := AddItem(dir, ItemSpec{Title: ptr("Turn"), Type: ptr("build"), DependsOn: &[]string{i1}})
	if _, err := AddItem(dir, ItemSpec{Title: ptr("Bad"), Type: ptr("fix"), Express: ptr(true)}); err == nil {
		t.Fatal("express on a fix item must fail")
	}
	if err := EditItem(dir, i1, ItemSpec{DependsOn: &[]string{i2}}); err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("a cycle must fail: %v", err)
	}
	if _, err := Confirm(dir, ""); err == nil {
		t.Fatal("confirm needs the user's words")
	}
	n, err := Confirm(dir, "looks right")
	if err != nil || n != 1 {
		t.Fatalf("confirm: %d %v", n, err)
	}
	if _, err := os.Stat(filepath.Join(dir, ConfirmedDir, "1", PlanFile)); err != nil {
		t.Fatal("confirmation 1 must keep the plan")
	}
	if _, err := Confirm(dir, "again"); err == nil || !strings.Contains(err.Error(), "nothing changed") {
		t.Fatalf("an unchanged confirm: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ContractsFile), []byte("# Contracts\n\nDoor returns 404 when off.\n"), 0644); err != nil {
		t.Fatal(err)
	}
	i3, _ := AddItem(dir, ItemSpec{Title: ptr("Rollout"), Type: ptr("build"), DependsOn: &[]string{i2}})
	f, _ := Load(dir)
	if p := Pending(dir, f); strings.Join(p, ",") != "contracts,plan" {
		t.Fatalf("pending = %v", p)
	}
	s, _ := Derive(dir, f)
	if it := s.item(i3); it.InConfirmed {
		t.Fatal("an item added after confirmation is not in it")
	}
	if n, err := Confirm(dir, "with rollout"); err != nil || n != 2 {
		t.Fatalf("second confirm: %d %v", n, err)
	}
}

func TestMutateRefusesNewerSchema(t *testing.T) {
	dir, f := newFeature(t)
	f.Schema = Schema + 1
	if err := state.WriteJSON(filepath.Join(dir, FeatureFile), f); err != nil {
		t.Fatal(err)
	}
	if _, err := AddItem(dir, ItemSpec{Title: ptr("x"), Type: ptr("build")}); err == nil || !strings.Contains(err.Error(), "newer bts") {
		t.Fatalf("a newer schema must be refused: %v", err)
	}
}

// writeAttempt fakes what a child leaves in the store.
func writeAttempt(t *testing.T, dir, item string, n int, phase, outcome, via string) {
	t.Helper()
	ad := filepath.Join(dir, ItemsDir, item, "a"+itoa(n))
	if err := os.MkdirAll(filepath.Join(ad, SnapshotDir, "recipe"), 0755); err != nil {
		t.Fatal(err)
	}
	_ = state.WriteJSON(filepath.Join(ad, ClaimFile), &Claim{Schema: Schema, Item: item, Attempt: n, RecipeID: "r-001-x", Kind: "build"})
	if phase != "" {
		_ = state.WriteJSON(filepath.Join(ad, SnapshotDir, ManifestFile), &Manifest{Schema: Schema, RecipeID: "r-001-x", Phase: phase, Outcome: outcome, CompletedVia: via, ContentHash: phase})
	}
}

func itoa(n int) string { return strconv.Itoa(n) }

func TestDeriveAndNext(t *testing.T) {
	dir, _ := newFeature(t)
	i1, _ := AddItem(dir, ItemSpec{Title: ptr("a"), Type: ptr("build")})
	i2, _ := AddItem(dir, ItemSpec{Title: ptr("b"), Type: ptr("build"), DependsOn: &[]string{i1}})
	i3, _ := AddItem(dir, ItemSpec{Title: ptr("c"), Type: ptr("fix")})
	f, _ := Load(dir)
	if ready, _, _ := Next(dir, f); len(ready) != 0 {
		t.Fatal("a draft feature offers nothing")
	}
	if _, err := Confirm(dir, "go"); err != nil {
		t.Fatal(err)
	}
	f, _ = Load(dir)
	ready, s, _ := Next(dir, f)
	if s.State != FeatureConfirmed || len(ready) != 2 || ready[0].ID != i1 || ready[1].ID != i3 {
		t.Fatalf("next = %v (%s)", ready, s.State)
	}
	if s.item(i2).Stage != 2 {
		t.Fatalf("i2 stage = %d", s.item(i2).Stage)
	}

	writeAttempt(t, dir, i1, 1, "implement", "", "")
	f, _ = Load(dir)
	s, _ = Derive(dir, f)
	if s.item(i1).State != StateActive || s.State != FeatureActive {
		t.Fatalf("an open attempt: %s / %s", s.item(i1).State, s.State)
	}
	writeAttempt(t, dir, i1, 1, "complete", "done", "forced")
	f, _ = Load(dir)
	ready, s, _ = Next(dir, f)
	if s.item(i1).State != StateDone || !strings.Contains(s.item(i1).Note, "forced") {
		t.Fatalf("a forced completion is done and says so: %+v", s.item(i1))
	}
	if len(ready) != 2 || ready[0].ID != i2 {
		t.Fatalf("i2 is ready once i1 is done: %v", ready)
	}

	writeAttempt(t, dir, i3, 1, "cancelled", "cancelled", "")
	f, _ = Load(dir)
	s, _ = Derive(dir, f)
	if s.item(i3).State != StateClaimable {
		t.Fatalf("a cancelled attempt leaves the item claimable: %s", s.item(i3).State)
	}
	if err := DecideItem(dir, i3, "skip", "not needed"); err != nil {
		t.Fatal(err)
	}
	writeAttempt(t, dir, i2, 1, "complete", "done", "gate")
	f, _ = Load(dir)
	s, _ = Derive(dir, f)
	if s.State != FeatureReady {
		t.Fatalf("every item done or skipped: %s", s.State)
	}
	if err := Close(dir, "done", "ship it"); err != nil {
		t.Fatal(err)
	}
	f, _ = Load(dir)
	if s, _ := Derive(dir, f); s.State != "done" {
		t.Fatalf("closed: %s", s.State)
	}
}

func TestReleaseAndReconcile(t *testing.T) {
	dir, _ := newFeature(t)
	i1, _ := AddItem(dir, ItemSpec{Title: ptr("a"), Type: ptr("build")})
	_, _ = Confirm(dir, "go")
	writeAttempt(t, dir, i1, 1, "implement", "", "")
	ch, err := Reconcile(dir)
	if err != nil || len(ch) != 1 || ch[0].Item != i1 {
		t.Fatalf("first reconcile: %v %v", ch, err)
	}
	if ch, _ := Reconcile(dir); len(ch) != 0 {
		t.Fatalf("nothing changed: %v", ch)
	}
	if _, err := ReleaseItem(dir, i1, ""); err == nil {
		t.Fatal("release needs the user's words")
	}
	if n, err := ReleaseItem(dir, i1, "the run died"); err != nil || n != 1 {
		t.Fatalf("release: %d %v", n, err)
	}
	f, _ := Load(dir)
	if s, _ := Derive(dir, f); s.item(i1).State != StateClaimable {
		t.Fatalf("a released attempt frees the item: %s", s.item(i1).State)
	}
}

func TestBeginClaimOnlyOneWins(t *testing.T) {
	dir, _ := newFeature(t)
	i1, _ := AddItem(dir, ItemSpec{Title: ptr("a"), Type: ptr("build")})
	i2, _ := AddItem(dir, ItemSpec{Title: ptr("b"), Type: ptr("build"), DependsOn: &[]string{i1}})
	f, _ := Load(dir)

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".bts", "specs", "recipes"), 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := BeginClaim(ClaimRequest{FeatureID: f.ID, ItemID: i1, Root: root}); err == nil || !strings.Contains(err.Error(), "not confirmed") {
		t.Fatalf("an unconfirmed feature: %v", err)
	}
	_, _ = Confirm(dir, "go")
	if _, err := BeginClaim(ClaimRequest{FeatureID: f.ID, ItemID: i2, Root: root}); err == nil || !strings.Contains(err.Error(), "waits for "+i1) {
		t.Fatalf("i2 before i1 is done: %v", err)
	}

	var wg sync.WaitGroup
	results := make([]error, 8)
	for k := range results {
		wg.Add(1)
		go func(k int) {
			defer wg.Done()
			_, results[k] = BeginClaim(ClaimRequest{FeatureID: f.ID, ItemID: i1, Root: root})
		}(k)
	}
	wg.Wait()
	wins := 0
	for _, err := range results {
		if err == nil {
			wins++
		}
	}
	if wins != 1 {
		t.Fatalf("%d concurrent claims won, want exactly 1: %v", wins, results)
	}
}
