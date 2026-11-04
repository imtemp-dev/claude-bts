# Design: verdicts that name what they judged

Status: design proposal, nothing implemented.  Written 2026-11-04.  It covers two items from [`pstack-lessons.md`](pstack-lessons.md): "Tree hash" and "Falsifier for code-phase criticals".  Code facts below were mapped on branch `feat/architect-runners`.

## Where bts stands

- `test-results.json` (`state.TestResults`, `internal/state/recipe.go:82-100`) records status, counts, exit code, command and `recorded_by`, and no identity of the code it ran on.  `bts test run` (`internal/cli/test_run.go:86-175`) is its only Go writer, and status comes from the exit code alone.
- The implement gate, `handleImplementDone` (`internal/hook/stop.go:578-705`), checks that tests passed and that `review.md` exists.  It never reads `review.md`, and it does not call `DirtyVerifiedDocs`, which spec gates use to refuse a document changed after verification.
- `review.md` is prose the model writes (`bts-review/SKILL.md:211-265`).  No Go code parses it.
- The reviewer agents have `Read, Grep, Glob` only.  The pre-tool-use hook cannot tell which agent made a Bash call: `HookInput` has no agent type, only `AgentID` on subagent start and stop.
- The spec side already has the precedent: `ContentHash` (sha256 over bytes with CRLF normalised, `internal/state/verify_snapshot.go:62-85`) recorded as `doc_hash` per verify round, and `DirtyVerifiedDocs` comparing it at the gate.  Go shells out to git in one place only (`internal/comment/parser.go:298`).

## A. Tests and review name the code they judged

### Proposal

1. **Implementation hash.**  A new `state.ImplHash(root, recipeID)`: sha256 over the sorted list of `path NUL ContentHash(file)` for every file a task anchor names (`create`, `modify`; a `delete` anchor contributes `path NUL absent`), plus the test files in `test-results.json`.  It reuses `ContentHash`, needs no git and no commit (some recipes run in repositories with no commits yet), and ignores churn outside the recipe, such as `.bts/local/`.
2. **Recorded by Go, never by the model.**  `bts test run` writes `impl_hash` into `test-results.json`.  `bts recipe log --action review` computes it and stores it on that changelog entry, the same way `--doc` stores `doc_hash`.
3. **Gate.**  `handleImplementDone` recomputes the hash after the existing checks.  If the test result's `impl_hash` differs, it blocks with "tests ran on different code: rerun `bts test run`".  A missing field (a recipe started before this change) passes, and `bts doctor` warns about it.  `handleFixDone` gets the same test check.
4. **Override.**  Register the check in `gate_registry.go` as an overridable gate, like the other findings gates, so an operator can record a deliberate exception.

### The open choice: does the review need the same stamp?

`bts-implement` tells the loop to fix every actionable critical and major finding after the review (`bts-implement/SKILL.md:420-433`).  A review stamp that must match the final tree would force a second full review (three reviewers, validator, rebuttal) after every fix round.

- **A1 (recommended first): tests only.**  Re-running tests after a fix is cheap, and it is the check that catches "fixed the finding, broke something else".  The review entry still records its hash, so a later slice can measure how often code changed after review.
- **A2: tests and review.**  The review must match too, with a delta re-review (changed files only, no rebuttal) as the cheap path after fixes.  More protection, more cost, and a new review mode to build.

### Tests

The hash is order-independent and CRLF-insensitive, it changes when an anchored file or a test file changes, and a deleted file differs from an empty one.  The gate blocks on a mismatch, passes on a missing field, and honours an override.

## B. A code-review critical needs a run

### Proposal

1. **Reviewers propose; the orchestrator runs.**  A reviewer that files a critical adds a falsifier: a command, the output it expects to see, and the output correct code would give.  The orchestrator, which already has Bash, runs each critical's command once in the main tree after collecting findings and records the real output.  Reviewers keep `Read, Grep, Glob`: Bash cannot be confined per agent today, and a reviewer running commands concurrently in one tree risks writes.
2. **Security criticals may use a scenario instead.**  Input, path, outcome, which is the rebuttal agent's existing bar (`bts-reviewer-rebuttal.md:28-33`).  Security review is recall-bound, and the flaws it exists to catch are the hardest to show with one command.
3. **Unproven means downgraded, not dropped.**  A critical whose command does not show the failure becomes major, with the run output attached.  Majors and below keep today's rules.
4. **A machine-readable block in `review.md`.**  A new `<bts-review-findings>` JSON block: per finding `id`, `severity`, `title`, `anchor` (`file:line`), and for criticals an `evidence` object (`kind: run|scenario`, `command`, `expected`, `observed`).  A new block rather than `<bts-findings>`, because that parser's severity vocabulary (`minor_resolvable`, `minor_deferred`) and count rules belong to spec verification.
5. **Go checks it.**  A parser beside `ParseFindingsBlock`, called from `bts recipe log --action review --from-review review.md` (which records the counts) and from `handleImplementDone`: a critical with no evidence, or with `kind: run` and an empty `observed`, blocks.  It is overridable, like the other findings gates.

### Cost

One orchestrator Bash call per code-review critical.  Criticals are the rarest severity, so this is bounded.  No new agents and no new rounds.

### Tests

Parser accepts a valid block and rejects a critical without evidence, a `run` with no `observed`, and an unknown severity.  The gate blocks on each rejection and passes when `review.md` predates the block (no block present), with a doctor warning.

## Open decisions

1. A1 (tests only) or A2 (tests and review) for the tree stamp.
2. Whether B's orchestrator-runs design is acceptable, or reviewers should get Bash despite the confinement gap.
3. Order: A1 is small and independent; B touches the review skill, three agent prompts, a new parser and the gate.  Proposed order: A1, then B.
