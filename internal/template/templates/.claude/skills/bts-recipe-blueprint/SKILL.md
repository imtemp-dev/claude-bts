---
name: bts-recipe-blueprint
description: >
  Create a Level 3 implementation spec through an adaptive loop of research,
  drafting, debate, simulation, and verification. The loop continues until
  the document is bulletproof.
user-invocable: true
allowed-tools: Read Write Edit Grep Glob Bash Agent AskUserQuestion mcp__context7__resolve-library-id mcp__context7__get-library-docs
argument-hint: "\"feature description\""
---

# Recipe: Blueprint

Create a bulletproof implementation spec for: $ARGUMENTS

## Settings

Read `.bts/config/settings.yaml` for project-specific limits.
Use settings values if present, otherwise use defaults noted in each step.

## Resume Check

Before starting, check for an existing recipe:
```bash
bts recipe status
```
If active, check the phase to determine resume strategy:

**If phase is `scoping`:** Follow the Scoping Loop "On resume" protocol below —
read `scope.md` and re-present if Status is DRAFT, or skip to adaptive loop if CONFIRMED.

**If phase is any other (research, draft, verify, debate, etc.):** Resume with **minimum reads**:
1. `changelog.jsonl` — last 5 entries only (determine current position in the loop)
2. `draft.md` — the current draft (if not found, check `manifest.json` `current_draft` for legacy path)
3. `verification.md` — latest verification findings
4. `scope.md` — confirm scope is still valid

Do NOT read on resume: research documents (already incorporated into the draft).

Then run `/bts-assess` on the current draft to determine the next action.

## Adaptive Loop

This recipe does NOT follow a fixed sequence. Instead, it runs an adaptive loop:

```
ASSESS → decide action → execute → VERIFY (mandatory after any change) → ASSESS → ...
```

ASSESS determines what to do next based on the document's current state.

### Loop Protocol

**At recipe start (MANDATORY):**
1. Create `recipe.json` following the schema in bts-schema rules
2. Create `manifest.json` following the schema in bts-schema rules
3. Run `bts validate` to confirm both files are schema-compliant

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
5. Run /verify on draft.md → save findings to `verification.md` (overwrite previous)
6. After /verify, update manifest: set draft.md `verified_by` to `"verification.md"`
7. Record verify results to verify-log:
   ```bash
   bts recipe log {id} --iteration N --critical X --major Y --minor Z
   ```
   This writes to verify-log.jsonl which the stop hook checks at completion.
8. Run /assess to determine the next action

**Refer to `.claude/rules/bts-schema.md` for exact JSON field names, types, and structures.**

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
   - **Read project-map.md** (at `.bts/state/project-map.md`) for the
     project layer overview: what layers exist, how to build/test each.
     If it doesn't exist but code exists, scan root to create it.
     If it doesn't exist and no code exists, skip (new project).
     If it exists, verify layer paths still exist (quick stat check).
     If any layer path is missing or new directories found → re-scan root
     to rebuild project-map.md before proceeding.
   - **Identify affected layers** for this feature
   - **Load affected layers' detail** from `.bts/state/layers/{name}.md`.
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

   ### Status: DRAFT
   ```

**4. Save immediately**: Write scope to `.bts/state/recipes/{id}/scope.md`
   even before user confirms. This persists the conversation state so it
   survives compaction or session breaks.

**5. Ask user for confirmation** using AskUserQuestion:
   - "Confirm scope and proceed (Recommended)" → mark Status: CONFIRMED → exit loop
   - "Adjust scope" → user provides feedback → update scope.md → ask again
   - "Cancel recipe" → set phase to cancelled

**6. On resume** (session restart or compaction):
   - Read scope.md
   - If Status is DRAFT → present current scope and ask user to confirm/adjust
   - If Status is CONFIRMED → skip to adaptive loop

**7. Log confirmation and transition phase**:
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

**Trigger words**: "바꾸자", "변경", "pivot", "다른 방향", "scope change",
or any user statement that contradicts the confirmed scope.

### Entering the Adaptive Loop

**Starting from scratch (no existing code):**
1. /research — investigate technology, best practices, libraries.
   Research is scoped by `.bts/state/recipes/{id}/scope.md`.
2. Write initial draft (Level 1) → **Draft Self-Check** → draft.md → /verify
3. /assess → loop begins

**Starting with existing code:**
1. /research — explore existing codebase, scoped by scope.md constraints.
2. Write initial draft referencing existing code → **Draft Self-Check** → draft.md → /verify
3. /assess → loop begins

### Draft Self-Check (before /verify)

After writing a draft, run through this checklist BEFORE saving and running /verify.
This catches obvious errors that would waste a verify cycle (~5 min each).

Every function/method in the draft must pass:
- [ ] **Defined**: Body is specified (no `...` or `pass` placeholders)
- [ ] **Callable**: All functions it calls are also defined in the draft
- [ ] **Importable**: All imports reference real packages (verified in research)
- [ ] **Typed**: Parameters and return types are explicit, not inferred
- [ ] **Connected**: Every function has at least one caller or is a public API entry

Every file in the draft must pass:
- [ ] **Path valid**: File path is consistent with project structure
- [ ] **Dependencies listed**: All external packages in pyproject.toml / package.json / go.mod

Cross-section consistency:
- [ ] **No contradictions**: Error handling strategy is the same across all sections
- [ ] **Naming consistent**: Same concept uses same name everywhere
- [ ] **Config matches usage**: Config fields defined match how they're accessed in code

If any check fails → fix it in the draft before saving. This is proofreading,
not verification (which requires a separate context).

Also apply this checklist after every IMPROVE step, before /verify.

### ASSESS Decision Tree

After each /assess, update phase and execute the recommended action:

| Assessment | Phase | Action | Details |
|------------|-------|--------|---------|
| "Scope issue found" | scoping | Scope Re-opening | Research flagged infeasible/missing scope items |
| "Information insufficient" | research | /research | Investigate docs, APIs, libraries |
| "Technical decision needed" | debate | /debate → /adjudicate | 3 experts, then evaluate. Pass current draft path for expert reference |
| "Gaps may exist" | simulate | /simulate | Design 5+ scenarios. Walk through spec |
| "Content missing for next level" | draft | IMPROVE | Add specific items. Edit draft.md |
| "Contradictions suspected" | verify | /verify | Check internal consistency |
| "Completeness uncertain" | audit | /audit | Review for missing cases |
| "Level 3 achieved" | verify | /sync-check | Final cross-document verification |

Update phase before each action:
```bash
bts recipe log {id} --phase [phase from table above]
```
This keeps session-start hints accurate if session breaks mid-loop.

### Quality Rules

1. **Every document modification → /verify.** No exceptions.
   **Max `verify.max_iterations` (default: 3) consecutive IMPROVE→VERIFY cycles without level change.**
   If that many cycles pass and the level hasn't increased, report [CONVERGENCE FAILED]
   and ask the user for guidance. Check verify-log.jsonl iteration count.
2. **Every debate conclusion → /adjudicate → if accepted → update draft → /verify.**
3. **Every simulation gap found → update draft → /verify.**
4. **/simulate early**: Run after the FIRST verify cycle that produces critical=0.
   Simulation catches scenario-level gaps (failure modes, race conditions, edge cases)
   that structural verification cannot find. Running it early prevents late-stage rework.
   - First verify has critical=0 → run /simulate immediately (before more IMPROVE cycles)
   - First verify has critical>0 → fix criticals first, then /simulate
   - Run /simulate again before finalization if major structural changes were made
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
.bts/state/{id}/
├── recipe.json
├── manifest.json
├── changelog.jsonl
├── verify-log.jsonl
├── scope.md
├── research/v1.md
├── draft.md                  # Single file, Edit-based
├── verification.md            # Single file, overwritten each cycle
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

When /assess declares Level 3 achieved AND /sync-check passes:
1. Copy `draft.md` to `final.md`
2. Output `<bts>DONE</bts>`
3. Stop hook will verify:
   - verify-log last entry: critical=0, major=0
   - All sync checks passed

> **Checkpoint**: Blueprint finalized. Proceed directly to output `<bts>DONE</bts>`.
> Do NOT /clear — clearing loses context and requires re-reading files.

### Human Intervention Points

The loop runs automatically. It pauses ONLY when:
- **[DECISION REQUIRED]**: A technical choice needs human judgment
- **[CONVERGENCE FAILED]**: Same issues persist after N iterations
- **[DEBATE DEADLOCK]**: Experts can't agree after 3 rounds

## Output Target

The final document should contain, for every component:
- Exact file paths (create/modify)
- Function signatures (name, params with types, return type)
- Data types and interfaces
- Connection points to other components
- Error handling for every failure mode
- Edge cases enumerated
- Code scaffolding (skeleton structure)
- Test scenarios (happy + error + edge)

When this document is given to Claude Opus, it should generate working code
with minimal additional iteration.
