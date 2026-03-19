---
name: bts-recipe-analyze
description: >
  Analyze an existing system or codebase. Produces a verified Level 1
  (understanding) document.
user-invocable: true
allowed-tools: Read Write Edit Grep Glob Bash Agent mcp__context7__resolve-library-id mcp__context7__get-library-docs
argument-hint: "\"what to analyze\""
---

# Recipe: Analyze (Level 1 Understanding)

Analyze: $ARGUMENTS

## Step 1: Research
Use Skill("bts-research") to explore the target.
Save to `.bts/state/{id}/01-research.md`.

## Step 2: Draft Analysis Document
Write a structured analysis:
- Architecture overview
- Key components and their roles
- Data model / schema
- Dependencies and integration points
- Patterns and conventions used

Save to `.bts/state/{id}/02-draft.md`.

## Step 3: Verify Loop (max 3 iterations)
- Skill("bts-cross-check"): file/function references correct?
- Skill("bts-verify"): logical consistency?
- Skill("bts-audit"): anything missing?
- Fix issues, re-verify until critical=0, major=0.

Log each iteration:
```bash
bts recipe log {id} --iteration N --critical X --major Y --minor Z
```

## Step 4: Finalize
Copy to `.bts/state/{id}/final.md`.
Output `<bts>DONE</bts>`.
