---
paths:
  - ".jig/**"
authoritative_for:
  - severity_classification
  - measurement_strength
  - completion_evidence
  - convergence_threshold
  - minor_subclassification
  - verification_scope
  - finding_identity
  - decision_handoff
---

# jig Verification Protocol

## Core Principle

Never verify your own output in the same context.

- **Internal consistency**: Checked by `jig verify` (deterministic) + Agent(verifier) (separate context)
- **Completeness**: Checked by Agent(auditor) (separate context)
- **Scenario coverage**: Checked by Agent(simulator) or /simulate (separate context)
- **Code references**: Checked by `jig verify` when code exists (deterministic, optional)

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
title, assigned by `jig` — never by hand. IDs live in `findings.jsonl`
(append-only) and survive across rounds, which is what makes the
stagnation rule below computable at all. Before this ledger existed,
findings were numbered positionally in a verification.md that was
overwritten every cycle, so "#4" in one round and "#4" in the next were
unrelated and nothing could tell re-litigation from a real regression.

The ledger is written automatically when a verify round is logged with
both a `--doc` and a `<jig-findings>` block containing a `findings`
array:

```bash
jig recipe log {id} --from-verification <verification.md> --doc <doc-path>
```

The array must carry one non-empty title per finding and match the
block's counts per severity; a mismatch fails the command rather than
recording a round whose ledger disagrees with its gate
(`findings_array_consistency`).

Consequences the loop must honour:

- **Carry forward.** Each verify round receives the adjudicated findings
  from previous rounds (`jig recipe findings carry-forward {id} --doc <doc>`).
  Settled points are not re-derived from scratch.
- **Do not re-raise dismissed findings.** A finding dismissed via
  `jig recipe findings dismiss` was adjudicated as not-a-defect. Raising
  it again is recorded as a reopen and counts against convergence.
- **Reopens are signal.** A finding that goes fixed → open again means
  the last IMPROVE regressed something. Treat it as a defect in the fix,
  not as a new finding.
- **Absence is not closure.** A finding that stops being reported goes
  to `unreported`, not `fixed`. It closes only after a second silent
  round, and never while its anchor is still producing new findings.
  Absence is what a repair looks like, but it is also what a verifier
  rewording the same defect looks like, and what a verifier told to skip
  deliberately-open items looks like. One measured round recorded "68
  new, 27 fixed" where all 27 were restatements still present in the
  document under new IDs; at least 40 of that recipe's 458 closures were
  false. `jig recipe log` reports unreported and held-back counts
  separately — read them, and say explicitly for each whether the fix
  landed rather than letting silence decide.

## Verification Scope {gate: hard}

A round is either a **full pass** (the entire document) or a **delta
pass** (the sections changed since the last verified revision, plus
their reference closure — every section that cites a changed term,
anchor, interface, or invariant).

- **Round 1 on a document is always a full pass.** There is no prior
  verified revision to diff against.
- **Later rounds may be delta passes.** Use
  `jig recipe verify-focus <doc>` for the changed hunks and follow the
  references out from them. Declare the round with `--scope delta`.
- **The last round before finalization MUST be a full pass.** The stop
  hook blocks `<jig>DONE</jig>` when the spec's last verify entry is a
  delta pass (`full_pass_before_final`). A delta pass never re-checked
  the untouched sections against the edits, so it is not sufficient
  evidence that the whole spec still holds together.
- **Only full passes advance the verified revision**, so a delta round
  does not shrink the next round's focus diff, and does not clear a
  rule-3 dirty flag on the document as a whole.

Rationale: re-deriving the whole document every round is what let
untouched sections generate new findings faster than edits resolved
them, so a delta pass is the right default for iteration.

What a delta pass does NOT do is make a round repeatable. The original
rationale here claimed a document "is not re-randomised by an edit to one
section"; measurement showed the re-randomisation comes from re-reading,
not from editing — rounds following no edit at all still averaged 8.9
new findings. That is why a delta pass is cheap iteration and never
completion evidence (§ Completion Evidence).

## Measurement Strength {gate: hard}

A verify round is a **sample**, not a measurement of the document.
Every rule below follows from that.

Each round declares what produced its counts:

- `--scope full|delta` — how much of the document was read.
- `--dimension verify|audit|simulate` — which instruments read it, one
  flag per pass actually run. Declare only what ran; declaring nothing
  is not a claim that everything ran, and a round with no dimensions
  cannot count toward completion.

Together these are the round's **measurement class**. Two rounds are
comparable — one can be said to have improved on the other — only when
their classes match. A verify-only round finds less than a
verify+audit+simulate round on identical text, and a delta pass finds
less than a full one, neither because the text improved.

Changing class is therefore not progress and does not reset the
convergence budget. A round that is the first of its class has nothing
comparable behind it: it sets that class's baseline and leaves the
streak exactly where it was — neither reset nor advanced. Only beating
an earlier round of your **own** class resets it.

Rotating instruments therefore delays the budget by at most the number
of distinct classes and cannot prevent it: once the measurements stop
being new, every round that fails to beat its own class counts. It is a
way to measure differently, never a way to buy more rounds.

Why this is a hard gate rather than advice, from measured runs:

- Four times in one recipe, two consecutive rounds verified a
  byte-identical document — same recorded `doc_hash`, no edit between —
  and disagreed. `(0,1,3)` became `(1,10,10)`. `(0,0,0)` became
  `(2,9,13)`.
- Rounds that followed no edit at all still averaged **8.9** findings
  never seen before, including criticals.
- Findings tracked how hard the round looked (r=+0.69 against subagents
  spawned) far more than what changed in the document (r=+0.16 against
  edits). Ten-agent rounds averaged 40 new findings; one-agent rounds
  averaged 6.5.

A gate reading "this round found nothing" therefore rewards the weakest
available measurement. Declaring the class is what stops the loop from
comparing a number one instrument produced against a number three
produced — the artefact that made one recipe's budget fire for fourteen
consecutive rounds against a target no honest round could reach, until
the operator raised `verify.max_iterations` twice to escape it.

## Completion Evidence {gate: hard}

`<jig>DONE</jig>` needs a clean triple that is **reproducible**, not one
clean triple:

1. **Clean** — critical=0, major=0, minor_resolvable=0.
2. **Whole** — `--scope full`. A delta pass never re-read the untouched
   sections against the edits.
3. **Every instrument** — all of `verify`, `audit`, `simulate`, declared
   on the round. A clean result from one is not evidence the others
   agree, and recording no dimensions at all is not a declaration that
   every pass ran.
4. **Revision recorded** — a `doc_hash`. A round that cannot say which
   revision it read cannot be replicated against, and the gate says so
   rather than falling open. `--doc` must resolve from where the command
   runs, or nothing is recorded.
5. **Reproduced independently** — `verify.confirm_passes` (default 2)
   consecutive rounds meeting 1-4 on the **same recorded revision**, each
   citing its **own** verification.md content. Editing the document
   resets the count, which is the point. Re-recording one round does not
   raise it: two rows are not two readings, and a gate that counts rows
   is satisfied by re-typing a command.

Enforced in `internal/engine/completion_evidence.go` +
`internal/hook/stop.go`, and `jig recipe assess-precheck` reads the same
function so the loop has one oracle rather than two. Set
`verify.confirm_passes: 1` to restore the old single-round rule.

## Convergence {gate: hard}

- critical + major must reach 0 for Level 3.
- **Convergence budget**: `verify.max_iterations` consecutive rounds
  (default: 3) that fail to improve on the best `(critical, major,
  minor_resolvable)` triple reached so far **by an earlier round of the
  same measurement class** → the round is logged with
  `status: failed`, `jig recipe log` exits non-zero with
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
- Every logged round records the `budget` it was judged under. Changing
  `verify.max_iterations` mid-document re-judges that document's whole
  history, so `jig recipe log` prints a notice when it changes.

This is enforced in code (`internal/engine/convergence.go`), not by
self-counting. Recipes measured before it existed ran up to 15 verify
rounds against a cap of 3.

## Gate Overrides {gate: hard}

A hard gate the operator disagrees with does not stop them — it stops the
recorded path and leaves the unrecorded one open. A measured recipe
finalized with seven majors open and its last verify round marked
`failed`: the completion gate refused `<jig>DONE</jig>`, final.md was
written from draft.md anyway seventeen hours later, and the two real
decisions behind that lived only as prose. Every status surface went on
reporting an ordinary finalized recipe.

So the bypass is a command, not a workaround:

```bash
jig recipe override grant {id} --gate replicated_clean_pass --doc draft.md \
    --finding F-1a2b3c4d --finding F-5e6f7a8b \
    --reason "<why proceeding is the right call>"
```

- It names **one** gate. `jig recipe override list --gates` shows which
  gates accept one; the rest protect the integrity of the record rather
  than the quality of the work, and are not overridable.
- It **enumerates** the findings it excuses. An override without a named
  set is refused — that is a blanket pass, which is the thing this
  replaces. Gates that are not about findings (a missing full pass, an
  unreplicated round) take `--no-findings` instead; the block message
  tells you which one applies.
- It **names the document** and is **pinned** to the revision it was
  granted on. `--doc` is required for every document-scoped gate, and
  the file must be readable — an override that is not pinned matches
  every revision forever, which is the blanket pass under another name.
  Edit the document and the override goes stale, because the judgement
  was about that text. A round that recorded no `doc_hash` is stale
  too: without knowing which text is in front of it, an override is not
  evidence about anything.
- It is **visible**: `jig recipe status`, `jig doctor` and `jig stats`
  report the recipe as overridden until it is revoked, and `jig stats`
  excludes it from any claim that the gates held.

Revoke with `jig recipe override revoke {id} --gate <gate> --reason "..."`.

An override is a legitimate operator decision. Working around a gate
without one is not — it produces a recipe whose records say something
that is not true.

## Handing a question to the user {gate: hard}

"Stop the loop and ask the user" is only half a handoff. The question and
its answer live in the conversation, and a compaction, a new session, or
simply the next day loses both — while the recipe's own state still looks
like ordinary in-progress work.

When the loop stops for a decision only the user can make — a convergence
failure, or a finding that needs a product call rather than a spec edit —
record it before ending the turn:

```bash
jig recipe decision hold {id} --key <stable-key> \
    --question "<what the user must decide>" \
    [--option A --option B] [--doc draft.md] [--blocks <finding-id>]
```

- The key is the decision's identity across rounds. Reuse it when the same
  question comes back; never reuse it for a different question (rejected).
- While any decision is open the recipe is BLOCKED: the completion gate
  refuses to finalize, session start leads with it, `jig doctor` reports
  it as an error, and the status line shows `⛔N`.
- Ending the turn with an open decision recorded is correct — that IS the
  handoff. Ending it with the question only in chat is not.
- Record the answer, never assume it:
  `jig recipe decision resolve {id} <key> --answer "..."`.
  If the question stopped mattering, retire it with
  `jig recipe decision drop {id} <key> --reason "..."` rather than
  inventing an answer.

The ledger is `decisions.jsonl` in the recipe's tracked directory: a
decision that shaped a spec is part of that spec's provenance.
