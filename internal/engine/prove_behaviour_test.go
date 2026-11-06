package engine

import (
	"reflect"
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

// Trial problem #111: neat-wolf's review skill owns 18 of its 22
// invariants in SKILL.md sections, and each is falsified by a test of the
// section's words; P1 then "kills" it by editing those words.  Nothing
// asked for the behaviour itself in a model run.  An invariant whose
// mutation only touched prompt text is guarded by the model, so a driven
// journey has to name it.
func behaviourEvidence() *state.BuildEvidence {
	return &state.BuildEvidence{Falsifiers: map[string]*state.Falsifier{
		"INV-001": {Command: "pytest -k pages", Mutation: &state.MutationRun{
			Files: []string{"agent_server/tools/pdf_tool.py"}, Matched: true}},
		"INV-018": {Command: "pytest -k withheld", Mutation: &state.MutationRun{
			Files: []string{"agent_server/agents/specialists/onboarding/skills/onboarding-document-review/SKILL.md"}, Matched: true}},
		"INV-020": {Command: "pytest -k readonly", Green: &state.CommandRun{ExitCode: 0}},
	}}
}

func TestPromptGuardedInvariants(t *testing.T) {
	if got := PromptGuardedInvariants(behaviourEvidence()); !reflect.DeepEqual(got, []string{"INV-018"}) {
		t.Fatalf("only the invariant whose guard is prompt text, got %v", got)
	}
}

func TestCheckBehaviourObserved(t *testing.T) {
	ev := behaviourEvidence()

	none := &state.ProveRecord{Journeys: map[string]*state.Journey{"review-synthetic": {}}}
	if r := CheckBehaviourObserved(none, ev, "r-111"); !strings.Contains(r, "INV-018") || strings.Contains(r, "INV-001") {
		t.Errorf("a planned journey that names no invariant leaves INV-018 unobserved, got %q", r)
	}

	named := &state.ProveRecord{Journeys: map[string]*state.Journey{"review-synthetic": {Invariants: []string{"INV-018"}}}}
	if r := CheckBehaviourObserved(named, ev, "r-111"); r != "" {
		t.Errorf("a journey naming INV-018 observes it, got %q", r)
	}

	noSurface := &state.ProveRecord{NoSurface: "a skill only"}
	if r := CheckBehaviourObserved(noSurface, ev, "r-111"); !strings.Contains(r, "INV-018") {
		t.Errorf("a skill's behaviour is a surface: no-surface cannot cover INV-018, got %q", r)
	}

	if r := CheckBehaviourObserved(none, &state.BuildEvidence{Falsifiers: map[string]*state.Falsifier{}}, "r-111"); r != "" {
		t.Errorf("no prompt-guarded invariant, nothing to report, got %q", r)
	}
}
