---
name: bts-review-page
description: >
  Run a recipe's or a feature's user review on a review page instead of
  AskUserQuestion: draw the change, publish it with the review kit as a
  claude.ai artifact or serve it from this machine, let the user pin notes
  (typed or spoken) and send them in rounds, answer each round from this
  session, turn change requests into proposals the user accepts or rejects,
  apply them as a new document version, and record the confirmation of a
  fingerprinted version.  Used by intent, scope and spec confirmation, and
  by a feature's confirmation of its intent, contracts and plan, when
  review.surface is artifact or local.
user-invocable: true
allowed-tools: Read Write Edit Grep Glob Bash Artifact ArtifactData ArtifactComments AskUserQuestion
argument-hint: "<recipe-id> <intent|scope|spec>  |  --feature <feature-id>"
effort: high
---

# Review page

The user reviews a recipe's documents on one page per recipe: they read a
drawing of the change, pin notes on what they want changed, and send the
notes together.  You answer from this session, which has the work's
context, never from the comment thread.  bts keeps the record
(`review.json`, `feedback/round-N.json`): which note led to which
proposal, which proposal to which version, which version the user
confirmed.

Use it when the calling skill reaches intent, scope or spec confirmation
and `review.surface` in `.bts/config/settings.yaml` is `local` (the
default, § Local page: bts serves the page on this machine) or `artifact`
(the page published on claude.ai).  With `local` the PreToolUse hook
refuses to publish a review page with the Artifact tool.  The spec is the document the code is built from: final.md, or
fix-spec.md in a fix recipe; `/bts-implement` and `/bts-recipe-fix`
review it before the first product code.  A feature's plan is reviewed
the same way before its items start (§ Feature).  Use AskUserQuestion as the calling skill says when no
user sits at this machine to open the page (a headless run, Multica),
under `artifact` when the Artifact tool is not available here, when `bts
review page` refuses, or when the user says they would rather answer in
the terminal.  Words the user types in the terminal always count, page
or not.

## Build or extend the page

1. `bts review page {id} --point <intent|scope|spec>` prints what the page must
   show, the document versions and fingerprints, the kit files, the
   capabilities to declare and the config block.  Run it again whenever a
   document changes; its versions are the ones the page shows.
2. Write the page to `.bts/local/recipes/{id}/review/page.html`.  At the
   scope and spec points, extend the page: earlier sections stay.  A fix
   or debug recipe has no intent or scope, so its page starts at the spec.
   - **Draw this change.**  The page exists to show the mechanism the
     documents describe: where the item fits, today and after side by side
     (a mock of the output when there is one, marked as an illustration),
     one scenario step by step, the shape with this item solid and later
     work dashed, lanes for neighbouring items, decisions as option cards
     drawn side by side.  Follow the brief's section list.
   - **Draw the spec as the code it will become.**  The parts are boxes
     with their files, each invariant a marker on the box that owns it
     with its test beside it, each edge labelled with what crosses it,
     and the Known Uncertainties as cards saying what is open and what
     will settle it.  For a fix or debug recipe, the failing path today
     and after the fix side by side, the cause marked where it lives.
     The user confirmed a direction at intent and scope; here they see
     the parts, owners and contracts the code follows, so draw those
     rather than restating the direction.
   - **Boxes say what they hold and who fills it**, never a code concept
     ("what was read and left out; PR 4 fills it", not "typed,
     harness-facing").  In the pilot two of six rounds asked what a box
     meant.
   - **Mark everything reviewable** with `data-note="<Section> › <part>"`:
     every card, lane item, diagram node (an SVG `<g>`), assumption,
     defect, invariant marker, contract and uncertainty.  Keep a label stable across versions: pins and proposals find
     their element by it.  A lane container gets a label too, so a move
     proposal can draw into it.
   - **Decisions:** `<div data-bts-decision="<key>">` around option cards
     `<div data-bts-option="<value>" data-note="…">`.  The kit adds Pick.
   - Put the config block from the brief in the page (add `"title"`, and
     `"lang"` for dictation in the user's language, e.g. `"ko"`), link
     `bts-review-kit.css` in the head and load `bts-review-kit.js` last in
     the body.  The kit draws the review bar, the editor, pins, tray,
     proposals and confirm rows: do not build your own.  Optional
     `<div data-bts-proposals-slot></div>` and
     `<div data-bts-confirm-slot></div>` place them; otherwise they follow
     the main content.
   - Page text is English (persisted text); facts come from the recipe's
     documents as they stand.  No client or patient data.
3. Publish it with the Artifact tool: the page file, `files` mapping
   `bts-review-kit.js` and `bts-review-kit.css` to the paths the brief
   printed, the capabilities exactly as printed, and `icon: "notes"` on the
   first publish.  Republish the same file path to keep the URL.
4. `bts review attach {id} --url <link> --point <intent|scope|spec>`.  In
   an Orca terminal it also opens a new page in Orca's own browser, in a
   tab beside this terminal (Claude Code has already opened it in the
   system browser); Orca's browser may ask the user to sign in to claude.ai
   once.
5. Tell the user in the terminal, in their language: the link; press N and
   click to pin a note, speak or type, Send when done; Pick on a decision;
   Confirm when it reads right; or answer here.  The first send asks them
   to let the page comment and keep data.  Then end the turn and wait.

## Local page

With `review.surface: local` bts serves the page from this machine.  It
belongs to no claude.ai account, so a session on another account, or a new
session, takes its rounds.  Everything in this skill holds, with these
differences:

- **Build** (step 2): link `bts-review-kit.css` in the head and load
  `bts-review-kit.js` last in the body, by base name; put the config block
  in as the brief prints it.  The server serves the kit and adds the local
  transport.  There are no capabilities.
- **Show it** (steps 3 and 4): `bts review open {id} --point <point>` in
  place of publishing and attach.  It starts the user's page server (one
  per machine, for every project) when it is down, registers this project
  with it, records the local link in review.json, prints the page's links
  (this Mac, and the tailnet once `bts pages setup` mapped it), and opens
  the page in Orca's browser beside the terminal or in the system browser.
  To show a new version, rewrite page.html; the page reloads, after the
  user closes any note they have open.
- **Wait** (step 5): tell the user the link and how to use the page (the
  tailnet link when they are away from this Mac, on their phone); there
  is no sign-in and nothing to allow.  Then run `bts review wait {id}` with
  `run_in_background` and end the turn.  It returns when the user sends a
  round, which wakes you, or after 100 minutes with "no round yet", when you
  run it again.  The page shows whether a session is waiting; a round sent
  while none waits is kept until one checks.
- **Round** steps 1 and 2 are one command: `bts review import {id}` reads
  the page's data and marks the rounds it takes picked.  No ArtifactData.
- **Round** steps 3 and 4: `bts review dispose ... --reply "<one or two
  sentences, in the user's language>"` and `bts review propose ... --reply
  "..."` write the note's outcome on the page, and propose writes the
  proposal there too.  Nothing to upload.
- **Round** step 5: `bts review done {id} --round N --summary "<one line,
  in the user's language>"`, and rewrite page.html if you fixed the page.
  Step 6 has no thread to resolve.  After step 7, run `bts review wait {id}`
  in the background again.
- **Update plan**: `bts review apply` writes the proposals' outcomes to the
  page itself; step 4 rewrites page.html instead of republishing.
- **Confirm** step 3: `bts review reply {id} --note <id> --status applied
  --reply "..."`, or `--status declined` with bts's reason, then `done`.
- **A feature's page** takes `--feature {f}` on `open`, `wait`, `import` and
  the rest, as in § Feature.
- **Moving a review from claude.ai**: `bts review open` takes the versions
  and rounds from bts's records.  Notes the user never sent stay on the
  claude.ai page, unless you first export them with ArtifactData into
  `.bts/local/recipes/{id}/review/data/notes/`.
- **Sharing** with someone outside the owner's tailnet: `bts review export
  {id}` writes the page as one read-only HTML file, without the notes.
  Sending it is a separate step the user asks for.
- `bts pages status` shows the server; `bts pages stop` stops it for every
  project.  Without the LaunchAgent it also stops after 12 idle hours, and
  `bts review open` starts it again.

## Round

A round arrives as a task notification about an auto-replied comment on
the page, or the user-prompt-submit hook reminds you, or the user says
they sent it.  The auto-reply has already posted one line in the thread;
never post another there.

1. ArtifactData `query` on `rounds` where `status` is `submitted`.  For
   each, first `update` it to `{"status": "picked", "picked": <now>}` (pin
   `if_version`): the page shows the user you have it.
2. ArtifactData `list` of `notes` and of `rounds`, each with
   `out_dir: .bts/local/recipes/{id}/review/export`, then
   `bts review import {id} --dir .bts/local/recipes/{id}/review/export`.
   It prints each round's notes, or an apply round's accepted and rejected
   proposals.
3. **Review round**, per note:
   - A question: answer it.  `bts review dispose {id} --round N --note <id> --as answered --why "<one line>"`.
   - The page's own wording or drawing is unclear: fix the page, `--as page`.
   - A change to the plan (intent.md, scope.md, the spec), including a Pick: do not
     edit the document.  `bts review propose {id} --round N --note <id>
     --target <doc> --kind <edit|add|remove|move> --summary "…" --before "…"
     --after "…" --why "…"` (a move adds `--to-lane "<lane's data-note>"
     --to-label "<short name>"`), then upload the file it prints:
     ArtifactData `set`, collection `proposals`, the printed doc id and
     `file_path`.  Split a note that mixes two things into proposals that
     can be accepted apart.
   - Not acted on: `--as declined --why "<the reason>"`.
4. Write each note's outcome to the page in one ArtifactData `batch`:
   `notes/<id>` gets `status` (`answered`, `proposed`, `page` or
   `declined`), `reply` (one or two sentences in the user's language) and
   `processed`.
5. `bts review done {id} --round N`; then update `rounds/r-N` to
   `status: processed` with `processed` and a one-line `summary` in the
   user's language.  Republish the page if you fixed it.
6. Resolve the round's comment thread (ArtifactComments `resolve`) without
   replying in it.
7. Answer in the terminal, in the user's language: per note, what it was
   and what you did; the proposals waiting for Accept or Reject.

## Update plan

An apply round (the user pressed Update plan) carries the accepted and
rejected proposal ids.

1. Mark it picked and import it as in Round steps 1 and 2.
2. Edit the plan documents for the accepted proposals and nothing else.
   On the spec, a proposal that moves an invariant, a signature, a file
   or a test also changes it where it lives (the skeleton, the test,
   domain.md, the wireframe) and reruns what it touched (`bts check build
   {id} --cmd ...`, `bts falsifier check {id} --red`).
3. `bts review apply {id} --round M`.  It refuses a target document that
   did not change, records the new version and fingerprint, marks the
   proposals applied or rejected, and prints the proposal documents to
   upload (ArtifactData `update`, collection `proposals`).
   - On the spec, review the change before the new version goes to the
     page: `/bts-verify` on the spec at delta scope, recorded with
     `bts recipe log {id} --doc <spec> --dimension verify --scope delta
     --from-verification ...`.  An express build reruns `bts verify
     .bts/specs/recipes/{id}/final.md` instead.  Fix a critical or major
     it finds the same way (an edit `bts review page` shows as a new
     version) and review again; the user confirms the version that came
     out clean.
4. `bts review page {id} --point <point>`, then update the page to the new
   version: the drawing and lanes as the document now reads, the config
   block's versions, and a short "changed in vN" line naming the proposal.
   Republish the same path.
5. `bts review done {id} --round M`, mark `rounds/r-M` processed with a
   summary, resolve the thread, and tell the user in the terminal what
   changed (the document diff in a few lines) and that the new version is
   ready to confirm.

## Confirm

A confirm round carries one note of kind `confirm`: its `doc` (intent,
scope, final or fix-spec), its `value` (the fingerprint on the row the
user pressed) and the user's words.

1. Mark it picked and import it.
2. `bts recipe confirm {id} <doc> --via review --hash <value> --answer
   "<the user's words, in the project's record language>"`.  bts refuses
   when the fingerprint is not the file's current one or the page still
   has open notes or proposals on that document; it prints why.  For the
   spec it records the decision `spec-confirm`, which the move into
   implement waits for, and leaves the file as it is.
3. Write the outcome on the note: `status: applied` with a reply saying it
   is recorded, or `status: declined` with bts's reason in the user's
   words.  Mark the round processed and resolve the thread.
4. Tell the user in the terminal, then continue the calling skill from its
   confirmation step (log the phase, move on).

## Feature

A feature's intent, contracts and plan are confirmed together (`bts feature
confirm` pins all three), so its page carries one point, `feature`, and one
confirm row covering the three.  Whoever settles the feature (a
coordinator, /bts-feature-orca) runs it from the bts project it works in;
every `bts review` command takes `--feature {f}` in place of the recipe id,
and everything above applies with these differences:

- `bts review page --feature {f}` prints the sections, the versions of
  intent.md, contracts.md, the plan and the bundle (`feature`), and where to
  write the page: `.bts/local/features/{f}/review/page.html`.  Draw the
  mechanism: what the feature delivers today and after, the items as a
  dependency graph (each box an item with its type and mode, an arrow per
  depends-on, the order the workers start in), each item's slice, and each
  contract on the edge it governs, naming the item that provides it and
  the items that rely on it.  Give every item, edge and contract a
  `data-note`, so a note can say "split this item" or "this contract is
  missing a field".
- `bts review attach --feature {f} --url <link>`.
- Export rounds and notes to `.bts/local/features/{f}/review/export`.  A
  proposal targets `intent.md`, `contracts.md` or `plan`.  On Update plan, a
  `plan` proposal is applied by changing the items with
  `bts feature item add`, `edit` or `remove` (never feature.json by hand),
  then `bts review apply --feature {f} --round M` records the plan's and the
  bundle's new versions.
- A confirm note's `doc` is `feature`:
  `bts feature confirm {f} --via review --hash <value> --answer "<the
  user's words>"`.  bts refuses when the fingerprint is not the current one
  of the three together, or the page still has open notes or proposals.
  Then continue the calling skill (a coordinator dispatches the items).

## Rules

- The page never changes the plan by itself: a note becomes a proposal,
  the user accepts it, Update plan applies it, Confirm signs a version.
  Once a page is published, an edit to a plan document outside an apply
  round shows up as "edited outside a review round" in `bts review page`.
- Answers come from this session, on the pins and in the terminal.  The
  comment thread is a receipt: resolve it, never reply in it.
- Report what you change as what it is: the page, its data, the recipe's
  documents.
- `bts review status {id}` shows the page, the versions, the rounds and
  what is still open.
