---
name: bts-simulate
description: >
  Walk through scenarios to find gaps and incorrect assumptions.
  Document mode: test a spec document. Code mode: test implemented code
  against its spec. Both use scenario-based walkthrough.
user-invocable: true
allowed-tools: Read Write Agent Grep Glob
argument-hint: "[file-path] or code"
effort: max
---

# Simulation

Run scenarios to find what's missing or wrong: $ARGUMENTS

## Settings

Read `.bts/config/settings.yaml`. If `agents.simulator` is set, pass that model
when spawning Agent(simulator) below.

## Mode Detection

Parse $ARGUMENTS:
- If first word is `code` → **Code Simulation** (see below)
- Otherwise → **Document Simulation** (spec walkthrough)

---

## Code Simulation

Simulate against implemented code to verify all paths are covered.

### Step 1: Identify Code Files and Spec

If tasks.json exists (implement recipe):
- Read tasks.json for implemented file list
- Read final.md for expected behavior and test scenarios

If no tasks.json (fix recipe):
- Read fix-spec.md "Changes" section for file paths
- Read fix-spec.md for expected behavior

### Step 2: Read Code

Read each implemented code file completely. Build a mental model of:
- All functions and their call graph
- All branches (if/else, switch, error returns)
- All error handling paths
- All external calls (DB, API, file I/O)

### Step 3: Design Scenarios

Design at least `simulate.min_scenarios` (default: 5) scenarios from the spec.
Cover the full risk surface — think about what could go wrong, what could be
misused, and what happens at boundaries. Typical concerns include normal flow,
failure modes, edge cases, security, and cross-component interactions, but
adapt the scenarios to what matters for this specific code.

### Step 4: Walk Through Code

For each scenario, trace the actual code path:
```
Scenario: [name]
Entry: [function/handler]
Step 1: [input] → code path: [function:line] → result: [X] ✓
Step 2: [action] → code path: [function:line] → **GAP: no handling for [Y]**
Step 3: [action] → code path: [function:line] → **ISSUE: spec says [A], code does [B]**
```

Additionally, for each scenario:
- Check if a test exists that exercises this code path
- If no test found → flag as **COVERAGE GAP**: "No test for scenario: [name]"
- Coverage gaps should be addressed by adding tests before re-running

Spawn Agent(simulator) for deeper analysis:
```
Read the code files [list] and spec at [final.md/fix-spec.md].
For each scenario [list], trace through the actual code paths.
At each step, check:
- Does the code handle this case?
- If handled, does it match the spec's expected behavior?
- If not handled, this is a GAP.
- Is there a test that covers this scenario? If not, flag COVERAGE GAP.
Report all GAPs, ISSUEs, and COVERAGE GAPs with severity and file:line references.
```

### Step 5: Classify and Report

- **critical**: Code path leads to crash, data loss, or security issue
- **major**: Important scenario not handled in code
- **minor**: Edge case missing but unlikely in practice

Save to `.bts/state/recipes/{id}/simulations/NNN-code.md`

Log:
```bash
bts recipe log {id} --action simulate --result "N scenarios, N gaps (N critical)"
```

### After Code Simulation

The implement/fix flow should:
1. Fix the code to address GAPs and ISSUEs
2. Add tests for any COVERAGE GAPs found
3. Re-run tests: use Skill("bts-test") (mandatory after fixes)
4. Do NOT re-run simulation (runs once per implementation)

---

## Document Simulation

Run scenarios against the spec to find what's missing or wrong.

### Protocol

1. Read the target document fully.

2. Design at least `simulate.min_scenarios` (default: 5) scenarios.
   Cover the full risk surface for this specific document — think about what could
   go wrong, what could be misused, what happens at boundaries, and what breaks
   under load. Adapt the scenario categories to what matters for this spec rather
   than following a fixed checklist.

3. For each scenario, walk through the spec step by step:
   ```
   Scenario: [name]
   Step 1: [action] → spec says [X] ✓ or → spec says nothing → GAP
   Step 2: [action] → spec says [Y] but [problem] → ISSUE
   ...
   ```

4. Spawn Agent(simulator) for deeper scenario analysis:
   ```
   Read the document at $ARGUMENTS and these scenarios: [list].
   For each scenario, trace through the document's described flow.
   At each step, check:
   - Is this step specified in the document?
   - If specified, is it correct and complete?
   - If not specified, this is a GAP.
   Report all GAPs and ISSUEs with severity.
   ```

5. Classify findings:
   - **critical**: Scenario leads to undefined behavior or crash
   - **major**: Important scenario not covered
   - **minor**: Edge case not mentioned but handleable

6. Save simulation results to `.bts/state/{id}/simulations/NNN-[category].md`

7. Log in changelog:
   ```bash
   bts recipe log {id} --action simulate --gaps N
   ```

### Output Format

```markdown
# Simulation: [document name]

## Scenario 1: [Happy Path - User Login]
- Step 1: User clicks login → spec: redirect to OAuth ✓
- Step 2: OAuth callback → spec: exchange code for token ✓
- Step 3: Token received → spec: create session → **GAP: session store not specified**
- Step 4: Redirect to dashboard → spec: redirect to / ✓
Result: 1 GAP found

## Scenario 2: [Error - Expired Auth Code]
- Step 1: Callback with expired code → spec: return 401 ✓
- Step 2: User experience → **GAP: what does the user see? Error page? Redirect?**
Result: 1 GAP found

...

## Summary
Total scenarios: 5
GAPs found: 4 (critical: 1, major: 2, minor: 1)
```

### After Document Simulation

The recipe's adaptive loop should:
1. IMPROVE the spec to fill the gaps
2. Run /verify after improvement (mandatory)
3. Consider re-simulating after major changes
