---
paths:
  - ".bts/**"
authoritative_for:
  - severity_classification
  - convergence_threshold
  - minor_subclassification
  - verification_scope
  - finding_identity
---

# BTS Verification Protocol

## Core Principle

Never verify your own output in the same context.

- **Internal consistency**: Checked by `bts verify` (deterministic) + Agent(verifier) (separate context)
- **Completeness**: Checked by Agent(auditor) (separate context)
- **Scenario coverage**: Checked by Agent(simulator) or /simulate (separate context)
- **Code references**: Checked by `bts verify` when code exists (deterministic, optional)

## Mandatory Verification Rule

**Every time a document is modified, /verify MUST run immediately after.**
This is non-negotiable. The recipe protocol enforces this.

## Severity Classification

- **critical**: Internal contradiction, undefined behavior in scenarios, impossible claims, execution path leading to undefined behavior. Never `[deferred]`.
- **major**: Missing error handling, incomplete data flow, unresolved design questions, important execution path not specified. Never `[deferred]`.
- **minor [resolvable]**: Fixable in the spec itself — metadata, typos, internal inconsistencies, cross-reference errors, unused declarations, outdated level/version headers, misused terminology, ambiguous wording, unspecified minor branches.
- **minor [deferred]**: Only resolvable at implementation/runtime — device-specific behavior, measured thresholds, framework-version-specific quirks, observable race windows. Every `[deferred]` minor MUST include a `Why-deferred:` line naming the specific runtime observation that would resolve it.
- **info**: Improvement suggestions, alternative approaches.

Rule: if filling the gap requires executing the code (or observing it on a physical device) to resolve, it is `[deferred]`, not an IMPROVE target. CRITICAL and MAJOR are never `[deferred]` — unknowable-pre-implementation gaps that would cause failure stay MAJOR; the spec must document the uncertainty as a defensive design decision.

## Finding Identity {gate: hard}

Every finding carries a stable ID derived from its document and its
title, assigned by `bts` — never by hand. IDs live in `findings.jsonl`
(append-only) and survive across rounds, which is what makes the
stagnation rule below computable at all. Before this ledger existed,
findings were numbered positionally in a verification.md that was
overwritten every cycle, so "#4" in one round and "#4" in the next were
unrelated and nothing could tell re-litigation from a real regression.

The ledger is written automatically when a verify round is logged with
both a `--doc` and a `<bts-findings>` block containing a `findings`
array:

```bash
bts recipe log {id} --from-verification <verification.md> --doc <doc-path>
```

The array must carry one non-empty title per finding and match the
block's counts per severity; a mismatch fails the command rather than
recording a round whose ledger disagrees with its gate
(`findings_array_consistency`).

Consequences the loop must honour:

- **Carry forward.** Each verify round receives the adjudicated findings
  from previous rounds (`bts recipe findings carry-forward {id} --doc <doc>`).
  Settled points are not re-derived from scratch.
- **Do not re-raise dismissed findings.** A finding dismissed via
  `bts recipe findings dismiss` was adjudicated as not-a-defect. Raising
  it again is recorded as a reopen and counts against convergence.
- **Reopens are signal.** A finding that goes fixed → open again means
  the last IMPROVE regressed something. Treat it as a defect in the fix,
  not as a new finding.

## Verification Scope {gate: hard}

A round is either a **full pass** (the entire document) or a **delta
pass** (the sections changed since the last verified revision, plus
their reference closure — every section that cites a changed term,
anchor, interface, or invariant).

- **Round 1 on a document is always a full pass.** There is no prior
  verified revision to diff against.
- **Later rounds may be delta passes.** Use
  `bts recipe verify-focus <doc>` for the changed hunks and follow the
  references out from them. Declare the round with `--scope delta`.
- **The last round before finalization MUST be a full pass.** The stop
  hook blocks `<bts>DONE</bts>` when the spec's last verify entry is a
  delta pass (`full_pass_before_final`). A delta pass never re-checked
  the untouched sections against the edits, so it is not sufficient
  evidence that the whole spec still holds together.
- **Only full passes advance the verify snapshot**, so a delta round
  does not shrink the next round's focus diff.

Rationale: a document is not re-randomised by an edit to one section.
Re-deriving all of it every round is what let untouched sections
generate new findings faster than edits resolved them.

## Convergence {gate: hard}

- critical + major must reach 0 for Level 3.
- **Convergence budget**: `verify.max_iterations` consecutive rounds
  (default: 3) that fail to improve on the best `(critical, major,
  minor_resolvable)` triple reached so far → the round is logged with
  `status: failed`, `bts recipe log` exits non-zero with
  `[CONVERGENCE FAILED]`, and the loop MUST stop and ask the user.
  Progress is measured lexicographically: fewer criticals beats fewer
  majors beats fewer resolvable minors. Deferred minors and info are
  excluded — they never block completion, so churn in them must not
  reset the budget.
- The budget is evaluated **per document**. A wireframe round no longer
  resets or satisfies the draft's budget.
- A document already at `(0,0,0)` is converged, never failed — it cannot
  improve on a clean triple.
- Stagnation detector: findings still open across the whole streak are
  reported by name in the `[CONVERGENCE FAILED]` message. Do not retry
  them; bring them to the user.

This is enforced in code (`internal/engine/convergence.go`), not by
self-counting. Recipes measured before it existed ran up to 15 verify
rounds against a cap of 3.
