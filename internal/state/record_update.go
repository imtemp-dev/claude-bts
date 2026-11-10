package state

import "errors"

// A command that runs something long (a drive, every falsifier, a
// mutation in a copy, an adversary's repro) used to read its record before
// the run and write that copy back after it, so what another bts command
// recorded in between was lost: neat-wolf deferred one journey four
// seconds into a drive of another, and the drive's save would have dropped
// the deferral (trial problem #117).  These read the record again after
// the run, let the command apply only its own change, and write it back.

// UpdateProveRecord applies change to prove.json as it is now.
func UpdateProveRecord(root, recipeID string, change func(*ProveRecord) error) error {
	rec, err := LoadProveRecord(root, recipeID)
	if err != nil {
		return err
	}
	if rec == nil {
		rec = &ProveRecord{}
	}
	if rec.Journeys == nil {
		rec.Journeys = map[string]*Journey{}
	}
	if err := change(rec); err != nil {
		return err
	}
	return SaveProveRecord(root, recipeID, rec)
}

// UpdateBuildEvidence applies change to build-evidence.json as it is now.
func UpdateBuildEvidence(root, recipeID string, change func(*BuildEvidence) error) error {
	ev, err := LoadBuildEvidence(root, recipeID)
	if err != nil {
		return err
	}
	if err := change(ev); err != nil {
		return err
	}
	return SaveBuildEvidence(root, recipeID, ev)
}

// UpdateInterrogateRun applies change to interrogate.json as it is now.
func UpdateInterrogateRun(root, recipeID string, change func(*InterrogateRun) error) error {
	run, err := LoadInterrogateRun(root, recipeID)
	if err != nil {
		return err
	}
	if run == nil {
		return errors.New("interrogate.json is gone; nothing was recorded")
	}
	if err := change(run); err != nil {
		return err
	}
	return SaveInterrogateRun(root, recipeID, run)
}
