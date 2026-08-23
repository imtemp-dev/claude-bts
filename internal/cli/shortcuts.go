package cli

import (
	"strings"

	"github.com/spf13/cobra"
)

// Shortcuts are top-level aliases for the commands typed most often during a
// recipe. The nested forms stay: `jig ls` and `jig recipe list` are the same
// command, and scripts written against the long form keep working.
//
// Only the hot path gets a shortcut. Commands typed once per project (init,
// doctor, migrate) keep their full name, where the extra clarity is worth more
// than the saved keystrokes.
var shortcuts = []struct {
	name   string
	short  string
	target func() *cobra.Command
}{
	{"ls", "List recipes (alias for `recipe list`)", func() *cobra.Command { return recipeListCmd }},
	{"log", "Record an action or phase (alias for `recipe log`)", func() *cobra.Command { return recipeLogCmd }},
	{"new", "Create a recipe (alias for `recipe create`)", func() *cobra.Command { return recipeCreateCmd }},
	{"ask", "Record a question only the user can answer (alias for `recipe decision hold`)",
		func() *cobra.Command { return recipeDecisionHoldCmd }},
	{"ans", "Answer a held question (alias for `recipe decision resolve`)",
		func() *cobra.Command { return recipeDecisionResolveCmd }},
}

// registerShortcuts must run after every init() has registered the target
// commands and their flags — it copies those flag sets rather than redeclaring
// them, so a flag added to `recipe log` reaches `jig log` for free.
func registerShortcuts() {
	for _, s := range shortcuts {
		target := s.target()
		alias := &cobra.Command{
			Use:     s.name + argSuffix(target.Use),
			Short:   s.short,
			GroupID: "recipe",
			Args:    target.Args,
			RunE:    target.RunE,
		}
		alias.Flags().AddFlagSet(target.Flags())
		rootCmd.AddCommand(alias)
	}
}

// argSuffix keeps the target's argument hints ("[recipe-id]", "<recipe-id> <key>")
// so `jig ans --help` documents the same positional arguments as the long form.
func argSuffix(use string) string {
	if i := strings.IndexByte(use, ' '); i >= 0 {
		return use[i:]
	}
	return ""
}
