---
name: bts-recipe-blueprint
description: >
  Create a Level 3 blueprint — the skeleton a compiler cannot settle — through
  an adaptive loop of research, drafting, debate, simulation, and verification.
  The loop is bounded: it converges or it hands its open questions to
  implementation.
user-invocable: true
allowed-tools: Read Write Edit Grep Glob Bash Agent AskUserQuestion mcp__context7__resolve-library-id mcp__context7__get-library-docs
argument-hint: "\"feature description\""
---

# Recipe: Blueprint

Create a Level 3 blueprint for: $ARGUMENTS

**This recipe creates a SPEC DOCUMENT, not code.**
Do NOT write source code files (.ts, .js, .go, .py, .rs, etc.) during this recipe.
Only create documents in `.bts/specs/recipes/{id}/`.
Code implementation happens in `/bts-implement` AFTER this recipe completes with `<bts>DONE</bts>`.

## Settings

Read `.bts/config/settings.yaml` for project-specific limits.
Use settings values if present, otherwise use defaults noted in each step.

## Resume Check

Before starting, check for an existing recipe:
```bash
bts recipe status
```
If active, check the phase to determine resume strategy:

**If phase is `discovery`:** Read intent.md.
- Status EXPLORING → continue discovery conversation using AskUserQuestion
- Status CONFIRMED → proceed to Vision & Roadmap Check

**If phase is `scoping`:** Check vision/roadmap state first (in order):
1. If `.bts/specs/vision.md` exists with Status: DRAFT → re-present vision for confirmation.
   After vision confirmed, check roadmap below.
2. If `.bts/specs/vision.md` CONFIRMED but `.bts/specs/roadmap.md` missing →
   go to Vision & Roadmap Check step 3b (create roadmap from confirmed vision).
3. If `.bts/specs/roadmap.md` exists with Status: DRAFT → re-present roadmap for confirmation.
4. If scope.md exists → follow the Scoping Loop "On resume" protocol below —
   re-present if Status is DRAFT, or skip to adaptive loop if CONFIRMED.
5. If scope.md does not exist → go to Scoping Loop step 1 (start scoping with roadmap context).

**If phase is `wireframe`:** Read `wireframe.md` if it exists.
- If incomplete → continue wireframe design
- If complete (all quality gate checks pass) → transition to draft

**If phase is any other (research, draft, verify, debate, etc.):** Resume with **minimum reads**:
1. `changelog.jsonl` — last 5 entries only (determine current position in the loop)
2. `draft.md` — the current draft (if not found, check `manifest.json` `current_draft` for legacy path)
3. `wireframe.md` — structural reference for draft alignment
4. `verification.md` — latest verification findings
5. `scope.md` — confirm scope is still valid

Do NOT read on resume: research documents (already incorporated into the draft).

Then determine the next action:
```bash
bts recipe assess-precheck {id} --doc .bts/specs/recipes/{id}/draft.md
```
Execute the printed `<bts-decision>` if there is one; only run
`/bts-assess` when the precheck reports `UNDECIDED`. Resuming is exactly
the case where state already holds the answer — a converged, unchanged
draft needs finalization, not a fresh assessment round.

## Adaptive Loop

This recipe does NOT follow a fixed sequence. Instead, it runs an adaptive loop:

```
ASSESS → decide action → execute → VERIFY (mandatory after any change) → ASSESS → ...
```

ASSESS determines what to do next based on the document's current state.

### Loop Protocol

**At recipe start (MANDATORY):**
1. Check `bts recipe status`. If no active recipe exists:
   ```bash
   bts recipe create --type blueprint --topic "$ARGUMENTS"
   ```
   This creates `recipe.json` and `manifest.json` automatically and outputs the recipe ID.
2. Run `bts validate` to confirm schema compliance

**ALWAYS after modifying any JSON file in .bts/:**
1. Run `bts validate` to verify schema compliance. Fix any errors before continuing.

**ALWAYS after modifying draft.md:**
1. Edit `draft.md` in place (Write for initial creation, Edit for improvements)
2. Log the action to changelog:
   ```bash
   bts recipe log {id} --action [draft|improve] --output draft.md
   ```
3. Update `manifest.json` directly (Edit tool on the JSON file):
   - Add to `incorporates` array if a debate conclusion was applied
   - Add to `resolves` array if a simulation gap was addressed
   - Clear `verified_by` to `""` (draft changed, not yet re-verified)
4. Run `bts validate` to verify schema compliance
5. Run the semantic pass on draft.md. **Invoke /bts-verify, /bts-audit
   and /bts-simulate in a SINGLE message so all three forks run
   concurrently** — they are independent (logical consistency vs.
   completeness vs. scenario coverage), read the same document, and
   share no state. Do NOT run them one after another, and do not hold
   one back for a later round.

   All three from the first full pass, not just the first two. Rotating
   instruments across the loop looks like thoroughness and behaves like
   a reset: a measured recipe ran audit at rounds 1-2, then not again
   until rounds 10 and 16, and each return produced five majors nobody
   had seen. The document had not got worse — a different instrument was
   pointed at it. Findings that arrive on round 10 cost ten rounds of
   IMPROVE built on a draft that had not been audited; the same findings
   on round 1 cost one edit. And a round's counts are only comparable to
   rounds of the same measurement class, so a loop that keeps changing
   class is a loop whose convergence budget never accumulates.

   Save the verify findings to `verification.md` (overwrite previous).
6. After /verify, update manifest: set draft.md `verified_by` to `"verification.md"`
7. Record verify results to verify-log (atomic — parses the `<bts-findings>`
   block from verification.md so counts can never drift):
   ```bash
   bts recipe log {id} --from-verification .bts/specs/recipes/{id}/verification.md \
     --doc {verified-doc-path} --scope {full|delta} --dimension {verify|audit|simulate ...}
   ```
   `--doc` is REQUIRED: it scopes convergence and the findings ledger to
   that document and snapshots the revision — and the path must RESOLVE
   (a bare basename against the recipe directory, anything with a
   separator against the project root). A path that resolves nowhere
   records no revision, which silently disarms both rule-3 gates and the
   completion replication check; `bts recipe log` warns on stderr when
   that happens. `--scope` records whether
   the round covered the whole document or only the changed sections
   plus their reference closure (`bts-verification-protocol.md §
   Verification Scope`). Iteration auto-increments per document.

   `--dimension` is REQUIRED and names which semantic passes produced
   these counts — one flag per pass actually run this round
   (`--dimension verify --dimension audit`). Pass only what ran: a round
   claiming a dimension it did not run makes the budget compare
   incomparable numbers, which is the specific defect this flag exists
   to prevent. Rounds are only judged against rounds of the same
   dimensions and scope, and completion needs all three
   (`bts-verification-protocol.md § Measurement Strength`). Omitting the
   flag is not the safe option: a dimensionless round is comparable with
   nothing, does not reset the convergence budget, and cannot count
   toward completion at all.
   Fallback (only if the findings block is missing): pass explicit SPLIT
   counts — `--iteration N --critical X --major Y --minor-resolvable R
   --minor-deferred D`. NEVER use the legacy `--minor` flag: it maps
   every minor to [resolvable] and blocks finalization even when only
   [deferred] minors remain (contradicting rule 3b).
   This writes to verify-log.jsonl which the stop hook checks at completion.

   **This command can fail on purpose.** A non-zero exit carrying
   `[CONVERGENCE FAILED]` means the convergence budget is exhausted —
   the round was logged, but do NOT start another IMPROVE cycle. Report
   the message and its stagnant finding IDs to the user and stop (this
   is the `[CONVERGENCE FAILED]` intervention point below).
8. Ask state before asking a model:
   ```bash
   bts recipe assess-precheck {id} --doc .bts/specs/recipes/{id}/draft.md
   ```
   - Prints a `<bts-decision>` (exit 0) → execute that action directly.
     **Skip /assess entirely** — the answer came from recorded state, so
     an assessment round would only re-derive it at the cost of a full
     document read.
   - `UNDECIDED` (exit 10) → run /assess for a judgement round.
9. **IMMEDIATELY execute the action** from the precheck or /assess. Do NOT
   output the assessment and stop. The loop is autonomous — continue executing
   until Level 3 is achieved or a human intervention point is reached.

**Refer to `.claude/rules/bts-schema.md` for exact JSON field names, types, and structures.**

### Intent Check (before vision/roadmap/scoping)

Before anything else, check if the intent is clear:

1. If `.bts/specs/recipes/{id}/intent.md` exists with Status: CONFIRMED → proceed.
2. If intent.md exists with Status: EXPLORING → re-present current understanding,
   continue discovery conversation until confirmed.
3. If no intent.md → run Skill("bts-discover") with the recipe topic.
   Wait for intent.md Status: CONFIRMED before proceeding.

After intent is confirmed, intent.md informs all subsequent decisions:
- Vision creation references intent's Purpose and Users
- Scope proposals are evaluated against intent's Success Criteria
- Out of Scope items justified by what the intent does NOT include

### Vision & Roadmap Check (before scoping)

Before scoping, check for project-level planning documents:

**1. Read existing vision/roadmap:**
   - `.bts/specs/vision.md` exists? → Read it.
     - Status CONFIRMED → check roadmap.
     - Status DRAFT → present vision for confirmation before proceeding.
   - `.bts/specs/roadmap.md` exists? → Read it. Find next pending `- [ ]` item.
     - Both exist and CONFIRMED → set scope target from roadmap's next pending item.
       Skip to Scoping Loop step 1 with context: "Roadmap item {N}/{total}: {description}"
     - No pending items → all done. Use AskUserQuestion: "Roadmap complete. What next?"
       - "Add new roadmap items" → update roadmap.md with new items, pick first new item as scope
       - "Start fresh as single recipe" → proceed without roadmap context
   - Vision exists but no roadmap → go to step 3b (create roadmap from existing vision).

**2. ASSESS_SIZE (only if no vision.md):**
   Analyze the user's request **based on the description alone** (no codebase scan yet):
   - Estimated files to create/modify
   - Number of distinct independent subsystems
   - Greenfield project? (check if project root has source files)

   **Decision:**
   | Condition | Action |
   |-----------|--------|
   | Greenfield + (files > `vision.size_threshold` (default: 15) OR 2+ independent subsystems) | Vision/Roadmap mandatory → step 3 |
   | Existing project + small addition (files ≤ threshold, single subsystem) | SKIP → Scoping Loop |
   | Ambiguous | Use AskUserQuestion: "This looks like a multi-recipe project." with options: "Create vision/roadmap to decompose" → step 3 / "Proceed as single recipe" → Scoping Loop |

**3. Create Vision & Roadmap:**
   a. **Vision**: Draft purpose, users, core components, constraints, success criteria.
      Write to `.bts/specs/vision.md` with Status: DRAFT.
      Present to user → confirm/adjust loop → Status: CONFIRMED.
   b. **Roadmap**: Decompose vision into `vision.min_roadmap_items`~`vision.max_roadmap_items`
      (default: 3~8) ordered items. Each item should be:
      - Implementable in one recipe session
      - Affecting a bounded set of files
      - Independently testable
      Write to `.bts/specs/roadmap.md` with Status: DRAFT.
      Present to user → confirm/adjust loop → Status: CONFIRMED.
   c. Select first pending roadmap item as this recipe's scope target.

**4. Proceed to Scoping Loop** with roadmap context (if any).

### Scoping (MANDATORY before adaptive loop)

Before any research or drafting, align scope with the user. This step
iterates until the user explicitly confirms.

Set phase to `scoping`:
```bash
bts recipe log {id} --phase scoping
```

#### Scoping Loop

**1. Analyze the request**: Parse the feature description. Identify ambiguities.

**2. Scan existing context**:
   - **Read project-map.md** (at `.bts/specs/project-map.md`) for the
     project layer overview: what layers exist, how to build/test each.
     If it doesn't exist but code exists, scan root to create it.
     If it doesn't exist and no code exists, skip (new project).
     If it exists, verify layer paths still exist (quick stat check).
     If any layer path is missing or new directories found → re-scan root
     to rebuild project-map.md before proceeding.
   - **Identify affected layers** for this feature
   - **Load affected layers' detail** from `.bts/specs/layers/{name}.md`.
     If detail doesn't exist for a layer, scan that layer's code to create it.
     Only load layers relevant to this feature — skip unrelated ones.
   - Scan codebase for anything layers might have missed (recent changes)
   - Check recent deviation.md files for follow-up items
   - Check recent review.md files for recurring quality/security patterns

**3. Propose scope**: Present to the user:
   ```
   ## Scope: {feature description}

   ### In Scope
   - [specific deliverable 1]
   - [specific deliverable 2]

   ### Out of Scope
   - [explicitly excluded item]

   ### Tech Stack Constraints
   - Language: [detected or proposed]
   - Framework: [detected or proposed]
   - Dependencies: [existing ones to reuse, new ones to add]

   ### Assumptions
   - [assumption about environment, users, scale]

   ### Complexity Estimate
   - Files to create/modify: ~N
   - Key challenges: [list]

   ### Intent Reference
   - Problem: {from intent.md}
   - Success Criteria: {from intent.md}

   ### Roadmap Reference (if roadmap exists)
   - Item: {N} of {total} — "{description}"
   - Prerequisites: {completed items or "none"}
   - Next: "{next item description}"

   ### Status: DRAFT
   ```

**4. Save immediately**: Write scope to `.bts/specs/recipes/{id}/scope.md`
   even before user confirms. This persists the conversation state so it
   survives compaction or session breaks.

**5. MUST use AskUserQuestion** to confirm scope — do NOT ask as text output:
   - "Confirm scope and proceed" → mark Status: CONFIRMED → exit loop
   - "Adjust scope" → user provides feedback → update scope.md → ask again
   - "Cancel recipe" → set phase to cancelled

**6. On resume** (session restart or compaction):
   - Read scope.md
   - If Status is DRAFT → present current scope and ask user to confirm/adjust
   - If Status is CONFIRMED → skip to adaptive loop

**7. Register with roadmap** (if roadmap exists):
   If this recipe's scope targets a roadmap item, annotate that item with the recipe ID.
   Read `.bts/specs/roadmap.md`, find the matching pending item, and add `(recipe: {id})`
   if not already present. This links the recipe to its roadmap item so completion
   tracking works correctly. Save roadmap.md.

**8. Log confirmation and transition phase**:
   ```bash
   bts recipe log {id} --phase research --action research --output scope.md --result "scope confirmed"
   ```

Phase is now `research`. Only after scope Status is CONFIRMED, proceed to the adaptive loop.

> **Checkpoint**: Scope confirmed. Continue directly to the adaptive loop.
> Do NOT /clear — work state is saved automatically and the recipe can always be resumed.

### Scope Re-opening

If the user requests a fundamental direction change during the adaptive loop
(different tech stack, different feature boundaries, pivot):

1. Acknowledge: "This changes the confirmed scope. Re-opening scope alignment."
2. Set phase back to scoping: `bts recipe log {id} --phase scoping`
3. Read current scope.md, apply the user's change, set Status: DRAFT
4. Present updated scope for confirmation
5. After re-confirmation (Status: CONFIRMED):
   - Assess impact on draft.md
   - If draft is invalidated → rewrite draft.md based on new scope
   - If draft is partially valid → IMPROVE draft.md to align with new scope
6. Resume adaptive loop

If the direction change affects the vision:
- Update `.bts/specs/vision.md` with changes, set Status: DRAFT, re-confirm
- Assess roadmap impact: which items are affected?
- Update `.bts/specs/roadmap.md` if items changed/added/removed

**When to re-open**: Any user statement whose intent contradicts the confirmed
scope — different technology, different boundaries, added/removed features,
or a fundamental shift in approach. Judge by intent, not by keywords.

### Entering the Adaptive Loop

**Starting from scratch (no existing code):**
1. /research — investigate technology, best practices, libraries.
   Research is scoped by `.bts/specs/recipes/{id}/scope.md`.
2. **/bts-domain-model — define entities, invariants (single owner),
   state partitioning, and illegal state cells. Creates `domain.md`.
   CANNOT skip — wireframe and architect gates require it.**
   > **Checkpoint**: After domain-model completes, continue IMMEDIATELY.
3. **/bts-architect — propose ≥2 alternative decompositions, debate
   them, adjudicate, and commit the winner as the
   `<!-- architect-decision -->` block. CANNOT skip —
   `bts verify wireframe.md` raises missing_architect_decision_block
   (major) without it. Tiny scopes (≤2 entities AND ≤3 files) may use
   the skill's skip condition, which still writes a minimal block.**
   > **Checkpoint**: After architect completes, continue IMMEDIATELY.
4. /bts-wireframe — design structure referencing `domain.md` entities,
   honoring the committed architect decision. Component
   responsibilities MUST honor the invariant owners declared in
   domain.md § 2. After saving, run `bts verify wireframe.md` and fix
   any critical/major before drafting.
   > **Checkpoint**: After wireframe completes, continue IMMEDIATELY.
5. Write initial draft (Level 1) referencing wireframe.md + domain.md
   → **Draft Self-Check** → draft.md → /verify
6. /assess → **execute** recommended action → loop runs autonomously
   until Level 3.

**Starting with existing code:**
1. /research — explore existing codebase, scoped by scope.md constraints.
2. **/bts-domain-model** — model the ADDED or CHANGED domain pieces.
   If existing domain docs live in `.bts/specs/layers/{name}.md`, load
   them and add only the delta for this recipe.
3. **/bts-architect** — same as above: ≥2 alternatives against the
   delta domain model, commit the decision block.
4. /bts-wireframe — design structure changes honoring domain.md
   invariants and the architect decision. Run `bts verify wireframe.md`
   after saving.
5. Write initial draft referencing wireframe.md + domain.md → **Draft
   Self-Check** → draft.md → /verify.
6. /assess → **execute** recommended action → loop runs autonomously
   until Level 3.

### Draft Self-Check (before /verify)

The previous self-check duplicated logic already covered by other
skills and broke `bts-verification-protocol.md`'s "never verify your
own output in the same context" rule. It is now split by responsibility:

- **Mechanical checks** run via `bts verify draft.md`:
  - Wireframe path anchor matching (every `<!-- path-id: X -->` in
    wireframe.md has a `<!-- path: wireframe.md#X -->` section in
    draft.md, and vice versa) — `engine/wireframe_anchor_checker.go`
  - File path / dependency declarations — existing consistency checker
  - Level criteria coverage — existing level assessor

- **Semantic checks** stay in fork context (separate Claude instance):
  - Contradiction detection, naming consistency, error-handling
    uniformity → `/bts-verify`
  - Completeness, missing cases, branch coverage → `/bts-audit`
  - Behavioral gaps, cross-boundary scenarios → `/bts-simulate`

After saving draft.md, run `bts verify .bts/specs/recipes/{id}/draft.md`
for the mechanical pass, then run `/bts-verify` for the semantic pass.
Do NOT re-implement either set inline here.

Also apply after every IMPROVE step, before the next /bts-verify run.

### ASSESS Decision Tree

The decision tree is **single-sourced in `bts-assess/SKILL.md`**. After each
`/assess`, read the `<bts-decision>` machine-readable block from the assess
output and execute the named action. The block's `phase` field is the next
phase to record:

```bash
bts recipe log {id} --phase <phase from <bts-decision>>
```

Action enum (see bts-assess § Part A for authoritative list): RESEARCH,
DEBATE, ADJUDICATE, SIMULATE, AUDIT, IMPROVE, VERIFY, SYNC_CHECK, FINALIZE,
SCOPE_REOPEN, WIREFRAME, DOMAIN_MODEL, ARCHITECT, and three HALT_* codes.

Do NOT maintain a parallel decision table here — keeping the tree in one
place prevents the drift that historically produced conflicting blueprint
and assess behavior.

### Quality Rules

1. **Every document modification → /verify.** No exceptions.
   The convergence budget (`verify.max_iterations`, default: 3) is
   enforced by `bts recipe log`, not by counting rounds yourself: after
   that many consecutive rounds without progress on
   `(critical, major, minor_resolvable)` the command exits non-zero with
   `[CONVERGENCE FAILED]` and names the stagnant finding IDs. Stop the
   loop there and ask the user. See `bts-verification-protocol.md §
   Convergence`.
1a. **Round scope.** The first verification of a document is a full
   pass. Later rounds may be `--scope delta` (changed sections plus
   their reference closure), which is what keeps a long loop cheap —
   but the round before finalization MUST be a full pass, and the stop
   hook blocks `<bts>DONE</bts>` otherwise. When in doubt, run full.
1b. **Findings carry forward.** Each verify round receives the previous
   rounds' adjudicated findings, so settled points are not re-derived.
   If a finding comes back as `reopened`, the last IMPROVE regressed
   something — fix the fix, do not treat it as a new finding. Inspect
   with `bts recipe findings list {id} --open`.
2. **Every debate conclusion → /adjudicate → if accepted → update draft → /verify.**
3. **Every simulation gap found → update draft → /verify.**
3a. **Prose-minimal IMPROVE — avoid amplification.** Each IMPROVE step
   resolves findings with the MINIMAL text change required. Adding prose
   around a fix expands the claim surface that the next /verify must
   re-check, which is the loop pattern that drove r-012's 13-iteration
   thrash.
   - Finding includes `Source:` citation → copy the citation verbatim
     into draft.md where the claim is made. The citation IS the
     justification; do NOT add speculative defense prose.
   - Finding has no citation (internal inconsistency, metadata error,
     duplicate declaration, `[resolvable]` minor without external claim)
     → fix in the smallest possible edit: one-line change or a strict
     delete. Do NOT add explanatory prose around the fix. The fix IS
     the resolution.
   - Framework-silent finding downgraded to MINOR → one concise
     acknowledgment line, not extensive rationalization.
   - `[deferred]` minors are never IMPROVE targets (see rule 3b).
3b. **Minor handling split.** /verify and /audit tag minors as `[resolvable]`
   or `[deferred]` per `bts-verification-protocol.md § Severity Classification`:
   - `[resolvable]` minors → fix directly in draft.md, then re-verify normally.
   - `[deferred]` minors → append to a "## Known Uncertainties" section at the
     end of draft.md. Each entry MUST use the heading form
     `### U-NNN: <short title>` (monotonic ids: U-001, U-002, …) followed
     by the finding description + the `Why-deferred:` observation copied
     verbatim — the stop hook and /bts-implement Step 5.7 parse exactly
     this shape; free-form bullets are invisible to both. Do NOT run
     IMPROVE or another /verify cycle for `[deferred]` minors — they are
     implementation watch-items.
   - Loop exit: when `/verify` shows ONLY `[deferred]` minors, `/bts-assess`
     will emit `action: FINALIZE` (see its "only [deferred] minors remain"
     branch). Follow that — do NOT call IMPROVE again. The deferred items
     carry into `/bts-implement` as a watch-list.
4. **All three instruments, every round.** /bts-verify, /bts-audit and
   /bts-simulate go in one concurrent batch (loop protocol step 5) from
   the first full pass onward. They read the same document and answer
   independent questions, so running them sequentially triples the
   wall-clock for no gain — and running them in different rounds makes
   each round's counts incomparable with the last, which is what the
   convergence budget is counting.
   - Completion requires a clean round from all three anyway
     (`bts-verification-protocol.md § Completion Evidence`). A dimension
     held back is a dimension whose findings arrive later, on top of
     more IMPROVE work.
   - The round cap (`verify.max_rounds`, default 6) counts rounds, not
     dimensions. Spending three of them on one instrument each buys
     nothing and costs half the budget.
5. **/debate for every uncertain technical choice.** Don't guess.
6. **/sync-check before finalizing.** All documents must be in sync.

### Debate → Adjudicate Flow

When /assess recommends "Technical decision needed":

```
/debate "topic"
  → conclusion
  → /adjudicate (evaluate feasibility, over-engineering, evidence quality)
    → ACCEPT → Edit draft.md with conclusion → /verify
    → EXTEND N/3 → preparation brief → research → /debate (next round)
                    → /adjudicate again (loop, max 3 extensions)
    → ACCEPT WITH RESERVATIONS → update draft + list caveats → /verify
```

The adjudicate step prevents poorly-supported conclusions from entering the spec.
Max `debate.max_extensions` (default: 3) debate extensions.

**Debate DEADLOCK handling:**
If /debate reports [DEBATE DEADLOCK] instead of a conclusion:
1. Do NOT run /adjudicate (there is no conclusion to evaluate)
2. Present the deadlock to the user with each expert's final position
3. User makes the decision → this becomes the "conclusion"
4. Run /adjudicate on the USER's decision (verify feasibility, scope, etc.)
5. If adjudicate rejects → present feedback to user, ask to reconsider

### File Structure

```
.bts/specs/recipes/{id}/
├── recipe.json
├── manifest.json
├── changelog.jsonl
├── verify-log.jsonl           # One entry per verify round, scoped by doc + scope
├── findings.jsonl             # Append-only findings ledger (stable IDs across rounds)
├── scope.md
├── research/v1.md
├── draft.md                  # Single file, Edit-based
├── verification.md            # Latest round only, overwritten each cycle —
│                              # cross-round history lives in findings.jsonl
├── debates/001-topic/
│   ├── meta.json
│   ├── round-1.md
│   └── round-2.md
├── simulations/001-scenarios.md
└── final.md
```

After each action:
- **Changelog**: `bts recipe log {id} --action [type] --output [path]`
- **Manifest relationships** (incorporates, resolves, verified_by): Edit `manifest.json` directly.
  The CLI creates/updates document entries but cannot set relationship fields.

### Finalization

When the precheck or /assess declares Level 3 achieved AND /sync-check passes:
0. **Confirm the last round was a full pass.** If the most recent verify
   entry for draft.md used `--scope delta`, run one more /bts-verify at
   full scope and record it with `--scope full` first. The precheck
   reports this as `action: VERIFY` with a "delta pass" reason; the stop
   hook blocks DONE on it either way.
1. Copy `draft.md` to `final.md`
2. Run Skill("bts-status") with arguments: {id}
   This updates project-status.md, roadmap.md, and project-map.md.
3. Output `<bts>DONE</bts>`
4. Stop hook will verify:
   - draft.md's own last verify entry: critical=0, major=0, no resolvable minors
   - that entry is a full pass, not a scoped delta pass
   - that entry is not `status: failed` (convergence budget exhausted)
   - All sync checks passed
5. Tell the user (plaintext, after the marker):
   > **Blueprint complete** — `{id}` spec finalized.
   > Next: run `/bts-implement {id}` to start implementation.

> **Checkpoint**: Blueprint finalized. Proceed directly to output `<bts>DONE</bts>`.
> Do NOT /clear — clearing loses context and requires re-reading files.

### Human Intervention Points

The loop runs automatically. **Do NOT stop between steps to summarize progress
or ask the user if they want to continue.** Execute each step and proceed to the next.

The loop pauses ONLY when:
- **[DECISION REQUIRED]**: A technical choice needs human judgment
- **[CONVERGENCE FAILED]**: Same issues persist after N iterations
- **[DEBATE DEADLOCK]**: Experts can't agree after 3 rounds

Any other reason to stop (including "let the user know what happened") is NOT valid.
Progress is tracked in changelog.jsonl and the user can check `/status` at any time.

## Output Target

A blueprint, not a transcription.

The document carries the part **code cannot cheaply falsify**, and stops
there. Concretely (`bts-level-criteria.md § Level 3` is authoritative):

- What ships, and what explicitly does not
- **Invariants and their owners** — every `INV-NNN` on a line that names
  the file that keeps it
- **Boundary contracts** — the exact shape of what crosses a wire, a
  schema, a stored row, a public API
- **Units and dependency order** — `wireframe.md § File Structure` is
  authoritative; reference it, do not copy it
- **Irreversible order and rollback** — migrations, rollouts, data
  transforms, and what undoes each
- **Falsifiers** — for every invariant, the test or observation that
  would prove it false. Names only
- **Known Uncertainties** — each with `Opens-with:` (the command that
  settles it) or `Why-deferred:`

What is deliberately NOT here: function signatures, type definitions,
code scaffolding, per-file walkthroughs, error enumerations, edge-case
tables, and test assertion values. A compiler, a type checker and one
test run settle every one of those in seconds. Arguing about them in
prose costs a verify round each — and every paragraph written to settle
one becomes a claim the next round has to re-check, in a document where
correcting a statement in one section falsifies a statement in another.

**Delegation is the rule, not a shortcut.** `domain.md`, `wireframe.md`
and `scope.md` are the recipe's other links, not drafts of this one.
Naming them is how the blueprint stays a blueprint; each copy is a second
place the same claim can go stale.

**Length is a diagnosis.** The skeleton of a feature is short — a few
hundred lines — because invariants are independent of one another and do
not accumulate seams. If the document is growing past
`verify.max_section_lines` per section, flesh has gotten in: find what a
compiler or a test would have settled and take it out.

**Code in the spec**: short snippets (5-15 lines) ONLY where a shape
cannot be stated any other way — a wire payload, a migration's ordering,
a tricky sequence. Never a function body. Implementation happens in
`/bts-implement`.

### The skeleton

Start from this shape. Sections may be renamed or merged when a recipe
genuinely has nothing for one, but nothing here is optional filler —
each is a Level 3 criterion.

```markdown
# Blueprint: {topic}

## 1. What ships
What the user gets, and — explicitly — what this does NOT do.

## 2. Invariants and owners
| ID | Statement | Owner |
|---|---|---|
| INV-001 | what is always true | `path/to/the/file/that/keeps/it` |

## 3. Boundary contracts
The exact shape of what crosses a wire, a schema, a stored row, an API.
| Layer | Name | Shape | Absence |
|---|---|---|---|

## 4. Units and dependency order
`wireframe.md § File Structure` is authoritative. Reference it. Do not
copy the table.

## 5. Irreversible order and rollback
Ordered steps, what each depends on, and what undoes it. Name the one
mistake that cannot be taken back.

## 6. Falsifiers
| Invariant | Falsifier |
|---|---|
| INV-001 | `path/to/the.spec.ts` — one clause on what going red means |

Names only. What the assertion should contain is decided while writing
the test.

## Known Uncertainties

### U-001: {the open question}
Opens-with: `{the command that settles it}`
Why-deferred: {the observation that would resolve it}
```

**Execution paths.** `bts verify` requires every `<!-- path-id: X -->`
in wireframe.md to be referenced by a `<!-- path: wireframe.md#X -->`
somewhere in the blueprint, so that no enumerated path ships without
anyone having said what it does. That is an anchor and a line — usually
in section 2 next to the invariant the path has to preserve, or in
section 6 next to its falsifier. It is **not** a walkthrough: one
measured recipe answered it with a 198-line section that re-narrated the
wireframe's own path enumeration, and the wireframe is where paths live.

Section 6 is what used to be "Test scenarios", and the difference is the
point: a scenario section states expected values, which is a claim about
unwritten code that only an execution can settle — and four rounds
arguing about one threshold is what a measured recipe actually spent.
A falsifier row names the thing that would go red and stops.

`bts verify` raises a major for every invariant section 6 leaves out
(`falsifier_assigned`), and the stop hook blocks `<bts>DONE</bts>` on it.

## Recovery: `recipe.json` stuck mid-phase

If a session ended before `<bts>DONE</bts>` was emitted, the stop hook
did not fire and `recipe.json` may still show a mid-blueprint phase
(e.g. `phase=simulate, level=0, iteration=0`) even though
`verify-log.jsonl` already shows `converged`. Recover with:

```bash
bts recipe reconcile <recipe-id>             # prompts dry-run first
bts recipe reconcile <recipe-id> --dry-run   # preview plan
```

Reconcile is idempotent, blueprint-only (never touches implement-phase
recipes), and writes `recipe.json.bak` before modifying anything. It
promotes the recipe to `phase=finalize, level=3.0, iteration=max`.
