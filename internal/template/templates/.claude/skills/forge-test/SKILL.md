---
name: forge-test
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

Read `.forge/config/settings.yaml` for project-specific limits.

## Prerequisites

0. **Resolve recipe ID**: If `$ARGUMENTS` is empty, run `forge recipe status`
   to find the active recipe. Use its ID for all `{id}` references below.

1. Verify tasks.json exists and implementation is done:
   ```bash
   ls .forge/state/recipes/{id}/tasks.json
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

1. Read `.forge/state/recipes/{id}/final.md`
2. Find all test scenarios defined in the spec:
   - Happy path scenarios
   - Error path scenarios
   - Edge cases
   - Integration scenarios
3. Read existing test files in the project to understand:
   - Test framework (Jest, Go testing, pytest, etc.)
   - Test file naming convention
   - Test utilities and helpers already available

## Step 2: Generate Test Code

For each test scenario from final.md:
1. Create test file following project conventions
2. Write test cases that verify the behavior described in the spec
3. Use existing test utilities where available
4. Include setup/teardown as needed

Test files go in the project's test directory (not in `.forge/`).

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

Repeat the following (max `implement.max_test_iterations` from settings, default: 5):

### RUN
Execute the test suite:
```bash
# Detect and run appropriate test command
# Node.js: npx jest / npx vitest / npm test
# Go: go test ./...
# Python: pytest
# Rust: cargo test
```

### ASSESS
If all tests pass → proceed to Step 4.

If tests fail:
- Read the failure output carefully
- Classify each failure:
  - **Implementation bug**: the code doesn't match the spec → fix the implementation
  - **Test bug**: the test itself is wrong (bad assertion, wrong setup) → fix the test
  - **Spec ambiguity**: the spec doesn't clearly define expected behavior → note for sync

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
- Re-run tests after fixes
- Continue until all pass or max iterations reached

If max iterations reached with failures:
- Report remaining failures with classification
- Continue to Step 4 with partial results (status will be "fail")

## Step 4: Record Results

1. Save test results to `.forge/state/recipes/{id}/test-results.json`:
   ```json
   {
     "recipe_id": "{id}",
     "run_at": "ISO8601",
     "framework": "jest",
     "iterations": 3,
     "status": "pass",
     "total": 15,
     "passed": 15,
     "failed": 0,
     "skipped": 0,
     "test_files": [
       "src/__tests__/auth.test.ts",
       "src/__tests__/session.test.ts"
     ],
     "failures": [],
     "notes": []
   }
   ```

2. Log to changelog and manifest:
   ```bash
   forge recipe log {id} --action test --output test-results.json --based-on tasks.json --result "N/N passed"
   ```

3. Validate:
   ```bash
   forge validate
   ```

If all tests pass → output `<forge>TESTS PASS</forge>`
If failures remain → report them and ask for guidance.
