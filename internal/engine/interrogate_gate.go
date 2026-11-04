package engine

import (
	"fmt"
	"strings"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

// CheckInterrogateClean is gate interrogate_clean: an interrogate run was
// prepared and verified, and every finding that failed again on a fresh
// copy is fixed on the current code or waived with a reason. It returns
// "" when the gate is met.
func CheckInterrogateClean(run *state.InterrogateRun, currentHash, recipeID string) string {
	if run == nil {
		return fmt.Sprintf("No interrogate run. Adversaries must try to break the change before it is done: "+
			"run /bts-interrogate %s (bts interrogate prepare, the adversaries, then bts interrogate verify).", recipeID)
	}
	if run.VerifiedAt == "" {
		return fmt.Sprintf("The interrogate run was prepared but not verified. Run `bts interrogate verify %s` once every adversary has written its report.", recipeID)
	}
	var open, stale []string
	for _, f := range run.Findings {
		switch f.Status {
		case state.InterrogateReproduced:
			open = append(open, f.ID)
		case state.InterrogateFixed:
			if currentHash != "" && (f.Recheck == nil || f.Recheck.CodeHash != currentHash) {
				stale = append(stale, f.ID)
			}
		}
	}
	var parts []string
	if len(open) > 0 {
		parts = append(parts, fmt.Sprintf("%s reproduced and not fixed", strings.Join(open, ", ")))
	}
	if len(stale) > 0 {
		parts = append(parts, fmt.Sprintf("the code changed after %s was rechecked", strings.Join(stale, ", ")))
	}
	if len(parts) == 0 {
		return ""
	}
	return fmt.Sprintf("Interrogate findings are open: %s. Fix each and run `bts interrogate verify %s --recheck`; "+
		"leave one unfixed only with the user's agreement, through `bts interrogate waive %s --finding <id> --reason \"...\"`.",
		strings.Join(parts, "; "), recipeID, recipeID)
}
