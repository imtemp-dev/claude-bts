---
name: bts-recipe-blueprint
description: >
  Create a Level 3 implementation spec through an adaptive loop of research,
  drafting, debate, simulation, and verification. The loop continues until
  the document is bulletproof.
user-invocable: true
allowed-tools: Read Write Edit Grep Glob Bash Agent mcp__context7__resolve-library-id mcp__context7__get-library-docs
argument-hint: "\"feature description\""
---

# Recipe: Blueprint

Create a bulletproof implementation spec for: $ARGUMENTS

## Resume Check

Before starting, check for an existing recipe:
```bash
bts recipe status
```
If active, read `.bts/state/{id}/manifest.json` and `changelog.jsonl` to understand
what has been done. Resume from where it left off.

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

**ALWAYS after modifying the document:**
1. Save as new version: `drafts/vN.md` (never overwrite previous versions)
2. Update `manifest.json` (add document entry with type, created_at, based_on)
3. Append to `changelog.jsonl` (use key `"time"`, not `"timestamp"`)
4. Run `bts validate` to verify
5. Run /verify on the new version
6. Run /assess to determine the next action

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

**2. Scan existing context**: Quick codebase scan to understand:
   - Current tech stack (language, framework, dependencies)
   - Existing patterns and conventions
   - Related code that already exists

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

**5. Wait for user response**:
   - User adjusts → update scope.md → present updated scope → wait again
   - User confirms → mark `### Status: CONFIRMED` in scope.md → exit loop
   - User asks unrelated question → answer it, then remind:
     "[bts] Scope alignment in progress for recipe {id}. Current scope is at
     .bts/state/recipes/{id}/scope.md. Ready to continue?"

**6. On resume** (session restart or compaction):
   - Read scope.md
   - If Status is DRAFT → present current scope and ask user to confirm/adjust
   - If Status is CONFIRMED → skip to adaptive loop

**7. Log confirmation and transition phase**:
   ```bash
   bts recipe log {id} --phase research --action research --output scope.md --result "scope confirmed"
   ```

Phase is now `research`. Only after scope Status is CONFIRMED, proceed to the adaptive loop.

> **Checkpoint**: Scope confirmed. If context is heavy from codebase exploration,
> consider `/clear` before the adaptive loop. Work state is saved automatically.

### Scope Re-opening

If the user requests a fundamental direction change during the adaptive loop
(different tech stack, different feature boundaries, pivot):

1. Acknowledge: "This changes the confirmed scope. Re-opening scope alignment."
2. Set phase back to scoping: `bts recipe log {id} --phase scoping`
3. Read current scope.md, apply the user's change, set Status: DRAFT
4. Present updated scope for confirmation
5. After re-confirmation (Status: CONFIRMED):
   - Assess impact on existing drafts
   - If draft is invalidated → start fresh draft (new vN based on new scope)
   - If draft is partially valid → IMPROVE to align with new scope
6. Resume adaptive loop

**Trigger words**: "바꾸자", "변경", "pivot", "다른 방향", "scope change",
or any user statement that contradicts the confirmed scope.

### Entering the Adaptive Loop

**Starting from scratch (no existing code):**
1. /research — investigate technology, best practices, libraries (guided by scope)
2. Write initial draft (Level 1) → drafts/v1.md → /verify
3. /assess → loop begins

**Starting with existing code:**
1. /research — explore existing codebase (focused by scope constraints)
2. Write initial draft referencing existing code → drafts/v1.md → /verify
3. /assess → loop begins

### ASSESS Decision Tree

After each /assess, update phase and execute the recommended action:

| Assessment | Phase | Action | Details |
|------------|-------|--------|---------|
| "Scope issue found" | scoping | Scope Re-opening | Research flagged infeasible/missing scope items |
| "Information insufficient" | research | /research | Investigate docs, APIs, libraries |
| "Technical decision needed" | debate | /debate → /adjudicate | 3 experts, then evaluate conclusion |
| "Gaps may exist" | simulate | /simulate | Design 5+ scenarios. Walk through spec |
| "Content missing for next level" | draft | IMPROVE | Add specific items. Save as new draft |
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
   **Max 3 consecutive IMPROVE→VERIFY cycles without level change.**
   If 3 cycles pass and the level hasn't increased, report [CONVERGENCE FAILED]
   and ask the user for guidance. Check verify-log.jsonl iteration count.
2. **Every debate conclusion → /adjudicate → if accepted → update draft → /verify.**
3. **Every simulation gap found → update draft → /verify.**
4. **/simulate at least once** before declaring Level 3.
5. **/debate for every uncertain technical choice.** Don't guess.
6. **/sync-check before finalizing.** All documents must be in sync.

### Debate → Adjudicate Flow

When /assess recommends "Technical decision needed":

```
/debate "topic"
  → conclusion
  → /adjudicate (evaluate feasibility, over-engineering, evidence quality)
    → ACCEPT → update draft with conclusion → /verify
    → EXTEND N/3 → preparation brief → research → /debate (next round)
                    → /adjudicate again (loop, max 3 extensions)
    → ACCEPT WITH RESERVATIONS → update draft + list caveats → /verify
```

The adjudicate step prevents poorly-supported conclusions from entering the spec.
Max 3 debate extensions (3 original rounds + up to 3 additional rounds = 6 total).

**Debate DEADLOCK handling:**
If /debate reports [DEBATE DEADLOCK] instead of a conclusion:
1. Do NOT run /adjudicate (there is no conclusion to evaluate)
2. Present the deadlock to the user with each expert's final position
3. User makes the decision → this becomes the "conclusion"
4. Run /adjudicate on the USER's decision (verify feasibility, scope, etc.)
5. If adjudicate rejects → present feedback to user, ask to reconsider

### Version Management

Every draft version is preserved. Never overwrite.
```
.bts/state/{id}/
├── manifest.json            # Document relationships
├── changelog.jsonl           # Every action logged
├── research/v1.md
├── drafts/
│   ├── v1.md                # Initial draft
│   ├── v2.md                # After debate-001
│   ├── v3.md                # After simulation gaps
│   └── v4.md                # After audit items
├── debates/001-topic/
│   ├── meta.json
│   ├── round-1.md
│   └── round-2.md
├── simulations/001-scenarios.md
├── verifications/draft-v1.md
└── final.md
```

After each action, update manifest.json:
```bash
bts recipe log {id} --action [type] --output [path] --based-on [deps]
```

### Finalization

When /assess declares Level 3 achieved AND /sync-check passes:
1. Copy current draft to `final.md`
2. Output `<bts>DONE</bts>`
3. Stop hook will verify:
   - verify-log last entry: critical=0, major=0
   - All sync checks passed

> **Checkpoint**: Blueprint finalized. Run `/clear` before `/implement` to
> maximize context window for code generation. Work state is saved automatically.

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
