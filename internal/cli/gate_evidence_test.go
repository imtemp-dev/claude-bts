package cli

import (
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

func evidenceFixture(t *testing.T, entries []state.VerifyLogEntry) string {
	t.Helper()
	return newRecipeFixture(t, "r-ev", "verify", 0, len(entries), entries)
}

// A harness that never emits subagent events marks every round "none".
// Warning on all of them would say nothing about the project, so the
// check must stay silent.
func TestCheckGateEvidence_NoEvidenceAnywhereIsSilent(t *testing.T) {
	root := evidenceFixture(t, []state.VerifyLogEntry{
		{Iteration: 1, Doc: "draft.md", AgentEvidence: state.AgentEvidenceNone},
		{Iteration: 2, Doc: "draft.md", AgentEvidence: state.AgentEvidenceNone},
	})
	if issues := checkGateEvidence(root, "r-ev"); len(issues) != 0 {
		t.Fatalf("signal unavailable in this environment — must not accuse; got %v", issues)
	}
}

// Once the same recipe HAS produced rounds with evidence, an
// evidence-free round is a genuine outlier.
func TestCheckGateEvidence_MixedReportsTheBareRounds(t *testing.T) {
	root := evidenceFixture(t, []state.VerifyLogEntry{
		{Iteration: 1, Doc: "draft.md", AgentEvidence: state.AgentEvidenceObserved},
		{Iteration: 2, Doc: "draft.md", AgentEvidence: state.AgentEvidenceNone},
		{Iteration: 3, Doc: "draft.md", AgentEvidence: state.AgentEvidenceObserved},
	})
	issues := checkGateEvidence(root, "r-ev")
	if len(issues) != 1 {
		t.Fatalf("expected one issue, got %v", issues)
	}
	if issues[0].level != "warning" {
		t.Errorf("evidence about how a round was produced is not a verdict; want warning, got %s", issues[0].level)
	}
	if want := "#2 draft.md"; !contains(issues[0].message, want) {
		t.Errorf("message must name the bare round (%s), got %q", want, issues[0].message)
	}
}

// All rounds carry evidence → nothing to report.
func TestCheckGateEvidence_AllObservedIsSilent(t *testing.T) {
	root := evidenceFixture(t, []state.VerifyLogEntry{
		{Iteration: 1, Doc: "draft.md", AgentEvidence: state.AgentEvidenceObserved},
	})
	if issues := checkGateEvidence(root, "r-ev"); len(issues) != 0 {
		t.Fatalf("expected silence, got %v", issues)
	}
}

// Rounds written before the field existed carry no claim either way and
// must not be counted as bare.
func TestCheckGateEvidence_LegacyRoundsAreNotBare(t *testing.T) {
	root := evidenceFixture(t, []state.VerifyLogEntry{
		{Iteration: 1, Doc: "draft.md", AgentEvidence: state.AgentEvidenceObserved},
		{Iteration: 2, Doc: "draft.md"}, // legacy: no field
	})
	if issues := checkGateEvidence(root, "r-ev"); len(issues) != 0 {
		t.Fatalf("a legacy round records no claim; got %v", issues)
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}
