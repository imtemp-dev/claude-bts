---
name: bts-simulate
description: >
  Walk through scenarios against a spec document to find gaps, unconsidered
  cases, and incorrect assumptions. Like testing code, but testing the document.
user-invocable: true
allowed-tools: Read Write Agent
argument-hint: "[file-path]"
---

# Document Simulation

Run scenarios against the spec to find what's missing or wrong.

## Protocol

1. Read the target document fully.

2. Design 5+ scenarios across these categories:
   - **Happy path**: Normal successful flow end-to-end
   - **Error/failure**: What happens when things break? Network down? Invalid input? Timeout?
   - **Security**: What can a malicious user do? Injection? CSRF? Auth bypass?
   - **Scale**: What happens with 10x, 100x, 1000x load? Concurrent access?
   - **Edge cases**: Empty input, null, maximum size, unicode, boundary values?

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

## Output Format

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

## After Simulation

The recipe's adaptive loop should:
1. IMPROVE the spec to fill the gaps
2. Run /verify after improvement (mandatory)
3. Consider re-simulating after major changes
