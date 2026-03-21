---
name: bts-review
description: >
  Multi-perspective code review with quality, security, and architecture agents.
  Basic review (all perspectives) by default, or focused review with category.
  Includes practical assessment of findings.
user-invocable: true
allowed-tools: Read Grep Glob Bash Agent AskUserQuestion
argument-hint: "[security|performance|patterns] [file-path]"
---

# Code Review

Review code for: $ARGUMENTS

## Step 1: Determine Review Mode

Parse $ARGUMENTS:
- If first word is `security`, `performance`, or `patterns` → **focused mode**
  Remaining words = file scope (or all if empty)
- Otherwise → **full mode** (all perspectives), arguments = file scope

## Step 2: Identify Scope and Context

**File scope:**
If inside a recipe (tasks.json exists):
- Read tasks.json for the list of implemented files
- If file scope given → filter to matching files
- If no scope → review all files from tasks.json

If standalone (no recipe):
- If file scope given → review those files/directories
- If no scope → try `git diff --name-only HEAD~1` to detect recently changed files.
  If changes found, propose reviewing those files.
  If no git or no changes, ask user which files to review.

**Architecture context:**
- Read `.bts/state/project-map.md` for layer structure
- Read `.bts/state/layers/{name}.md` for the relevant layer's patterns
- If inside a recipe, read final.md for design intent
- Pass this context to the architecture agent

## Step 3: Multi-Perspective Review

### Full Mode (no category — default)

Spawn 3 agents in parallel, each reviewing the same files from a different angle:

1. **Agent(reviewer-quality)**: Code quality — error handling, validation,
   resource management, code smells, null safety, logging

2. **Agent(reviewer-security)**: Security — injection, auth bypass,
   data exposure, XSS/CSRF, crypto weaknesses

3. **Agent(reviewer-arch)**: Architecture — structural alignment with
   project-map/layers, naming conventions, pattern consistency, API consistency.
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

## Step 4: Synthesize and Assess Practicality

After collecting findings from all agents:

1. **Deduplicate**: Same issue found by multiple agents → merge, note all perspectives
2. **Reclassify severity**: With full context, some findings may be more or less severe
3. **Practical assessment** for each finding:
   - Will this actually cause a bug in production?
   - Is this a real security risk or purely theoretical?
   - Is fixing this worth the effort vs the risk?
   - Tag: **[ACTIONABLE]** (should fix) vs **[INFORMATIONAL]** (good to know)

## Step 5: Generate Report

Save to `.bts/state/recipes/{id}/review.md` if inside a recipe.
Otherwise output directly to user.

```markdown
# Code Review: {scope}

Generated: {ISO8601}
Recipe: {id} (if applicable)
Mode: {full|security|performance|patterns}
Perspectives: {quality + security + architecture | single agent}

## Summary
- Critical: N (N actionable)
- Major: N (N actionable)
- Minor: N
- Info: N

## Critical — Actionable
1. [CRT-001] **{title}** in `{file}:{line}`
   Found by: {agent name}
   Practical: {HIGH|MEDIUM|LOW} — {why}
   {code context}
   → {fix suggestion}

### Major — Actionable
...

### Minor
...

### Informational (non-actionable)
...
```

Log if inside recipe:
```bash
bts recipe log {id} --action review --output review.md --result "N critical, N major (N actionable)"
```

Review is a **report**, not a gate. Findings are recommendations.
