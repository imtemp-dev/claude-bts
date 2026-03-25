---
name: forge-audit
description: >
  Audit a document for completeness. Find missing scenarios, unconsidered
  edge cases, and hidden assumptions. Includes mermaid branch completeness
  analysis. Use after verify and cross-check.
user-invocable: true
allowed-tools: Read Grep Glob Agent
argument-hint: "[file-path]"
context: fork
---

# Completeness Audit

Audit the specified document for missing items.

## Settings

Audit requires finding what's missing — it uses the main session model by default.
If `agents.auditor` is explicitly set in `.forge/config/settings.yaml`, use that model instead.

## Steps

1. Read the target document fully
2. Spawn Agent(auditor) with the following prompt:

   ```
   You are a completeness audit specialist. Read the document at $ARGUMENTS.

   Your goal: find everything the document fails to address that could cause
   problems at runtime, during deployment, or under adversarial conditions.

   **Content completeness:**
   Think about failure modes, boundary conditions, unstated assumptions,
   missing integrations, security gaps, and operational concerns. Do not
   limit yourself to a fixed checklist — reason about what this specific
   system needs and what the document leaves unanswered.

   **Flow completeness (mermaid diagrams):**
   If the document contains mermaid diagrams:
   - At EVERY decision node: are ALL branches specified? (yes/no/error/timeout)
   - At EVERY state: what happens on timeout? invalid input? resource exhaustion?
     concurrent access? If unspecified, flag as a completeness gap.
   - Are there states that can only be reached through a single path?
     (fragile — what if that path fails?)
   - For each error state: is the error message/response defined? Is cleanup specified?
   - Count: total decision nodes, branches specified, branches missing.

   For each missing item, classify:
   - critical: Will cause runtime failure if not addressed
   - major: Important gap that should be filled before implementation
   - minor: Nice to have but not blocking

   Output findings as a numbered list with severity tags.
   Include: "Branch coverage: N/M decision branches specified (N%)."
   ```

3. Collect the auditor's findings
4. Report results with severity counts
