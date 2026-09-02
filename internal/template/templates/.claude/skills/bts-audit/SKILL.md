---
name: bts-audit
description: >
  Audit a document for completeness. Find missing scenarios, unconsidered
  edge cases, and hidden assumptions. Includes mermaid branch completeness
  analysis. Runs as the auditor agent itself — one context, no nested agent.
user-invocable: true
allowed-tools: Read Grep Glob Bash WebSearch WebFetch mcp__context7__resolve-library-id mcp__context7__get-library-docs
argument-hint: "[file-path]"
context: fork
agent: auditor
effort: max
---

# Completeness Audit

Audit the specified document for missing items.

## Who runs this

This skill runs **inside the `auditor` agent** (frontmatter
`agent: auditor`). You are the auditor: run the two read-only commands
below, then read and audit the document yourself. Do not spawn
`Agent(auditor)` — the fork-that-spawns-a-fork shape cost $1–4 and a
polling loop per round for no second opinion.

Bash is ONLY for the read-only commands in steps 1–1b and for
`bts evidence get/put`. Never run state-mutating bts commands or write
files from this fork — the orchestrator records the round.

## Settings

Audit requires finding what's missing — it uses the main session model by default.
If `agents.auditor` is explicitly set in `.bts/config/settings.yaml`, that
model applies via the agent's frontmatter.

## Steps

1. Run the deterministic graph analysis:
   ```bash
   bts graph paths $ARGUMENTS
   ```
   Capture the full output as GRAPH_ANALYSIS.

1b. Get the adjudicated findings from previous rounds:
   ```bash
   bts recipe findings carry-forward {id} --doc $ARGUMENTS
   ```
   Capture the output as CARRY_FORWARD (empty on the first round).
   Those gaps were already raised on this document: do not re-derive
   them, never re-raise a DISMISSED one, and when reporting a gap that
   already has an ID there, reuse its exact title so the ledger tracks
   it as the same finding rather than opening a duplicate.

2. Read the document at $ARGUMENTS and audit it.

   Your goal: find everything the document fails to address that could cause
   problems at runtime, during deployment, or under adversarial conditions.

   **Content completeness:**
   Think about failure modes, boundary conditions, unstated assumptions,
   missing integrations, security gaps, and operational concerns. Do not
   limit yourself to a fixed checklist — reason about what this specific
   system needs and what the document leaves unanswered.

   **Flow completeness (mermaid diagrams):**
   GRAPH_ANALYSIS's path/cycle/dead-end/orphan enumeration is
   AUTHORITATIVE; do not re-enumerate (fall back to manual enumeration
   only for diagrams it flags as unparsed, truncated, or unsupported).
   Using it:
   - At EVERY decision node: are ALL branches specified? (yes/no/error/timeout)
   - At EVERY state: what happens on timeout? invalid input? resource exhaustion?
     concurrent access? If unspecified, flag as a completeness gap.
   - States appearing in only ONE listed path are single-path-reachable
     (fragile — what if that path fails?)
   - Listed dead-ends and orphans are completeness gaps unless the text
     justifies them.
   - For each error state: is the error message/response defined? Is cleanup specified?
   - Count: total decision nodes, branches specified, branches missing.

   **Evidence policy for framework/platform claims**
   (authoritative policy: `.claude/rules/bts-evidence-policy.md` — this
   inline carries the rules; the official-domain examples list lives
   only in that file):

   Before classifying a claim about framework or platform internals
   (animation timing, reconciler behavior, async runtime semantics,
   memory/lifecycle rules, OS-level UI dismissal windows, known failure
   modes, etc.) as CRITICAL or MAJOR, attempt evidence gathering in this
   order:

   0. Cache first — these claims recur every round and network round
      trips dominate iteration time:
      ```bash
      bts evidence get --library <lib> --topic <topic> --claim "<claim>"
      ```
      HIT (exit 0) → reuse its verdict, Source and Gathered lines
      verbatim and skip steps 1-3. MISS (exit 10) → gather below, then
      record the result with `bts evidence put` (same flags plus
      `--verdict` and `--gathered`).
   1. Context7 MCP (preferred): mcp__context7__resolve-library-id then
      mcp__context7__get-library-docs with a topic from the claim.
      If the Context7 tools are absent or return errors (rate limit,
      auth), retry AT MOST once, then fall through to step 2 — record
      Context7:unavailable (not miss).
   2. WebFetch on OFFICIAL domains when Context7 misses or is
      unavailable. Official
      domain = the platform/framework vendor's OWN primary documentation
      domain (e.g. developer.android.com, react.dev, go.dev — apply the
      rule, not a memorized list; the full examples list lives in
      `.claude/rules/bts-evidence-policy.md`). Official GitHub
      RFCs/issues in the framework's own repo and WWDC / Google I/O
      official transcripts also count.
   3. WebSearch as last resort, always with site: filters on the
      vendor's official domains (same rule). Never generic queries.

   NOT evidence: Medium, dev.to, personal blogs, StackOverflow (lead only),
   unofficial tutorials, unversioned docs.

   Reclassify by outcome:
   - Official source CONTRADICTS → CRITICAL, cite URL.
   - Official source CONFIRMS → REMOVE finding.
   - Official source SILENT, affects user code → keep as MAJOR (defensive).
   - Official source SILENT, purely framework-internal → downgrade to MINOR.
   - Only non-official sources found → downgrade to MINOR, note why.

   Citations:
   - Each evidence-resolved finding MUST include a `Source:` line with URLs.
   - Never invent citations. If a fetch fails, write "Evidence unavailable"
     and keep the conservative classification from the table above.
   - For every claim you attempted to evidence, include a line
     `Gathered: [Context7:<hit|miss|unavailable> | WebFetch:<url>:<status> | WebSearch:<n>]`
     so downstream improve cycles can see what was tried.

   Budget: evidence-gather only CRITICAL/MAJOR candidates, cap at 5 findings
   per run to keep iteration time bounded. Minor findings need no evidence.

   **Severity classification:**

   See `bts-verification-protocol.md § Severity Classification` for the
   authoritative definitions. In audit context:
   - **critical**: Will cause runtime failure if not addressed
   - **major**: Important gap that should be filled before implementation
   - **minor [resolvable]**: Fixable in the spec (see protocol)
   - **minor [deferred]**: Only confirmable at implementation/runtime (see protocol)
   - **info**: Improvement suggestions

   Every `[deferred]` minor MUST include a `Why-deferred:` line naming the
   specific runtime observation that would resolve it.

3. Output — your final message, in this order:

   **Structured findings block (REQUIRED, exact format), FIRST:**

   ```
   <bts-findings>
   {
     "critical": 0,
     "major": 2,
     "minor_resolvable": 1,
     "minor_deferred": 3,
     "info": 0,
     "branches_total": 12,
     "branches_unspecified": 2,
     "evidence_resolved": {"removed": 0, "downgraded": 1},
     "findings": [
       {"severity": "major", "title": "no behaviour specified for concurrent session refresh", "anchor": "§6"},
       {"severity": "major", "title": "deployment rollback path is unaddressed", "anchor": "§9"},
       {"severity": "minor_resolvable", "title": "glossary omits the term 'snap'", "anchor": "§1"},
       {"severity": "minor_deferred", "title": "cold-start budget needs a production measurement", "anchor": "§8"},
       {"severity": "minor_deferred", "title": "OCR accuracy on glossy covers is unmeasured", "anchor": "§4"},
       {"severity": "minor_deferred", "title": "keyboard dismissal window is device-specific", "anchor": "§5"}
     ]
   }
   </bts-findings>
   ```

   The `findings` array is REQUIRED: one entry per finding, in the same
   order as the numbered list, with severities that match the counts
   exactly (`bts` rejects a block whose array and counts disagree).
   `title` is the finding's identity — keep it stable across rounds, and
   reuse the carry-forward block's title verbatim when it already lists
   the gap.

   **Then** the findings as a numbered list with severity tags. For each
   finding also include (when applicable):
     Source: <URL> | <URL>
     Gathered: <Context7|WebFetch|WebSearch summary>
     Why-deferred: <runtime observation that would resolve it>   (deferred only)

   **Then**: "Branch coverage: N/M decision branches specified (N%).
   Evidence-resolved: X (removed Y, downgraded Z). Framework-claim findings: W.
   Minors: R resolvable, D deferred."

## Division of labor

This fork has no Write tool and must not log. The ORCHESTRATOR writes
your output verbatim to `.bts/specs/recipes/{id}/audit.md` and joins its
block into the round with `bts recipe log … --merge audit.md` (see
`bts-verify` § Count consistency). One batch, one round.
