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

1. **Context7 MCP** (preferred): `mcp__context7__resolve-library-id` →
   `mcp__context7__get-library-docs` with a topic derived from the claim.
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
- `Gathered: [Context7:<hit|miss> | WebFetch:<url>:<status> | WebSearch:<n>]`
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
in the architect debate.
