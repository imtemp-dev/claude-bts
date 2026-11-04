package state

import (
	"errors"
	"os"
	"path/filepath"
)

// InterrogateRun is P3 of the prove stage (docs/research/prove-stage-design.md):
// adversaries, each in its own copy of the project, try to break the
// change, and a finding counts only when bts reran its command in a fresh
// copy of the current code and the command failed. Written by `bts
// interrogate prepare|verify|waive`; read by the gate interrogate_clean.
type InterrogateRun struct {
	Adversaries []*Adversary          `json:"adversaries"`
	Findings    []*InterrogateFinding `json:"findings,omitempty"`
	PreparedAt  string                `json:"prepared_at"`
	// VerifiedAt is set once `bts interrogate verify` has read every
	// adversary's report; until then the run proves nothing.
	VerifiedAt string `json:"verified_at,omitempty"`
}

// Adversary is one seat: the angle it attacks from, the copy of the
// project it works in, and where it writes its report.
type Adversary struct {
	Name   string `json:"name"`
	Angle  string `json:"angle"`
	Copy   string `json:"copy"`   // absolute path of its private copy
	Report string `json:"report"` // relative to the recipe directory
}

// Finding statuses.
const (
	InterrogateReproduced = "reproduced" // failed again on a fresh copy: open until fixed or waived
	InterrogateDropped    = "dropped"    // did not fail again, or had no command
	InterrogateFixed      = "fixed"      // its command passes on the current code
	InterrogateWaived     = "waived"     // left unfixed with a reason the user accepted
)

// InterrogateFinding is one adversary claim and what bts made of it.
type InterrogateFinding struct {
	ID        string `json:"id"` // F-001
	Adversary string `json:"adversary"`
	Title     string `json:"title"`
	// Command exits non-zero while the code is wrong (a failing test or an
	// assertion script); it runs from the project root of a copy.
	Command  string `json:"command"`
	Expected string `json:"expected,omitempty"` // what the intent requires
	Observed string `json:"observed,omitempty"` // what the adversary saw
	// ReproFiles are files the adversary wrote for the command, relative to
	// the project root; bts keeps them under interrogate/repro/<id>/.
	ReproFiles  []string    `json:"repro_files,omitempty"`
	Status      string      `json:"status"`
	DropReason  string      `json:"drop_reason,omitempty"`
	Verify      *CommandRun `json:"verify,omitempty"`  // the rerun on a fresh copy
	Recheck     *CommandRun `json:"recheck,omitempty"` // the rerun after a fix
	WaiveReason string      `json:"waive_reason,omitempty"`
}

// InterrogatePath is the run record for a recipe.
func InterrogatePath(root, recipeID string) string {
	return filepath.Join(RecipeDir(root, recipeID), "interrogate.json")
}

// LoadInterrogateRun reads a recipe's run; a missing file is (nil, nil).
func LoadInterrogateRun(root, recipeID string) (*InterrogateRun, error) {
	var run InterrogateRun
	if err := ReadJSON(InterrogatePath(root, recipeID), &run); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	return &run, nil
}

// SaveInterrogateRun writes a recipe's run.
func SaveInterrogateRun(root, recipeID string, run *InterrogateRun) error {
	return WriteJSON(InterrogatePath(root, recipeID), run)
}
