---
name: bts-status
description: >
  Generate or update project-status.md — a comprehensive view of all recipes,
  their implementation state, deviations, and next steps.
user-invocable: true
allowed-tools: Read Write Edit Grep Glob Bash
argument-hint: "[recipe-id or 'all']"
---

# Project Status: Generate/Update

Update project status for: $ARGUMENTS

If argument is a recipe ID, update status for that recipe only.
If argument is "all" or empty, scan all recipes.

## Step 1: Scan Recipes

Read `.bts/state/recipes/` directory:
```bash
ls .bts/state/recipes/
```

For each recipe directory, read:
- `recipe.json` → type, topic, phase
- `tasks.json` → implementation progress (if exists)
- `test-results.json` → test status (if exists)
- `deviation.md` → sync status (if exists)
- `final.md` → spec exists? (if exists)

## Step 2: Determine Recipe States

For each recipe, determine its state:

| State | Criteria |
|-------|----------|
| `drafting` | recipe.json exists, no final.md |
| `spec` | final.md exists, no tasks.json |
| `implementing` | tasks.json exists, some tasks pending |
| `implemented` | tasks.json exists, all tasks done |
| `tested` | test-results.json exists, status=pass |
| `synced` | deviation.md exists |
| `complete` | tested + synced |

## Step 3: Generate project-status.md

Write to `.bts/state/project-status.md`:

```markdown
# Project Status

Updated: {ISO8601}

## Features

| Recipe | Type | Topic | State | Tests | Deviations |
|--------|------|-------|-------|-------|------------|
| r-xxx | blueprint | Auth | complete | 15/15 pass | 0 |
| r-yyy | design | API | spec | — | — |

## Architecture

### Implemented Files
List all files created/modified across all recipes with tasks.json:

```
src/
  auth/
    types.ts (r-xxx)
    oauth.ts (r-xxx)
    session.ts (r-xxx)
  api/
    routes.ts (r-yyy)
```

## Deviations

Aggregate all deviation.md findings:

| Recipe | Item | Type | Status |
|--------|------|------|--------|
| r-xxx | getUserById | signature | resolved |

## Next Steps

Based on current state, recommend what to do next:
- Recipes in `spec` state → "Run /implement {id}"
- Recipes in `implementing` state → "Resume /implement {id}"
- Recipes in `implemented` state → "Run /test {id}"
- Recipes with failing tests → "Fix failures in ..."
- Complete recipes with deviations → "Follow-up: review deviation.md for improvements"
```

**Note**: `project-status.md` is a global derived document at `.bts/state/` level.
It is NOT tracked in per-recipe manifests because it spans multiple recipes.

## Step 4: Project Map Sync

Two-level map: Level 0 (lightweight overview) + Level 1 (on-demand detail).

### Level 0: project-map.md

Update `.bts/state/project-map.md`:

**If it doesn't exist** and codebase has source files → scan root directory
for layer directories (look for package.json, go.mod, Cargo.toml, pyproject.toml,
or similar build markers). For single-directory projects, one layer at root (./).

**If it exists** → verify layer paths still exist. Remove stale layers,
add newly discovered ones.

Format (~300 tokens):
```markdown
# Project Map

## Layers
services/api/      — NestJS API, npm run build, npm test
services/web/      — React SPA, npm run build, npm run test
packages/shared/   — Shared types, npm run build
```

### Level 1: layers/ (incremental)

For layers changed by this recipe:
- If tasks.json exists: check file paths from tasks
- If fix-spec.md exists (fix recipe): check Changes section for modified files
- If neither: scan changelog.jsonl for implement actions with file references
- Determine which layer each changed file belongs to
- Scan that layer's source files
- Update `.bts/state/layers/{layer-name}.md` with
  (naming: replace `/` with `-`, e.g., `services/api/` → `services-api.md`):
  - File structure (tree with one-line role descriptions)
  - Data models (if schema/model files exist)
  - API endpoints (if route files exist)
  - Key patterns observed
- Don't touch layers that weren't changed

Both are derived documents — delete and re-scan if inconsistent.

## Step 5: Log

If a specific recipe ID was given:
```bash
bts recipe log {id} --action status --result "state: {determined-state}"
```

Validate:
```bash
bts validate
```

Output the status summary to the user directly.
