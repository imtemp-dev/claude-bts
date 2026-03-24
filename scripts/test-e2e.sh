#!/bin/bash
# test-e2e.sh — bts integration test
set -e

BTS="$(cd "$(dirname "$0")/.." && pwd)/bin/bts"
TEST_DIR=$(mktemp -d)
trap 'rm -rf "$TEST_DIR"' EXIT
cd "$TEST_DIR"

echo "=== bts E2E Test ==="
echo "Binary: $BTS"
echo "Test dir: $TEST_DIR"
echo ""

# 1. Init
$BTS init . > /dev/null
[ -f .claude/skills/bts-verify/SKILL.md ] && echo "✓ 1. init" || { echo "✗ 1. init"; exit 1; }

# 2. Verify (no code, from-scratch spec)
printf "# OAuth2 Design\n\n**Auth component** handles user login.\n**Session manager** stores tokens.\nUses **Express** framework with **Passport.js**.\nData flows from **login form** to **OAuth provider** to **callback handler**.\nOn error, returns **401 Unauthorized**.\n" > spec.md
$BTS verify --no-code spec.md | grep -q '"level"' && echo "✓ 2. verify --no-code (level assessment)" || { echo "✗ 2. verify"; exit 1; }

# 3. Recipe log (verify iteration — backward compatible)
mkdir -p .bts/state/recipes/test-001
echo '{"id":"test-001","type":"blueprint","topic":"OAuth2","phase":"verify","iteration":1,"level":1.5,"started_at":"2026-03-18T00:00:00Z","updated_at":"2026-03-18T00:00:00Z"}' > .bts/state/recipes/test-001/recipe.json
$BTS recipe log test-001 --iteration 1 --critical 2 --major 1 > /dev/null
[ -f .bts/state/recipes/test-001/verify-log.jsonl ] && echo "✓ 3. recipe log (verify-log)" || { echo "✗ 3. verify-log"; exit 1; }

# 4. Recipe log (changelog action)
$BTS recipe log test-001 --action improve --output draft.md > /dev/null
[ -f .bts/state/recipes/test-001/changelog.jsonl ] && echo "✓ 4. recipe log (changelog)" || { echo "✗ 4. changelog"; exit 1; }

# 5. Recipe log (manifest update)
$BTS recipe log test-001 --action research --output research/v1.md --based-on "topic" > /dev/null
[ -f .bts/state/recipes/test-001/manifest.json ] && echo "✓ 5. recipe log (manifest)" || { echo "✗ 5. manifest"; exit 1; }

# 6. Recipe status (with Level)
$BTS recipe status | grep -q "Level" && echo "✓ 6. recipe status (Level shown)" || { echo "✗ 6. status"; exit 1; }

# 7. Debate log (create new)
$BTS debate log --topic "OAuth2 vs JWT" --round 1 --content "Expert 1: OAuth2 is standard-compliant" > /dev/null
$BTS debate list | grep -q "OAuth2 vs JWT" && echo "✓ 7. debate log + list" || { echo "✗ 7. debate"; exit 1; }

# 8. Debate resume
DEBATE_ID=$($BTS debate list 2>/dev/null | tail -1 | awk '{print $1}')
$BTS debate resume "$DEBATE_ID" | grep -q "Expert 1" && echo "✓ 8. debate resume" || { echo "✗ 8. resume"; exit 1; }

# 9. Debate log (add round 2)
$BTS debate log --id "$DEBATE_ID" --round 2 --content "Expert 2: JWT is stateless" > /dev/null
$BTS debate resume "$DEBATE_ID" | grep -q "Expert 2" && echo "✓ 9. debate round 2" || { echo "✗ 9. round 2"; exit 1; }

# 10. Sync check
$BTS sync-check test-001 2>&1 | grep -qE "sync|UNVERIFIED|issue" && echo "✓ 10. sync-check" || { echo "✗ 10. sync-check"; exit 1; }

# 11. Stop hook — should BLOCK (verify-log has critical>0)
RESULT=$(echo '{"session_id":"t","cwd":"'"$TEST_DIR"'","hook_event_name":"stop","content":"<bts>DONE</bts>"}' | $BTS hook stop 2>&1; echo "EXIT:$?")
echo "$RESULT" | grep -q "EXIT:2" && echo "✓ 11. stop hook blocks (critical>0)" || { echo "✗ 11. stop block"; exit 1; }

# 12. Add converged entry + verification.md, stop hook should ALLOW
echo '{"iteration":2,"critical":0,"major":0,"minor":1,"status":"converged","timestamp":"2026-03-18T00:01:00Z"}' >> .bts/state/recipes/test-001/verify-log.jsonl
echo "# Verification findings" > .bts/state/recipes/test-001/verification.md
RESULT=$(echo '{"session_id":"t","cwd":"'"$TEST_DIR"'","hook_event_name":"stop","content":"<bts>DONE</bts>"}' | $BTS hook stop 2>&1; echo "EXIT:$?")
echo "$RESULT" | grep -q "EXIT:0" && echo "✓ 12. stop hook allows (converged)" || { echo "✗ 12. stop allow"; exit 1; }

# --- IMPLEMENT DONE stop hook tests ---
# Setup: impl-001 recipe with all artifacts except review.md
mkdir -p .bts/state/recipes/impl-001
echo '{"id":"impl-001","type":"blueprint","topic":"Auth","phase":"status","iteration":1,"level":3.0,"started_at":"2026-03-18T00:00:00Z","updated_at":"2026-03-18T00:00:00Z"}' > .bts/state/recipes/impl-001/recipe.json
echo '{"recipe_id":"impl-001","started_at":"2026-03-18T00:00:00Z","updated_at":"2026-03-18T00:00:00Z","tasks":[{"id":"t-001","file":"src/auth.ts","action":"create","status":"done","description":"auth","depends_on":[],"retry_count":0,"last_error":""}]}' > .bts/state/recipes/impl-001/tasks.json
echo '{"recipe_id":"impl-001","run_at":"2026-03-18T00:00:00Z","framework":"jest","iterations":1,"status":"pass","total":5,"passed":5,"failed":0,"skipped":0}' > .bts/state/recipes/impl-001/test-results.json
echo "# Deviation Report" > .bts/state/recipes/impl-001/deviation.md

# 13. IMPLEMENT DONE — BLOCK (no review.md)
RESULT=$(echo '{"session_id":"t","cwd":"'"$TEST_DIR"'","hook_event_name":"stop","content":"<bts>IMPLEMENT DONE</bts>"}' | $BTS hook stop 2>&1; echo "EXIT:$?")
echo "$RESULT" | grep -q "EXIT:2" && echo "✓ 13. IMPLEMENT DONE blocks (no review.md)" || { echo "✗ 13. IMPL DONE no review"; exit 1; }

# 14. IMPLEMENT DONE — ALLOW (add review.md)
echo "# Code Review" > .bts/state/recipes/impl-001/review.md
RESULT=$(echo '{"session_id":"t","cwd":"'"$TEST_DIR"'","hook_event_name":"stop","content":"<bts>IMPLEMENT DONE</bts>"}' | $BTS hook stop 2>&1; echo "EXIT:$?")
echo "$RESULT" | grep -q "EXIT:0" && echo "✓ 14. IMPLEMENT DONE allows (all present)" || { echo "✗ 14. IMPL DONE allow"; exit 1; }

# --- FIX DONE stop hook tests ---
# Setup: fix-001 recipe
mkdir -p .bts/state/recipes/fix-001
echo '{"id":"fix-001","type":"fix","topic":"Login bug","phase":"test","iteration":1,"level":0,"started_at":"2026-03-18T00:00:00Z","updated_at":"2026-03-18T00:00:00Z","ref_recipe":"test-001"}' > .bts/state/recipes/fix-001/recipe.json

# 15. FIX DONE — BLOCK (no fix-spec.md)
echo '{"recipe_id":"fix-001","run_at":"2026-03-18T00:00:00Z","framework":"jest","iterations":1,"status":"pass","total":3,"passed":3,"failed":0,"skipped":0}' > .bts/state/recipes/fix-001/test-results.json
RESULT=$(echo '{"session_id":"t","cwd":"'"$TEST_DIR"'","hook_event_name":"stop","content":"<bts>FIX DONE</bts>"}' | $BTS hook stop 2>&1; echo "EXIT:$?")
echo "$RESULT" | grep -q "EXIT:2" && echo "✓ 15. FIX DONE blocks (no fix-spec.md)" || { echo "✗ 15. FIX DONE no spec"; exit 1; }

# 16. FIX DONE — ALLOW (add fix-spec.md)
echo "# Fix Spec" > .bts/state/recipes/fix-001/fix-spec.md
RESULT=$(echo '{"session_id":"t","cwd":"'"$TEST_DIR"'","hook_event_name":"stop","content":"<bts>FIX DONE</bts>"}' | $BTS hook stop 2>&1; echo "EXIT:$?")
echo "$RESULT" | grep -q "EXIT:0" && echo "✓ 16. FIX DONE allows (spec + tests pass)" || { echo "✗ 16. FIX DONE allow"; exit 1; }

# --- Session-start hook tests ---
# Cancel completed recipes so they don't interfere
echo '{"id":"impl-001","type":"blueprint","topic":"Auth","phase":"complete","iteration":1,"level":3.0,"started_at":"2026-03-18T00:00:00Z","updated_at":"2026-03-18T00:00:00Z"}' > .bts/state/recipes/impl-001/recipe.json
echo '{"id":"fix-001","type":"fix","topic":"Login bug","phase":"complete","iteration":1,"level":0,"started_at":"2026-03-18T00:00:00Z","updated_at":"2026-03-18T00:00:00Z"}' > .bts/state/recipes/fix-001/recipe.json

# 17. Session-start — review phase → /bts-implement hint
mkdir -p .bts/state/recipes/ss-001
echo '{"id":"ss-001","type":"blueprint","topic":"API","phase":"review","iteration":1,"level":3.0,"started_at":"2026-03-18T00:00:00Z","updated_at":"2026-03-18T00:00:00Z"}' > .bts/state/recipes/ss-001/recipe.json
RESULT=$(echo '{"session_id":"t","cwd":"'"$TEST_DIR"'","hook_event_name":"session_start"}' | $BTS hook session-start 2>&1)
echo "$RESULT" | grep -q "bts-implement" && echo "✓ 17. session-start (review phase → /bts-implement)" || { echo "✗ 17. session-start review"; exit 1; }

# 18. Session-start — finalized recipe → /bts-implement hint
echo '{"id":"ss-001","type":"blueprint","topic":"API","phase":"finalize","iteration":1,"level":3.0,"started_at":"2026-03-18T00:00:00Z","updated_at":"2026-03-18T00:00:00Z"}' > .bts/state/recipes/ss-001/recipe.json
RESULT=$(echo '{"session_id":"t","cwd":"'"$TEST_DIR"'","hook_event_name":"session_start"}' | $BTS hook session-start 2>&1)
echo "$RESULT" | grep -q "bts-implement" && echo "✓ 18. session-start (finalized → /bts-implement)" || { echo "✗ 18. session-start finalize"; exit 1; }

# --- Validation tests ---
# 19. Validate — review phase accepted
echo '{"id":"ss-001","type":"blueprint","topic":"API","phase":"review","iteration":1,"level":3.0,"started_at":"2026-03-18T00:00:00Z","updated_at":"2026-03-18T00:00:00Z"}' > .bts/state/recipes/ss-001/recipe.json
$BTS validate 2>&1 | grep -qv "invalid.*phase" && echo "✓ 19. validate accepts phase=review" || { echo "✗ 19. validate review phase"; exit 1; }

# 20. Validate — review document type accepted
echo '{"current_draft":"draft.md","level":2.0,"documents":{"review.md":{"type":"review","created_at":"2026-03-18T00:00:00Z"}}}' > .bts/state/recipes/ss-001/manifest.json
$BTS validate 2>&1 | grep -qv "invalid.*type" && echo "✓ 20. validate accepts type=review" || { echo "✗ 20. validate review type"; exit 1; }

# --- Vision/Roadmap tests ---
# Cancel all active recipes so roadmap hint appears
echo '{"id":"test-001","type":"blueprint","topic":"OAuth2","phase":"cancelled","iteration":1,"level":1.5,"started_at":"2026-03-18T00:00:00Z","updated_at":"2026-03-18T00:00:00Z"}' > .bts/state/recipes/test-001/recipe.json
echo '{"id":"ss-001","type":"blueprint","topic":"API","phase":"cancelled","iteration":1,"level":3.0,"started_at":"2026-03-18T00:00:00Z","updated_at":"2026-03-18T00:00:00Z"}' > .bts/state/recipes/ss-001/recipe.json

# 21. Session-start — roadmap exists, no active recipe → roadmap hint
printf '# Roadmap\n\nStatus: CONFIRMED\nProgress: 1/3\n\n## Items\n\n- [x] Core models (recipe: test-001)\n- [ ] API endpoints (recipe: rm-001)\n- [ ] UI components\n' > .bts/state/roadmap.md
RESULT=$(echo '{"session_id":"t","cwd":"'"$TEST_DIR"'","hook_event_name":"session_start"}' | $BTS hook session-start 2>&1)
echo "$RESULT" | grep -q "Roadmap" && echo "✓ 21. session-start roadmap hint" || { echo "✗ 21. roadmap hint"; exit 1; }

# 22. Session-start — roadmap shows next item
echo "$RESULT" | grep -q "API endpoints" && echo "✓ 22. roadmap next item shown" || { echo "✗ 22. roadmap next item"; exit 1; }

# 23. IMPLEMENT DONE with roadmap → completion shows roadmap progress
mkdir -p .bts/state/recipes/rm-001
echo '{"id":"rm-001","type":"blueprint","topic":"API endpoints","phase":"status","iteration":1,"level":3.0,"started_at":"2026-03-18T00:00:00Z","updated_at":"2026-03-18T00:00:00Z"}' > .bts/state/recipes/rm-001/recipe.json
echo '{"recipe_id":"rm-001","started_at":"2026-03-18T00:00:00Z","updated_at":"2026-03-18T00:00:00Z","tasks":[{"id":"t-001","file":"src/api.ts","action":"create","status":"done","description":"api","depends_on":[],"retry_count":0,"last_error":""}]}' > .bts/state/recipes/rm-001/tasks.json
echo '{"recipe_id":"rm-001","run_at":"2026-03-18T00:00:00Z","framework":"jest","iterations":1,"status":"pass","total":3,"passed":3,"failed":0,"skipped":0}' > .bts/state/recipes/rm-001/test-results.json
echo "# Code Review" > .bts/state/recipes/rm-001/review.md
echo "# Deviation Report" > .bts/state/recipes/rm-001/deviation.md
RESULT=$(echo '{"session_id":"t","cwd":"'"$TEST_DIR"'","hook_event_name":"stop","content":"<bts>IMPLEMENT DONE</bts>"}' | $BTS hook stop 2>&1; echo "EXIT:$?")
echo "$RESULT" | grep -q "EXIT:0" && echo "$RESULT" | grep -q "Roadmap" && echo "✓ 23. IMPLEMENT DONE roadmap hint" || { echo "✗ 23. impl done roadmap"; exit 1; }

# 24. IMPLEMENT DONE marks roadmap item [x]
grep -q '\[x\] API endpoints' .bts/state/roadmap.md && echo "✓ 24. roadmap item marked done" || { echo "✗ 24. roadmap mark done"; exit 1; }

# 25. Roadmap nextItem is now "UI components" (not the completed one)
echo "$RESULT" | grep -q "UI components" && echo "✓ 25. roadmap next item updated" || { echo "✗ 25. next item after complete"; exit 1; }

# 26. Session-start — vision DRAFT hint (no active recipe)
printf '# Vision\n\nStatus: DRAFT\n' > .bts/state/vision.md
rm .bts/state/roadmap.md
RESULT=$(echo '{"session_id":"t","cwd":"'"$TEST_DIR"'","hook_event_name":"session_start"}' | $BTS hook session-start 2>&1)
echo "$RESULT" | grep -q "Vision" && echo "✓ 26. session-start vision DRAFT hint" || { echo "✗ 26. vision DRAFT hint"; exit 1; }

# 27. Session-start — roadmap all done → "complete" hint
rm -f .bts/state/vision.md
printf '# Roadmap\n\nStatus: CONFIRMED\nProgress: 2/2\n\n## Items\n\n- [x] Core models (recipe: test-001)\n- [x] API endpoints (recipe: rm-001)\n' > .bts/state/roadmap.md
RESULT=$(echo '{"session_id":"t","cwd":"'"$TEST_DIR"'","hook_event_name":"session_start"}' | $BTS hook session-start 2>&1)
echo "$RESULT" | grep -q "complete" && echo "✓ 27. session-start roadmap complete hint" || { echo "✗ 27. roadmap complete"; exit 1; }

# --- PreToolUse tests ---
# 28. PreToolUse — spec phase에서 소스코드 Write 경고
mkdir -p .bts/state/recipes/ptu-001
echo '{"id":"ptu-001","type":"blueprint","topic":"Test","phase":"draft","iteration":1,"level":1.0,"started_at":"2026-03-18T00:00:00Z","updated_at":"2026-03-18T00:00:00Z"}' > .bts/state/recipes/ptu-001/recipe.json
RESULT=$(echo '{"session_id":"t","cwd":"'"$TEST_DIR"'","hook_event_name":"pre-tool-use","tool_name":"Write","tool_input":{"file_path":"src/app.ts","content":"code"}}' | $BTS hook pre-tool-use 2>&1)
echo "$RESULT" | grep -q "spec phase" && echo "✓ 28. PreToolUse warns during spec phase" || { echo "✗ 28. PreToolUse"; exit 1; }

# 29. PreToolUse — .bts/ 파일은 허용
RESULT=$(echo '{"session_id":"t","cwd":"'"$TEST_DIR"'","hook_event_name":"pre-tool-use","tool_name":"Write","tool_input":{"file_path":".bts/state/recipes/ptu-001/draft.md","content":"spec"}}' | $BTS hook pre-tool-use 2>&1)
echo "$RESULT" | grep -qv "spec phase" && echo "✓ 29. PreToolUse allows .bts/ writes" || { echo "✗ 29. PreToolUse bts"; exit 1; }

# 30. PreToolUse — implement phase에서는 허용
echo '{"id":"ptu-001","type":"blueprint","topic":"Test","phase":"implement","iteration":1,"level":3.0,"started_at":"2026-03-18T00:00:00Z","updated_at":"2026-03-18T00:00:00Z"}' > .bts/state/recipes/ptu-001/recipe.json
RESULT=$(echo '{"session_id":"t","cwd":"'"$TEST_DIR"'","hook_event_name":"pre-tool-use","tool_name":"Write","tool_input":{"file_path":"src/app.ts","content":"code"}}' | $BTS hook pre-tool-use 2>&1)
echo "$RESULT" | grep -qv "spec phase" && echo "✓ 30. PreToolUse allows during implement" || { echo "✗ 30. PreToolUse impl"; exit 1; }

echo ""
echo "=== All 30 tests passed ==="
