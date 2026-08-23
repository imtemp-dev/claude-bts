---
name: bts-recipe-debug
description: >
  Debug unknown bugs through multi-perspective analysis. Collects 6 "blueprints"
  (data flow, dependencies, design intent, runtime, change history, impact),
  cross-references them to find root cause, runs adversarial validation on
  the top hypothesis, then produces a verified fix spec implementable via
  /bts-implement.
user-invocable: true
allowed-tools: Read Write Edit Grep Glob Bash Agent AskUserQuestion WebSearch WebFetch mcp__context7__resolve-library-id mcp__context7__get-library-docs
argument-hint: "\"symptom description\""
---

# Recipe: Debug

Debug through multi-perspective analysis: $ARGUMENTS

## Context Briefing

Before starting:
1. List `.bts/specs/recipes/` → find related recipes (especially the one
   that built the affected code)
2. Set `ref_recipe` in recipe.json to the most relevant recipe ID
3. Read related recipe's final.md → original design intent
4. Check deviation.md → known spec-code differences
5. Check review.md from related recipes → quality issues that may be related
6. Scan codebase for files likely related to the symptom
7. If `.bts/specs/roadmap.md` exists, read it for project context
   (what's been built, what's planned — helps understand system boundaries)

## Resume Check

```bash
bts recipe status
```
If active debug recipe found, read perspectives.md and draft.md to resume.

**Autonomous execution**: This recipe runs without stopping between steps.
Do NOT pause to summarize or ask the user. Only stop for [CONVERGENCE FAILED]
or when experts disagree on root cause (user decision needed).

If no active recipe, create one:
```bash
bts recipe create --type debug --topic "$ARGUMENTS"
```
Use the output as recipe ID for all subsequent commands.

## Step 1: Collect Perspectives

Read `.bts/config/settings.yaml` for project-specific limits.

Investigate the symptom from `debug.perspective_count` (default: 6) angles. Create
`.bts/specs/recipes/{id}/perspectives.md`:

### 1.1 Data Flow Map
Trace the complete path of the failing operation:
- Request/input → processing steps → output/response
- At each step: what data enters, what transforms, what exits
- Mark where the flow breaks or produces wrong results

### 1.2 Dependency Graph
Map all modules/functions involved in the failing path:
- Which module calls which
- External dependencies (libraries, APIs, DB)
- Configuration dependencies (env vars, config files)
- Identify any recently changed dependencies

### 1.3 Design Intent
Read `.bts/specs/project-map.md` for the layer overview.
Load the affected layer's detail from `.bts/specs/layers/{name}.md`.
Then read the specific recipe's final.md (via ref_recipe) for
detailed design of the affected feature.
- project-map.md: what layers exist, how they connect
- layers/{name}.md: specific layer's structure, models, APIs
- final.md: specific feature's intended behavior
- Where does actual behavior diverge from the design?

### 1.4 Runtime Context
Check the execution environment:
- Environment variables and configuration values
- Database state (schema, data that might cause issues)
- External service availability and connectivity
- Version mismatches (installed vs expected)

### 1.5 Change History
```bash
git log --oneline -20
git log --all --oneline -- [affected files]
```
- What changed recently in the affected area?
- When did the symptom first appear?
- Correlate timing: symptom start ↔ code changes

### 1.6 Impact Map
- What other features share code with the affected area?
- If we fix this, what else could be affected?
- Upstream and downstream dependencies of the failing module

```bash
bts recipe log {id} --phase research --action research --output perspectives.md --result "6 perspectives collected"
```

## Step 2: Cross-Reference

Add a "Cross-Reference Analysis" section to perspectives.md:

For each pair of perspectives, check for inconsistencies:
- Design says X, but code does Y
- Config expects format A, but env provides format B
- Code changed at time T, symptom started at time T
- Dependency version X expects API Y, but we call Z

Produce ranked hypotheses:

```markdown
## Hypotheses (ranked by evidence strength)

1. [HIGH] {hypothesis} — supported by perspectives {list}
   Evidence: {specific cross-reference findings}

2. [MEDIUM] {hypothesis} — supported by perspectives {list}
   Evidence: {specific cross-reference findings}

3. [LOW] {hypothesis} — single perspective only
```

```bash
bts recipe log {id} --phase research --action research --output perspectives.md --result "cross-reference: N hypotheses ranked"
```

## Step 3: Adversarial Hypothesis

The ranked hypothesis #1 was chosen by the same orchestrator that collected
the perspectives — confirmation bias risk. Before committing to a draft
based on #1, an independent agent challenges the ranking by reading the
actual code.

Configure the validator model via `agents.diagnosis_validator` (default:
session model) and the rebuttal model via `agents.diagnosis_rebuttal`
(default: session model) in settings.yaml. Both default to session model
because code path tracing requires deeper reasoning than pattern-based checks.

**Fallback**: If a validator or rebuttal agent fails (error, timeout), tag
the top hypothesis as `[UNVALIDATED]` in perspectives.md and proceed to Step 4.

### Round 1 — Defense (Validator)

Spawn **Agent(diagnosis-validator)** with a structured prompt:

```
Challenge the following top-ranked hypothesis by reading the actual code and
tracing execution paths. Is the #1 hypothesis actually the best explanation
for the symptom, or does another hypothesis from the ranking fit better?

## Mode
Debug

## Symptom
{symptom description}

## Reproduction
{reproduction steps from perspectives.md}

## Ranked Hypotheses
1. [HIGH] {hypothesis #1} — Evidence: {perspective findings}
2. [MEDIUM] {hypothesis #2} — Evidence: {perspective findings}
3. [LOW] {hypothesis #3} — Evidence: {perspective findings}

## Perspectives evidence
{relevant excerpts from perspectives.md sections 1.1-1.6}

## Files in scope
{code file paths referenced across perspectives}
```

The validator reads the actual code and returns:
- **CONFIRM**: Hypothesis #1 is the best fit for the evidence.
- **CHALLENGE**: Alternative hypothesis (from the ranking or new) with
  file:line evidence and confidence.

### Round 2 — Rebuttal (only if CHALLENGED)

If the validator returned CONFIRM, skip to Verdict.

Spawn **Agent(diagnosis-rebuttal)** with a structured prompt:

```
The top-ranked hypothesis was challenged by a validator with an alternative.
Defend the original ranking with a concrete execution trace, or concede if
the alternative is more consistent with the evidence.

## Mode
Debug

## Symptom and Reproduction
{from perspectives.md}

## Original Top Hypothesis
{hypothesis #1 with evidence}

## Validator's Challenge
{CHALLENGE reasoning, alternative hypothesis, confidence level}

## All Perspectives
{relevant excerpts from perspectives.md}

## Files to read
{files from both the original hypothesis and validator's alternative}
```

The rebuttal agent returns:
- **INSIST**: Concrete execution trace proving hypothesis #1 is correct.
- **CONCEDE**: Validator's alternative is more consistent with the evidence.

### Verdict (orchestrator — no agent)

| Orchestrator | Validator | Rebuttal | Result |
|--------------|-----------|----------|--------|
| Hypothesis #1 | CONFIRM   | —        | **CONFIRMED**: proceed to Step 4 |
| Hypothesis #1 | CHALLENGE | CONCEDE  | **RECONSIDERED**: re-rank hypotheses and proceed |
| Hypothesis #1 | CHALLENGE | INSIST   | **DISPUTED**: proceed with flag |

**If CONFIRMED**: Proceed directly to Step 4 with hypothesis #1.

**If RECONSIDERED**: Update perspectives.md with the new ranking — promote
the validator's alternative to #1, demote the original, and note "reconsidered
after adversarial challenge" in the ranking. Re-run Round 1 + Round 2 once on
the new #1.
- **Max 1 reconsideration attempt.** On the retry:
  - Retry returns CONFIRMED → proceed to Step 4 with the new #1
  - Retry returns DISPUTED → proceed to Step 4 with the WARNING flag (normal DISPUTED handling)
  - Retry would trigger another RECONSIDERED → stop and ask the user to choose between the original, first alternative, and second alternative

**If DISPUTED**: Proceed to Step 4 with the original #1, but add a
`> [!WARNING] Hypothesis DISPUTED` admonition at the top of perspectives.md
documenting both sides' arguments. The subsequent Expert Review (Step 6)
should re-examine the dispute.

```bash
bts recipe log {id} --action research --result "hypothesis: CONFIRMED | RECONSIDERED | DISPUTED | UNVALIDATED"
```

## Step 4: Draft Fix Spec

Based on the validated top hypothesis, create `.bts/specs/recipes/{id}/draft.md`:

```markdown
# Debug Fix: {symptom}

Recipe: {id}
Ref: r-XXXX (original recipe)
Root Cause: {from cross-reference analysis}

## Evidence
- [Perspective 1.X]: {finding}
- [Perspective 1.Y]: {finding}
- Cross-reference: {inconsistency that proves the cause}

## Changes
For each file to modify:

### {file path}
- **Function**: {name}
- **Current behavior**: {what it does now (wrong)}
- **Fixed behavior**: {what it should do (correct)}
- **Code change**: {specific code modification}
- **Rationale**: {why this fixes the root cause}

## Edge Cases
- {edge case 1}: {how the fix handles it}

## Test Scenarios
- {scenario reproducing the original bug → now passes}
- {regression scenario → still works}
- {edge case scenario → handled correctly}
```

Apply **Draft Self-Check** before saving (same checklist as blueprint).

```bash
bts recipe log {id} --phase draft --action draft --output draft.md
```

## Step 5: Simulate

Use Skill("bts-simulate") on draft.md:
- Does the fix resolve the original symptom?
- Does it handle all evidence from perspectives?
- Does it break anything identified in the impact map (perspective 1.6)?

If gaps found → Edit draft.md → /verify → re-simulate.
When simulation passes, continue immediately to Step 6.

## Step 6: Expert Review (1 round)

Run a focused 1-round debate on draft.md.
Choose 3 experts matching the relevant perspectives:
- e.g., Security Expert (if auth-related), Data Expert (if DB-related),
  Ops Expert (if config/runtime-related)

1 round only: each expert states their assessment.
- Is the root cause correctly identified?
- Is the fix complete?
- Are there risks the perspectives missed?

If Step 3 returned DISPUTED, the Expert Review must specifically re-examine
the validator's alternative hypothesis before reaching consensus.

If experts disagree on root cause → use AskUserQuestion to present each expert's position and ask user to choose.

```bash
bts recipe log {id} --phase debate --action debate --result "{consensus}"
```

## Step 7: Verify Loop

Run /bts-verify on the current draft:
- Is the fix logically sound?
- Does it contradict the original design (final.md)?
- Are edge cases covered?
- Does the evidence support the conclusion?

If issues found → Edit draft.md → re-verify. Do NOT stop to report — fix and continue.
When critical=0, major=0 → continue immediately to Step 8.
`verify.max_iterations` (default: 3) consecutive rounds without progress →
`bts recipe log` exits non-zero with `[CONVERGENCE FAILED]` and names the
stagnant finding IDs → stop and ask the user. Enforced in code, not by
self-counting (`bts-verification-protocol.md § Convergence`).

```bash
bts recipe log {id} --phase verify --action verify
```

Record verify results to verify-log (required for stop hook DONE gate):
```bash
bts recipe log {id} --from-verification .bts/specs/recipes/{id}/verification.md \
  --doc {verified-doc-path} --scope {full|delta} --dimension {verify|audit|simulate ...}
```
Iteration auto-increments. Fallback (no findings block): `--iteration N --critical X --major Y --minor-resolvable R --minor-deferred D`. Never use legacy `--minor` (it maps all minors to blocking [resolvable]).

## Step 8: Finalize

When verify shows critical=0, major=0:
1. Copy `draft.md` to `final.md`
2. Run Skill("bts-status") with arguments: {id}
3. Output `<bts>DONE</bts>`
4. Tell the user (plaintext, after the marker):
   > **Debug spec complete** — `{id}` finalized.
   > Next: run `/bts-implement {id}` to apply the fix.

Stop hook validates verify-log → phase=finalize.

## Next: Implement

After finalization, the debug recipe has produced a verified `final.md`.
Use Skill("bts-implement") with arguments: {id}

This reuses the entire implement infrastructure:
- Task decomposition from final.md
- Build loop with verification
- Test (existing + regression from Test Scenarios)
- Sync (spec ↔ code)
- Status → Complete
