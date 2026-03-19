---
paths:
  - ".bts/**"
---

# BTS File Schema Reference

When creating or updating files in `.bts/state/`, you MUST follow these exact JSON schemas.
After creating or modifying any JSON file, run `bts validate` to verify compliance.

## manifest.json

```json
{
  "current_draft": "drafts/v3.md",
  "level": 2.5,
  "documents": {
    "research/v1.md": {
      "type": "research",
      "created_at": "2026-03-18T10:00:00Z",
      "based_on": [],
      "verified_by": ""
    },
    "drafts/v1.md": {
      "type": "draft",
      "created_at": "2026-03-18T10:30:00Z",
      "based_on": ["research/v1.md"],
      "incorporates": ["debates/001-auth-strategy"],
      "verified_by": "verifications/draft-v1.md"
    },
    "debates/001-auth-strategy": {
      "type": "debate",
      "created_at": "2026-03-18T11:00:00Z",
      "based_on": ["drafts/v1.md"]
    },
    "simulations/001-scenarios.md": {
      "type": "simulation",
      "created_at": "2026-03-18T12:00:00Z",
      "based_on": ["drafts/v2.md"],
      "resolves": []
    },
    "verifications/draft-v1.md": {
      "type": "verification",
      "created_at": "2026-03-18T10:35:00Z"
    }
  }
}
```

Required fields:
- `current_draft` (string): path to current draft version
- `level` (number): document level 0.0-3.0
- `documents` (object): keys are file paths, values are DocumentEntry objects

DocumentEntry required fields:
- `type` (string): one of "research", "draft", "debate", "simulation", "verification", "implementation", "test-result", "deviation"
- `created_at` (string): ISO 8601 timestamp

DocumentEntry optional fields:
- `based_on` (array of strings): parent document paths
- `incorporates` (array of strings): debate/simulation paths incorporated
- `resolves` (array of strings): gap identifiers resolved
- `verified_by` (string): verification document path

## recipe.json

```json
{
  "id": "r-1710720000000",
  "type": "blueprint",
  "topic": "OAuth2 authentication",
  "phase": "verify",
  "iteration": 2,
  "draft_version": 3,
  "level": 2.5,
  "started_at": "2026-03-18T10:00:00Z",
  "updated_at": "2026-03-18T12:00:00Z"
}
```

Required fields:
- `id` (string): unique recipe identifier
- `type` (string): "analyze", "design", "blueprint", or "fix"
- `topic` (string): what the recipe is about
- `phase` (string): current phase — "scoping", "research", "draft", "assess", "improve", "verify", "debate", "simulate", "audit", "finalize", "cancelled", "implement", "test", "sync", "status", "complete"
- `iteration` (number): current verify iteration count
- `draft_version` (number): current draft version number
- `level` (number): assessed document level 0.0-3.0
- `started_at` (string): ISO 8601 timestamp
- `updated_at` (string): ISO 8601 timestamp

Optional fields:
- `ref_recipe` (string): referenced recipe ID (for fix recipes that reference the original)

## diagnosis.md (fix recipe)

Located at `.bts/state/recipes/{id}/diagnosis.md`. Markdown format with sections:
Symptom, Reproduction, Root Cause, Affected Files, Impact, Related Recipe.

## fix-spec.md (fix recipe)

Located at `.bts/state/recipes/{id}/fix-spec.md`. Markdown format with sections:
Current Behavior, Expected Behavior, Changes (per file: function, current, fixed, rationale),
Edge Cases, Regression Test.

## changelog.jsonl

Each line is a JSON object:

```json
{"time":"2026-03-18T10:00:00Z","action":"research","output":"research/v1.md"}
{"time":"2026-03-18T10:30:00Z","action":"draft","output":"drafts/v1.md","based_on":["research/v1.md"]}
{"time":"2026-03-18T10:35:00Z","action":"verify","input":"drafts/v1.md","result":"2 critical, 3 major"}
{"time":"2026-03-18T11:00:00Z","action":"improve","output":"drafts/v2.md","based_on":["drafts/v1.md"],"incorporates":["debates/001"]}
{"time":"2026-03-18T11:30:00Z","action":"debate","output":"debates/001-auth","result":"concluded: OAuth2"}
{"time":"2026-03-18T12:00:00Z","action":"simulate","output":"simulations/001.md","result":"4 gaps found"}
{"time":"2026-03-18T12:30:00Z","action":"assess","result":"Level 2.5","level":2.5}
```

Required fields:
- `time` (string): ISO 8601 timestamp. **Key name is "time", not "timestamp".**
- `action` (string): one of "research", "draft", "improve", "verify", "debate", "simulate", "audit", "assess", "sync-check", "finalize", "implement", "test", "sync", "status", "adjudicate"

Optional fields:
- `input` (string): what was acted on
- `output` (string): what was produced
- `based_on` (array of strings): dependencies
- `incorporates` (array of strings): incorporated debates/simulations
- `resolves` (array of strings): resolved gaps
- `result` (string): summary of outcome
- `level` (number): level after this action

## debate meta.json

Located at `.bts/state/recipes/{id}/debates/{debate-id}/meta.json`:

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

Located at `.bts/state/recipes/{id}/tasks.json`:

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
      "depends_on": [],
      "retry_count": 0,
      "last_error": ""
    },
    {
      "id": "t-002",
      "file": "src/auth/oauth.ts",
      "action": "create",
      "status": "in_progress",
      "description": "OAuth2 implementation",
      "depends_on": ["t-001"],
      "retry_count": 2,
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

Task object optional fields:
- `depends_on` (array of strings): task IDs this depends on
- `retry_count` (number): build retry attempts so far (persisted across sessions)
- `last_error` (string): last build error message (for stagnation detection)

## test-results.json

Located at `.bts/state/recipes/{id}/test-results.json`:

```json
{
  "recipe_id": "r-1710720000000",
  "run_at": "2026-03-18T15:00:00Z",
  "framework": "jest",
  "iterations": 2,
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
  "notes": ["Fixed off-by-one in token expiry check"]
}
```

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

Located at `.bts/state/recipes/{id}/deviation.md`. Markdown format:

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

Located at `.bts/state/project-status.md`. Markdown format:

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

## IMPORTANT RULES

1. **Use exact field names** as shown above. `"time"` not `"timestamp"`. `"decided"` not `"status"`.
2. **`conclusion` is a string**, not an object. Write structured conclusions as a single sentence.
3. **`documents` in manifest is a flat map** where keys are file paths and values are DocumentEntry objects. Not categorized lists.
4. **Always run `bts validate` after creating/modifying any JSON file in `.bts/`.**
5. **Always create `recipe.json`** at the start of a recipe. This is how `bts recipe status` finds the active recipe.
