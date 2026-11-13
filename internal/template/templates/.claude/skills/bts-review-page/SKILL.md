---
name: bts-review-page
description: >
  Run a recipe's user review on an artifact page instead of AskUserQuestion:
  draw the change, publish it with the review kit, let the user pin notes
  (typed or spoken) and send them in rounds, answer each round from this
  session, turn change requests into proposals the user accepts or
  rejects, apply them as a new document version, and record the
  confirmation of a fingerprinted version.  Used by intent and scope
  confirmation when review.surface is artifact.
user-invocable: true
allowed-tools: Read Write Edit Grep Glob Bash Artifact ArtifactData ArtifactComments AskUserQuestion
argument-hint: "<recipe-id> <intent|scope>"
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

Use it when the calling skill reaches intent or scope confirmation and
`review.surface` in `.bts/config/settings.yaml` is `artifact` (the
default).  Use AskUserQuestion as the calling skill says when the
Artifact tool is not available here (a headless run, Multica), when
`bts review page` refuses, or when the user says they would rather answer
in the terminal.  Words the user types in the terminal always count, page
or not.

## Build or extend the page

1. `bts review page {id} --point <intent|scope>` prints what the page must
   show, the document versions and fingerprints, the kit files, the
   capabilities to declare and the config block.  Run it again whenever a
   document changes; its versions are the ones the page shows.
2. Write the page to `.bts/local/recipes/{id}/review/page.html`.  At the
   scope point, extend the intent page: earlier sections stay.
   - **Draw this change.**  The page exists to show the mechanism the
     documents describe: where the item fits, today and after side by side
     (a mock of the output when there is one, marked as an illustration),
     one scenario step by step, the shape with this item solid and later
     work dashed, lanes for neighbouring items, decisions as option cards
     drawn side by side.  Follow the brief's section list.
   - **Boxes say what they hold and who fills it**, never a code concept
     ("what was read and left out; PR 4 fills it", not "typed,
     harness-facing").  In the pilot two of six rounds asked what a box
     meant.
   - **Mark everything reviewable** with `data-note="<Section> › <part>"`:
     every card, lane item, diagram node (an SVG `<g>`), assumption and
     defect.  Keep a label stable across versions: pins and proposals find
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
   - Page text is English (persisted text); facts come from intent.md and
     scope.md as they stand.  No client or patient data.
3. Publish it with the Artifact tool: the page file, `files` mapping
   `bts-review-kit.js` and `bts-review-kit.css` to the paths the brief
   printed, the capabilities exactly as printed, and `icon: "notes"` on the
   first publish.  Republish the same file path to keep the URL.
4. `bts review attach {id} --url <link> --point <intent|scope>`.
5. Tell the user in the terminal, in their language: the link; press N and
   click to pin a note, speak or type, Send when done; Pick on a decision;
   Confirm when it reads right; or answer here.  The first send asks them
   to let the page comment and keep data.  Then end the turn and wait.

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
   - A change to the plan (intent.md, scope.md), including a Pick: do not
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
2. Edit intent.md or scope.md for the accepted proposals and nothing else.
3. `bts review apply {id} --round M`.  It refuses a target document that
   did not change, records the new version and fingerprint, marks the
   proposals applied or rejected, and prints the proposal documents to
   upload (ArtifactData `update`, collection `proposals`).
4. `bts review page {id} --point <point>`, then update the page to the new
   version: the drawing and lanes as the document now reads, the config
   block's versions, and a short "changed in vN" line naming the proposal.
   Republish the same path.
5. `bts review done {id} --round M`, mark `rounds/r-M` processed with a
   summary, resolve the thread, and tell the user in the terminal what
   changed (the document diff in a few lines) and that the new version is
   ready to confirm.

## Confirm

A confirm round carries one note of kind `confirm`: its `doc` (intent or
scope), its `value` (the fingerprint on the row the user pressed) and the
user's words.

1. Mark it picked and import it.
2. `bts recipe confirm {id} <doc> --via review --hash <value> --answer
   "<the user's words, in the project's record language>"`.  bts refuses
   when the fingerprint is not the file's current one or the page still
   has open notes or proposals on that document; it prints why.
3. Write the outcome on the note: `status: applied` with a reply saying it
   is recorded, or `status: declined` with bts's reason in the user's
   words.  Mark the round processed and resolve the thread.
4. Tell the user in the terminal, then continue the calling skill from its
   confirmation step (log the phase, move on).

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
