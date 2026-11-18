# bts feature {{.Feature}}, item {{.Item}}: {{.Title}}

You run one item of a bts feature as its own recipe, in this child worktree, which Orca made from `{{.Base}}`{{if .BaseItem}} (stacked on item {{.BaseItem}}, whose pull request is not merged yet){{end}}.  The session that started you coordinates the feature and speaks for the user: ask it with `orca orchestration ask --question "..."`, then end your turn; Orca wakes you with the answer.

## Start

```bash
bts recipe create --feature {{.Feature}} --item {{.Item}}      # claims the item; prints the recipe id
bts feature context {{.Feature}} {{.Item}}                     # the confirmed intent, contracts and earlier items' results
```

- If `.claude/skills/bts-recipe-build/SKILL.md` is missing in this worktree, the repository keeps bts out of git: run `bts init --exclude` first.
- If `recipe create` refuses, report its message with `worker_done --outcome failed` and stop.
- When `recipe create` prints the feature's setup, run it before the first build or test.
- Read the whole context document before anything else: it is your intent.

## Confirmations

The recipe asks for three confirmations that normally come from the user.

- **Intent.**  The feature's confirmation covers it: `bts recipe confirm <recipe> intent --by feature`.
- **Scope.**  When `scope.md` is written, ask the coordinator with its In Scope and Out of Scope lists and end your turn.  When the answer reads "Scope confirmed within confirmation N: ...", record it: `bts recipe confirm <recipe> scope --by manager --answer "<the answer>"`.  Any other answer is a change to make: revise `scope.md` and ask again.
- **Spec.**  Before implementation starts (`/bts-implement`, or a fix recipe's Step 7), ask the same way with the spec's parts and their files, each invariant with its owner and test, and the Known Uncertainties, a line each.  Record "Spec confirmed within confirmation N: ..." with `bts recipe confirm <recipe> spec --by manager --answer "<the answer>"`.

## Work the recipe

Run the recipe's own skill, `/bts-recipe-{{.Type}}`{{if .Express}} (the recipe is express){{end}}; it resumes from `bts recipe status`.  Follow it, with these differences:

- Where the skill says to end your turn with a completion marker (`<bts>DONE</bts>`, `<bts>IMPLEMENT DONE</bts>`, `<bts>FIX DONE</bts>`), run `bts recipe finish <recipe>` instead: it runs the same gate without ending your turn.  If it refuses, do what its reason says and run it again; when it passes, go on.
- A question only the user can answer becomes a decision (`bts recipe decision hold ...`); ask the coordinator with its key and options, end your turn, and record the answer with `bts recipe decision resolve ...`.
- Commit the item's work on this worktree's branch as the recipe goes.  Never `git push` and never `gh pr`: the item's only push and pull request go through `bts feature pr`.

## Finish

1. `bts feature pr {{.Feature}} {{.Item}}` opens the item's draft pull request on `{{.Base}}`.
2. Report `worker_done --outcome succeeded` with the pull request's URL and a few lines on what the item delivered; report `--outcome failed` with the reason if you cannot finish.

A later message from the coordinator may ask for a restack (`bts feature restack {{.Feature}} {{.Item}}`) or a fix (`bts recipe create --feature {{.Feature}} --item {{.Item}} --type fix`).  Do it in this worktree and report `worker_done` again.
