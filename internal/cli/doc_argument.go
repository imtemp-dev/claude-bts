package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

// /bts-verify runs `graph paths`, `recipe verify-focus` and `recipe
// findings carry-forward` side by side, and only the last took
// `[recipe-id] --doc <doc>`.  Two of cool-crow's verify forks wrote all
// three in that form, and the first two refused it with "unknown flag:
// --doc" (trial problem #119).  The first two now take it too.
func init() {
	for _, c := range []*cobra.Command{recipeVerifyFocusCmd, graphPathsCmd} {
		c.Flags().String("doc", "", "The document, as `bts recipe findings carry-forward` takes it; with a recipe id, a bare file name is that recipe's document")
	}
}

// docArgument reads the document a command works on, given either as
// `<doc-path>` or as `[recipe-id] --doc <doc>`.  With a recipe id, a bare
// file name is that recipe's document, and a path inside another recipe
// is refused.
func docArgument(cmd *cobra.Command, args []string) (string, error) {
	doc, _ := cmd.Flags().GetString("doc")
	forms := fmt.Sprintf("`%s <doc-path>` or `%s [recipe-id] --doc <doc-path>`", cmd.CommandPath(), cmd.CommandPath())
	if doc == "" {
		if len(args) != 1 {
			return "", fmt.Errorf("name the document: %s", forms)
		}
		return args[0], nil
	}
	if len(args) == 0 {
		return doc, nil
	}
	id := args[0]
	if len(args) > 1 || strings.Contains(id, "/") || strings.HasSuffix(id, ".md") {
		return "", fmt.Errorf("give the document once: %s", forms)
	}
	if from := state.RecipeIDFromDocPath(doc); from != "" && from != id {
		return "", fmt.Errorf("%s is recipe %s's document, not %s's", doc, from, id)
	}
	if strings.Contains(doc, "/") {
		return doc, nil
	}
	if _, err := os.Stat(doc); err == nil {
		return doc, nil
	}
	cwd, _ := os.Getwd()
	root, err := state.FindRoot(cwd)
	if err != nil {
		return doc, nil
	}
	return filepath.Join(state.RecipeDir(root, id), doc), nil
}
