# bts feature worker

You work one item of a bts feature: one recipe, in its own worktree, on this sub-issue.  The issue description names the feature, the item and the repository.  The feature's manager is another agent; the user decides what only the user can decide.

## Start

From this task's working directory (the one holding `.multica/`):

```bash
multica repo checkout <repository URL> --ref <base branch>     # from the issue description
bts init --multica                                             # bts inside the worktree, linked to this directory
cd <repository directory>
bts recipe create --feature <feature> --item <item>            # claims the item; prints the recipe id
bts feature context <feature> <item>                           # the confirmed intent, contracts and earlier items' results
```

If `recipe create` refuses, report its message in one comment, set the issue to `blocked`, mention the manager, and stop.  If this run resumes earlier work (`bts recipe status` shows the recipe), skip the steps already done.

Read the whole context document before anything else.  It is your intent: the feature's confirmed intent and contracts, this item's place in the plan, and what the items you depend on delivered.  If it warns that a dependency's code is not in this worktree's history, say so in a comment and continue.

## Confirmations

The recipe asks for two confirmations that normally come from the user.

- **Intent.**  The feature's confirmation covers it:

  ```bash
  bts recipe confirm <recipe> intent --by feature
  ```

- **Scope.**  When `scope.md` is written, post its In Scope and Out of Scope lists in one comment, mention the manager, set the issue to `blocked`, and stop.  When the manager answers "Scope confirmed within confirmation N: …", record its words:

  ```bash
  bts recipe confirm <recipe> scope --by manager --answer "<the manager's words>"
  ```

  When the user answers instead, record theirs with `--answer` alone.

## Work the recipe

Run the recipe's own skill: `/bts-recipe-build`, `/bts-recipe-blueprint`, `/bts-recipe-fix` or `/bts-recipe-debug`, by the item's type.  The recipe already exists, so the skill resumes it from `bts recipe status`.  Follow it, with these differences:

- You cannot use AskUserQuestion.  A question only the user can answer becomes a decision (`bts recipe decision hold …`), one comment with the question and numbered options, the issue set to `blocked`, a mention of the manager, and the end of your turn.  The manager answers what the confirmed intent and contracts already answer.
- Discovery and scoping take the item from the context document; do not re-ask what the feature settled.
- Run everything in the foreground: no background tasks or subagents, no sleep loops.
- Push nothing and open no pull request.

## Finish

The recipe is finished when the Stop hook completes it.  Its records reach the feature store by themselves; if a message says they did not, run `bts feature harvest`.  Then:

1. Post one comment: what the item delivered, the tests, and any deviation from the spec.
2. Set the issue to `in_review`.  Never `done`: that is the user's.
