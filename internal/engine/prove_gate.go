package engine

import (
	"fmt"
	"sort"
	"strings"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

// CheckAppProven is gate app_proven: the recipe planned the user journeys
// it touches, and each was driven through the real app on the current
// code with its evidence left behind, or deferred with a reason and an
// owner; or the change was recorded as having no user-facing surface.
// It returns "" when the gate is met.
func CheckAppProven(rec *state.ProveRecord, currentHash, recipeID string) string {
	if rec == nil {
		return fmt.Sprintf("No drive record. Plan the user journeys this change touches from the project's verify-<app> skill "+
			"(/bts-prove %s; /bts-verification-skill makes one if the project has none), or record "+
			"`bts prove plan %s --no-surface \"<reason>\"` for a change with nothing a user drives.", recipeID, recipeID)
	}
	if strings.TrimSpace(rec.NoSurface) != "" {
		return ""
	}
	if len(rec.Journeys) == 0 {
		return fmt.Sprintf("No journeys planned. Run `bts prove plan %s --journey <name>` for each user path this change touches.", recipeID)
	}
	names := make([]string, 0, len(rec.Journeys))
	for n := range rec.Journeys {
		names = append(names, n)
	}
	sort.Strings(names)
	var undriven, failing, stale []string
	for _, n := range names {
		j := rec.Journeys[n]
		switch {
		case j.Deferred != nil:
			continue
		case j.Run == nil:
			undriven = append(undriven, n)
		case !j.Passed():
			failing = append(failing, n)
		case currentHash != "" && j.Run.CodeHash != currentHash:
			stale = append(stale, n)
		}
	}
	var parts []string
	if len(undriven) > 0 {
		parts = append(parts, fmt.Sprintf("%s not driven", strings.Join(undriven, ", ")))
	}
	if len(failing) > 0 {
		parts = append(parts, fmt.Sprintf("%s failed or left evidence missing", strings.Join(failing, ", ")))
	}
	if len(stale) > 0 {
		parts = append(parts, fmt.Sprintf("the code changed after %s was driven", strings.Join(stale, ", ")))
	}
	if len(parts) == 0 {
		return ""
	}
	return fmt.Sprintf("The app is not shown working: %s. Drive each with `bts prove run %s --journey <name> --cmd \"<drive>\" --evidence <path>`, "+
		"or, when it cannot run here, `bts prove defer %s --journey <name> --reason \"...\" --owner \"...\"`.",
		strings.Join(parts, "; "), recipeID, recipeID)
}
