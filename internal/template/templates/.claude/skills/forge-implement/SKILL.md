---
name: forge-implement
description: >
  Implement code from a finalized Level 3 spec (final.md). Uses an adaptive loop
  with build verification — the same ASSESS→action→VERIFY pattern as spec creation.
user-invocable: true
allowed-tools: Read Write Edit Grep Glob Bash Agent AskUserQuestion mcp__context7__resolve-library-id mcp__context7__get-library-docs
argument-hint: "[recipe-id]"
---

# Implementation: final.md → Working Code

Implement the spec for recipe: $ARGUMENTS

## Settings

Read `.forge/config/settings.yaml` for project-specific limits.
Use settings values if present, otherwise use defaults noted in each step.

## Prerequisites

0. **Resolve recipe ID**: If `$ARGUMENTS` is empty or not a recipe ID,
   run `forge recipe status` to find the active recipe. Use its ID for
   all `{id}` references below. If no active recipe → "No active recipe. Run /recipe blueprint first."

1. Verify final.md exists:
   ```bash
   ls .forge/state/recipes/{id}/final.md
   ```
   If not found → "Run /recipe blueprint first."

2. Verify spec quality gate:
   - Check `verify-log.jsonl` exists and last entry has critical=0, major=0
   - If verify-log is missing or last entry has critical/major > 0 →
     "Spec not verified. Run /recipe blueprint to complete verification before implementing."
   - This prevents implementing from unverified or manually-created specs.

3. Check recipe phase:
   ```bash
   forge recipe status
   ```
   - If phase is "finalize" → fresh start, go to Step 1
   - If phase is "implement" → resume from tasks.json (Step 3)
   - If phase is "test" → smart resume based on artifacts:
     - test-results.json (pass) + simulations/ exists + review.md exists → Step 6 (sync)
     - test-results.json (pass) + simulations/ exists → Step 5.5 (review)
     - test-results.json (pass) → Step 5.3 (simulate)
     - otherwise → Step 5 (test)
   - If phase is "review" → check review.md:
     - review.md exists → Step 6 (sync)
     - otherwise → Step 5.5 (review)
   - If phase is "sync" → Step 6
   - If phase is "status" → check completion requirements:
     - tasks done + test-results pass + review.md + deviation.md → skip to Completion
     - otherwise → Step 7

4. **Load design context**:
   - Read scope.md for tech stack constraints and assumptions
   - Read project-map.md (at `.forge/state/project-map.md`) for layer-specific
     build and test commands. When implementing files across multiple layers,
     use each layer's build command for verification (not a single global command).

## Resume Protocol

If tasks.json exists in the recipe directory:

1. **Stale check**: Compare tasks.json `updated_at` with final.md modification time.
   If final.md is newer → warn: "Spec changed since last implementation. Re-decompose? [y/n]"
   - If yes → go to Step 1 (fresh decomposition)
   - If no → resume below

2. **Task status recovery**: Read tasks.json and find resume point:
   - `in_progress` tasks → the last session was interrupted mid-task. Read the actual file
     to assess how much was written. Complete or rewrite as needed.
   - `pending` tasks → start from the first pending task
   - All `done`/`skipped` → skip to Step 4

3. **Retry count preservation**: Each task's `retry_count` persists across sessions.
   Resume from the saved count, not from 0. If a task has retry_count=4 and max is 5,
   it gets ONE more attempt before being blocked.

## Step 1: Task Decomposition

1. Read `.forge/state/recipes/{id}/final.md`
2. Extract file-level tasks: each file to create or modify becomes a task
3. Determine dependency order (shared types first, then modules, then integration)
4. Save task list to `.forge/state/recipes/{id}/tasks.json`:
   ```json
   {
     "recipe_id": "{id}",
     "started_at": "ISO8601",
     "updated_at": "ISO8601",
     "tasks": [
       {
         "id": "t-001",
         "file": "src/auth/types.ts",
         "action": "create",
         "status": "pending",
         "description": "Auth type definitions — see final.md Section 3.1",
         "depends_on": [],
         "retry_count": 0,
         "last_error": ""
       }
     ]
   }
   ```

5. Update phase and log:
   ```bash
   forge recipe log {id} --phase implement --action implement --output tasks.json --based-on final.md --result "N tasks decomposed"
   ```

6. Validate:
   ```bash
   forge validate
   ```

## Step 2: Scaffolding

1. Create directories for all new files
2. Install dependencies if needed:
   - Node.js: `npm install` / `yarn add`
   - Go: `go mod tidy`
   - Python: `pip install` / `poetry add`
3. Create empty files or boilerplate as needed

**Environment check**: Run the build command once before writing any code.
If it fails with "command not found" or similar environment error → stop immediately
and report: "Build tool not available. Install [tool] before proceeding."
Do NOT proceed to task implementation if the build environment is broken.

## Step 3: Implementation Loop

**Reservations check**: If `.forge/state/recipes/{id}/reservations.md` exists,
read it before starting. When implementing a file listed in the "Affected Files"
section, warn: "[RESERVATION] This area has unresolved concerns from debate:
{concern}. Proceed with extra caution."

For each task in dependency order:

**Dependency check**: If a task's `depends_on` includes a blocked or skipped task,
auto-skip it with status `"skipped"` and last_error `"dependency blocked: {id}"`.

### ASSESS
- Read the task from tasks.json
- If status is `in_progress` → file may be partially written. Read the actual file
  and decide: complete the remaining parts, or rewrite from scratch.
- If status is `pending` → fresh start for this task
- Set status to `in_progress` and save tasks.json immediately

### IMPLEMENT
- Write the code exactly as specified in final.md
- Follow function signatures, types, error handling from the spec
- Preserve existing code when modifying files

### VERIFY
Run the project's build command:
```bash
# Detect and run appropriate build
# TypeScript: npx tsc --noEmit
# Go: go build ./...
# Rust: cargo check
# Python: python -m py_compile
```

**If build fails:**
1. Increment `retry_count` in tasks.json and save `last_error`
2. **Stagnation check**: Compare current error with `last_error`.
   If the error message is substantially the same as the previous attempt →
   try a fundamentally different approach (different algorithm, different API, etc.)
   Do NOT repeat the same fix.
3. Rebuild (check `retry_count` < `implement.max_build_retries` from settings, default: 5)
4. If retry_count reaches the limit → mark task as `blocked`, save error, move to next task

**If build passes:**
- Update task status to `done`, clear `last_error`
- Update tasks.json `updated_at`
- Move to next task

**Crash safety**: tasks.json is the source of truth for implementation progress.
It is written to disk (via Write tool) after every task status change. If the session
crashes, the next resume reads tasks.json and knows exactly where to continue.
No separate work-state save is needed during the loop — tasks.json IS the checkpoint.

### Log Each Task
```bash
forge recipe log {id} --action implement --result "task {task-id} done"
```

## Step 4: Checkpoint

Review task status:
- All `done` or `skipped` → continue to Step 5
- Any `blocked` → ask user:
  - "N task(s) blocked. Options:"
  - "[1] Skip blocked and continue (mark as skipped)"
  - "[2] Retry blocked tasks (reset retry_count to 0)"
  - "[3] Stop and review"
  - If [1] → mark blocked as `skipped`, continue
  - If [2] → reset retry_count, set status to `pending`, go back to Step 3
  - If [3] → stop and report details

> **Checkpoint**: Implementation tasks complete. Proceed directly to testing.
> Do NOT /clear — test/simulate/review steps need implementation context.

## Step 5: Test

Update phase and run tests:
```bash
forge recipe log {id} --phase test
```

Use Skill("forge-test") with arguments: {id}
The test skill will read final.md for test scenarios and tasks.json
for the list of implemented files.

**If tests fail** (forge-test does not output `<forge>TESTS PASS</forge>`):
- Do NOT proceed to review. Stop here.
- Report: "Tests failed. Fix implementation and re-run /implement {id} to retry from Step 5."
- The recipe stays in phase "test" for resume.

## Step 5.3: Simulate

Run /forge-simulate code to verify all code paths are covered:
```bash
/forge-simulate code
```

This reads tasks.json for implemented files and final.md for expected
scenarios, then walks through each scenario against the actual code.

If simulation finds GAPs or ISSUEs:
- Fix the code to address each finding
- Add tests for any COVERAGE GAPs (missing test scenarios)
- Re-run tests: use Skill("forge-test") with arguments: {id}
  (forge-test skips generation, re-runs tests, updates test-results.json)
- If tests fail → fix and let forge-test retry loop handle it
- Do NOT re-run simulation (runs once per implementation)

If no gaps → proceed to Step 5.5 (Review).

```bash
forge recipe log {id} --action simulate --result "N scenarios, N gaps"
```

## Step 5.5: Review

Update phase:
```bash
forge recipe log {id} --phase review
```

Run /forge-review (full mode, no arguments — uses tasks.json for scope).

After review.md is generated, read it and fix all [ACTIONABLE] critical
and major items:
- For each [ACTIONABLE] finding, modify the code to address it
- After all fixes, re-run tests: use Skill("forge-test") with arguments: {id}
  (forge-test skips generation, re-runs tests, updates test-results.json)
- If tests fail → fix and let forge-test retry loop handle it
- Do NOT re-run /review after fixes (review runs once per implementation)

If no actionable items → proceed directly to Step 6.

```bash
forge recipe log {id} --action review --output review.md --result "N critical, N major (N actionable)"
```

## Step 6: Sync

Always run sync (even if deviation.md exists from a previous run — code may have
changed since then, and sync is idempotent).

After tests pass, update phase and sync:
```bash
forge recipe log {id} --phase sync
```

Use Skill("forge-sync") with arguments: {id}

## Step 7: Status

After sync:
```bash
forge recipe log {id} --phase status
```

Use Skill("forge-status") with arguments: {id}

## Completion

When all steps are done:
- Verify tasks.json shows all tasks as `done` or `skipped`
- Verify no `blocked` tasks remain (all resolved or skipped)
- Verify review.md exists (review has run)
- Output `<forge>IMPLEMENT DONE</forge>`

If unresolved blocked tasks remain:
- Report which tasks are blocked and why
- Ask user for guidance
