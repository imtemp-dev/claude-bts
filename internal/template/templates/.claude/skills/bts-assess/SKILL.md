---
name: bts-assess
description: >
  Assess a document's current level (1-3) and determine the next action needed.
  This is the brain of the adaptive loop — it decides what to do next.
user-invocable: true
allowed-tools: Read Bash
argument-hint: "[file-path]"
context: fork
---

# Document Assessment

Assess the document and decide the next action.

## Division of labor

This skill runs in a FORK: the full document read in step 3 stays out
of the main loop's context (repeated assess rounds were its largest
context accumulator). Everything the loop needs comes back as your
final output — the `<bts-decision>` block (Part A) plus the
human-readable rationale (Part B). The ORCHESTRATOR parses the block
and executes the action; you only decide.

Bash in this fork is ONLY for read-only commands (`bts verify`,
`bts recipe status`). Never run state-mutating bts commands (log,
create, finalize, …) or write files from this fork.

## Do not call this skill when state already answers

The orchestrator should run `bts recipe assess-precheck {id} --doc <doc>`
first. When that prints a `<bts-decision>`, the next action is determined
by recorded state (converged + unchanged → FINALIZE; changed since last
verification → VERIFY; convergence budget exhausted → HALT) and this
skill must NOT be invoked — an assessment round would re-read the whole
document to reach the same conclusion. Only `UNDECIDED` (exit 10) means
judgement is genuinely required.

## Steps

1. Run level assessment via bts binary:
   ```bash
   bts verify $ARGUMENTS
   ```
   This returns the current level score and missing criteria.
   If `$ARGUMENTS` is empty, resolve the active recipe via
   `bts recipe status` and target its draft:
   `bts verify .bts/specs/recipes/{id}/draft.md`
   (`bts verify` requires exactly one file argument).

2. Build situational awareness:
   - Read changelog.jsonl (last 5 entries) to know what was just done
   - Check if simulation has run (look for "simulate" action in changelog)
   - Check if debates exist and whether conclusions are reflected in the draft
   - Read scope.md to keep boundaries in mind
   - Run `bts recipe findings list {id} --open` for the findings that
     have survived previous rounds. A finding with a high `ROUNDS` count
     or any `REOPEN` is the signal that IMPROVE is not working on it:
     recommend a different action (DEBATE, RESEARCH, or halting for the
     user) rather than another IMPROVE cycle against the same wall.

3. Read the document fully yourself and evaluate:
   - What level is this document at? (1=understanding, 2=design, 3=implementation-ready)
   - What specific content is missing to reach the next level?
   - Are there uncertain technical decisions that need debate?
   - Are there scenarios that haven't been walked through?
   - Are there internal contradictions?

4. Decide the next action based on assessment:

   **Structural prerequisites — check FIRST, in this order** (a draft
   built on an unchosen or unmodeled decomposition wastes IMPROVE
   cycles; these actions take precedence over all content-level
   actions below):
   - `domain.md` missing, or `bts verify domain.md` reports
     critical/major (blueprint/design recipes) → action `DOMAIN_MODEL`,
     phase `domain-model`
   - `wireframe.md` lacks the `<!-- architect-decision -->` block
     → action `ARCHITECT`, phase `architect`
   - `wireframe.md` missing, or `bts verify wireframe.md` reports
     critical/major → action `WIREFRAME`, phase `wireframe`

   **If information is insufficient** → recommend `/research`
   "Need to investigate [specific topic] before proceeding."

   **If technical decision is uncertain** → recommend `/debate`
   "The choice between [A] and [B] needs expert discussion."

   **If gaps may exist** → recommend `/simulate`
   "Walk through [specific scenarios] to find blind spots."

   **If a level criterion is unmet** → recommend IMPROVE
   Name the criterion and what would satisfy it, from
   `bts verify`'s `Missing` list and `bts-level-criteria.md`:
   "INV-004 and INV-007 have owners but no falsifier — add a row naming
   the test that would go red. Then run /verify."

   Every criterion is structural and saturating: it is met by adding a
   specific missing STRUCTURE, never by adding detail to what is already
   there. **Never recommend IMPROVE for elaboration** — more signatures,
   more edge cases, more scaffolding, a fuller walkthrough. Those are not
   criteria, a compiler settles them faster than a verify round, and the
   prose written to settle one becomes a claim the next round re-checks.

   If a criterion's content belongs upstream (the flow, the
   decomposition, the recorded decision), the fix is a reference to
   `wireframe.md` or `domain.md` — not a copy of it.

   (Do NOT use this branch for `MINOR [deferred]` findings — those are
   runtime-observable items, not missing spec content. If the only gaps
   are [deferred] minors, skip to the "only [deferred] minors remain"
   branch below and recommend FINALIZE instead.)

   **If contradictions are suspected** → recommend `/verify`
   "Check sections [X] and [Y] for consistency."

   **If completeness is uncertain** → recommend `/audit`
   "Review for missing error cases, edge cases, security."

   **If the structure or flow is missing** → recommend WIREFRAME, not
   IMPROVE. State machines, flow diagrams and enumerated execution paths
   live in `wireframe.md`; the blueprint references them. Recommending
   IMPROVE here asks the draft for a second copy, which is one more place
   the same claim can go stale.

   **If Level 3 criteria all met** → recommend `/sync-check` then finalize
   "Document appears complete. Run sync-check before finalizing."

   **If only [deferred] minors remain** → recommend FINALIZE
   Read verification.md — if every remaining finding is tagged
   `MINOR [deferred]` (or no findings at all) with zero critical,
   zero major, and zero resolvable minors, do NOT recommend IMPROVE.
   The deferred items are runtime-observable uncertainties and belong
   in the draft's "## Known Uncertainties" section (per blueprint rule 3b).
   Recommendation: "Level 3 achieved with N runtime-deferred watch items.
   Run /bts-sync-check, then finalize. Deferred items will be validated
   during /bts-implement's test/simulate loop."

5. Output your assessment in TWO parts:

   **Part A — Machine-readable decision block (REQUIRED, exact format):**

   Emit this block verbatim, with valid JSON inside. The blueprint loop
   and `bts validate` parse this block; anything outside the block is
   ignored by the loop orchestrator.

   ```
   <bts-decision>
   {
     "level": 2.5,
     "action": "IMPROVE",
     "phase": "draft",
     "reason": "INV-004 and INV-007 have owners but no falsifier",
     "findings_ref": "verification.md#last-run"
   }
   </bts-decision>
   ```

   `action` MUST be one of (case-sensitive):
   `RESEARCH`, `DEBATE`, `ADJUDICATE`, `SIMULATE`, `AUDIT`, `IMPROVE`,
   `VERIFY`, `SYNC_CHECK`, `FINALIZE`, `SCOPE_REOPEN`, `WIREFRAME`,
   `DOMAIN_MODEL`, `ARCHITECT`, `HALT_DECISION_REQUIRED`,
   `HALT_CONVERGENCE_FAILED`, `HALT_DEBATE_DEADLOCK`.

   `phase` MUST be a valid recipe phase from `bts-schema.md`.

   `findings_ref` is optional — cite the verification.md / audit output
   section that justifies this action.

   **Part B — Human-readable rationale** (free text, for the user):
   ```
   ## Assessment
   Current Level: [X.Y]
   Missing for next level: [list]

   ## Recommended Action
   [ACTION]: [specific instruction]

   ## Rationale
   [Why this action is needed now]
   ```

## Measurement Timing

**Priority rule**: every verify round runs all three instruments —
verify, audit and simulate — in one concurrent batch. If the last round
recorded fewer than three dimensions, recommend VERIFY and say which
instrument was missing.

Rationale: a round's counts are comparable only against rounds of the
same measurement class, so a loop that changes class each round is a
loop whose convergence budget never accumulates. Worse, a held-back
instrument does not find less; it finds the same things later. A
measured recipe ran audit at rounds 1-2, then not again until rounds 10
and 16, and each return produced five majors nobody had seen — on top of
ten rounds of IMPROVE built on an un-audited draft.

Completion requires a clean round from all three anyway
(`bts-verification-protocol.md § Completion Evidence`), and the round cap
counts rounds rather than dimensions, so spending a round per instrument
buys nothing and costs half the budget.

## Important

- Always be specific, and always name a STRUCTURE rather than an amount.
  Not "needs more detail", and not "add function signatures for the auth
  module" — that is elaboration, which no criterion asks for. Name the
  unmet criterion and the smallest structure that satisfies it: "INV-006
  has no falsifier — add a row naming the test that would go red."
- Consider what has already been done (check .bts/specs/recipes/{id}/ for previous research, debates, simulations).
- If previous debates exist, check if their conclusions are reflected in the current draft.
