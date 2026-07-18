---
name: bts-architect
description: >
  Propose at least 2 alternative structural decompositions, debate them,
  adjudicate, and commit the winner to wireframe.md. Prevents the
  single-path trap where the first decomposition the model imagines
  becomes the only one considered.
user-invocable: true
allowed-tools: Read Write Edit Grep Glob Bash Agent AskUserQuestion WebSearch WebFetch mcp__context7__resolve-library-id mcp__context7__get-library-docs
argument-hint: "[recipe-id]"
effort: max
---

# Architect

Force the decomposition choice to be a **judgment between alternatives**,
not the first thing that comes to mind. One proposed decomposition is
a default, not a decision.

## Prerequisites

1. `domain.md` exists and passes `bts verify domain.md` (invariant
   ownership clean)
2. `scope.md` Status: CONFIRMED
3. `research/v1.md` exists AND contains an `## Official Guidance`
   section (per bts-research Step 3.5). If the section is missing,
   gather it NOW before proposing alternatives: identify the stack from
   scope.md / project-map.md and fetch the platform's officially
   recommended architecture per `.claude/rules/bts-evidence-policy.md`
   (Context7 → official docs → site-filtered search). Append the
   findings to the research doc with `Source:` lines. Do NOT propose
   decompositions from imagination while the vendor's own guidance
   sits unread.

If any prerequisite fails, STOP and point the user at the missing step.

## Step 1: Propose alternatives (min 2, max 4)

**Grounding rule**: when the platform has an officially recommended
architecture (research `## Official Guidance`), EXACTLY ONE alternative
MUST be that pattern applied to this feature's domain — carrying its
official `Source:` URL. It may lose the debate to a simpler
decomposition, but it must be on the table; rejecting it requires a
stated reason in the decision block's Rejected list.

For each alternative, produce:

- **Name** — short kebab-case identifier, e.g. `state-machine-centric`,
  `entity-per-file`, `pipeline`, `event-sourced`.
- **Basis** — one line: `official: {pattern name} for {framework}@{major}
  (Source: {URL})` for the vendor-guidance alternative, or
  `custom: {one-sentence rationale}` for the others. The official
  alternative must state which framework major the guidance applies to
  and it must match research's `Target versions:` line — a mismatch is
  not disqualifying but the debate must address it. Unsourced "official"
  claims are invalid — the adjudicator treats them as evidence-quality
  FAIL.
- **Node list** — files or modules with a ONE-SENTENCE responsibility
  (same "and"/"&"/"및" rule as bts-wireframe Step 1). Include file path
  hints so it is clear what the decomposition produces.
- **Invariant ownership mapping** — for every invariant in domain.md § 2,
  name the module that owns it in this alternative. Two invariants may
  share a module; the same invariant must NOT appear under two modules.
- **YAGNI check** — list every interface the alternative introduces.
  For each, either name ≥ 2 concrete implementations planned in this
  recipe, or justify why the abstraction earns its keep. Single-
  implementation interfaces without justification are a reject signal
  at Step 3.

Save each alternative as `alternatives/{name}.md` under the recipe
directory.

## Step 2: Debate

Run `Skill("bts-debate")` with topic:

> Which decomposition in `alternatives/` best fits the invariants in
> `domain.md` and the scope in `scope.md`? Evaluate against: invariant
> ownership clarity, blast radius of change, extension paths, YAGNI risk.

Use three personas:

- **Orthodox architect** — pattern rigor, layered separation, DIP.
- **Simplicity engineer** — YAGNI enforcement, smallest surface.
- **Domain expert** — invariant fit, protection of the core entities.

## Step 3: Adjudicate

Run `Skill("bts-adjudicate")`. Outcome possibilities:

- `ACCEPT` → proceed to Step 4.
- `ACCEPT WITH RESERVATIONS` → proceed to Step 4, record the caveats
  in the Rejected list as "partially-rejected notes".
- `EXTEND` → research the gap the adjudicator flagged, add a new
  alternative or revise an existing one, re-debate. Max 3 extensions.
- `[DEBATE DEADLOCK]` → surface to user for tiebreak; user's choice
  becomes the "conclusion", re-adjudicate for feasibility.

## Step 4: Commit decision to wireframe.md

Prepend this block to wireframe.md (before any mermaid). It must remain
at the top — `engine/consistency_checker.go` looks for the literal
opening tag `<!-- architect-decision -->` on a line by itself.

```
<!-- architect-decision -->
Selected: {alternative-name}
Basis: {official: {pattern} for {framework}@{major} (Source: {URL}) | custom: {rationale}}
Rationale: {one paragraph — why this decomposition fits the domain
invariants and scope better than the alternatives}
Rejected:
  - {alt-name-1}: {one-line reason — if this was the official-guidance
    alternative, the reason must address why the vendor pattern does
    not fit here}
  - {alt-name-2}: {one-line reason}
Invariant ownership:
  - INV-001: {module}
  - INV-002: {module}
  - …
<!-- /architect-decision -->
```

Update `manifest.json` with the decision name:

```bash
# Via bts CLI (future: dedicated flag). For now, edit manifest.json
# directly: set `architect_decision: "{alternative-name}"`.
```

## Step 5: Log and advance phase

```bash
bts recipe log {id} --phase architect --action architect \
  --output wireframe.md --based-on domain.md --doc-type architect-decision \
  --result "selected {name}; rejected N alternative(s)"
bts recipe log {id} --phase wireframe
```

> **Checkpoint**: architect decision committed. Continue IMMEDIATELY to
> `/bts-wireframe` — the component diagram now has a chosen
> decomposition and an invariant-ownership contract it must honor.

## Skip condition

For recipes with a very small scope (entity count ≤ 2 in domain.md §1
AND file estimate ≤ 3 in scope.md), the architect step can be skipped:

1. Write a minimal `<!-- architect-decision -->` block with
   `Selected: single-path`,
   `Basis: custom: scope too small to warrant alternatives`, and
   `Rejected: (none — scope too small to warrant alternatives)`.
   The `Basis:` line is required — `bts verify` checks for it.
2. Still record invariant ownership — skipping the debate does NOT
   skip the ownership contract (`bts verify` cross-checks the mapping
   against domain.md § 2).

This keeps wireframe's header-presence gate consistent while avoiding
unnecessary process overhead on trivial additions.

## Rationale

The Duolingo word-sort failure started with a single decomposition
("Card + DropZone + Game each hold state") that was never stress-tested
against alternatives. By the time the coupling showed up in
implementation, every fix touched 3 modules. Forcing ≥ 2 alternatives
at design time — and making one of them explicit about invariant
ownership — is cheaper than the implementation-time rework.
