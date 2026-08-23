#!/bin/bash
# test-e2e.sh — jig integration test
set -e

JIG="$(cd "$(dirname "$0")/.." && pwd)/bin/jig"
TEST_DIR=$(mktemp -d)
trap 'rm -rf "$TEST_DIR"' EXIT
cd "$TEST_DIR"

echo "=== jig E2E Test ==="
echo "Binary: $JIG"
echo "Test dir: $TEST_DIR"
echo ""

# 1. Init
$JIG init . > /dev/null
[ -f .claude/skills/jig-verify/SKILL.md ] && echo "✓ 1. init" || { echo "✗ 1. init"; exit 1; }

# 2. Verify (no code, from-scratch spec)
printf "# OAuth2 Design\n\n**Auth component** handles user login.\n**Session manager** stores tokens.\nUses **Express** framework with **Passport.js**.\nData flows from **login form** to **OAuth provider** to **callback handler**.\nOn error, returns **401 Unauthorized**.\n" > spec.md
$JIG verify --no-code spec.md | grep -q '"level"' && echo "✓ 2. verify --no-code (level assessment)" || { echo "✗ 2. verify"; exit 1; }

# 3. Recipe log (verify iteration — backward compatible)
mkdir -p .jig/specs/recipes/test-001
echo '{"id":"test-001","type":"spec","topic":"OAuth2","phase":"verify","iteration":1,"level":1.5,"started_at":"2026-03-18T00:00:00Z","updated_at":"2026-03-18T00:00:00Z"}' > .jig/specs/recipes/test-001/recipe.json
$JIG recipe log test-001 --iteration 1 --critical 2 --major 1 > /dev/null
[ -f .jig/specs/recipes/test-001/verify-log.jsonl ] && echo "✓ 3. recipe log (verify-log)" || { echo "✗ 3. verify-log"; exit 1; }

# 4. Recipe log (changelog action)
$JIG recipe log test-001 --action improve --output draft.md > /dev/null
[ -f .jig/specs/recipes/test-001/changelog.jsonl ] && echo "✓ 4. recipe log (changelog)" || { echo "✗ 4. changelog"; exit 1; }

# 5. Recipe log (manifest update)
$JIG recipe log test-001 --action research --output research/v1.md --based-on "topic" > /dev/null
[ -f .jig/specs/recipes/test-001/manifest.json ] && echo "✓ 5. recipe log (manifest)" || { echo "✗ 5. manifest"; exit 1; }

# 6. Recipe status (with Level)
$JIG recipe status | grep -q "Level" && echo "✓ 6. recipe status (Level shown)" || { echo "✗ 6. status"; exit 1; }

# 7. Debate log (create new)
$JIG debate log --topic "OAuth2 vs JWT" --round 1 --content "Expert 1: OAuth2 is standard-compliant" > /dev/null
$JIG debate list | grep -q "OAuth2 vs JWT" && echo "✓ 7. debate log + list" || { echo "✗ 7. debate"; exit 1; }

# 8. Debate resume
DEBATE_ID=$($JIG debate list 2>/dev/null | tail -1 | awk '{print $1}')
$JIG debate resume "$DEBATE_ID" | grep -q "Expert 1" && echo "✓ 8. debate resume" || { echo "✗ 8. resume"; exit 1; }

# 9. Debate log (add round 2)
$JIG debate log --id "$DEBATE_ID" --round 2 --content "Expert 2: JWT is stateless" > /dev/null
$JIG debate resume "$DEBATE_ID" | grep -q "Expert 2" && echo "✓ 9. debate round 2" || { echo "✗ 9. round 2"; exit 1; }

# 10. Sync check
$JIG sync-check test-001 2>&1 | grep -qE "sync|UNVERIFIED|issue" && echo "✓ 10. sync-check" || { echo "✗ 10. sync-check"; exit 1; }

# 11. Stop hook — should BLOCK (verify-log has critical>0)
RESULT=$(echo '{"session_id":"t","cwd":"'"$TEST_DIR"'","hook_event_name":"stop","content":"<jig>DONE</jig>"}' | $JIG hook stop 2>&1; echo "EXIT:$?")
echo "$RESULT" | grep -q "EXIT:2" && echo "✓ 11. stop hook blocks (critical>0)" || { echo "✗ 11. stop block"; exit 1; }

# 12. Satisfy every DONE gate, stop hook should ALLOW.
# Gates (hardened in v0.7.x): converged verify-log with zero blocking
# findings, a simulate action in the changelog, a verified current
# draft with resolved simulation gaps, and a passing sync-check logged
# after the last draft modification.
$JIG recipe log test-001 --action simulate --output simulations/sim-001.md --gaps 0 --result "5 scenarios, 0 gaps" > /dev/null
$JIG recipe log test-001 --iteration 2 --critical 0 --major 0 > /dev/null
echo "# Verification findings" > .jig/specs/recipes/test-001/verification.md
# The orchestrator normally records these manifest fields per
# jig-document-management; the fixture patches them directly.
python3 - <<'PYEOF'
import json
p = ".jig/specs/recipes/test-001/manifest.json"
m = json.load(open(p))
d = m["documents"]["draft.md"]
d["verified_by"] = "verification.md"
d["resolves"] = ["simulations/sim-001.md"]
json.dump(m, open(p, "w"), indent=2)
PYEOF
$JIG sync-check test-001 > /dev/null 2>&1 && echo "✓ 12a. sync-check passes" || { echo "✗ 12a. sync-check pass"; exit 1; }
RESULT=$(echo '{"session_id":"t","cwd":"'"$TEST_DIR"'","hook_event_name":"stop","content":"<jig>DONE</jig>"}' | $JIG hook stop 2>&1; echo "EXIT:$?")
echo "$RESULT" | grep -q "EXIT:0" && echo "✓ 12. stop hook allows (converged)" || { echo "✗ 12. stop allow"; exit 1; }

# --- IMPLEMENT DONE stop hook tests ---
mkdir -p .jig/specs/recipes/impl-001
echo '{"id":"impl-001","type":"spec","topic":"Auth","phase":"status","iteration":1,"level":3.0,"started_at":"2026-03-18T00:00:00Z","updated_at":"2026-03-18T00:00:00Z"}' > .jig/specs/recipes/impl-001/recipe.json
echo '{"recipe_id":"impl-001","started_at":"2026-03-18T00:00:00Z","updated_at":"2026-03-18T00:00:00Z","tasks":[{"id":"t-001","file":"src/auth.ts","action":"create","status":"done","description":"auth","depends_on":[],"retry_count":0,"last_error":""}]}' > .jig/specs/recipes/impl-001/tasks.json
echo '{"recipe_id":"impl-001","run_at":"2026-03-18T00:00:00Z","framework":"jest","iterations":1,"status":"pass","total":5,"passed":5,"failed":0,"skipped":0}' > .jig/specs/recipes/impl-001/test-results.json
echo "# Deviation Report" > .jig/specs/recipes/impl-001/deviation.md

# 13. IMPLEMENT DONE — BLOCK (no review.md)
RESULT=$(echo '{"session_id":"t","cwd":"'"$TEST_DIR"'","hook_event_name":"stop","content":"<jig>IMPLEMENT DONE</jig>"}' | $JIG hook stop 2>&1; echo "EXIT:$?")
echo "$RESULT" | grep -q "EXIT:2" && echo "✓ 13. IMPLEMENT DONE blocks (no review.md)" || { echo "✗ 13. IMPL DONE no review"; exit 1; }

# 14. IMPLEMENT DONE — ALLOW (add review.md)
echo "# Code Review" > .jig/specs/recipes/impl-001/review.md
RESULT=$(echo '{"session_id":"t","cwd":"'"$TEST_DIR"'","hook_event_name":"stop","content":"<jig>IMPLEMENT DONE</jig>"}' | $JIG hook stop 2>&1; echo "EXIT:$?")
echo "$RESULT" | grep -q "EXIT:0" && echo "✓ 14. IMPLEMENT DONE allows (all present)" || { echo "✗ 14. IMPL DONE allow"; exit 1; }

# --- FIX DONE stop hook tests ---
mkdir -p .jig/specs/recipes/fix-001
echo '{"id":"fix-001","type":"fix","topic":"Login bug","phase":"test","iteration":1,"level":0,"started_at":"2026-03-18T00:00:00Z","updated_at":"2026-03-18T00:00:00Z","ref_recipe":"test-001"}' > .jig/specs/recipes/fix-001/recipe.json

# 15. FIX DONE — BLOCK (no fix-spec.md)
echo '{"recipe_id":"fix-001","run_at":"2026-03-18T00:00:00Z","framework":"jest","iterations":1,"status":"pass","total":3,"passed":3,"failed":0,"skipped":0}' > .jig/specs/recipes/fix-001/test-results.json
RESULT=$(echo '{"session_id":"t","cwd":"'"$TEST_DIR"'","hook_event_name":"stop","content":"<jig>FIX DONE</jig>"}' | $JIG hook stop 2>&1; echo "EXIT:$?")
echo "$RESULT" | grep -q "EXIT:2" && echo "✓ 15. FIX DONE blocks (no fix-spec.md)" || { echo "✗ 15. FIX DONE no spec"; exit 1; }

# 16. FIX DONE — ALLOW (add fix-spec.md)
echo "# Fix Spec" > .jig/specs/recipes/fix-001/fix-spec.md
RESULT=$(echo '{"session_id":"t","cwd":"'"$TEST_DIR"'","hook_event_name":"stop","content":"<jig>FIX DONE</jig>"}' | $JIG hook stop 2>&1; echo "EXIT:$?")
echo "$RESULT" | grep -q "EXIT:0" && echo "✓ 16. FIX DONE allows (spec + tests pass)" || { echo "✗ 16. FIX DONE allow"; exit 1; }

# --- Session-start hook tests ---
echo '{"id":"impl-001","type":"spec","topic":"Auth","phase":"complete","iteration":1,"level":3.0,"started_at":"2026-03-18T00:00:00Z","updated_at":"2026-03-18T00:00:00Z"}' > .jig/specs/recipes/impl-001/recipe.json
echo '{"id":"fix-001","type":"fix","topic":"Login bug","phase":"complete","iteration":1,"level":0,"started_at":"2026-03-18T00:00:00Z","updated_at":"2026-03-18T00:00:00Z"}' > .jig/specs/recipes/fix-001/recipe.json

# 17. Session-start — review phase → /jig-implement hint
mkdir -p .jig/specs/recipes/ss-001
echo '{"id":"ss-001","type":"spec","topic":"API","phase":"review","iteration":1,"level":3.0,"started_at":"2026-03-18T00:00:00Z","updated_at":"2026-03-18T00:00:00Z"}' > .jig/specs/recipes/ss-001/recipe.json
RESULT=$(echo '{"session_id":"t","cwd":"'"$TEST_DIR"'","hook_event_name":"session_start"}' | $JIG hook session-start 2>&1)
echo "$RESULT" | grep -q "jig-implement" && echo "✓ 17. session-start (review phase → /jig-implement)" || { echo "✗ 17. session-start review"; exit 1; }

# 18. Session-start — finalized recipe → /jig-implement hint
echo '{"id":"ss-001","type":"spec","topic":"API","phase":"finalize","iteration":1,"level":3.0,"started_at":"2026-03-18T00:00:00Z","updated_at":"2026-03-18T00:00:00Z"}' > .jig/specs/recipes/ss-001/recipe.json
RESULT=$(echo '{"session_id":"t","cwd":"'"$TEST_DIR"'","hook_event_name":"session_start"}' | $JIG hook session-start 2>&1)
echo "$RESULT" | grep -q "jig-implement" && echo "✓ 18. session-start (finalized → /jig-implement)" || { echo "✗ 18. session-start finalize"; exit 1; }

# --- Validation tests ---
# 19. Validate — review phase accepted
echo '{"id":"ss-001","type":"spec","topic":"API","phase":"review","iteration":1,"level":3.0,"started_at":"2026-03-18T00:00:00Z","updated_at":"2026-03-18T00:00:00Z"}' > .jig/specs/recipes/ss-001/recipe.json
$JIG validate 2>&1 | grep -qv "invalid.*phase" && echo "✓ 19. validate accepts phase=review" || { echo "✗ 19. validate review phase"; exit 1; }

# 20. Validate — review document type accepted
echo '{"current_draft":"draft.md","level":2.0,"documents":{"review.md":{"type":"review","created_at":"2026-03-18T00:00:00Z"}}}' > .jig/specs/recipes/ss-001/manifest.json
$JIG validate 2>&1 | grep -qv "invalid.*type" && echo "✓ 20. validate accepts type=review" || { echo "✗ 20. validate review type"; exit 1; }

# --- Vision/Roadmap tests ---
echo '{"id":"test-001","type":"spec","topic":"OAuth2","phase":"cancelled","iteration":1,"level":1.5,"started_at":"2026-03-18T00:00:00Z","updated_at":"2026-03-18T00:00:00Z"}' > .jig/specs/recipes/test-001/recipe.json
echo '{"id":"ss-001","type":"spec","topic":"API","phase":"cancelled","iteration":1,"level":3.0,"started_at":"2026-03-18T00:00:00Z","updated_at":"2026-03-18T00:00:00Z"}' > .jig/specs/recipes/ss-001/recipe.json

# 21. Session-start — roadmap exists, no active recipe → roadmap hint
printf '# Roadmap\n\nStatus: CONFIRMED\nProgress: 1/3\n\n## Items\n\n- [x] Core models (recipe: test-001)\n- [ ] API endpoints (recipe: rm-001)\n- [ ] UI components\n' > .jig/specs/roadmap.md
RESULT=$(echo '{"session_id":"t","cwd":"'"$TEST_DIR"'","hook_event_name":"session_start"}' | $JIG hook session-start 2>&1)
echo "$RESULT" | grep -q "Roadmap" && echo "✓ 21. session-start roadmap hint" || { echo "✗ 21. roadmap hint"; exit 1; }

# 22. Session-start — roadmap shows next item
echo "$RESULT" | grep -q "API endpoints" && echo "✓ 22. roadmap next item shown" || { echo "✗ 22. roadmap next item"; exit 1; }

# 23. IMPLEMENT DONE with roadmap → completion shows roadmap progress
mkdir -p .jig/specs/recipes/rm-001
echo '{"id":"rm-001","type":"spec","topic":"API endpoints","phase":"status","iteration":1,"level":3.0,"started_at":"2026-03-18T00:00:00Z","updated_at":"2026-03-18T00:00:00Z"}' > .jig/specs/recipes/rm-001/recipe.json
echo '{"recipe_id":"rm-001","started_at":"2026-03-18T00:00:00Z","updated_at":"2026-03-18T00:00:00Z","tasks":[{"id":"t-001","file":"src/api.ts","action":"create","status":"done","description":"api","depends_on":[],"retry_count":0,"last_error":""}]}' > .jig/specs/recipes/rm-001/tasks.json
echo '{"recipe_id":"rm-001","run_at":"2026-03-18T00:00:00Z","framework":"jest","iterations":1,"status":"pass","total":3,"passed":3,"failed":0,"skipped":0}' > .jig/specs/recipes/rm-001/test-results.json
echo "# Code Review" > .jig/specs/recipes/rm-001/review.md
echo "# Deviation Report" > .jig/specs/recipes/rm-001/deviation.md
RESULT=$(echo '{"session_id":"t","cwd":"'"$TEST_DIR"'","hook_event_name":"stop","content":"<jig>IMPLEMENT DONE</jig>"}' | $JIG hook stop 2>&1; echo "EXIT:$?")
echo "$RESULT" | grep -q "EXIT:0" && echo "$RESULT" | grep -q "Roadmap" && echo "✓ 23. IMPLEMENT DONE roadmap hint" || { echo "✗ 23. impl done roadmap"; exit 1; }

# 24. IMPLEMENT DONE marks roadmap item [x]
grep -q '\[x\] API endpoints' .jig/specs/roadmap.md && echo "✓ 24. roadmap item marked done" || { echo "✗ 24. roadmap mark done"; exit 1; }

# 25. Roadmap nextItem is now "UI components" (not the completed one)
echo "$RESULT" | grep -q "UI components" && echo "✓ 25. roadmap next item updated" || { echo "✗ 25. next item after complete"; exit 1; }

# 26. Session-start — vision DRAFT hint (no active recipe)
printf '# Vision\n\nStatus: DRAFT\n' > .jig/specs/vision.md
rm .jig/specs/roadmap.md
RESULT=$(echo '{"session_id":"t","cwd":"'"$TEST_DIR"'","hook_event_name":"session_start"}' | $JIG hook session-start 2>&1)
echo "$RESULT" | grep -q "Vision" && echo "✓ 26. session-start vision DRAFT hint" || { echo "✗ 26. vision DRAFT hint"; exit 1; }

# 27. Session-start — roadmap all done → "complete" hint
rm -f .jig/specs/vision.md
printf '# Roadmap\n\nStatus: CONFIRMED\nProgress: 2/2\n\n## Items\n\n- [x] Core models (recipe: test-001)\n- [x] API endpoints (recipe: rm-001)\n' > .jig/specs/roadmap.md
RESULT=$(echo '{"session_id":"t","cwd":"'"$TEST_DIR"'","hook_event_name":"session_start"}' | $JIG hook session-start 2>&1)
echo "$RESULT" | grep -q "complete" && echo "✓ 27. session-start roadmap complete hint" || { echo "✗ 27. roadmap complete"; exit 1; }

# --- PreToolUse tests ---
# 28. PreToolUse — spec phase에서 소스코드 Write 경고
mkdir -p .jig/specs/recipes/ptu-001
echo '{"id":"ptu-001","type":"spec","topic":"Test","phase":"draft","iteration":1,"level":1.0,"started_at":"2026-03-18T00:00:00Z","updated_at":"2026-03-18T00:00:00Z"}' > .jig/specs/recipes/ptu-001/recipe.json
RESULT=$(echo '{"session_id":"t","cwd":"'"$TEST_DIR"'","hook_event_name":"pre-tool-use","tool_name":"Write","tool_input":{"file_path":"src/app.ts","content":"code"}}' | $JIG hook pre-tool-use 2>&1)
echo "$RESULT" | grep -q "spec phase" && echo "✓ 28. PreToolUse warns during spec phase" || { echo "✗ 28. PreToolUse"; exit 1; }

# 29. PreToolUse — .jig/ 파일은 허용
RESULT=$(echo '{"session_id":"t","cwd":"'"$TEST_DIR"'","hook_event_name":"pre-tool-use","tool_name":"Write","tool_input":{"file_path":".jig/specs/recipes/ptu-001/draft.md","content":"spec"}}' | $JIG hook pre-tool-use 2>&1)
echo "$RESULT" | grep -qv "spec phase" && echo "✓ 29. PreToolUse allows .jig/ writes" || { echo "✗ 29. PreToolUse jig"; exit 1; }

# 30. PreToolUse — implement phase에서는 허용
echo '{"id":"ptu-001","type":"spec","topic":"Test","phase":"implement","iteration":1,"level":3.0,"started_at":"2026-03-18T00:00:00Z","updated_at":"2026-03-18T00:00:00Z"}' > .jig/specs/recipes/ptu-001/recipe.json
RESULT=$(echo '{"session_id":"t","cwd":"'"$TEST_DIR"'","hook_event_name":"pre-tool-use","tool_name":"Write","tool_input":{"file_path":"src/app.ts","content":"code"}}' | $JIG hook pre-tool-use 2>&1)
echo "$RESULT" | grep -qv "spec phase" && echo "✓ 30. PreToolUse allows during implement" || { echo "✗ 30. PreToolUse impl"; exit 1; }

# --- Discovery phase test ---
# 31. Validate — discovery phase accepted
mkdir -p .jig/specs/recipes/disc-001
echo '{"id":"disc-001","type":"spec","topic":"Test","phase":"discovery","iteration":0,"level":0,"started_at":"2026-03-18T00:00:00Z","updated_at":"2026-03-18T00:00:00Z"}' > .jig/specs/recipes/disc-001/recipe.json
$JIG validate 2>&1 | grep -qv "invalid.*phase" && echo "✓ 31. validate accepts phase=discovery" || { echo "✗ 31. discovery phase"; exit 1; }

# --- Verify support commands (graph paths + verify-focus) ---
# 32. graph paths — deterministic mermaid enumeration for /jig-verify
mkdir -p .jig/specs/recipes/gp-001
printf '# Doc\n\n```mermaid\nstateDiagram-v2\n[*] --> A\nA --> B : ok\nA --> C : fail\nB --> [*]\n```\n' > .jig/specs/recipes/gp-001/draft.md
$JIG graph paths .jig/specs/recipes/gp-001/draft.md | grep -q "paths_total: 2" && echo "✓ 32. graph paths enumerates" || { echo "✗ 32. graph paths"; exit 1; }

# 33. verify-focus — first verify has no snapshot; after log --doc, diff shows edits
echo '{"id":"gp-001","type":"spec","topic":"GP","phase":"verify","iteration":1,"level":2.0,"started_at":"2026-03-18T00:00:00Z","updated_at":"2026-03-18T00:00:00Z"}' > .jig/specs/recipes/gp-001/recipe.json
$JIG recipe verify-focus .jig/specs/recipes/gp-001/draft.md | grep -q "FIRST VERIFICATION" || { echo "✗ 33. verify-focus first"; exit 1; }
$JIG recipe log gp-001 --iteration 1 --critical 0 --major 0 --doc .jig/specs/recipes/gp-001/draft.md > /dev/null
echo "New line for focus" >> .jig/specs/recipes/gp-001/draft.md
$JIG recipe verify-focus .jig/specs/recipes/gp-001/draft.md | grep -q "+ New line for focus" && echo "✓ 33. verify-focus snapshot diff" || { echo "✗ 33. verify-focus diff"; exit 1; }

# --- Rule 3 dirty-doc gate + machine-truth test run + outcomes/doctor ---
# Park earlier still-active fixtures so ad-001 is the single active recipe
# (stop gates fire only for the active recipe).
for rid in disc-001 ptu-001 gp-001; do
  python3 -c "
import json
p = '.jig/specs/recipes/$rid/recipe.json'
d = json.load(open(p)); d['phase'] = 'complete'
json.dump(d, open(p, 'w'))
"
done

# 34. Dirty-doc gate — converged + snapshotted spec allows DONE
mkdir -p .jig/specs/recipes/ad-001
echo '{"id":"ad-001","type":"spec","topic":"Dirty gate","phase":"verify","iteration":1,"level":2.5,"started_at":"2026-03-18T00:00:00Z","updated_at":"2026-03-18T00:00:00Z"}' > .jig/specs/recipes/ad-001/recipe.json
printf '# Draft\n\nVerified content v1.\n' > .jig/specs/recipes/ad-001/draft.md
echo "# Verification findings" > .jig/specs/recipes/ad-001/verification.md
$JIG recipe log ad-001 --action improve --output draft.md > /dev/null
$JIG recipe log ad-001 --action simulate --output simulations/sim-001.md --gaps 0 --result "5 scenarios, 0 gaps" > /dev/null
$JIG recipe log ad-001 --iteration 1 --critical 0 --major 0 --doc .jig/specs/recipes/ad-001/draft.md --scope full --dimension verify --dimension audit --dimension simulate > /dev/null
# Completion needs two independent clean rounds on the same revision, each
# with its own verification.md — re-recording one file does not raise the count.
echo "# Verification findings (round 2)" > .jig/specs/recipes/ad-001/verification.md
$JIG recipe log ad-001 --iteration 2 --critical 0 --major 0 --doc .jig/specs/recipes/ad-001/draft.md --scope full --dimension verify --dimension audit --dimension simulate > /dev/null
python3 -c "
import json
p = '.jig/specs/recipes/ad-001/manifest.json'
m = json.load(open(p))
d = m['documents']['draft.md']
d['verified_by'] = 'verification.md'
d['resolves'] = ['simulations/sim-001.md']
json.dump(m, open(p, 'w'))
"
$JIG sync-check ad-001 > /dev/null 2>&1 || { echo "✗ 34. sync-check pass"; exit 1; }
RESULT=$(echo '{"session_id":"t","cwd":"'"$TEST_DIR"'","hook_event_name":"stop","content":"<jig>DONE</jig>"}' | $JIG hook stop 2>&1; echo "EXIT:$?")
echo "$RESULT" | grep -q "EXIT:0" && echo "✓ 34. DONE allows with clean snapshot" || { echo "✗ 34. clean snapshot allow: $RESULT"; exit 1; }

# 35. Modifying the doc AFTER verification blocks DONE (changelog gates
# cannot see raw file edits — the snapshot gate must catch it).
python3 -c "
import json
p = '.jig/specs/recipes/ad-001/recipe.json'
d = json.load(open(p)); d['phase'] = 'verify'
json.dump(d, open(p, 'w'))
"
echo "sneaky post-verify edit" >> .jig/specs/recipes/ad-001/draft.md
RESULT=$(echo '{"session_id":"t","cwd":"'"$TEST_DIR"'","hook_event_name":"stop","content":"<jig>DONE</jig>"}' | $JIG hook stop 2>&1; echo "EXIT:$?")
echo "$RESULT" | grep -q "EXIT:2" && echo "$RESULT" | grep -q "modified after last verification" && echo "✓ 35. dirty doc blocks DONE" || { echo "✗ 35. dirty block: $RESULT"; exit 1; }

# 36. Re-verifying (log --doc re-snapshots) unblocks DONE
echo "# Verification findings (post-edit round 1)" > .jig/specs/recipes/ad-001/verification.md
$JIG recipe log ad-001 --iteration 3 --critical 0 --major 0 --doc .jig/specs/recipes/ad-001/draft.md --scope full --dimension verify --dimension audit --dimension simulate > /dev/null
echo "# Verification findings (post-edit round 2)" > .jig/specs/recipes/ad-001/verification.md
$JIG recipe log ad-001 --iteration 4 --critical 0 --major 0 --doc .jig/specs/recipes/ad-001/draft.md --scope full --dimension verify --dimension audit --dimension simulate > /dev/null
RESULT=$(echo '{"session_id":"t","cwd":"'"$TEST_DIR"'","hook_event_name":"stop","content":"<jig>DONE</jig>"}' | $JIG hook stop 2>&1; echo "EXIT:$?")
echo "$RESULT" | grep -q "EXIT:0" && echo "✓ 36. re-verify unblocks DONE" || { echo "✗ 36. re-verify allow: $RESULT"; exit 1; }

# 37. jig test run — status is machine-truth from the exit code
$JIG test run ad-001 --cmd "exit 0" > /dev/null 2>&1 || { echo "✗ 37. test run pass"; exit 1; }
grep -q '"recorded_by": "jig"' .jig/specs/recipes/ad-001/test-results.json || { echo "✗ 37. recorded_by"; exit 1; }
grep -q '"status": "pass"' .jig/specs/recipes/ad-001/test-results.json || { echo "✗ 37. pass status"; exit 1; }
if $JIG test run ad-001 --cmd "exit 1" > /dev/null 2>&1; then echo "✗ 37. fail exit"; exit 1; fi
grep -q '"status": "fail"' .jig/specs/recipes/ad-001/test-results.json && grep -q '"iterations": 2' .jig/specs/recipes/ad-001/test-results.json && echo "✓ 37. test run machine-truth (pass→fail, iteration 2)" || { echo "✗ 37. fail status"; exit 1; }

# 38. stats --outcomes runs and correlates
$JIG stats --outcomes | grep -q "Grouped means" && echo "✓ 38. stats --outcomes" || { echo "✗ 38. outcomes"; exit 1; }

# 39. doctor flags config drift + hand-recorded test results
python3 -c "
import re
p = '.jig/config/settings.yaml'
s = open(p).read()
s = s.replace('# reviewer_security: sonnet', 'reviewer_security: sonnet')
open(p, 'w').write(s)
"
DOCTOR=$($JIG doctor 2>&1 || true)
echo "$DOCTOR" | grep -q "reviewer_security" || { echo "✗ 39. doctor drift"; exit 1; }
echo "$DOCTOR" | grep -q "hand-recorded" && echo "✓ 39. doctor drift + provenance" || { echo "✗ 39. doctor provenance"; exit 1; }

# 40. Findings ledger — stable IDs, carry-forward, fixed/reopened tracking
mkdir -p .jig/specs/recipes/fl-001
echo '{"id":"fl-001","type":"spec","topic":"ledger","phase":"verify","iteration":0,"level":1.0,"started_at":"2026-03-18T00:00:00Z","updated_at":"2026-03-18T00:00:00Z"}' > .jig/specs/recipes/fl-001/recipe.json
echo "# Draft" > .jig/specs/recipes/fl-001/draft.md
cat > .jig/specs/recipes/fl-001/verification.md <<'VEOF'
<jig-findings>
{"critical": 1, "major": 0, "minor_resolvable": 0, "minor_deferred": 0, "info": 0,
 "findings": [{"severity": "critical", "title": "retry policy contradicts timeout section"}]}
</jig-findings>
VEOF
$JIG recipe log fl-001 --from-verification .jig/specs/recipes/fl-001/verification.md --doc .jig/specs/recipes/fl-001/draft.md > /dev/null
FID=$($JIG recipe findings list fl-001 --json | python3 -c "import json,sys; print(json.load(sys.stdin)[0]['id'])")
$JIG recipe findings carry-forward fl-001 --doc draft.md | grep -q "$FID" && echo "✓ 40. findings ledger + carry-forward" || { echo "✗ 40. carry-forward"; exit 1; }

# 41. A finding absent from the next round is recorded as fixed, and its
#     return is a reopen — the regression signal positional #N numbering
#     could never express.
cat > .jig/specs/recipes/fl-001/verification.md <<'VEOF'
<jig-findings>
{"critical": 0, "major": 0, "minor_resolvable": 0, "minor_deferred": 0, "info": 0, "findings": []}
</jig-findings>
VEOF
# Absence does not close a finding: it is demoted to `unreported` and needs a
# second consecutive silent round before it counts as fixed (see SyncFindings —
# absence is equally the signature of a repair and of a reworded restatement).
$JIG recipe log fl-001 --from-verification .jig/specs/recipes/fl-001/verification.md --doc .jig/specs/recipes/fl-001/draft.md | grep -q "1 unreported" || { echo "✗ 41. unreported"; exit 1; }
$JIG recipe log fl-001 --from-verification .jig/specs/recipes/fl-001/verification.md --doc .jig/specs/recipes/fl-001/draft.md | grep -q "1 confirmed fixed" || { echo "✗ 41. fixed"; exit 1; }
cat > .jig/specs/recipes/fl-001/verification.md <<'VEOF'
<jig-findings>
{"critical": 1, "major": 0, "minor_resolvable": 0, "minor_deferred": 0, "info": 0,
 "findings": [{"severity": "critical", "title": "retry policy contradicts timeout section"}]}
</jig-findings>
VEOF
$JIG recipe log fl-001 --from-verification .jig/specs/recipes/fl-001/verification.md --doc .jig/specs/recipes/fl-001/draft.md | grep -q "1 reopened" && echo "✓ 41. fixed → reopened tracked" || { echo "✗ 41. reopened"; exit 1; }

# 42. A findings array inconsistent with its counts is rejected outright
cat > .jig/specs/recipes/fl-001/bad.md <<'VEOF'
<jig-findings>
{"critical": 2, "major": 0, "minor_resolvable": 0, "minor_deferred": 0, "info": 0,
 "findings": [{"severity": "critical", "title": "only one entry for a count of two"}]}
</jig-findings>
VEOF
if $JIG recipe log fl-001 --from-verification .jig/specs/recipes/fl-001/bad.md --doc .jig/specs/recipes/fl-001/draft.md > /dev/null 2>&1; then
  echo "✗ 42. inconsistent findings array accepted"; exit 1
fi
echo "✓ 42. findings/counts mismatch rejected"

# 43. Convergence budget halts a loop that stops making progress
mkdir -p .jig/specs/recipes/cv-001
echo '{"id":"cv-001","type":"spec","topic":"converge","phase":"verify","iteration":0,"level":1.0,"started_at":"2026-03-18T00:00:00Z","updated_at":"2026-03-18T00:00:00Z"}' > .jig/specs/recipes/cv-001/recipe.json
echo "# Draft" > .jig/specs/recipes/cv-001/draft.md
cat > .jig/specs/recipes/cv-001/verification.md <<'VEOF'
<jig-findings>
{"critical": 0, "major": 2, "minor_resolvable": 0, "minor_deferred": 0, "info": 0,
 "findings": [{"severity": "major", "title": "stuck finding one"}, {"severity": "major", "title": "stuck finding two"}]}
</jig-findings>
VEOF
for i in 1 2 3; do
  $JIG recipe log cv-001 --from-verification .jig/specs/recipes/cv-001/verification.md --doc .jig/specs/recipes/cv-001/draft.md > /dev/null 2>&1 || true
done
OUT=$($JIG recipe log cv-001 --from-verification .jig/specs/recipes/cv-001/verification.md --doc .jig/specs/recipes/cv-001/draft.md 2>&1 || true)
echo "$OUT" | grep -q "CONVERGENCE FAILED" && echo "✓ 43. convergence budget halts a stalled loop" || { echo "✗ 43. convergence: $OUT"; exit 1; }
grep -q '"status":"failed"' .jig/specs/recipes/cv-001/verify-log.jsonl || { echo "✗ 43. failed status not recorded"; exit 1; }

# 44. assess-precheck answers FINALIZE from state, without an LLM round
mkdir -p .jig/specs/recipes/pc-001
echo '{"id":"pc-001","type":"spec","topic":"precheck","phase":"verify","iteration":0,"level":1.0,"started_at":"2026-03-18T00:00:00Z","updated_at":"2026-03-18T00:00:00Z"}' > .jig/specs/recipes/pc-001/recipe.json
echo "# Draft" > .jig/specs/recipes/pc-001/draft.md
cat > .jig/specs/recipes/pc-001/verification.md <<'VEOF'
<jig-findings>
{"critical": 0, "major": 0, "minor_resolvable": 0, "minor_deferred": 0, "info": 0, "findings": []}
</jig-findings>
VEOF
# A clean DELTA round must not finalize — untouched sections were never re-checked.
$JIG recipe log pc-001 --from-verification .jig/specs/recipes/pc-001/verification.md --doc .jig/specs/recipes/pc-001/draft.md --scope delta > /dev/null
$JIG recipe assess-precheck pc-001 --doc .jig/specs/recipes/pc-001/draft.md | grep -q '"action": "VERIFY"' || { echo "✗ 44. delta finalized"; exit 1; }
# A replicated clean FULL pass across every dimension does. One round is a
# sample, so each needs its own verification.md — re-recording one file does
# not raise the count.
$JIG recipe log pc-001 --from-verification .jig/specs/recipes/pc-001/verification.md --doc .jig/specs/recipes/pc-001/draft.md --scope full --dimension verify --dimension audit --dimension simulate > /dev/null
printf '<jig-findings>\n{"critical": 0, "major": 0, "minor_resolvable": 0, "minor_deferred": 0, "info": 0, "findings": []}\n</jig-findings>\n<!-- round 2 -->\n' > .jig/specs/recipes/pc-001/verification.md
$JIG recipe log pc-001 --from-verification .jig/specs/recipes/pc-001/verification.md --doc .jig/specs/recipes/pc-001/draft.md --scope full --dimension verify --dimension audit --dimension simulate > /dev/null
$JIG recipe assess-precheck pc-001 --doc .jig/specs/recipes/pc-001/draft.md | grep -q '"action": "FINALIZE"' || { echo "✗ 44. full pass did not finalize"; exit 1; }
# Editing the doc reopens the obligation to re-verify.
echo "edited after verification" >> .jig/specs/recipes/pc-001/draft.md
$JIG recipe assess-precheck pc-001 --doc .jig/specs/recipes/pc-001/draft.md | grep -q '"action": "VERIFY"' && echo "✓ 44. assess-precheck: delta/full/dirty decisions" || { echo "✗ 44. dirty doc"; exit 1; }

# 45. Per-document verify state — a clean wireframe round must not
#     satisfy a dirty draft's gate (one shared counter before v0.10).
$JIG recipe log pc-001 --from-verification .jig/specs/recipes/pc-001/verification.md --doc .jig/specs/recipes/pc-001/wireframe.md --scope full > /dev/null 2>&1 || true
echo "# Wireframe" > .jig/specs/recipes/pc-001/wireframe.md
$JIG recipe log pc-001 --from-verification .jig/specs/recipes/pc-001/verification.md --doc .jig/specs/recipes/pc-001/wireframe.md --scope full > /dev/null
$JIG recipe assess-precheck pc-001 --doc .jig/specs/recipes/pc-001/draft.md | grep -q '"action": "VERIFY"' && echo "✓ 45. wireframe round cannot clear draft.md" || { echo "✗ 45. cross-doc leak"; exit 1; }

# 46. Evidence cache — miss, put, normalised hit, and citation discipline
$JIG evidence get --library swiftui --topic "safeAreaInset" --claim "does not propagate into sheets" > /dev/null 2>&1 && { echo "✗ 46. unexpected hit"; exit 1; }
$JIG evidence put --library swiftui --topic "safeAreaInset" --claim "does not propagate into sheets" --verdict silent --gathered "Context7:miss" > /dev/null
$JIG evidence get --library SwiftUI --topic "  safeAreaInset " --claim "does not propagate into sheets" | grep -q "HIT" || { echo "✗ 46. normalised lookup missed"; exit 1; }
if $JIG evidence put --library go --claim "maps are ordered" --verdict confirms > /dev/null 2>&1; then
  echo "✗ 46. uncited confirms accepted"; exit 1
fi
echo "✓ 46. evidence cache (miss/put/hit, citation required)"

echo ""
echo "=== All 46 tests passed ==="
