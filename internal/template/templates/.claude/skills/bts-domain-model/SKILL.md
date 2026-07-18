---
name: bts-domain-model
description: >
  Model the problem domain BEFORE structural decomposition. Defines entities,
  invariants (each with a single owner), state partitioning
  (transient/persistent/derived), and enumerates legal/illegal combinatorial
  state cells. Produces domain.md as the contract /bts-wireframe and
  /bts-architect must honor.
user-invocable: true
allowed-tools: Read Write Edit Grep Glob Bash Agent AskUserQuestion
argument-hint: "[recipe-id]"
effort: max
---

# Domain Model

Design the problem's conceptual model **before** drawing modules.

This skill exists because verify/audit/simulate all check "is the drawn
decomposition internally consistent" — none of them ask "is this the
*right* decomposition?". A bad decomposition (e.g., duplicating truth
across Card/DropZone/Game state) passes every downstream check while
producing patch-of-patches bugs in implementation. domain.md fixes the
boundaries BEFORE that drift can start.

## Resume Check

```bash
bts recipe status
```

- Phase already `domain-model` and `domain.md` exists → read it, continue
  refining if sections are incomplete; else advance to `/bts-architect`
  (or straight to `/bts-wireframe` if wireframe.md already carries an
  `<!-- architect-decision -->` block).
- Phase is `research` or later without `domain.md` → start from Step 1.
- No active recipe → fail: this skill runs inside an active blueprint
  or design recipe only.

## Prerequisites

1. Read `.bts/specs/recipes/{id}/scope.md` — scope MUST be Status: CONFIRMED
2. Read `.bts/specs/recipes/{id}/research/v1.md` (or latest research doc)
3. Read `.bts/specs/recipes/{id}/intent.md` for the Success Criteria
4. Read `.bts/specs/project-map.md` for existing layer structure

## Output: `.bts/specs/recipes/{id}/domain.md`

Emit exactly these five sections. All are required for the Quality Gate.

### 1. Entities

The stable nouns of the domain. One row per entity. Entities are
**concepts**, not files or modules — those come later in /bts-wireframe.

| Entity | Definition (one sentence, no "and") | Identity | Lifecycle |
|--------|-------------------------------------|----------|-----------|
| Arrangement | Ordered mapping of Words to Slots | arrangement_id | persists across drags, cleared on new exercise |
| Word | A token the user sorts | word_id (stable) | created once per exercise |
| Slot | A position the Arrangement exposes | slot_index | created once per exercise |
| DragGesture | An in-flight user drag interaction | gesture_id | from pointerdown to pointerup |

Rule: if an entity's definition needs "and", it is probably two entities.
Split before continuing.

### 2. Invariants (SINGLE OWNER per invariant)

Facts that MUST be true at every observable moment. Each invariant has
exactly ONE owner entity or module. If two owners seem plausible, the
decomposition is wrong — re-partition before completing this section.

| ID | Statement | Owner | Enforcement point |
|----|-----------|-------|-------------------|
| INV-001 | A Word occupies at most one Slot | Arrangement | Arrangement.place() |
| INV-002 | At most one DragGesture is active | DragGesture | DragGesture.begin() guard |
| INV-003 | Display order equals Arrangement order | Arrangement | derived, no independent store |

### 3. State Partitioning

Every piece of state falls into exactly one category:

- **Transient** — recomputed on every render/tick (cursor position,
  hover, scroll). MUST NOT be persisted or duplicated.
- **Persistent** — crosses time/request boundaries (DB row, file,
  localStorage). Has a single source-of-truth entity.
- **Derived** — computed from other state (sorted order from raw array,
  validation result from Arrangement). MUST NOT have independent
  storage. If cached for performance, label "cache" with its invalidation
  trigger.

| State | Category | Source-of-truth | Derivation formula (Derived only) |
|-------|----------|-----------------|-----------------------------------|
| Arrangement | Persistent | Arrangement entity | — |
| DragPosition | Transient | pointer event | — |
| IsValid | Derived | Arrangement | equals canonical order |
| AnimationFrame | Transient | raf timestamp | — |

Rule: if a Derived row lists an independent source-of-truth, that is
the bug Duolingo ran into. Re-classify or remove the independent store.

### 4. Combinatorial State Space

Cross-product of the state axes from Section 3. Enumerate every cell as
either `legal` or `ILLEGAL`. For every ILLEGAL cell, answer:

1. Which state transition would reach this cell?
2. What mechanism prevents that transition?
3. If nothing prevents it → mark CRITICAL — this is a spec gap.

Small example (two axes — real problems often have 3-4):

| drag.status × slot.state | empty | filled |
|--------------------------|-------|--------|
| idle                     | legal | legal |
| dragging                 | legal | legal (source slot temporarily empty for display) |
| dropping                 | legal | **ILLEGAL: double-fill** — prevented by Arrangement.place() atomic swap |

For 3+ axes, produce a flat table: one row per legal cell, one row per
illegal cell with "prevented by" or "CRITICAL" in the Notes column.

### 5. Invariant Test Matrix

For each invariant in Section 2, describe ONE scenario that WOULD
violate it if the enforcement mechanism were removed. This feeds
`/bts-simulate` Phase 6.2 — the illegal cells above must have
reachability scenarios.

| Invariant | Violating scenario | Enforcement checked |
|-----------|---------------------|---------------------|
| INV-001 | Drag word A to slot 2 while word B mid-snap-back to slot 2 | Arrangement.place() rejects second placement |
| INV-002 | Pointer down on word A while word B drag still in progress | DragGesture.begin() guard rejects second start |

## Quality Gate (before proceeding to /bts-wireframe)

Run through each check. Fail any one → fix in domain.md before exiting.

- [ ] Every entity row has a one-sentence, "and"-free definition
- [ ] Every entity row has a unique identity rule
- [ ] Every invariant has exactly ONE owner
- [ ] No invariant statement appears twice with different owners
- [ ] No Derived-row state has an independent source-of-truth
- [ ] Every ILLEGAL cell in Section 4 has "prevented by ..." OR is flagged CRITICAL
- [ ] Every invariant in Section 2 has a Section 5 violating-scenario row
- [ ] `bts verify .bts/specs/recipes/{id}/domain.md` returns 0 critical, 0 major

## Completion

```bash
bts recipe log {id} --phase domain-model --action domain-model \
  --output domain.md --based-on scope.md --doc-type domain \
  --result "N entities, M invariants, K illegal cells"
```

Then advance:

```bash
bts recipe log {id} --phase wireframe
```

> **Checkpoint**: domain.md saved, quality gate passed.
> Continue IMMEDIATELY to `/bts-architect` — do NOT stop to summarize.
> The architect debates ≥2 decompositions against these invariants and
> commits the winner as the `<!-- architect-decision -->` block;
> `/bts-wireframe` then draws the chosen decomposition.

## Rationale (why this phase exists)

Historical failure mode (Duolingo word-sort, 2026-04): Card, DropZone,
and Game each held their own copy of "where is each word now". Every
individual file passed /verify. Cross-axis interactions ("drag during
snap-back") produced bugs that took 3+ modules to fix, then broke
elsewhere. Root cause was decomposition, not any single function.

Fixing decomposition after implementation costs order-of-magnitude more
than fixing it in domain.md text. The invariant-owner-single rule and
Derived-no-storage rule, applied here, would have caught it before code.
