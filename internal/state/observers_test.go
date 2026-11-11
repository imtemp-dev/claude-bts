package state

// setPhaseObserversForTest replaces the observers and returns a function
// that restores them.
func setPhaseObserversForTest(fs ...func(root, recipeID, from, to string)) (restore func()) {
	prev := phaseObservers
	phaseObservers = fs
	return func() { phaseObservers = prev }
}
