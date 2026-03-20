---
name: bts-recipe-debug
description: >
  Debug unknown bugs through multi-perspective analysis. Collects 6 "blueprints"
  (data flow, dependencies, design intent, runtime, change history, impact),
  cross-references them to find root cause, then produces a verified fix spec
  implementable via /bts-implement.
user-invocable: true
allowed-tools: Read Write Edit Grep Glob Bash Agent WebSearch WebFetch mcp__context7__resolve-library-id mcp__context7__get-library-docs
argument-hint: "\"symptom description\""
---

# Recipe: Debug

Debug through multi-perspective analysis: $ARGUMENTS

## Context Briefing

Before starting:
1. List `.bts/state/recipes/` → find related recipes (especially the one
   that built the affected code)
2. Read related recipe's final.md → original design intent
3. Check deviation.md → known spec-code differences
4. Scan codebase for files likely related to the symptom

## Resume Check

```bash
bts recipe status
```
If active debug recipe found, read perspectives.md and current draft to resume.

## Step 1: Collect Perspectives

Investigate the symptom from 6 angles. Create
`.bts/state/recipes/{id}/perspectives.md`:

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
Read `.bts/state/architecture.md` for the system-wide view (how modules
connect, overall structure). Then read the specific recipe's final.md
(via ref_recipe) for the detailed design of the affected feature.
- architecture.md: overall structure, module relationships
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

## Step 3: Draft Fix Spec

Based on the top hypothesis, create `.bts/state/recipes/{id}/drafts/v1.md`:

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
bts recipe log {id} --phase draft --action draft --output drafts/v1.md
```

## Step 4: Simulate

Use Skill("bts-simulate") on drafts/v1.md:
- Does the fix resolve the original symptom?
- Does it handle all evidence from perspectives?
- Does it break anything identified in the impact map (perspective 1.6)?

If gaps found → update draft → re-simulate.

## Step 5: Expert Review (1 round)

Run a focused 1-round debate on drafts/v1.md.
Choose 3 experts matching the relevant perspectives:
- e.g., Security Expert (if auth-related), Data Expert (if DB-related),
  Ops Expert (if config/runtime-related)

1 round only: each expert states their assessment.
- Is the root cause correctly identified?
- Is the fix complete?
- Are there risks the perspectives missed?

If experts disagree on root cause → ask user for decision.

```bash
bts recipe log {id} --phase debate --action debate --result "{consensus}"
```

## Step 6: Verify Loop

Run /bts-verify on the current draft:
- Is the fix logically sound?
- Does it contradict the original design (final.md)?
- Are edge cases covered?
- Does the evidence support the conclusion?

If issues found → update draft (new version) → re-verify.
Max 3 iterations → [CONVERGENCE FAILED] → ask user.

```bash
bts recipe log {id} --phase verify --action verify
```

## Step 7: Finalize

When verify shows critical=0, major=0:
1. Copy current draft to `final.md`
2. Output `<bts>DONE</bts>`

Stop hook validates verify-log → phase=finalize.

## Next: Implement

After finalization, the debug recipe has produced a verified `final.md`.
Use the standard implement flow:

```
/bts-implement {id}
```

This reuses the entire implement infrastructure:
- Task decomposition from final.md
- Build loop with verification
- Test (existing + regression from Test Scenarios)
- Sync (spec ↔ code)
- Status → Complete
