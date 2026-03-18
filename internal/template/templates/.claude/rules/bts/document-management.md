---
paths:
  - ".bts/**"
---

# BTS Document Management

## Version Rule
**Never overwrite a draft.** Always create a new version:
- `drafts/v1.md` → `drafts/v2.md` → `drafts/v3.md` → ...
- Previous versions are preserved for reference and traceability.

## Mandatory Verification
**Every document modification triggers /verify.** No exceptions.
- After IMPROVE → /verify
- After incorporating debate conclusion → /verify
- After resolving simulation gaps → /verify

## Changelog
Every action is logged to `changelog.jsonl`:
```
bts recipe log {id} --action [type] --output [path]
```
Actions: research, draft, improve, verify, debate, simulate, audit, assess, sync-check

## Manifest
`manifest.json` tracks document relationships:
- `based_on`: which documents this was derived from
- `incorporates`: which debate conclusions are included
- `resolves`: which simulation gaps are addressed
- `verified_by`: which verification document confirmed this

## Sync Rule
Before finalizing, run `/sync-check` to verify:
- All debate conclusions reflected in current draft
- All simulation gaps resolved
- Current draft has been verified
- Code (if exists) matches spec

## Naming Conventions
```
research/v1.md                    # Research version 1
drafts/v1.md ~ vN.md             # Draft versions
debates/001-topic-name/           # Debate by sequence + topic
  round-1.md, round-2.md, ...
simulations/001-category.md       # Simulation by sequence + category
verifications/draft-vN.md         # Verification for specific draft
final.md                          # Final verified document
```
