package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/feature"
	"github.com/imtemp-dev/claude-bts/internal/state"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// resetFlags puts a package-level command's flags back to their defaults
// and clears Changed, so one test's flags do not leak into the next.
func resetFlags(c *cobra.Command) {
	c.Flags().VisitAll(func(f *pflag.Flag) {
		if sv, ok := f.Value.(pflag.SliceValue); ok {
			_ = sv.Replace(nil)
		} else {
			_ = f.Value.Set(f.DefValue)
		}
		f.Changed = false
	})
}

// confirmedFeature makes a confirmed feature with the given build items;
// deps[i] lists the indexes item i depends on.
func confirmedFeature(t *testing.T, wt string, titles []string, deps map[int][]int) (fid, dir string, items []string) {
	t.Helper()
	repo, err := feature.RepoAt(wt)
	if err != nil {
		t.Fatal(err)
	}
	dir, f, _, err := feature.Create(feature.CreateOptions{Title: "feature", Repo: repo})
	if err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(dir, feature.IntentFile), []byte("# Intent\n\nx\n"), 0644)
	build := "build"
	for i, title := range titles {
		tt := title
		spec := feature.ItemSpec{Title: &tt, Type: &build}
		if d, ok := deps[i]; ok {
			var ds []string
			for _, k := range d {
				ds = append(ds, items[k])
			}
			spec.DependsOn = &ds
		}
		id, err := feature.AddItem(dir, spec)
		if err != nil {
			t.Fatal(err)
		}
		items = append(items, id)
	}
	if _, err := feature.Confirm(dir, "go"); err != nil {
		t.Fatal(err)
	}
	return f.ID, dir, items
}

func doneManifest(t *testing.T, dir, item string, n int) *feature.Manifest {
	t.Helper()
	var m feature.Manifest
	if err := state.ReadJSON(filepath.Join(dir, feature.ItemsDir, item, "a"+string(rune('0'+n)), feature.SnapshotDir, feature.ManifestFile), &m); err != nil {
		t.Fatal(err)
	}
	return &m
}

func commitIn(t *testing.T, wt string) {
	t.Helper()
	if out, err := exec.Command("git", "-C", wt, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "later").CombinedOutput(); err != nil {
		t.Fatalf("commit: %v\n%s", err, out)
	}
}

// Code review F1 and F5.  A done attempt's snapshot is a record: later syncs,
// from its own worktree or a fix-forward's, leave its head, branch and
// metrics alone.  After the fix-forward completes, a dependent's context
// serves the build's documents and the fix's.
func TestDoneSnapshotIsFrozenAndContextServesEveryAttempt(t *testing.T) {
	t.Setenv(feature.StoreEnv, t.TempDir())
	_, wts := featureRepo(t, 3)
	a, b, c := wts[0], wts[1], wts[2]
	fid, dir, items := confirmedFeature(t, a, []string{"Door", "Turn"}, map[int][]int{1: {0}})
	i1, i2 := items[0], items[1]

	idA, err := createFeatureRecipe(a, fid, i1, "", "")
	if err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(state.RecipeDir(a, idA), "final.md"), []byte("# Final\n\nBUILD SPEC TEXT\n"), 0644)
	mdir := filepath.Join(state.LocalPath(a), "recipes", idA)
	_ = os.MkdirAll(mdir, 0755)
	_ = os.WriteFile(filepath.Join(mdir, "metrics.jsonl"), []byte(`{"kind":"tool_use"}`+"\n"), 0644)
	complete(t, a, idA)
	before := doneManifest(t, dir, i1, 1)
	if before.CompletedAt == "" {
		t.Fatal("a done attempt records when it ended")
	}

	commitIn(t, a) // A's HEAD moves on
	if _, err := feature.SyncPending(a); err != nil {
		t.Fatal(err)
	}
	idC, err := createFeatureRecipe(c, fid, i1, "fix", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := feature.SyncPending(c); err != nil { // as C's next session start would
		t.Fatal(err)
	}
	after := doneManifest(t, dir, i1, 1)
	if after.HeadSHA != before.HeadSHA || after.Branch != before.Branch || after.SyncedAt != before.SyncedAt {
		t.Fatalf("a1 was rewritten: head %s→%s branch %s→%s", before.HeadSHA, after.HeadSHA, before.Branch, after.Branch)
	}
	if _, err := os.Stat(filepath.Join(dir, feature.ItemsDir, i1, "a1", feature.SnapshotDir, "metrics.jsonl")); err != nil {
		t.Fatal("a1's metrics must survive")
	}

	_ = os.WriteFile(filepath.Join(state.RecipeDir(c, idC), "fix-spec.md"), []byte("# Fix\n\nFIX SPEC TEXT\n"), 0644)
	complete(t, c, idC)
	if _, err := createFeatureRecipe(b, fid, i2, "", ""); err != nil {
		t.Fatal(err)
	}
	doc, served, err := feature.Context(fid, i2, b)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, "BUILD SPEC TEXT") || !strings.Contains(doc, "FIX SPEC TEXT") {
		t.Fatalf("the context must serve the build and its fix:\n%s", doc)
	}
	if len(served.Dependencies) != 2 || served.Dependencies[0].Attempt != 1 || served.Dependencies[1].Attempt != 2 {
		t.Fatalf("served: %+v", served.Dependencies)
	}
}

// Code review F2.  Two items whose titles slug alike are r-001-… in their
// own worktrees; a fix-forward on one in the other's worktree is refused.
func TestFixForwardRefusesAnotherItemsRecipeWithTheSameID(t *testing.T) {
	t.Setenv(feature.StoreEnv, t.TempDir())
	_, wts := featureRepo(t, 2)
	a, b := wts[0], wts[1]
	fid, _, items := confirmedFeature(t, a, []string{"Global ask backend endpoint", "Global ask backend tests"}, nil)
	idA, err := createFeatureRecipe(a, fid, items[0], "", "")
	if err != nil {
		t.Fatal(err)
	}
	idB, err := createFeatureRecipe(b, fid, items[1], "", "")
	if err != nil {
		t.Fatal(err)
	}
	if idA != idB {
		t.Skipf("the titles slugged apart (%s, %s); the collision this test needs did not happen", idA, idB)
	}
	complete(t, a, idA)
	complete(t, b, idB)
	_, err = createFeatureRecipe(b, fid, items[0], "fix", "")
	if err == nil || !strings.Contains(err.Error(), "another worktree") {
		t.Fatalf("a fix-forward onto another item's recipe: %v", err)
	}
	r, _ := state.LoadRecipeState(b, idB)
	if r.FeatureItem != items[1] {
		t.Fatal("worktree B's recipe must be untouched")
	}
}

// Should-fix 1.  A plain --amends on a feature item's recipe is refused.
func TestPlainAmendsOfABoundRecipeIsRefused(t *testing.T) {
	t.Setenv(feature.StoreEnv, t.TempDir())
	_, wts := featureRepo(t, 1)
	a := wts[0]
	fid, _, items := confirmedFeature(t, a, []string{"Door"}, nil)
	id, _ := createFeatureRecipe(a, fid, items[0], "", "")
	complete(t, a, id)
	if _, err := executeRecipeCreate(a, "fix", "patch", id, "", false); err == nil || !strings.Contains(err.Error(), "--feature") {
		t.Fatalf("plain --amends: %v", err)
	}
}

// Should-fix 3.  A worktree whose recipe sits at finalize (spec done, not
// implemented) cannot take a second item.
func TestSecondClaimWhileFinalizedIsRefused(t *testing.T) {
	t.Setenv(feature.StoreEnv, t.TempDir())
	_, wts := featureRepo(t, 1)
	a := wts[0]
	fid, _, items := confirmedFeature(t, a, []string{"one", "two"}, nil)
	id, _ := createFeatureRecipe(a, fid, items[0], "", "")
	r, _ := state.LoadRecipeState(a, id)
	r.Phase = "finalize"
	_ = state.SaveRecipeState(a, r)
	if _, err := createFeatureRecipe(a, fid, items[1], "", ""); err == nil || !strings.Contains(err.Error(), "one item per worktree") {
		t.Fatalf("a second item while the first is finalized: %v", err)
	}
}

// Through cobra: `recipe log --phase complete --force` marks the item's
// completion as forced.
func TestForcedCompletionThroughCobra(t *testing.T) {
	t.Setenv(feature.StoreEnv, t.TempDir())
	_, wts := featureRepo(t, 1)
	a := wts[0]
	fid, _, items := confirmedFeature(t, a, []string{"Door"}, nil)
	id, _ := createFeatureRecipe(a, fid, items[0], "", "")
	t.Chdir(a)
	t.Cleanup(func() { resetFlags(recipeLogCmd); rootCmd.SetArgs(nil) })
	rootCmd.SetArgs([]string{"recipe", "log", id, "--phase", "complete", "--force"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if st := itemState(t, fid, items[0]); st.DoneAttempt == nil || st.DoneAttempt.CompletedVia != "forced" {
		t.Fatalf("forced through cobra: %+v", st.DoneAttempt)
	}
}
