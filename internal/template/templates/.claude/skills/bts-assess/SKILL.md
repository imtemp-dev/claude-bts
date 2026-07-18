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

   **If content needs to be added** → recommend IMPROVE
   "Add [specific items] to reach Level [N]. Then run /verify."
   (Do NOT use this branch for `MINOR [deferred]` findings — those are
   runtime-observable items, not missing spec content. If the only gaps
   are [deferred] minors, skip to the "only [deferred] minors remain"
   branch below and recommend FINALIZE instead.)

   **If contradictions are suspected** → recommend `/verify`
   "Check sections [X] and [Y] for consistency."

   **If completeness is uncertain** → recommend `/audit`
   "Review for missing error cases, edge cases, security."

   **If mermaid diagrams are missing or incomplete** → recommend IMPROVE
   "Add state machine and flow diagrams with all execution paths enumerated."

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
     "reason": "Add function signatures for arrangement module",
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

## Simulate Timing

**Priority rule**: If verify-log shows critical=0 has been achieved AND no simulation
has run yet (check changelog.jsonl for "simulate" action), strongly recommend /simulate
BEFORE further IMPROVE cycles.

Rationale: structural verification (critical=0) means the spec is internally consistent.
Now is the best time to test scenarios — simulation catches behavioral gaps (failure modes,
race conditions, edge cases) that verification cannot find. Running simulation early
prevents discovering these gaps late and needing additional rework iterations.

## Important

- Always be specific. Not "needs more detail" but "add function signatures for auth module."
- Consider what has already been done (check .bts/specs/recipes/{id}/ for previous research, debates, simulations).
- If previous debates exist, check if their conclusions are reflected in the current draft.
