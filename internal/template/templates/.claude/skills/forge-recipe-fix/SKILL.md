---
name: forge-recipe-fix
description: >
  Diagnose and fix a bug through document-first approach. Creates fix-spec.md
  with root cause analysis, simulation, expert review, and verified implementation.
  Lighter than blueprint — no scoping, no task decomposition, 1-round debate.
user-invocable: true
allowed-tools: Read Write Edit Grep Glob Bash Agent mcp__context7__resolve-library-id mcp__context7__get-library-docs
argument-hint: "\"bug description\""
---

# Recipe: Fix

Fix a bug through document-first diagnosis and verified implementation: $ARGUMENTS

## Context Briefing

Before starting, build situational awareness:
1. List `.forge/state/recipes/` → find related recipes (especially completed ones)
2. Set `ref_recipe` in recipe.json to the most relevant recipe ID
3. For each related recipe, read its final.md → understand design intent
4. Check deviation.md → known issues that may be relevant
5. Check review.md from related recipes → known quality/security issues
6. Scan codebase for files mentioned in the bug report

## Resume Check

```bash
forge recipe status
```
If active fix recipe found, read diagnosis.md and fix-spec.md to resume.

If no active recipe, create one:
```bash
forge recipe create --type fix --topic "$ARGUMENTS"
```
Use the output as recipe ID for all subsequent commands.

## Step 1: Diagnose

Investigate the bug and create `.forge/state/recipes/{id}/diagnosis.md`:

```markdown
# Diagnosis: {bug description}

## Symptom
[What the user observed — exact error, unexpected behavior]

## Reproduction
[Steps to reproduce the bug]

## Root Cause
[Code location, logic error, why it happens]
[Include file path, function name, line context]

## Affected Files
- [file 1]: [how it's involved]
- [file 2]: [how it's involved]

## Impact
[What else could be affected by this bug]
[Could other features break?]

## Related Recipe
[r-XXXX: topic — reference to the recipe that built this code]
[final.md says X, but code does Y]
```

```bash
forge recipe log {id} --phase research --action research --output diagnosis.md --result "root cause: [summary]"
```

## Step 2: Fix Spec (document first)

Based on diagnosis, create `.forge/state/recipes/{id}/fix-spec.md`:

```markdown
# Fix Spec: {bug description}

Recipe: {id}
Ref: r-XXXX (original recipe)

## Current Behavior
[What the code does now (wrong)]

## Expected Behavior
[What the code should do (correct)]

## Changes
For each file to modify:

### [file path]
- **Function**: [name]
- **Current**: [current logic/code]
- **Fixed**: [corrected logic/code]
- **Rationale**: [why this fixes the bug]

## Edge Cases
- [edge case 1: how the fix handles it]
- [edge case 2: how the fix handles it]

## Regression Test
- [test scenario that would catch this bug if it recurs]
- [expected input → expected output]
```

```bash
forge recipe log {id} --phase draft --action draft --output fix-spec.md --result "fix for [root cause]"
```

## Step 3: Simulate

Use Skill("forge-simulate") on fix-spec.md:
- Focus scenarios on: does this fix break other functionality?
- Reference the original recipe's final.md for impact analysis
- 3 scenarios are enough: fix verification, regression, side effect check

```bash
forge recipe log {id} --phase simulate --action simulate
```

## Settings

Read `.forge/config/settings.yaml` for project-specific limits.

## Step 4: Expert Review (`fix.debate_rounds`, default: 1 round)

Run a focused 1-round review on fix-spec.md:

Choose 3 experts relevant to the bug domain. Each expert states:
- Is the root cause correctly identified?
- Is the fix complete and correct?
- Are there risks or side effects?

1 round only — positions + consensus. No rebuttals needed for a focused fix.

If experts disagree on root cause → ask user for decision.

```bash
forge recipe log {id} --phase debate --action debate --result "[consensus summary]"
```

## Step 5: Verify Loop

Run /verify on fix-spec.md:
- Is the fix logically sound?
- Does it contradict the original spec (final.md)?
- Are edge cases covered?
- Could the fix introduce new issues?

If issues found → update fix-spec.md → re-verify.
Max `verify.max_iterations` (default: 3) → [CONVERGENCE FAILED] → ask user.

```bash
forge recipe log {id} --phase verify --action verify --result "critical=N, major=N"
```

## Step 6: Implement

Apply the fix exactly as described in fix-spec.md:
- Read each change item
- Modify the code accordingly
- Run build to verify compilation

No task decomposition — fix is typically 1-3 files.

```bash
forge recipe log {id} --phase implement --action implement --result "N files modified"
```

## Step 7: Test

Run existing test suite + add regression test from fix-spec.md's
"Regression Test" section:
- If all pass → Step 7.3 (Simulate)
- If fail → re-examine fix-spec.md (back to Step 5)

```bash
forge recipe log {id} --phase test --action test --output test-results.json --result "N/N passed"
```

## Step 7.3: Simulate

Run /forge-simulate code to verify the fix covers all paths:
```bash
/forge-simulate code
```

Focus on: does the fix handle all edge cases from fix-spec.md?
Are there code paths where the original bug could still occur?

If gaps found → fix code → re-test.
If tests fail after simulate fixes → fix tests → re-test.

```bash
forge recipe log {id} --action simulate --result "N scenarios, N gaps"
```

## Step 7.5: Review

Update phase:
```bash
forge recipe log {id} --phase review
```

Run /forge-review with the files from fix-spec.md's "Changes" section:
```bash
# Extract file paths from fix-spec.md Changes section and pass as arguments
/forge-review [file1] [file2] ...
```

After review.md is generated, fix [ACTIONABLE] critical and major items.
Re-test if code was modified.

```bash
forge recipe log {id} --action review --output review.md --result "N critical, N major"
```

## Step 8: Complete

When tests pass and review is done:

1. Run Skill("forge-status") with arguments: {id}
   This updates project-status.md and project-map with the fix changes.
2. Verify fix-spec.md exists
3. Verify test-results.json shows pass
4. Output `<forge>FIX DONE</forge>`

**Note:** Fix recipes implement code directly in Step 6, not via /forge-implement.
fix-spec.md is the authoritative document (not final.md). The original recipe's
final.md is preserved unmodified — deviations from spec are tracked in the
original recipe's deviation.md during future /forge-sync runs.

Project history:

```
r-1002/final.md        ← original design
r-fix-1003/fix-spec.md ← bug fix (references r-1002)
r-fix-1004/fix-spec.md ← another fix (references r-1002)
```
