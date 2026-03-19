---
name: bts-sync
description: >
  Synchronize final.md spec with actual implementation. Finds deviations between
  spec and code, updates final.md to reflect reality, and generates deviation report.
user-invocable: true
allowed-tools: Read Write Edit Grep Glob Bash Agent
argument-hint: "[recipe-id]"
---

# Sync: final.md ↔ Code

Synchronize spec and implementation for recipe: $ARGUMENTS

## Prerequisites

1. Verify test-results.json exists:
   ```bash
   ls .bts/state/recipes/{id}/test-results.json
   ```
   If not found → "Run /test first. Sync requires passing tests."

2. Check test status: should be `"pass"`.
   If `"fail"` → "Tests are failing. Fix tests before syncing."

## Step 1: Extract Spec Definitions

Read `.bts/state/recipes/{id}/final.md` and extract:
- All file paths mentioned
- All function/method signatures (name, params, return type)
- All type/interface/struct definitions
- All API endpoints (if applicable)
- All configuration values
- All error handling behaviors

## Step 2: Scan Actual Code

For each file path from the spec:
1. Check if the file exists
2. If it exists, read it and extract:
   - Actual function signatures
   - Actual type definitions
   - Actual error handling
   - Any additional functions/types not in spec

Also scan for code files NOT mentioned in the spec that appear related
(same directory, similar naming).

## Step 3: Compare and Classify

For each item, classify as one of:

### Match
Spec and code agree. No action needed.

### Not Implemented
Spec defines it but code doesn't have it.
- Record in deviation report
- Severity: major (if core functionality) or minor (if optional)

### Spec Addition Needed (non-blocking)
Code has it but spec doesn't mention it.
- This happens when implementation required additional helpers, utilities, or types
- Record in deviation report for traceability
- Add to final.md — once added, this is resolved
- These do NOT block completion (sync already updated the spec)

### Deviation
Both exist but differ (different signature, different behavior).
- Record exact difference
- Determine which is correct (usually the code, since it passed tests)
- Update final.md to match code if code is correct
- Flag for user review if unclear

## Step 4: Preserve and Update final.md

1. **Preserve the original**: Copy `final.md` → `final.pre-sync.md`
2. **Update final.md** to reflect actual implementation:
   - Fix incorrect file paths
   - Update function signatures to match code
   - Add missing types/functions that were created during implementation
   - Mark removed items as deprecated or remove them
3. **Do not change the spec's intent or requirements** — only update implementation
   details to match reality.

## Step 5: Generate Deviation Report

Create `.bts/state/recipes/{id}/deviation.md`:

```markdown
# Deviation Report: {topic}

Generated: {ISO8601}
Recipe: {id}

## Summary
- Matches: N
- Not Implemented: N
- Spec Additions Needed: N
- Deviations: N

## Not Implemented
| Item | File | Reason |
|------|------|--------|
| ... | ... | ... |

## Spec Additions
| Item | File | Description |
|------|------|-------------|
| ... | ... | ... |

## Deviations
| Item | Spec Says | Code Has | Resolution |
|------|-----------|----------|------------|
| ... | ... | ... | ... |
```

## Step 6: Log and Validate

1. Log sync action (includes manifest registration and pre-sync backup):
   ```bash
   bts recipe log {id} --action sync --output deviation.md --based-on final.md --result "N matches, N deviations"
   ```
   Also manually register `final.pre-sync.md` in manifest.json as type `"draft"`.

2. Validate schema:
   ```bash
   bts validate
   ```

3. If final.md was modified (any Spec Addition or Deviation resolved by updating spec),
   run /verify on the updated final.md to ensure no contradictions were introduced.
   This satisfies the "every modification → /verify" rule.

Output `<bts>SYNC DONE</bts>` when complete.
