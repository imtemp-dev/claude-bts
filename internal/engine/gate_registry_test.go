package engine

import "testing"

// Every gate the CLI will grant has to be one the CLI will also list.
// `bts recipe override list --gates` is the command every block message
// points at, and it walked HardGates only — so falsifier_assigned, which
// lives in InvariantGates, would have been grantable and undiscoverable
// at the same time.
func TestEveryOverridableGateIsListed(t *testing.T) {
	listed := map[string]bool{}
	for _, g := range OverridableGates() {
		listed[g.ID] = true
	}
	for id := range overridableGates {
		if !listed[id] {
			t.Errorf("%s is overridable but `override list --gates` does not show it", id)
		}
	}
}

// falsifier_assigned blocks <bts>DONE</bts> at stop.go step 2c-bis. It
// was the only DONE-blocking gate with no escape hatch: overrideFooter
// returns "" for a gate the CLI would refuse, so the block message named
// no way forward and `override grant --gate falsifier_assigned` answered
// "unknown gate". Its sibling one step above, deferred_minors_declared,
// has always been overridable — the same kind of judgement about the
// same document.
func TestFalsifierAssignedIsOverridable(t *testing.T) {
	if !IsOverridableGate("falsifier_assigned") {
		t.Error("a DONE-blocking gate must have a recordable escape hatch")
	}
	// It fires on invariants, not on findings, so `override grant` wants
	// --no-findings rather than an ID that could not exist.
	if GateExcusesFindings("falsifier_assigned") {
		t.Error("falsifier_assigned is not about a set of findings")
	}
	// It is about one spec document, so the grant must name it.
	if !GateIsDocumentScoped("falsifier_assigned") {
		t.Error("an override without --doc would be a permanent project-wide bypass")
	}
}
