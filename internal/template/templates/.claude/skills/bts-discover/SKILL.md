---
name: bts-discover
description: >
  Explore the intent and direction behind a feature request through
  multi-turn conversation. Clarifies purpose, users, problem, and
  direction before scoping begins.
user-invocable: true
allowed-tools: Read Write Edit Grep Glob Agent AskUserQuestion WebSearch WebFetch mcp__context7__resolve-library-id mcp__context7__get-library-docs
argument-hint: "\"feature or project description\""
effort: high
---

# Intent Discovery

Explore the intent behind: $ARGUMENTS

## Resume Check

Check for an existing recipe with phase `discovery`:
```bash
bts recipe status
```
If found, read `.bts/specs/recipes/{id}/intent.md`:
- Status EXPLORING → continue the conversation from where it left off
- Status CONFIRMED → skip discovery, proceed to blueprint

If no active recipe, create one with the type
`.claude/rules/bts-recipe-protocol.md` § Choosing a recipe type gives
(blueprint when nothing else is said):
```bash
bts recipe create --type <type> --topic "$ARGUMENTS"
```
Capture the output — this is the recipe ID (e.g., `r-001-mcp-server`).
Use this ID for all subsequent `bts recipe log` calls.

## Discovery Conversation

Your goal is to understand **why** this needs to exist, **who** it serves,
and **what problem** it solves — through natural conversation with the user.

**How to conduct the conversation:**

Do NOT use a fixed questionnaire. Instead, reason about what you need to
understand based on what the user has told you so far. Ask one or two
questions at a time as text output. Adapt your questions based on each response.

**Areas to explore** (not a checklist — pursue what matters for this request):
- What problem does this solve? What's painful about the current state?
- Who will use this? What's their context and expertise?
- Why now? What triggered this request?
- What does success look like? How will you know it worked?
- Are there existing solutions? Why not use them?
- What constraints exist? (budget, timeline, compliance, tech)

**When to research:**
If the user mentions a domain, technology, or constraint you're uncertain about,
use WebSearch or Agent(Explore) to investigate before asking more questions.
Share relevant findings with the user — this builds shared understanding.

**After each meaningful exchange:**
Update `.bts/specs/recipes/{id}/intent.md` with the current understanding.
This ensures the conversation survives context compaction or session breaks.

## intent.md Format

```markdown
# Intent: {topic}

Status: EXPLORING

## Problem
{What is painful or missing today}

## Purpose
{Why this needs to exist — the motivation}

## Users
{Who will use this, their context}

## Success Criteria
{How to know this succeeded}

## Direction
{The agreed direction — NOT scope, but the path forward}

## Key Decisions
- {Decision made during conversation, with reasoning}

## Research Notes
{Any web research findings, competitive analysis, technical investigation}
```

## Completion

The conversation ends when **both sides agree** on the direction.
There is no turn limit — simple requests may take 2-3 turns,
complex ones may take 10+.

When you believe you have enough understanding:
1. Present a summary of the intent to the user
2. Ask for confirmation.  When `review.surface` in `.bts/config/settings.yaml`
   is `artifact` (the default) and the Artifact tool is available here, or
   `local`, the review runs on a page: `/bts-review-page {id} intent` draws
   the intent, publishes or serves it, answers the notes the user pins on it and records the
   confirmation with `bts recipe confirm {id} intent --via review`; skip
   step 3 then.  Otherwise ask using AskUserQuestion:
   - "Direction confirmed — proceed" → record it (step 3)
   - "Needs adjustment" → continue conversation

   When the user answers somewhere else (through another session, or
   later), record the wait so it is visible to the next session and to
   `bts recipe status`: `bts recipe decision hold {id} --key intent-confirm
   --question "Confirm the intent in intent.md?"`.  The confirm command
   below resolves it when the answer arrives.  The same applies to the
   scope confirmation.
3. After confirmation, record it with the user's words; never edit the
   Status line by hand:
   ```bash
   bts recipe confirm {id} intent --answer "<the user's words>"
   ```
   It resolves the confirmation decision and sets intent.md's Status line
   to CONFIRMED.  Scope is confirmed the same way (`... confirm {id} scope`),
   and again after any revision: a revised scope is unconfirmed until the
   user has seen it.
4. Log:
   ```bash
   bts recipe log {id} --phase discovery --action discover --output intent.md --result "intent confirmed"
   ```

The recipe stays in phase `discovery` until blueprint advances it.
