# Completion gates

Evidence for the gates that decide when a spec may finalize and when a turn
may end. Format and scope: [README.md](README.md).

```
Verification date: 2026-08-01
Version:           v0.12.0-1-g9032fe5 (working tree, uncommitted)
Platform:          darwin/arm64, go1.26.1
```

## G1 — A verify round records the budget it was judged under

The convergence verdict is recomputed over a document's whole history from
whatever `verify.max_iterations` says **today** (`internal/cli/recipe.go`,
`internal/cli/recipe_findings.go`). Without a per-round stamp the log cannot
say which regime produced a given `status`, and a stored `failed` written
under one budget can disagree with a fresh evaluation under another.

Guarantee: every logged round carries its budget, and a budget change is
announced rather than silently re-judging history.

```
$ jig recipe log r-ev --iteration 1 --critical 2 --doc draft.md
Logged iteration 1 [draft.md, full pass]: critical=2 major=0 minor_resolvable=0 minor_deferred=0 → continue (budget=3)

# settings.yaml: verify.max_iterations 3 → 6
$ jig recipe log r-ev --iteration 2 --critical 2 --doc draft.md
[jig] note: verify.max_iterations changed 3 → 6 since the last round of draft.md. Earlier rounds were judged under the old budget; the convergence verdict is recomputed from the current one.
Logged iteration 2 [draft.md, full pass]: critical=2 major=0 minor_resolvable=0 minor_deferred=0 → continue (budget=6)
Convergence: 1/6 rounds without progress (best so far: critical=2 major=0 minor_resolvable=0)

$ cat .jig/specs/recipes/r-ev/verify-log.jsonl
{"iteration":1,"critical":2,"major":0,"doc":"draft.md","full_pass":true,"status":"continue","budget":3,"agent_evidence":"none","timestamp":"2026-08-01T11:54:28Z"}
{"iteration":2,"critical":2,"major":0,"doc":"draft.md","full_pass":true,"status":"continue","budget":6,"agent_evidence":"none","timestamp":"2026-08-01T11:54:28Z"}
```

The settings template no longer advertises `require_zero_critical`,
`require_zero_major`, or `allow_minor`. Nothing ever read them; the
completion gate is fixed in `internal/hook/stop.go`. Asserted by
`TestSettingsTemplate_HasNoInertConvergenceKnobs`.

```
$ go test ./internal/state/ -run 'BudgetDrift|BudgetRoundTrips' -v
--- PASS: TestVerifyLogEntry_BudgetRoundTrips (0.00s)
--- PASS: TestBudgetDrift (0.00s)
    --- PASS: TestBudgetDrift/empty_log (0.00s)
    --- PASS: TestBudgetDrift/legacy_entries_record_no_budget (0.00s)
    --- PASS: TestBudgetDrift/unchanged_budget_is_not_drift (0.00s)
    --- PASS: TestBudgetDrift/lowered_budget_drifts (0.00s)
    --- PASS: TestBudgetDrift/raised_budget_drifts (0.00s)
    --- PASS: TestBudgetDrift/most_recent_recorded_budget_wins (0.00s)
    --- PASS: TestBudgetDrift/skips_legacy_tail_to_reach_the_last_recorded_budget (0.00s)
    --- PASS: TestBudgetDrift/disabled_budget_never_drifts (0.00s)
ok  	github.com/imtemp-dev/claude-jig/internal/state	0.178s
```

## G2 — A turn cannot end with the recipe's own records inconsistent

Before this, the Stop gate ran only on an explicit `<jig>DONE</jig>` marker;
a turn that simply never said DONE was never checked at all.

Guarantee: the backstop blocks three specific states — convergence failed, a
verification produced but never logged, a verified document edited after its
verification — and blocks nothing else. Open findings mid-loop are the normal
state and must stay stoppable.

```
$ go test ./internal/hook/ -run 'BlindStop|StopBlockBudget' -v
--- PASS: TestBlindStop_OpenFindingsAlone_Allows (0.00s)
--- PASS: TestBlindStop_ConvergenceFailed_Blocks (0.00s)
--- PASS: TestBlindStop_UnloggedVerification_Blocks (0.00s)
--- PASS: TestBlindStop_VerificationWithNoLog_Blocks (0.00s)
--- PASS: TestBlindStop_DirtyVerifiedDoc_Blocks (0.00s)
--- PASS: TestBlindStop_ImplementPhase_Allows (0.00s)
--- PASS: TestBlindStop_NoActiveRecipe_Allows (0.00s)
--- PASS: TestStopBlockBudget_StandsDownAfterThreeIdenticalBlocks (0.00s)
--- PASS: TestStopBlockBudget_ProgressResetsCount (0.00s)
--- PASS: TestStopBlockBudget_AllowClearsCounter (0.00s)
--- PASS: TestStopBlockBudget_AppliesToDonePath (0.00s)
--- PASS: TestBlindStop_OpenDecisionAllowsTurnEnd (0.00s)
ok  	github.com/imtemp-dev/claude-jig/internal/hook	0.413s
```

Loop bound: the same block reason may fire at most
`state.DefaultStopBlockBudget` (3) times consecutively, below Claude Code's
own 8-block override, after which jig stands down with a visible message
naming the unresolved issue. Progress changes the reason text and restarts
the count, so a model that is actually fixing things never exhausts it.
Covered by the three `TestStopBlockBudget_*` cases above.

## G3 — A question for the user survives the session that raised it

`[CONVERGENCE FAILED]` told the loop to "ask the user for guidance", and the
question and answer then lived only in the conversation. A recipe waiting on
a person was indistinguishable from one still working.

Guarantee: a held decision blocks completion, is announced on resume, is
reported by `jig doctor`, and shows in the status line; and the ledger
refuses both a key collision and a fabricated answer.

```
$ jig recipe decision hold r-smoke --key token-storage \
    --question "Refresh tokens in keychain or httpOnly cookie?" \
    --option keychain --option cookie --doc draft.md
Decision "token-storage" held. r-smoke is blocked until it is resolved:
  jig recipe decision resolve r-smoke token-storage --answer "..."

$ jig recipe decision list r-smoke
KEY            STATUS  DOC       QUESTION
token-storage  open    draft.md  Refresh tokens in keychain or httpOnly cookie?

# Idempotent for an exact repeat — a retried skill step does not
# multiply the ledger.
$ jig recipe decision hold r-smoke --key token-storage \
    --question "Refresh tokens in keychain or httpOnly cookie?"
Decision "token-storage" already open — unchanged.

# A key collision is refused rather than silently overwriting the
# original question.
$ jig recipe decision hold r-smoke --key token-storage --question "a different question"
Error: decision "token-storage" is already open with a different question ("Refresh tokens in keychain or httpOnly cookie?"). Use a new key for a new decision, or resolve the existing one first

$ jig recipe decision resolve r-smoke token-storage --answer "httpOnly cookie"
Decision "token-storage" resolved: httpOnly cookie

$ cat .jig/specs/recipes/r-smoke/decisions.jsonl
{"key":"token-storage","doc":"draft.md","question":"Refresh tokens in keychain or httpOnly cookie?","options":["keychain","cookie"],"status":"open","timestamp":"2026-08-01T11:50:35Z"}
{"key":"token-storage","doc":"draft.md","question":"Refresh tokens in keychain or httpOnly cookie?","status":"resolved","answer":"httpOnly cookie","timestamp":"2026-08-01T11:50:35Z"}
```

Gate integration is covered by `TestSpecDone_OpenDecisionBlocksCompletion`,
`TestSpecDone_ResolvedDecisionDoesNotBlock`,
`TestBlindStop_OpenDecisionAllowsTurnEnd`,
`TestSessionStart_SurfacesOpenDecisions`, and
`TestSessionStart_NoDecisionsNoNotice`.

## G4 — Gate evidence is recorded, and its absence is not an accusation

The gate skills carry `context: fork` and spawn their own agent, so the
isolation is harness-enforced. What was untied is the record: the
orchestrator writes `verification.md` and runs `jig recipe log`, and nothing
connected that write to a fork having run.

Guarantee: each round records whether a subagent finished since the previous
round, and `jig doctor` reports an evidence-free round **only** when the same
recipe has also produced rounds with evidence — the one configuration in
which absence is informative rather than a missing signal.

The `agent_evidence` field is visible in the G1 log excerpt above. Both
values were produced by the real binary; `"none"` there is correct, since
that fixture ran no agents.

```
$ go test ./internal/metrics/ ./internal/cli/ -run 'SubagentActivity|GateEvidence' -v
--- PASS: TestSubagentActivitySince_DetectsStopAfterCutoff (0.00s)
--- PASS: TestSubagentActivitySince_IgnoresStopBeforeCutoff (0.00s)
--- PASS: TestSubagentActivitySince_OtherKindsAreNotEvidence (0.00s)
--- PASS: TestSubagentActivitySince_ZeroCutoffMatchesEverything (0.00s)
--- PASS: TestSubagentActivitySince_EmptyLogIsReadable (0.00s)
--- PASS: TestCheckGateEvidence_NoEvidenceAnywhereIsSilent (0.00s)
--- PASS: TestCheckGateEvidence_MixedReportsTheBareRounds (0.00s)
--- PASS: TestCheckGateEvidence_AllObservedIsSilent (0.00s)
--- PASS: TestCheckGateEvidence_LegacyRoundsAreNotBare (0.00s)
```

**Known limitation.** Whether Claude Code emits `SubagentStop` for a
`context: fork` skill invocation (as opposed to an explicit `Agent` call) has
not been confirmed on a live session. The self-calibrating rule is what makes
that uncertainty safe: if the harness never emits the event, every round is
`"none"`, no round is `"observed"`, and the check reports nothing. Confirming
the live behavior would upgrade this from evidence to proof; until then it is
labelled evidence deliberately.

## Whole suite

```
$ go test -race ./...
?   	github.com/imtemp-dev/claude-jig/cmd/jig	[no test files]
ok  	github.com/imtemp-dev/claude-jig/internal/cli	3.069s
ok  	github.com/imtemp-dev/claude-jig/internal/comment	(cached)
ok  	github.com/imtemp-dev/claude-jig/internal/engine	1.493s
ok  	github.com/imtemp-dev/claude-jig/internal/hook	3.455s
ok  	github.com/imtemp-dev/claude-jig/internal/metrics	2.868s
ok  	github.com/imtemp-dev/claude-jig/internal/state	2.599s
?   	github.com/imtemp-dev/claude-jig/internal/statusline	[no test files]
ok  	github.com/imtemp-dev/claude-jig/internal/template	(cached)
?   	github.com/imtemp-dev/claude-jig/pkg/version	[no test files]
```
