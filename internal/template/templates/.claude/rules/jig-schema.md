---
paths:
  - ".jig/**"
authoritative_for:
  - json_schema
  - field_names
  - file_paths
---

# jig File Schema Reference

When creating or updating files in `.jig/specs/`, you MUST follow these exact JSON schemas.
After creating or modifying any JSON file, run `jig validate` to verify compliance.

## manifest.json

```json
{
  "current_draft": "draft.md",
  "level": 2.5,
  "documents": {
    "research/v1.md": {
      "type": "research",
      "created_at": "2026-03-18T10:00:00Z"
    },
    "draft.md": {
      "type": "draft",
      "created_at": "2026-03-18T10:30:00Z",
      "based_on": ["research/v1.md"],
      "incorporates": ["debates/001-auth-strategy"],
      "verified_by": "verification.md"
    },
    "verification.md": {
      "type": "verification",
      "created_at": "2026-03-18T10:35:00Z"
    },
    "debates/001-auth-strategy": {
      "type": "debate",
      "created_at": "2026-03-18T11:00:00Z",
      "based_on": ["draft.md"]
    },
    "simulations/001-scenarios.md": {
      "type": "simulation",
      "created_at": "2026-03-18T12:00:00Z",
      "based_on": ["draft.md"]
    }
  }
}
```

Required fields:
- `current_draft` (string): path to the draft file (always `"draft.md"`)
- `level` (number): document level 0.0-3.0
- `documents` (object): keys are file paths, values are DocumentEntry objects

DocumentEntry required fields:
- `type` (string): one of "research", "wireframe", "scope", "draft", "debate", "simulation", "verification", "implementation", "test-result", "deviation", "review", "final"
- `created_at` (string): ISO 8601 timestamp

DocumentEntry optional fields:
- `based_on` (array of strings): parent document paths
- `incorporates` (array of strings): debate/simulation paths incorporated
- `resolves` (array of strings): gap identifiers resolved
- `verified_by` (string): verification document path

Optional manifest fields (review-comment tracking):
- `open_comments` (object): map of doc filename → jig callout count.
  Populated by `jig comment apply --finalize`.
- `blocking_comments` (object): map of doc filename → `[!JIG-BLOCK]`
  callout count. The recipe cannot finalize while sum > 0.

## recipe.json

```json
{
  "id": "r-001-oauth-auth",
  "type": "spec",
  "topic": "OAuth2 authentication",
  "phase": "verify",
  "iteration": 2,
  "level": 2.5,
  "started_at": "2026-03-18T10:00:00Z",
  "updated_at": "2026-03-18T12:00:00Z"
}
```

Required fields:
- `id` (string): unique recipe identifier
- `type` (string): "map", "design", "spec", "fix", or "debug"
- `topic` (string): what the recipe is about
- `phase` (string): current phase — "discovery", "scoping", "research", "domain-model", "architect", "wireframe", "draft", "assess", "improve", "verify", "debate", "simulate", "audit", "finalize", "cancelled", "implement", "test", "review", "sync", "status", "complete"
- `iteration` (number): current verify iteration count
- `level` (number): assessed document level 0.0-3.0
- `started_at` (string): ISO 8601 timestamp
- `updated_at` (string): ISO 8601 timestamp

Optional fields:
- `ref_recipe` (string): referenced recipe ID (for fix recipes that reference the original)

## diagnosis.md (fix recipe)

Located at `.jig/specs/recipes/{id}/diagnosis.md`. Markdown format with sections:
Symptom, Reproduction, Root Cause, Affected Files, Impact, Related Recipe.

## fix-spec.md (fix recipe)

Located at `.jig/specs/recipes/{id}/fix-spec.md`. Markdown format with sections:
Current Behavior, Expected Behavior, Changes (per file: function, current, fixed, rationale),
Edge Cases, Regression Test.

## changelog.jsonl

Each line is a JSON object:

```json
{"time":"2026-03-18T10:00:00Z","action":"research","output":"research/v1.md"}
{"time":"2026-03-18T10:30:00Z","action":"draft","output":"draft.md","based_on":["research/v1.md"]}
{"time":"2026-03-18T10:35:00Z","action":"verify","input":"draft.md","result":"2 critical, 3 major"}
{"time":"2026-03-18T11:00:00Z","action":"improve","output":"draft.md","incorporates":["debates/001"]}
{"time":"2026-03-18T11:30:00Z","action":"debate","output":"debates/001-auth","result":"concluded: OAuth2"}
{"time":"2026-03-18T12:00:00Z","action":"simulate","output":"simulations/001.md","result":"4 gaps found"}
{"time":"2026-03-18T12:30:00Z","action":"assess","result":"Level 2.5","level":2.5}
```

Required fields:
- `time` (string): ISO 8601 timestamp. **Key name is "time", not "timestamp".**
- `action` (string): one of "discover", "research", "domain-model", "architect", "wireframe", "draft", "improve", "verify", "debate", "simulate", "audit", "assess", "sync-check", "finalize", "implement", "test", "sync", "status", "adjudicate", "review", "comment-apply", "resolve-uncertainties", "midrun-review" (must match `validActions` in engine/validator.go)

Optional fields:
- `input` (string): what was acted on
- `output` (string): what was produced
- `based_on` (array of strings): dependencies
- `incorporates` (array of strings): incorporated debates/simulations
- `resolves` (array of strings): resolved gaps
- `result` (string): summary of outcome
- `level` (number): level after this action

## verify-log.jsonl

Located at `.jig/specs/recipes/{id}/verify-log.jsonl`. Each line is a JSON object:

```json
{"time":"2026-03-18T10:35:00Z","iteration":1,"critical":2,"major":3,"minor_resolvable":1,"minor_deferred":0,"doc":"draft.md","full_pass":true,"status":"continue"}
{"time":"2026-03-18T11:00:00Z","iteration":2,"critical":0,"major":1,"minor_resolvable":2,"minor_deferred":1,"doc":"draft.md","status":"continue"}
{"time":"2026-03-18T11:20:00Z","iteration":3,"critical":0,"major":0,"minor_resolvable":0,"minor_deferred":1,"doc":"draft.md","full_pass":true,"status":"converged"}
```

Required fields:
- `time` (string): ISO 8601 timestamp
- `iteration` (number): verify iteration number (1-based, per document)
- `critical` (number): count of critical issues
- `major` (number): count of major issues
- `status` (string): "continue", "converged" (critical=0, major=0, minor_resolvable=0), or "failed" (convergence budget exhausted)

Optional fields:
- `minor_resolvable` (number): [resolvable] minors — block completion
- `minor_deferred` (number): [deferred] minors — runtime watch-items, do not block
- `minor` (number): LEGACY pre-split count; readers treat it as resolvable
- `info` (number): count of info suggestions
- `doc` (string): basename of the verified document. Absent on legacy
  entries, when all documents shared one iteration counter and one
  convergence verdict — so a wireframe round could satisfy draft.md's
  completion gate. Readers narrow by this field; a log with no `doc`
  anywhere is treated as one undifferentiated legacy stream.
- `full_pass` (bool): true when the round verified the whole document,
  false/absent for a `--scope delta` round. Only a full pass may satisfy
  completion (`full_pass_before_final`).
- `dimensions` (string[]): which semantic passes produced these counts —
  any of `verify`, `audit`, `simulate`, canonicalised (lowercased,
  de-duplicated, sorted). Absent on rounds written before `--dimension`
  existed. Together with `full_pass` this is the round's **measurement
  class**: the convergence budget compares a round only against rounds of
  the same class, and completion requires all three
  (`jig-verification-protocol.md § Measurement Strength`).
- `budget` (number): the `verify.max_iterations` in effect when the round
  was judged. The convergence verdict is recomputed over the whole
  history from CURRENT settings, so without this the log cannot say which
  regime produced a given `status`.
- `agent_evidence` (string): "observed" or "none" — whether a subagent
  finished between the previous round and this one. Evidence that
  verification ran in a forked context, deliberately not a gate.
- `doc_hash` / `verification_hash` (string): `sha256:<hex>` of the
  verified document and of `verification.md` as of this round, with line
  endings normalised. These are what the rule-3 gates compare against.
  They live here, in tracked state, precisely so the gates hold in a
  worktree or a fresh clone — the local snapshot directory does not
  travel, and a file's mtime describes the checkout rather than the
  document's history.

Entries are written by `jig recipe log {id} --from-verification <verification.md>`
(preferred — parses the `<jig-findings>` block atomically) or by the
explicit split flags. Always pass `--doc <verified-doc-path>`: it scopes
convergence and the findings ledger to that document, records the
verified revision's hash, and snapshots it for
`jig recipe verify-focus <doc>`. Pass `--scope full|delta` to record the
round's coverage.
Pass `--dimension` once per semantic pass actually run
(`--dimension verify --dimension audit`); declaring one that did not run
makes the budget compare incomparable numbers.

Used by the stop hook and by `jig recipe assess-precheck` — the same
function, so the loop has one oracle — to gate `<jig>DONE</jig>`: the
spec document's own recent entries must show `verify.confirm_passes`
(default 2) consecutive rounds that are each clean (critical=0, major=0,
minor_resolvable=0), a full pass, running every dimension, carrying a
`doc_hash`, agreeing on the same `doc_hash`, and each carrying a
DIFFERENT `verification_hash` — and the last must not be
`status: failed`. One clean round is a sample, not a measurement, and
two rows citing one verification.md are one sample recorded twice
(`jig-verification-protocol.md § Completion Evidence`).

## findings.jsonl

Located at `.jig/specs/recipes/{id}/findings.jsonl`. Append-only event
log giving verification findings a stable identity across rounds — the
substrate the stagnation rule in `jig-verification-protocol.md` needs.
Written automatically by `jig recipe log --from-verification --doc` when
the `<jig-findings>` block carries a `findings` array.

```json
{"id":"F-7fb1c391","doc":"draft.md","iteration":1,"severity":"critical","title":"retry policy contradicts the timeout section","anchor":"§3","status":"open","timestamp":"2026-03-18T10:35:00Z"}
{"id":"F-7fb1c391","doc":"draft.md","iteration":2,"severity":"critical","title":"retry policy contradicts the timeout section","status":"fixed","timestamp":"2026-03-18T11:00:00Z"}
```

Fields:
- `id` (string): `F-` + 8 hex chars of sha256(doc + normalised title).
  Assigned by `jig`, never by hand.
- `doc`, `iteration`, `severity`, `title`, `anchor`: as reported
- `status` (string): `open`, `unreported`, `fixed`, `deferred`,
  `dismissed`. **`unreported` is not a closure** — a finding the latest
  round stopped mentioning. It becomes `fixed` only after a second
  consecutive silent round, and never while its anchor is still
  producing findings of any age — a restatement that is merely carried
  from an earlier round keeps the anchor live just as a brand-new one
  does. Deferred watch-items do not, since they are accepted carry-
  forwards rather than evidence the section is still defective. Absence
  is what a repair looks like, and also what a verifier rewording the
  same defect looks like
  (`jig-verification-protocol.md § Finding Identity`).
- `reason` (string, optional): why it was dismissed

Current state is the fold of the events (last event per ID wins, with
open-round and reopen counters accumulated). Inspect with
`jig recipe findings list {id}`; never hand-edit the file.

## debate.json

Located at `.jig/specs/debates/{debate-id}/debate.json` — the project
tree, which is where `jig debate log` writes it and where `jig debate
list` reads it. `/jig-debate` writes its round markdown under
`.jig/specs/recipes/{id}/debates/{debate-id}/` instead, so a debate
normally has a half in each tree. `jig validate` treats the debate ID as
the unit and looks in both, taking its list of IDs from the recipe's own
tree so one recipe is not held to another's debates. `meta.json` is
accepted as a legacy name for this file; nothing writes it any more.

Two copies of the same debate that DISAGREE are the failure this
arrangement can produce — `jig doctor` reports it under `documents`.

```json
{
  "id": "001-auth-strategy",
  "topic": "OAuth2 vs JWT",
  "rounds": 3,
  "conclusion": "OAuth2 with Redis session cache",
  "decided": true,
  "started_at": "2026-03-18T11:00:00Z",
  "updated_at": "2026-03-18T11:30:00Z"
}
```

Required fields:
- `id` (string): debate identifier
- `topic` (string): debate topic
- `rounds` (number): number of completed rounds
- `decided` (boolean): whether a conclusion was reached
- `started_at` (string): ISO 8601 timestamp
- `updated_at` (string): ISO 8601 timestamp

Optional fields:
- `conclusion` (string): the decision reached (plain text, not object)

## tasks.json

Located at `.jig/specs/recipes/{id}/tasks.json`:

```json
{
  "recipe_id": "r-1710720000000",
  "started_at": "2026-03-18T10:00:00Z",
  "updated_at": "2026-03-18T14:00:00Z",
  "tasks": [
    {
      "id": "t-001",
      "file": "src/auth/types.ts",
      "action": "create",
      "status": "done",
      "description": "Auth type definitions",
      "anchor": "src/auth/types.ts create",
      "depends_on": [],
      "retry_count": 0,
      "last_error": ""
    },
    {
      "id": "t-002",
      "file": "src/auth/session.ts",
      "action": "modify",
      "status": "in_progress",
      "description": "Token refresh path",
      "anchor": "src/auth/session.ts modify scope=validateToken,refreshSession",
      "modify_scope": ["validateToken", "refreshSession"],
      "depends_on": ["t-001"],
      "retry_count": 2,
      "attempts_in_tier": 2,
      "retry_tier": 1,
      "last_error": "TS2345: Argument of type 'string' is not assignable"
    }
  ]
}
```

Required fields:
- `recipe_id` (string): recipe this task list belongs to
- `started_at` (string): ISO 8601 timestamp
- `updated_at` (string): ISO 8601 timestamp
- `tasks` (array): list of task objects

Task object required fields:
- `id` (string): unique task identifier (e.g., "t-001")
- `file` (string): target file path
- `action` (string): "create" or "modify"
- `status` (string): "pending", "in_progress", "done", "blocked", "skipped"
- `description` (string): what this task does
- `anchor` (string): "path action" (Phase 9) — must match a
  `<!-- task-anchor: path action -->` comment in final.md verbatim;
  `jig verify` enforces the 1:1 mapping
- `modify_scope` (array of strings): REQUIRED when action=="modify" —
  authorized symbol list; the anchor carries the same list after `scope=`

Task object optional fields:
- `depends_on` (array of strings): task IDs this depends on
- `retry_count` (number): TOTAL build retry attempts (hard-cap budget vs
  `implement.max_build_retries`; persisted across sessions, never reset)
- `attempts_in_tier` (number): retry-ladder per-tier counter — reset to 0
  on every tier transition (Phase 15)
- `retry_tier` (number): current retry-ladder tier 1..5 (Phase 15)
- `escalation_notes` (array of strings): one entry per tier transition
- `structure_findings` (array of objects): per-task MINI-CHECK results
  `{task_id, category, severity, detail}` (Phase 10)
- `pre_image_sha` / `post_image_sha` (string): file sha256 before
  IMPLEMENT / after VERIFY build pass
- `last_error` (string): last build error message (for stagnation detection)

## test-results.json

Located at `.jig/specs/recipes/{id}/test-results.json`:

```json
{
  "recipe_id": "r-1710720000000",
  "run_at": "2026-03-18T15:00:00Z",
  "framework": "jest",
  "iterations": 2,
  "status": "pass",
  "exit_code": 0,
  "command": "npx jest",
  "recorded_by": "jig",
  "total": 15,
  "passed": 15,
  "failed": 0,
  "skipped": 0,
  "test_files": [
    "src/__tests__/auth.test.ts",
    "src/__tests__/session.test.ts"
  ],
  "failures": [],
  "notes": ["Fixed off-by-one in token expiry check"]
}
```

The core is written by `jig test run {id} --cmd "..."` — it executes
the command and derives `status` from the ACTUAL exit code
(`recorded_by: "jig"`). `status`, `exit_code`, `iterations`,
`recorded_by` MUST NOT be hand-edited; supplement only the descriptive
fields (counts, test_files, scenario_coverage, failures, notes) after
the final run. `jig doctor` flags hand-recorded files (missing
`recorded_by`).

Required fields:
- `recipe_id` (string): recipe this test run belongs to
- `run_at` (string): ISO 8601 timestamp
- `framework` (string): test framework used (e.g., "jest", "go", "pytest")
- `iterations` (number): how many fix-and-rerun iterations
- `status` (string): "pass" or "fail"
- `total` (number): total test count
- `passed` (number): passing test count
- `failed` (number): failing test count
- `skipped` (number): skipped test count

Optional fields:
- `test_files` (array of strings): test file paths
- `failures` (array of objects): failure details `{"test": "name", "error": "message"}`
- `notes` (array of strings): observations for sync step

## deviation.md

Located at `.jig/specs/recipes/{id}/deviation.md`. Markdown format:

```markdown
# Deviation Report: {topic}

Generated: {ISO8601}
Recipe: {id}

## Summary
- Matches: N
- Not Implemented: N
- Spec Additions Needed: N
- Deviations: N

## Not Implemented
| Item | File | Reason |
|------|------|--------|

## Spec Additions
| Item | File | Description |
|------|------|-------------|

## Deviations
| Item | Spec Says | Code Has | Resolution |
|------|-----------|----------|------------|
```

Required sections:
- Summary with counts
- Tables for each category (may be empty)

## project-status.md

Located at `.jig/specs/project-status.md`. Markdown format:

```markdown
# Project Status

Updated: {ISO8601}

## Features

| Recipe | Type | Topic | State | Tests | Deviations |
|--------|------|-------|-------|-------|------------|

## Architecture

### Implemented Files
(tree of implemented files with recipe attribution)

## Deviations

| Recipe | Item | Type | Status |
|--------|------|------|--------|

## Next Steps
(recommendations based on current state)
```

Required sections:
- Features table with state for each recipe
- Architecture section
- Deviations aggregate
- Next steps recommendations

## intent.md

Located at `.jig/specs/recipes/{id}/intent.md`. Markdown format:

```markdown
# Intent: {topic}

Status: EXPLORING | CONFIRMED

## Problem
{pain point or gap}

## Purpose
{why this needs to exist}

## Users
{who and their context}

## Success Criteria
{measurable outcomes}

## Direction
{agreed path forward}

## Key Decisions
- {decision with reasoning}

## Research Notes
{findings from investigation}
```

Status transitions: EXPLORING → CONFIRMED (mutual agreement).
Updated incrementally during discovery conversation.

## vision.md

Located at `.jig/specs/vision.md`. Markdown format:

```markdown
# Vision: {product name}

Status: DRAFT | CONFIRMED
Created: {ISO8601}
Updated: {ISO8601}

## Purpose
{What is being built and why}

## Users
{Who will use this}

## Core Components
- {Component}: {role}

## Technical Constraints
- {constraint}

## Success Criteria
- {criterion}
```

Status transitions: DRAFT → CONFIRMED (user confirms).
Updated when direction changes — always re-confirm after edits.

## roadmap.md

Located at `.jig/specs/roadmap.md`. Markdown format:

```markdown
# Roadmap: {product name}

Status: DRAFT | CONFIRMED
Progress: {done}/{total}

## Items

- [x] {description} (recipe: {recipe-id})
- [ ] {description}
- [-] {description} (skipped: {reason})
```

Checkbox convention: `[x]` done, `[ ]` pending, `[-]` skipped.
Active items get `(recipe: {id})` annotation when a recipe starts.
Go code counts checkboxes for progress hints — no complex parsing needed.

## IMPORTANT RULES

1. **Use exact field names** as shown above. `"time"` not `"timestamp"`. `"decided"` not `"status"`.
2. **`conclusion` is a string**, not an object. Write structured conclusions as a single sentence.
3. **`documents` in manifest is a flat map** where keys are file paths and values are DocumentEntry objects. Not categorized lists.
4. **Always run `jig validate` after creating/modifying any JSON file in `.jig/`.**
5. **Always create `recipe.json`** at the start of a recipe. This is how `jig recipe status` finds the active recipe.
