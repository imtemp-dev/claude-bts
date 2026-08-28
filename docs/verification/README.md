# Verification records

A verification record is durable evidence that a **current guarantee** holds:
the date it was checked, the version it was checked against, the exact
commands run, and their exact output.

jig is a tool whose entire premise is "don't take the claim, check it." That
standard applies to jig's own claims too. Unit tests prove the code does what
the code says; a verification record proves the shipped system does what the
README says, including the parts no unit test reaches.

## What belongs here

- Evidence for a guarantee jig makes to its users — a gate that blocks, a
  state that survives a restart, a bypass that is actually closed.
- Anything verified by running the real binary end to end rather than by a
  Go test, especially the local-only checks CI cannot run.
- The exact command and the exact output. Not a summary, not "verified ✓".

## What does not belong here

- Task chronology, delivery transcripts, branch names, failed hypotheses.
  Those are PR evidence.
- Design rationale. That belongs with the mechanism it explains.
- Aspirations. A record describes what was observed, never what should
  happen.

## Format

Every record carries, at minimum:

```
Verification date: YYYY-MM-DD
Version:           <git describe, plus "working tree" if uncommitted>
Platform:          <os/arch, toolchain>
```

then one section per guarantee, each stating the guarantee in one sentence,
followed by the commands and their literal output.

## Maintaining a record

When behavior changes, re-run the commands and replace the output. A record
whose output no longer reproduces is worse than no record: it asserts a
guarantee that may already be broken. If a guarantee is retired, delete its
section rather than leaving stale evidence behind.

## Current records

- [completion-gates.md](completion-gates.md) — the gates that decide when a
  spec may finalize and when a turn may end.
