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

## Resume Check

Before starting, check for an existing recipe:
```bash
bts recipe status
```
If no active recipe, create one:
```bash
bts recipe create --type design --topic "$ARGUMENTS"
```
Use the output as recipe ID for all subsequent commands.

If active:
- Phase `research` → read existing research doc, continue from Step 2.
- Phase `draft` → read draft, run /bts-assess, then **immediately execute** the recommended action.
- Phase `debate` → read debate state, continue deliberation.
- Phase `verify` → read draft + verification, run /bts-assess, then **immediately execute**.
- Phase `finalize` → skip to Step 5.

**Autonomous execution**: This recipe runs without stopping between steps.
Do NOT pause to summarize or ask the user. Only stop for [CONVERGENCE FAILED] or [DEBATE DEADLOCK].

## Step 1: Research

Read existing project context if available:
- `.bts/specs/project-map.md` — layer overview, build/test commands
- `.bts/specs/layers/{name}.md` — detail for layers relevant to this design

Use Skill("bts-research") to understand the current state.
Save to `.bts/specs/recipes/{id}/research/v1.md`.

## Step 2: Draft Design Document
Write a design spec:
- Problem statement and goals
- Proposed solution architecture
- Component breakdown
- Data flow (how data moves through the system)
- API contracts (if applicable)
- Technology choices with rationale

Save to `.bts/specs/recipes/{id}/draft.md`.

## Step 3: Verify Loop (max `verify.max_iterations`, default 3)
- Skill("bts-cross-check"): referenced code/systems exist?
- Skill("bts-verify"): design is logically consistent?
- Skill("bts-audit"): missing considerations?
- Fix issues, re-verify until critical=0, major=0.

After each skill completes, immediately proceed to the next check.
When all checks pass (critical=0, major=0), continue directly to Step 4.
If issues found, fix them and re-run the loop — do NOT stop to report.

The convergence budget (`verify.max_iterations`, default: 3) is enforced
by `bts recipe log`: after that many consecutive rounds without progress
it exits non-zero with `[CONVERGENCE FAILED]` and names the stagnant
finding IDs. Stop there and ask the user — do not start another fix
cycle. See `bts-verification-protocol.md § Convergence`.

Log each iteration:
```bash
bts recipe log {id} --from-verification .bts/specs/recipes/{id}/verification.md \
  --doc {verified-doc-path} --scope {full|delta} --dimension {verify|audit|simulate ...}
```
Iteration auto-increments. Fallback (no findings block): `--iteration N --critical X --major Y --minor-resolvable R --minor-deferred D`. Never use legacy `--minor` (it maps all minors to blocking [resolvable]).

## Step 4: Decision (if needed)
If uncertain choices exist:
1. Use Skill("bts-debate") to deliberate
2. Immediately after debate completes, run Skill("bts-adjudicate") to evaluate
   - ACCEPT → update design → re-verify → continue to Step 5
   - ACCEPT WITH RESERVATIONS → update design + note caveats → re-verify → continue to Step 5
3. If debate reaches [DEBATE DEADLOCK] → present each position to user,
   user decides, run Skill("bts-adjudicate") on user's decision

## Step 5: Finalize
1. Copy `draft.md` to `final.md`.
2. Run Skill("bts-status") with arguments: {id}
3. Output `<bts>DONE</bts>`.

## Next Steps

After design is complete:
- To create a full implementation spec: `/recipe blueprint "topic"`
  The design final.md provides the Level 2 foundation for Level 3 spec.
- To implement directly (if design is detailed enough): `/bts-implement {id}`
