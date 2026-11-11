package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/imtemp-dev/claude-bts/internal/state"
	"github.com/imtemp-dev/claude-bts/internal/template"
	"github.com/imtemp-dev/claude-bts/pkg/version"
)

// refreshTemplatesBeforeCreate brings the project's templates to the
// binary's version before a recipe is created, when no recipe holds them.
// Templates moved only at SessionStart or by `bts update`, so a session
// that outlived its last recipe created the next one on old skills:
// cool-crow's r-006 started on templates 931fca8 under binary 0b8ec2f,
// whose gates its skills did not describe (trial problem #128).  An active
// recipe still holds them (trial problem #55).  A project `bts init` never
// set up records no version, and is left alone.  Everything goes to stderr:
// create's stdout is the recipe id a caller captures.
func refreshTemplatesBeforeCreate(root string) {
	recorded, err := os.ReadFile(filepath.Join(root, ".bts", "config", ".template-version"))
	if err != nil {
		return
	}
	if active, _ := state.GetActiveRecipe(root); active != nil {
		return
	}
	current := version.GetTemplateVersion()
	res, err := template.Update(root, current)
	for _, w := range res.Warnings {
		fmt.Fprintf(os.Stderr, "[bts] warning: %s\n", w)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "[bts] warning: templates were not updated (%v); run `bts update` before the recipe's first step\n", err)
		return
	}
	if res.Deployed {
		old := strings.TrimSpace(string(recorded))
		if old == "" {
			old = "an unrecorded version"
		}
		fmt.Fprintf(os.Stderr, "[bts] Templates updated from %s to %s before creating the recipe (%d files), so it starts on the "+
			"skills this binary checks.  Rules already loaded in this session refresh at its next start or compaction.\n",
			old, current, len(res.Files))
	}
}
