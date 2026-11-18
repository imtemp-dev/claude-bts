package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/feature"
	"github.com/imtemp-dev/claude-bts/internal/review"
	"github.com/spf13/pflag"
)

// runFeatureConfirm runs `bts feature confirm` in the working directory
// with fresh flags and returns its error.
func runFeatureConfirm(t *testing.T, args ...string) error {
	t.Helper()
	featureConfirmCmd.Flags().VisitAll(func(f *pflag.Flag) {
		_ = f.Value.Set(f.DefValue)
		f.Changed = false
	})
	rootCmd.SetArgs(append([]string{"feature", "confirm"}, args...))
	return rootCmd.Execute()
}

// A feature confirmed on its review page records the version and the
// fingerprint of the intent, contracts and plan the user pressed, and only
// that version, from the session's bts project.
func TestFeatureConfirmViaReview_RecordsTheFingerprintedBundle(t *testing.T) {
	root := newRecipeFixture(t, "r-host", "scoping", 0, 0, nil)
	for _, k := range review.KitFiles {
		p := filepath.Join(root, k)
		_ = os.MkdirAll(filepath.Dir(p), 0755)
		_ = os.WriteFile(p, []byte("/* kit */"), 0644)
	}
	t.Setenv(feature.StoreEnv, t.TempDir())
	dir, f, _, err := feature.Create(feature.CreateOptions{Title: "attach files", Repo: &feature.Repo{Key: "local/fr-1"}, Base: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, feature.IntentFile), []byte("# Intent\n\nAttach files on GPT.\n"), 0644); err != nil {
		t.Fatal(err)
	}
	title, typ := "existing chat on GPT", "debug"
	if _, err := feature.AddItem(dir, feature.ItemSpec{Title: &title, Type: &typ}); err != nil {
		t.Fatal(err)
	}

	t.Chdir(t.TempDir())
	if _, err := featureSubject(f.ID); err == nil || !strings.Contains(err.Error(), "bts project") {
		t.Errorf("outside a bts project there is nowhere for the page and kit, got %v", err)
	}
	t.Chdir(root)
	sub, err := featureSubject(f.ID)
	if err != nil {
		t.Fatal(err)
	}
	b, err := review.PageBrief(sub, "")
	if err != nil {
		t.Fatal(err)
	}
	hash := ""
	for _, d := range b.Docs {
		if d.File == review.FeatureBundle {
			hash = d.Hash
		}
	}
	if hash == "" {
		t.Fatalf("the brief carries the bundle's fingerprint, got %+v", b.Docs)
	}

	if _, err := featureReviewAnswer(f.ID, "000000000000", "yes"); err == nil {
		t.Error("a fingerprint that is not the bundle's is refused")
	}
	if _, err := featureReviewAnswer(f.ID, hash, " "); err == nil {
		t.Error("the user's words are still required")
	}
	if err := runFeatureConfirm(t, f.ID, "--hash", hash, "--answer", "yes"); err == nil || !strings.Contains(err.Error(), "--hash goes with --via review") {
		t.Errorf("--hash without --via review is refused, got %v", err)
	}
	if err := runFeatureConfirm(t, f.ID, "--via", "page", "--answer", "yes"); err == nil {
		t.Error("--via takes review only")
	}
	if err := runFeatureConfirm(t, f.ID, "--via", "review", "--hash", hash, "--answer", "reads right"); err != nil {
		t.Fatal(err)
	}
	got, err := feature.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Confirmations) != 1 {
		t.Fatalf("one confirmation recorded, got %+v", got.Confirmations)
	}
	a := got.Confirmations[0].Answer
	if !strings.Contains(a, "Confirmed on the review page, feature v1 ("+hash+")") || !strings.HasSuffix(a, "reads right") {
		t.Errorf("the answer names the version and fingerprint confirmed, got %q", a)
	}

	// An item added after the page's version is a new version to show
	// and confirm, not the one on the page.
	t2, typ2 := "global ask server", "build"
	if _, err := feature.AddItem(dir, feature.ItemSpec{Title: &t2, Type: &typ2}); err != nil {
		t.Fatal(err)
	}
	if _, err := featureReviewAnswer(f.ID, hash, "again"); err == nil {
		t.Error("the plan changed after the page's version: the old fingerprint is refused")
	}
}
