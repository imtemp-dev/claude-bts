---
paths:
  - ".bts/**"
authoritative_for:
  - evidence_policy
  - official_architecture_grounding
---

# BTS Evidence Policy

Single source of truth for how BTS skills and agents ground
framework/platform claims. `bts-verify` and `bts-audit` embed an inline
copy of the reclassification rules inside their agent prompts — keep
those in sync with this file when editing.

## When evidence is required

Any claim about framework or platform internals that influences a
CRITICAL or MAJOR finding, a debate position, an architect alternative,
or a research conclusion: animation timing, reconciler behavior, async
runtime semantics, memory/lifecycle rules, OS-level UI behavior,
recommended architecture patterns, API contracts.

## Gathering order

1. **Context7 MCP** (preferred): `mcp__context7__resolve-library-id` →
   `mcp__context7__get-library-docs` with a topic derived from the claim.
2. **WebFetch on OFFICIAL domains** when Context7 misses:
   developer.apple.com, developer.android.com, react.dev, nodejs.org,
   docs.swift.org, kotlinlang.org, pytorch.org, tensorflow.org,
   learn.microsoft.com, docs.oracle.com, go.dev, docs.python.org,
   nextjs.org, vuejs.org, angular.dev, official GitHub RFCs/issues in
   the framework's own repo, WWDC / Google I/O official transcripts.
3. **WebSearch as last resort**, always with `site:` filters on the same
   official domains. Never generic queries.

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
