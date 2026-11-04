package state

import (
	"errors"
	"os"
	"path/filepath"
)

// BuildEvidence is what the build recipe's executables recorded, in the
// recipe's tracked directory as build-evidence.json. The build recipe takes
// its truth from runs, not from rounds of reading prose: whether the
// skeleton compiles, and whether each invariant's falsifier fails on the
// skeleton and passes on the finished code.
//
// Only the CLI writes it (`bts skeleton check`, `bts falsifier run|check`).
// The stop hook reads it. A hand edit is detectable: every run carries the
// code hash it ran on, and the gates recompute that hash.
type BuildEvidence struct {
	Skeleton   *CommandRun           `json:"skeleton,omitempty"`
	Falsifiers map[string]*Falsifier `json:"falsifiers,omitempty"` // by invariant id, e.g. INV-003
}

// CommandRun is one execution of a recorded command.
type CommandRun struct {
	Command  string `json:"command"`
	ExitCode int    `json:"exit_code"`
	TimedOut bool   `json:"timed_out,omitempty"`
	// CodeHash is engine.CodeHash when the command ran: the recipe's code
	// files as they were, so a gate can tell whether the run still
	// describes the code on disk.
	CodeHash string `json:"code_hash,omitempty"`
	RunAt    string `json:"run_at"`
}

// Passed reports whether the run exited 0 without timing out.
func (r *CommandRun) Passed() bool { return r != nil && r.ExitCode == 0 && !r.TimedOut }

// Falsifier is one invariant's test: the command that runs it, its run on
// the skeleton (expected to fail, for the invariant's own reason), and its
// latest run on the implementation (expected to pass).
type Falsifier struct {
	Command string      `json:"command"`
	Red     *CommandRun `json:"red,omitempty"`
	Green   *CommandRun `json:"green,omitempty"`
}

// BuildEvidencePath is the evidence file for a recipe.
func BuildEvidencePath(root, recipeID string) string {
	return filepath.Join(RecipeDir(root, recipeID), "build-evidence.json")
}

// LoadBuildEvidence reads a recipe's evidence. A missing file is an empty
// record, not an error: a recipe that has run nothing yet has no evidence.
func LoadBuildEvidence(root, recipeID string) (*BuildEvidence, error) {
	var ev BuildEvidence
	if err := ReadJSON(BuildEvidencePath(root, recipeID), &ev); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &BuildEvidence{Falsifiers: map[string]*Falsifier{}}, nil
		}
		return nil, err
	}
	if ev.Falsifiers == nil {
		ev.Falsifiers = map[string]*Falsifier{}
	}
	return &ev, nil
}

// SaveBuildEvidence writes a recipe's evidence.
func SaveBuildEvidence(root, recipeID string, ev *BuildEvidence) error {
	return WriteJSON(BuildEvidencePath(root, recipeID), ev)
}
