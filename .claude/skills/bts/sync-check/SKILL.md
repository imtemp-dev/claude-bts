---
name: bts-sync-check
description: >
  Verify all documents within a recipe are in sync — debate conclusions
  reflected, simulation gaps resolved, drafts verified.
user-invocable: true
allowed-tools: Read Bash
argument-hint: "[recipe-id]"
---

# Document Sync Check

Verify all documents are consistent with each other.

## Steps

1. Run sync-check via bts binary:
   ```bash
   bts sync-check $ARGUMENTS
   ```
   This checks manifest.json for unverified drafts, unincorporated debates, unresolved gaps.

2. Additionally, read the current draft and manually check:
   - Do debate conclusions match what's written in the draft?
   - Are simulation-found gaps actually addressed (not just mentioned)?
   - If code exists, does the spec match the code?
   - Are version references consistent (e.g., "see research v1" → does v1 exist?)

3. Report findings:
   ```
   ## Sync Check Results

   ### Debates
   - debate-001 "OAuth2 vs JWT": ✓ reflected in draft/v3
   - debate-002 "Session store": ✗ NOT in current draft

   ### Simulations
   - simulation-001 gap #1: ✓ resolved in draft/v4
   - simulation-001 gap #3: ✗ STILL OPEN

   ### Verifications
   - draft/v5: ✗ NOT VERIFIED (verify was not run after last change)

   ### Code (if exists)
   - src/auth/oauth.ts: ✓ matches spec
   - src/auth/session.ts: ✗ spec says Redis but code uses in-memory

   Summary: 3 sync issues found
   ```

4. If issues found, recommend specific actions to resolve each one.
