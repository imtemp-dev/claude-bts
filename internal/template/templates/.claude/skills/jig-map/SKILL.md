---
name: jig-map
description: >
  Analyze an existing system or codebase. Produces a verified Level 1
  (understanding) document.
user-invocable: true
allowed-tools: Read Write Edit Grep Glob Bash Agent mcp__context7__resolve-library-id mcp__context7__get-library-docs
argument-hint: "\"what to analyze\""
---

# Recipe: Map (Level 1 Understanding)

Analyze: $ARGUMENTS

## Resume Check

Before starting, check for an existing recipe:
```bash
jig recipe status
```
If no active recipe, create one:
```bash
jig recipe create --type map --topic "$ARGUMENTS"
```
Use the output as recipe ID for all subsequent commands.

If active:
- Phase `research` → read existing research doc, continue from Step 2.
- Phase `verify` → read draft, run /jig-assess, then **immediately execute** the recommended action.
- Phase `finalize` → skip to Step 4.

**Autonomous execution**: This recipe runs without stopping between steps.
Do NOT pause to summarize or ask the user. Only stop for [CONVERGENCE FAILED].

## Step 1: Research

Read existing project context if available:
- `.jig/specs/project-map.md` — layer overview, build/test commands
- `.jig/specs/layers/{name}.md` — detail for layers relevant to this analysis

Use Skill("jig-research") to explore the target.
Save to `.jig/specs/recipes/{id}/research/v1.md`.

## Step 2: Draft Analysis Document
Write a structured analysis:
- Architecture overview
- Key components and their roles
- Data model / schema
- Dependencies and integration points
- Patterns and conventions used

Save to `.jig/specs/recipes/{id}/draft.md`.

## Step 3: Verify Loop (max `verify.max_iterations`, default 3)
- Skill("jig-cross-check"): file/function references correct?
- Skill("jig-verify"): logical consistency?
- Skill("jig-audit"): anything missing?
- Fix issues, re-verify until critical=0, major=0.

After each skill completes, immediately proceed to the next check.
When all checks pass (critical=0, major=0), continue directly to Step 4.
If issues found, fix them and re-run the loop — do NOT stop to report.

The convergence budget (`verify.max_iterations`, default: 3) is enforced
by `jig recipe log`: after that many consecutive rounds without progress
it exits non-zero with `[CONVERGENCE FAILED]` and names the stagnant
finding IDs. Stop there and ask the user — do not start another fix
cycle. See `jig-verification-protocol.md § Convergence`.

Log each iteration:
```bash
jig recipe log {id} --from-verification .jig/specs/recipes/{id}/verification.md \
  --doc {verified-doc-path} --scope {full|delta} --dimension {verify|audit|simulate ...}
```
Iteration auto-increments. Fallback (no findings block): `--iteration N --critical X --major Y --minor-resolvable R --minor-deferred D`. Never use legacy `--minor` (it maps all minors to blocking [resolvable]).

## Step 4: Finalize
1. Copy `draft.md` to `final.md`.
2. Run Skill("jig-status") with arguments: {id}
3. Output `<jig>DONE</jig>`.

## Next Steps

After analysis is complete:
- To design a solution: `/jig-design "topic"`
- To create an implementation spec: `/jig-spec "topic"`

The analysis final.md provides foundation for subsequent recipes.
