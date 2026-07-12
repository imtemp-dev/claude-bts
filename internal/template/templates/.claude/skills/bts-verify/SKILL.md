---
name: bts-verify
description: >
  Verify a document for logical errors, contradictions, and unsupported claims.
  Includes mermaid flow path enumeration to find unspecified execution paths.
user-invocable: true
allowed-tools: Read Grep Glob Agent WebSearch WebFetch mcp__context7__resolve-library-id mcp__context7__get-library-docs
argument-hint: "[file-path]"
context: fork
effort: max
---

# Logical Verification

Verify the specified document for logical correctness.

## Settings

Verification is the core quality gate — it uses the main session model by default.
If `agents.verifier` is explicitly set in `.bts/config/settings.yaml`, use that model instead.

## Steps

1. Read the target document fully

2. Spawn Agent(verifier) with the following prompt:

   ```
   You are a logical verification specialist. Read the document at $ARGUMENTS and check for:

   **Text-level verification:**
   - Contradictions: Does the document make conflicting claims?
   - Unsupported conclusions: Are conclusions drawn from insufficient evidence?
   - Causal errors: Are cause-effect relationships correctly established?
   - Missing premises: Are there hidden assumptions not stated?
   - Circular reasoning: Does any argument reference itself?

   **Flow-level verification (mermaid diagrams):**
   If the document contains mermaid diagrams (stateDiagram, flowchart, etc.):
   - Enumerate ALL possible paths from start to end in each diagram
   - For EACH path: is the behavior fully specified in the document text?
   - Flag paths where behavior is unspecified as GAPs
   - Check for dead-end states (states with no exit transition)
   - Check for orphan states (states with no entry transition)
   - Check that every error/failure state has a recovery or terminal path
   - Check for missing transitions: at each state, what happens on
     timeout? invalid input? resource exhaustion? concurrent access?

   **Evidence policy for framework/platform claims**
   (authoritative copy: `.claude/rules/bts-evidence-policy.md` — keep
   this inline mirror in sync):

   Before classifying a claim about framework or platform internals
   (animation timing, reconciler behavior, async runtime semantics,
   memory/lifecycle rules, OS-level UI dismissal windows, etc.) as
   CRITICAL or MAJOR, attempt evidence gathering in this order:

   1. Context7 MCP (preferred): mcp__context7__resolve-library-id then
      mcp__context7__get-library-docs with a topic from the claim.
   2. WebFetch on OFFICIAL domains only when Context7 misses:
      developer.apple.com, developer.android.com, react.dev, nodejs.org,
      docs.swift.org, kotlinlang.org, pytorch.org, tensorflow.org,
      learn.microsoft.com, docs.oracle.com, official GitHub RFCs/issues
      in the framework's own repo, WWDC / Google I/O official transcripts.
   3. WebSearch as last resort, always with site: filters on the same
      official domains. Never generic queries.

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
     `Gathered: [Context7:<hit|miss> | WebFetch:<url>:<status> | WebSearch:<n>]`
     so downstream improve cycles can see what was tried.

   Budget: evidence-gather only CRITICAL/MAJOR candidates, cap at 5 findings
   per run to keep iteration time bounded. Minor findings need no evidence.

   **Severity classification:**

   See `bts-verification-protocol.md § Severity Classification` for the
   authoritative definitions of critical, major, minor [resolvable],
   minor [deferred], and info. Tag every finding with exactly one of
   these severity levels.

   **Structured findings block (REQUIRED, exact format):**

   Emit this block verbatim at the TOP of verification.md, with valid JSON
   inside. `bts validate` and the stop hook parse this block; numbers in
   the free-text summary below are informational only.

   ```
   <bts-findings>
   {
     "critical": 0,
     "major": 1,
     "minor_resolvable": 2,
     "minor_deferred": 1,
     "info": 0,
     "paths_total": 7,
     "paths_unspecified": 0,
     "evidence_resolved": {"removed": 1, "downgraded": 1}
   }
   </bts-findings>
   ```

   Output your findings as a numbered list with severity tags AFTER the
   block. For each finding also include (when applicable):
     Source: <URL> | <URL>
     Gathered: <Context7|WebFetch|WebSearch summary>
     Why-deferred: <runtime observation that would resolve it>   (deferred only)

   Summary line:
     Text issues: N. Flow path issues: N. Total paths analyzed: N.
     Evidence-resolved: X (removed Y, downgraded Z). Framework-claim findings: W.
     Minors: R resolvable, D deferred.
   ```

3. Collect the verifier's findings
4. Report results to the user with severity counts

## Count consistency (Phase 22)

The counts embedded in the `<bts-findings>` block MUST match the most
recent `verify-log.jsonl` entry. `bts validate` cross-checks the two —
drift between them surfaces as `verification_log_mismatch` (major)
per-field.

Division of labor — this skill runs in a fork WITHOUT Write/Bash, so it
cannot save files or run the CLI itself:
1. The verifier agent returns its findings, `<bts-findings>` block first.
2. The ORCHESTRATOR (main loop, after the fork returns) writes that
   output to verification.md VERBATIM — the block must land
   byte-identical, no re-summarizing.
3. The orchestrator records the verify-log entry atomically:
   ```bash
   bts recipe log {id} --from-verification .bts/specs/recipes/{id}/verification.md
   ```
   The CLI parses the block itself, so the two sources cannot drift.
   Explicit `--critical/--major/--minor-resolvable/--minor-deferred`
   flags remain as a fallback; NEVER use legacy `--minor` (it maps all
   minors to blocking [resolvable]).

Do not hand-edit verify-log.jsonl or verification.md.

Migrate-seeded blocks (produced by `bts migrate verification`) carry
`"source": "migrated-from-verify-log"`. Cross-check mismatches on
those are still flagged but labeled `[migrate-seeded]` so operators
can distinguish migration artifacts from real drift.
