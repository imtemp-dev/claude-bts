package engine

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

// promptTextExts are the file types a model reads as instructions: skill
// and knowledge files, prompt templates.  A guard there is model behaviour.
var promptTextExts = map[string]bool{".md": true, ".mdx": true, ".txt": true, ".prompt": true, ".j2": true, ".jinja": true, ".jinja2": true}

// PromptGuardedInvariants returns the invariants, sorted, whose mutation
// broke only prompt text: the guard is words a model is told, so the
// falsifier that caught the mutation read those words, and only a model
// run shows the behaviour (trial problem #111).
func PromptGuardedInvariants(ev *state.BuildEvidence) []string {
	if ev == nil {
		return nil
	}
	var out []string
	for id, f := range ev.Falsifiers {
		if f == nil || f.Mutation == nil || len(f.Mutation.Files) == 0 {
			continue
		}
		prompt := true
		for _, p := range f.Mutation.Files {
			if !promptTextExts[strings.ToLower(filepath.Ext(p))] {
				prompt = false
				break
			}
		}
		if prompt {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

// CheckBehaviourObserved is app_proven's half for model behaviour: every
// prompt-guarded invariant is named by a planned journey
// (`bts prove plan --journey X --inv INV-...`), so that CheckAppProven's
// demand that each journey be driven on the current code, or deferred
// with an owner, covers it.  It returns "" when that holds.
func CheckBehaviourObserved(rec *state.ProveRecord, ev *state.BuildEvidence, recipeID string) string {
	guarded := PromptGuardedInvariants(ev)
	if len(guarded) == 0 {
		return ""
	}
	observed := map[string]bool{}
	if rec != nil {
		for _, j := range rec.Journeys {
			for _, inv := range j.Invariants {
				observed[inv] = true
			}
		}
	}
	var missing []string
	for _, inv := range guarded {
		if !observed[inv] {
			missing = append(missing, inv)
		}
	}
	if len(missing) == 0 {
		return ""
	}
	return fmt.Sprintf("%s %s guarded by prompt text (each mutation edited only %s), so its test read the words, not the behaviour. "+
		"Name each on a journey that drives the model through it on synthetic inputs: `bts prove plan %s --journey <name> --inv %s`, "+
		"then drive that journey, or defer it with an owner.",
		strings.Join(missing, ", "), pluralIs(len(missing)), "skill or prompt files", recipeID, strings.Join(missing, ","))
}

func pluralIs(n int) string {
	if n == 1 {
		return "is"
	}
	return "are"
}

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
		parts = append(parts, fmt.Sprintf("%s failed, recorded no passing checks, or left evidence missing (see `bts prove status`)", strings.Join(failing, ", ")))
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
