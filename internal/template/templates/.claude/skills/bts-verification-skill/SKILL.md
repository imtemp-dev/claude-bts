---
name: bts-verification-skill
description: >
  Generate the project's verify-<app> skill: a scripted way to launch the
  real app, drive it the way a user does and capture evidence, with a
  feature map of user journeys. Run once per project, before the first
  /bts-prove. Adapted from pstack's create-verification-skill.
user-invocable: true
allowed-tools: Read Write Edit Grep Glob Bash AskUserQuestion
argument-hint: "[app name]"
---

# Generate a verification skill

Every project needs a scripted way to drive the real app and prove
behaviour: launch it, exercise a feature the way a user would, and capture
evidence. You write `.claude/skills/verify-<app>/` for the next agent, which
will read it cold, mid-task, having never seen the app.

## 1. Interview the repository, not the user

Answer from the code; ask the user only what you cannot observe.

- **Surface:** what a user touches: web UI, CLI or TUI, a service or API, a
  long-running process fed by events (a bot, a worker), a library.  A
  repository may have several; pick the primary one and note the rest.
  For a bot or a worker the user is whoever sends it events, and the
  operator who installs and runs it is a second user whose steps (setup,
  a smoke test) are journeys too.
- **Run:** how the app starts locally, from the project's own documented
  command (README, package scripts, Makefile).  Ports, environment, seed
  data, auth.  A documented step that writes outside the project (installs
  a binary on PATH, loads a launchd or systemd unit, stores a keychain
  entry) is the user's setup, not a launch: run from this checkout's own
  build and never install.
- **Drive:** how an agent can interact with it: existing harnesses first
  (Playwright specs, a PTY helper, a replay of recorded events, a debug
  endpoint), then a generic recipe (a browser for web, a subprocess with its
  output and exit code for a CLI that never prompts and a PTY for one that
  does, HTTP for a service, recorded payloads for an event consumer).  A
  harness that sets internal state directly (writes state files, calls a
  handler past the entry point) shows what to check, not how to drive.
- **Observe:** the evidence a run can leave: screenshots, transcripts,
  response bodies, logs, exit codes, database rows, files written.
- **Isolate:** whether two instances can run side by side (ports, data
  directories), and what the app reads from the machine: the home
  directory, a keychain, a global config, a command on PATH or at a fixed
  path.  Point each at the run's own sandbox or a stub, or keep the drive
  away from it, and say which in the skill.  A wrapper or hook that finds
  the app through PATH can run an installed copy instead of this checkout's
  build.
- **Reach:** whether starting the app as documented touches production
  from this machine: real credentials it reads, a real service it connects
  to at boot, a real system it writes to.  If it does, the drive starts at
  the highest layer below the entry point that does not (the app object,
  the handler wiring), with every external system behind a stand-in, and
  the layers it skips (the real boot) are a journey `/bts-prove` defers to
  whoever runs the app on its real host.

If the project does not build or start as it is, fix that first or report
it precisely.  An app that cannot start safely here (Reach) is not broken:
drive it below its entry point.

## 2. Generate the skill

Write `.claude/skills/verify-<app>/SKILL.md` with frontmatter (`name:
verify-<app>`, a `description` naming the app, the surface and when to use
it) and these sections, each from what the interview found, no
placeholders:

- **Launch:** the exact command that starts the app for verification and how
  to tell it is ready (a log line, a prompt, a health check that means
  usable).  When the app's logging hides its startup line, the process
  the launch started listening on its port is the signal; a port answered
  by any other process is not ours.  It reaches a usable state from an
  empty data directory.
  Include teardown.  An app with no long-running process (a CLI, a
  library, an event handler driven in-process), or one launched below its
  entry point (Reach), has no ready signal: its instance is a fresh sandbox
  (a temporary directory under a root an environment variable can move,
  with HOME and TMPDIR inside it when the app reads them) on a current build
  of this checkout, and it is ready once the app object is built there.
- **Doctor:** one read-only check that the instance is worth driving: the
  drive will run this checkout's current build, not an installed copy, and
  for a service the process is up, the port is ours and auth is valid.
- **Drive:** the harness recipe with real handles from this repository
  (selectors, commands, routes, event fixtures), not examples.  Each
  journey's drive is one command that `bts prove run --cmd` runs from the
  project root: it launches, drives, checks the end state, cleans up, and
  exits non-zero when any check fails.  It writes its verdict file,
  `.verify/<journey>/verdict.json`, on every run, failing ones included:
  `{"checks": [{"check": "<what it compared>", "pass": true|false, ...}]}`,
  one entry per end-state comparison (`bts prove run --verdict` passes a
  journey only with at least one check and none failed).  Every file it
  names as evidence exists and is non-empty after a passing run.
- **Evidence:** what each proof captures and where it goes: under
  `.verify/<journey>/` at the project root, never inside a sandbox that
  cleanup deletes.  `bts prove run` looks for it there, and bts leaves
  `.verify/` and gitignored paths out of the code fingerprint, so anything
  else a drive writes in the project (a log, a cache, bytecode in a path
  git does not ignore) can read as a code change.  Unless the team keeps
  evidence in git, ignore `.verify/` in the file that already keeps bts
  out: `git check-ignore -v .bts/local/x` names it.  A shared repository
  keeps bts out through the exclude file every worktree shares
  (`$(git rev-parse --git-common-dir)/info/exclude`), where a line in the
  tracked `.gitignore` would land in a pull request.  The `verify-<app>`
  skill directory goes in the same file unless the team commits it.
  Proof standards:
  - exercise the real user path, not internal setters or test-only
    endpoints;
  - capture the action and the resulting state, and the side effects
    (files, rows, messages) beside what is visible;
  - stand-ins only at a production boundary: a seam the app already has (a
    configurable binary or URL, an injected client), or, where it has none,
    the client library's own request call, never the app's code; name each
    stand-in in the skill;
  - when the safe path is a dry run or test mode, check by observing what
    it skips (files, network, external CLIs) rather than trusting its name.
- **Cleanup:** remove what the run started, never by process name; keep the
  evidence.  What a run starts includes files: sandboxes, temporary
  directories, and the copies a tool makes under TMPDIR.
- **Helpers:** any script the skill ships is executable and its invocation is
  shown.

## 3. Seed the feature map

Create `.claude/skills/verify-<app>/features/README.md` and one file per
user-facing feature (start with the three to five a user reaches most
often).  Each file has four sections: `Sub-features`, `How to get to it
(user POV)`, `Driving it with <harness>` (the harness by name: Playwright,
the PTY helper, the skill's drive scripts), and `Gotchas`.  A journey whose
precondition the seed data lacks reports a failure and says why under
`Gotchas`.  When a drive fails, decide which side is wrong: a drive that
skips a step a real user takes, or checks the wrong thing, is corrected to
the user's path; an app that misses a right check is a failure to report.
When the project's own docs promise what the code does not do, keep the
check the docs imply and report the disagreement; the owner fixes one or
the other.  Never drop or loosen a check because the app fails it.  A
journey that needs something this machine cannot provide (an installed
platform app, real credentials, a real target, a paid run the user has not
approved) says so in its own feature file: it is the one `/bts-prove`
defers, with its owner.

## 4. Prove the skill before handing it over

Run its own instructions end to end once: launch, doctor, drive one mapped
feature, capture evidence, clean up, and confirm the evidence survived the
cleanup.  Fix what fails, and run the cleanup after every failed attempt.
A skill never executed is a draft.  If you proved it on a stand-in for the
real target, list each setup step the stand-in did that the target will
not, in the skill's SKILL.md under a heading of its own.
