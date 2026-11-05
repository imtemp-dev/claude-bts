---
name: bts-skeleton
description: >
  Write a build recipe's chosen decomposition as code that compiles: the
  files the wireframe's File Structure rows create or modify, their public
  types and signatures, bodies that fail loudly, and the callers' usage.
  Then record the typecheck or build with `bts check build`.
user-invocable: true
allowed-tools: Read Write Edit Grep Glob Bash
argument-hint: "[recipe-id]"
effort: high
---

# Skeleton

The skeleton is the contract in the language the code will be written in.
A compiler checks it in seconds; a prose description of the same shape
costs a verify round per question. It carries the shape, never the logic.

## Read

- `wireframe.md`: the File Structure rows (file, action, depends on,
  responsibility) and the `<!-- architect-decision -->` block.
- `domain.md`: § 1 entities, § 2 invariants and their owners, the illegal
  cells.
- `alternatives/{Selected}.md`: its Usage (caller's view) section.
- `scope.md`, `.bts/specs/project-map.md` when present, and the existing
  code next to each row, for the project's conventions.

## Write

For each row whose action is `create` or `modify`:

- **create:** the file with the module's public surface. Domain entities
  as types, written so an illegal cell cannot be represented where the
  language allows it. Public functions and methods with parameter and
  return types. The constants a contract names. Every body fails loudly
  and names itself:
  - Python `raise NotImplementedError("relay.handle")`
  - TypeScript `throw new Error("not implemented: relay.handle")`
  - Go `panic("not implemented: relay.Handle")`
  - Rust `todo!("relay::handle")`
  - Swift `fatalError("not implemented: Relay.handle")`
  - C# `throw new NotImplementedException("Relay.Handle")`
- **modify:** add only the symbols the row names (its `scope=` list, or
  what its responsibility requires). Leave existing bodies alone.  A new
  symbol that existing code already calls on every path (a setting read
  at startup, a parameter the hot path passes) gets an inert body
  instead of a loud one: an empty set, a no-op, a value that matches
  nothing.  A loud body there would break every existing test and make
  the new invariant's test fail for the wrong reason; an inert one keeps
  the old tests green and leaves the new test failing on its own
  assertion.

Each public symbol gets one line of doc comment: its contract, and the
invariant it owns (`Owns INV-003.`).

No logic, no private helpers, no test cases here. A row for test support
(a fake at a boundary, a fixture loader) is written like any other row:
its public surface, with bodies that fail loudly; the falsify step fills
what the tests need. Every symbol traces to a row and to an entity or
invariant; anything that does not is speculative and stays out (the
architect's YAGNI check applies here too).

## Usage

Write the selected alternative's Usage call sites as code that compiles
now: a test file or example module that imports the public surface and
calls it as a caller would. It fails when run, at the first body; it will
be the first thing to go green.

## Build

Use the project's own typecheck or build:

| Stack | Command |
| --- | --- |
| TypeScript | `npx tsc --noEmit` |
| Python with a checker | `mypy <pkg>` or `pyright <pkg>` |
| Python without one | `python -m compileall -q <pkg> && python -c "import <pkg>.<module>"` for each module |
| Go | `go build ./... && go vet ./...` |
| Rust | `cargo check` |
| .NET | `dotnet build` |
| Swift | `swift build` |

```bash
bts check build {id} --cmd "<command>"
```

Fix the skeleton until it passes. Never narrow the command (excluding
files, loosening the checker) to make it pass. `bts check build` records
the run in the changelog itself; log nothing else for this step (an
`--output` on `bts recipe log` would retype that document in the
manifest).

The signatures decided here bind implementation. Changing one later is a
deviation that sync records, and the falsifiers that call it must be
rerun.
