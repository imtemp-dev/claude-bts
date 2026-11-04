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
- **Run:** how the app starts locally, from the project's own documented
  command (README, package scripts, Makefile).  Ports, environment, seed
  data, auth.
- **Drive:** how an agent can interact with it: existing harnesses first
  (Playwright specs, a PTY helper, a replay of recorded events, a debug
  endpoint), then a generic recipe (a browser for web, tmux or a PTY for a
  CLI, HTTP for a service, recorded payloads for an event consumer).
- **Observe:** the evidence a run can leave: screenshots, transcripts,
  response bodies, logs, exit codes, database rows, files written.
- **Isolate:** whether two instances can run side by side (ports, data
  directories).  If not, say so in the skill.

If the project does not build or start as it is, fix that first or report
it precisely.

## 2. Generate the skill

Write `.claude/skills/verify-<app>/SKILL.md` with frontmatter (`name:
verify-<app>`, a `description` naming the app, the surface and when to use
it) and these sections, each from what the interview found, no
placeholders:

- **Launch:** the exact command that starts the app for verification and how
  to tell it is ready (a log line, a prompt, a health check that means
  usable).  It reaches a usable state from an empty data directory.
  Include teardown.
- **Doctor:** one read-only check that the instance is worth driving:
  process up, right build, port owned by us, auth valid.
- **Drive:** the harness recipe with real handles from this repository
  (selectors, commands, routes, event fixtures), not examples.
- **Evidence:** what each proof captures and where it goes (a path inside
  the project, for example `.verify/<journey>/`).  Proof standards:
  - exercise the real user path, not internal setters or test-only
    endpoints;
  - capture the action and the resulting state, and the side effects
    (files, rows, messages) beside what is visible;
  - mocks only where a production boundary already isolates the external
    system;
  - when the safe path is a dry run or test mode, check by observing what
    it skips (files, network, external CLIs) rather than trusting its name.
- **Cleanup:** remove what the run started, never by process name; keep the
  evidence.
- **Helpers:** any script the skill ships is executable and its invocation is
  shown.

## 3. Seed the feature map

Create `.claude/skills/verify-<app>/features/README.md` and one file per
user-facing feature (start with the top three to five).  Each file has four
sections: `Sub-features`, `How to get to it (user POV)`, `Driving it with
<harness>`, and `Gotchas`.  A journey whose precondition the seed data lacks
reports a failure and says why under `Gotchas`; never weaken a journey to
make it pass.  A journey that needs something this machine cannot provide
(an installed platform app, real credentials, a real target) says so: it is
the one `/bts-prove` defers, with its owner.

## 4. Prove the skill before handing it over

Run its own instructions end to end once: launch, doctor, drive one mapped
feature, capture evidence, clean up, and confirm the evidence survived the
cleanup.  Fix what fails, and run the cleanup after every failed attempt.
A skill never executed is a draft.  If you proved it on a stand-in for the
real target, list each setup step the stand-in did that the target will
not.
