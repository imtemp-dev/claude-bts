# Local review pages (`review.surface: local`)

Status: approved 2026-11-22 with D1–D3 as recommended; built the same day (§ As built).  Later that day the user made `local` the default at once and had bts enforce it (§ Enforcement).  Extends [review-pages-design.md](review-pages-design.md); everything not named here stays as that note and the shipped code have it.

## Why

Review pages are claude.ai artifacts.  An artifact belongs to the account that published it, and so do the three things the review loop runs on: the page's data (`db`), the round that reaches the session (`comments` → `sendToClaude` → an auto-reply task notification), and the consent to comment (`permissions`).  The user now works across two Claude Code accounts, switching when one hits its usage limit.  After a switch the session runs as the other account: it cannot write the first account's page data, and a round sent from the page goes to the first account's sessions.  The kit then shows "No Claude session is watching", or the session sees "not found".  Seen in the f-006 trial on 2026-11-21: the coordinator could read the review page only once it was shared to the new account as a writer, and its decision doc stayed "not found".

The loop has to stop depending on a claude.ai account.

### Orca's beta artifacts are not the answer (checked 2026-11-22, Orca 1.4.267)

`orca artifacts share|update|unshare|list|delete` uploads one HTML or Markdown file (up to 10 MB) to Orca's cloud as a **public** link (`shareUrl`) with an expiry (`expiresAt`).  It belongs to the Orca desktop account, can be updated only from the Orca profile that shared it (`editToken`), and stays off until Settings → Artifacts → "Allow publishing public artifact links" is turned on (`out/shared/artifacts/artifact-sharing-gate.js`).  The page gets no runtime: no comments, no data, no way to wake an agent, no supporting files.  It can carry a read-only snapshot, not the loop.  Orca's plugins (a sandboxed HTML panel plus a trusted Node worker with `terminal.sendText` and `agents.list`) could carry the loop inside Orca; they are a later option on top of this design (§ Later).

## Goal

The same review loop, with no claude.ai account: pins, typed or dictated notes, rounds, proposals with Accept and Reject, Update plan, fingerprinted confirmation, and feature pages.  bts serves the page itself from the project, and any session on the machine (whichever account it runs as) processes the rounds.  The records stay as they are: `review.json`, `feedback/round-N.json`, document versions and fingerprints, `confirm --via review --hash` and its gates, and the user-prompt-submit reminder.

Not goals: showing a page to another person or device (the `artifact` surface stays for that); an Orca sidebar panel; publishing to Orca artifacts.

## Design

### 1. The setting

`review.surface` gains `local` beside `artifact` and `ask`.  `bts review page` stops refusing `local`; the skill branches on it.  Default: `artifact` until the local path has run end to end in a real recipe, then `local` (decision D1).

### 2. The server: `bts review serve`

- One server per project root, inside the bts binary (Go `net/http`).  It binds `127.0.0.1` on a free port and records `{pid, port, token, version, started}` in `.bts/local/review-server.json`.
- Started on demand by `bts review open` (below).  A later call reuses it while its pid is alive and its version matches the installed binary; otherwise it is replaced.  It exits after 12 idle hours, or on `bts review serve --stop` (D3).
- Routes, all under one origin and all requiring the token:
  - `GET /p/<subject>/` serves the subject's `page.html` from its work directory (`.bts/local/recipes/<id>/review/` or `.bts/local/features/<f>/review/`).  It injects `"transport": "local"` and the API base into the page's `bts-review-config` block.
  - `GET /kit/bts-review-kit.{js,css}` serves the kit embedded in the binary, the same files `bts init` deploys.
  - `GET /api/<subject>/state` returns notes, rounds and proposals.  `PUT|PATCH|DELETE /api/<subject>/notes/<id>`, `PUT /api/<subject>/rounds/<id>` and `PATCH /api/<subject>/proposals/<id>` write them.  `GET /api/<subject>/events` is a server-sent event stream that pushes every change, including those the session's CLI writes.
- `<subject>` is a recipe id or `feature:<f>`, resolved to a `review.Subject` as the CLI already does.

### 3. Storage

The page data lives in files under the subject's work directory: `data/notes/<id>.json`, `data/rounds/<id>.json` and `data/proposals/<id>.json`.  That is the layout `readExport` already reads, so `bts review import` works unchanged.  With the local surface it reads `data/` by default and the ArtifactData export step goes away.  Every write is atomic (temp file and rename).  The server watches `data/` and pushes changes to the page, so the CLI writes files and never talks to the server.

### 4. The session's side

The skill's ArtifactData and ArtifactComments steps become bts commands that write the same files:

| Today (artifact) | Local |
|---|---|
| Publish with the Artifact tool, then `bts review attach --url` | `bts review open <id> --point <p>` starts or reuses the server, records the URL in `review.json` (as attach does), opens it in Orca's browser beside the terminal (`openReviewInOrca`) or the system browser, and prints it |
| ArtifactData `query` rounds, then `update` to picked | `bts review pick <id> [--round N]` marks submitted rounds picked and prints them |
| ArtifactData `list` notes and rounds to `export/`, then `import` | `bts review import <id>` reads `data/` |
| ArtifactData `set` of each proposal `propose` printed | `bts review propose` writes `data/proposals/<id>.json` itself |
| ArtifactData `batch` of note outcomes | `bts review dispose ... --reply "<one or two sentences>"` writes the note's status, reply and processed time |
| `rounds/r-N` to processed with a summary | `bts review done <id> --round N --summary "..."` |
| `apply` prints proposal docs to upload | `bts review apply` updates `data/proposals/` itself |
| Republish the page | rewrite `page.html`; the page reloads on the change event |
| ArtifactComments `resolve` | nothing to resolve |

`confirm --via review --hash` and its refusals are unchanged.

### 5. Waking the session

Today a round reaches the session as a claude.ai task notification.  Locally:

- **Everywhere:** after `open`, and after each round, the skill runs `bts review wait <id>` with `run_in_background`.  It exits as soon as a submitted round appears and prints it, so Claude Code wakes the session.  It also exits after 100 minutes with "no round yet", inside the 2-hour background limit seen in this environment, and the skill runs it again.  The page shows "A session is waiting" while a wait is open; the server tracks it with a heartbeat file the wait refreshes.
- **In Orca (optional, D2):** when `open` runs in an Orca terminal, it records the terminal handle, and the server sends an Orca orchestration message to that handle when a round is submitted.  That is the wake a worker's question gives a coordinator.  To verify first (V2).
- The user-prompt-submit reminder stays as it is.  It reads `review.json`, whose URL is now the local one.

### 6. The kit

The kit gets a transport seam with two implementations behind the same UI:

- `claude`: today's `window.claude.use('db'|'comments'|'dictation'|'permissions')`.
- `local`: `fetch` and `EventSource` against the page's own origin.  A round is a `PUT` of `rounds/r-N` with `status: submitted`, with no comment.  The consent step and the Allow button disappear.

The kit picks `local` when the config says so and `claude` when `window.claude.use` exists; with neither, it keeps today's browser-only fallback.

Dictation with `local`: the Web Speech API (`webkitSpeechRecognition`) where the browser has it (Chrome, Safari); otherwise the mic button is hidden and the note editor says that macOS dictation (press Fn twice) types into it.  Whether either works in Orca's browser is V1.

### 7. Security

- Binds `127.0.0.1` only.
- A random 128-bit token, kept in `review-server.json` and in the URL `open` prints.  The page carries it on every API call in a header; requests without it get 403.
- `Host` must be `127.0.0.1:<port>` or `localhost:<port>`, which blocks DNS rebinding.  A request that carries an `Origin` header must come from the same origin.  No CORS.
- Only the subject's work directory and the embedded kit are served.  No listing; request bodies are capped at 1 MiB.

### 8. Moving an existing review

A page already on claude.ai keeps working under `artifact`.  To move a review in flight, `bts review open` rebuilds the page from the records: versions, rounds, proposals and confirmations live in bts.  Draft notes that were never sent stay on claude.ai unless the session exports them once with ArtifactData into `data/` first.  The skill says so.

## Verify before building

- **V1.** In Orca's browser: does `webkitSpeechRecognition` work, and does macOS dictation type into a textarea?  The same two in Chrome.
- **V2.** Does `orca orchestration send` to a plain terminal handle, outside a run, wake the Claude session in that terminal?
- **V3.** Does a `run_in_background` command that exits wake an idle session in this Claude Code build, in a terminal and in an Orca chat session?

## Work

1. `internal/review/server.go`: the server, routes, token and Host checks, SSE over a `data/` watcher, idle shutdown.
2. `internal/review/data.go`: atomic reads and writes of `data/`.  Import reads it under the local surface.
3. CLI: `review open`, `review wait`, `review pick`, `review serve --stop`; `--reply` on `dispose`, `--summary` on `done`; `propose` and `apply` write `data/proposals`.
4. Kit v3: the transport seam, local dictation and the waiting indicator.  `KitVersion` goes to 3.
5. `engine`: the `local` value, with its migrate and doctor entries.
6. Skill: a `review.surface: local` branch in `/bts-review-page`, covering recipe and feature, Round, Update plan and Confirm.  The calling skills (discover, blueprint, build, implement, fix, feature-orca) need no change, since they already defer to `/bts-review-page`.
7. Tests: server auth (token, Host, Origin); an API round trip into `data/` and `import`; `wait` exits on a submitted round; the SSE push after a CLI write; the CLI flags.
8. Docs: a line each in README.md and llms.txt; `bts docs check`.

About one and a half to two days.  V1–V3 come first, about an hour.

## Decisions for the user

- **D1:** default surface `local` after its first real run, with `artifact` kept for sharing.  Recommended.  Taken early (§ Enforcement).
- **D2:** in Orca, wake by orchestration message as well as `bts review wait`, if V2 holds.  Recommended.
- **D3:** one server per project root, stopping after 12 idle hours.  Recommended.

## As built (2026-11-22)

- Built: `review.surface: local`; `internal/review/data.go` (page data, atomic writes under a `flock`); `internal/review/server.go` (`bts review serve`); `bts review open|wait|reply|serve --stop`; `--reply` on `dispose` and `propose`; `--summary` on `done`.  `propose` and `apply` write the page data themselves.  Kit 3 adds the local transport, browser speech recognition with a macOS dictation hint, and a reload on a page.html change that waits for an open note.  The `/bts-review-page § Local page` section is written, and the seven calling skills route `local` to the page.
- `bts review pick` was folded into `bts review import`.  On a local page `import` needs no `--dir`, reads `data/`, and marks the rounds it takes picked.
- The page carries the token as an `HttpOnly`, `SameSite=Strict` cookie set on the authorized page load, so the kit's relative loads and its `fetch`/`EventSource` calls need no token in their URLs.
- **V2 not run, D2 not built.** `orca orchestration send --to` accepts `run:`, `dispatch:`, `bot:` or a legacy handle, not a plain terminal.  Checking whether a legacy handle wakes an ordinary session means sending into a real session's terminal, so the Orca wake is left for a later slice.  `bts review wait` covers every terminal and the Orca chat.
- **V3 holds.** This environment's background commands wake the session when they exit; every watcher in the trial watch ran that way.
- **V1 is pending.** It needs the user to speak into the page in Orca's browser and in Chrome.
- A CLI smoke on a scratch project ran: review, proposal, apply (intent.md v2) and confirm (`bts recipe confirm --via review --hash`) end to end over the server, plus token, Host and Origin refusals and an SSE push after a CLI write.

## Enforcement (2026-11-22)

A session asked for "an artifact" of an intent, scope or final.md still reached for the Artifact tool, because skills and CLAUDE.md text only suggest.  The user chose to have bts refuse it, since the rule concerns bts's own review documents only.  D1 is taken without waiting for a first real run.

- **Default `local`.**  An unset or unknown `review.surface` reads as `local`; an explicit `artifact` or `ask` stays.  The settings template, `bts migrate`, README, llms.txt and the calling skills say so.
- **PreToolUse deny.**  Under `local`, an Artifact publish (no `action`, or `publish`) whose `file_path`, `file_paths` or `files` names a file under `.bts/local/{recipes,features}/<id>/review/`, or the review kit, is denied, and the reason names `bts review open <id> --point <p>` (or `--feature <f>`).  Reads, lists and every other artifact pass (`internal/hook/review_surface.go`).
- **`bts review attach` refused** under `local`, with the same pointer, before anything is written.
- **UserPromptSubmit reminder.**  A prompt that asks for an artifact or a review page, in English or Korean, while a recipe is active or a feature review exists, gets one line saying the surface is local and how to open the page.  The review reminder tells a local page's session to run `bts review import`, not to query ArtifactData.  The hook input gained `prompt`, with a captured payload as its contract fixture.

## Later

- `bts review share`: a read-only snapshot of a page as an Orca artifact.  It is a public link with an expiry, so it is opt-in, and the skill warns before sharing anything internal.
- An Orca plugin panel that shows the page beside the terminal over the same API, with a worker for notifications.
