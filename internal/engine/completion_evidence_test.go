package engine

import (
	"fmt"
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

var allDims = []string{"audit", "simulate", "verify"}

// cleanRound builds a qualifying round. Each one carries its own
// verification hash, which is the normal case: a round writes a
// verification.md and logs from it. Tests that care about independence
// override the field explicitly.
func cleanRound(iter int, hash string, dims []string, full bool) state.VerifyLogEntry {
	return state.VerifyLogEntry{
		Iteration: iter, Doc: "draft.md", Critical: 0, Major: 0,
		FullPass: full, Dimensions: dims, DocHash: hash,
		VerificationHash: fmt.Sprintf("sha256:v%d", iter), Status: "converged",
	}
}

func TestCompletionEvidence_ConfirmedByTwoRoundsOnOneRevision(t *testing.T) {
	ev := EvaluateCompletionEvidence([]state.VerifyLogEntry{
		cleanRound(1, "sha256:aaa", allDims, true),
		cleanRound(2, "sha256:aaa", allDims, true),
	}, 2)
	if !ev.Confirmed {
		t.Fatalf("want confirmed, got %+v", ev)
	}
	if ev.Have != 2 {
		t.Errorf("have = %d, want 2", ev.Have)
	}
}

func TestCompletionEvidence_SingleCleanRoundIsNotEvidence(t *testing.T) {
	ev := EvaluateCompletionEvidence([]state.VerifyLogEntry{
		cleanRound(1, "sha256:aaa", allDims, true),
	}, 2)
	if ev.Confirmed {
		t.Fatal("one clean round must not confirm")
	}
	if !strings.Contains(ev.Reason, "1 of 2") {
		t.Errorf("reason should name the shortfall, got %q", ev.Reason)
	}
	if ev.Remedy == "" {
		t.Error("a block must say what to run next")
	}
}

// The defect that made the replication gate ornamental: it counted rows,
// and two rows are produced by running one `bts recipe log` twice. The
// document was never read a second time, verification.md never changed,
// and "two agreeing rounds" was one sample recorded twice.
func TestCompletionEvidence_RerecordingOneRoundIsNotReplication(t *testing.T) {
	a := cleanRound(1, "sha256:aaa", allDims, true)
	b := cleanRound(2, "sha256:aaa", allDims, true)
	b.VerificationHash = a.VerificationHash // same verification.md on disk

	ev := EvaluateCompletionEvidence([]state.VerifyLogEntry{a, b}, 2)
	if ev.Confirmed {
		t.Fatalf("re-recording one measurement must not confirm it, got %+v", ev)
	}
	if ev.Have != 1 {
		t.Errorf("have = %d, want 1 — the duplicate must not count", ev.Have)
	}
	if !strings.Contains(ev.Reason, "recorded twice") {
		t.Errorf("reason must say what went wrong, got %q", ev.Reason)
	}
}

// Two rounds that both recorded nothing are equally indistinguishable.
// The first may be blank — there is nothing yet for it to differ from —
// so confirm_passes=1 keeps working on a log written before the field
// was stamped.
func TestCompletionEvidence_BlankVerificationHashesDoNotCorroborate(t *testing.T) {
	a := cleanRound(1, "sha256:aaa", allDims, true)
	b := cleanRound(2, "sha256:aaa", allDims, true)
	a.VerificationHash, b.VerificationHash = "", ""

	ev := EvaluateCompletionEvidence([]state.VerifyLogEntry{a, b}, 2)
	if ev.Confirmed {
		t.Fatalf("two unidentified rounds must not confirm each other, got %+v", ev)
	}
	if ev.Have != 1 {
		t.Errorf("have = %d, want 1", ev.Have)
	}
	if !strings.Contains(ev.Reason, "no verification_hash") {
		t.Errorf("reason must name the missing artefact, got %q", ev.Reason)
	}

	if ev := EvaluateCompletionEvidence([]state.VerifyLogEntry{a}, 1); !ev.Confirmed {
		t.Errorf("a lone legacy round must still satisfy confirm_passes=1, got %+v", ev)
	}
}

// An edit between the clean rounds changes the revision, so they are not
// two measurements of the same document.
func TestCompletionEvidence_RevisionChangeResetsTheCount(t *testing.T) {
	ev := EvaluateCompletionEvidence([]state.VerifyLogEntry{
		cleanRound(1, "sha256:aaa", allDims, true),
		cleanRound(2, "sha256:bbb", allDims, true),
	}, 2)
	if ev.Confirmed {
		t.Fatal("rounds on different revisions must not confirm each other")
	}
	if ev.Have != 1 {
		t.Errorf("have = %d, want 1 (only the latest revision counts)", ev.Have)
	}
}

// A dirty round in the middle breaks the run even when the revision
// matches — the document was not clean on that reading.
func TestCompletionEvidence_InterveningDirtyRoundBreaksTheRun(t *testing.T) {
	dirty := cleanRound(2, "sha256:aaa", allDims, true)
	dirty.Major = 3
	ev := EvaluateCompletionEvidence([]state.VerifyLogEntry{
		cleanRound(1, "sha256:aaa", allDims, true),
		dirty,
		cleanRound(3, "sha256:aaa", allDims, true),
	}, 2)
	if ev.Confirmed {
		t.Fatal("a dirty round between the clean ones must break confirmation")
	}
	if ev.Have != 1 {
		t.Errorf("have = %d, want 1", ev.Have)
	}
}

func TestCompletionEvidence_WeakRoundsDoNotQualify(t *testing.T) {
	cases := []struct {
		name    string
		entries []state.VerifyLogEntry
		want    string
	}{
		{"delta pass", []state.VerifyLogEntry{
			cleanRound(1, "sha256:aaa", allDims, false),
			cleanRound(2, "sha256:aaa", allDims, false),
		}, "delta pass"},
		{"verify only", []state.VerifyLogEntry{
			cleanRound(1, "sha256:aaa", []string{"verify"}, true),
			cleanRound(2, "sha256:aaa", []string{"verify"}, true),
		}, "recorded verify only"},
		{"no revision", []state.VerifyLogEntry{
			cleanRound(1, "", allDims, true),
			cleanRound(2, "", allDims, true),
		}, "no doc_hash"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ev := EvaluateCompletionEvidence(tc.entries, 2)
			if ev.Confirmed {
				t.Fatalf("%s must not confirm", tc.name)
			}
			if !strings.Contains(ev.Reason, tc.want) {
				t.Errorf("reason = %q, want it to mention %q", ev.Reason, tc.want)
			}
		})
	}
}

// The exemption that inverted the incentive: recording no dimensions
// used to pass the gate that recording ONE dimension failed, so the
// honest caller was the only one it blocked. Absence of a declaration is
// not a declaration of completeness.
func TestCompletionEvidence_DimensionlessRoundsDoNotOutrankHonestOnes(t *testing.T) {
	silent := EvaluateCompletionEvidence([]state.VerifyLogEntry{
		cleanRound(1, "sha256:aaa", nil, true),
		cleanRound(2, "sha256:aaa", nil, true),
	}, 2)
	if silent.Confirmed {
		t.Fatalf("recording no dimensions must not pass as recording all of them, got %+v", silent)
	}
	if silent.Gate != "all_dimensions_before_final" {
		t.Errorf("gate = %q, want all_dimensions_before_final", silent.Gate)
	}
	if !strings.Contains(silent.Reason, "no dimensions at all") {
		t.Errorf("reason must name what was missing, got %q", silent.Reason)
	}

	honest := EvaluateCompletionEvidence([]state.VerifyLogEntry{
		cleanRound(1, "sha256:aaa", []string{"verify"}, true),
		cleanRound(2, "sha256:aaa", []string{"verify"}, true),
	}, 2)
	if honest.Gate != silent.Gate {
		t.Errorf("declaring one dimension (%q) and declaring none (%q) must fail the same gate",
			honest.Gate, silent.Gate)
	}
}

func TestCompletionEvidence_NeedOneRestoresTheOldRule(t *testing.T) {
	ev := EvaluateCompletionEvidence([]state.VerifyLogEntry{
		cleanRound(1, "sha256:aaa", allDims, true),
	}, 1)
	if !ev.Confirmed {
		t.Fatalf("confirm_passes=1 should accept a single clean round, got %+v", ev)
	}
}
