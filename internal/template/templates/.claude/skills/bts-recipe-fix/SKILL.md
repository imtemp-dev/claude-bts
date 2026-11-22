---
name: bts-recipe-fix
description: >
  Diagnose and fix a bug through document-first approach. Creates fix-spec.md
  with root cause analysis, adversarial validation, a reproduction run, expert review,
  and verified implementation. Lighter than blueprint — no scoping, no task
  decomposition, 1-round debate.
user-invocable: true
allowed-tools: Read Write Edit Grep Glob Bash Agent mcp__context7__resolve-library-id mcp__context7__get-library-docs
argument-hint: "\"bug description\""
---

# Recipe: Fix

Fix a bug through document-first diagnosis and verified implementation: $ARGUMENTS

## Context Briefing

Before starting, build situational awareness:
1. List `.bts/specs/recipes/` → find related recipes (especially completed ones)
2. Name the most relevant recipe when creating this one (`--ref`, or
   `--amends` for a completed recipe this fix changes; see below)
3. For each related recipe, read its final.md → understand design intent
4. Check deviation.md → known issues that may be relevant
5. Check review.md from related recipes → known quality/security issues
6. Scan codebase for files mentioned in the bug report

## Resume Check

```bash
bts recipe status
```
If active fix recipe found, read diagnosis.md and fix-spec.md to resume.

**Autonomous execution**: This recipe runs without stopping between steps.
Do NOT pause to summarize or ask the user. Only stop for [CONVERGENCE FAILED],
when experts disagree on root cause (user decision needed), or for the
user's confirmation of fix-spec.md (Step 6.5).

If no active recipe, create one, naming the most relevant recipe with
`--ref` (never by editing recipe.json):
```bash
bts recipe create --type fix --topic "$ARGUMENTS" --ref <related recipe>
```
When the fix changes the code of a completed recipe (a review after
completion, a later bug in it), create it with `--amends <that recipe>`
instead: the completed recipe's documents stay sealed, never edited; this
recipe's fix-spec states which of its invariants the change retires,
narrows or restates; and once this recipe completes, its records answer
for the files it touched, so `bts doctor` stops reporting them as drift.
Use the output as recipe ID for all subsequent commands.

## Step 1: Diagnose

Investigate the bug and create `.bts/specs/recipes/{id}/diagnosis.md`:

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
bts recipe log {id} --phase research --action research --output diagnosis.md --result "root cause: [summary]"
```

## Step 2: Adversarial Diagnosis

A wrong diagnosis wastes the entire downstream fix flow. Before committing to
a fix spec, an independent agent challenges the root cause by reading the
actual code and tracing execution paths.

The validator and the rebuttal run on the session model, because code path
tracing requires deeper reasoning than pattern-based checks. When
settings.yaml sets `agents.diagnosis_validator` or `agents.diagnosis_rebuttal`,
pass that value as the spawn's `model`.

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
bts recipe log {id} --action research --result "diagnosis: CONFIRMED | RECONSIDERED | DISPUTED | UNVALIDATED"
```

## Step 3: Fix Spec (document first)

Based on the validated diagnosis, create `.bts/specs/recipes/{id}/fix-spec.md`:

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
bts recipe log {id} --phase draft --action draft --output fix-spec.md --result "fix for [root cause]"
```

## Step 4: Reproduce, and prove what the fix rests on

Settle the fix spec by running, not by walking scenarios (pstack's `tdd`
and `blast-radius`):

1. **Reproduce.** Turn the Reproduction section into a test that fails on
   the current code for the bug's own reason.  Run it and keep the output.
   A bug you cannot reproduce goes back to Step 1.
2. **The fact the fix rests on.** Name the one or two facts the fix's
   safety depends on (a caller that relies on the old behaviour, a library
   call that behaves as assumed) and prove each by a run: a small script
   that calls the real code and fails loudly if the fact is wrong.  Say in
   fix-spec.md how far each got: said, pointed at the line, walked, ran.

Continue to Step 5 with the failing reproduction test in hand.

```bash
bts recipe log {id} --action test --result "reproduction fails as expected: <test id>"
```

## Settings

Read `.bts/config/settings.yaml` for project-specific limits.

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
bts recipe log {id} --phase debate --action debate --result "[consensus summary]"
```

## Step 6: Verify Loop

Run /verify on fix-spec.md:
- Is the fix logically sound?
- Does it contradict the original spec (final.md)?
- Are edge cases covered?
- Could the fix introduce new issues?

If issues found → update fix-spec.md → re-verify. Do NOT stop to report — fix and continue.
When critical=0, major=0 → continue immediately to Step 6.5.
`verify.max_iterations` (default: 3) consecutive rounds without progress →
`bts recipe log` exits non-zero with `[CONVERGENCE FAILED]` and names the
stagnant finding IDs → stop and ask the user. Enforced in code, not by
self-counting (`bts-verification-protocol.md § Convergence`).

```bash
bts recipe log {id} --doc .bts/specs/recipes/{id}/fix-spec.md \
  --dimension verify --scope full \
  --critical N --major N --minor-resolvable N
```

Neither `--action` nor `--phase` belongs on that call. They select the
changelog and phase-change modes, where the counts are accepted and
dropped — which is what this step used to do, so the convergence budget
promised just above never ran for a fix recipe. `bts recipe log` now
refuses the combination rather than exiting 0 on a round it discarded.

## Step 6.5: Confirm the fix spec

The `--phase implement` log in Step 7 is refused until the user has
confirmed fix-spec.md (a resolved `spec-confirm` decision).  Put it in
front of them now:

- When `review.surface` in `.bts/config/settings.yaml` is `artifact` (the
  default) and the Artifact tool is available, or `local`: `/bts-review-page {id}
  spec` draws the failing path today and after the fix, the changes and
  the regression test, and runs the rounds; its Confirm step records the
  confirmation.  A change the user accepts edits fix-spec.md and is
  verified again, as that skill says, before the new version goes to the
  page.
- Otherwise ask with AskUserQuestion: the root cause, each change and the
  regression test in a line each, with "Confirm fix-spec.md as written"
  and "Change something" as options.  On a confirmation, `bts recipe
  confirm {id} spec --answer "<their words>"`; on a change, edit
  fix-spec.md, verify it again (Step 6) and ask again.
- With no user to ask, `bts recipe decision hold {id} --key spec-confirm
  --question "Confirm fix-spec.md as written?"` and stop.

## Step 7: Implement

When `.bts/config/settings.yaml` sets `implement.require_edit_go: true`,
the `--phase implement` log below is refused until the user's go-ahead
is recorded, so record it before the first edit: ask the user, then
`bts recipe decision hold {id} --key code-edit-go --question "Start
writing product code for {id}?"` and, with their answer, `bts recipe
decision resolve {id} code-edit-go --answer "<their words>"`.

Read `.bts/specs/project-map.md` for layer-specific build/test commands.
When fix spans multiple layers, use each layer's build command for verification.

Apply the fix exactly as described in fix-spec.md:
- Read each change item
- Modify the code accordingly
- Run the appropriate build command to verify compilation

No task decomposition — fix is typically 1-3 files.

```bash
bts recipe log {id} --phase implement --action implement --result "N files modified"
```

## Step 8: Test

Add the regression test from fix-spec.md's "Regression Test" section,
then run the suite THROUGH bts so status is machine-truthful:

```bash
bts test run {id} --cmd "<project test command>"
```

`bts test run` writes test-results.json with `status` derived from the
actual exit code (`recorded_by: "bts"`) — the FIX DONE gate trusts
this. Do NOT run tests via plain Bash and hand-write test-results.json.
- Exits 0 → Step 9 (Prove)
- Fails → re-examine fix-spec.md (back to Step 6), then re-run

```bash
bts recipe log {id} --phase test --action test --output test-results.json --result "N/N passed"
```

## Step 9: Prove

The reproduction test from Step 4 now passes.  Then run the prove stage on
the fix, as /bts-implement does:

- Use Skill("bts-interrogate") with arguments: {id}: adversaries try to
  break the fix, and only what a rerun proves survives; fix what
  reproduces, with a regression test.
- Use Skill("bts-prove") with arguments: {id}: drive the user journey the
  bug was reported on through the real app, or record why it cannot run
  here and who runs it.

Continue to Step 10 when no interrogate finding is open and the journey is
driven or deferred.

## Step 10: Review

Update phase:
```bash
bts recipe log {id} --phase review
```

Use Skill("bts-review") with the files from fix-spec.md's "Changes" section as arguments.

After review.md is generated, fix [ACTIONABLE] critical and major items.
Re-test if code was modified.

```bash
bts recipe log {id} --action review --output review.md --result "N critical, N major"
```

## Step 11: Complete

When tests pass and review is done:

1. Run Skill("bts-status") with arguments: {id}
   This updates project-status.md and project-map with the fix changes.
2. Verify fix-spec.md exists
3. Verify test-results.json shows pass
4. Output `<bts>FIX DONE</bts>`
5. Tell the user (plaintext, after the marker):
   > **Fix complete** — `{id}` done.
   > Next: run `/bts-recipe-blueprint` to continue roadmap, or `/bts-recipe-fix` for another bug.

**Note:** Fix recipes implement code directly in Step 7, not via /bts-implement.
fix-spec.md is the authoritative document (not final.md). The original recipe's
final.md is preserved unmodified — deviations from spec are tracked in the
original recipe's deviation.md during future /bts-sync runs.

Project history:

```
r-1002/final.md        ← original design
r-fix-1003/fix-spec.md ← bug fix (references r-1002)
r-fix-1004/fix-spec.md ← another fix (references r-1002)
```
