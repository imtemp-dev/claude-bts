---
name: forge-recipe-analyze
description: >
  Analyze an existing system or codebase. Produces a verified Level 1
  (understanding) document.
user-invocable: true
allowed-tools: Read Write Edit Grep Glob Bash Agent mcp__context7__resolve-library-id mcp__context7__get-library-docs
argument-hint: "\"what to analyze\""
---

# Recipe: Analyze (Level 1 Understanding)

Analyze: $ARGUMENTS

## Resume Check

Before starting, check for an existing recipe:
```bash
forge recipe status
```
If no active recipe, create one:
```bash
forge recipe create --type analyze --topic "$ARGUMENTS"
```
Use the output as recipe ID for all subsequent commands.

If active:
- Phase `research` → read existing research doc, continue from Step 2.
- Phase `verify` → read draft, run /forge-assess to determine next action.
- Phase `finalize` → skip to Step 4.

## Step 1: Research
Use Skill("forge-research") to explore the target.
Save to `.forge/state/{id}/research/v1.md`.

## Step 2: Draft Analysis Document
Write a structured analysis:
- Architecture overview
- Key components and their roles
- Data model / schema
- Dependencies and integration points
- Patterns and conventions used

Save to `.forge/state/{id}/draft.md`.

## Step 3: Verify Loop (max 3 iterations)
- Skill("forge-cross-check"): file/function references correct?
- Skill("forge-verify"): logical consistency?
- Skill("forge-audit"): anything missing?
- Fix issues, re-verify until critical=0, major=0.

Max 3 iterations. If same issues persist → [CONVERGENCE FAILED],
report findings and ask user for guidance.

Log each iteration:
```bash
forge recipe log {id} --iteration N --critical X --major Y --minor Z
```

## Step 4: Finalize
1. Copy `draft.md` to `final.md`.
2. Run Skill("forge-status") with arguments: {id}
3. Output `<forge>DONE</forge>`.

## Next Steps

After analysis is complete:
- To design a solution: `/recipe design "topic"`
- To create an implementation spec: `/recipe blueprint "topic"`

The analysis final.md provides foundation for subsequent recipes.
