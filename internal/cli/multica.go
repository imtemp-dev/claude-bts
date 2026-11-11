package cli

import (
	"github.com/imtemp-dev/claude-bts/internal/multica"
	"github.com/imtemp-dev/claude-bts/internal/state"
)

// Every bts command and hook runs in this binary, so registering here mirrors
// every recipe phase change onto the recipe's Multica issue, if it has one.
func init() {
	state.PhaseObserver = multica.OnPhaseChange
}
