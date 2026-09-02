---
paths:
  - ".bts/**"
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

# BTS Verification Protocol

## Core Principle

Never verify your own output in the same context.

- **Internal consistency**: `bts verify` (deterministic) + `/bts-verify`, a fork that runs AS Agent(verifier)
- **Completeness**: `/bts-audit`, a fork that runs as Agent(auditor)
- **Scenario coverage**: `/bts-simulate`, a fork that runs as Agent(simulator)
- **Defense**: `/bts-defend`, a fork that runs as Agent(defender) over the ledger's open critical/major findings after the round is logged — the finder never adjudicates its own finding, and the orchestrator dismisses only on the defender's cited evidence
- **Code references**: Checked by `bts verify` when code exists (deterministic, optional)

Each gate is ONE context: the fork is the agent (skill frontmatter
`agent:`). The earlier shape — a general-purpose fork that spawned the
agent and waited — was measured at $12–30 and 40–56 minutes per
simulate round against $2.5 and 5 minutes for the walk itself, most of
it the fork re-reading its own context while polling a background
child. A gate that spawns a second gate is the failure mode, not the
principle.

## Mandatory Verification Rule

**Every time a document is modified, /verify MUST run immediately after.**
This is non-negotiable. The recipe protocol enforces this.

## Severity Classification

- **critical**: Internal contradiction, undefined behavior in scenarios, impossible claims, execution path leading to undefined behavior. Never `[deferred]`.
- **major**: Missing error handling, incomplete data flow, unresolved design questions, important execution path not specified. Never `[deferred]`.
- **minor [resolvable]**: Fixable in the spec itself — metadata, typos, internal inconsistencies, cross-reference errors, unused declarations, outdated level/version headers, misused terminology, ambiguous wording, unspecified minor branches.
- **minor [deferred]**: Only resolvable at implementation/runtime — device-specific behavior, measured thresholds, framework-version-specific quirks, observable race windows. Every `[deferred]` minor MUST include a `Why-deferred:` line naming the specific runtime observation that would resolve it, and an `Opens-with:` line carrying the exact command whenever one exists.
- **info**: Improvement suggestions, alternative approaches.

### What a severity is about

A finding is CRITICAL or MAJOR only when it names a **load-bearing**
item — one of:

- an **invariant** or its **owner**
- a **boundary contract**: the shape of what crosses a wire, a schema, a
  stored row, a public API
- an **irreversible order** or its rollback: migrations, rollouts, data
  transforms
- a **scope** decision: what ships and what does not

Everything else is at most `minor`. Signatures, type shapes, thresholds,
test assertion values, enumerated error cases, edge-case tables and
cross-references between detail sections are not load-bearing, however
convincingly wrong they look. A compiler, a type checker and one test run
settle them in seconds; a verify round costs a full document read and
leaves behind the prose it wrote to settle them, which the next round
re-checks.

Measured on one recipe: of seventeen CRITICAL findings, five named the
direction, three existed only because the document was long enough to
contradict itself, and **nine were questions a single execution
answers** — whether `RegExp.prototype.source` escapes forward slashes,
whether a synthesized `Encodable` omits nil optionals, whether Postgres
bracket ranges expand over the collating sequence.

### Opening the box

If something you can run would settle a finding, **do not argue about it
in prose**. Either run it now, or record it as `[deferred]` with the
command:

```
Opens-with: `node -e "console.log(/a\/b/.source)"`
```

A claim about code that does not exist yet has no truth value until
something executes. Writing the expected answer into the spec does not
settle it — it moves the argument earlier, where it costs more and where
two rounds reading identical bytes can disagree.

The inverse is the same rule. A load-bearing claim that nothing can
falsify is not verified because three agents read it and agreed; it is
**unopened**, and it stays unopened through completion unless the spec
names what would prove it false.

That half **is** machine-enforced: `falsifier_assigned`
(`engine/falsifier_checker.go` raises a major per uncovered invariant,
and `hook/stop.go:handleSpecDone` blocks `<bts>DONE</bts>`). The rest of
this section is not — no code can tell whether you argued about a
regex for four rounds or ran it. Both rules above are yours to keep.

Rule: if filling the gap requires executing the code (or observing it on a physical device) to resolve, it is `[deferred]`, not an IMPROVE target. The one exception is a gap whose ANSWER CHANGES WHAT GETS BUILT — a different host allowlist, a different release order, a different contract. Those stay MAJOR even though only a run can settle them, because the spec has to commit to a defensible choice now and say so. A gap that only changes a value the implementation discovers on its first run is `[deferred]`, whatever it would have cost at runtime.

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
- **Absence is not closure.** A finding that stops being reported goes
  to `unreported`, not `fixed`. It closes only after a second silent
  round, and never while its anchor is still producing new findings.
  Absence is what a repair looks like, but it is also what a verifier
  rewording the same defect looks like, and what a verifier told to skip
  deliberately-open items looks like. One measured round recorded "68
  new, 27 fixed" where all 27 were restatements still present in the
  document under new IDs; at least 40 of that recipe's 458 closures were
  false. `bts recipe log` reports unreported and held-back counts
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
  `bts recipe verify-focus <doc>` for the changed hunks and follow the
  references out from them. Declare the round with `--scope delta`.
- **The last round before finalization MUST be a full pass.** The stop
  hook blocks `<bts>DONE</bts>` when the spec's last verify entry is a
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

**One batch, one entry.** The three instruments are three forks and
return three findings blocks in three files. Record them as one round:

```bash
bts recipe log {id} --from-verification verification.md \
    --merge audit.md --merge simulations/NNN.md \
    --doc draft.md --scope full --dimension verify,audit,simulate
```

The CLI joins the blocks into verification.md and parses the result. A
measured recipe ran all three concurrently and logged them by hand as
three single-dimension rounds — three of its six cap slots, and no two
rounds of one class to judge convergence by.

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

`<bts>DONE</bts>` needs a clean triple that is **reproducible**, not one
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
`internal/hook/stop.go`, and `bts recipe assess-precheck` reads the same
function so the loop has one oracle rather than two. Set
`verify.confirm_passes: 1` to restore the old single-round rule.

## Convergence {gate: hard}

- critical + major must reach 0 for Level 3.
- **Convergence budget**: `verify.max_iterations` consecutive rounds
  (default: 3) that fail to improve on the best `(critical, major,
  minor_resolvable)` triple reached so far **by an earlier round of the
  same measurement class** → the round is logged with
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
- Every logged round records the `budget` it was judged under. Changing
  `verify.max_iterations` mid-document re-judges that document's whole
  history, so `bts recipe log` prints a notice when it changes.

- **Baseline staleness**: a class's best expires once the loop has run
  longer than one full rotation of its own measurements without
  re-measuring that class. A stale round sets a fresh baseline and HOLDS
  the streak, exactly like a first sighting.

  Without this, a class measured once — early, when the document was
  genuinely bad — left a target that stayed trivially beatable forever.
  A measured recipe left through that door: budget exhausted at round 16
  with a streak of 6, then round 17 rotated to `simulate/full`, beat the
  baseline that class had set at round 4 with `(4,17,8)` by scoring
  `(1,10,4)`, and reset the streak to zero. Nothing recent had improved.

- **Round cap**: `verify.max_rounds` (default 6) total rounds on one
  document, regardless of what any of them measured → the round is
  logged with `status: failed`, `bts recipe log` exits non-zero with
  `[ROUND CAP]`, and the loop stops.

  The budget is a judgement about the triple, and therefore about which
  instruments produced it. Every refinement of that judgement —
  per-document, per-class, staleness — closed a way to reset it without
  the document improving, and each was found only after a recipe had
  already escaped through it. The cap makes no judgement. It counts.

  At the cap, the answer is not another round. Move the open findings to
  `## Known Uncertainties`, each with the `Opens-with:` command that
  would settle it, and start implementing — a compiler and one test run
  answer in seconds what the next round would spend a full document read
  arguing about.

This is enforced in code (`internal/engine/convergence.go`), not by
self-counting. Recipes measured before it existed ran up to 15 verify
rounds against a cap of 3.

## Gate Overrides {gate: hard}

A hard gate the operator disagrees with does not stop them — it stops the
recorded path and leaves the unrecorded one open. A measured recipe
finalized with seven majors open and its last verify round marked
`failed`: the completion gate refused `<bts>DONE</bts>`, final.md was
written from draft.md anyway seventeen hours later, and the two real
decisions behind that lived only as prose. Every status surface went on
reporting an ordinary finalized recipe.

So the bypass is a command, not a workaround:

```bash
bts recipe override grant {id} --gate replicated_clean_pass --doc draft.md \
    --finding F-1a2b3c4d --finding F-5e6f7a8b \
    --reason "<why proceeding is the right call>"
```

- It names **one** gate. `bts recipe override list --gates` shows which
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
- It is **visible**: `bts recipe status`, `bts doctor` and `bts stats`
  report the recipe as overridden until it is revoked, and `bts stats`
  excludes it from any claim that the gates held.

Revoke with `bts recipe override revoke {id} --gate <gate> --reason "..."`.

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
bts recipe decision hold {id} --key <stable-key> \
    --question "<what the user must decide>" \
    [--option A --option B] [--doc draft.md] [--blocks <finding-id>]
```

- The key is the decision's identity across rounds. Reuse it when the same
  question comes back; never reuse it for a different question (rejected).
- While any decision is open the recipe is BLOCKED: the completion gate
  refuses to finalize, session start leads with it, `bts doctor` reports
  it as an error, and the status line shows `⛔N`.
- Ending the turn with an open decision recorded is correct — that IS the
  handoff. Ending it with the question only in chat is not.
- Record the answer, never assume it:
  `bts recipe decision resolve {id} <key> --answer "..."`.
  If the question stopped mattering, retire it with
  `bts recipe decision drop {id} <key> --reason "..."` rather than
  inventing an answer.

The ledger is `decisions.jsonl` in the recipe's tracked directory: a
decision that shaped a spec is part of that spec's provenance.
