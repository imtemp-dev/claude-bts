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
Actions: research, draft, improve, verify, debate, simulate, audit, assess, sync-check, finalize, implement, test, sync, status, adjudicate, review

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

## Implementation Documents

After implementation, additional artifacts are tracked:
- `tasks.json` — implementation task decomposition (type: "implementation")
- `test-results.json` — test execution results (type: "test-result")
- `deviation.md` — spec↔code differences (type: "deviation")

These MUST be registered in `manifest.json` with appropriate `based_on` references.

## final.md Sync Policy

`final.md` is the verified spec from the blueprint phase. When `/sync` updates it
to reflect actual implementation:
1. **Preserve the original**: Copy `final.md` → `final.pre-sync.md` before any modification
2. **Update in place**: Modify `final.md` with implementation reality
3. **Track in manifest**: Register `final.pre-sync.md` as type "draft" and update `final.md` entry
4. **Record deviations**: All differences go to `deviation.md`

This is the ONE exception to the "never overwrite" rule — `final.md` is a living
document that bridges spec and implementation.

## Follow-up Lifecycle

After a recipe reaches `complete`, its documents stay alive:
- `deviation.md` lists items for follow-up (not-implemented, deviations)
- `final.md` is the living spec — it can be updated through follow-up work
- A new recipe can reference the completed one's deviation.md as input
- project-status.md shows follow-up items in "Next Steps"

Deviations do NOT block completion. They are a report, not a gate.

## Global Documents

`project-status.md` (at `.bts/state/project-status.md`) is a **derived global document**
that aggregates state across all recipes. It is NOT tracked in per-recipe manifests
because it spans multiple recipes.

## Naming Conventions
```
research/v1.md                    # Research version 1
drafts/v1.md ~ vN.md             # Draft versions
debates/001-topic-name/           # Debate by sequence + topic
  round-1.md, round-2.md, ...
simulations/001-category.md       # Simulation by sequence + category
verifications/draft-vN.md         # Verification for specific draft
final.md                          # Final verified document
final.pre-sync.md                 # Original final.md before sync
tasks.json                        # Implementation tasks
test-results.json                 # Test execution results
deviation.md                      # Spec↔code deviation report
diagnosis.md                      # Bug fix: symptom, root cause, impact
fix-spec.md                       # Bug fix: change specification
review.md                         # Code review findings report
```
