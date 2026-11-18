package hook

import (
	"fmt"

	"github.com/imtemp-dev/claude-bts/internal/engine"
	"github.com/imtemp-dev/claude-bts/internal/orca"
	"github.com/imtemp-dev/claude-bts/internal/state"
)

// inOrca reports whether the session runs in an Orca terminal; a variable
// so tests need not set the process environment.
var inOrca = orca.InOrca

// orcaContinueOffer is the sentence that offers `bts orca continue` to a
// session running in Orca, or "" when the session is not in Orca, the
// project turned orca.launch off, or the recipe has nothing to continue.
// It is offered where a fresh session pays off: a spec just finalized,
// and a context just compacted.
func orcaContinueOffer(root string, recipe *state.RecipeState) string {
	if recipe == nil || !inOrca() {
		return ""
	}
	settings, err := engine.LoadSettings(root)
	if err != nil {
		return ""
	}
	how := "ask the user first; on their yes add --yes"
	switch settings.Orca.LaunchPolicy() {
	case engine.OrcaLaunchOff:
		return ""
	case engine.OrcaLaunchAuto:
		how = "orca.launch is auto, so it launches at once"
	}
	return fmt.Sprintf("This session runs in Orca: a fresh session can take %s over with `bts orca continue %s` (%s), "+
		"after which this session ends its turn.", recipe.ID, recipe.ID, how)
}
