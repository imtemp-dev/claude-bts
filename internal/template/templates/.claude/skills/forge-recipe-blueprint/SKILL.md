---
name: forge-recipe-blueprint
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

**This recipe creates a SPEC DOCUMENT, not code.**
Do NOT write source code files (.ts, .js, .go, .py, .rs, etc.) during this recipe.
Only create documents in `.forge/state/recipes/{id}/`.
Code implementation happens in `/forge-implement` AFTER this recipe completes with `<forge>DONE</forge>`.

## Settings

Read `.forge/config/settings.yaml` for project-specific limits.
Use settings values if present, otherwise use defaults noted in each step.

## Resume Check

Before starting, check for an existing recipe:
```bash
forge recipe status
```
If active, check the phase to determine resume strategy:

**If phase is `discovery`:** Read intent.md.
- Status EXPLORING → continue discovery conversation using AskUserQuestion
- Status CONFIRMED → proceed to Vision & Roadmap Check

**If phase is `scoping`:** Check vision/roadmap state first (in order):
1. If `.forge/state/vision.md` exists with Status: DRAFT → re-present vision for confirmation.
   After vision confirmed, check roadmap below.
2. If `.forge/state/vision.md` CONFIRMED but `.forge/state/roadmap.md` missing →
   go to Vision & Roadmap Check step 3b (create roadmap from confirmed vision).
3. If `.forge/state/roadmap.md` exists with Status: DRAFT → re-present roadmap for confirmation.
4. If scope.md exists → follow the Scoping Loop "On resume" protocol below —
   re-present if Status is DRAFT, or skip to adaptive loop if CONFIRMED.
5. If scope.md does not exist → go to Scoping Loop step 1 (start scoping with roadmap context).

**If phase is any other (research, draft, verify, debate, etc.):** Resume with **minimum reads**:
1. `changelog.jsonl` — last 5 entries only (determine current position in the loop)
2. `draft.md` — the current draft (if not found, check `manifest.json` `current_draft` for legacy path)
3. `verification.md` — latest verification findings
4. `scope.md` — confirm scope is still valid

Do NOT read on resume: research documents (already incorporated into the draft).

Then run `/forge-assess` on the current draft to determine the next action.

## Adaptive Loop

This recipe does NOT follow a fixed sequence. Instead, it runs an adaptive loop:

```
ASSESS → decide action → execute → VERIFY (mandatory after any change) → ASSESS → ...
```

ASSESS determines what to do next based on the document's current state.

### Loop Protocol

**At recipe start (MANDATORY):**
1. Check `forge recipe status`. If no active recipe exists:
   ```bash
   forge recipe create --type blueprint --topic "$ARGUMENTS"
   ```
   This creates `recipe.json` and `manifest.json` automatically and outputs the recipe ID.
2. Run `forge validate` to confirm schema compliance

**ALWAYS after modifying any JSON file in .forge/:**
1. Run `forge validate` to verify schema compliance. Fix any errors before continuing.

**ALWAYS after modifying draft.md:**
1. Edit `draft.md` in place (Write for initial creation, Edit for improvements)
2. Log the action to changelog:
   ```bash
   forge recipe log {id} --action [draft|improve] --output draft.md
   ```
3. Update `manifest.json` directly (Edit tool on the JSON file):
   - Add to `incorporates` array if a debate conclusion was applied
   - Add to `resolves` array if a simulation gap was addressed
   - Clear `verified_by` to `""` (draft changed, not yet re-verified)
4. Run `forge validate` to verify schema compliance
5. Run /verify on draft.md → save findings to `verification.md` (overwrite previous)
6. After /verify, update manifest: set draft.md `verified_by` to `"verification.md"`
7. Record verify results to verify-log:
   ```bash
   forge recipe log {id} --iteration N --critical X --major Y --minor Z
   ```
   This writes to verify-log.jsonl which the stop hook checks at completion.
8. Run /assess to determine the next action

**Refer to `.claude/rules/forge-schema.md` for exact JSON field names, types, and structures.**

### Intent Check (before vision/roadmap/scoping)

Before anything else, check if the intent is clear:

1. If `.forge/state/recipes/{id}/intent.md` exists with Status: CONFIRMED → proceed.
2. If intent.md exists with Status: EXPLORING → re-present current understanding,
   continue discovery conversation until confirmed.
3. If no intent.md → run Skill("forge-discover") with the recipe topic.
   Wait for intent.md Status: CONFIRMED before proceeding.

After intent is confirmed, intent.md informs all subsequent decisions:
- Vision creation references intent's Purpose and Users
- Scope proposals are evaluated against intent's Success Criteria
- Out of Scope items justified by what the intent does NOT include

### Vision & Roadmap Check (before scoping)

Before scoping, check for project-level planning documents:

**1. Read existing vision/roadmap:**
   - `.forge/state/vision.md` exists? → Read it.
     - Status CONFIRMED → check roadmap.
     - Status DRAFT → present vision for confirmation before proceeding.
   - `.forge/state/roadmap.md` exists? → Read it. Find next pending `- [ ]` item.
     - Both exist and CONFIRMED → set scope target from roadmap's next pending item.
       Skip to Scoping Loop step 1 with context: "Roadmap item {N}/{total}: {description}"
     - No pending items → all done. Ask user: "Roadmap complete. Add new items or start fresh?"
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
   | Ambiguous | Ask user: "This looks like a multi-recipe project. Create a vision/roadmap to decompose, or proceed as single recipe?" |

**3. Create Vision & Roadmap:**
   a. **Vision**: Draft purpose, users, core components, constraints, success criteria.
      Write to `.forge/state/vision.md` with Status: DRAFT.
      Present to user → confirm/adjust loop → Status: CONFIRMED.
   b. **Roadmap**: Decompose vision into `vision.min_roadmap_items`~`vision.max_roadmap_items`
      (default: 3~8) ordered items. Each item should be:
      - Implementable in one recipe session
      - Affecting a bounded set of files
      - Independently testable
      Write to `.forge/state/roadmap.md` with Status: DRAFT.
      Present to user → confirm/adjust loop → Status: CONFIRMED.
   c. Select first pending roadmap item as this recipe's scope target.

**4. Proceed to Scoping Loop** with roadmap context (if any).

### Scoping (MANDATORY before adaptive loop)

Before any research or drafting, align scope with the user. This step
iterates until the user explicitly confirms.

Set phase to `scoping`:
```bash
forge recipe log {id} --phase scoping
```

#### Scoping Loop

**1. Analyze the request**: Parse the feature description. Identify ambiguities.

**2. Scan existing context**:
   - **Read project-map.md** (at `.forge/state/project-map.md`) for the
     project layer overview: what layers exist, how to build/test each.
     If it doesn't exist but code exists, scan root to create it.
     If it doesn't exist and no code exists, skip (new project).
     If it exists, verify layer paths still exist (quick stat check).
     If any layer path is missing or new directories found → re-scan root
     to rebuild project-map.md before proceeding.
   - **Identify affected layers** for this feature
   - **Load affected layers' detail** from `.forge/state/layers/{name}.md`.
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

**4. Save immediately**: Write scope to `.forge/state/recipes/{id}/scope.md`
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

**7. Register with roadmap** (if roadmap exists):
   If this recipe's scope targets a roadmap item, annotate that item with the recipe ID.
   Read `.forge/state/roadmap.md`, find the matching pending item, and add `(recipe: {id})`
   if not already present. This links the recipe to its roadmap item so completion
   tracking works correctly. Save roadmap.md.

**8. Log confirmation and transition phase**:
   ```bash
   forge recipe log {id} --phase research --action research --output scope.md --result "scope confirmed"
   ```

Phase is now `research`. Only after scope Status is CONFIRMED, proceed to the adaptive loop.

> **Checkpoint**: Scope confirmed. Continue directly to the adaptive loop.
> Do NOT /clear — work state is saved automatically and the recipe can always be resumed.

### Scope Re-opening

If the user requests a fundamental direction change during the adaptive loop
(different tech stack, different feature boundaries, pivot):

1. Acknowledge: "This changes the confirmed scope. Re-opening scope alignment."
2. Set phase back to scoping: `forge recipe log {id} --phase scoping`
3. Read current scope.md, apply the user's change, set Status: DRAFT
4. Present updated scope for confirmation
5. After re-confirmation (Status: CONFIRMED):
   - Assess impact on draft.md
   - If draft is invalidated → rewrite draft.md based on new scope
   - If draft is partially valid → IMPROVE draft.md to align with new scope
6. Resume adaptive loop

If the direction change affects the vision:
- Update `.forge/state/vision.md` with changes, set Status: DRAFT, re-confirm
- Assess roadmap impact: which items are affected?
- Update `.forge/state/roadmap.md` if items changed/added/removed

**When to re-open**: Any user statement whose intent contradicts the confirmed
scope — different technology, different boundaries, added/removed features,
or a fundamental shift in approach. Judge by intent, not by keywords.

### Entering the Adaptive Loop

**Starting from scratch (no existing code):**
1. /research — investigate technology, best practices, libraries.
   Research is scoped by `.forge/state/recipes/{id}/scope.md`.
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
forge recipe log {id} --phase [phase from table above]
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
.forge/state/{id}/
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
- **Changelog**: `forge recipe log {id} --action [type] --output [path]`
- **Manifest relationships** (incorporates, resolves, verified_by): Edit `manifest.json` directly.
  The CLI creates/updates document entries but cannot set relationship fields.

### Finalization

When /assess declares Level 3 achieved AND /sync-check passes:
1. Copy `draft.md` to `final.md`
2. Run Skill("forge-status") with arguments: {id}
   This updates project-status.md, roadmap.md, and project-map.md.
3. Output `<forge>DONE</forge>`
3. Stop hook will verify:
   - verify-log last entry: critical=0, major=0
   - All sync checks passed

> **Checkpoint**: Blueprint finalized. Proceed directly to output `<forge>DONE</forge>`.
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
- Data types and interfaces (full type definitions)
- Connection points to other components
- Error handling strategy for every failure mode
- Edge cases enumerated
- Test scenarios (happy + error + edge)

**Code in the spec**: Use short code snippets (5-15 lines) ONLY to clarify
non-obvious logic — algorithms, tricky transformations, critical sequences.
Do NOT write full function implementations. The spec describes WHAT and WHY,
not the complete HOW. Implementation happens in `/forge-implement`.
