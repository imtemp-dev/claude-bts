---
name: jig-wireframe
description: >
  Design the high-level structure of a feature using mermaid diagrams.
  Creates wireframe.md with component diagram, state machine, data flow,
  file structure, and all execution paths enumerated.
user-invocable: true
allowed-tools: Read Write Edit Grep Glob Bash Agent
argument-hint: "[recipe-id]"
effort: xhigh
---

# Wireframe: System Structure Design

Design the high-level structure for: $ARGUMENTS

## Prerequisites

0. **Resolve recipe ID**: If `$ARGUMENTS` is empty, run `jig recipe status`
   to find the active recipe. Use its ID for all `{id}` references below.

1. Read research output:
   - `.jig/specs/recipes/{id}/research/v1.md` (or latest)
   - `.jig/specs/recipes/{id}/scope.md`
   - `.jig/specs/recipes/{id}/domain.md` — the invariant-ownership
     contract every component responsibility must honor
   - `.jig/specs/project-map.md` (existing system structure)
   - `.jig/specs/layers/{name}.md` for affected layers (existing models, APIs, patterns)

2. **Architect decision check**: wireframe.md should already START with
   the `<!-- architect-decision -->` block committed by `/jig-architect`.
   - Block present → design the SELECTED decomposition. Never overwrite
     the block: use Edit to add content BELOW it, not Write on the file.
   - Block missing → STOP and run `/jig-architect` first
     (`jig verify wireframe.md` raises missing_architect_decision_block
     as major otherwise).

## Step 1: Component Diagram

Identify the main modules/packages/files and their relationships.
Express as a mermaid flowchart:

```mermaid
flowchart TD
    A["module-a\n(responsibility)"] --> B["module-b\n(responsibility)"]
    A --> C["module-c\n(responsibility)"]
    B --> D["shared/types\n(data definitions)"]
    C --> D
```

Rules:
- Each node = one file or module with its primary responsibility.
- Each edge = a dependency or data flow.
- Include external dependencies (DB, API, filesystem) as distinct nodes.
- Show the direction of dependency (who imports whom).

**Responsibility rule (single-job gate):** Each node's responsibility
line MUST be a single sentence that does NOT contain "and"/"&"/"및"
as a conjunction. If you cannot express a module's job without a
conjunction, it has two jobs — split it into two nodes.

❌ `Card\n(renders view AND owns word placement)` → two jobs
✓  `Card\n(renders view)` + `Arrangement\n(owns word placement)`

The anti-pattern this rule catches is the one the Duolingo word-sort
hit: a "Card" node that secretly owned both UI rendering AND position
truth. Splitting earlier would have surfaced the invariant-ownership
conflict before implementation. `jig verify wireframe.md` reports each
violation as major (see engine/wireframe_responsibility_checker.go).

## Step 2: State Machine

Define ALL states the system can be in and ALL transitions between them.
This is the most critical diagram — it forces enumeration of every execution path.

```mermaid
stateDiagram-v2
    [*] --> State1: trigger
    State1 --> State2: action
    State2 --> State1: success
    State2 --> Error: failure
    Error --> State1: recovery
    Error --> [*]: fatal
```

Rules:
- **Every state must have at least one exit transition** (no dead ends)
- **Every state must be reachable** (no orphan states)
- **Error states must have recovery paths** unless explicitly terminal
- **Concurrent states** use parallel notation if applicable
- Include timeout transitions where relevant

## Step 3: Data Flow

Show how data moves through the system from input to output:

```mermaid
flowchart LR
    input["stdin/HTTP/event"] --> Parser --> Handler
    Handler --> Service["Business Logic"]
    Service --> Store["DB/File/Cache"]
    Store --> Service
    Service --> Handler
    Handler --> output["stdout/response"]
```

Rules:
- Show transformation at each step (what format changes)
- Include error response paths (not just happy path)
- Mark async vs sync flows

## Step 4: File Structure

List ALL files to create or modify with dependencies:

| File | Action | Depends On | Responsibility |
|------|--------|------------|----------------|
| `pkg/types.go` | create | — | Shared type definitions |
| `pkg/server.go` | create | types.go | Server lifecycle |
| `pkg/handler.go` | create | server.go, types.go | Request handling |
| `main.go` | modify | pkg/ | Entry point |

Rules:
- Order by dependency (roots first)
- Mark create vs modify
- This becomes the basis for task decomposition in /jig-implement

## Step 5: Execution Path Enumeration

**This is the key verification step.** List ALL execution paths from the
state machine. **Each path MUST carry a stable anchor** so draft.md can
reference it; the anchor lets `jig verify --check wireframe-anchors`
detect paths that exist in the wireframe but are never specified in
draft.md, and vice versa.

```markdown
### All Execution Paths

<!-- path-id: path-1 -->
1. **Happy path**: [*] → State1 → State2 → State1 (loop)
   - Triggers: normal request
   - Expected: process and return result

<!-- path-id: path-2 -->
2. **Error + recovery**: State2 → Error → State1
   - Triggers: processing failure
   - Expected: log error, clean up, return to ready state

<!-- path-id: path-3 -->
3. **Fatal error**: State2 → Error → [*]
   - Triggers: unrecoverable failure
   - Expected: log, notify, graceful shutdown

<!-- path-id: path-4 -->
4. **Timeout**: State2 → Error (after N seconds)
   - Triggers: no response within deadline
   - Expected: cancel operation, return timeout error

(continue until ALL paths are enumerated, each with a unique path-id)
```

Rules for path-id:
- Use lowercase kebab-case: `path-1`, `path-2`, … or semantic like
  `path-happy`, `path-timeout`.
- Each id MUST be unique within wireframe.md.
- Once assigned, path-ids are stable — renaming breaks draft references
  and forces re-verify.

For each path, verify:
- Is the trigger defined?
- Is the expected behavior described?
- Are side effects (logging, cleanup, notifications) specified?
- Does the path-id appear exactly once in wireframe.md?

draft.md will later reference these with
`<!-- path: wireframe.md#path-N -->` headers on the sections that
specify each path in detail. The anchor check enforces 1:1 coverage.

## Step 6: Save and Log

Save to `.jig/specs/recipes/{id}/wireframe.md` — via Edit, preserving
the `<!-- architect-decision -->` block at the top of the file.

Then run the mechanical gate:
```bash
jig verify .jig/specs/recipes/{id}/wireframe.md
```
This checks single-job responsibilities (no "and"/"&"/"및" conjunctions)
and architect-decision block presence. Fix all critical/major findings
before proceeding.

Log:
```bash
jig recipe log {id} --phase wireframe --action wireframe --output wireframe.md --result "N components, N states, N paths"
```

Register in manifest:
```bash
jig recipe log {id} --action wireframe --output wireframe.md --doc-type research --based-on scope.md
```

## Quality Gate

Before proceeding to draft, the wireframe must have:
- [ ] `<!-- architect-decision -->` block preserved at the top
- [ ] Component diagram with all modules and dependencies
- [ ] Component responsibilities match the architect decision's
      invariant-ownership mapping (domain.md § 2 owners)
- [ ] State machine with no dead-end states
- [ ] Data flow with error paths included
- [ ] File structure with dependency order
- [ ] ALL execution paths enumerated with triggers and expected behavior
- [ ] No orphan states (every state reachable)
- [ ] Every error state has a recovery or terminal path
- [ ] `jig verify .jig/specs/recipes/{id}/wireframe.md` returns
      0 critical, 0 major

> **Checkpoint**: Wireframe saved and quality gate passed.
> If executing as part of a recipe flow, continue IMMEDIATELY to the next step
> (writing the initial draft). Do NOT stop to summarize or ask the user.
