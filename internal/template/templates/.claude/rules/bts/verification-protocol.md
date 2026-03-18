---
paths:
  - ".bts/**"
---

# BTS Verification Protocol

## Core Principle

Never verify your own output in the same context.

- **Internal consistency**: Checked by `bts verify` (deterministic) + Agent(verifier) (separate context)
- **Completeness**: Checked by Agent(auditor) (separate context)
- **Scenario coverage**: Checked by Agent(simulator) or /simulate (separate context)
- **Code references**: Checked by `bts verify` when code exists (deterministic, optional)

## Mandatory Verification Rule

**Every time a document is modified, /verify MUST run immediately after.**
This is non-negotiable. The recipe protocol enforces this.

## Severity Classification

- **critical**: Internal contradiction, undefined behavior in scenarios, impossible claims
- **major**: Missing error handling, incomplete data flow, unresolved design questions
- **minor**: Ambiguous wording, approximate descriptions, style issues
- **info**: Improvement suggestions, alternative approaches

## Convergence

- critical + major must reach 0 for Level 3
- max 3 iterations per improvement cycle
- If same issues persist for 2 iterations → change strategy or ask human
