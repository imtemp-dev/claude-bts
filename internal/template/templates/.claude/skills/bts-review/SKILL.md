---
name: bts-review
description: >
  Multi-perspective code review with quality, security, and architecture agents.
  Basic review (all perspectives) by default, focused review with category,
  or lightweight mid-run checkpoint during implementation.
user-invocable: true
allowed-tools: Read Grep Glob Bash Agent AskUserQuestion
argument-hint: "[security|performance|patterns|--mid-run {window}] [file-path]"
effort: high
---

# Code Review

Review code for: $ARGUMENTS

## Settings

Quality, security, and architecture review require deep reasoning — all
three use the main session model by default. Security is recall-bound:
semantic flaws (authz bypass, state-dependent privilege escalation) that
the initial sweep misses can never be rescued by the rebuttal step, so
the sweep itself needs the strong model. When `.bts/config/settings.yaml`
sets `agents.reviewer_quality`, `agents.reviewer_security` or
`agents.reviewer_arch`, pass that value as the spawn's `model`; the Agent
tool's `model` takes precedence over the agent file. Unset, omit `model`.

## Step 1: Determine Review Mode

Parse $ARGUMENTS:
- If first word is `--mid-run` → **mid-run mode** (Phase 11).
  The second argument is a window like `t-006..t-010` — review ONLY the
  files those tasks touched, plus the files they import. Architecture
  perspective only; quality + security agents are skipped. Adversarial
  Round 2 (rebuttal) is skipped to keep mid-run cost bounded.
  Output goes to `.bts/specs/recipes/{id}/reviews/midrun-{timestamp}.md`,
  NOT to `review.md` — the terminal review (no --mid-run) still runs
  once at implementation completion.
- If first word is `security`, `performance`, or `patterns` → **focused mode**.
  Remaining words = file scope (or all if empty).
- Otherwise → **full mode** (all perspectives), arguments = file scope.

## Step 2: Identify Scope and Context

**File scope:**
If inside a recipe (tasks.json exists):
- Read tasks.json for the list of implemented files
- If test-results.json exists, also include files from `test_files` in scope.
  Test code quality matters: correct assertions, realistic mocks, test isolation.
- If file scope given → filter to matching files
- If no scope → review all files from tasks.json + test files

If inside a recipe but no tasks.json (fix recipe):
- Read fix-spec.md "Changes" section for file paths
- If no fix-spec.md → fall back to git diff

If standalone (no recipe):
- If file scope given → review those files/directories
- If no scope → try `git diff --name-only HEAD~1` to detect recently changed files.
  If changes found, propose reviewing those files.
  If no git or no changes, ask user which files to review.

**Architecture context:**
- Read `.bts/specs/project-map.md` for layer structure.
- Read `.bts/specs/layers/{name}.md` for the relevant layer's patterns.
- If inside a recipe:
  - Read `final.md` for design intent.  For an express build recipe
    (`Mode: express` in `bts recipe status`) no review round read
    `final.md`, so report also where it disagrees with the code: an
    invariant the code does not hold as stated, or a scope-coverage row
    whose test does not hold its item.
  - Read `wireframe.md` for the planned component diagram and, if
    present, the `<!-- architect-decision -->` block showing which
    decomposition was selected and why.
  - Read `domain.md` for invariant ownership — the architecture agent
    verifies each invariant's owner module is the one actually holding
    state in the implementation.
  - Run `bts graph --import --recipe {id}` to extract the actual import
    graph from the files listed in tasks.json. Compare against the
    wireframe component diagram:
      - Edges present in code but not in wireframe → flag as
        `unauthorized_coupling` (critical). Module pair indicates
        unplanned dependency.
      - Edges present in wireframe but not in code → flag as
        `unimplemented_dependency` (critical). Planned dependency was
        omitted during implementation.
- Pass ALL of this context to the architecture agent so it judges
  alignment against the selected decomposition, not just "layer
  separation is clean".

## Step 3: Multi-Perspective Review

### Full Mode (no category — default)

Spawn all 3 agents **in a single message with multiple Agent tool calls** to ensure
true parallel execution. Do NOT spawn them sequentially.
The default perspectives are quality, security, and architecture, but adapt
if the code warrants different emphasis (e.g., performance-critical code may
need a performance perspective instead of or in addition to security):

1. **Agent(reviewer-quality)**: Code quality — correctness, error handling,
   resource management, maintainability.
   If the spec (final.md) contains mermaid flow diagrams, compare the code's
   actual control flow against the spec's expected flow. Flag deviations.

2. **Agent(reviewer-security)**: Security — input validation, authentication,
   data protection, common vulnerability patterns

3. **Agent(reviewer-arch)**: Architecture — alignment with project structure
   (project-map.md, layers), naming conventions, pattern consistency.
   Include project-map.md and layers content in the agent prompt.

Each agent produces a numbered list of findings with severity tags.

### Focused Mode

| Category | Agent(s) | Focus |
|----------|----------|-------|
| `security` | reviewer-security only | Deep security analysis |
| `performance` | reviewer-quality | Performance focus: N+1 queries, memory, blocking I/O, algorithm complexity |
| `patterns` | reviewer-arch | Pattern focus: conventions, structure, consistency |

For focused mode, give the single agent a deeper, more thorough prompt
for its specific domain rather than a broad scan.

## Step 4: Synthesize Raw Findings

After collecting findings from all agents:

1. **Deduplicate**: Same issue found by multiple agents → merge, note all perspectives
2. **Assign unified IDs**: [CRT-001], [MAJ-001], [MIN-001], [INF-001]
3. **Compile findings list**: Each finding with ID, severity, perspective
   (quality, security, architecture), file:line, description, fix
   suggestion, and for a critical the reviewer's `Falsifier:` and
   `Required:` lines (or, for security, `Input:`, `Path:`, `Outcome:`)

This is the raw input for adversarial validation. Do NOT assess practicality here.

## Step 4.2: Run the falsifiers

A critical code-review finding is a claim until something shows it. The
reviewers have no Bash, so you run each critical's falsifier, once, from
the project root:

1. Run it only if it reads or tests: a focused test, a build, a script
   that prints. Never run a command that deploys, sends, deletes, writes
   outside a temp directory, or calls an external service, and never
   `bts test run` (it overwrites the recipe's test results). A falsifier
   you will not run counts as not shown.
2. Record what it printed (the first 40 lines are enough) as the
   finding's observed output.
3. Output shows the failure → the finding stays critical, with the run as
   its evidence. Output does not show it, the command errors for another
   reason, or there is no command → downgrade it to **major**, keep the
   output in its description, and tag it `[UNPROVEN]`.
4. A security critical with an `Input`/`Path`/`Outcome` scenario and no
   command stays critical on the scenario; Step 4.5 tests it.

`bts recipe log --action review` refuses a review.md whose block (Step 5)
has a critical without this evidence, and so does the completion gate.
The cost is one Bash call per critical, the rarest severity.

## Step 4.5: Adversarial Validation

The review agents find problems (prosecution). Now the code gets a defense.
The validator runs on sonnet (its agent file) and the rebuttal on the session
model. When settings.yaml sets `agents.reviewer_validator` or
`agents.reviewer_rebuttal`, pass that value as the spawn's `model`. Rebuttal
uses the session model because constructing concrete failure scenarios
requires deeper reasoning than pattern-based validation.

**Fallback**: If a validator or rebuttal agent fails (error, timeout), skip the
adversarial step and proceed to Step 5 with raw findings. Tag all findings as
`[UNVALIDATED]` in the report so the user knows they were not adversarially checked.

### Round 1 — Defense (Validator)

Spawn **Agent(reviewer-validator)** with a structured prompt:

```
Review the following findings against the actual source code.
For each finding, read the referenced code and determine if it is
a real, practical problem.

## Findings

1. [CRT-001] {title}
   File: {file}:{line}
   Severity: {critical|major|minor|info}
   Found by: {agent}
   Description: {what the reviewer found}
   Shown by: {critical only: the command and the output Step 4.2 recorded,
     or the security scenario}
   Fix suggestion: {suggested fix}

2. [MAJ-001] ...

## Files in scope
{list of file paths the agent should read}
```

The validator reads the actual code for each finding and returns:
- **CONFIRM**: Cannot defend the code. Finding is legitimate.
- **CHALLENGE**: Code-based evidence that this is not a practical problem.

### Round 2 — Rebuttal (only if CHALLENGED items exist)

Collect all CHALLENGED findings. If none, skip to Step 5.

Spawn **Agent(reviewer-rebuttal)** with a structured prompt:

```
The following review findings were challenged by a code validator.
For each, evaluate whether the challenge is valid or the original
finding stands. You must read the actual code to decide.

## Challenged Findings

1. [MAJ-001] {title}
   Original finding: {description from reviewer}
   Validator's defense: {validator's CHALLENGE reasoning with code refs}
   Files to check: {relevant file paths}

2. [MAJ-002] ...
```

The rebuttal agent returns for each:
- **INSIST**: Concrete scenario proving the issue is real, rebutting the defense.
- **CONCEDE**: Validator's defense is valid. Finding is not practical.

### Verdict (orchestrator — no agent)

Classify each finding into consensus categories:

| Review | Validator | Rebuttal | Result |
|--------|-----------|----------|--------|
| Found  | CONFIRM   | —        | **AGREED**: Real issue |
| Found  | CHALLENGE | CONCEDE  | **DISMISSED**: Not practical |
| Found  | CHALLENGE | INSIST   | **DISPUTED**: Orchestrator adjudicates |

For **DISPUTED** items: read the code yourself, evaluate both sides' evidence,
and make a final call. Include both arguments in the report for transparency.

## Step 5: Generate Report

Save to `.bts/specs/recipes/{id}/review.md` if inside a recipe.
Otherwise output directly to user.

```markdown
# Code Review: {scope}

Generated: {ISO8601}
Recipe: {id} (if applicable)
Mode: {full|security|performance|patterns}
Perspectives: {quality + security + architecture | single agent}
Validation: adversarial (2-round debate)

## Summary
- Critical: N
- Major: N
- Minor: N
- Dismissed: N (by adversarial validation)

## Critical
1. [CRT-001] **{title}** in `{file}:{line}`
   Found by: {agent name}
   Consensus: AGREED | ADJUDICATED
   Shown by: `{command}` → {the lines of output that show it}
     (security scenario: Input / Path / Outcome)
   {code context}
   → {fix suggestion}

## Major
...

## Minor
...

## Dismissed (validator defended successfully)
<details>
<summary>N findings dismissed — click to expand</summary>

1. [MAJ-002] **{title}** in `{file}:{line}`
   Original: {finding summary}
   Defense: {validator's evidence}
   Concession: {why rebuttal agent conceded}
</details>

## Adjudicated (disputed — orchestrator decided)
1. [MAJ-003] **{title}** in `{file}:{line}`
   Prosecution: {rebuttal agent's scenario}
   Defense: {validator's evidence}
   Verdict: {INCLUDED|EXCLUDED} — {orchestrator's reasoning}
```

End review.md with one machine-readable block listing every finding that
survived (not the dismissed ones), after severities are final:

```
<bts-review-findings>
{"findings": [
  {"id": "CRT-001", "severity": "critical", "perspective": "quality",
   "title": "...", "anchor": "src/auth/session.go:42", "level": "L4",
   "evidence": {"kind": "run", "command": "go test ./src/auth -run TestExpiry",
                "observed": "--- FAIL: TestExpiry ...", "required": "PASS"}},
  {"id": "CRT-002", "severity": "critical", "perspective": "security",
   "title": "...", "evidence": {"kind": "scenario", "input": "...",
                                "path": "a.go:30 -> b.go:88", "outcome": "..."}},
  {"id": "MAJ-001", "severity": "major", "perspective": "architecture", "title": "...",
   "anchor": "pkg/a.go:12", "level": "L2"}
]}
</bts-review-findings>
```

`severity` is `critical`, `major`, `minor` or `info`. A critical needs
`evidence`: kind `run` with `command`, `observed` and `required`, or, for
`perspective: security` only, kind `scenario` with `input`, `path` and
`outcome`. Other severities need none.

`level` is how far the finding was shown (prove stage P4, after pstack's
blast-radius ladder): `L1` said so, `L2` pointed at the line (`anchor`),
`L3` walked the failure step by step, `L4` ran it (a test or script:
kind `run` evidence), `L5` reproduced it in the running app.  State it on
every finding.  bts refuses a level the evidence does not support: `L4`
or `L5` without `run` evidence, or a `scenario` called more than `L3`.
Prose that a finding "would fail" is `L3` at most until it is run.

Log if inside recipe:
```bash
bts recipe log {id} --action review --output review.md --result "N critical, N major, N dismissed"
```

Review is a **mandatory step** in implement and fix flows.
Critical/major consensus items should be fixed before proceeding.

## Mid-Run Mode (Phase 11)

When invoked as `/bts-review --mid-run {window}`:

1. Parse `{window}` — `t-006..t-010` means tasks t-006 through t-010
   inclusive. Single id (`t-007`) also accepted.
2. Load the recipe's tasks.json. Collect:
   - The files listed in those tasks.
   - The files those tasks import (one hop deep via
     `bts graph --import --recipe {id}`).
   - Any per-task `structure_findings` already recorded by Phase 10's
     MINI-CHECK — include them verbatim as pre-existing findings so the
     reviewer can corroborate or dismiss.
3. Run `bts refactor-signal {id} --json`. It reads the recipe's own
   history for a patch-of-patches pattern (`test_fix_cascade`,
   `cross_module_churn`), deterministically. Each signal is a major
   structural finding: put it in the report under `## Refactor signals`
   and pass it to the reviewer with the pre-existing findings. The
   validator in step 5 does not get to dismiss it — it is a count, not a
   claim.
4. Skip the security + quality agents (architecture only). Add two checks
   to the architecture reviewer's prompt, for the patterns the detector
   in step 3 cannot see:
   - a type escape hatch added to make the planned shape work: `any` or
     an equivalent, a cast, an optional field that is always set;
   - a lock, mutex or serialised queue around state that `domain.md` or
     `wireframe.md` says is not shared.
   Each is major in one task, and critical when the same pattern shows
   up in two or more tasks of the window: one is an edge case, a
   repeated one says the decomposition is wrong.
5. Skip the adversarial rebuttal round — mid-run is cheap on purpose.
   Adversarial validator round 1 (defense) still runs to dismiss
   obviously-wrong findings.
6. Write the output to:
   `.bts/specs/recipes/{id}/reviews/midrun-{ISO8601}.md`
   NOT to `review.md`. The terminal review at /bts-implement Step 5.5
   still runs once at the end with all perspectives + full adversarial.
7. Interpret findings severity for the implement loop:
   - critical → flip the offending task(s) back to `pending` with
     `last_error: "midrun_critical: {short}"` so the loop retries with
     this context.
   - major   → append to the task's `structure_findings`. Surface in
     the final review later.
   - minor   → log only.
   - refactor signal (step 3) → after this report is written, invoke
     `/bts-architect escalate {id} {task-id}` for the latest task in the
     window that touched a module the signal names, before the loop
     starts another task.
8. Log the invocation:
   ```bash
   bts recipe log {id} --action midrun-review --output reviews/midrun-<ts>.md --result "N critical, N major, N dismissed, N refactor signals"
   ```

The mid-run review exists to catch structural drift before it spreads
across many tasks. Run it every `settings.implement.midrun_review_every`
tasks (see bts-implement SKILL.md Step 3).
