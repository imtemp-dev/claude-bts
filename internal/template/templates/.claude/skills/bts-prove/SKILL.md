---
name: bts-prove
description: >
  Drive the user journeys a change touches through the real app, with the
  project's verify-<app> skill, and record each run with its evidence
  (bts prove). Prove stage P2, run by /bts-implement Step 5.45.
user-invocable: true
allowed-tools: Read Write Edit Grep Glob Bash Skill AskUserQuestion
argument-hint: "[recipe-id]"
---

# Prove: drive the app the way a user does

Recipe: $ARGUMENTS (if empty, use the active recipe from `bts recipe status`).

Tests check the code; this checks the app.  Each journey runs through the
real entry point and leaves evidence a reviewer can open.

## Step 1: The verification skill

Look for `.claude/skills/verify-*/SKILL.md`.

- None: run `Skill("bts-verification-skill")` first.
- The change has nothing a user drives (an internal refactor, a library
  internal with no behaviour change): record it and stop.

```bash
bts prove plan {id} --no-surface "<why nothing a user drives changes>"
```

## Step 2: Plan the journeys

From the verify skill's `features/` map and the wireframe's File Structure
rows, name every user journey this change touches.  Add a feature file for
a journey the map lacks.

```bash
bts prove plan {id} --journey <name> [--journey <name>...] --feature features/<file>.md
```

Name the invariants each journey's run observes with `--inv INV-...`
(call plan once per journey when the lists differ).  An invariant whose
mutation edited only prompt text (a SKILL.md section, a knowledge file,
a prompt template) is guarded by the model: its test read the words, so
a journey must drive the model through it, on synthetic inputs, never a
real person's data.  `app_proven` names any such invariant no journey
observes.

```bash
bts prove plan {id} --journey <name> --inv INV-018,INV-019
```

## Step 3: Drive each journey

Write one drive command per journey, following the verify skill's Launch,
Drive, Evidence and Cleanup sections: it launches the app, drives the
journey, **exits non-zero when the end state is not what the journey
expects**, writes its evidence under `.verify/<journey>/`, and cleans up.

```bash
bts prove run {id} --journey <name> --cmd "<drive>" --verdict .verify/<name>/verdict.json --evidence .verify/<name>/<file>
```

The drive writes its verdict on every run, failing ones included: each
check it made and whether it passed,
`{"checks": [{"check": "<what it compared>", "pass": true|false, ...}]}`.
A run counts only when it exits 0, its verdict was written by this run
with at least one check and no failed one, and every `--evidence` file
exists and is not empty.  The exit code alone proves nothing: a drive
that always exits 0 would pass.  When it fails, the app is wrong or the drive is: fix the one
that is wrong; never weaken the drive to make it pass.

## Step 4: Defer what cannot run here

A journey that needs something this machine cannot provide (an installed
platform app, real credentials, a production-only target) is deferred with
the reason and the owner who runs it on the real target:

```bash
bts prove defer {id} --journey <name> --reason "<what it needs>" --owner "<who runs it, where>"
```

Deferrals are listed at completion and never count as passes.  Defer only
what truly cannot run here; drive the rest.

## Done

`bts prove status {id}` shows every journey driven or deferred.  A later
code change makes a drive stale: rerun its `bts prove run` before
completion.  The stop hook checks the same at IMPLEMENT DONE
(`app_proven`).
