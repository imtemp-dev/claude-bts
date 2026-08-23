---
name: jig-status
description: >
  Generate or update project-status.md — a comprehensive view of all recipes,
  their implementation state, deviations, and next steps.
user-invocable: true
allowed-tools: Read Write Edit Grep Glob Bash
argument-hint: "[recipe-id or 'all']"
effort: low
---

# Project Status: Generate/Update

Update project status for: $ARGUMENTS

If argument is a recipe ID, update status for that recipe only.
If argument is "all" or empty, scan all recipes.

## The report contract {gate: hard}

Both the chat reply and the "Where things stand" section of
`project-status.md` render **exactly these four sections, in this order**:

1. **Your Call** — items that need the *user's own* action now: an open
   decision, a spec waiting on approval, a credential, a blocker only they
   can clear.
2. **Recently Landed** — recipes that reached `complete`, plus merged
   follow-ups, in the current baseline.
3. **Underway** — work progressing on its own, one line of current state
   per recipe.
4. **Charted Next** — queued or gated work waiting on the project or on
   another recipe, never on the user.

Rules that make the contract unambiguous:

- **Every section always renders**, even when empty, with its short
  empty-state sentence. Never omit a section.
  - Your Call → "Nothing needs your action right now."
  - Recently Landed → "No recent completions."
  - Underway → "Nothing is underway."
  - Charted Next → "Nothing is queued."
- **The four buckets are mutually exclusive.** Every item lands in exactly
  one: needs-your-action is Your Call, done is Recently Landed,
  self-progressing is Underway, not-yet-started or blocked-on-something-
  else is Charted Next.
- **Action-free items never enter Your Call.** A recipe mid-verify-loop, a
  recipe blocked on another recipe, a completed recipe's deviation report,
  and a failing test the loop is still retrying all belong to one of the
  other three sections. Your Call is for things that stay stuck until the
  user personally does something.
- **Every report is a complete current snapshot, never a delta.** Do NOT
  read the previous `project-status.md` to decide what changed, what to
  omit, or what to call new. Regenerate from the recipe state on disk every
  time. A report that describes itself relative to an earlier report
  compounds every error the earlier one made.
- Recently Landed renders its current baseline every run, even when the
  same completions appeared last time.

## Step 1: Scan Recipes

Read `.jig/specs/recipes/` directory:
```bash
ls .jig/specs/recipes/
```

For each recipe directory, read:
- `recipe.json` → type, topic, phase
- `tasks.json` → implementation progress (if exists)
- `test-results.json` → test status (if exists)
- `deviation.md` → sync status (if exists)
- `review.md` → code review findings (if exists)
- `final.md` → spec exists? (if exists)

Then collect the cross-recipe signals that decide bucket placement:

```bash
jig recipe decision list <id> --open --json   # per recipe: questions owed by the user
jig doctor                                     # errors that need a person
```

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

Then assign each recipe to exactly one bucket:

| Signal | Bucket |
|--------|--------|
| any open decision (`jig recipe decision list --open`) | **Your Call** |
| verify-log last entry `status: failed` (convergence gave up) | **Your Call** |
| state `spec` — spec finalized, implementation not started | **Your Call** |
| state `complete` | **Recently Landed** |
| state `drafting`, `implementing`, `implemented`, `tested`, `synced` | **Underway** |
| blocked on another recipe, or a roadmap item with no recipe yet | **Charted Next** |

A recipe matching more than one row takes the **topmost** match, which is
what keeps the buckets mutually exclusive.

## Step 3: Generate project-status.md

Write to `.jig/specs/project-status.md`, regenerating the whole file from
the scan above — never by editing the previous version:

```markdown
# Project Status

Updated: {ISO8601}

## Where things stand

### Your Call
- **r-xxx** "OAuth2" — decision `token-storage` open: keychain or httpOnly cookie?
  → `jig recipe decision resolve r-xxx token-storage --answer "..."`
(or: Nothing needs your action right now.)

### Recently Landed
- **r-yyy** "API routes" — complete, 15/15 tests pass, 0 unresolved deviations
(or: No recent completions.)

### Underway
- **r-zzz** "Session store" — implementing, 4/9 tasks done
(or: Nothing is underway.)

### Charted Next
- **Rate limiting** — roadmap item, no recipe yet
- **r-www** "Admin UI" — blocked on r-zzz
(or: Nothing is queued.)

## Features

| Recipe | Type | Topic | State | Tests | Deviations |
|--------|------|-------|-------|-------|------------|
| r-xxx | spec | Auth | complete | 15/15 pass | 0 |
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

## Reviews (if any recipe has review.md)

| Recipe | Mode | Critical | Actionable |
|--------|------|----------|-----------|
| r-xxx | full | 0 | 2 |

## Roadmap

If `.jig/specs/roadmap.md` exists: "Roadmap: {done}/{total} done", and the
next pending item.
```

**Note**: `project-status.md` is a global derived document at `.jig/specs/` level.
It is NOT tracked in per-recipe manifests because it spans multiple recipes.

## Step 3.5: Roadmap Update

If `.jig/specs/roadmap.md` exists:

1. Read roadmap.md
2. For each completed recipe (phase=complete):
   - First, look for `(recipe: {id})` annotation in roadmap items → exact match
   - If no annotation found, fall back to topic similarity match
   - Mark as `[x]` and add `(recipe: {id})` if not already present
3. For each active recipe (not complete/cancelled):
   - Add `(recipe: {id})` to matching item if not present
4. Update `Progress:` line with current counts
5. Save roadmap.md

If no roadmap.md → skip this step.

## Step 4: Project Map Sync

Two-level map: Level 0 (lightweight overview) + Level 1 (on-demand detail).

### Level 0: project-map.md

Update `.jig/specs/project-map.md`:

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
- Update `.jig/specs/layers/{layer-name}.md` with
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
jig recipe log {id} --action status --result "state: {determined-state}"
```

Validate:
```bash
jig validate
```

## Step 6: Report to the user

Render the same four sections in chat, in the same order, each always
present. Keep it to one scannable line per item; the detail belongs in the
file. Do not add a fifth section, and do not describe the report as a
change relative to a previous run.
