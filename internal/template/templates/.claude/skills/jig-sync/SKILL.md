---
name: jig-sync
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

0. **Resolve recipe ID**: If `$ARGUMENTS` is empty, run `jig recipe status`
   to find the active recipe. Use its ID for all `{id}` references below.

1. Verify test-results.json exists:
   ```bash
   ls .jig/specs/recipes/{id}/test-results.json
   ```
   If not found → "Run /test first. Sync requires passing tests."

2. Check test status: should be `"pass"`.
   If `"fail"` → "Tests are failing. Fix tests before syncing."

## Step 1: Extract Spec Definitions (grouped by file)

Read `.jig/specs/recipes/{id}/final.md` and group all definitions by file.
Also read `tasks.json` to know which files were created vs modified —
this helps focus the comparison on files that actually changed.

```
{
  "src/config.py": {
    functions: [name, params, return_type, error_handling],
    types: [name, fields],
    exports: [public API items]
  },
  "src/agent.py": { ... },
  ...
}
```

This creates one group per file, containing ALL items for that file.

## Step 2: File-by-File Comparison

For EACH file group (not each individual item):

1. Read the spec's definitions for this file (all functions, types, exports)
2. Read the actual code file
3. Compare ALL items in ONE pass:
   - List every function/type in spec → check it exists in code with correct signature
   - List every function/type in code → check if spec mentions it
   - This is a full comparison — nothing is skipped

This reduces comparison rounds from N-items to N-files while checking
every single item within each file.

Also scan for code files NOT mentioned in the spec that appear related
(same directory, similar naming).

## Step 2.5: Ingest Simulation DEVIATIONs (Phase 12)

Before classifying Step 2's code-vs-spec results, fold in the DEVIATION
entries that /jig-simulate already recorded during code mode. They are
already validated (adversarially), so rediscovering them here is waste.

```bash
jig sim-deviations --recipe {id} --json
```

For each entry `{id, driver="simulate", severity, file, detail}`:

- If Step 2 found a matching code-vs-spec difference → APPEND
  `simulate:{id}` to that row's Driver column (comma-separated).
- If Step 2 missed it → ADD a new row to the Deviations table with:
    - ID: next D-NNN
    - Item: short description derived from `detail`
    - Spec Says / Code Has: extracted from `detail` if possible, else
      leave the spec side from final.md and code side as "(see simulation)"
    - Driver: `simulate:{id}`
    - Severity: `severity` from the simulation (default major)
    - Resolution: `pending` unless the code already reflects the
      finding, in which case "spec updated" and update final.md.

`jig validate` enforces that every sim DEVIATION id ends up with a
matching `simulate:{id}` token in deviation.md. Missing consumption
surfaces as `sim_deviation_unconsumed` (major).

## Step 3: Classify Results

### Cosmetic vs Functional

**Cosmetic** differences do not affect correctness or behavior — formatting,
import ordering, naming conventions (e.g., snake_case vs camelCase across
languages), lint directives, whitespace. Record these briefly but do not
analyze them in detail.

**Functional** differences affect behavior — missing functions, different
signatures, different return types, different error handling. These require
full analysis.

For each item, classify as one of:

### Match
Spec and code agree. No action needed.

### Not Implemented (recorded for follow-up)
Spec defines it but code doesn't have it.
- Record in deviation report
- Does NOT block recipe completion — becomes follow-up work

### Spec Addition Needed (non-blocking)
Code has it but spec doesn't mention it.
- This happens when implementation required additional helpers, utilities, or types
- Record in deviation report for traceability
- Add to final.md — once added, this is resolved
- These do NOT block completion (sync already updated the spec)

### Deviation (recorded for follow-up)
Both exist but differ (different signature, different behavior).
- Record exact difference
- Determine which is correct (usually the code, since it passed tests)
- Update final.md to match code if code is correct
- Flag for user review if unclear
- Does NOT block recipe completion — becomes follow-up work

## Step 4: Preserve and Update final.md

1. **Preserve the original** (crash-safe):
   - If `final.pre-sync.md` already exists → do NOT overwrite it.
     The existing backup is the true original from the spec phase.
     A previous sync may have crashed mid-update, leaving final.md corrupted.
     In this case, RESTORE: copy `final.pre-sync.md` back to `final.md` first,
     then proceed with a clean sync.
   - If `final.pre-sync.md` does NOT exist → copy `final.md` → `final.pre-sync.md`
2. **Update final.md** to reflect actual implementation:
   - Fix incorrect file paths
   - Update function signatures to match code
   - Add missing types/functions that were created during implementation
   - Mark removed items as deprecated or remove them
3. **Do not change the spec's intent or requirements** — only update implementation
   details to match reality.

## Step 5: Generate Deviation Report

Create `.jig/specs/recipes/{id}/deviation.md`:

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
| ID | Item | File | Driver | Severity | Reason |
|----|------|------|--------|----------|--------|
| D-001 | ... | ... | code-diff | major | ... |

## Spec Additions
| ID | Item | File | Driver | Severity | Description |
|----|------|------|--------|----------|-------------|
| D-002 | ... | ... | code-diff | minor | ... |

## Deviations
| ID | Item | Spec Says | Code Has | Driver | Severity | Resolution |
|----|------|-----------|----------|--------|----------|------------|
| D-003 | ... | ... | ... | code-diff,review:MAJ-005 | major | ... |
```

**ID rule (Phase 16)**: every row MUST carry a unique D-NNN identifier.
IDs are monotonic within the recipe and never reused even across
sections.

**Driver vocabulary (Phase 16)**: exactly these tokens, comma-separated
when multiple apply:
  - `code-diff`     — row surfaced by this sync step's file-by-file compare
  - `sync-check`    — row surfaced by /jig-sync-check
  - `simulate:<id>` — row forwarded from a simulations/*.md DEVIATION entry
  - `review:<id>`   — row forwarded from a review.md finding (CRT-001 / MAJ-005 / ...)
  - `test:<name>`   — row surfaced by a failing or category="spec" test
  - `midrun:<win>`  — row surfaced by a mid-run review (reviews/midrun-*.md)

**Severity**: critical / major / minor / info. `jig validate` enforces
the vocabulary; missing or invalid Driver / Severity / ID blocks
`<jig>IMPLEMENT DONE</jig>`.

## Step 6: Log and Validate

1. Log sync action (includes manifest registration and pre-sync backup):
   ```bash
   jig recipe log {id} --action sync --output deviation.md --based-on final.md --result "N matches, N deviations"
   ```
   Also manually register `final.pre-sync.md` in manifest.json as type `"draft"`.

2. Validate schema:
   ```bash
   jig validate
   ```

3. If final.md was modified (any Spec Addition or Deviation resolved by updating spec),
   run /verify on the updated final.md to ensure no contradictions were introduced.
   This satisfies the "every modification → /verify" rule.

Output `<jig>SYNC DONE</jig>` when complete.
