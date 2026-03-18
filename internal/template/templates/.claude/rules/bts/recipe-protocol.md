---
paths:
  - ".bts/**"
---

# BTS Recipe Protocol

## Adaptive Loop

Recipes use an adaptive loop, NOT a fixed sequence:

```
ASSESS → decide action → execute → VERIFY (mandatory) → ASSESS → ...
```

/assess determines what to do next based on the document's current state and level.

## Mandatory Rules

1. **Check for resume first**: `bts recipe status` before starting any recipe.
2. **Never overwrite drafts**: Save as `drafts/vN+1.md`, preserve all versions.
3. **VERIFY after every modification**: No exceptions. This includes post-debate and post-simulation fixes.
4. **Log every action**: `bts recipe log {id}` after every step.
5. **Simulate at least once**: Before declaring Level 3, run /simulate with 5+ scenarios.
6. **Debate uncertain choices**: Don't guess. Use /debate for technology decisions.
7. **Sync-check before finalizing**: All debates reflected, all gaps resolved, all drafts verified.

## Human Intervention

The loop runs automatically. It pauses ONLY for:
- **[DECISION REQUIRED]**: Human must choose between alternatives
- **[CONVERGENCE FAILED]**: Same issues after N iterations
- **[DEBATE DEADLOCK]**: Experts can't agree after 3 rounds

## Completion

Output `<bts>DONE</bts>` only when:
1. /assess declares Level 3
2. /sync-check passes
3. Last verify-log entry shows critical=0, major=0
