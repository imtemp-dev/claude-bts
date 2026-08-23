---
paths:
  - ".jig/**"
authoritative_for:
  - draft_file
  - manifest
  - sync_rule
  - naming_conventions
---

# jig Document Management

## Draft File
A single `draft.md` is maintained at the recipe root. Use Edit-based
incremental modifications instead of rewriting the full file. The draft
is written once (Write), then edited in place (Edit) for each improvement cycle.

## Mandatory Verification
**Every document modification triggers /verify.** No exceptions.
- After IMPROVE → /verify
- After incorporating debate conclusion → /verify
- After resolving simulation gaps → /verify

## Changelog
Every action is logged to `changelog.jsonl`:
```
jig recipe log {id} --action [type] --output [path]
```
Actions: research, draft, improve, verify, debate, simulate, audit, assess, sync-check, finalize, implement, test, sync, status, adjudicate, review

## Manifest
`manifest.json` tracks document relationships:
- `based_on`: which documents this was derived from
- `incorporates`: which debate conclusions are included
- `resolves`: which simulation gaps are addressed
- `verified_by`: verification document path (always `verification.md`)

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

`final.md` is the verified spec from the spec phase. When `/sync` updates it
to reflect actual implementation:
1. **Preserve the original**: Copy `final.md` → `final.pre-sync.md` before any modification
2. **Update in place**: Modify `final.md` with implementation reality
3. **Track in manifest**: Register `final.pre-sync.md` as type "draft" and update `final.md` entry
4. **Record deviations**: All differences go to `deviation.md`

This is consistent with `draft.md` — both are edited in place as living documents.

## Follow-up Lifecycle

After a recipe reaches `complete`, its documents stay alive:
- `deviation.md` lists items for follow-up (not-implemented, deviations)
- `final.md` is the living spec — it can be updated through follow-up work
- A new recipe can reference the completed one's deviation.md as input
- project-status.md shows follow-up items in "Next Steps"

Deviations do NOT block completion. They are a report, not a gate.

## Global Documents

`vision.md` (at `.jig/specs/vision.md`) records the project's final product vision.
Created during spec scoping when the project is greenfield or the feature is large.
Status: DRAFT → CONFIRMED. Updatable on direction changes.

`roadmap.md` (at `.jig/specs/roadmap.md`) tracks ordered recipe decomposition.
Each item maps to one recipe. Updated by `/jig-status` when recipes complete.

`project-status.md` (at `.jig/specs/project-status.md`) is a **derived global document**
that aggregates state across all recipes. It is NOT tracked in per-recipe manifests
because it spans multiple recipes.

## Naming Conventions
```
research/v1.md                    # Research document
draft.md                          # Single draft, Edit-based
verification.md                   # Single verification, overwritten each cycle
debates/001-topic-name/           # Debate by sequence + topic
  round-1.md, round-2.md, ...
simulations/001-category.md       # Simulation by sequence + category
final.md                          # Final verified document
final.pre-sync.md                 # Original final.md before sync
tasks.json                        # Implementation tasks
test-results.json                 # Test execution results
deviation.md                      # Spec↔code deviation report
diagnosis.md                      # Bug fix: symptom, root cause, impact
fix-spec.md                       # Bug fix: change specification
review.md                         # Code review findings report
perspectives.md                   # Debug: multi-perspective analysis
project-map.md                    # Level 0: layer overview (auto-synced)
layers/{name}.md                  # Level 1: layer detail (on-demand)
intent.md                         # Intent discovery (per-recipe)
vision.md                         # Product vision (project-level)
roadmap.md                        # Ordered recipe decomposition (project-level)
```
