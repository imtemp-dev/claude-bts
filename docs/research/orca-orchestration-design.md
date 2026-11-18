# bts on Orca orchestration: sessions bts starts and continues

Status: design, 2026-11-18.  Spike O0 done (results below); O1 not started.

## Goal

bts starts Claude Code sessions itself and keeps the work going across
them.  Three situations need it:

- A recipe crosses a phase boundary (spec DONE, then implement) or its
  session's context is nearly full, and the work should go on in a fresh
  session in the same worktree.
- A feature's items each run as their own recipe, in their own worktree,
  with someone supervising: answering questions, holding decisions,
  starting the next item when one finishes.
- A recipe wants a helper of another kind (a Codex adversary or reviewer)
  that reports back.

Today the second runs on Multica (`docs/research/multica-integration-design.md`).
Its workers are headless `claude -p` runs: no Artifact tool, so no review
page; no AskUserQuestion; the Stop hook ends the run, so DONE gates go
through `bts recipe finish`; background work is lost.  The first and third
have no mechanism at all: the user opens a new session by hand.

Orca (Stably AI, `com.stablyai.orca`, MIT, https://github.com/stablyai/orca)
runs coding agents in its own terminals and worktrees and ships an
orchestration layer: Runs, Tasks with dependencies, supervised workers,
messages that wake the coordinator, decision gates.  Its workers are
ordinary interactive Claude Code sessions.

## What the spike showed (O0, 2026-11-18, Orca 1.4.262, bts v0.30.0)

A scratch repository, `bts init`-ed and committed, was registered with
`orca repo add`.  Three Claude sessions ran on Sonnet 5.5: one launched
with a slash-command prompt, one coordinator, and one worker the
coordinator started in a child worktree.

| Question | Result |
|---|---|
| Does `orca agent launch --prompt "/bts-status"` run the skill? | Yes.  The session ran `/bts-status` and wrote `project-status.md`. |
| Do bts hooks run in an Orca session? | Yes, beside Orca's own user-level hooks (SessionStart, Pre/PostToolUse, Stop: both fired each time).  Orca's Stop hook answers `{}`, so bts's Stop gate is unaffected. |
| Do they run in a child worktree, where `.claude/settings.local.json` is absent (the user's global gitignore drops it)? | Yes.  Claude Code read the primary checkout's local settings and ran the primary checkout's hook scripts; bts resolved the worktree as its root and wrote `.bts/local/` there. |
| Does the worker have the Artifact tool and AskUserQuestion? | Both yes, so review pages and questions work in a worker. |
| Does a worker's `orca orchestration ask` wake the coordinator, and the reply resume the worker? | Yes.  Orca types "You have 1 orchestration message. Run orca orchestration check --run <id>." into the coordinator's terminal, as a user message. |
| Does `worker_done` reach the coordinator? | Yes.  Once it came in the same turn as the reply (the `reply` receipt said to finish the mail); once as its own wake.  One extra notice arrived for mail already read. |
| Can a finished worker be continued? | Yes.  `orchestration send --to dispatch:<id>` gave the same worker, in the same worktree, a follow-up Task with a new dispatch id; it reported `worker_done` again. |

Seen but not settled:

- **Permissions.** The spike sessions showed "bypass permissions on": the
  user runs Orca's Claude agents with Orca's "full access" setting (their
  choice, 2026-11-18), which launches Claude Code with
  `--dangerously-skip-permissions`.  The worry was that the user's
  `permissions.ask` rules (on `git push`, `gh pr *`) would prompt no one
  there.  Checked in the O1 smoke (Claude Code 2.1.327, an Orca-launched
  session showing "bypass permissions on"): `git push --dry-run` stopped at
  "Rule Bash(git push *) from user settings says to ask", and a PreToolUse
  hook's `ask` decision also stopped a command no rule covered ("Hook
  PreToolUse:Bash requires confirmation").  Ask rules and hook asks both
  hold under bypassed permissions, so the user's own rules already guard
  pushes in Orca workers.
- **The coordinator must be an Orca agent.**  Waking is input typed into
  the coordinator's Orca terminal, so a session running elsewhere (a herdr
  pane) can start workers but is never woken.

## Claude Agent Teams and bts (spike O0b, 2026-11-18, Claude Code 2.1.327)

Two runs, one per teammate mode, each a lead plus two teammates on
Sonnet 5.5 in a fresh `bts init`-ed project: in-process
(`CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS=1 claude --teammate-mode
in-process`) and split panes (`orca claude-teams`, teammates as Orca
splits).  The lead created a build recipe; teammate `prober` ran `bts
recipe status`, logged an action to the lead's recipe, wrote a stub
`verification.md` into it and ended with `<bts>DONE</bts>`; teammate
`racer` created a second recipe.  Every bts hook payload of the lead was
captured with `BTS_HOOK_CAPTURE`.  Headless `claude -p` spawns no
teammates (Claude Code's docs), so neither mode can run headless.

| | In-process teammates | Split-pane teammates |
|---|---|---|
| Session seen by hooks | the lead's `session_id`, with `agent_id` and `agent_type` (the teammate's name) on tool events; a teammate's turn end is a SubagentStop, never a Stop | a session of its own |
| What bts makes of them | the recipe's owner: the owner's PreToolUse notices, tool calls counted to the owner | observers: SessionStart says another session works the recipe and no gates apply |
| A teammate's `<bts>DONE</bts>` | arrives in SubagentStop's `last_assistant_message`, which bts does not read: neither applied nor refused | refused by the Stop hook, naming the owner and `bts recipe claim` |
| A teammate's stub `verification.md` | blocked the lead's Stop twice | blocked the lead's Stop five times: each time the lead ended a turn to wait for the teammate |
| A teammate's `bts recipe log` on the lead's recipe | accepted (the CLI sees no session) | accepted |
| A teammate's `bts recipe create` | a second recipe every hook ignores: the active recipe is the first directory, so the new one runs with no gates for anyone | same |

Concurrent writes, checked from the shell without agents: 60 appends from
four writers all landed in `changelog.jsonl` with none torn; 20 parallel
`bts task status` calls on different tasks lost 1 to 5 updates in each of
four runs (`tasks.json` is read, changed and replaced, with no lock); nine
verify rounds from three writers got iterations 1,1,1,2,2,2,3,3,3.

Bounds:

- **Works today:** the lead owns the recipe and is the only writer of bts
  state; teammates research, review or edit files no other teammate edits,
  and report by message.  Either mode.  Teammates cannot complete the
  recipe (refused in panes, ignored in-process).
- **Works with care:** teammates edit disjoint product files during
  implementation while the lead records the tasks one at a time; the
  lead's gates still hold at its Stop.
- **Does not work today:** a teammate that runs its own recipe in the
  lead's worktree (its recipe is invisible to the hooks); teammates that
  write recipe records at once (lost task updates, duplicate iterations);
  a teammate that produces a verification the lead logs later (the lead's
  every wait turn is refused, the class of bts #168 = AGEN-240).
- So: Agent Teams for fan-out inside one recipe; Orca workers, each in its
  own worktree and session, for anything that runs a recipe of its own.

Fixes these point at, none built: the Stop hook leaves an unlogged
verification alone while a teammate of the session is alive (as
`VerifyInFlight` does for a fork); SubagentStop refuses a teammate's
completion marker instead of dropping it; hooks resolve the recipe the
session owns before the project-wide first active one; a per-recipe lock
around read-change-replace writes and verify iteration numbering.

## Orca primitives this uses

| Need | Orca |
|---|---|
| A fresh session in an existing worktree | `orca agent launch --agent claude --worktree <selector> --prompt "<text>" [--model <id>] --json` |
| A new worktree with a session | `orca worktree create --name <n> --agent claude --prompt "<text>"` |
| A supervised worker | `orca orchestration worker-start (--task <id> \| --spec-file <f>) --worktree current\|new-child --agent claude --json` |
| Work with dependencies | `run-create`, `task-create --deps '[...]'`, `task-list --ready` |
| Worker asks, coordinator answers | `ask` / `reply --id <msg>` |
| Continue a finished worker | `send --to dispatch:<id>` |
| A decision blocking a task | `gate-create --task <id> --question ... --options '[...]'`, `gate-resolve` |
| Read a session | `orca agent read <session-id>`, `orchestration worker-read` |
| Clean up | `worker-release`, `terminal close --worktree <sel> --all`, `worktree rm --force --delete-branch`, `repo hide` |
| Per-worktree setup | `orca.yaml` `scripts.setup` (with `setupAgentStartupPolicy: wait-for-setup`) and `.worktreeinclude` for ignored files |

Not used: `orca claude-teams [claude args]` starts Claude Code's Agent
Teams in the current Orca terminal and opens each teammate as an Orca
split.  Teammates share the lead's session and worktree and end with it,
so they serve fan-out inside one session, not work that has to outlive
it; Orca orchestration covers that.

The CLI's command schema is `orca agent-context --json` (schema v1, 338
commands); `orca skills get orchestration` prints the guide matched to the
installed binary.  `orca` is not on the user's login `PATH` outside Orca
terminals; the binary is `/Applications/Orca.app/Contents/Resources/bin/orca`.

## Design

### Principle

bts keeps the state and the gates; Orca keeps the sessions, terminals,
worktrees and mail.  bts's records already survive a change of session (a
new session resumes from `bts recipe status`), so a continuation needs no
transcript, only a prompt naming the skill and the recipe.  bts never types
into a session's terminal: it launches, starts workers and sends mail
through Orca.

### Three moves

1. **Continue.**  A fresh session in the same worktree takes the recipe
   over.  `bts orca continue <id> [--skill <skill>]` builds the prompt
   (`/bts-implement <id>`, or the recipe's own skill to resume), launches
   it with `orca agent launch --worktree current`, records the launch in
   `recipe.json` and prints what it started.  The old session ends its turn
   and is not used again; `bts recipe claim` moves ownership as today.
   Offered at:
   - spec DONE, before `/bts-implement` (build, blueprint, debug);
   - PreCompact, when the context is about to be compacted mid-recipe;
   - any time the user asks.
2. **Delegate.**  A feature's items run as Orca workers.
   - feature ↔ Run; item ↔ Task, with the plan's dependencies as `--deps`;
     an item's recipe ↔ a worker started with `--task <id> --worktree
     new-child --agent claude`;
   - the worker runs the item's recipe skill interactively: the Stop hook,
     AskUserQuestion, review pages and subagents work as in any session;
   - a question the confirmed intent and contracts answer goes to the
     coordinator with `ask`; one only the user can answer is a bts
     decision, held as today, and mirrored as an Orca gate so it shows on
     Orca's board;
   - `worker_done` wakes the coordinator, which runs `bts feature next`
     and starts the Tasks `task-list --ready` shows;
   - a finished item is continued with `send` (a review fix, a restack);
   - draft PRs still go through `bts feature pr`.
   `bts feature dispatch <f> --via orca` does the Orca side; Multica stays
   as the other backend.
3. **Helper.**  A recipe step that wants another agent (an interrogate
   adversary or a reviewer in Codex) starts a worker with `--worktree
   current --agent codex` and reads its `worker_done`.  Later; it needs the
   step's own contract for what the helper returns.

### Who coordinates

The user's own Claude Code session, running in Orca, coordinates a
feature: that is Orca's model (the session that starts workers is the one
woken), and it keeps the user in the loop without a separate manager
agent.  A dedicated manager session stays possible later.

### The bts command layer

Skills never call `orca` directly.  `bts orca` wraps it, as `bts feature
pr` wraps push:

- finds the binary (`ORCA_CLI_COMMAND`, then `orca` on `PATH`, then the app
  bundle), checks `orca status --json` and the CLI version;
- passes `--json` and reads the receipts;
- records run, task, dispatch and session ids in bts state (`recipe.json`
  for a continuation, the feature store for an item), so `bts recipe
  status`, `bts doctor` and `bts feature status` can show them;
- reuses an Orca request id on retry, as the CLI asks, instead of
  launching twice.

### Launch policy

A launch is a new session the user pays for and sees.  Setting
`orca.launch: ask` (default) has bts print the command and ask before it
runs it; `auto` launches at the points above without asking.  `off`
disables the integration.

### Worktree setup

Hooks reach a child worktree through the primary checkout's local settings
(spike).  What a worktree still lacks is the project's own setup (a venv,
`node_modules`): the project's `orca.yaml` `scripts.setup` does it, with
`setupAgentStartupPolicy: wait-for-setup` so the agent starts after setup.
bts can propose that block when it sees Orca, but the file is the
project's.

## Slices

| Slice | Content |
|---|---|
| O0 | Spike (done, above). |
| O1 | Continue: `bts orca continue`, the `orca.launch` setting, the offers at spec DONE (the Stop hook's notice) and after a compaction (SessionStart, since PreCompact cannot add context), the launch recorded as a `continue` changelog entry. |
| O2 | Delegate: `bts feature dispatch --via orca`, Run and Tasks from the plan, worker skill text for an Orca worker, gates mirrored from decisions, `bts feature next` on `worker_done`. |
| O3 | Helpers in another agent, starting with one interrogate adversary in Codex. |
| O4 | Optional Orca plugin: a panel and status bar item showing each workspace's recipe and open decisions (`docs`: plugin manifest v2, `trusted-worker`). |

## Risks and open questions

- Full access in Orca-launched sessions (above): the user's ask rules hold
  there in Claude Code 2.1.327.  A later Claude Code that stops honouring
  them under bypassed permissions would reopen the question; a PreToolUse
  `ask` from bts is the fallback, and it was seen to work too.
- Orca must be running; `orca status --json` says whether it is.  A
  `runtime_unavailable` error is transient by Orca's own guide.
- Waking types into the coordinator's terminal; a duplicate notice can
  arrive.  The coordinator's `check` is the source of truth, never the
  notice.
- Plugins and parts of the orchestration surface are Alpha.  The CLI
  publishes a versioned schema and a version-matched guide; `bts orca`
  should read them rather than hard-code flags beyond the stable ones.
- SSH and WSL workspaces are out of scope until a local flow works.
- Cost: every continuation and worker is a full session.  `ask` before
  launch is the default for that reason.

## Decisions (2026-11-18)

The user named the orchestrator, "sessions bts starts and continues", as
the integration that matters most, and took every recommendation: spike,
then Continue, then Delegate; the user's own Orca session coordinates; the
Multica backend stays; bts asks before launching, with `auto` as a setting.
