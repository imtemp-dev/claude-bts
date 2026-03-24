---
paths:
  - ".bts/**"
---

# BTS Recipe Protocol

## Development Lifecycle

bts maps to a standard development process:

```
Requirements → Planning → Design → Implementation → Verification → Release
     ↓            ↓         ↓           ↓                ↓           ↓
  discover     vision    blueprint   implement        test+review   sync+status
  (intent)    roadmap    (spec)      (code)          simulate      (complete)
               scope
```

### Phase Flow

```
discovery → scoping → research → draft ←→ adaptive loop → finalize
                                              ↓
                                   implement → test → simulate(code)
                                              → review → sync → status → complete
```

### Recipe Types and Their Lifecycle

| Recipe | Phases Used | Output |
|--------|-------------|--------|
| blueprint | discovery → scoping → adaptive loop → finalize → implement → complete | Level 3 spec + code |
| design | research → adaptive loop → finalize | Level 2 design doc |
| analyze | research → adaptive loop → finalize | Level 1 analysis doc |
| fix | research → draft → verify → implement → test → review → complete | fix-spec.md + code |
| debug | research → perspectives → draft → verify → finalize → implement → complete | debug spec + code |

### Project-Level Documents

These persist across recipes and provide continuity:

| Document | Created | Updated | Purpose |
|----------|---------|---------|---------|
| intent.md | /bts-discover | per-recipe | Why this feature exists |
| vision.md | blueprint scoping | direction changes | Final product vision |
| roadmap.md | blueprint scoping | recipe completion | Ordered recipe sequence |
| project-map.md | /bts-status | recipe completion | Layer overview |
| project-status.md | /bts-status | recipe completion | All recipes status |

## Adaptive Loop

Recipes use an adaptive loop, NOT a fixed sequence:

```
ASSESS → decide action → execute → VERIFY (mandatory) → ASSESS → ...
```

/assess determines what to do next based on the document's current state and level.

## Mandatory Rules

1. **Check for resume first**: `bts recipe status` before starting any recipe.
2. **Edit draft.md in place**: Use Edit tool for incremental modifications, not full rewrites.
3. **VERIFY after every modification**: No exceptions. This includes post-debate and post-simulation fixes.
4. **Log every action**: `bts recipe log {id}` after every step.
5. **Simulate at least once**: Before declaring Level 3, run /simulate with 5+ scenarios.
6. **Debate uncertain choices**: Don't guess. Use /debate for technology decisions.
7. **Adjudicate every debate**: After /debate, ALWAYS run /adjudicate to evaluate the conclusion. Never accept a debate result without adjudication.
8. **Sync-check before finalizing**: All debates reflected, all gaps resolved, all drafts verified.
9. **Run /bts-status at finalization**: Every recipe calls bts-status before completion to update roadmap, project-status, and project-map.

## Human Intervention

The loop runs automatically. It pauses ONLY for:
- **[DECISION REQUIRED]**: Human must choose between alternatives
- **[CONVERGENCE FAILED]**: Same issues after N iterations
- **[DEBATE DEADLOCK]**: Experts can't agree after 3 rounds

## Completion

### Spec Completion
Output `<bts>DONE</bts>` only when:
1. /assess declares Level 3
2. /sync-check passes
3. Last verify-log entry shows critical=0, major=0

### Implementation Completion
Output `<bts>IMPLEMENT DONE</bts>` only when:
1. All tasks in tasks.json are `done` or `skipped` (no `blocked` or `pending`)
2. test-results.json shows status=pass
3. /review has run (review.md exists)
4. /sync has run (deviation.md exists)

deviation.md is a **report**, not a gate. Deviations and not-implemented items
are recorded for follow-up but do NOT block completion.

### Fix Completion
Output `<bts>FIX DONE</bts>` only when:
1. fix-spec.md exists (fix documented)
2. test-results.json shows status=pass

Fix recipes implement code directly (not via /bts-implement).
fix-spec.md is the authoritative document, not final.md.

### Follow-up After Completion
After a recipe reaches `complete`, its deviation.md feeds the next cycle:
- Not-implemented items → new recipe or manual fix
- Deviations → bug fix or spec update
- The spec (final.md) stays alive — it evolves through follow-up work

### Roadmap Progression
If `.bts/state/roadmap.md` exists:
- The recipe's roadmap item is marked done (stop hook + /bts-status)
- Session-start hint shows the next pending item
- The next /bts-recipe-blueprint scoping reads roadmap context automatically
