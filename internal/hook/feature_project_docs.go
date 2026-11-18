package hook

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

// projectWideDocs are the documents under .bts/specs/ that describe the
// whole project rather than one recipe.  /bts-status writes them at every
// recipe's finalization.
var projectWideDocs = []string{"project-status.md", "project-map.md"}

// featureItemProjectDoc refuses a write of a project-wide document from
// the recipe of a feature item, and returns "" for every other write.
// Each item works on its own branch, which holds only its own recipe, so
// its copy of a project-wide document is partial, and the items' pull
// requests collided on it: in the v0.32.0 Orca smoke one item's branch
// added project-status.md and project-map.md while the other's worker left
// them uncommitted to dodge the add/add conflict (bts #173).  The feature
// (`bts feature status`) is the view across items, and the base branch
// regenerates these documents after the items merge.
func featureItemProjectDoc(recipe *state.RecipeState, input *HookInput) string {
	if recipe == nil || recipe.Feature == "" {
		return ""
	}
	fp, _ := input.ToolInput["file_path"].(string)
	if fp == "" {
		return ""
	}
	slashed := filepath.ToSlash(filepath.Clean(fp))
	for _, name := range projectWideDocs {
		if strings.HasSuffix(slashed, "/.bts/specs/"+name) || slashed == ".bts/specs/"+name {
			return fmt.Sprintf("[bts] %s is item %s of feature %s, so it leaves .bts/specs/%s alone: each item's branch holds "+
				"only its own recipe, and the items' pull requests would collide on a project-wide file (bts #173).  "+
				"Report the recipe's status in your reply instead; `bts feature status %s` is the view across items, and the "+
				"base branch regenerates %s after the items merge.", recipe.ID, recipe.FeatureItem, recipe.Feature, name, recipe.Feature, name)
		}
	}
	return ""
}
