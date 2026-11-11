package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/feature"
	"github.com/imtemp-dev/claude-bts/internal/state"
)

// featureRepo makes a clone with an origin and n worktrees, each a bts root.
func featureRepo(t *testing.T, n int) (main string, wts []string) {
	t.Helper()
	main = t.TempDir()
	run := func(dir string, args ...string) {
		t.Helper()
		full := append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)
		if out, err := exec.Command("git", full...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run(main, "init", "-q")
	run(main, "commit", "-q", "--allow-empty", "-m", "init")
	run(main, "remote", "add", "origin", "git@github.com:Org/App.git")
	for i := 0; i < n; i++ {
		wt := filepath.Join(t.TempDir(), "wt")
		run(main, "worktree", "add", "-q", "-b", "w"+string(rune('a'+i)), wt)
		for _, d := range []string{".bts/specs/recipes", ".bts/local"} {
			if err := os.MkdirAll(filepath.Join(wt, d), 0755); err != nil {
				t.Fatal(err)
			}
		}
		wts = append(wts, wt)
	}
	return main, wts
}

// complete moves a recipe to complete the way the Stop hook does.
func complete(t *testing.T, root, id string) {
	t.Helper()
	r, err := state.LoadRecipeState(root, id)
	if err != nil {
		t.Fatal(err)
	}
	prev := r.Phase
	r.Phase = "complete"
	if err := state.SaveRecipeState(root, r); err != nil {
		t.Fatal(err)
	}
	if err := state.LogPhaseChangeVia(root, id, prev, "complete", state.PhaseViaGate); err != nil {
		t.Fatal(err)
	}
}

func itemState(t *testing.T, fid, item string) *feature.ItemStatus {
	t.Helper()
	dir, err := feature.Find(fid)
	if err != nil {
		t.Fatal(err)
	}
	f, _ := feature.Load(dir)
	s, err := feature.Derive(dir, f)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range s.Items {
		if it.ID == item {
			return it
		}
	}
	t.Fatalf("no item %s", item)
	return nil
}

func TestFeatureItemsAcrossWorktrees(t *testing.T) {
	t.Setenv(feature.StoreEnv, t.TempDir())
	_, wts := featureRepo(t, 3)
	a, b, c := wts[0], wts[1], wts[2]

	repo, err := feature.RepoAt(a)
	if err != nil || repo.Key != "github.com/org/app" {
		t.Fatalf("repo key %+v %v", repo, err)
	}
	dir, f, _, err := feature.Create(feature.CreateOptions{Title: "Global ask", Repo: repo, Base: "main"})
	if err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(dir, feature.IntentFile), []byte("# Intent\n\nOne question box.\n"), 0644)
	title1, title2, build := "Door", "Turn", "build"
	i1, _ := feature.AddItem(dir, feature.ItemSpec{Title: &title1, Type: &build})
	i2, _ := feature.AddItem(dir, feature.ItemSpec{Title: &title2, Type: &build, DependsOn: &[]string{i1}})

	if _, err := createFeatureRecipe(a, f.ID, i1, "", ""); err == nil || !strings.Contains(err.Error(), "not confirmed") {
		t.Fatalf("before confirm: %v", err)
	}
	if _, err := feature.Confirm(dir, "yes, that plan"); err != nil {
		t.Fatal(err)
	}

	// Worktree A takes i1; the initial phase is already in the store.
	idA, err := createFeatureRecipe(a, f.ID, i1, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if st := itemState(t, f.ID, i1); st.State != feature.StateActive || st.Latest.RecipeID != idA || st.Latest.Phase == "" {
		t.Fatalf("after the claim: %+v %+v", st, st.Latest)
	}
	if _, err := createFeatureRecipe(b, f.ID, i1, "", ""); err == nil || !strings.Contains(err.Error(), "held by") {
		t.Fatalf("a second claim while a1 is open: %v", err)
	}
	if _, err := createFeatureRecipe(b, f.ID, i2, "", ""); err == nil || !strings.Contains(err.Error(), "waits for") {
		t.Fatalf("i2 before i1 is done: %v", err)
	}

	// A writes its spec and completes through the gate path.
	_ = os.WriteFile(filepath.Join(state.RecipeDir(a, idA), "final.md"), []byte("# Final\n\nThe door answers 404 when the flag is off.\n"), 0644)
	complete(t, a, idA)
	st := itemState(t, f.ID, i1)
	if st.State != feature.StateDone || st.DoneAttempt.CompletedVia != "gate" || st.DoneAttempt.HeadSHA == "" {
		t.Fatalf("i1 after completion: %+v %+v", st, st.DoneAttempt)
	}

	// Worktree B takes i2.  Its recipe id is r-001 again: harmless.
	idB, err := createFeatureRecipe(b, f.ID, i2, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if idB != idA && !strings.HasPrefix(idB, "r-001") {
		t.Fatalf("worktree B's first recipe: %s", idB)
	}

	// A is deleted; i2's context still serves i1's spec, from the store.
	if err := os.RemoveAll(a); err != nil {
		t.Fatal(err)
	}
	doc, served, err := feature.Context(f.ID, i2, b)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, "answers 404") || len(served.Dependencies) != 1 || served.Dependencies[0].Attempt != 1 {
		t.Fatalf("context after A is gone:\n%s\n%+v", doc, served)
	}

	// B cancels: the item is claimable again, as attempt a2.
	t.Chdir(b)
	if err := recipeCancelCmd.RunE(recipeCancelCmd, nil); err != nil {
		t.Fatal(err)
	}
	if st := itemState(t, f.ID, i2); st.State != feature.StateClaimable {
		t.Fatalf("i2 after cancel: %s", st.State)
	}
	idB2, err := createFeatureRecipe(b, f.ID, i2, "", "")
	if err != nil {
		t.Fatalf("reclaim after cancel: %v", err)
	}
	if st := itemState(t, f.ID, i2); st.Latest.N != 2 || st.Latest.RecipeID != idB2 {
		t.Fatalf("the reclaim is attempt a2: %+v", st.Latest)
	}

	// A fix-forward on done i1, in worktree C, which never had i1's recipe.
	if _, err := createFeatureRecipe(c, f.ID, i1, "", ""); err == nil || !strings.Contains(err.Error(), "--type fix") {
		t.Fatalf("a plain claim on a done item: %v", err)
	}
	idC, err := createFeatureRecipe(c, f.ID, i1, "fix", "")
	if err != nil {
		t.Fatalf("fix-forward: %v", err)
	}
	rc, _ := state.LoadRecipeState(c, idC)
	if rc.Type != "fix" || rc.Amends != idA || rc.FeatureAttempt != 2 {
		t.Fatalf("the fix-forward recipe: %+v", rc)
	}
	if _, err := os.Stat(state.RecipeDir(c, idA)); err != nil {
		t.Fatal("the amended recipe must be restored from the store")
	}
	if st := itemState(t, f.ID, i1); st.State != feature.StateDone || !strings.Contains(st.Note, "fix-forward a2 open") {
		t.Fatalf("i1 during its fix-forward: %+v", st)
	}
}

// A sync that cannot write never blocks the phase change, shows as behind,
// and the next session start's SyncPending catches up.
func TestFeatureMissedSyncIsRetried(t *testing.T) {
	t.Setenv(feature.StoreEnv, t.TempDir())
	_, wts := featureRepo(t, 1)
	a := wts[0]
	repo, _ := feature.RepoAt(a)
	dir, f, _, _ := feature.Create(feature.CreateOptions{Title: "x", Repo: repo})
	_ = os.WriteFile(filepath.Join(dir, feature.IntentFile), []byte("# Intent\n\nx\n"), 0644)
	title, build := "only", "build"
	i1, _ := feature.AddItem(dir, feature.ItemSpec{Title: &title, Type: &build})
	_, _ = feature.Confirm(dir, "go")
	id, err := createFeatureRecipe(a, f.ID, i1, "", "")
	if err != nil {
		t.Fatal(err)
	}

	ad := filepath.Join(dir, feature.ItemsDir, i1, "a1")
	if err := os.Chmod(ad, 0555); err != nil {
		t.Fatal(err)
	}
	complete(t, a, id) // the observer warns; completion stands
	r, _ := state.LoadRecipeState(a, id)
	if r.Phase != "complete" {
		t.Fatalf("completion must not be blocked: %s", r.Phase)
	}
	if !feature.Behind(a, r) {
		t.Fatal("the store must show the recipe as behind")
	}
	if err := os.Chmod(ad, 0755); err != nil {
		t.Fatal(err)
	}
	synced, err := feature.SyncPending(a)
	if err != nil || len(synced) != 1 {
		t.Fatalf("SyncPending: %v %v", synced, err)
	}
	if st := itemState(t, f.ID, i1); st.State != feature.StateDone {
		t.Fatalf("after the retry: %s", st.State)
	}
}

// Through cobra, as a session types it: an item's recipe needs no --topic,
// and a plain recipe still does.  The smoke found --topic marked required.
func TestRecipeCreateFeatureThroughCobra(t *testing.T) {
	t.Setenv(feature.StoreEnv, t.TempDir())
	_, wts := featureRepo(t, 1)
	a := wts[0]
	repo, _ := feature.RepoAt(a)
	dir, f, _, _ := feature.Create(feature.CreateOptions{Title: "x", Repo: repo})
	_ = os.WriteFile(filepath.Join(dir, feature.IntentFile), []byte("# Intent\n\nx\n"), 0644)
	title, build := "only", "build"
	i1, _ := feature.AddItem(dir, feature.ItemSpec{Title: &title, Type: &build})
	_, _ = feature.Confirm(dir, "go")
	t.Chdir(a)
	t.Cleanup(func() { resetFlags(recipeCreateCmd); rootCmd.SetArgs(nil) })

	rootCmd.SetArgs([]string{"recipe", "create", "--feature", f.ID, "--item", i1})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("recipe create --feature --item: %v", err)
	}
	if st := itemState(t, f.ID, i1); st.State != feature.StateActive {
		t.Fatalf("the item must be claimed: %s", st.State)
	}

	resetFlags(recipeCreateCmd)
	rootCmd.SetArgs([]string{"recipe", "create", "--type", "fix"})
	if err := rootCmd.Execute(); err == nil || !strings.Contains(err.Error(), "topic") {
		t.Fatalf("a plain recipe without --topic: %v", err)
	}
}

// A forced completion counts, and says it was forced.
func TestFeatureForcedCompletionIsMarked(t *testing.T) {
	t.Setenv(feature.StoreEnv, t.TempDir())
	_, wts := featureRepo(t, 1)
	a := wts[0]
	repo, _ := feature.RepoAt(a)
	dir, f, _, _ := feature.Create(feature.CreateOptions{Title: "x", Repo: repo})
	_ = os.WriteFile(filepath.Join(dir, feature.IntentFile), []byte("# Intent\n\nx\n"), 0644)
	title, build := "only", "build"
	i1, _ := feature.AddItem(dir, feature.ItemSpec{Title: &title, Type: &build})
	_, _ = feature.Confirm(dir, "go")
	id, _ := createFeatureRecipe(a, f.ID, i1, "", "")
	r, _ := state.LoadRecipeState(a, id)
	prev := r.Phase
	r.Phase = "complete"
	_ = state.SaveRecipeState(a, r)
	_ = state.LogPhaseChangeVia(a, id, prev, "complete", state.PhaseViaCLI)
	if st := itemState(t, f.ID, i1); st.DoneAttempt == nil || st.DoneAttempt.CompletedVia != "forced" || !strings.Contains(st.Note, "forced") {
		t.Fatalf("a forced completion: %+v", st)
	}
}
