---
name: bts-verify
description: >
  Verify a document for logical errors, contradictions, and unsupported claims.
  Includes mermaid flow path enumeration to find unspecified execution paths.
user-invocable: true
allowed-tools: Read Grep Glob Bash Agent WebSearch WebFetch mcp__context7__resolve-library-id mcp__context7__get-library-docs
argument-hint: "[file-path]"
context: fork
effort: max
---

# Logical Verification

Verify the specified document for logical correctness.

## Settings

Verification is the core quality gate — it uses the main session model by default.
If `agents.verifier` is explicitly set in `.bts/config/settings.yaml`, use that model instead.

Bash in this fork is ONLY for the two read-only commands in steps 1-2.
Never run state-mutating bts commands (log, create, finalize, …) or
write files from this fork.

## Steps

Do NOT read the target document in this fork — the verifier agent reads
it independently (single-read discipline; a copy here would be unused).

1. Run the deterministic graph analysis:
   ```bash
   bts graph paths $ARGUMENTS
   ```
   Capture the full output as GRAPH_ANALYSIS. It enumerates every
   mermaid diagram's paths, cycles, dead-ends, and orphans so the
   verifier does not have to enumerate paths itself.

2. Get focus hints from changes since the last verified revision:
   ```bash
   bts recipe verify-focus $ARGUMENTS
   ```
   Capture the output as FOCUS_DIFF. (On first verification it reports
   that no snapshot exists — pass that through as-is.)

2b. Get the adjudicated findings from previous rounds:
   ```bash
   bts recipe findings carry-forward {id} --doc $ARGUMENTS
   ```
   Capture the output as CARRY_FORWARD. Empty on the first round.
   This is what stops a fresh verifier from re-deriving settled points;
   see `bts-verification-protocol.md § Finding Identity`.

2c. **Decide this round's scope** (`bts-verification-protocol.md §
   Verification Scope` is authoritative):
   - FOCUS_DIFF reports no snapshot (first round) → **full**
   - The orchestrator is about to finalize → **full**
   - FOCUS_DIFF shows changed hunks → **delta** is allowed
   Remember the choice — the orchestrator passes it as `--scope` when
   recording the round, and a delta round can never satisfy completion.

3. Spawn Agent(verifier) with the following prompt, appending
   GRAPH_ANALYSIS, FOCUS_DIFF and CARRY_FORWARD verbatim at the end:

   ```
   You are a logical verification specialist. Read the document at $ARGUMENTS and check for:

   **Scope of this round: {full | delta}** (from step 2c)

   - **full** — verify the ENTIRE document. Every section, including
     ones no edit touched: an edit elsewhere can contradict them.
   - **delta** — verify the sections listed in the appended "Changes
     since last verified revision" block, PLUS their reference closure:
     every section that cites a term, anchor, interface, invariant or
     flow that the changed sections redefine. Follow those references
     out until they stop leading anywhere new. Do not re-derive sections
     outside that closure — a previous full pass already cleared them
     and re-litigating them is what makes this loop oscillate. If you
     cannot determine the closure with confidence, verify the whole
     document and say so.

   An "Adjudicated findings from previous rounds" block may be appended.
   Those points are already settled:
   - STILL OPEN — expect them unless the fix landed; reuse the given ID.
   - FIXED — report again ONLY if the fix was reverted or is wrong.
   - DISMISSED — adjudicated as not-a-defect. Do NOT re-raise.
   - DEFERRED — runtime watch-items, not spec defects.
   When you report a finding that already has an ID, use its exact title
   from that block so the ledger tracks it as the same finding rather
   than opening a duplicate.

   **Text-level verification:**
   - Contradictions: Does the document make conflicting claims?
   - Unsupported conclusions: Are conclusions drawn from insufficient evidence?
   - Causal errors: Are cause-effect relationships correctly established?
   - Missing premises: Are there hidden assumptions not stated?
   - Circular reasoning: Does any argument reference itself?

   **Flow-level verification (mermaid diagrams):**
   A deterministic "Mermaid Graph Analysis" block is appended to this
   prompt — bts computed it; its path enumeration is AUTHORITATIVE.
   Do not re-enumerate paths yourself. For each listed path/cycle:
   - Is the behavior along this path fully specified in the document text?
   - Flag paths where behavior is unspecified as GAPs
   - Every listed cycle needs a specified exit condition
   - Every listed dead-end / orphan state is a finding unless the text
     justifies it
   - Check for missing transitions: at each state, what happens on
     timeout? invalid input? resource exhaustion? concurrent access?
   Fallbacks (only then enumerate manually, the old way): the analysis
   reports unparsed edge lines or truncation for a diagram, or a
   diagram type it does not support.

   **Evidence policy for framework/platform claims**
   (authoritative policy: `.claude/rules/bts-evidence-policy.md` — this
   inline carries the rules; the official-domain examples list lives
   only in that file):

   Before classifying a claim about framework or platform internals
   (animation timing, reconciler behavior, async runtime semantics,
   memory/lifecycle rules, OS-level UI dismissal windows, etc.) as
   CRITICAL or MAJOR, attempt evidence gathering in this order:

   0. Cache first — the same claims recur every round, and network
      round trips are the slowest part of an iteration:
      ```bash
      bts evidence get --library <lib> --topic <topic> --claim "<claim>"
      ```
      HIT (exit 0) → reuse its verdict, Source and Gathered lines
      verbatim; skip steps 1-3 entirely. MISS (exit 10) → gather below,
      then record the outcome so the next round does not repeat it:
      ```bash
      bts evidence put --library <lib> --topic <topic> --claim "<claim>" \
        --verdict <contradicts|confirms|silent|unofficial|unavailable> \
        --gathered "<the Gathered: line>" --url <url>
      ```
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
     "evidence_resolved": {"removed": 1, "downgraded": 1},
     "findings": [
       {"severity": "major", "title": "token refresh failure has no error path", "anchor": "§5"},
       {"severity": "minor_resolvable", "title": "level header still says Level 2", "anchor": "§1"},
       {"severity": "minor_resolvable", "title": "retryCount declared twice", "anchor": "§4.2"},
       {"severity": "minor_deferred", "title": "scroll threshold needs a device measurement", "anchor": "§7"}
     ]
   }
   </bts-findings>
   ```

   The `findings` array is REQUIRED and feeds the findings ledger, which
   is what gives findings identity across rounds. Rules:
   - Exactly one entry per finding, in the same order as your numbered
     list below the block.
   - `severity` is one of `critical`, `major`, `minor_resolvable`,
     `minor_deferred`, `info` — it must match the counts. `bts` rejects
     the block if the array and the counts disagree, so do not
     hand-adjust one without the other.
   - `title` is the finding's identity. Keep it a stable, specific
     one-line statement of the defect. When the carry-forward block
     already lists this finding, reuse ITS title exactly — a reworded
     title opens a duplicate finding and hides the fact that the issue
     has persisted for several rounds.
   - `anchor` is the section the finding is about (optional but useful).

   `paths_total` MUST equal the paths_total from the appended Mermaid
   Graph Analysis block (plus any paths you enumerated manually for
   fallback diagrams only). `paths_unspecified` is your judgment of how
   many of those paths lack specified behavior.

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

4. Collect the verifier's findings
5. Report results to the user with severity counts, and state which
   scope the round used (`full` or `delta`) so the orchestrator records
   it correctly.

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
3. The orchestrator records the verify-log entry atomically, passing
   the verified document via `--doc` and this round's scope:
   ```bash
   bts recipe log {id} --from-verification .bts/specs/recipes/{id}/verification.md \
     --doc {verified-doc-path} --scope {full|delta} --dimension {verify|audit|simulate ...}
   ```
   The CLI parses the block itself, so the two sources cannot drift.
   Explicit `--critical/--major/--minor-resolvable/--minor-deferred`
   flags remain as a fallback; NEVER use legacy `--minor` (it maps all
   minors to blocking [resolvable]).

   `--doc` is not optional any more. It scopes convergence to this
   document, feeds the findings ledger, and snapshots the revision.
   Without it the round is recorded as unscoped, no findings are
   tracked, and stagnation detection is unavailable.

   **The `--doc` path must resolve.** A bare `draft.md` is resolved
   against the recipe directory and a path containing a separator
   against the project root; anything else hashes nothing, and a round
   with no `doc_hash` cannot be replicated against and cannot complete.
   `bts recipe log` warns on stderr when it could not read the file —
   that warning is a failed round, not a note.

   `--dimension` is not optional either. Name every semantic pass whose
   findings are in this block and no others — `--dimension verify` alone
   when only this skill ran, plus `--dimension audit` and/or
   `--dimension simulate` when their findings were merged into the same
   verification.md. The budget compares a round only against rounds of
   the same dimensions and scope, so an inflated claim here reintroduces
   exactly the apples-to-oranges verdict the flag removes.

   Declaring nothing is NOT a shortcut. A round with no dimensions is
   comparable with nothing and counts toward completion not at all —
   the honest `--dimension verify` and the silent round fail the same
   gate, so there is no version of this where omitting the flag helps.
   The rounds that finish a document must run all three, because that
   is what `<bts>DONE</bts>` asks for; see
   `bts-verification-protocol.md § Completion Evidence`.

   **This command can fail on purpose.** A non-zero exit with
   `[CONVERGENCE FAILED]` means the convergence budget is exhausted:
   the round IS logged, but the loop must stop and ask the user rather
   than starting another IMPROVE cycle. See
   `bts-verification-protocol.md § Convergence`.

Do not hand-edit verify-log.jsonl, verification.md, or findings.jsonl.

Migrate-seeded blocks (produced by `bts migrate verification`) carry
`"source": "migrated-from-verify-log"`. Cross-check mismatches on
those are still flagged but labeled `[migrate-seeded]` so operators
can distinguish migration artifacts from real drift.
