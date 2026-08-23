---
name: jig-comment-apply
description: >
  Incorporate inline jig review comments embedded in a recipe's docs.
  Reads `.jig/local/recipes/{id}/pending-comments.json` (written by
  `jig comment apply`), runs meta-analysis (conflicts, cascades,
  verify-contradictions, ambiguity), applies edits per-doc in parallel,
  and shows a git diff for approval before finalizing.
user-invocable: true
allowed-tools: Read Write Edit Bash Agent AskUserQuestion
argument-hint: "<recipe-id>"
---

# Comment-apply

Incorporate jig review comments for recipe: $ARGUMENTS

This skill is the second half of the comment workflow. The first half
(`jig comment apply <id>`) parses inline `> [!JIG-COMMENT]`,
`> [!JIG-BLOCK]`, `> [!JIG-Q]` callouts and writes a structured handoff
to `.jig/local/recipes/{id}/pending-comments.json`. This skill reads that
handoff, edits the recipe docs, then calls `jig comment apply --finalize`
to update the manifest.

## Pre-flight

```bash
jig recipe status
```

Resolve `{id}`:
- If `$ARGUMENTS` is set, use it.
- Else use the active recipe ID from the status output.
- If neither, ask the user.

Then load the handoff:

```bash
cat .jig/local/recipes/{id}/pending-comments.json
```

If the file does not exist, tell the user to run `jig comment apply {id}`
first and stop.

## Safety snapshot

Before any edits, snapshot the recipe directory so a partial-apply crash
can be rolled back cleanly. Capture the stash REF (not just rely on
`stash@{0}`) so a later pop targets *our* stash, not whatever stash the
user already had on top.

```bash
STASH_MSG="jig-comment-apply {id} $(date +%s)"
git stash push --include-untracked -m "$STASH_MSG" -- .jig/specs/recipes/{id} >/dev/null 2>&1
# Locate the stash by message — empty if nothing was pushed
STASH_REF=$(git stash list | grep -F "$STASH_MSG" | head -1 | cut -d: -f1)
echo "$STASH_REF" > .jig/local/recipes/{id}/comment-apply.stash-ref
```

`STASH_REF` is empty in two cases:
- The push had nothing to save (working tree was already clean).
- The push failed for some reason (rare).

Either way, "no stash" means "no rollback needed / possible." Persist the
ref to a local file so a different shell invocation (e.g. retry after a
crash) can recover it.

On any unexpected agent failure during Pass B, restore by ref:

```bash
REF=$(cat .jig/local/recipes/{id}/comment-apply.stash-ref 2>/dev/null)
if [ -n "$REF" ] && git stash list | grep -q "^$REF:"; then
  git stash pop "$REF"
fi
```

## Pass A — Meta-analysis (one Agent call)

The handoff has already done mechanical parsing. Pass A is a single
Agent call that reads the comment bodies plus the source docs plus
`verification.md` (if present) plus the manifest, and returns a
structured assessment.

Spawn **Agent(general-purpose)** with this prompt:

```
You are reviewing a batch of inline review comments for a jig recipe and
producing a structured meta-analysis. Do NOT edit any files. Return only
the JSON described below.

## Recipe ID
{id}

## Pending comments
{paste the full pending-comments.json contents}

## Source docs
{list of all .md files under .jig/specs/recipes/{id}/ — read each before
analyzing}

## Verification record (optional)
{path to verification.md if it exists, else "none"}

## Manifest
{path to manifest.json}

## Your task

For each comment, decide:
1. **Conflicts** — does this comment contradict another comment? (e.g.,
   one says "use Redis", another says "use in-memory")
2. **Cascades** — does applying this comment require touching a different
   doc too? (e.g., a draft.md comment that changes scope must also
   update scope.md)
3. **Verify contradictions** — does this comment contradict a verified
   finding in verification.md? (e.g., comment says "rename to X" but
   verification confirmed the original name is required by the API)
4. **Ambiguous** — is the comment too vague to act on without more
   information from the user?

For cascades, also identify any **other docs** that should be touched.
This becomes a synthetic comment record for Pass B.

Output strictly this JSON shape:

{
  "conflicts": [
    {"comment_ids": ["c-aaa", "c-bbb"], "reason": "...", "confidence": "low|medium|high"}
  ],
  "cascades": [
    {"comment_id": "c-aaa", "also_touches": ["scope.md", "wireframe.md"], "why": "...", "confidence": "low|medium|high"}
  ],
  "verify_contradictions": [
    {"comment_id": "c-aaa", "verify_finding": "F-3", "reason": "...", "confidence": "low|medium|high"}
  ],
  "ambiguous": [
    {"comment_id": "c-aaa", "needs_clarification": "...", "confidence": "low|medium|high"}
  ],
  "questions": [
    {"comment_id": "c-q1", "question": "..."}
  ]
}

`questions` enumerates every JIG-Q callout — Pass A does NOT answer them
(that's the user's job in the resolution loop). Including them here makes
the resolution loop self-contained: every JIG-Q gets explicitly asked,
nothing falls through to "TODO" silently.

If a category is empty, return an empty list. No prose, no markdown — just JSON.
```

Save the returned JSON to `.jig/local/recipes/{id}/meta-analysis.json`.

### Resolution loop

For each non-empty list, present the findings to the user and resolve
**before** Pass B runs. Use AskUserQuestion. Examples:

- **Conflict**: "Comment c-aaa says X. Comment c-bbb says Y. They
  contradict. Which should win?" — options: keep c-aaa / keep c-bbb / both wrong (you decide) / cancel
- **Verify contradiction**: "Comment c-aaa contradicts verification finding
  F-3. Apply anyway, skip, or cancel?" — options: apply / skip / cancel
- **Ambiguous**: "Comment c-aaa needs clarification: «...». What did you
  mean?" — open-ended; capture user response as `clarification`
- **Question**: For each entry in `questions`, AskUserQuestion with the
  question body verbatim. Capture the user's answer as `answer` on that
  comment record. If the user opts to skip, set `answer = "TODO"`.

For **cascades** with confidence=high, do NOT ask — accept and add the
synthetic cascade record to the per-doc apply queue. For
confidence=low/medium, ask: "Comment c-aaa likely also requires updating
scope.md. Include the cascade?" — options: include / skip cascade.

For **freeform** comments (kind="freeform" — only present when the user
ran `jig comment apply --include-freeform`), there is no marker to
remove. Ask the user how to treat each: (a) incorporate the intent and
delete the lines, (b) leave the lines in place as drafted prose,
(c) move the lines to "## Q&A", (d) discard. Encode the choice on the
comment record as `freeform_action`.

After resolution, write the **resolved comment queue** to
`.jig/local/recipes/{id}/resolved-comments.json` — same shape as the
pending file but with:
- conflicting comments dropped (or one chosen)
- ambiguous comments enriched with `clarification` field
- JIG-Q comments enriched with `answer` field
- freeform comments enriched with `freeform_action` field
- cascade synthetic comments appended (kind="cascade", body=
  "Apply because of cascade from {origin doc}#{section}: {original body}")
  — `kind="cascade"` matches the `KindCascade` constant in the Go-side
  enum, so downstream tooling can recognize them as synthetic.

If the user chose "cancel" on any blocking finding, abort the skill and
tell them to fix the comments and re-run `jig comment apply`.

## Pass B — Per-doc apply (parallel Agent calls)

For each doc that has at least one comment in the resolved queue,
spawn one **Agent(general-purpose)** in parallel. Send all spawn calls
in a single message so they run concurrently.

**Cascade-target docs are handled in this same parallel pass** — a doc
that has zero original comments but is the target of a cascade still
gets an agent spawned for it (its only input is the cascade synthetic
records). Two Pass-B agents will never edit the same file: each doc has
exactly one agent, which handles both its own comments AND any
cascades pointing at it.

Per-doc agent prompt:

```
You are editing a jig spec document to incorporate review comments.

## Doc to edit
.jig/specs/recipes/{id}/{doc}

## Comments to apply
{slice of resolved-comments.json filtered to this doc, including any
synthetic cascade records targeting this doc}

## Rules

1. For each comment, edit the doc to incorporate the change. Use the
   `section_path`, `anchor_before`, and `anchor_after` fields to locate
   the right place — the body sits between those anchors. The marker
   block itself (`> [!JIG-COMMENT]` etc.) is the exact location.
2. After editing, REMOVE the entire callout block (the marker line plus
   all body lines). Do NOT leave the marker behind.
3. For JIG-Q (questions), do NOT auto-edit the body. Instead, append a
   "## Q&A" section at the bottom of the doc (or extend an existing one)
   with the question and the `answer` field carried on the comment
   record. If `answer` is missing or "TODO", write "TODO" verbatim —
   never invent answers. Then remove the marker.
4. For freeform comments, follow the `freeform_action` field on the
   comment record:
   - "incorporate": fold the intent into the surrounding doc, delete the
     freeform lines.
   - "leave": no edit; report this comment in the `deferred` list.
   - "qa": move the lines to "## Q&A".
   - "discard": delete the lines.
5. For cascade synthetic comments (kind="cascade"), apply the change
   without leaving any marker — the original marker lives in the OTHER
   doc and that doc's Pass-B agent will remove it.
6. Preserve the doc's structure, heading levels, and unrelated content.
7. Do NOT change anything outside the comment scopes.

Return a JSON summary:

{
  "doc": "{doc}",
  "applied": ["c-aaa", "c-bbb"],
  "deferred": [{"id": "c-ccc", "reason": "..."}],
  "marker_residue_check": "clean | residue_found"
}
```

After all per-doc agents return, verify each `marker_residue_check` is
"clean". If any reports residue, ask the user to look at the doc and
re-run.

## Pass C — Diff approval

Show the user what changed:

```bash
git diff --color=always .jig/specs/recipes/{id}
```

Then AskUserQuestion: "Apply these changes?" with options:
- **Apply** — accept all, drop the stash if any
- **Reject** — restore from stash, abort
- **Inspect manually** — leave the working tree as-is and stop the skill
  (user will commit/discard manually); do NOT call --finalize

On Apply, drop the safety stash by ref (so we don't drop someone else's):

```bash
REF=$(cat .jig/local/recipes/{id}/comment-apply.stash-ref 2>/dev/null)
if [ -n "$REF" ] && git stash list | grep -q "^$REF:"; then
  git stash drop "$REF"
fi
rm -f .jig/local/recipes/{id}/comment-apply.stash-ref
```

On Reject, restore:

```bash
REF=$(cat .jig/local/recipes/{id}/comment-apply.stash-ref 2>/dev/null)
if [ -n "$REF" ] && git stash list | grep -q "^$REF:"; then
  git stash pop "$REF"
else
  git checkout -- .jig/specs/recipes/{id}
fi
rm -f .jig/local/recipes/{id}/comment-apply.stash-ref
```
Then abort.

## Finalize

When the user accepted the changes, call:

```bash
jig comment apply {id} --finalize
```

This re-parses the docs (markers should now be removed for applied
comments), recomputes `manifest.open_comments` and
`manifest.blocking_comments`, appends a `comment-apply` changelog entry
with `applied=N deferred=M resolved_blocking=K remaining_blocking=L`,
and removes the pending-comments.json handoff.

## Wrap-up

Print a short summary to the user:

> Applied N comment(s). Deferred M. Resolved K blocking comment(s);
> L blocking remaining.
>
> If L > 0, the recipe still cannot finalize. Run `jig comment list {id}`
> to see what's left.

## Failure modes

| Symptom | Action |
|---------|--------|
| Pass A returns malformed JSON | Re-spawn the agent once with "Your previous output was not valid JSON. Return only the JSON object."; if still bad, fall back to per-doc-only mode (skip A, treat every comment as independent) and warn the user |
| A Pass B agent edits outside scope | Restore from stash, tell the user, abort |
| `marker_residue_check: residue_found` | Show the user the affected doc, ask them to clean up manually, abort before --finalize |
| User cancels mid-flow | If a stash was pushed, ask: "Restore from snapshot or keep partial changes?" |

## Notes for orchestrator

- Pass A is **one** Agent call across the whole recipe (cheap meta).
- Pass B is **parallel** Agent calls — one per doc — kept focused.
- Pass C is **deterministic** — no Agent, just `git diff` + AskUserQuestion.
- Never call `jig comment apply --finalize` until the user explicitly
  accepted in Pass C.
