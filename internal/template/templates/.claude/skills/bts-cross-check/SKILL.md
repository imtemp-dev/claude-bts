---
name: bts-cross-check
description: >
  Check internal consistency of a document — terms used consistently,
  interfaces match between sections, no contradictions. Optionally checks
  against source code if it exists.
user-invocable: true
allowed-tools: Read Grep Glob Bash Agent
argument-hint: "[file-path]"
context: fork
model: sonnet
---

# Internal Consistency Check

Check the document for internal contradictions and inconsistencies.

## Steps

1. Run consistency check via bts binary:
   ```bash
   bts verify $ARGUMENTS
   ```
   For from-scratch specs (no existing code), add `--no-code`:
   ```bash
   bts verify --no-code $ARGUMENTS
   ```

2. Read the document yourself and check for internal contradictions.
   The document should be consistent throughout — the same concept should use
   the same name, interfaces should match where they connect, types should agree,
   data flows should be coherent, and assumptions should not conflict.
   Look for any place where one section says something that contradicts or
   is incompatible with another section.

   If the document contains mermaid diagrams, verify that:
   - State/node names in diagrams match the terms used in text
   - Data flow directions in diagrams match the described behavior
   - Interfaces shown as edges match the function signatures in text

3. Classify findings:
   - critical: Same entity described differently in incompatible ways
   - major: Inconsistent naming or interface that would cause implementation errors
   - minor: Slightly different terminology but meaning is clear

4. Report findings with severity counts.
