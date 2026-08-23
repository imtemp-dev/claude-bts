package engine

import (
	"fmt"
	"strings"

	"github.com/imtemp-dev/jig/internal/state"
)

// Completion evidence — what it takes for a clean round to mean the
// document is clean.
//
// The old gate was one clean round: critical=0, major=0, no resolvable
// minors, on a full pass. That treats a verify round as a measurement of
// the document. It is not. It is a sample, and the sampling error is
// large enough to swamp the signal the gate is reading.
//
// Measured on one recipe's 34 rounds:
//
//   - Four times, two consecutive rounds verified a byte-identical
//     document — same recorded doc_hash, no edit in between — and
//     returned materially different verdicts. (0,1,3) became (1,10,10).
//     (0,0,0) became (2,9,13).
//   - Across the eight rounds that followed no edit at all, each still
//     produced a mean of 8.9 findings never seen before, including three
//     criticals and thirty majors.
//   - Findings correlated with how hard the round looked (r=+0.69 against
//     subagents spawned) far more than with what changed in the document
//     (r=+0.16 against edits). Rounds using ten agents averaged 40 new
//     findings; rounds using one averaged 6.5.
//
// A gate reading "this round found nothing" therefore rewards the
// weakest available measurement, and the recipe drifted exactly that way
// — its last three rounds used a single agent each.
//
// So completion asks for four things instead of one: the round used
// every instrument (dimensions), over the whole document (full pass), on
// a recorded revision (doc_hash), and enough INDEPENDENT rounds agreed on
// that revision (verify.confirm_passes) that the clean result is
// reproducible rather than lucky. Independent means each round cites its
// own verification.md content — otherwise "two agreeing rounds" is one
// round recorded twice, and the gate reads a copy as a corroboration.

// GateFailure is one unmet condition, named by the gate registry ID an
// override would have to cite to excuse it.
type GateFailure struct {
	Gate   string
	Reason string
	Remedy string
}

// CompletionEvidence is the verdict on whether a document's verify
// history can support finalization.
type CompletionEvidence struct {
	Confirmed bool
	Need      int    // verify.confirm_passes
	Have      int    // consecutive independent qualifying clean rounds on one revision
	Revision  string // doc_hash those rounds agree on
	Reason    string // why not, when Confirmed is false — the first failure
	Remedy    string // what to run next, when Confirmed is false
	// Gate is the gate_registry ID of the FIRST unmet condition.
	Gate string
	// Failures is EVERY unmet condition, not just the first.
	//
	// Reporting one at a time made an override of that one silently
	// excuse the others. An unclean round fails the clean check first, so
	// a delta round with no dimensions and no recorded revision reported
	// only `verification_not_passed` — and one grant of that gate let it
	// finalize without full_pass, dimensions or replication ever being
	// evaluated. An override names one gate; it must not carry the ones
	// that happened to be behind it in the evaluation order.
	Failures []GateFailure
}

// qualifyFailures returns EVERY condition the round fails to meet, in
// evaluation order. An empty result means the round is strong enough to
// count toward completion.
func qualifyFailures(e *state.VerifyLogEntry, doc string) []GateFailure {
	var out []GateFailure
	if !ProgressKeyOf(e).Clean() {
		out = append(out, GateFailure{
			Gate:   "verification_not_passed",
			Reason: fmt.Sprintf("round %d is not clean (%s)", e.Iteration, ProgressKeyOf(e)),
			Remedy: "Resolve the open findings (`jig recipe findings list --open`), then re-verify.",
		})
	}
	if !e.FullPass {
		out = append(out, GateFailure{
			Gate: "full_pass_before_final",
			Reason: fmt.Sprintf(
				"round %d was a scoped delta pass — it never re-read the untouched sections against the edits",
				e.Iteration),
			Remedy: fullPassRemedy(doc),
		})
	}
	// Recording no dimensions does not mean all of them ran. An earlier
	// draft of this gate exempted dimensionless rounds so that recipes
	// in flight at upgrade time would not strand — but the exemption
	// inverted the incentive it was written to create: declaring
	// `--dimension verify` truthfully blocked completion while declaring
	// nothing passed it. The honest caller was the only one penalised.
	if !e.HasAllDimensions() {
		ran := "no dimensions at all"
		if len(e.Dimensions) > 0 {
			ran = strings.Join(e.Dimensions, "+") + " only"
		}
		out = append(out, GateFailure{
			Gate: "all_dimensions_before_final",
			Reason: fmt.Sprintf(
				"round %d recorded %s — completion needs %s, because a clean result from one instrument is not evidence the others agree",
				e.Iteration, ran, strings.Join(state.VerifyDimensions, "+")),
			Remedy: fullPassRemedy(doc),
		})
	}
	if e.DocHash == "" {
		out = append(out, GateFailure{
			Gate: "revision_recorded_before_final",
			Reason: fmt.Sprintf(
				"round %d recorded no doc_hash, so jig cannot tell which revision it verified",
				e.Iteration),
			Remedy: "Re-run `jig recipe log` with a --doc path that resolves, so the revision is recorded. " +
				"`jig recipe log` warns on stderr when it could not read the document.",
		})
	}
	return out
}

func fullPassRemedy(doc string) string {
	if doc == "" {
		doc = "<doc>"
	}
	return fmt.Sprintf(
		"Run one more round covering %s over the whole document: "+
			"`jig recipe log {id} --from-verification <verification.md> --doc %s --scope full --dimension %s`.",
		strings.Join(state.VerifyDimensions, "+"), doc,
		strings.Join(state.VerifyDimensions, " --dimension "))
}

// apply records the failures on the verdict, keeping the first as the
// headline the block message renders.
func (ev *CompletionEvidence) apply(failures []GateFailure) {
	ev.Failures = failures
	if len(failures) == 0 {
		return
	}
	ev.Gate, ev.Reason, ev.Remedy = failures[0].Gate, failures[0].Reason, failures[0].Remedy
}

// EvaluateCompletionEvidence walks a single document's rounds, newest
// first, and counts how many consecutive qualifying clean rounds agree on
// one revision AND were produced by distinct verification artefacts.
// need <= 1 restores the single-round rule.
//
// The independence clause is what makes the count a replication rather
// than a repetition. Without it the gate counted rows, and two rows are
// produced by re-running one `jig recipe log` invocation — the
// verification.md on disk never changed, no second reading of the
// document ever happened, and the gate that exists because "one clean
// round is a sample" was satisfied by recording that same sample twice.
// Two rounds count as two only when they cite different verification.md
// content; a round that recorded no verification_hash at all cannot be
// shown to be independent of its neighbour and stops the count.
func EvaluateCompletionEvidence(entries []state.VerifyLogEntry, need int) CompletionEvidence {
	if need < 1 {
		need = 1
	}
	ev := CompletionEvidence{Need: need}
	if len(entries) == 0 {
		ev.apply([]GateFailure{{
			Gate:   "verification_not_passed",
			Reason: "no verification history for this document",
			Remedy: "Run /jig-verify and record it with `jig recipe log ... --doc <doc> --scope full`.",
		}})
		return ev
	}

	last := &entries[len(entries)-1]
	if failures := qualifyFailures(last, last.Doc); len(failures) > 0 {
		ev.apply(failures)
		return ev
	}

	ev.Revision = last.DocHash
	seen := make(map[string]bool, need)
	stop := "" // why the walk stopped, when it was an independence problem
	for i := len(entries) - 1; i >= 0; i-- {
		e := &entries[i]
		if e.DocHash != ev.Revision {
			break
		}
		if len(qualifyFailures(e, e.Doc)) > 0 {
			break
		}
		if seen[e.VerificationHash] {
			// An absent hash is not a distinct artefact either: two
			// rounds that both recorded nothing are indistinguishable,
			// which is the same failure with a worse diagnostic. The
			// FIRST round may carry an empty hash — there is nothing yet
			// for it to be independent of — so confirm_passes=1 still
			// restores the single-round rule exactly.
			if e.VerificationHash == "" {
				stop = fmt.Sprintf(
					"round %d recorded no verification_hash, so jig cannot tell it apart from the round before it",
					e.Iteration)
			} else {
				stop = fmt.Sprintf(
					"round %d cites the same verification.md as an earlier round (%s) — that is one measurement recorded twice, not two that agree",
					e.Iteration, shortHash(e.VerificationHash))
			}
			break
		}
		seen[e.VerificationHash] = true
		ev.Have++
	}

	if ev.Have >= need {
		ev.Confirmed = true
		return ev
	}
	reason := fmt.Sprintf(
		"%d of %d independent confirming rounds on revision %s — a single clean round is a sample, not a measurement "+
			"(unchanged documents in this project have produced criticals on re-verification)",
		ev.Have, need, shortHash(ev.Revision))
	if stop != "" {
		reason += "; " + stop
	}
	remedy := fmt.Sprintf(
		"Run %d more full pass(es) over the unchanged document, writing a fresh verification.md each time, and record each with "+
			"`--from-verification <verification.md> --scope full --dimension %s`. "+
			"Editing the document resets the count, which is the point; re-recording the same verification.md does not raise it.",
		need-ev.Have, strings.Join(state.VerifyDimensions, " --dimension "))
	ev.apply([]GateFailure{{Gate: "replicated_clean_pass", Reason: reason, Remedy: remedy}})
	return ev
}

// shortHash trims a "sha256:..." digest to something a message can carry.
func shortHash(h string) string {
	h = strings.TrimPrefix(h, "sha256:")
	if len(h) > 12 {
		return h[:12]
	}
	return h
}
