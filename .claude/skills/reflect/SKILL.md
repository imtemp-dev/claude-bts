---
name: reflect
description: >
  Mine a finished bts recipe run for durable lessons and route each to
  the strongest place in this repository that can hold it: a Go check,
  an enforced settings key, a template's prose, or docs. Three reviewers
  read the run in their own contexts, a synthesizer sorts their findings
  into Accepted, Rejected and Backlog, and nothing is applied until the
  maintainer picks rows. Maintainer tool for the claude-bts repository;
  never shipped by `bts init`.
user-invocable: true
disable-model-invocation: true
allowed-tools: Read Grep Glob Bash Agent
argument-hint: "<project-path> <recipe-id> | <session.jsonl>"
effort: high
---

# Reflect

Turn one finished recipe run into changes to bts, each in the strongest
place that can hold it: $ARGUMENTS

## Where this sits

This skill lives in the claude-bts repository's own `.claude/skills/`,
never under `internal/template/`. It reads a run in a user project and
proposes changes to this repository. It is what the `measure:` commits
do by hand, with three readers in separate contexts instead of one.

`disable-model-invocation` is set because one pass spawns four agents,
three of them on Opus. It runs when the maintainer types `/reflect`.

Skip a run that ended before its first gate, or one that went as the
templates say. A one-off slip is not a lesson.

Adapted from pstack's `reflect`. Two things differ. pstack moves anything
a lint or script could enforce to Backlog, because its skills and its
tooling have different owners; here the binary and the templates live in
the same repository, so such an item is Accepted and routed to Go (see
Routing). And pstack files Backlog items to a tracker at once; here
nothing is filed, because filing publishes text other people read.

## Models

| Seat | `model` | Why |
| --- | --- | --- |
| Judgment reviewer | `opus` | Names the principle behind an incident. |
| Tooling reviewer | `sonnet` | Commands, flags, paths: pattern work. |
| Divergent reviewer | `opus` | The finding under the obvious one. |
| Synthesizer | `opus` | Reads the repository to accept or reject each row. |

Pass `model` on every spawn, and only `opus` or `sonnet`. Never `fable`:
it is rate-limited, and its one role in bts is the architect tie-break
(`docs/research/pstack-lessons.md`, Decisions). pstack's model-role rule
file does not exist here.

## Step 1: Gather the run

Make a working directory for this pass, `$W`: the session's scratchpad
when the system prompt names one, else `mktemp -d`. Everything this
skill writes goes there. Nothing is written to the user's project.
`$REPO` is this repository's root. Shell variables do not survive from
one Bash call to the next, so write both out as literal paths in every
command.

### From a project path and recipe id

Run from the project root. Every command here only reads.

```bash
cd <project>
bts stats <id> --json                > "$W/stats.json"
bts stats --indicators --recipe <id> > "$W/indicators.json"
bts validate <id>                    > "$W/validate.txt" 2>&1
bts doctor <id>                      > "$W/doctor.txt"   2>&1
cat .bts/config/.template-version
```

- The recipe's record stays in place at `.bts/specs/recipes/<id>/`:
  `recipe.json`, `changelog.jsonl`, `findings.jsonl`, `verify-log.jsonl`,
  `decisions.jsonl`, `overrides.jsonl` when present, and the documents.
  Reviewers read it there.
- `.template-version` reads `v{version}-{commit}`, e.g. `v0.14.0-f8005e3`.
  Keep the commit. The synthesizer uses it to tell a gap that still
  exists from one fixed since the run. A bare `dev` means a local build
  with no commit recorded; then the synthesizer checks HEAD only, and the
  report says so.
- `validate.txt` and `doctor.txt` show what Go already reported. A lesson
  Go already reports is the agent reading past a report, not a missing
  check.

### Sessions and transcripts

```bash
jq -r '.sessions[].session_id' "$W/stats.json"
```

Claude Code keeps each session at
`~/.claude/projects/<slug>/<session-id>.jsonl`. The slug is the project's
absolute path with every `/` and `.` replaced by `-`:
`/Users/me/Work/app` becomes `-Users-me-Work-app`. Subagent transcripts
sit under `<slug>/<session-id>/subagents/`, each `agent-*.jsonl` beside a
`.meta.json` that names its agent type and description.

If the computed slug has no such file, find it by its exact name:
`find ~/.claude/projects -maxdepth 2 -name '<session-id>.jsonl'`. Never
glob or read other projects' transcripts. They are unrelated private
sessions.

### From a transcript alone

The project is the `cwd` on the transcript's user lines, and the recipe
id is the one its `bts recipe` commands name. Then gather as above. If
the transcript names no recipe, review it alone and say so in the
report.

### Digest

A recipe session runs to tens of megabytes; one measured session was
36 MB with 1,031 Bash calls. Reviewers read a digest and go to the raw
file only to check a quote.

```bash
for t in <each session transcript and each subagent transcript>; do
  jq -r -f "$REPO/.claude/skills/reflect/references/digest.jq" "$t" \
    > "$W/digest-$(basename "$t" .jsonl).txt"
done
wc -c "$W"/digest-*.txt
```

`references/digest.jq` keeps one line per event: user messages,
assistant text, every tool call with its main argument, and failed tool
results. It drops successful tool output, which is most of the bytes;
the 36 MB session digests to about 0.8 MB. Pass the sizes to the
reviewers. A digest over about 400 KB is read in ranges with `offset`
and `limit`, not in one Read.

## Step 2: Three reviewers in parallel

One message, three `Agent` calls, `subagent_type: "general-purpose"`,
each with its `model`:

| Lens | `model` | Prompt |
| --- | --- | --- |
| Judgment | `opus` | `references/judgment-reviewer.md` |
| Tooling | `sonnet` | `references/tooling-reviewer.md` |
| Divergent | `opus` | `references/divergent-reviewer.md` |

Each prompt is its file verbatim with the placeholders filled:

- `<PROJECT>`, `<RECIPE_ID>`, `<RECIPE_DIR>`
- `<TEMPLATE_COMMIT>`: the commit from `.template-version`
- `<DIGESTS>`: each digest's path and size
- `<TRANSCRIPTS>`: each raw transcript's path
- `<REPORTS>`: the four files Step 1 wrote
- `<REPO>`: this repository's root

Give paths, not pasted contents. Add nothing of your own about what went
wrong: a reviewer handed a hypothesis tends to return it confirmed, and
three reviewers handed the same one are one reviewer.

Then wait for the three completion notifications. Do not poll; measured
in `06d381e`, polling turns were 62% of a 72-minute wall clock.

## Step 3: Synthesize

One `Agent` call, `subagent_type: "general-purpose"`, `model: "opus"`.
The prompt is `references/synthesizer.md` verbatim, with each reviewer's
full output inlined where marked and `<REPO>` and `<TEMPLATE_COMMIT>`
filled. It reads every target file at HEAD before accepting a row, and
returns Accepted, Rejected and Backlog.

Present its output as returned. If you disagree with a row, say so under
the table; do not rewrite the row.

## Routing

Route each item to the first destination that can hold it. The order is
by enforcement strength: what binds whatever the agent decides comes
before what only asks. The settings file states the reason in its own
header: one measured round was told to batch findings at 6, used 9, "and
nothing could have stopped it".

1. **Go check.** Runs whatever the agent decides. A refusal (non-zero
   exit, hook deny, stop-hook block) beats a report the agent can read
   past.
   - Completion gates: `internal/hook/stop.go`, registered in
     `internal/engine/gate_registry.go`. An overridable gate is listed by
     `bts recipe override list --gates`, and its test checks that.
   - Spawn checks: `internal/hook/pre_tool_use.go`. It sees every
     Agent-tool spawn's `subagent_type`, `prompt` and `model`, and can
     deny one. It does not see `context: fork` skills, so a role it must
     police has to be spawned through the Agent tool.
   - Document checks: `bts verify <file>`, the checkers in
     `internal/engine/*_checker.go`.
   - Recipe checks: `bts validate` (`internal/engine/validator.go`) and
     `bts doctor` (`internal/cli/doctor*.go`).
   - Ledger commands: `bts recipe log` and `bts recipe findings …`
     (`internal/cli/recipe*.go`). The defense batch cap lives in
     `defend-batch` because a cap stated in prose was exceeded.
   - Template invariants: a Go test under `internal/template/` that
     reads the shipped templates. It fires in this repository's CI, not
     in a user's run.

   A Go check needs a signal in something bts already writes: a recipe
   document, a ledger, `metrics.jsonl`, or a hook's input. When the
   signal does not exist, the item is to make bts write it, then check
   it. That is still a Go item.

2. **Enforced settings key.** Only for the threshold of a check that
   exists. A key binds only if Go reads it: a field and default in
   `internal/engine/settings.go`, the code that acts on it, an entry in
   `settingsKeysRead` (`internal/cli/doctor_config.go`), and a comment in
   `internal/template/templates/.bts/config/settings.yaml` without
   `[advisory]`. `bts update` never overwrites a project's settings file,
   so existing projects get the Go default, not the template's value. An
   `[advisory]` key is prose kept in YAML; route it as prose.

3. **Template prose.** For what needs judgment:
   `internal/template/templates/.claude/skills/<skill>/SKILL.md`,
   `agents/<agent>.md`, `rules/<rule>.md`, `commands/bts-recipe.md`.
   Never a deployed copy in a project: `bts update` overwrites it. A
   prose change that alters agent behaviour is a candidate for
   `/template-eval` before it ships.

4. **Docs.** `docs/` (an indicator goes in `docs/bts-flow-metrics.md`,
   with `scripts/bts-monitor.ts`), `README.md` and `llms.txt`. Docs
   explain; they bind nothing.

When an item goes to Go, name the prose it makes redundant too. That
prose should point at the check or go. pstack's principle says it in one
line: "If the fix is structural, only use the structural fix. The
instruction is the symptom." `bts-defend` is the precedent: it states
its batch size once and names the command that enforces it.

## Step 4: Present and wait

Show the synthesizer's full output, then stop. Apply nothing until the
maintainer names rows. A template change reaches every project on its
next `bts update`, and a new Go refusal can block a user's recipe;
neither is this skill's call.

The maintainer may pick a subset and redirect a routing. When a redirect
moves an item from Go to prose, say once what enforcement it gives up,
then follow it.

Backlog items are listed, not filed. The maintainer decides where each
goes.

## Step 5: Apply the picked rows

For each picked row, show the files and what changes in each before
editing. This skill's `allowed-tools` leaves out Edit and Write on
purpose, so every edit still passes the session's permission prompt.

- **Go check or settings key:** the change and a test that fails without
  it. Run `go vet ./...` and `go test -race ./...` (`make test`). A new
  completion gate is registered in `gate_registry.go`.
- **Template prose:** edit the template under `internal/template/`.
- **Docs:** run `go run ./cmd/bts docs check` from the repository root.
  It checks names, links and counted claims with the working tree's
  code.

Never commit, push, run `make install` or run `bts update`. The last two
put the change in front of real projects, which is the step
`/template-eval` and the maintainer's review exist to gate.

## Step 6: Report

Short list, no preamble:

- Applied: `<file>`, one line on what changed, and the test that covers
  a Go change.
- Not applied: the Accepted rows the maintainer did not pick.
- Backlog: one line each, with the mechanism or measurement that would
  settle it.
- Rejected: one line each, with the synthesizer's reason.
- Run read: project, recipe id, template version, sessions, digest sizes.

## Rationale

The `measure:` commits show the method: read a real run, find where the
time, money or correctness went, and change bts so the next run cannot
go there. Done by hand it has one reader, who is also the author of the
templates under review. Three lenses in separate contexts, and a
synthesizer that must read the target before accepting a row, keep the
reading from being the author's own. bts already holds that rule for its
gates: never verify your own output in the same context.

Routing by enforcement strength is the bts part. pstack's reflect ends in
skill edits because prose is what pstack has. bts has a binary that
refuses, and its history is a series of prose rules that were read and
not followed until Go enforced them.
