---
name: bts-recipe-design
description: >
  Design a feature or system. Produces a verified Level 2 (design) document.
  Can be followed by /recipe blueprint to reach Level 3.
user-invocable: true
allowed-tools: Read Write Edit Grep Glob Bash Agent mcp__context7__resolve-library-id mcp__context7__get-library-docs
argument-hint: "\"what to design\""
---

# Recipe: Design (Level 2 Design)

Design: $ARGUMENTS

## Step 1: Research
Use Skill("bts-research") to understand the current state.
Save to `.bts/state/{id}/01-research.md`.

## Step 2: Draft Design Document
Write a design spec:
- Problem statement and goals
- Proposed solution architecture
- Component breakdown
- Data flow (how data moves through the system)
- API contracts (if applicable)
- Technology choices with rationale

Save to `.bts/state/{id}/02-draft.md`.

## Step 3: Verify Loop (max 3 iterations)
- Skill("bts-cross-check"): referenced code/systems exist?
- Skill("bts-verify"): design is logically consistent?
- Skill("bts-audit"): missing considerations?
- Fix issues, re-verify until critical=0, major=0.

Log each iteration:
```bash
bts recipe log {id} --iteration N --critical X --major Y --minor Z
```

## Step 4: Decision (if needed)
If uncertain choices exist, use Skill("bts-debate").
Update design, re-verify.

## Step 5: Finalize
Copy to `.bts/state/{id}/final.md`.
Output `<bts>DONE</bts>`.
