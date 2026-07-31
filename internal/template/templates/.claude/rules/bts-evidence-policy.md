---
paths:
  - ".bts/**"
authoritative_for:
  - evidence_policy
  - official_architecture_grounding
---

# BTS Evidence Policy

Single source of truth for how BTS skills and agents ground
framework/platform claims. `bts-verify` and `bts-audit` embed the
gathering order and reclassification rules inline in their agent
prompts, but the official-domain examples list lives ONLY in this file —
the inline copies state the rule and point here, so the list cannot
drift again.

## When evidence is required

Any claim about framework or platform internals that influences a
CRITICAL or MAJOR finding, a debate position, an architect alternative,
or a research conclusion: animation timing, reconciler behavior, async
runtime semantics, memory/lifecycle rules, OS-level UI behavior,
recommended architecture patterns, API contracts.

## Gathering order

0. **Local evidence cache** — always first. The verification loop
   re-checks the same framework claims on every round, and network round
   trips are the slowest part of an iteration.

   ```bash
   bts evidence get --library <lib> --topic <topic> --claim "<claim>"
   ```

   - **HIT** (exit 0): reuse the cached verdict and reproduce its
     `Source:` and `Gathered:` lines verbatim. Skip steps 1-3.
   - **MISS** (exit 10): gather via steps 1-3, then record the outcome
     so later rounds do not repeat the work:
     ```bash
     bts evidence put --library <lib> --topic <topic> --claim "<claim>" \
       --verdict <contradicts|confirms|silent|unofficial|unavailable> \
       --gathered "<Gathered: line>" --url <url> --summary "<one line>"
     ```

   Cache semantics that matter for correctness:
   - `contradicts` and `confirms` REQUIRE at least one `--url`; the CLI
     refuses them otherwise. A cached verdict is replayed verbatim into
     later rounds, so an uncited one would launder an invented citation
     into every future round.
   - `unavailable` entries expire after **one hour**, so a Context7
     outage never pins a claim to "evidence unavailable" beyond the
     incident. Successful lookups live for `verify.evidence_ttl_days`
     (default 30).
   - The cache is machine-local (`.bts/local/evidence-cache.jsonl`) and
     never committed. It is a latency optimisation, not project truth —
     when in doubt, `bts evidence prune` and re-gather.

1. **Context7 MCP** (preferred): `mcp__context7__resolve-library-id` →
   `mcp__context7__get-library-docs` with a topic derived from the claim.
   If Context7 is UNAVAILABLE (tools not present, server error, auth or
   rate-limit failure), retry AT MOST once, then move to step 2
   immediately — do not burn turns on a dead tier. Record
   `Context7:unavailable` (not `miss`) so downstream cycles know the
   service failed rather than the docs not existing.
2. **WebFetch on OFFICIAL domains** when Context7 misses. An official
   domain is **the platform/framework vendor's OWN primary documentation
   domain** — the domain the vendor itself publishes docs on. Apply the
   rule; the list below is non-exhaustive examples, not a closed
   whitelist:
   developer.apple.com, developer.android.com, react.dev, nextjs.org,
   nodejs.org, docs.swift.org, kotlinlang.org, go.dev, docs.python.org,
   docs.djangoproject.com, guides.rubyonrails.org, spring.io,
   svelte.dev, vuejs.org, angular.dev, flutter.dev, dart.dev,
   doc.rust-lang.org, developer.mozilla.org (web platform standards),
   learn.microsoft.com, docs.oracle.com, pytorch.org, tensorflow.org,
   kubernetes.io, official GitHub RFCs/issues in the framework's own
   repo, WWDC / Google I/O official transcripts.
3. **WebSearch as last resort**, always with `site:` filters on the
   vendor's official domains (same rule as above). Never generic queries.

NOT evidence: Medium, dev.to, personal blogs, StackOverflow (lead only),
unofficial tutorials, unversioned docs.

## Reclassification by outcome (verify/audit findings)

| Outcome | Action |
|---------|--------|
| Official source CONTRADICTS the claim | CRITICAL, cite URL |
| Official source CONFIRMS | REMOVE the finding |
| Official source SILENT, affects user code | keep MAJOR (defensive) |
| Official source SILENT, purely framework-internal | downgrade to MINOR |
| Only non-official sources found | downgrade to MINOR, note why |

## Citation format

- `Source: <URL>` — one line per claim; multiple URLs separated by ` | `
- `Gathered: [Context7:<hit|miss|unavailable> | WebFetch:<url>:<status> | WebSearch:<n>]`
  — `miss` = Context7 answered but has no matching library/topic;
  `unavailable` = server error, rate limit, or not configured. Only
  `unavailable` is worth re-trying Context7 for in a later cycle.
- Never invent citations. Failed fetch → write "Evidence unavailable"
  and keep the conservative classification.

## Budget

Evidence-gather only CRITICAL/MAJOR candidates (plus decision-driving
claims in debate/architect), cap 5 per run to keep iteration time
bounded. Minor findings need no evidence.

## Official recommended architectures

When a recipe targets a platform/framework with an officially documented
recommended architecture, `/bts-research` MUST record it (research doc
`## Official Guidance` section) and `/bts-architect` MUST include it as
one of the alternatives. "Official" means the platform vendor's own
guidance, e.g.: Android Guide to App Architecture
(developer.android.com/topic/architecture), Apple's SwiftUI data-flow
and app-structure guidance, React's "Thinking in React" and framework
docs, Next.js App Router docs, Go project layout guidance (go.dev),
Django/Rails official application-structure guides.

The official pattern may LOSE the debate to a simpler decomposition —
but it must be on the table with a `Source:` line, and rejecting it
requires a stated reason in the `<!-- architect-decision -->` block's
Rejected list.

Guidance must be **version-matched**: `/bts-research` Step 3.5 records
the project's target framework majors (`Target versions:` line) from the
dependency manifests, and the fetched guidance must apply to those
majors — pattern advice often flips across majors (SwiftUI
`ObservableObject` → `@Observable`, React class components → hooks).
A mismatch is not disqualifying, but it must be recorded and addressed
in the architect debate. When Context7 is unavailable, version-match via
the vendor docs' own version selector / versioned URL paths instead
(e.g. `docs.djangoproject.com/en/{major}/`) and note it in `Gathered:`.
