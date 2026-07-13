---
name: bts-implement
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

Read `.bts/config/settings.yaml` for project-specific limits.
Use settings values if present, otherwise use defaults noted in each step.

## Prerequisites

0. **Resolve recipe ID**: If `$ARGUMENTS` is empty or not a recipe ID,
   run `bts recipe status` to find the active recipe. Use its ID for
   all `{id}` references below. If no active recipe → "No active recipe. Run /recipe blueprint first."

1. Verify final.md exists:
   ```bash
   ls .bts/specs/recipes/{id}/final.md
   ```
   If not found → "Run /recipe blueprint first."

2. Verify spec quality gate:
   - Check `verify-log.jsonl` exists and last entry has critical=0,
     major=0, minor_resolvable=0 (status "converged" — same bar the
     stop hook applied at `<bts>DONE</bts>`; minor_deferred may be > 0)
   - If verify-log is missing or the bar is not met →
     "Spec not verified. Run /recipe blueprint to complete verification before implementing."
   - This prevents implementing from unverified or manually-created specs.

3. Check recipe phase:
   ```bash
   bts recipe status
   ```
   - If phase is "finalize" → fresh start, go to Step 1
   - If phase is "implement" → resume from tasks.json (Step 3)
   - If phase is "test" → smart resume based on artifacts.
     Code simulation is detected by a `simulations/*-code.md` file (the
     code-mode output), NOT by the simulations/ directory existing —
     document-mode simulations from the blueprint phase always live
     there, so directory existence proves nothing:
     - test-results.json (pass) + simulations/*-code.md + review.md exists → Step 6 (sync)
     - test-results.json (pass) + simulations/*-code.md → Step 5.5 (review)
     - test-results.json (pass) → Step 5.3 (simulate)
     - otherwise → Step 5 (test)
   - If phase is "review" → check review.md and Known Uncertainties:
     - review.md exists AND final.md has no "## Known Uncertainties" section
       OR every Uncertainty entry already carries a `Resolved:` / `Diverged:`
       / `Still-unknown:` line → Step 6 (sync)
     - review.md exists AND at least one Uncertainty entry lacks a
       resolution line → Step 5.7 (resolve uncertainties)
     - review.md does not exist → Step 5.5 (review)
   - If phase is "sync" → Step 6
   - If phase is "status" → check completion requirements:
     - tasks done + test-results pass + review.md + deviation.md → skip to Completion
     - otherwise → Step 7

4. **Load design context**:
   - Read scope.md for tech stack constraints and assumptions
   - Read project-map.md (at `.bts/specs/project-map.md`) for layer-specific
     build and test commands. When implementing files across multiple layers,
     use each layer's build command for verification (not a single global command).
   - Read the "## Known Uncertainties" section of final.md (if present).
     These are runtime-observable items deferred from spec verification — each
     entry has a finding description and a `Why-deferred:` observation that
     would confirm or deny it. Keep this list as an in-memory watch-list for
     Steps 5 (Test), 5.3 (Simulate), and 5.5 (Review). Do NOT treat them as
     implementation tasks; they are things to VALIDATE during normal test/
     simulate/review, not things to build.

## Resume Protocol

If tasks.json exists in the recipe directory:

1. **Anchor staleness** (Phase 9): run `bts validate`. If `task_anchor`
   errors (missing_anchor / orphan_anchor / duplicate_anchor /
   action_mismatch) appear, the final.md anchors have diverged from
   tasks.json either direction — re-decompose. This check runs first
   because mtime-based detection misses cases where both files were
   updated but drifted.
2. **Mtime staleness**: compare tasks.json `updated_at` with final.md
   modification time. If final.md is newer → use AskUserQuestion:
   - "Re-decompose tasks from updated spec" → go to Step 1 (fresh decomposition)
   - "Resume with existing tasks" → resume below

2. **Task status recovery**: Read tasks.json and find resume point:
   - `in_progress` tasks → the last session was interrupted mid-task. Read the actual file
     to assess how much was written. Complete or rewrite as needed.
   - `pending` tasks → start from the first pending task
   - All `done`/`skipped` → skip to Step 4

3. **Retry count preservation**: Each task's `retry_count` (total budget),
   `attempts_in_tier`, and `retry_tier` persist across sessions. Resume
   from the saved counts, not from 0. If a task has retry_count=7 and
   max is 8, it gets ONE more attempt before being blocked.

## Step 1: Task Decomposition

1. Read `.bts/specs/recipes/{id}/final.md`
2. Extract file-level tasks: each file to create or modify becomes a task
3. Determine dependency order (shared types first, then modules, then integration)
4. **Anchor every decomposition** (Phase 9 contract): for each task you
   intend to add, locate the spec block in final.md that describes that
   file and insert an HTML comment immediately above the heading:
   ```
   <!-- task-anchor: {file} {action} -->
   ### `{file}`
   ```
   If the anchor is already present, reuse it. If the spec block does
   not exist yet, EDIT final.md to add the block and the anchor together.
   Every Task MUST carry an `"anchor": "{file} {action}"` field matching
   its comment verbatim. `bts verify` enforces a 1:1 mapping — a missing
   anchor in final.md or tasks.json is a critical finding.

   **Modify tasks declare scope at decomposition time (Phase 14).**
   When `{action}` is `modify`, the anchor MUST carry the authorized
   symbol list and the task mirrors it:
   ```
   <!-- task-anchor: src/auth/session.ts modify scope=validateToken,refreshSession -->
   ```
   plus `"modify_scope": ["validateToken", "refreshSession"]` on the
   task. Derive the list from the final.md spec block for that file
   (the functions/types it says to change). A modify task without
   scope raises `modify_scope_required` (major) at `bts validate`;
   declared symbols absent from the target file raise
   `scope_symbol_missing` (critical) at IMPLEMENT DONE.

5. Save task list to `.bts/specs/recipes/{id}/tasks.json`:
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
         "anchor": "src/auth/types.ts create",
         "depends_on": [],
         "retry_count": 0,
         "last_error": ""
       },
       {
         "id": "t-002",
         "file": "src/auth/session.ts",
         "action": "modify",
         "status": "pending",
         "description": "Token refresh path — see final.md Section 3.2",
         "anchor": "src/auth/session.ts modify scope=validateToken,refreshSession",
         "modify_scope": ["validateToken", "refreshSession"],
         "depends_on": ["t-001"],
         "retry_count": 0,
         "last_error": ""
       }
     ]
   }
   ```

6. Update phase and log:
   ```bash
   bts recipe log {id} --phase implement --action implement --output tasks.json --based-on final.md --result "N tasks decomposed"
   ```

7. Validate:
   ```bash
   bts validate
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

**Reservations check**: If `.bts/specs/recipes/{id}/reservations.md` exists,
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
- **Modify scope (Phase 14)**: when `task.action == "modify"`, touch
  ONLY the symbols listed in `task.modify_scope` (the same list that
  the final.md anchor carries after `scope=`). Adding, removing, or
  renaming any other symbol in the same file is a scope violation.
  If a required change exceeds scope:
    1. Stop work on the task.
    2. Mark it `blocked` with `last_error: "scope_expansion: needs {new_symbols}"`.
    3. Defer to the user — either widen the anchor's `scope=` list in
       final.md (and mirror it in `task.modify_scope`) or split the
       task into two. Do not silently widen scope.
- For other actions, preserve unrelated code when modifying existing files.

### VERIFY
Run the project's build command:
```bash
# Detect and run appropriate build
# TypeScript: npx tsc --noEmit
# Go: go build ./...
# Rust: cargo check
# Python: python -m py_compile
```

**If build fails (Phase 15 retry ladder):**

1. Save the error message to a temp file `last_error.txt`, increment
   BOTH `retry_count` (total budget, never reset) AND `attempts_in_tier`
   (per-tier budget) in tasks.json, and persist `last_error` onto the task.
2. Ask the engine what to do next:
   ```bash
   bts retry next --recipe {id} --task {task-id} --json
   ```
   The response carries `{error_class, current_tier, next_tier, action,
   rationale, reset_attempts_in_tier}`.
   The engine reads `attempts_in_tier` to decide tier exhaustion —
   without the reset in step 4 below, upper tiers are skipped.
3. Execute the returned action:
   - `retry_inplace`    — fix in place, rebuild.
   - `strategy_switch`  — discard previous approach, try a different
     algorithm/API/data-structure. This is the behavior that was
     implicit in the pre-Phase-15 "stagnation check".
   - `spec_escalate`    — re-read the final.md block for this file,
     run `/bts-verify` focused on it. If the block needs additions,
     edit final.md (and re-validate the anchor).
   - `domain_escalate`  — run `/bts-verify domain.md` and confirm the
     invariant ownership for this file has not drifted. If it has,
     reconcile before continuing.
   - `architect_escalate` — invoke `/bts-architect escalate {id} {task-id}`
     to re-decompose the task. The architect may rewrite the anchor's
     scope= list, split the task into two, or update final.md.
   - `block`            — ladder exhausted: set status=blocked,
     persist a `last_error: "retry_ladder_exhausted: {signature}"`
     value, move to the next task.
4. Persist the returned `next_tier` onto `task.retry_tier`. **If the
   response's `reset_attempts_in_tier` is `true`, set
   `task.attempts_in_tier` to 0** — each tier gets its own budget. Use
   the flag verbatim; do NOT recompute it by comparing tiers yourself
   (on the first failure the stored tier is 0 while the engine ran
   tier 1, and a hand-rolled `0 != 1` reset silently costs the ladder
   its architect-escalation tier). Append a one-line
   `task.escalation_notes` entry summarizing the transition
   (`tier N→M: {action} because {error_class}`).
5. Hard cap still applies: `retry_count >= settings.implement.max_build_retries`
   (default 8 — sized to cover the full ladder: 3+2+1+1+1) forces the
   block action regardless of the ladder's preference.

**If build passes:**
- Update task status to `done`, clear `last_error`
- Update tasks.json `updated_at`

**MINI-CHECK (Phase 10)** — run before moving to next task:

```bash
bts check task --recipe {id} --task {task-id} --write
```

Interpret results:
  - 0 findings → continue to next task.
  - severity=major (e.g. import_drift) → persist via `--write` so the
    mid-run review (Step 5.5 / Phase 11) and final review can cite
    them. Do NOT flip the task to blocked — majors are advisory.
  - severity=critical (e.g. symbol_missing, owner_drift) → flip the
    task back to `pending` with `last_error: "structure_critical: {detail}"`.
    These indicate spec-vs-code drift big enough to warrant rework.
  - `bts check task` exits 2 on critical, so shells checking `$?`
    see the failure.

- Move to next task.

**MID-RUN REVIEW (Phase 11)** — after each task transition, check the
mid-run cadence:

On (re)entry to the implementation loop, derive the counter from disk —
it is not persisted separately, so recompute instead of restarting at 0:
`completed_since_last_midrun` = number of `done` tasks whose id comes
after the end of the LAST `reviews/midrun-*.md` window (all done tasks
if no midrun file exists).

```
completed_since_last_midrun += 1
if settings.implement.midrun_review_every > 0 AND
   completed_since_last_midrun >= settings.implement.midrun_review_every:
    window = "{first-task-in-window}..{just-completed-task}"
    Use Skill("bts-review") with arguments: "--mid-run {window}"
    Read the resulting reviews/midrun-*.md. For each AGREED finding:
      - critical → flip task(s) back to pending with last_error
                   "midrun_critical: {short}" and restart the loop for
                   that window.
      - major    → append to per-task structure_findings.
      - minor    → log only.
    completed_since_last_midrun = 0
```

The counter resets after each mid-run run. When `midrun_review_every=0`
the entire block is skipped; the terminal Step 5.5 review still runs.

**Crash safety**: tasks.json is the source of truth for implementation progress.
It is written to disk (via Write tool) after every task status change. If the session
crashes, the next resume reads tasks.json and knows exactly where to continue.
No separate work-state save is needed during the loop — tasks.json IS the checkpoint.

### Log Each Task
```bash
bts recipe log {id} --action implement --result "task {task-id} done"
```

## Step 4: Checkpoint

Review task status:
- All `done` or `skipped` → continue to Step 5
- Any `blocked` → use AskUserQuestion ("N task(s) blocked. How to proceed?"):
  - "Skip blocked and continue" → mark blocked as `skipped`, continue
  - "Retry blocked tasks" → reset retry_count to 0, set status to `pending`, go back to Step 3
  - "Stop and review" → stop and report blocked task details

> **Checkpoint**: Implementation tasks complete. Proceed directly to testing.
> Do NOT /clear — test/simulate/review steps need implementation context.

## Step 5: Test

Update phase and run tests:
```bash
bts recipe log {id} --phase test
```

Use Skill("bts-test") with arguments: {id}
The test skill will read final.md for test scenarios and tasks.json
for the list of implemented files.

**If tests fail** (bts-test does not output `<bts>TESTS PASS</bts>`):
- Do NOT proceed to review. Stop here.
- Report: "Tests failed. Fix implementation and re-run /implement {id} to retry from Step 5."
- The recipe stays in phase "test" for resume.

## Step 5.3: Simulate

Use Skill("bts-simulate") with arguments: "code" to verify all code paths are covered.

This reads tasks.json for implemented files and final.md for expected
scenarios, then walks through each scenario against the actual code.

If simulation finds GAPs or ISSUEs:
- Fix the code to address each finding
- Add tests for any COVERAGE GAPs (missing test scenarios)
- Re-run tests: use Skill("bts-test") with arguments: {id}
  (bts-test skips generation, re-runs tests, updates test-results.json)
- If tests fail → fix and let bts-test retry loop handle it
- Do NOT re-run simulation (runs once per implementation)

If no gaps → proceed to Step 5.5 (Review).

```bash
bts recipe log {id} --action simulate --result "N scenarios, N gaps"
```

## Step 5.5: Review

Update phase:
```bash
bts recipe log {id} --phase review
```

Use Skill("bts-review") (full mode, no arguments — uses tasks.json for scope).

After review.md is generated, read it and fix all [ACTIONABLE] critical
and major items:
- For each [ACTIONABLE] finding, modify the code to address it
- After all fixes, re-run tests: use Skill("bts-test") with arguments: {id}
  (bts-test skips generation, re-runs tests, updates test-results.json)
- If tests fail → fix and let bts-test retry loop handle it
- Do NOT re-run /review after fixes (review runs once per implementation)

If no actionable items → proceed directly to Step 6.

```bash
bts recipe log {id} --action review --output review.md --result "N critical, N major (N actionable)"
```

## Step 5.7: Resolve Known Uncertainties

If final.md has a "## Known Uncertainties" section (loaded in Prerequisites
Step 4), walk each entry now that tests, simulation, and review have all
run. For each Uncertainty entry:

1. Check whether the test suite, simulation walkthrough, or review
   actually exercised the `Why-deferred:` observation.
2. Classify each entry inline in final.md by appending ONE line:
   - `Resolved: <observation>` — a test/simulate/review confirmed the
     expected behavior. Reference the specific test name or simulation
     scenario ID where possible.
   - `Diverged: <observation>` — actual behavior differs from what the
     spec assumed. The spec needs to be synced with reality in Step 6.
   - `Still-unknown: <reason>` — none of the phases exercised this path
     (e.g., requires a specific device, network condition, or user action
     that the test suite does not simulate). Keep in the watch-list for
     post-deploy validation.
3. Do NOT delete entries. The Known Uncertainties section is a persistent
   audit trail showing which runtime concerns were raised and how they
   were addressed.

Log:
```bash
bts recipe log {id} --action resolve-uncertainties --result "N resolved, N diverged, N still-unknown"
```

Diverged entries stay inline in final.md's Known Uncertainties section as
documentation of what changed during implementation. `bts-sync` compares
code against spec and generates deviation.md from code-vs-spec diff; it
does NOT inspect Known Uncertainties entries. The Completion step below
reports Diverged and Still-unknown counts so the user is alerted to both
kinds of runtime findings even though deviation.md won't list them.

## Step 6: Sync

Always run sync (even if deviation.md exists from a previous run — code may have
changed since then, and sync is idempotent).

After tests pass, update phase and sync:
```bash
bts recipe log {id} --phase sync
```

Use Skill("bts-sync") with arguments: {id}

## Step 7: Status

After sync:
```bash
bts recipe log {id} --phase status
```

Use Skill("bts-status") with arguments: {id}

## Completion

When all steps are done:
- Verify tasks.json shows all tasks as `done` or `skipped`
- Verify no `blocked` tasks remain (all resolved or skipped)
- Verify review.md exists (review has run)
- Verify deviation.md exists (sync has run)
- If final.md has a "## Known Uncertainties" section, confirm every entry
  carries a `Resolved:`, `Diverged:`, or `Still-unknown:` line from Step 5.7.
- Output `<bts>IMPLEMENT DONE</bts>`
- Tell the user (plaintext, after the marker):
  > **Implementation complete** — `{id}` done.
  > Check `deviation.md` for any follow-up items (code-vs-spec drift).
  > If final.md has a Known Uncertainties section, separately report:
  >   - `Diverged:` entries — runtime behavior differs from spec assumption;
  >     these do NOT appear in deviation.md (sync only handles code-vs-spec),
  >     so call them out explicitly here.
  >   - `Still-unknown:` entries — not exercised by tests/simulation/review;
  >     watch for these post-deploy.
  > Next: run `/bts-recipe-blueprint` to start the next roadmap item, or `/bts-recipe-fix` for any bugs.

If unresolved blocked tasks remain:
- Report which tasks are blocked and why
- Ask user for guidance
