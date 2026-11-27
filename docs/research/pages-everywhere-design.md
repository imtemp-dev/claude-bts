# bts pages everywhere

Status: confirmed by the owner 2026-11-27 with D1 and D2 as recommended ("The direction is right, go ahead", through Pika item 42); built the same day on `bts-pages-everywhere` (§ As built), not yet released.  Extends [local-review-pages-design.md](local-review-pages-design.md): review pages keep their loop, records and gates; this note moves their server, adds general pages beside them, opens both to the owner's phone over the tailnet, and adds a file export for teammates.

## Why

- **Accounts.** A claude.ai artifact belongs to the account that published it, and the active Claude Code account switches when one hits its usage limit.  Review pages left claude.ai in v0.36.0; every other page a session makes (a Tron architecture page, a PR explainer, a research note) still goes there and is stranded on whichever account made it.
- **Phone.** The owner opens pages from the phone over Tailscale, not by sharing the Mac's screen.  Today `bts review serve` binds `127.0.0.1` on a random port, one server per project root, links are `http://127.0.0.1:<port>/p/<subject>/?t=<token>`, and the Host check accepts only `127.0.0.1` and `localhost`.  On 2026-11-27 six such servers were listening on this Mac.
- **Teammates.** They are not on the owner's tailnet, and the server is the owner's laptop.  A page they must read leaves as a file.

## The owner's decisions (2026-11-27)

- Every new page is created and updated through bts.  Existing claude.ai artifacts stay where they are.
- The phone path is `tailscale serve` (tailnet-only HTTPS).  The server stays bound to `127.0.0.1`.  Needed: a stable port, accepting the host name the proxy forwards, links built on the tailnet URL (this Mac: `jongyoons-macbook-air.tail0c00f6.ts.net`), and the token kept.
- Pages for teammates are exported as a static file.

## How finished tools do it (checked 2026-11-27)

| Need | What they do | What bts takes |
|---|---|---|
| Accept a proxied host name without opening DNS rebinding | Vite `server.allowedHosts`: localhost and IP addresses by default, extra names listed one by one, `true` (any host) flagged as a DNS rebinding risk.  Jupyter: `allow_remote_access=False` with `local_hostnames` (default `['localhost']`) | An explicit list: `127.0.0.1`, `localhost` and the one tailnet name recorded at setup.  Never "any host" |
| Print the link people open | Jupyter `custom_display_url`: overrides the URL shown, appends the token itself, changes nothing about where it listens | `public_url` in the pages config.  Every command prints the local link and the tailnet link, token appended |
| Know the scheme behind a TLS-terminating proxy | Jupyter `trust_xheaders` (X-Forwarded-Proto) "necessary if the proxy handles SSL" | Not needed: the tailnet name is only ever reached through the proxy, so bts ties `https` to that name and trusts no header |
| A sub-path or its own port | Jupyter `base_url` and its kin need the app to know its prefix | Its own HTTPS port (below), so the server keeps root-relative paths |
| A proxy that survives reboots | `tailscale serve --bg` "runs persistently in the background until you disable it" and resumes after a reboot or `tailscale down`/`up` (KB 1242); the config is kept in tailscaled's state store | Setup runs it once with `--bg` |
| A resident server that survives logout and reboot | Homebrew's `brew services` writes a LaunchAgent into `~/Library/LaunchAgents` and loads it with `launchctl` | A LaunchAgent (D2) |
| Publish and update a page by id, keep versions | `gh gist create` returns an id; `gh gist edit <id>` updates it; a gist keeps its revisions.  The Artifact tool: a publish with `url` updates, a republish of the same file path updates, a new path makes a new page | `bts page publish`: the same rules, a page id in place of a URL |
| One file that opens anywhere | pandoc `--embed-resources` inlines linked scripts, stylesheets, images and videos as `data:` URIs, downloads absolute URLs, leaves elements with `data-external="1"` alone, and cannot embed what JavaScript loads at run time.  Quarto `embed-resources` leaves MathJax and KaTeX out by default as too large | The same inlining for local files; remote files stay links unless asked (§ 5) |

Tailscale serve's proxy, read from `ipn/ipnlocal/serve.go` on `main` (the pinned 1.104.7 commit was not fetchable; V1 checks it on this Mac):

- The backend gets the client's `Host`, port included (`r.Out.Host = r.In.Host` for a TCP backend), so the Host check sees the tailnet name, not `127.0.0.1`.
- `X-Forwarded-Host`, `X-Forwarded-Proto: https` and `X-Forwarded-For` are set, replacing any client copy.  For a tailnet user (not a tagged node, not Funnel) it adds `Tailscale-User-Login`, `-Name` and `-Profile-Pic`, after deleting any copies the client sent.
- The `httputil.ReverseProxy` sets no buffering, so server-sent events stream (Go's proxy flushes `text/event-stream` at once).
- A mount path (`--set-path /foo`) is stripped before the request reaches the backend.
- Tailnet-only serve takes any HTTPS port; the 443/8443/10000 limit is Funnel's.

This Mac's serve config already maps `https://jongyoons-macbook-air.tail0c00f6.ts.net/` to `127.0.0.1:3000` and `/ws` to `127.0.0.1:8080`.  bts takes its own port and leaves those alone.

## Design

### 1. One page server per user

`bts pages serve` replaces the per-project `bts review serve`: one server for the user on this machine, on a fixed port (default `7337`, set in the pages config), bound to `127.0.0.1`.  One stable port is what `tailscale serve` can point at, phone links must outlive a project's worktree, and a general page belongs to no project.

- State lives in `~/.bts/pages/` (`BTS_PAGES_HOME` overrides it, as `BTS_FEATURE_STORE` does for features): `config.json` (port, public URL), `token` (mode 0600), `server.json` (pid, port, version, started), `projects.json`, and one directory per page.
- Routes, all behind the token:
  - `/` an index: general pages newest first, then review pages by project with their open work.  One bookmark on the phone.
  - `/p/<page>/` a general page and its files; `/p/<page>/v/<n>/` an older version.
  - `/r/<project>/<subject>/` a review page, its kit and its API (`api/state`, `api/events`, `api/doc/...`), the routes of today's server under a project prefix.
- `<project>` is the project root's base name and six hex digits of its path's SHA-256 (`claude-bts-3fa2c1`).  `bts review open` registers the root in `projects.json`; a root that no longer exists answers "gone" and is pruned.
- `bts review open` uses this server and, the first time, stops the project's old server if `.bts/local/review-server.json` names one.
- Only the installed binary replaces a running server of another version.  A development build or a test uses its own `BTS_PAGES_HOME` and port.
- The server stays up under a LaunchAgent (D2).  Without one it starts on demand, as today.

### 2. General pages through bts

A session makes a page as it does now (the artifact-design pass, an HTML file in its scratchpad) and publishes it with bts instead of the Artifact tool.  bts serves static pages: no `window.claude` runtime.

- `bts page publish <file.html> [--title t] [--description d] [--file published=source]... [--page <id> | --new]` creates a page or updates one, mirroring the Artifact tool's rules: `--page <id>` updates that page; a file whose path an earlier publish used updates that page; anything else makes a new one.  The title comes from `--title`, else the file's `<title>`, else its name.  It prints the page id, the version, the local link and the tailnet link, and opens the page in Orca's browser beside the terminal (or the system browser), as `bts review open` does.
- A page is `~/.bts/pages/<id>/`: `page.json` (id, title, description, created, updated, current version, source path, the project and session that last published) and `v/<n>/` holding each version's files.  Versions are kept.  The id is a slug of the first title and four random characters (`tron-run-lifecycle-k3f9`), and it stays when the title changes.  A publish takes a lock, so two sessions publishing one page each get their own version.
- `bts page list [--json]`, `bts page open <id>`, `bts page read <id>` (prints where the current version's files are, so a later session edits a copy and republishes, which the Artifact tool's `read` did), `bts page delete <id>` (a session runs it only when the owner asks).
- Any session can publish, including headless ones: a `claude -p` run has no Artifact tool at all (the Multica probe of 2026-11-16), but it can run bts.

How sessions are sent to bts:

- A **user-level** PreToolUse hook, matcher `Artifact`, runs `bts hook artifact-guard`.  It has to be user-level: Carepatron-App, where the Tron pages are made, is not a bts project, so its sessions run no bts project hooks.  The guard needs no project and does nothing for other tools.
- It denies a publish that would make a new claude.ai page: action `publish` (or none), no `url`, no `asset`, and a `file_path` (or `files`).  Its reason names the command: `bts page publish <file_path> --title "<title>"`, keep the page as written, give the owner the links it prints.  An update to an existing claude.ai artifact (publish with `url`, asset uploads to it), and `read`, `list`, `open`, `pin` and `delete` pass, so existing pages stay where they are.  D1 covers typed artifacts and pages that declare `capabilities`.
- A republish without `url` of an artifact made earlier in the same conversation looks the same as a new page, so after the hook is installed it becomes a bts page.  That only touches conversations that span the install.
- Review pages keep the project hook's own denial and message; the guard leaves files under `.bts/local/{recipes,features}/*/review/` to it.
- No prompt hook: the deny arrives when the session is about to publish, which is the moment it needs the command, and a user-level prompt hook would run on every prompt in every project.

### 3. Host, origin, token and links

- **Host:** `127.0.0.1:<port>`, `localhost:<port>`, and the tailnet name with or without the HTTPS port (`jongyoons-macbook-air.tail0c00f6.ts.net:7337`).  Anything else gets 403, which keeps DNS rebinding blocked.
- **Origin** (writes only, as today): it must be the origin the request came to, `http://<Host>` for a loopback name and `https://<Host>` for the tailnet name.  No header is trusted.
- **Token:** the same role as today: `?t=` on the first load, then an `HttpOnly`, `SameSite=Strict` cookie, marked `Secure` when served under the tailnet name.  It is generated once and kept across restarts, so a link on the phone keeps working; `bts pages token --rotate` replaces it.  Tailscale's identity headers are not used for access: on `127.0.0.1` any local process could send them.
- **Links:** commands print both links with the token.  Records keep them without it: `review.json` sits under `.bts/specs/`, which a project may commit, and today's local `review.json` holds the token.  With a token that outlives restarts that would matter.

### 4. Setup

`bts pages setup [--dry-run]` does each step once and prints what it changed:

1. Writes `~/.bts/pages/config.json` and the token.
2. Adds the guard to `~/.claude/settings.json` (`command -v bts >/dev/null && bts hook artifact-guard`, so a machine without bts is unaffected), next to the entries already there.
3. Reads `tailscale serve status --json`; if the HTTPS port is free, runs `tailscale serve --bg --https=7337 http://127.0.0.1:7337` and records `https://<tailnet name>:7337` as the public URL.  A taken port stops setup with what holds it.  No tailnet: setup skips this step and the links stay local.
4. Installs and loads the LaunchAgent (D2).

`bts pages teardown` undoes each step.  `bts pages status` shows the server, the serve mapping, the guard and the agent.

### 5. Static export

`bts page export <id> [-o file]` and `bts review export <recipe-id>|--feature <f> [-o file]` write one HTML file that needs no server:

- Local stylesheets, scripts and images (including `url()` in an inlined stylesheet) are inlined, as pandoc does.  Remote (`http`/`https`) files stay links, since teammates are online and a CDN library can be megabytes; `--embed-remote` downloads and inlines them.  An element marked `data-external="1"` is left alone (pandoc's convention).
- It reports what it could not embed: a missing file, a load made by script at run time, and any link to `127.0.0.1` or the tailnet name, which a teammate cannot open.
- A review page exports as a read-only snapshot: the kit is inlined with a `static` transport that hides note mode, Send, proposals and Confirm, and no notes, rounds or replies are included, since they hold the owner's own words.  Kit 4.
- A short footer says what it is: the title, the version and the export date.  The file holds no token.
- Default output: `~/Downloads/<id>-v<n>.html`.  Sending it is a separate step the owner asks for (a Slack upload, a drive); bts writes the file only.

## Out of scope

- Moving or mirroring existing claude.ai artifacts.  They are updated on claude.ai as before, by an account that can.
- The claude.ai runtime on bts pages: page data, comments sent to Claude, calls to Claude, connected data.  Review pages keep their own loop (local-review-pages-design.md).
- Anyone outside the owner's tailnet reaching the server: no Funnel, no public link.  Teammates get the export.
- Uploading an export anywhere, and PDF output.
- Pages made on another machine, and a Linux service (`systemd`): there the server starts on demand.
- The owner's own rules that assume claude.ai artifacts: the Tron artifacts paragraph of `~/.claude/rules/tron.md` (publish from this account, the claude.ai series index, the Obsidian mirror) and the `tron-docs-sync` skill.  They need a matching change once bts pages ship, drafted for the owner's approval as a separate step.

## Verify before building

- **V1.** On this Mac, a throwaway server behind `tailscale serve --bg --https=<port>`: the Host and `X-Forwarded-Proto` it receives, and an event stream arriving unbuffered.  The source was read on `main`, not 1.104.7.
- **V2.** On the owner's phone, over the tailnet: the link sets the cookie, the page loads, a note saves, the page updates live, and keyboard dictation types into the note editor.  Needs the owner for about ten minutes.
- **V3.** The LaunchAgent restarts the server after the installed binary is swapped and the old server stopped, and starts it after login.
- **V4.** The user-level guard fires in a session outside any bts project (Carepatron-App) and its reason reaches the session.

## Work

1. `internal/pages`: config, token, page store with versions and lock, project registry.
2. The server: `internal/review/server.go` becomes the user's page server with `/`, `/p/` and `/r/` routes, the host and origin lists, the kept token and the `Secure` cookie.  The review handlers keep their code under the project prefix.
3. CLI: `bts page publish|list|open|read|export|delete`, `bts pages serve|status|setup|teardown|token`, `bts review export`; `bts review open` on the new server; records without the token.
4. `bts hook artifact-guard` and its decision table.
5. The LaunchAgent (D2).
6. Export: the inliner and the kit's `static` transport (Kit 4).
7. Skill and docs: `/bts-review-page § Local page` links, README, llms.txt, `bts docs check`.
8. Tests: the host and origin lists (tailnet name with and without the port, `https` only for it), token kept and rotated, publish by id, by source path and `--new`, versions, the guard's table (new page, update with `url`, typed artifact, `capabilities`, review page, other actions), export inlining and its report, project registry and the old server's shutdown.

About two to two and a half days; V1–V4 first, about an hour, V2 with the owner's phone.

## Decisions for the owner

- **D1:** A page that needs claude.ai itself (a typed artifact such as Docs or Slides, or a page declaring runtime `capabilities`): the guard lets it through to claude.ai, with a note that it stays on the current account (recommended: bts serves static pages only, and such a page cannot work there).  Or the guard denies it too, and the session makes a static page or tells the owner.
- **D2:** A LaunchAgent keeps the page server running, so a link opens on the phone whenever the Mac is awake (recommended).  Or the server starts on demand and exits after 12 idle hours, as today, so a link fails until a session starts it again.

## As built (2026-11-27)

- `internal/pages`: config, token, page store with versions, project registry, the server (`/`, `/p/`, `/r/`), `Ensure` (start, reuse, or replace the server), the LaunchAgent, setup (guard, tailnet) and the exporter.  `internal/review/serve.go` keeps a subject's page, kit and API handlers, which the page server calls under `/r/<project>/<subject>/`; the per-project server is gone.
- CLI: `bts page publish|list|open|read|export|delete`, `bts pages serve|status|stop|open|setup|teardown|token --rotate`, `bts review export`.  `bts review serve` is removed; `bts review open` registers the project, uses the page server, and the first time stops the project's old server.  `bts hook artifact-guard` is the user-level guard.
- Changes from the design:
  - **No Kit 4.**  `bts review export` drops the kit's script, inlines its stylesheet and sets the config's transport to `static`: the same read-only snapshot with no kit change.
  - **The cookie is `SameSite=Lax`, kept for a year, `Secure` under the tailnet name, and the token leaves the address bar.**  A page load with `?t=` sets the cookie and redirects (303) to the same path without it, so the token stays out of history, bookmarks and screenshots.  `Strict` would drop the cookie on a link opened from a chat app, which is a cross-site navigation that the redirect keeps cross-site.  Writes stay guarded by the origin check and are never GETs.
  - **The token lives in its own file (`token`, 0600); `server.json` holds none.**  Comparisons are constant time.
  - **The tailnet name is accepted with its port only** (`<name>:7337`), since the mapping is on that port.
  - **An old per-project server is stopped only if it answers with its own token**, so a pid the system reused is never signalled.  Old servers of other projects exit after their 12 idle hours.
  - **`Ensure` holds a lock**, so two sessions publishing while the server is down start one server, not two.
  - **The guard adds a hint for a `.md` file**: bts serves HTML pages only.
  - **Export reports missing files and does not fail**: the file is written and lists what it lacks.
  - `golang.org/x/net/html` (its tokenizer) is a new dependency, for the exporter.
- Verified:
  - `go vet ./...`, `go test ./...` and `bts docs check` pass.
  - **V1 holds** on this Mac (Tailscale 1.104.7).  A throwaway server behind a temporary `tailscale serve --bg --https=7337` received `Host: <name>.ts.net:7337`, `X-Forwarded-Proto: https` and the `?t=` query unchanged.  Server-sent events arrived one second apart, unbuffered.  The mapping was removed afterwards.
  - **V3 holds** with a scratch LaunchAgent (label `dev.imtemp.bts.pages.v3test`, scratch store, port 17337).  launchd restarted the server about two seconds after a SIGTERM, `kickstart -k` replaced it, and `bootout` stopped it.  A start at login was not tried; it is the same `RunAtLoad`.
  - A real-file check: adding the guard to `~/.claude/settings.json` and removing it again, in memory, gives back the file byte for byte.  That check found and fixed Go's HTML escaping of other hooks' `>` and `&`.
  - A CLI smoke on a scratch store and port with a dev build covered three paths.  Pages: publish with a stylesheet and an image, the token redirect and cookie, the host refusal, the index, an update with a carried-over image, and export.  A review in a scratch bts project: `review open`, a note and round written through the API, a cross-origin write refused, `review wait`, `import`, `reply`, and `review export` (kit script dropped, transport `static`, no notes).  Setup: `bts pages setup --dry-run`, which changed neither the settings file (same hash) nor tailscale, and which refuses port 443, already mapped.
- **Not yet verified, waiting on the owner's go to install:** V2 (the owner's phone over the tailnet) and V4 (the guard in a Carepatron-App session).  Both need `bts pages setup` on this Mac, which edits `~/.claude/settings.json`, maps tailnet port 7337 and installs the LaunchAgent.
