---
name: forge-recipe-design
description: >
  Design a feature or system. Produces a verified Level 2 (design) document.
  Can be followed by /recipe blueprint to reach Level 3.
user-invocable: true
allowed-tools: Read Write Edit Grep Glob Bash Agent mcp__context7__resolve-library-id mcp__context7__get-library-docs
argument-hint: "\"what to design\""
---

# Recipe: Design (Level 2 Design)

Design: $ARGUMENTS

## Resume Check

Before starting, check for an existing recipe:
```bash
forge recipe status
```
If no active recipe, create one:
```bash
forge recipe create --type design --topic "$ARGUMENTS"
```
Use the output as recipe ID for all subsequent commands.

If active:
- Phase `research` → read existing research doc, continue from Step 2.
- Phase `draft` → read draft, run /forge-assess to determine next action.
- Phase `debate` → read debate state, continue deliberation.
- Phase `verify` → read draft + verification, run /forge-assess.
- Phase `finalize` → skip to Step 5.

## Step 1: Research
Use Skill("forge-research") to understand the current state.
Save to `.forge/state/{id}/research/v1.md`.

## Step 2: Draft Design Document
Write a design spec:
- Problem statement and goals
- Proposed solution architecture
- Component breakdown
- Data flow (how data moves through the system)
- API contracts (if applicable)
- Technology choices with rationale

Save to `.forge/state/{id}/draft.md`.

## Step 3: Verify Loop (max 3 iterations)
- Skill("forge-cross-check"): referenced code/systems exist?
- Skill("forge-verify"): design is logically consistent?
- Skill("forge-audit"): missing considerations?
- Fix issues, re-verify until critical=0, major=0.

Max 3 iterations. If same issues persist → [CONVERGENCE FAILED],
report findings and ask user for guidance.

Log each iteration:
```bash
forge recipe log {id} --iteration N --critical X --major Y --minor Z
```

## Step 4: Decision (if needed)
If uncertain choices exist:
1. Use Skill("forge-debate") to deliberate
2. Run Skill("forge-adjudicate") to evaluate the conclusion
   - ACCEPT → update design → re-verify
   - ACCEPT WITH RESERVATIONS → update design + note caveats → re-verify
3. If debate reaches [DEBATE DEADLOCK] → present each position to user,
   user decides, run Skill("forge-adjudicate") on user's decision

## Step 5: Finalize
1. Copy `draft.md` to `final.md`.
2. Run Skill("forge-status") with arguments: {id}
3. Output `<forge>DONE</forge>`.

## Next Steps

After design is complete:
- To create a full implementation spec: `/recipe blueprint "topic"`
  The design final.md provides the Level 2 foundation for Level 3 spec.
- To implement directly (if design is detailed enough): `/forge-implement {id}`
