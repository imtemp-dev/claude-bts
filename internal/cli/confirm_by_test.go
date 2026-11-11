package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/feature"
	"github.com/imtemp-dev/claude-bts/internal/state"
)

// A feature item's intent is confirmed by the feature's confirmation, its
// scope by the manager; both record where the confirmation came from.
func TestRecipeConfirmBy(t *testing.T) {
	t.Setenv(feature.StoreEnv, t.TempDir())
	_, wts := featureRepo(t, 1)
	a := wts[0]
	fid, _, items := confirmedFeature(t, a, []string{"Door"}, nil)
	id, err := createFeatureRecipe(a, fid, items[0], "", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, doc := range []string{"intent.md", "scope.md"} {
		_ = os.WriteFile(filepath.Join(state.RecipeDir(a, id), doc), []byte("# Doc\n\nStatus: DRAFT\n\nx\n"), 0644)
	}

	if _, err := confirmAnswer(a, id, "scope", "feature", ""); err == nil {
		t.Fatal("--by feature must not confirm a scope")
	}
	if _, err := confirmAnswer(a, id, "intent", "feature", "words"); err == nil {
		t.Fatal("--by feature takes no --answer")
	}
	if _, err := confirmAnswer(a, id, "scope", "manager", ""); err == nil {
		t.Fatal("--by manager needs the manager's words")
	}

	t.Chdir(a)
	t.Cleanup(func() { resetFlags(recipeConfirmCmd); rootCmd.SetArgs(nil) })
	rootCmd.SetArgs([]string{"recipe", "confirm", id, "intent", "--by", "feature"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("confirm intent --by feature: %v", err)
	}
	resetFlags(recipeConfirmCmd)
	rootCmd.SetArgs([]string{"recipe", "confirm", id, "scope", "--by", "manager", "--answer", "fits i1 and the 404 contract"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("confirm scope --by manager: %v", err)
	}

	decisions, _ := state.LoadDecisions(a, id)
	var got []string
	for _, d := range decisions {
		got = append(got, d.Answer)
	}
	all := strings.Join(got, "\n")
	if !strings.Contains(all, "Confirmed through feature "+fid+", confirmation 1") || !strings.Contains(all, ": go") {
		t.Fatalf("the intent must cite the feature's confirmation and the user's words:\n%s", all)
	}
	if !strings.Contains(all, "Confirmed by the feature manager within feature "+fid+", confirmation 1: fits i1") {
		t.Fatalf("the scope must say the manager confirmed it:\n%s", all)
	}
	intent, _ := os.ReadFile(filepath.Join(state.RecipeDir(a, id), "intent.md"))
	if state.DocStatus(string(intent)) != "CONFIRMED" {
		t.Fatal("intent.md must read CONFIRMED")
	}

	// A recipe bound to no feature has no feature to confirm through.
	plain, _ := executeRecipeCreate(wts[0], "fix", "other", "", "", false)
	_ = os.WriteFile(filepath.Join(state.RecipeDir(a, plain), "intent.md"), []byte("x"), 0644)
	if _, err := confirmAnswer(a, plain, "intent", "feature", ""); err == nil {
		t.Fatal("--by feature on an unbound recipe must fail")
	}
}
