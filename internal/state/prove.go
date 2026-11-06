package state

import (
	"errors"
	"os"
	"path/filepath"
)

// ProveRecord is P2 of the prove stage (docs/research/prove-stage-design.md):
// the user journeys a recipe touches, each driven through the real app by
// the project's verify-<app> skill and recorded by `bts prove run`, or
// deferred with a reason and an owner. Read by the gate app_proven.
type ProveRecord struct {
	// NoSurface is the reason a change has no user-facing surface to drive
	// (a library internal, a refactor with no behaviour change).
	NoSurface string              `json:"no_surface,omitempty"`
	Journeys  map[string]*Journey `json:"journeys,omitempty"`
}

// Journey is one user path, from the verify skill's feature map.
type Journey struct {
	Feature  string      `json:"feature,omitempty"` // feature-map file it comes from
	Run      *CommandRun `json:"run,omitempty"`     // the last drive, stamped with the code hash
	Evidence []string    `json:"evidence,omitempty"`
	// EvidenceMissing lists evidence paths the last drive promised but did
	// not leave behind; a drive with missing evidence does not pass.
	EvidenceMissing []string  `json:"evidence_missing,omitempty"`
	Deferred        *Deferral `json:"deferred,omitempty"`
	// Verdict is the file the drive wrote its checks to, and what bts read
	// there: how many checks, the ones that failed, or why the file could
	// not be read.  A journey passes on its checks, not on its exit code
	// alone (trial problem #29).  A record with no Verdict predates it.
	Verdict      string   `json:"verdict,omitempty"`
	Checks       int      `json:"checks,omitempty"`
	ChecksFailed []string `json:"checks_failed,omitempty"`
	VerdictError string   `json:"verdict_error,omitempty"`
	// Invariants are the invariants this journey's run observes
	// (`bts prove plan --journey X --inv INV-018`).  An invariant whose
	// guard is prompt text holds only when the model follows it, so a
	// driven journey must name it (trial problem #111).
	Invariants []string `json:"invariants,omitempty"`
}

// Passed reports whether the journey's last drive exited 0, left every
// promised piece of evidence and, in its verdict, recorded at least one
// check and no failed one.
func (j *Journey) Passed() bool {
	if j == nil || !j.Run.Passed() || len(j.EvidenceMissing) > 0 {
		return false
	}
	if j.Verdict == "" {
		return true // recorded before verdicts were read
	}
	return j.VerdictError == "" && j.Checks > 0 && len(j.ChecksFailed) == 0
}

// Deferral is a journey that cannot run here (it needs an installed
// platform app, real credentials or a real target), with who runs it there.
type Deferral struct {
	Reason string `json:"reason"`
	Owner  string `json:"owner"`
	At     string `json:"at"`
}

// ProvePath is the record for a recipe.
func ProvePath(root, recipeID string) string {
	return filepath.Join(RecipeDir(root, recipeID), "prove.json")
}

// LoadProveRecord reads a recipe's record; a missing file is (nil, nil).
func LoadProveRecord(root, recipeID string) (*ProveRecord, error) {
	var rec ProveRecord
	if err := ReadJSON(ProvePath(root, recipeID), &rec); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	if rec.Journeys == nil {
		rec.Journeys = map[string]*Journey{}
	}
	return &rec, nil
}

// SaveProveRecord writes a recipe's record.
func SaveProveRecord(root, recipeID string, rec *ProveRecord) error {
	return WriteJSON(ProvePath(root, recipeID), rec)
}
