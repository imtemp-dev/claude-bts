package cli

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// Trial problem #126: pflag reads a back-quoted word in a flag's usage as
// the name of its value, so `bts recipe override grant --help` showed
// `--gate bts recipe override list --gates`, and `bts falsifier table
// --help` showed `--write ## 2.`.  cool-crow read the first while looking
// for a way to close r-005; #119's --doc had the same fault.  A
// placeholder is one word, so the class is checked over every flag.
func TestFlagUsage_PlaceholdersAreOneWord(t *testing.T) {
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		for _, fs := range []*pflag.FlagSet{c.Flags(), c.PersistentFlags()} {
			fs.VisitAll(func(f *pflag.Flag) {
				if name, _ := pflag.UnquoteUsage(f); strings.ContainsAny(name, " \t") {
					t.Errorf("%s --%s shows its value as %q: a back-quoted phrase in its usage became the placeholder",
						c.CommandPath(), f.Name, name)
				}
			})
		}
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(rootCmd)
}
