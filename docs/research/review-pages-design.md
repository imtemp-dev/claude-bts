# Review pages: the user reviews a recipe on an artifact

Status: approved 2026-11-13 with the recommended answers to the open questions; R1 built the same day (see "R1 as built"), not yet released.

## The problem

A recipe asks the user to review four times: intent, scope, the build spec before implementation (`code-edit-go`), and decisions held on the way.  Each review is an AskUserQuestion with a few lines of text over documents of about 1,700 words that the user does not read.  The Reports 2/4 PR 3 recipe (`r-001-report-contract`, 2026-11-12) showed the cost: scope was confirmed at 20:40 and then re-confirmed twice after implementation had started, once for the PR 3/PR 4 boundary (two invariants added, tasks and tests redone) and once for chart size caps the code review found.  Both were boundary questions a picture of the scope would have put in front of the user before any code.

The user understands a change fastest from a visual explainer of the kind they already ask for after a pull request (the PR 2 explainer: where it fits, the shape with this PR solid and later work dashed, one run step by step, case pickers).  This design moves that page to the front of the recipe and makes it the place where the user reviews and answers.

## Decisions the user made (2026-11-13)

- Every user review in a recipe can happen on an artifact page: intent, scope, the build spec and decisions.
- Feedback is a note pinned on the spot it is about, typed or dictated, the way Claude Code takes voice.
- Notes are collected and sent together, one round at a time, because one round is cheaper in context than a note at a time.
- bts must know the feedback: every round is recorded in the recipe.
- The answer comes from the session doing the work, in its terminal and on each note's pin.  The claude.ai comment thread a round creates is only a receipt; its automatic reply knows nothing of the work.
- A note does not change the plan.  Claude turns change requests into proposals; the user accepts or rejects each; a separate Update plan applies the accepted ones and moves the document's version; Confirm signs a version.
- The page shows a round's progress: sent, picked up by the session, answered.

## The pilot

Prototype: https://claude.ai/artifact/NTNNayeTJBddAvmoiHTrgd, built from PR 3's intent and scope as they stood at 20:36 on 2026-11-12, run against copies in this session's scratchpad (`intent-proto/recipe-copy/`, feedback records `feedback/round-1..6.json`).

| Round | Content | Sent → session notified → answered |
|---|---|---|
| 1 | 4 notes (2 tests, 1 wording question, 1 progress question) | 02:51:27 → 02:53:38 → 02:53 (answered before the notice, on the user's prompt) |
| 2 | "what is this box, why is it here" | 02:59:27 → ~03:00 → 03:01 |
| 3 | "are the notes arriving, is the session answering" | 03:01:36 → 03:02:15 → 03:02:30 |
| 4 | progress display test | 03:09:01 → 03:09:40 → picked 03:09:45 → 03:10:00 |
| 5 | "move the file-claim rule to PR 3" → proposal p-5-1 (a split: the rule moves, declined outcomes stay) | 03:17:42 → 03:18:15 → 03:18:40 |
| 6 | Update plan with p-5-1 accepted → scope v2 (`677f4fa26175` → `f7a7d77d01d2`) | 03:28:49 → 03:29:15 → 03:30:30 |

What the pilot established:

- The channel works end to end with no terminal input: notes in the page's data, one `sendToClaude` comment per round, the platform's auto-reply, then a task notification to the watching session 30 to 130 seconds later.
- The notification to the main session comes only while auto-replies are armed, and the auto-reply always posts first.  The round message must therefore carry no content to answer: a header, the note ids and "no reply is needed in this thread".  With content in the message (round 1) the auto-reply answered the notes from the page alone and got the progress question wrong.
- The first page-written comment needs the viewer's consent; a dismissed prompt is not shown again in that page load (`consent_required`).  The page needs an Allow control (`permissions.request(["comments"])`).
- Dictation (`dictation` capability) worked in Korean on the first try.
- Two of six rounds asked what a diagram box meant: boxes named after code ("typed, harness-facing", "pure functions") are not understood.  A box says what it holds and who fills it.
- One round asked whether the notes had arrived: the page must show progress between Send and the answer.
- The Accept buttons were missed below the fold: pending proposals need a control in the toolbar.

## Design

### Where each review happens

One living page per recipe, published at the start and grown at each review point.  Its URL is recorded in the recipe.

| Review point | Page section | Confirm records |
|---|---|---|
| Intent (discovery) | Where it fits; today and after, drawn (a mock of the output when there is one); the main scenario step by step; key decisions | `intent-confirm`, pinned to intent.md's hash |
| Scope (scoping) | The shape, this item solid, later items dashed, out greyed; lanes for neighbouring items; open decisions as option cards drawn side by side; assumptions and hard parts | `scope-confirm`, pinned to scope.md's hash |
| Build spec (finalize) | Components and paths from the wireframe; each invariant pinned to its owner on the drawing; falsifiers; boundary contracts; Known Uncertainties | `code-edit-go`, pinned to final.md's hash |
| A decision held mid-recipe | An option card section, options drawn when they differ in shape | the decision's resolution |

Later sections do not replace earlier ones; a reader scrolls from why to what to how.  At completion the same page can become the pull request explainer (a later slice, see S5 in `multica-integration-design.md`).

### What Claude builds and what bts ships

The drawings are bespoke: the page is worth having only because it draws this change, so a fixed template would lose what the user reads it for.  Everything that is the same on every page is shipped by bts as a kit:

- `bts-review-kit.js` and `bts-review-kit.css`, deployed by `bts init` beside the skill and published with the page as supporting files.  The kit owns note mode (N, click a `[data-note]` element), the note editor with dictation, pins, the Review tray, Send, round progress, the proposals panel with Accept and Reject, Update plan, Confirm rows, the Allow control, and the fallbacks (no page data: notes stay in the browser and Send carries them in the message; no comments: say so).
- The page marks its reviewable parts with `data-note="<section> › <part>"` labels and its confirm rows with `data-confirm`, `data-ver` and `data-hash`; the kit does the rest.
- The page declares `comments`, `dictation` and `db` with rules for `notes`, `rounds` and `proposals` (read view, write admin).  `bts review page` prints the exact declaration.

A new skill, `/bts-review-page`, holds the page's design rules: the section list per review point, the drawing vocabulary (shape with solid and dashed, step-through, lanes, option cards drawn side by side, invariants on the drawing), the box rule above, English page text, and the loop below.

### The loop

```
user: N → click → speak or type → Save           (notes/<id>: draft)
user: Send N notes                                (notes: submitted, rounds/r-N: submitted, one comment: receipt)
session, on the notification or the next prompt:
  rounds/r-N → picked
  bts review import <id> --round N --dir <ArtifactData out_dir>
  questions → answered on the pin and in the terminal
  change requests → proposals (bts review propose), pins → proposed
  page wording → fixed on the page directly (not the plan)
  rounds/r-N → processed
user: Accept / Reject each proposal → Update plan (rounds/r-M: kind apply)
session:
  edits the plan files for the accepted proposals only
  bts review apply <id> --round M   (records versions, hashes, proposal links)
  republishes the page at the new version; proposals → applied
user: Confirm <doc> vN → session: bts recipe confirm <id> <doc> --via review --hash <h>
```

The round answer goes to the terminal as well as the pins.  A round is never answered in the comment thread; the session resolves the thread when it is done.

### What bts records

`.bts/specs/recipes/<id>/review.json`: the page URL, the kit version, the review points published, and the last round seen.

`.bts/specs/recipes/<id>/feedback/round-N.json`, one per round, written by `bts review import`:

- `round`, `kind` (`review`, `apply` or `confirm`), `page` (the page version the notes were left on), `submitted`, `picked`, `processed`
- `files_before` and `files_after`: each plan file's hash
- `items`: per note, its id, anchor label, the element's excerpt, the user's words verbatim, kind, disposition (`answered`, `proposed`, `applied`, `declined`, `page`) and the proposal ids it produced
- `proposals`: id, target file, kind, summary, before, after, why, status (`proposed`, `accepted`, `rejected`, `applied`)
- `revisions` (apply rounds): file, from and to version and hash, the proposals and notes it came from

Each plan document gets a `Version: vN` line and a `Revisions` section that `bts review apply` keeps.  The chain from a note to a proposal to a version to a confirmation is then readable from the recipe alone, and it is the raw material for S6's learning loop (two pilot rounds already produced a lesson candidate).

### Gates

- `bts recipe confirm --via review --hash <h>` refuses when the hash is not the file's current hash, when a proposal on that file is undecided or accepted but not applied, or when a feedback item on that file has no disposition.
- `code-edit-go` through the page carries final.md's hash the same way.
- A round marked processed with an item that has no disposition is refused by `bts review import --finalize`.

### Waking the session

The notification is enough when the user stays on the page; a UserPromptSubmit hook covers the rest.  When `review.json` names a page, the hook adds one line to the prompt's context: the page may hold an unprocessed round, read `rounds` where status is `submitted`.  bts cannot read the page's data itself (only the session's ArtifactData tool can), so the hook reminds and the session reads.

### When the page is not available

AskUserQuestion stays as it is.  The skill falls back to it when the Artifact tool is unavailable, the user declines the page, or the run is headless.  A project setting picks the default (`bts config review.surface artifact|ask`).  Multica runs are headless and cannot watch an artifact; their review stays on Multica comments until a later slice.

## Slices

| Slice | Content |
|---|---|
| R1 | The kit; `/bts-review-page`; `bts review page|attach|import|propose|apply`; `confirm --via review --hash` and its gates; the UserPromptSubmit reminder; intent and scope pages in the build skill (express and full); `review.surface`. `/template-eval` before install. |
| R2 | The build spec page and `code-edit-go`; decisions on the page; the same for blueprint, fix and debug recipes. |
| R3 | The page grows into the pull request explainer at completion (merges with S5). |
| R4 | Multica: the manager reads rounds and relays; the worker stays headless. |

## Costs and risks

- Page build time.  Hand-built, the PR 2 explainer took about 25 minutes; the kit removes the mechanics, the drawings remain.  Express recipes feel this most.  Measure on the first real recipes; the intent section is small.
- The auto-reply cannot be turned off without losing the notification; the round message keeps it to one line.
- First use asks for three consents (comments, page data, microphone).
- Pages are private; a reviewer other than the user needs the page shared.
- Template changes go through `/template-eval`, which runs several headless sessions.

## Open questions, answered (2026-11-13)

1. Page language: English page text (the persisted-text rule); replies on pins and in the terminal in the user's language.
2. Default surface: the page, in every interactive recipe (`review.surface: artifact`); AskUserQuestion when the page cannot be used.
3. Intent goes through proposals and Update plan like scope, so the record is the same for every document.
4. The build spec page is R2.

## R1 as built

Uncommitted on `feat/multica-s0`, 2026-11-13.

- `internal/review`: `review.json` and `feedback/round-N.json`; `Fingerprint` (SHA-256 of the document with its status line blanked, first 12 hex digits, so confirming does not change it); `PageBrief`, `Attach`, `Import` (reads an ArtifactData `out_dir` export, wrapped or bare documents), `Dispose`, `Propose`, `Apply`, `Done`, `OpenWork`, `ConfirmCheck`.
- `bts review page|attach|import|dispose|propose|apply|done|status`; `bts recipe confirm <id> <intent|scope> --via review --hash <h> --answer "…"`, refused on a stale fingerprint, a document changed since the page's version, or open notes and proposals.
- `review.surface` in settings.yaml (enforced: `bts review page` refuses `ask`), with its migrate insertion and doctor entry.
- The user-prompt-submit hook (new event, script and settings entry): one line of context while a recipe with a page is in discovery or scoping or has open work.
- Templates: `/bts-review-page` (skill), the kit `bts-review-kit.js` and `.css` under it; intent confirmation in `/bts-discover` and scope confirmation in `/bts-recipe-blueprint` § Scoping use the page when the surface is artifact, and `/bts-recipe-build` step 2 points at it (express too).

Changes from the design above:

- Document versions live in `review.json` (and the round records), not in a `Version:` line and a `Revisions` section inside each document: parsers of intent.md and scope.md stay untouched, and a direct edit after publishing still shows as a version ("edited outside a review round").
- `dispose` and `done` are commands of their own (the design folded them into `import --finalize`); bts writes every page document the session uploads (`file_path` for ArtifactData), so the session never retypes a proposal or a status.
- Because intent and scope confirmation live in `/bts-discover` and `/bts-recipe-blueprint`, blueprint recipes get the page in R1 as well; fix and debug do not confirm intent and scope.
- Verified: `go vet`, `go test ./...`, `bts docs check`; a CLI smoke in a scratch project (`bts init`, page, attach, import, dispose, propose, done, an apply round refused before the edit and recorded after it, confirm refused on the old fingerprint and recorded on v2, the hook's line); the kit rendered in headless Chrome without an artifact viewer (local mode), which found and fixed buttons the `hidden` attribute did not hide.  Not verified at first: the kit inside the claude.ai viewer.
- Kit test in the viewer, 2026-11-13 (https://claude.ai/artifact/R3MGDWBBFdZX69usnebHev, a scratch recipe holding PR 3's 20:36 intent and scope, built and run by `/bts-review-page`'s steps): round 1 a Pick → proposal p-1-1 (04:11:54 sent, notified about 45 s later, picked 04:12:50); round 2 Update plan → scope v1 `29a9e977e864` to v2 `50545ae50f61`, page republished with only the page file (the kit files stayed); round 3 Confirm → `bts recipe confirm --via review --hash 50545ae50f61` recorded, scope.md CONFIRMED.  ArtifactData's `out_dir` export writes bare fields, which the reader takes.  Two fixes came out of it: the import dropped a Pick's decision key, and it told a confirm round to dispose notes instead of to confirm.
- `/template-eval`, 2026-11-13, 3 runs per variant of `/bts-recipe-build "Add a --shout option to greet.py…"` in a two-file Python project, headless Opus: a `claude -p` session has neither the Artifact tool nor AskUserQuestion, so it measures the fallback path.  All six drafted intent and scope, held `intent-confirm` and `scope-confirm`, confirmed nothing and stopped with a clear question; the change runs added one sentence saying no review page is available here and never called `bts review page`.  Blind Opus judge totals 24.3 (base, 24–25) and 23.3 (change, 23–24); cost $0.73 and $0.72, turns 10 and 10.7: no measurable difference at n = 3.  Outside the change, 4 of 6 runs (two per variant) moved the phase to scoping before the intent was confirmed.  Recommendation: promote.

## Feature pages (2026-11-19, trial problem #177)

In the first Orca feature trial (f-005, a coordinator planning three items) the coordinator asked its direction questions in the terminal and opened `intent.md` and `contracts.md` in VS Code for the user to confirm: the page covered recipe points only, and nothing routed `bts feature confirm` through `review.surface`.  Under Orca the items' scope and spec go to the coordinator too, so the user could see no page for a whole feature.

- A review now has a subject (`review.Subject`): a recipe, or a feature.  The subject says where the record lives (`Dir`: the recipe's directory, or the feature's store directory beside its documents), where the page and uploads live (`Work`: always in the session's bts project, `.bts/local/recipes/<id>/review/` or `.bts/local/features/<f>/review/`, because the Artifact and ArtifactData tools read files from the project), which documents a proposal may change, and how each is fingerprinted.  Recipe behaviour is unchanged.
- A feature's documents are `intent.md`, `contracts.md` and `plan`, the items as a confirmation covers them, fingerprinted from feature.json at each read.  `bts feature confirm` pins the three at once, so the page has one point (`feature`) and one confirm row, the bundle `feature`, whose fingerprint combines the three and which gets its own version whenever any of them changes (in an apply round, naming the round's proposals; otherwise "edited outside a review round").
- Every `bts review` command takes `--feature <f>`; `bts feature confirm <f> --via review --hash <h> --answer "…"` runs the same gate as a recipe's (`ConfirmCheck` on the bundle, open work on any of the three).  A `plan` proposal is applied with `bts feature item add|edit|remove`.
- Kit 2: a page's config names `"feature"` in place of `"recipe"`; the round comments and the confirm row read accordingly, and a doc may carry `"file"` for its row's label.  Kit 1 pages keep working.
- The user-prompt-submit hook also reminds a session with no recipe of its own (a coordinator) of a feature page in its project while the feature waits for the user's confirmation or the page holds open work.
- Not in this slice: an item's scope or spec that the coordinator cannot confirm within the feature's confirmation still reaches the user as a question in the coordinator's terminal.
