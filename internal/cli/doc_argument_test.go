package cli

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

// Trial problem #119: /bts-verify runs `graph paths`, `recipe
// verify-focus` and `recipe findings carry-forward`, and only the last
// took `[recipe-id] --doc <path>`.  Two of cool-crow's verify forks wrote
// `verify-focus r-005-... --doc .../final.md` and got "unknown flag:
// --doc".

// runQuiet runs the root command with args, cmd's flags reset first, and
// its output discarded.
func runQuiet(t *testing.T, cmd *cobra.Command, args ...string) error {
	t.Helper()
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		_ = f.Value.Set(f.DefValue)
		f.Changed = false
	})
	saved := os.Stdout
	_, w, _ := os.Pipe()
	os.Stdout = w
	rootCmd.SetErr(io.Discard)
	rootCmd.SetArgs(args)
	err := rootCmd.Execute()
	_ = w.Close()
	os.Stdout = saved
	rootCmd.SetErr(nil)
	rootCmd.SetArgs(nil)
	return err
}

func TestDocArgument_VerifyFocusAndGraphPathsTakeTheCarryForwardForm(t *testing.T) {
	root, id := newTestRunFixture(t)
	doc := filepath.Join(state.RecipeDir(root, id), "final.md")
	if err := os.WriteFile(doc, []byte("# Build\n\n```mermaid\ngraph TD\n  A --> B\n```\n"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	rel := filepath.Join(".bts", "specs", "recipes", id, "final.md")

	for _, c := range []struct {
		name string
		cmd  *cobra.Command
		args []string
	}{
		{"verify-focus <doc-path>", recipeVerifyFocusCmd, []string{"recipe", "verify-focus", rel}},
		{"verify-focus <recipe-id> --doc <doc-path>", recipeVerifyFocusCmd, []string{"recipe", "verify-focus", id, "--doc", rel}},
		{"verify-focus <recipe-id> --doc <file name>", recipeVerifyFocusCmd, []string{"recipe", "verify-focus", id, "--doc", "final.md"}},
		{"verify-focus --doc <doc-path>", recipeVerifyFocusCmd, []string{"recipe", "verify-focus", "--doc", rel}},
		{"graph paths <file>", graphPathsCmd, []string{"graph", "paths", rel}},
		{"graph paths <recipe-id> --doc <file name>", graphPathsCmd, []string{"graph", "paths", id, "--doc", "final.md"}},
	} {
		if err := runQuiet(t, c.cmd, c.args...); err != nil {
			t.Errorf("%s: %v", c.name, err)
		}
	}

	if err := runQuiet(t, recipeVerifyFocusCmd, "recipe", "verify-focus", "r-001-other", "--doc", rel); err == nil || !strings.Contains(err.Error(), id) {
		t.Errorf("a recipe id that is not the document's must be refused, naming the document's recipe; got %v", err)
	}
	if err := runQuiet(t, recipeVerifyFocusCmd, "recipe", "verify-focus", rel, "--doc", rel); err == nil || !strings.Contains(err.Error(), "once") {
		t.Errorf("the document given twice must be refused; got %v", err)
	}
}
