---
name: bts-test
description: >
  Generate and run tests based on final.md test scenarios. Runs an adaptive loop:
  execute tests, analyze failures, fix implementation code, re-run until all pass.
user-invocable: true
allowed-tools: Read Write Edit Grep Glob Bash Agent mcp__context7__resolve-library-id mcp__context7__get-library-docs
argument-hint: "[recipe-id]"
---

# Test: Generate + Execute + Fix Loop

Generate and run tests for recipe: $ARGUMENTS

## Settings

Read `.bts/config/settings.yaml` for project-specific limits.

## Prerequisites

0. **Resolve recipe ID**: If `$ARGUMENTS` is empty, run `bts recipe status`
   to find the active recipe. Use its ID for all `{id}` references below.

1. Verify tasks.json exists and implementation is done:
   ```bash
   ls .bts/specs/recipes/{id}/tasks.json
   ```
   If not found → "Run /implement first."

2. Check tasks.json: all tasks should be `done` or `skipped` (no `pending` or `in_progress`).
   If incomplete → "Implementation not finished. Complete /implement first."

## Resume

If test-results.json already exists:
- Skip Steps 1-2 (test scenarios already extracted, test code already generated)
- Go directly to Step 3 (always re-run tests — code may have changed since last run)
- If status was `"fail"` → read previous failures for context before re-running

If test-results.json does not exist → start from Step 1

## Step 1: Extract Test Scenarios

1. Read `.bts/specs/recipes/{id}/final.md`
2. Find all test scenarios defined in the spec:
   - Happy path scenarios
   - Error path scenarios
   - Edge cases
   - Integration scenarios
3. **Name the invariants**: list the `INV-NNN` ids from domain.md (and the
   falsifier rows in final.md); each test that checks one names it in its
   test name or docstring.
4. Read existing test files in the project to understand:
   - Test framework (Jest, Go testing, pytest, etc.)
   - Test file naming convention
   - Test utilities and helpers already available

## Step 2: Generate Test Code

For each test scenario from final.md:
1. Create test file following project conventions
2. Write test cases that verify the behavior described in the spec
3. Use existing test utilities where available
4. Include setup/teardown as needed
Test files go in the project's test directory (not in `.bts/`).

## Step 2.5: Test Coverage Check

Before running tests, verify that generated tests cover the spec:

1. For each test scenario in final.md, confirm a test case exists:
   - Match scenario name/description to test case name
   - If scenario has no matching test → generate the missing test

2. For each test case, verify assertions are meaningful:
   - Tests with no assertions → add assertions
   - Tests that only check "no error" → add value assertions
   - Tests that always pass (trivial) → fix to test actual behavior

3. Cross-check: do test edge cases match spec edge cases?

This step runs only on first invocation (when Steps 1-2 also run).
On resume (Steps 1-2 skipped), this step is also skipped.

## Step 3: Execution Loop

Track failure history for oscillation detection.

Repeat the following (max `settings.implement.max_test_iterations` — authoritative source is `.bts/config/settings.yaml`; do NOT hardcode a default here):

### RUN
Execute the test suite via the CLI so status is machine-truthful:
```bash
# Detect the project's test command, then run it THROUGH bts:
bts test run {id} --cmd "go test ./..."
# Node.js: --cmd "npx jest" / "npx vitest run" / "npm test"
# Python:  --cmd "pytest"        Rust: --cmd "cargo test"
```
`bts test run` executes the command itself, streams the output, and
writes test-results.json with `status` derived from the ACTUAL exit
code (`recorded_by: "bts"`), auto-incrementing `iterations`. It exits
non-zero on failure. Do NOT run the test command via plain Bash and
hand-write test-results.json — the DONE gates trust the
machine-recorded status, and `bts doctor` flags hand-recorded files.
Full output is also saved to `.bts/local/recipes/{id}/test-output.log`.

### ASSESS
If all tests pass → proceed to Step 4.

If tests fail:
- Read the failure output carefully
- Classify each failure by setting its `category` field in
  `test-results.json → failures[]` (Phase 13 — validator enforces
  presence of `category` when `status=="fail"`):
  - `implementation`: the test matches the spec's intent and the code
    fails. → fix impl.
  - `test`: the test asserts the wrong thing. → fix test.
  - `spec`: final.md contradicts itself, or is silent on the case. → note for sync.

**Oscillation check**: Compare current failing tests with previous iterations.
If a test that was fixed in iteration N is failing again in iteration N+2 →
the fixes are conflicting. Stop the loop and report:
"[OSCILLATION DETECTED] Tests {list} are cycling between pass and fail.
Manual intervention needed."

### FIX
- Fix implementation code for implementation bugs
- Fix test code for test bugs
- Note spec ambiguities for the sync step

**Important**: When fixing implementation code, note which files were modified.
These modifications will appear as deviations in the sync step — this is expected.

### RUN again
- Re-run via `bts test run {id} --cmd ...` after fixes (iterations
  auto-increment; each run rewrites the machine-truth core and clears
  prior annotations)
- Continue until all pass or max iterations reached

If max iterations reached with failures:
- Report remaining failures with classification
- Continue to Step 4 with partial results (status will be "fail")

## Step 4: Record Results

1. The final `bts test run` invocation already wrote the machine-truth
   core of `.bts/specs/recipes/{id}/test-results.json`: `status`,
   `exit_code`, `iterations`, `command`, `recorded_by: "bts"`.
   **NEVER hand-edit these fields** — the DONE gates trust them.
   Counts can be passed at run time (`--total 15 --passed 15 --failed 0
   --skipped 0`) or supplemented in step 2.

2. Supplement the SAME file with the descriptive fields via the Edit
   tool (add keys; do not rewrite the file — and only AFTER the final
   run, since each `bts test run` resets annotations):
   ```json
   {
     "total": 15,
     "passed": 15,
     "failed": 0,
     "skipped": 0,
     "test_files": [
       "src/__tests__/auth.test.ts",
       "src/__tests__/session.test.ts"
     ],
     "failures": [
       { "test": "x", "error": "...", "category": "implementation" }
     ],
     "notes": []
   }
   ```
   `failures[].category` is REQUIRED whenever `status=="fail"`.

2. Log to changelog and manifest:
   ```bash
   bts recipe log {id} --action test --output test-results.json --based-on tasks.json --result "N/N passed"
   ```

3. Validate:
   ```bash
   bts validate
   ```

If all tests pass → output `<bts>TESTS PASS</bts>`
If failures remain → report them and ask for guidance.
