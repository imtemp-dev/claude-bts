# Design: a recipe belongs to one session

Status: decided 2026-11-05 (both decisions as proposed); slices 1-3 built, slice 4 (the issue-owner skill) not started.  Covers AGEN-35 trial log #20 and #24, and #25, found while writing this.

## Problem

- **#20.**  bts hooks act on every Claude Code session whose working directory is the project.  On AGEN-35 the owner session (`/issue-owner`, same directory as the worker) was blocked by Rule B and by the implement-done gate for a recipe it was not working.
- **#24.**  Metrics attribute every session's events to the active recipe.  r-002's `metrics.jsonl` holds 612 events from the worker's session and 162 from the owner's (142 `tool_use`, 19 `subagent_stop`), so the recipe's tool and subagent counts are inflated by about a quarter.
- **#25.**  The PostToolUse hook reads the tool's result from the JSON key `tool_result` (`internal/hook/types.go:43`, `post_tool_use.go:51`).  None of r-002's 710 `tool_use` events carries an exit code or success flag, so the key never arrives.  It is the same class as the stop-marker bug (`2736cfd`): field names were never checked against a real Claude Code payload, and the tests build `HookInput` in Go.

## Facts

- The active recipe is project-wide: the first recipe directory whose phase is not `finalize`, `complete`, `cancelled` or empty (`state.GetActiveRecipe`, `internal/state/recipe.go:551`).  Nothing in it names a session.
- Every hook input carries `session_id` (Claude Code's common input fields).  Metrics events record it (`internal/metrics/metrics.go:28`); `phase_change` events carry none.
- In r-002, `subagent_start` and `subagent_stop` events carry the session id of the session that spawned them, so a subagent's events follow its parent.
- The worker kept one session id for 9 h 20 min, but it never compacted.  Whether `session_id` survives `/compact` and `claude --resume` is not established; Claude Code reruns SessionStart with `source` `compact`, `resume` or `fork` (`--fork-session`).
- The CLI never sees a session id.  Only hooks do.

## Design

### 1. The recipe records its owning session

`recipe.json` gains `owner_session`.  The PostToolUse hook binds it, because only a hook knows which session ran a command:

- after a successful Bash call of `bts recipe create`, the session that ran it owns the recipe it created;
- after `bts recipe claim <id>`, a new command, the session that ran it owns `<id>`, and the changelog records `claim` with the previous and new owner.

Rejected: binding the first session that starts.  On AGEN-35 the owner session started three minutes before the worker, so it would have taken the recipe.

### 2. What each session gets

| Hook | Owning session, or a recipe with no owner yet | Any other session |
| --- | --- | --- |
| Stop | the gates, as today | allowed; a tagged completion marker is refused with a note naming the owning session and `bts recipe claim` |
| PreToolUse | spec-phase warnings, fan-out budget | nothing, except a warning on a Write or Edit of a file the recipe's tasks.json or wireframe names (added for trial problem #56) |
| PostToolUse, SubagentStart/Stop metrics | `recipe_id` set | `recipe_id` empty and `observer: true`; the project metrics keep the event |
| SessionStart | resume hints | one line: the recipe is worked by another session, and how to claim it |
| PreCompact, SessionEnd | as today | nothing |

A recipe with no owner (every recipe created before this change) behaves as today, so nothing in flight breaks.

### 3. Handover

A worker that dies is replaced by a new session running `bts recipe claim <id>`.  `/compact` and `--resume` keep the session id (P-a), so they need no claim; a `--fork-session` copy is a new session and must claim.

### 4. Hook payloads from Claude Code itself (#25, the class fix)

- `BTS_HOOK_CAPTURE=<dir>` makes every `bts hook <event>` write its raw stdin to `<dir>/<event>-<n>.json` before parsing.  A developer runs one Claude Code session in a scratch project with it set.
- The captured payloads, with paths and ids scrubbed, become fixtures in `internal/hook/testdata/claude-code/`.  A contract test decodes each through `parseInput` and asserts that every field bts reads is present: `session_id`, `last_assistant_message`, `tool_input.command`, and the tool result's field.
- As built: `HookInput` reads `tool_response`, `error` and `is_interrupt`; `tool_result` is gone.  PostToolUse records `success: true`; a new PostToolUseFailure hook (template script and settings entry) records `success: false` and the exit code parsed from `error`.  Twelve scrubbed captures are the fixtures; a contract test checks by reflection that every `HookInput` field arrives in some capture.  Checked end to end in a project initialised by the new build: a passing and a failing Bash call recorded `success: true` and `success: false, exit_code: 1`.

## Probes (run 2026-11-05 with `BTS_HOOK_CAPTURE`, `claude -p` on Sonnet in a scratch project)

- **P-a, answered.**  `session_id` stays the same across `claude --resume` and across `/compact` (SessionStart arrives again with `source: compact` and the same id); `--fork-session` gives a new id.  So a binding made once survives compaction and resume, and §3's rebinding through the transcript path is not needed.
- **P-b, answered.**  The field is `tool_response`.  For Bash it holds `stdout`, `stderr`, `interrupted`, `isImage` and `noOutputExpected`, with no exit code.  PostToolUse fires only after a success; a command that exits non-zero fires **PostToolUseFailure** instead, with `error: "Exit code 1\n..."` and no `tool_response`; a permission denial fires neither.
- **P-c, answered.**  SubagentStart and SubagentStop carry the parent's `session_id`, plus `agent_id`; SubagentStop also carries `last_assistant_message`.
- Every other field `HookInput` reads is in at least one capture.  The only one never sent is `content`, the hand-piped fallback.

## As built (slices 2 and 3)

- `recipe.json` has `owner_session`; `bts recipe status` shows it.  The PostToolUse hook binds it after a successful `bts recipe create` (only while the recipe has no owner) or `bts recipe claim <id>` (always), and logs the changelog action `claim`.
- Another session gets: Stop allows, and a tagged marker is refused once with the owner named; no PreToolUse breadcrumbs, budget or spec-phase warnings, but a warning naming the owner and `bts recipe claim` when it writes one of the recipe's files (#56); SessionStart says the recipe is worked by another session; PreCompact and SessionEnd do nothing; tool and subagent events are `observer: true` with no `recipe_id`, so they stay out of the recipe's metrics.
- Checked end to end with two Claude Code sessions in a project initialised by this build: the creating session became the owner (`claim` logged); the second session's events were observers, its `<bts>DONE</bts>` was refused with the owner named, and the recipe's phase did not move.

## Tests

- PostToolUse of `bts recipe create` binds the running session; `bts recipe claim` rebinds and logs `claim`.
- In another session: Stop allows, a tagged marker is refused with the owner named, and PreToolUse adds nothing.
- Another session's metrics carry `observer: true` and no `recipe_id`.
- A recipe with no owner behaves exactly as today.
- The contract tests over the captured fixtures.

## Slices

1. `BTS_HOOK_CAPTURE`, the capture session, the fixtures and contract tests, and the #25 fix.  Answers P-a to P-c.
2. `owner_session`, binding on create, `bts recipe claim`.
3. Session roles in the hooks and in metrics.
4. The issue-owner skill: say that the worker owns the recipe it creates, and that the owner session may work in the same directory.

## Decisions

Taken by the operator on 2026-11-05, as proposed.

1. Another session's tagged completion marker is refused with a note naming the owning session.
2. Another session's metrics stay in the project metrics, flagged `observer`.
