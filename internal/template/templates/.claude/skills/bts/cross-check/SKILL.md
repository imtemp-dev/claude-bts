---
name: bts-cross-check
description: >
  Check internal consistency of a document — terms used consistently,
  interfaces match between sections, no contradictions. Optionally checks
  against source code if it exists.
user-invocable: true
allowed-tools: Read Grep Glob Bash Agent
argument-hint: "[file-path]"
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

2. Read the document yourself and check:
   - **Term consistency**: Is the same concept called the same name everywhere?
     (e.g., "session" vs "token" vs "auth state" for the same thing)
   - **Interface consistency**: If Section A defines `createUser(name, email)` but
     Section B calls `addUser(username)`, that's a mismatch.
   - **Type consistency**: If a field is `string` in one place and `number` in another.
   - **Flow consistency**: If the data flow says A→B→C but the component section
     describes C receiving from D.
   - **Assumption consistency**: If one section assumes Redis and another assumes PostgreSQL.

3. Classify findings:
   - critical: Same entity described differently in incompatible ways
   - major: Inconsistent naming or interface that would cause implementation errors
   - minor: Slightly different terminology but meaning is clear

4. Report findings with severity counts.
