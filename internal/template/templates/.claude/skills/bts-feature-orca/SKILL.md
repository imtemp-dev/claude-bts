---
name: bts-feature-orca
description: >
  Coordinate a confirmed bts feature whose items run as Orca workers: from
  your own Claude session in an Orca terminal, start the ready items (each a
  recipe in its own child worktree), answer the workers' questions and
  confirmations, start what their finished work unblocks, and hand restacks
  to them after merges.  Use when the user wants a feature run on Orca.
user-invocable: true
allowed-tools: Read Grep Glob Bash AskUserQuestion
argument-hint: "<feature-id>"
effort: high
---

# Coordinate a feature on Orca

Feature: $ARGUMENTS

You coordinate; the workers build.  Each item is a recipe in its own child
worktree, worked by an Orca worker that `bts feature dispatch` started from
this session.  You never write an item's code, never push, and never open,
ready or merge a pull request: ready and merge are the user's.

## Before the first dispatch

1. `bts orca status` must say this session runs in an Orca terminal.  If it
   does not, tell the user to open this session in Orca and stop: Orca wakes
   only the session that started the workers.
2. `bts feature status <feature>`: the feature is confirmed.  If it has no
   confirmed plan, settle it with the user first: `bts feature create`,
   `bts feature item add`, `intent.md` and `contracts.md` written.  With
   `review.surface: artifact` (the default) or `local` the user reviews and
   confirms it on a page (/bts-review-page § Feature), recorded with
   `bts feature confirm <feature> --via review --hash <fingerprint>
   --answer "<their words>"`; otherwise ask, and record
   `bts feature confirm <feature> --answer "<their words>"`.
3. `bts feature dispatch <feature> --via orca` shows the workers that would
   start, each on its base branch.  Under `orca.launch: ask` (the default)
   ask the user once whether to run the feature on Orca; on their yes run it
   again with `--yes`, and pass `--yes` on this feature's later dispatches.
   Under `orca.launch: auto` it starts them at once.
4. End your turn.  Orca wakes you when a worker asks something or finishes.

## When Orca wakes you

Run the `check` command the wake names and handle the whole batch, then end
your turn.  Reply to a question with
`orca orchestration reply --id <message id> --body "..."`.

- **A scope or spec to confirm.**  Compare it with the feature's confirmed
  intent and contracts (`bts feature path <feature>`, then `intent.md` and
  `contracts.md` under `confirmed/<n>/`).  If every point serves the item as
  confirmed and breaks no contract, reply
  `Scope confirmed within confirmation <n>: <one line on why>` (or
  `Spec confirmed ...`): the worker records those words.  Otherwise put the
  point to the user here, and reply with their decision in their words.
- **Any other question.**  Answer it only when the confirmed intent and
  contracts answer it, citing them.  Otherwise ask the user here and reply
  with their words.
- **`worker_done`, succeeded.**  Run `bts feature status <feature>` and
  check the item's results against `contracts.md`: what it provides must
  keep the shape the contract gives.  A deviation that breaks a contract
  holds the items that rely on it; tell the user before dispatching further.
  Otherwise run `bts feature dispatch <feature> --via orca --yes`, which
  starts what the item unblocked.
- **`worker_done`, failed, or an escalation** (a worker stopped without
  reporting).  Read why (`orca orchestration worker-read --dispatch <id>`),
  then send the worker guidance
  (`orca orchestration send --to dispatch:<id> --subject "..." --body "..."`)
  or tell the user.  Release an item's attempt only on the user's word
  (`bts feature item release <feature> <item> --answer "<their words>"`);
  the next dispatch starts it again.

## Merges and the end

- Dispatch reads each draft pull request's state.  After the user merges
  one, run `bts feature dispatch <feature> --via orca --yes`: it records the
  merge, starts what waited for it, and asks a stacked item's worker to
  restack onto the base branch.
- When dispatch says every item is settled with its draft pull request,
  give the user one summary per item: what it delivered, its pull request,
  and anything held.  Ready and merge stay theirs.
- Items leave `.bts/specs/project-status.md` and `project-map.md` alone
  (each branch holds one recipe).  Once the items have merged, offer to run
  `/bts-status all` on the base branch to bring them up to date.
