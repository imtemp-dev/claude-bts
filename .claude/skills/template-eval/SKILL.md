---
name: template-eval
description: >
  Test whether a template change (a skill, agent or rule edit under
  internal/template/templates/) changes agent behaviour before it ships
  with `bts update`. Builds the baseline ref and the working tree into
  separate binaries, runs the same organic task as Opus in sanitized
  scratch projects, and has a fresh-context Opus judge score the outputs
  under blind labels, with cost and turns per run beside the verdict.
  Maintainer tool for the claude-bts repository; never shipped by
  `bts init`.
user-invocable: true
disable-model-invocation: true
allowed-tools: Read Write Grep Glob Bash Agent
argument-hint: "\"what changed\" [--base <git-ref>] [--runs <n>]"
effort: high
---

# Template eval

Find out whether a template change changes what the agent does, and what
it costs, before `bts update` puts it in front of every project:
$ARGUMENTS

## Where this sits

This skill lives in the claude-bts repository's own `.claude/skills/`,
never under `internal/template/`. It writes only under one scratch
directory and never touches the maintainer's real projects.

`disable-model-invocation` is set because one eval runs several headless
Opus sessions, each a real recipe step; one simulate fork was measured
at $12–30. It runs when the maintainer types `/template-eval`.

Adapted from pstack's eval playbook. One of its rules cannot be kept:
pstack takes the judge from another model family, and here every seat is
Claude. Independence comes from a fresh context instead, which is the
rule bts already applies to its own gates ("Never verify your own output
in the same context", `bts-verification-protocol.md`), plus blind labels
and a rubric the candidates never see.

## Models

- Candidates: `claude -p --model opus`, one headless session per run.
- Judge: one `Agent` spawn, `subagent_type: "general-purpose"`,
  `model: "opus"`, in a fresh context.
- Never `fable`, on any seat. It is rate-limited, and its one role in bts
  is the architect tie-break. Step 6 checks the transcripts for it.

## Blinding rules

From pstack's playbook. Each exists because an agent that knows it is
measured writes to the measure.

- No `eval`, `test`, `judge`, `experiment`, `rubric`, `score`, `compare`,
  `benchmark`, `candidate`, `arena`, `baseline` or `variant` in any
  directory, file name or prompt this skill creates for a candidate. A
  candidate sees its absolute working directory, so the rule covers every
  path component, not just the project name. The deployed templates use
  some of these words; that is the product, not a leak.
- The prompt is what a bts user would type at that point: a slash
  command with its argument, or a plain request. It states the goal. It
  never names the measurement or the change.
- No chain-eliciting cues. Never ask a candidate which skills or rules it
  followed. Grade from what its transcript shows it read and did.
- Every run gets the same project name and the same prompt. Only the
  random parent directory and the binary differ.
- A candidate is never told others exist. Each is its own headless
  session in its own directory.
- The judge knows it is judging. It sees outputs by random label only:
  never a variant name, a git ref, a binary path, or what changed. Labels
  are shuffled across both sets, so the judge cannot group them.
- One judge scores every run of both variants in one pass, on one scale.

## Step 1: Frame

1. Find the change from this repository's root:
   `git diff --stat <base> -- internal/` and read the diff. The base
   defaults to `HEAD`; if the working tree has no template change against
   it, ask the maintainer for the base ref. A Go change in the same diff
   is part of the variant, because the binary carries both; say so in
   the report.
2. If Go changed, run `go test ./...` first. A binary that does not build
   or pass wastes every run.
3. Write the variant under test in one sentence: what changed, and which
   behaviour it should change.
4. Write the rubric: 3 to 6 criteria, each observable in the files a run
   writes or in its transcript (a file exists with a field; the agent read
   X before writing Y; a gate passed on the first try). Include one
   criterion the change could make worse, so the rubric can show a
   regression and not only the hoped-for gain. Leave cost out: it is
   measured, not judged (Step 6).
5. Save both to `$S/ctl/frame.md` (Step 3 makes `$S`) before any run. A
   rubric written after the runs fits what happened.

## Step 2: Choose the task and where it starts

- **From the start:** a small project and the recipe command a user would
  type. Use it when the change is in an orchestrator skill or early in
  the flow. It is costly: a full blueprint recipe runs for hours.
- **From a fixture:** the recipe state the changed step starts from. Use
  it when the change is one step, such as `bts-architect` Step 1. Build
  it from a finished recipe: copy the project's source and its
  `.bts/specs/recipes/<id>/`, delete the changed step's outputs and every
  ledger line recorded after the step began, and set `recipe.json`'s
  `phase` to the phase the step starts in. Never copy `.bts/local/`
  (another run's metrics and traces) or the project's `.claude/` (`bts
  init` writes the variant's own). Rename anything that carries a banned
  word.

A headless run cannot answer `AskUserQuestion`. Choose a start and an end
with no human gate between them. A run that stops at a question is
recorded as stopped there, never rerun with a reworded prompt.

The run ends when the changed step has written its outputs and the
session exits. Write the prompt so that is where a user's request would
end.

## Step 3: Build and set up

From this repository's root:

```bash
S=$(mktemp -d "${TMPDIR:-/tmp}/ws.XXXXXX")        # scratch root; nothing goes outside it
mkdir -p "$S/ctl/runs" "$S/ctl/view"

# Base: the ref, extracted without touching this repository's index or worktrees.
mkdir "$S/ctl/src" && git archive <base> | tar -x -C "$S/ctl/src"
B1="$S/$(openssl rand -hex 4)/bin"; mkdir -p "$B1"
(cd "$S/ctl/src" && go build -o "$B1/bts" ./cmd/bts)

# Change: the working tree. go:embed takes the templates as they are on disk.
B2="$S/$(openssl rand -hex 4)/bin"; mkdir -p "$B2"
go build -o "$B2/bts" ./cmd/bts

printf 'base %s\nchange %s\n' "$B1" "$B2" > "$S/ctl/key.txt"
```

Shell variables do not survive from one Bash call to the next. `key.txt`
holds every value a later step needs; write them out as literal paths in
each command.

Build with plain `go build`, not `make build`. Without ldflags both
binaries report version `dev`, so `bts --version` prints the same in
every run, and the session-start hook never redeploys templates (it does
whenever the binary's version differs from `.bts/config/.template-version`).

For each run (Step 4 sets how many per variant), with `BIN` set to `$B1`
or `$B2`:

```bash
L=<label>                                         # random, e.g. k7; never A1/B2
SID=$(uuidgen | tr 'A-Z' 'a-z')                   # names the run's transcript in advance
R="$S/$(openssl rand -hex 4)"; P="$R/<project-name>"   # same project name in every run
mkdir -p "$P" && <copy the project source into "$P">
(cd "$P" && PATH="$BIN:$PATH" bts init)
<copy the fixture recipe into "$P/.bts/specs/recipes/<id>/">
(cd "$P" && git init -q && git add -A \
  && git -c user.name=dev -c user.email=dev@localhost commit -qm "initial")
(cd "$P" && PATH="$BIN:$PATH" bts recipe status)  # the expected recipe and phase
(cd "$P" && find .bts/specs -type f | sort | xargs shasum | shasum) > "$S/ctl/runs/$L.fixture"
(cd "$P" && shasum <each changed template file, under .claude/>)   > "$S/ctl/runs/$L.sha"
echo "$L $BIN $P $SID" >> "$S/ctl/key.txt"
```

`bts init` comes before the fixture copy because it refuses a project
that already has `.bts/`. The fixture hash must match across runs: every
run starts from the same bytes.

Labels are two random characters and a digit, assigned now and recorded
only in `key.txt`. A label like `A1` tells the judge which set a run
belongs to.

The `PATH` prefix is not optional. Every bts hook script runs the first
`bts` on `PATH`. If that is the maintainer's installed binary, its
session-start hook sees a different template version and force-deploys
its own templates over the scratch project, and the run silently tests
the installed version instead. The template hashes catch it afterwards
(Step 6).

## Step 4: Smoke run, then the plan gate

Before the real runs, prove the harness with one throwaway run: a fresh
scratch project set up as above, a one-line prompt ("What state is this
project in?"), and the flags from Step 5. Check:

- `.bts/local/metrics.jsonl` in that project has a `session_start` event,
  so the hooks loaded under these flags;
- the JSON result has `total_cost_usd` and `num_turns`;
- the transcript is where Step 6 will look for it;
- whether the project's MCP server (Context7, from `.mcp.json`) loaded.
  If it did not, the evidence steps fall back to WebFetch in every run
  alike, which matters only when the change touches research.

Then show the maintainer the plan and wait for an explicit go: the
variant sentence, the rubric, the task and its start, the prompt, the
runs per variant, the budget cap per run, the permission flags, and an
estimated total (runs times the step's cost in a past recipe's
`bts stats`). The runs spend real money, and the permission flags decide
what an unattended Opus session may do on this machine.

One run per variant is a sample, not a measurement. bts measured this on
its own gates: two verify rounds over a byte-identical document disagreed
by two criticals and nine majors. Recommend at least three runs per
variant when the decision matters. One each shows only whether the
change does anything at all. `--runs` sets the number; without it,
propose three at the gate.

## Step 5: Run

Launch every run in one message, each as a background Bash command, so
they run in parallel:

```bash
cd "$P" && PATH="$BIN:$PATH" claude -p "$(cat "$S/ctl/prompt.txt")" \
  --model opus \
  --session-id "$SID" \
  --output-format json \
  --setting-sources project,local \
  --permission-prompts none \
  --max-budget-usd <cap> \
  <permission flags approved at the gate> \
  > "$S/ctl/runs/$L.json" 2> "$S/ctl/runs/$L.err"
```

- `--session-id` takes the UUID recorded for the label, which names the
  transcript in advance.
- `--setting-sources project,local`: bts registers its hooks in
  `.claude/settings.local.json`, so `local` must stay. Leaving out `user`
  keeps the maintainer's own hooks and permissions out of every run. If
  the CLI needs user settings to start (a provider `env`), keep `user`
  and note it in the report.
- Never `--bare`. It skips hooks, and the hooks are half of what bts is.
- `--permission-prompts none` denies anything that would prompt, instead
  of waiting on a host that is not there. bts steps run Bash and write
  files, so the run needs `--permission-mode acceptEdits` plus an
  `--allowedTools` list naming the commands the step runs (`bts`, `git`,
  the project's build and test tools), or
  `--dangerously-skip-permissions` inside a sandbox only. Every run gets
  the same flags. Denials show in the transcript (Step 6).
- `--max-budget-usd`: a run that hits the cap is a result. Record where
  it stopped; do not discard it.

Then wait for the completion notifications. Do not poll; measured in
`06d381e`, polling turns were 62% of a 72-minute wall clock.

## Step 6: Collect each run

```bash
jq '{cost: .total_cost_usd, turns: .num_turns, ms: .duration_ms, subtype, is_error}' "$S/ctl/runs/$L.json"
(cd "$P" && PATH="$BIN:$PATH" bts stats <id> --json) > "$S/ctl/runs/$L.stats.json"
T=$(find ~/.claude/projects -maxdepth 2 -name "$SID.jsonl")   # subagents: ${T%.jsonl}/subagents/*.jsonl
```

**Cost and turns.** `bts stats <id> --json` gives sessions, duration, the
phase timeline, and calls and failures per tool. Its cost comes from
token snapshots that only the statusline writes
(`internal/statusline/statusline.go`), and a `claude -p` run draws no
statusline, so expect no cost there. It has no turn count either. Take
cost from `total_cost_usd` and turns from `num_turns` in the run's JSON
result, and record which source each number came from.

**What it read and did,** from the session and each subagent transcript:

```bash
jq -r 'select(.type=="assistant") | .message.content[]? | select(.type=="tool_use")
  | [.name,
     (.input.file_path // .input.command // .input.skill // .input.pattern // .input.description // ""),
     (.input.subagent_type // ""), (.input.model // "")]
  | @tsv' "$T"
```

One line per call: tool, main argument, and for an `Agent` spawn its
`subagent_type` and `model`.

Reads also happen through Bash (`cat`, `sed -n`, `grep`), so read the
command column, not only `Read`. Note the skills invoked, the agents
spawned and their models, every gate block and what the agent did next,
and every permission denial.

**Void a run,** report why, and rerun it under a new label when:

- an assistant message came from a model other than Opus or a Sonnet
  seat a template assigns (`jq -r 'select(.type=="assistant") | .message.model'`
  over the session and its subagents);
- it read or ran anything under `$S` other than its own `$P` and its own
  binary, or anything in this repository: it may have seen the other
  variant or the control files;
- a changed template file's hash under `$P/.claude/` differs from
  `$L.sha`: another binary redeployed templates during the run.

A voided run never reaches the judge.

**Prepare the judge's view.** Copy the files the step wrote into
`$S/ctl/view/$L/`, and the tool-call list above as
`$S/ctl/view/$L/actions.txt`. Leave tool results out: they include the
text of the skill the candidate read, which names the variant to anyone
who knows the diff.

## Step 7: Judge

One `Agent` call, `subagent_type: "general-purpose"`, `model: "opus"`.
The prompt is `references/judge.md` with `<PROMPT>` (what the candidates
were asked), `<RUBRIC>`, `<VIEW_DIR>` (`$S/ctl/view`) and `<LABELS>`
(shuffled) filled. Nothing else: not the diff, not the
variant sentence, not which labels belong together. A judge that knows
the hypothesis looks for it.

Wait for its notification.

## Step 8: Read everything yourself

Read every run's outputs and `actions.txt` end to end, then the judge's
scores. Unblind with `key.txt` only after that.

Where you disagree with the judge, name the criterion and the label, and
keep both readings in the report. A disagreement means the rubric is
ambiguous or one reader is biased, and the maintainer should see which.
You set the runs up and know the variants; that is why you are not the
judge.

## Step 9: Variance and cost

Per variant: each criterion's mean and range; cost, turns and wall clock,
mean and range; tool calls and failures. When the difference between the
variants falls inside either variant's range, the result is "no
measurable difference at n = …", not a win.

Cost weighs as much as behaviour. A change that holds the behaviour and
costs less is worth promoting. One that improves behaviour says what the
improvement costs.

## Step 10: Report

```markdown
## Template eval: {variant sentence}

Base: {ref}. Change: working tree ({files}). Runs: {n} per variant.
Task: `{prompt}`, from {start}. Flags: {permission and setting flags}.

### Rubric
1. …

### Runs
| Label | Variant | C1 | C2 | … | Cost | Turns | Wall | Tool calls (failed) | Ended | Notes |
|---|---|---|---|---|---|---|---|---|---|---|
| k7 | base | 4 | 3 | … | $8.10 | 62 | 18m | 140 (3) | done | read the skill once; gate passed first try |

Voided: {label, reason}, or none.

### Judge's verdict
{its ranking and per-label summary, by label}

### Synthesis
{what the change did to behaviour and cost; where you and the judge
disagree, with both readings; the variance}

### Recommendation
Promote | Do not promote: {the evidence, n, and what result would change the call}

Scratch: {$S}, kept. Transcripts: ~/.claude/projects/{slug of each run}.
```

Promotion is the maintainer's decision. This skill never commits,
pushes, runs `make install` or runs `bts update`. Removing `$S` and the
runs' transcript folders is the maintainer's call too.

## Rationale

The `measure:` commits show how bts changes: each template change is
justified by what a real run did and cost. A change read by its author
and tried once on the author's own project is an anecdote. Two binaries,
the same prompt in identical scratch projects, and a judge that cannot
tell the variants apart turn it into a comparison, and the repeat runs
say how much of the difference is noise. Cost sits beside behaviour
because every bts change so far has been judged on both.
