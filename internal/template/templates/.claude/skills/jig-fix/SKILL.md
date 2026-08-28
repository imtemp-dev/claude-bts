---
name: jig-fix
description: >
  Diagnose and fix a bug through document-first approach. Creates fix-spec.md
  with root cause analysis, adversarial validation, simulation, expert review,
  and verified implementation. Lighter than spec — no scoping, no task
  decomposition, 1-round debate.
user-invocable: true
allowed-tools: Read Write Edit Grep Glob Bash Agent mcp__context7__resolve-library-id mcp__context7__get-library-docs
argument-hint: "\"bug description\""
---

# Recipe: Fix

Fix a bug through document-first diagnosis and verified implementation: $ARGUMENTS

## Context Briefing

Before starting, build situational awareness:
1. List `.jig/specs/recipes/` → find related recipes (especially completed ones)
2. Set `ref_recipe` in recipe.json to the most relevant recipe ID
3. For each related recipe, read its final.md → understand design intent
4. Check deviation.md → known issues that may be relevant
5. Check review.md from related recipes → known quality/security issues
6. Scan codebase for files mentioned in the bug report

## Resume Check

```bash
jig recipe status
```
If active fix recipe found, read diagnosis.md and fix-spec.md to resume.

**Autonomous execution**: This recipe runs without stopping between steps.
Do NOT pause to summarize or ask the user. Only stop for [CONVERGENCE FAILED]
or when experts disagree on root cause (user decision needed).

If no active recipe, create one:
```bash
jig recipe create --type fix --topic "$ARGUMENTS"
```
Use the output as recipe ID for all subsequent commands.

## Step 1: Diagnose

Investigate the bug and create `.jig/specs/recipes/{id}/diagnosis.md`:

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
jig recipe log {id} --phase research --action research --output diagnosis.md --result "root cause: [summary]"
```

## Step 2: Adversarial Diagnosis

A wrong diagnosis wastes the entire downstream fix flow. Before committing to
a fix spec, an independent agent challenges the root cause by reading the
actual code and tracing execution paths.

Configure the validator model via `agents.diagnosis_validator` (default:
session model) and the rebuttal model via `agents.diagnosis_rebuttal`
(default: session model) in settings.yaml. Both default to session model
because code path tracing requires deeper reasoning than pattern-based checks.

**Fallback**: If a validator or rebuttal agent fails (error, timeout), tag
the diagnosis as `[UNVALIDATED]` in diagnosis.md and proceed to Step 3.

### Round 1 — Defense (Validator)

Spawn **Agent(diagnosis-validator)** with a structured prompt:

```
Challenge the following root cause diagnosis by reading the actual code and
tracing execution paths. Is the identified cause actually responsible for
this symptom?

## Mode
Fix

## Symptom
{from diagnosis.md Symptom section}

## Reproduction
{from diagnosis.md Reproduction section}

## Identified Root Cause
{from diagnosis.md Root Cause section — include file:line}

## Affected Files
{from diagnosis.md Affected Files section}

## Files in scope
{all file paths the validator should read — affected files plus callers if needed}
```

The validator reads the actual code and returns:
- **CONFIRM**: Execution path matches the diagnosed cause.
- **CHALLENGE**: Alternative root cause with file:line evidence and confidence.

### Round 2 — Rebuttal (only if CHALLENGED)

If the validator returned CONFIRM, skip to Verdict.

Spawn **Agent(diagnosis-rebuttal)** with a structured prompt:

```
The original diagnosis was challenged by a validator with an alternative
hypothesis. Defend the original with a concrete execution trace, or concede
if the alternative is more consistent with the evidence.

## Mode
Fix

## Symptom and Reproduction
{from diagnosis.md}

## Original Diagnosis
{Root Cause section from diagnosis.md}

## Validator's Challenge
{CHALLENGE reasoning, alternative hypothesis, confidence level}

## Files to read
{files from both original diagnosis and validator's alternative}
```

The rebuttal agent returns:
- **INSIST**: Concrete execution trace proving the original diagnosis is correct.
- **CONCEDE**: Validator's alternative is more consistent with the evidence.

### Verdict (orchestrator — no agent)

| Orchestrator | Validator | Rebuttal | Result |
|--------------|-----------|----------|--------|
| Diagnosed    | CONFIRM   | —        | **CONFIRMED**: proceed to Step 3 |
| Diagnosed    | CHALLENGE | CONCEDE  | **RECONSIDERED**: update diagnosis and proceed |
| Diagnosed    | CHALLENGE | INSIST   | **DISPUTED**: proceed with flag |

**If CONFIRMED**: Proceed directly to Step 3.

**If RECONSIDERED**: The orchestrator must re-read the code using the
validator's alternative hypothesis, update `diagnosis.md` with the corrected
root cause, and re-run Round 1 + Round 2 once on the updated diagnosis.
- **Max 1 reconsideration attempt.** On the retry:
  - Retry returns CONFIRMED → proceed to Step 3 with the updated diagnosis
  - Retry returns DISPUTED → proceed to Step 3 with the WARNING flag (normal DISPUTED handling)
  - Retry would trigger another RECONSIDERED → stop and ask the user to choose between the original, first alternative, and second alternative

**If DISPUTED**: Proceed to Step 3 with the original diagnosis, but add a
`> [!WARNING] Diagnosis DISPUTED` admonition at the top of diagnosis.md
documenting both sides' arguments. The subsequent Expert Review (Step 5)
should re-examine the dispute.

```bash
jig recipe log {id} --action research --result "diagnosis: CONFIRMED | RECONSIDERED | DISPUTED | UNVALIDATED"
```

## Step 3: Fix Spec (document first)

Based on the validated diagnosis, create `.jig/specs/recipes/{id}/fix-spec.md`:

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
jig recipe log {id} --phase draft --action draft --output fix-spec.md --result "fix for [root cause]"
```

## Step 4: Simulate

Use Skill("jig-simulate") on fix-spec.md:
- Focus scenarios on: does this fix break other functionality?
- Reference the original recipe's final.md for impact analysis
- 3 scenarios are enough: fix verification, regression, side effect check

When simulation passes, continue immediately to Step 5.

```bash
jig recipe log {id} --phase simulate --action simulate
```

## Settings

Read `.jig/config/settings.yaml` for project-specific limits.

## Step 5: Expert Review (`fix.debate_rounds`, default: 1 round)

Run a focused 1-round review on fix-spec.md:

Choose 3 experts relevant to the bug domain. Each expert states:
- Is the root cause correctly identified?
- Is the fix complete and correct?
- Are there risks or side effects?

1 round only — positions + consensus. No rebuttals needed for a focused fix.

If experts disagree on root cause → ask user for decision.

If Step 2 returned DISPUTED, the Expert Review must specifically re-examine
the validator's alternative hypothesis before reaching consensus.

```bash
jig recipe log {id} --phase debate --action debate --result "[consensus summary]"
```

## Step 6: Verify Loop

Run /verify on fix-spec.md:
- Is the fix logically sound?
- Does it contradict the original spec (final.md)?
- Are edge cases covered?
- Could the fix introduce new issues?

If issues found → update fix-spec.md → re-verify. Do NOT stop to report — fix and continue.
When critical=0, major=0 → continue immediately to Step 7.
`verify.max_iterations` (default: 3) consecutive rounds without progress →
`jig recipe log` exits non-zero with `[CONVERGENCE FAILED]` and names the
stagnant finding IDs → stop and ask the user. Enforced in code, not by
self-counting (`jig-verification-protocol.md § Convergence`).

```bash
jig recipe log {id} --phase verify --action verify --result "critical=N, major=N"
```

## Step 7: Implement

Read `.jig/specs/project-map.md` for layer-specific build/test commands.
When fix spans multiple layers, use each layer's build command for verification.

Apply the fix exactly as described in fix-spec.md:
- Read each change item
- Modify the code accordingly
- Run the appropriate build command to verify compilation

No task decomposition — fix is typically 1-3 files.

```bash
jig recipe log {id} --phase implement --action implement --result "N files modified"
```

## Step 8: Test

Add the regression test from fix-spec.md's "Regression Test" section,
then run the suite THROUGH jig so status is machine-truthful:

```bash
jig test run {id} --cmd "<project test command>"
```

`jig test run` writes test-results.json with `status` derived from the
actual exit code (`recorded_by: "jig"`) — the FIX DONE gate trusts
this. Do NOT run tests via plain Bash and hand-write test-results.json.
- Exits 0 → Step 9 (Simulate)
- Fails → re-examine fix-spec.md (back to Step 6), then re-run

```bash
jig recipe log {id} --phase test --action test --output test-results.json --result "N/N passed"
```

## Step 9: Simulate (code)

Use Skill("jig-simulate") with arguments: "code" to verify the fix covers all paths.

Focus on: does the fix handle all edge cases from fix-spec.md?
Are there code paths where the original bug could still occur?

If gaps found → fix code → re-test.
If tests fail after simulate fixes → fix tests → re-test.
When simulation passes, continue immediately to Step 10.

```bash
jig recipe log {id} --action simulate --result "N scenarios, N gaps"
```

## Step 10: Review

Update phase:
```bash
jig recipe log {id} --phase review
```

Use Skill("jig-review") with the files from fix-spec.md's "Changes" section as arguments.

After review.md is generated, fix [ACTIONABLE] critical and major items.
Re-test if code was modified.

```bash
jig recipe log {id} --action review --output review.md --result "N critical, N major"
```

## Step 11: Complete

When tests pass and review is done:

1. Run Skill("jig-status") with arguments: {id}
   This updates project-status.md and project-map with the fix changes.
2. Verify fix-spec.md exists
3. Verify test-results.json shows pass
4. Output `<jig>FIX DONE</jig>`
5. Tell the user (plaintext, after the marker):
   > **Fix complete** — `{id}` done.
   > Next: run `/jig-spec` to continue roadmap, or `/jig-fix` for another bug.

**Note:** Fix recipes implement code directly in Step 7, not via /jig-implement.
fix-spec.md is the authoritative document (not final.md). The original recipe's
final.md is preserved unmodified — deviations from spec are tracked in the
original recipe's deviation.md during future /jig-sync runs.

Project history:

```
r-1002/final.md        ← original design
r-fix-1003/fix-spec.md ← bug fix (references r-1002)
r-fix-1004/fix-spec.md ← another fix (references r-1002)
```
