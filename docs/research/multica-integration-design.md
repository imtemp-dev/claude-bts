# Design: bts on Multica

Status: direction agreed in discussion on 2026-11-11.  Smoke checks against the real instance, run before any code, are recorded below; they changed decisions 3, 5 and open question 2.  S0 is released as v0.20.0 (`a340e9a`) and passed end to end in a real run (AGEN-162): with only the `.bts-root` pointer in the workdir, the installed `bts`'s hooks recorded the resumed session's events in the worktree's metrics, bound `owner_session` on `bts recipe create`, and the phase reached the issue's metadata, with `git status` clean throughout.  (A run's hooks call the `bts` on the daemon's `PATH`, and Multica refuses `PATH` in an agent's custom environment, so this needed the installed release.)

S0 as built differs in one place: a decision hold does not set the issue to `blocked`.  The Multica runtime writes the issue's status at the end of every turn, so a status bts wrote would be overwritten; status moves with the manager in S2.  S0 mirrors `bts.recipe_id` and `bts.phase` only.  Multica here is the self-hosted instance the Desktop app runs (`localhost:8080`, workspace `agent-tron`), read against a local clone of `multica-ai/multica` at `b115ec80f`.

## Goal

Run bts recipes as Multica agents.  A feature that needs several recipes is coordinated by a manager agent on a parent issue, with each recipe on a sub-issue, in its own worktree, ending in its own pull request.  Multica is the board, the dispatcher and the place the user talks to agents.  bts stays the discipline inside each issue and the source of truth for recipe state.

## Principles

- **bts owns state; Multica shows it.**  Recipe and feature state live in bts files.  bts projects them onto Multica (status, metadata, comments).  Only the user's decisions flow back, and only as bts commands an agent runs, so the gates still decide.
- **Judgement in the model, transitions in the harness.**  The manager decides decomposition, contracts and escalations.  `bts feature` decides base refs, fork points, dispatch order, item state and restack commands.
- **Files, not the conversation.**  Every manager run starts from `bts feature status`.  Multica resumes sessions, but a resumed session is not the record.
- **One writer per file.**  A child writes its own recipe directory.  Only the manager writes the feature's files.

## Facts (verified 2026-11-11)

Multica:

- The daemon launches Claude with `-p --output-format stream-json --input-format stream-json --permission-mode bypassPermissions --disallowedTools AskUserQuestion` (`server/pkg/agent/claude.go:1078-1092`).  `AskUserQuestion` has been disabled since v0.3.1 (MUL-2244): in stream-json mode it returned an empty answer and the agent inferred silently.
- A later run of the same agent on the same issue resumes the Claude session: `PriorSessionID` and `PriorWorkDir` (`server/internal/daemon/types.go:111-112`).  bts's own probe P-a showed `--resume` keeps `session_id`, so `owner_session` survives across runs, and another agent on the issue is an observer.
- Task layout: `~/multica_workspaces_desktop-localhost-8080/<workspace>/<issue-key>-<hash>/{logs,multica-config,output,workdir}`.  Claude's working directory is `workdir/`.  The repository is a `git worktree` at `workdir/<Repo>/`, taken from a shared bare cache under `.repos/` by `multica repo checkout <url> [--ref <ref>]` (`server/internal/daemon/repocache/cache.go:1478`, `server/cmd/multica/cmd_repo.go`).  A branch-name collision gets a timestamp suffix.
- Multica writes `workdir/CLAUDE.md` (the run brief), `workdir/.claude/skills/` (agent skills, `server/internal/daemon/execenv/context.go:133`) and `workdir/.multica/daemon_task_context.json`, whose keys are `issue_id`, `agent_id` and `managed_by`.
- Task environment variables include `MULTICA_TASK_ID`, `MULTICA_WORKSPACE_ID`, `MULTICA_AGENT_ID`, `MULTICA_SERVER_URL` and `MULTICA_TOKEN`.  None carries the issue id; read it from `daemon_task_context.json`.
- GC removes a completed task's environment after `MULTICA_GC_TTL` (24 h for done or cancelled issues) and discards unpushed work.  A rerun starts from a fresh checkout (`CLI_AND_DAEMON.md:350-352`).
- Sub-issues take `--stage N`: ordered barrier groups.  The platform's `child_done` rule wakes the parent's assignee when every sub-issue in the lowest open stage closes (`server/cmd/multica/cmd_issue.go:633`, `server/internal/handler/issue_system_wakeup.go`).  `issue wakeup create --until-issue <id> --until-issue-state …` fires at once if the state already holds.
- The built-in squad leader protocol is fixed text: delegate by @mention on the same issue, "do not open files", no child issues (`server/internal/handler/squad_briefing.go`).
- Chat is a private one-to-one conversation with an agent, outside any issue.  It resumes the session across messages and creates an issue only when asked (`apps/docs/content/docs/chat.mdx`).  `multica attachment upload <path>` attaches a file to the current chat reply (`server/cmd/multica/cmd_attachment.go:36`).
- A project resource of type `local_directory` runs tasks in a local path, `in_place` (one run at a time) or `worktree` (`server/cmd/multica/cmd_project.go:151-157`).
- The Desktop app runs its own daemon (`multica daemon start --foreground --profile desktop-localhost-8080`).  The Homebrew CLI's default profile is a different daemon.  The app daemon's `PATH` includes `~/.local/bin`, so runs see the installed `bts`.

bts and the target repository:

- `FindRoot` walks up from the working directory for `.bts/` (`internal/state/manager.go:14`).  Git calls run as `git -C <root>`: `internal/state/tree_hash.go`, `internal/state/code_seen.go`, `internal/engine/code_hash.go`, `internal/engine/consistency_checker.go`, `internal/engine/modify_scope_diff.go`, `internal/cli/falsifier_mutate.go`.
- `bts init` writes hooks to `.claude/settings.local.json` (`internal/template/settings.go:52,83`) and appends `.bts/local/` to `.gitignore` (`internal/template/gitignore.go`).
- Carepatron-App tracks `.claude/settings.json`, `.gitignore`, `.mcp.json` and `CLAUDE.md`, and tracks no `.bts/` file.  Its master history is one squash commit per pull request.
- Recipe ids are `r-%03d-<slug>`, numbered per checkout (`internal/state/recipe.go:753`).
- The project roadmap is one file, `.bts/specs/roadmap.md`, read by `internal/state/roadmap.go`, `internal/cli/doctor.go` and `internal/cli/graph.go`, and updated by every recipe's `/bts-status`.

## Decisions

### 1. A feature level between project and recipe

```
project   shared knowledge: project-map.md, layers/, project-status.md (derived), product vision.md (optional)
└ feature intent, roadmap (items and depends_on), contracts       Multica parent issue, manager
  └ recipe as today                                                Multica sub-issue, worker agent
    └ task tasks.json                                              not on Multica
```

- A feature is a bts concept that works without Multica.  Multica's parent issue is its projection.
- The roadmap moves from the project to the feature.  The project keeps a derived list of features.
- A feature has an intent, not a product vision.  The product `vision.md` stays at project level, for greenfield work.
- Blueprint's size assessment still decides that a request needs several recipes; its output is a feature.
- A recipe with no feature (a fix, a lone build) works as today.  `recipe.json` gains `feature` and `feature_item`.
- Feature files live in a feature store outside the product worktrees, at a fixed local path, because Carepatron-App tracks no `.bts/` and task environments are deleted.

### 2. The manager

- A regular agent assigned to the parent issue, driven by a `/bts-feature` skill.  Not Multica's squad leader, whose protocol forbids opening files and child issues.
- Does: write the intent, items, dependencies and contracts, and get them confirmed; dispatch ready items as sub-issues with a recipe type and a stage; reconcile when a child finishes (did its deviation break a contract, which downstream items need revalidating); answer a child's question when the confirmed intent and contracts answer it; escalate the rest; produce the feature page; run the learning loop.
- Does not: write code or a child's recipe files; approve its own change to the intent or a contract; mark an item done (the child's recipe reaching `complete` does); mark the feature done (the user does, as Multica's own protocol leaves `done` to a person).
- Dependencies are layered into stages; items in one stage run in parallel.  Sub-issues of a later stage are created in `backlog`, so they do not start; the manager moves them out when the stage before closes.  This is the platform's own `child_done` default instruction (`server/internal/service/issue_wakeup_system.go:49`), and the instruction can be replaced per issue, so it can name `/bts-feature reconcile`.
- The manager wakes on a stage closing, on comment mentions, and on a child's failed or cancelled run: at dispatch it registers on each child `multica issue wakeup create <child> --event task.failed,task.cancelled --agent-id <manager>`.  A failed or cancelled run leaves the child's status unchanged, so its stage never closes and the stage barrier alone would never wake the manager.
- Session-ownership slice 4 (the issue-owner skill) folds into this skill.

### 3. Worktree and pull-request lifecycle

- One recipe is one sub-issue, one worktree, one branch and one pull request.
- Item states: planned → dispatched (base ref and fork point recorded) → branched (the child reports the real branch name) → recipe complete (artifacts harvested) → draft PR open → merged (squash commit recorded) → closed (dependents restacked).
- Base ref: the predecessor's branch while it is unmerged, `origin/master` once merged.  `bts feature` decides it from the dependencies and pull-request state.
- Stacked work with at most two unmerged levels.  After a base is squash-merged, `git rebase --onto origin/master <fork point> <branch>`; the fork point must be recorded at dispatch.  bts ties proof to the code hash, so a rebase invalidates earlier evidence by itself.
- `multica repo checkout --ref <base>` starts the worktree at the base's tip on a branch Multica names (`agent/<agent>/<hash>`).  The child renames it to the user's convention before the first push.
- Push at the end of every step, since GC discards unpushed work.
- The child opens its pull request as a draft without asking.  Marking it ready, requesting reviewers and merging stay with the user.
- Push and draft creation go through `bts feature pr <item>`, never a typed `git push` or `gh pr create`.  The user's global `permissions.ask` covers `git push *`, `git -C * push *` and `gh pr create|ready|merge|edit *`; a headless run cannot answer an ask, so typed commands are refused.  `bts feature pr` pushes only the item's own branch and runs only `gh pr create --draft` with the base `bts feature` chose.  It offers no ready, merge or other push, so the ask rules still guard everything else.  The user chose this over a separate Claude config directory for runs (which drops every user-level safeguard) and over pushing by hand.
- Knowledge reaches the next recipe through the feature store: at completion the child's `final.md`, `deviation.md`, `review.md`, a proof summary and the pull-request URL are harvested, and `bts feature context <item>` assembles them.  Code reaches it through git.  A sub-issue's description names only the feature, the item and the recipe type.

### 4. Intent and preview

- Intent is settled once per feature, before decomposition, in a Multica Chat with the manager.  Children inherit it and raise a decision hold only for UX the intent does not cover.
- `bts-discover` needs a chat mode: no `AskUserQuestion` (Multica disables it), numbered text options (`D2: A/B/C`), and `intent.md` saved after every turn so a lost session loses nothing.
- A preview step before decomposition, cheapest first: a clickable HTML prototype; a UI skeleton with mock data for interaction-heavy work; sample inputs and outputs (API exchanges, an agent conversation turn) when there is no UI.
- A prototype goes in the reply as a fenced `html` block, which Multica renders in place, in chat and in comments: a Preview/Source bar, fullscreen, a scripts-only sandbox where CDN scripts load, and the app's theme variables (`var(--background)`, `var(--primary)` …) for light and dark.  An attached file (`multica attachment upload`) shows as a card to open, so it is for files the user keeps.
- When the user says go, the manager creates the parent issue and the feature.
- No dedicated conversation tool until text answers are shown to be clumsy.  The next step would be an artifact whose page records choices, not a new application.

### 5. Review artifacts

- For the user's own understanding, not for the team: private, linked from the Multica issue and the feature page, never from a pull-request body.
- The child makes the PR page, in its own issue, when it opens the draft pull request.  The manager makes and updates the feature page.
- Every number comes from `bts feature snapshot --json`; the model writes only prose and diagrams.  Hand-copied counts drift between pages.
- The feature page opens with "Needs you": decision holds, drafts ready to mark ready, failed or stalled children, lessons awaiting approval.
- The style follows the user's recent pages (a feature roadmap page and single-PR pages): English; eyebrow, short title, a lede with numbers, a meta row; built kept apart from verified; one inline-SVG diagram of the real code path; behaviour tables; a "Checked" block; open items tied to a step or ticket.
- The generator is a user-level skill (`~/.claude/skills/`).  bts ships the snapshot, not the style.
- Republish from the snapshot at milestones (feature confirmed, draft opened, merged, feature in review).
- Inside Multica a page can be a fenced `html` block in a comment (rendered in place, collapsed past 480px behind "Show all").  That works from a headless run today, so it is the first target; a claude.ai artifact is optional, and whether a headless run can publish one is still unverified.

### 6. Learning loop

- Gather: at recipe completion the harvest also takes `metrics.jsonl`, `changelog.jsonl`, the findings ledger, overrides, decision holds, retries, `deviation.md`, the workdir's Claude memory and Multica run failures.  Today these die with the workdir: Claude Code memory is keyed by workdir path, one per issue.
- Route by kind, strongest enforcement first (the `/reflect` order: Go check, enforced setting, skill prose, docs): a bts process lesson becomes input to `/reflect`; a project or domain lesson goes to a project skill or rule, a Multica workspace skill or the wiki; an orchestration lesson goes to the manager skill or a settings threshold.
- Cadence: a cheap, mostly mechanical digest at recipe end; a retro at feature end; a weekly Multica autopilot that looks for recurrence across features.  A one-off slip is not a lesson.
- Nothing is applied until the user picks it, from a "Lessons proposed" section on the feature page.  An applied lesson is checked for recurrence and removed if it does not help.

## Where bts lives in a run

bts runs inside the worktree, as it does in the user's own worktrees: `~/Workspace/CU-…/.bts/`, kept out of git by a block in Carepatron-App's shared `.git/info/exclude` ("claude-bts (bts) project files: kept out of git in every worktree of this repo": `/.bts/`, `/.claude/{agents,hooks,rules,commands}/bts-*`, `/.claude/skills/bts-*/`, `/.verify/`).  Because git already ignores `.bts/local/`, `bts init` leaves `.gitignore` alone (`gitIgnoresLocal`, `internal/template/gitignore.go`).

A run differs in two ways.  Multica's worktrees come from a separate bare cache (`.repos/…/Carepatron-App.git`), whose `info/exclude` lacks the block.  And the Claude session starts in `workdir/`, one level above the worktree, so it loads settings, hooks and skills from `workdir/.claude/`, and a hook's `CLAUDE_PROJECT_DIR` is `workdir/`.  bts needs its root to be the git worktree, because every git call runs at the root.

S0 therefore keeps every bts file under the worktree and only links the session root to it:

1. `bts init --exclude` writes the block above into the worktree's shared `info/exclude` (`git rev-parse --path-format=absolute --git-path info/exclude`).  No tracked file changes.
2. At the session root: symlinks to the worktree's `.claude/{skills,agents,rules,commands}/bts-*`, the worktree's hook registration copied to `settings.local.json` (its script paths are absolute), and a pointer file `.bts-root`.
3. `FindRoot` follows `.bts-root` when no `.bts/` is above the starting directory, so a hook started from `workdir/` resolves the worktree.
4. In Multica, `bts init --multica`, run from `workdir/`, does 1–3 for the worktree it finds there.

Rejected: rooting bts at `workdir/` with a configured code root, which would need every recorded path translated between two roots; moving Claude assets to the session root, which breaks the layout of the user's own worktrees.  Kept for later: a Multica project with a `local_directory` resource in `in_place` mode at a bts-managed worktree, which makes the session root the repository, but the resource is per project and `in_place` runs one at a time.

## Smoke checks (2026-11-11)

Run against the real instance with a temporary agent, `bts-smoke` (Sonnet, private), on AGEN-153 to AGEN-157, plus a Multica Chat.  Nothing remote was created.

| Area | Check | Result |
| --- | --- | --- |
| S0 | Runs can call `bts` | Pass: the app daemon's `PATH` has `~/.local/bin` (bts v0.19.0). |
| S0 | A run finds its issue | Pass: `issue_id` in `workdir/.multica/daemon_task_context.json` equals the issue.  A chat task's file has `chat_session_id` instead. |
| S0 | Exclude block in the bare cache, then `bts init` in the worktree | Pass: 63 files created, `git status --porcelain` empty, and still empty after a recipe was created, logged and cancelled. |
| S0 | Session-root link, done by hand | Pass: 30 skill symlinks; in the next run all 31 bts skills were in the agent's skill list.  The pointer was stood in for by a `workdir/.bts` symlink, which serves hooks but would break git calls at that root, so the real pointer is still needed. |
| S0 | Hooks fire from the linked settings | Pass: `metrics.jsonl` went from 4 to 11 events within the run; `session_start` carried `source: resume`. |
| S0 | Session resume | Pass: run 2 recalled run 1's branch without reading anything; both runs share one `session_id`. |
| S0 | `owner_session` binding | Pass: set by `bts recipe create` to the run's session. |
| S0 | Notifier stand-in | Pass: `multica issue metadata set <id> --key bts.phase --value research` shows on the issue.  This CLI takes `--key`/`--value`, not positional arguments. |
| S6 | Workspace skill injection | Pass: a skill attached to the agent reached the run (`bts-smoke-probe` answered `heron-42`). |
| S3 | Checkout at a stacked base | Pass: `--ref` put HEAD at the base's tip (`e41d9314`) on `agent/bts-smoke/a164d06f143f`. |
| S3 | Typed `git push` in a run | Refused: the user's global `permissions.ask` has no one to ask in a headless run. |
| S3 | Push and GitHub from a separate executable | Pass: a script run as `./smoke-pr.sh` did `git push --dry-run` over SSH, `gh auth status` (keyring) and a read-only `gh pr list`, with no prompt.  This is the path `bts feature pr` takes. |
| S3 | Restack after a squash merge (local scratch repository) | Pass: a plain rebase of B onto main conflicts once A had a review fix after B branched; `rebase --onto origin/main <fork point recorded at dispatch>` replays only B's commit and keeps A's fix. |
| S2 | Stage barrier | Pass: the parent's assignee woke with `children_done (each_stage)` once both stage-1 children were done, told that stage 2 is next. |
| S2 | A cancelled child run | Its issue stays `todo`, so stage 2 never closes and the parent is not woken again. |
| S2 | Failure wakeup on a child | Pass: `wakeup create <child> --event task.cancelled,task.failed --agent-id <agent>` woke the named agent with the event type, task id and previous status. |
| S4 | Chat | Pass: numbered options came as plain text (in Korean, the member's language), and the mockup arrived as an attachment card.  Session resume in chat and an inline `html` block are still to check. |

Not yet checked: a real draft pull request (no remote write was allowed in smoke), `task.failed` from a genuinely failed run (only `task.cancelled` was produced), and publishing a claude.ai artifact from a run.

## Open questions

1. Runtime cost: every agent on the MacBook runtime shares one personal Claude limit, and a blueprint's spec side has cost about 541K subagent tokens.  Stay on the plan or move to an API key.
2. Concurrent features write the project's shared documents (`project-status.md`, `project-map.md`, `layers/`) from different branches.
3. The feature store's exact path, and whether it moves to a spec repository later.

Resolved by smoke: a dead child run is caught by a failure wakeup on the child; review pages and prototypes can be `html` blocks rendered in Multica, so publishing an artifact from a run is no longer on the critical path.

## S1 specification (revised after review, 2026-11-11)

A first S1 proposal was reviewed by a separate session (verdict: proceed with changes; seven must-fix items).  This is the version the user approved.

**Store.**  `$BTS_FEATURE_STORE` or `~/.bts/features/`, then a repository key, then the feature: `<store>/<key>/f-NNN-<slug>/`.  The key is `RepoKey(origin URL)`: host, owner and name in lower case, with the user, port, `.git` and trailing slash dropped, so SSH and HTTPS clones agree (`git@github.com:Carepatron/Carepatron-App.git` → `github.com/carepatron/carepatron-app`).  With no origin it is `local/<name>-<hash of the git common dir>`.  Ids are unique across the store: `f-NNN` is reserved by `os.Mkdir(<store>/.ids/f-NNN)`, which records the feature's directory, so every command finds a feature by id, never by key; only `create` needs the key (`--repo` overrides it).  `create` with a `--ticket` that already has a feature returns that feature.

**One writer per file.**

```
f-001-<slug>/
├─ feature.json      manager only: the plan (items: id, title, type, express, depends_on),
│                    decisions (skip, defer, release, close, cancel, each with the user's words),
│                    confirmations[], the reconcile cursor; schema and rev; changed under a lock
├─ intent.md, contracts.md   working copies the manager edits
├─ confirmed/<n>/    intent.md, contracts.md and plan.json as confirmed the n-th time
├─ changelog.jsonl   manager only
└─ items/<item>/a<N>/            one directory per attempt, child only
   ├─ claim.json                 written once at claim
   └─ snapshot/{manifest.json, recipe/}   replaced whole on every sync
```

- A binary refuses to write a file whose `schema` is newer than it knows, so a rollback cannot drop fields it does not understand.  Mutating manager commands take a lock directory (`os.Mkdir`, stale after a bound) and bump `rev`.
- **Item state is derived, not stored**: no attempt → planned; latest attempt not terminal → active; an attempt finished in its type's terminal phase (`complete`; `finalize` for spec-only types, which S1 does not allow as items) → done, with `completed_via` gate or forced; a cancelled or released latest attempt → claimable again; a skip or defer decision overrides.  The feature is draft until confirmed, then active, ready for review when every item is done, skipped or deferred, and done or cancelled only by a decision.

**Confirm pins.**  `bts feature confirm <f> --answer "<the user's words>"` checks the plan (at least one item, no unknown dependency, no cycle, `--express` only on build, a non-empty intent), records `{n, answer, time, hashes of intent, contracts and plan}` and copies the three into `confirmed/<n>/`.  A later change to the plan or a contract is pending until the next confirmation; `next`, claims and `context` use the latest confirmed plan and say when the working copies differ.

**Claim.**  `bts recipe create --feature <f> --item <i>` (type and topic from the item) refuses unless the feature is confirmed and open, the item is in the confirmed plan and not skipped or deferred, every dependency is done, this worktree has no active recipe, and no earlier attempt of the item is still open.  It then takes `items/<i>/a<N>` with `os.Mkdir` (exactly one of two concurrent claims wins) and writes `claim.json`: attempt, recipe id, feature directory, confirmation number, worktree root, `start_sha`, branch, Multica issue, bts version, time.  recipe.json records `feature`, `feature_item`, `feature_attempt` and `feature_dir`.  On an item that is already done, `--type fix` opens a fix-forward attempt and restores the done attempt's recipe into the worktree so it can be amended.  The worktree's recipe-id collisions are harmless: each attempt has its own directory.

**Sync, not a one-off harvest.**  One idempotent function copies the recipe's whole directory and its metrics into a staging directory in the attempt and swaps it in as `snapshot/`, with `manifest.json` (phase, type, mode, terminal, `completed_via`, active overrides, branch, `head_sha`, `dirty`, bts version, content hash, time).  It runs on every phase change of a feature-bound recipe, through the phase observers (the store first, then Multica); `bts recipe cancel` goes through the phase log too; every SessionStart re-syncs feature-bound recipes whose snapshot is behind; `bts feature harvest` is the manual form.  A failure is a warning and never blocks a gate.  A feature-bound recipe gets no project-roadmap hints at completion.

**Context.**  `bts feature context <f> <i>` assembles the confirmed intent, contracts and item, and each transitive dependency's latest done attempt (direct ones in full: final or fix spec, deviation, review), from the store, so it works after the dependency's worktree is gone.  It warns when a dependency's `head_sha` is not an ancestor of HEAD, and records what it served in the recipe directory.

**Commands.**  `feature create | list | status [--json] | path | item add|edit|remove|skip|defer|release | confirm | next | context | harvest | reconcile | close | cancel`, plus `recipe create --feature --item`.

Deferred: a manager-session binding and warnings (S2), git history in the store, the stage an item was dispatched with (S2).

**As built (2026-11-11, after a code review by the same reviewer: ship after fixes, five must-fix defects, each reproduced).**  A finished attempt's snapshot is frozen: once its manifest records an outcome in the phase the recipe is still in, no sync rewrites it, so its head, branch and metrics stay those of the worktree that finished it (`completed_at` marks the moment).  The confirmed copy is read through one checked reader: an edited or missing file under `confirmed/<n>/` is an error, never served.  An item removed after a confirmation stays in force, shown as pending removal, and cannot be claimed.  `context` serves every done attempt of a dependency in order, the build's documents and then each fix-forward's.  A fix-forward refuses a worktree whose recipe with the amended id belongs to another item.  Also: the lock is an `flock`; `.ids` entries are relative to the store; keys with `.` or `..` segments are refused; attempt numbers are fixed before the state is read and continue from the highest; a skip or defer waits for an open attempt and `item restore` undoes it; a plain `--amends` on a feature item's recipe is refused in favour of `--feature --item --type fix`; a worktree with a recipe at `finalize` takes no second item; `dirty` ignores `.bts`, `.claude` and `.verify`; a snapshot left aside by a sync that died is read and put back.  A regression test covers each finding, one runs a feature item's completion through the Stop hook's gates, and a CLI smoke (`s1-smoke.sh` in the session scratchpad) runs the built binary through two worktrees, a session root and a missed sync caught by `bts hook session-start`.

## Slices

Each slice ends with a smoke test on the real Multica instance.

| Slice | Content | Smoke test |
| --- | --- | --- |
| S0 | `bts init --exclude`, the session-root link and `.bts-root` pointer, `bts init --multica`, the issue id from the task context, a minimal bts → Multica notifier (`--key`/`--value`) | A run does `bts init --multica` and creates a recipe; its skills load and hooks fire next run; the issue shows the phase; `git status` in the worktree is clean |
| S1 | Feature level and the `bts feature` commands, usable without Multica | A two-item feature locally: confirm, next, context, snapshot |
| S2 | Manager skill and the Multica adapter: dispatch with stages (later stages in backlog), failure wakeups, a `child_done` instruction naming reconcile, wake, reconcile | A manager dispatches two sub-issues in two stages, wakes when the first closes and promotes the second; a cancelled child wakes it too |
| S3 | Worktree and pull-request lifecycle: base refs, fork points, branch rename, `bts feature pr` (push and draft only), restack, harvest | A stacked pair in a scratch repository; squash-merge the first; the second restacks; one real draft pull request in a repository the user names |
| S4 | Chat-mode discovery and the preview step | A short intent conversation in Multica Chat ends in `intent.md` and a prototype file |
| S5 | Snapshot and the review-artifact skill | Feature and PR pages from one snapshot agree on every number |
| S6 | Learning loop | A seeded recurring problem appears under "Lessons proposed" with its evidence |

S4 is mostly skill text and helps without Multica, so it can start at any point.
