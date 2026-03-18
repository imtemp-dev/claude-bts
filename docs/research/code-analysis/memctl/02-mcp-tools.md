# memctl -- MCP 도구 분석

> 소스 경로: `~/Workspace/context-sync-research/memctl/packages/cli/src/`
> 분석 대상: `server.ts`, `tools/index.ts`, `tools/response.ts`, `tools/rate-limit.ts`, `tools/handlers/*.ts`

---

## 목차

1. [MCP 서버 초기화](#1-mcp-서버-초기화)
2. [도구 등록 허브](#2-도구-등록-허브)
3. [전체 도구 & 액션 목록](#3-전체-도구--액션-목록)
4. [요청 처리 플로우](#4-요청-처리-플로우)
5. [응답 포맷팅](#5-응답-포맷팅)
6. [Rate Limiting](#6-rate-limiting)
7. [상세 플로우 예시](#7-상세-플로우-예시)

---

## 1. MCP 서버 초기화

**파일**: `server.ts` (L98-175)

### createServer() 함수 시그니처

```typescript
export function createServer(config: {
  baseUrl: string;
  token: string;
  org: string;
  project: string;
})
```

### 초기화 플로우

```
createServer(config)
  |
  +-- (1) createSessionTracker()         // 세션 상태 추적기 생성
  |
  +-- (2) new McpServer("memctl", "0.1.0")  // MCP 서버 인스턴스 생성
  |
  +-- (3) new ApiClient({                // API 클라이언트 생성
  |         ...config,
  |         onRequest: trackApiCall,      //   모든 API 호출 추적
  |         onMutation: invalidateMemoriesCache  // 변경 시 캐시 무효화
  |       })
  |
  +-- (4) startSessionLifecycle(client, tracker)  // 세션 수명주기 시작
  |
  +-- (5) registerTools(server, client, tracker)  // 11개 도구 등록
  |
  +-- (6) registerResources(server, client, tracker)  // 리소스 등록
  |
  +-- (7) registerPrompts(server)         // 3개 프롬프트 등록
  |
  +-- (8) server.server.onclose = () => {  // MCP 연결 종료 감지
  |         finalizeSession(client, tracker)
  |       }
  |
  +-- (9) server.resource("connection_status", ...)  // 연결 상태 리소스
  |
  +-- (10) client.ping() -> 온라인/오프라인 판별
            |-- 온라인: incrementalSync() 또는 listMemories(100)
            |-- 오프라인: stderr 경고 출력, 로컬 캐시 모드
```

### 등록되는 프롬프트 (3개)

| 프롬프트 | 설명 | 파라미터 |
|---|---|---|
| `agent-startup` | 에이전트 시작 시 자동 주입되는 컨텍스트. memctl 사용 규칙 및 세션 종료 필수 단계 안내 | 없음 |
| `context-for-files` | 특정 파일 수정 전 관련 컨텍스트 조회 가이드 | `files` (쉼표 구분 파일 경로) |
| `session-handoff` | 세션 종료 시 핸드오프 요약 생성 가이드 | 없음 |

`agent-startup` 프롬프트의 핵심 지시:
- memctl MCP 도구를 모든 영속 메모리에 사용
- 내장 auto memory나 MEMORY.md 파일 사용 금지
- 코드/git 출력/파일 내용/명령 결과를 메모리에 저장 금지
- 세션 종료 시 `activity memo_leave` + `session end` 필수 실행

---

## 2. 도구 등록 허브

**파일**: `tools/index.ts` (L1-51)

### registerTools() 구조

```typescript
export function registerTools(
  server: McpServer,
  client: ApiClient,
  tracker: SessionTracker,
) {
  const rl = createRateLimitState();     // (a) Rate limit 상태 생성

  const onToolCall = (tool: string, action: string): string | undefined => {
    recordToolAction(tracker, tool, action);  // (b) 모든 호출 기록

    // (c) bootstrap 감지: context bootstrap/bootstrap_compact 호출 시
    if (tool === "context" && (action === "bootstrap" || action === "bootstrap_compact")) {
      tracker.bootstrapped = true;
      return undefined;
    }

    // (d) bootstrap 미완료 시 1회 힌트 표시
    if (!tracker.bootstrapped && !tracker.bootstrapHintShown) {
      tracker.bootstrapHintShown = true;
      return "[Hint] Run context action=bootstrap first...";
    }

    return undefined;
  };

  // (e) 11개 도구 등록
  registerMemoryTool(server, client, rl, onToolCall);
  registerMemoryAdvancedTool(server, client, rl, onToolCall);
  registerMemoryLifecycleTool(server, client, rl, onToolCall);
  registerContextTool(server, client, rl, onToolCall);
  registerContextConfigTool(server, client, rl, onToolCall);
  registerBranchTool(server, client, rl, onToolCall);
  registerSessionTool(server, client, rl, tracker, onToolCall);
  registerImportExportTool(server, client, rl, onToolCall);
  registerRepoTool(server, client, rl, onToolCall);
  registerOrgTool(server, client, rl, onToolCall);
  registerActivityTool(server, client, rl, onToolCall);
}
```

### onToolCall 콜백 동작 요약

```
onToolCall(tool, action)
  |
  +-- recordToolAction(tracker, tool, action)   // 항상 기록
  |
  +-- context bootstrap?
  |     YES -> tracker.bootstrapped = true, return undefined
  |
  +-- tracker.bootstrapped == false AND !bootstrapHintShown?
        YES -> bootstrapHintShown = true
               return "[Hint] Run context action=bootstrap first..."
        NO  -> return undefined
```

### 공유 의존성 주입 패턴

모든 핸들러 등록 함수는 동일한 4개 의존성을 받는다:

| 의존성 | 타입 | 용도 |
|---|---|---|
| `server` | `McpServer` | `server.tool()` 호출로 도구 등록 |
| `client` | `ApiClient` | API 서버 통신 |
| `rl` | `RateLimitState` | 쓰기 호출 횟수 제한 |
| `onToolCall` | `(tool, action) => string \| undefined` | 세션 추적 + bootstrap 힌트 |

예외: `registerSessionTool`은 추가로 `tracker: SessionTracker`를 받는다 (세션 종료 시 tracker 데이터 병합 필요).

---

## 3. 전체 도구 & 액션 목록

### 3.1 memory

**파일**: `tools/handlers/memory.ts` (L17-161)
**설명**: 핵심 메모리 CRUD 도구

| 액션 | 설명 | 주요 파라미터 | Rate Limited |
|---|---|---|---|
| `store` | 메모리 저장 | `key`, `content`, `metadata?`, `scope?`, `priority?`, `tags?`, `expiresAt?`, `ttl?`, `dedupAction?`, `autoBranch?`, `forceStore?` | Yes |
| `get` | 단건 조회 | `key`, `includeHints?` | No |
| `search` | 검색 | `query`, `limit?`, `sort?`, `includeArchived?` | No |
| `list` | 목록 조회 | `limit?`, `offset?`, `sort?`, `includeArchived?` | No |
| `delete` | 삭제 | `key` | Yes |
| `update` | 수정 | `key`, `content?`, `metadata?`, `priority?`, `tags?`, `forceStore?` | Yes |
| `pin` | 고정/해제 | `key`, `pin` (boolean) | No |
| `archive` | 아카이브/해제 | `key`, `archiveFlag` (boolean) | No |
| `bulk_get` | 다건 조회 | `keys` (string[]) | No |
| `store_safe` | 충돌 방지 저장 | `key`, `content`, `ifUnmodifiedSince`, `onConflict?`, `metadata?`, `priority?`, `tags?`, `forceStore?` | No |
| `capacity` | 용량 확인 | 없음 | No |

**Zod 스키마 주요 필드**:

```typescript
action: z.enum(["store", "get", "search", "list", "delete", "update",
                 "pin", "archive", "bulk_get", "store_safe", "capacity"])
key: z.string().optional()
content: z.string().optional()
scope: z.enum(["project", "shared"]).optional()
priority: z.number().int().min(0).max(100).optional()
tags: z.array(z.string()).optional()
ttl: z.enum(["session", "pr", "sprint", "permanent"]).optional()
dedupAction: z.enum(["warn", "skip", "merge"]).optional()
onConflict: z.enum(["reject", "last_write_wins", "append", "return_both"]).optional()
```

**store 시 내부 처리 단계**:
1. Rate limit 확인 -> 초과 시 거부
2. `key`, `content` 필수 검증
3. Hard limit 검사 (16,384자 초과 시 거부)
4. `isGenericCapabilityNoise()` 품질 필터 (forceStore=false일 때)
5. Soft limit 경고 (4,096자 초과 시)
6. `autoBranch`: 현재 git branch 태그 자동 추가 (main/master 제외)
7. TTL 해석: `session`=24h, `pr`=7d, `sprint`=14d
8. 중복 검사: `dedupAction`에 따라 warn/skip/merge
9. 관련 메모리 탐색 -> link 힌트 제공
10. 용량 초과 시 auto-eviction (low-health 메모리 자동 아카이브)
11. `client.storeMemory()` 호출
12. 응답 조합 (scope/ttl/dedup/size/eviction/link/rate 메시지)

**store_safe 충돌 해결 전략**:

| onConflict | 동작 |
|---|---|
| `reject` (기본값) | 충돌 시 양쪽 버전 500자 미리보기와 함께 거부 |
| `last_write_wins` | 현재 내용으로 덮어쓰기 |
| `append` | 기존 내용 + `\n\n---\n\n` + 새 내용 병합 |
| `return_both` | 양쪽 전체 버전을 반환하여 수동 병합 유도 |

---

### 3.2 memory_advanced

**파일**: `tools/handlers/memory-advanced.ts` (L17-516)
**설명**: 고급 메모리 연산 -- 지식 그래프, 스냅샷, 버전 관리, 품질 분석

| 액션 | 설명 | 주요 파라미터 | Rate Limited |
|---|---|---|---|
| `batch_mutate` | 다건 일괄 변경 | `keys`, `mutateAction`, `value?` | Yes |
| `snapshot_create` | 스냅샷 생성 | `name`, `description?` | No |
| `snapshot_list` | 스냅샷 목록 | `limit?` | No |
| `diff` | 버전 간 비교 | `key`, `v1`, `v2?` | No |
| `history` | 버전 이력 | `key`, `limit?` | No |
| `restore` | 버전 복원 | `key`, `version` | No |
| `link` | 메모리 연결/해제 | `key`, `relatedKey`, `unlink?` | No |
| `traverse` | 관계 그래프 순회 | `key`, `depth?` (max 5) | No |
| `graph` | 전체 그래프 조회 | 없음 | No |
| `contradictions` | 모순 탐지 | 없음 | No |
| `quality` | 품질 점수 분석 | `limit?` | No |
| `freshness` | 변경 감지 | `cachedHash?` | No |
| `size_audit` | 크기 감사 | `threshold?` | No |
| `sunset` | 폐기 후보 제안 | `limit?` | No |
| `undo` | 버전 롤백 | `key`, `steps?` | No |
| `compile` | 컨텍스트 컴파일 | `types?`, `compileTags?`, `branch?`, `maxTokens?`, `format?` | No |
| `change_digest` | 변경 요약 | `since`, `limit?` | No |
| `impact` | 영향도 분석 | `key` | No |
| `watch` | 변경 감시 | `keys`, `since` | No |
| `check_duplicates` | 중복 확인 | `content`, `excludeKey?`, `threshold?` | No |
| `auto_tag` | 자동 태깅 | `key`, `apply?` | No |
| `validate_schema` | 스키마 검증 | `type?`, `key?` | No |
| `branch_filter` | 브랜치별 필터 | `branch?` | No |
| `branch_merge` | 브랜치 병합 | `branch`, `mergeAction`, `dryRun?` | Conditional |
| `batch_ops` | 배치 API 연산 | `operations` (max 20개) | No |
| `consolidate` | 메모리 통합 | `keys` (2개 이상), `newKey` | Yes |

**mutateAction 옵션** (`batch_mutate`용):

```typescript
z.enum(["archive", "unarchive", "delete", "pin", "unpin",
         "set_priority", "add_tags", "set_scope"])
```

**graph 액션 분석 알고리즘**:
- 모든 메모리의 `relatedKeys`로 인접 리스트(adjacency list) 구성
- BFS로 연결 컴포넌트(cluster) 탐지
- DFS로 순환(cycle) 탐지 (WHITE/GRAY/BLACK 상태 머신)
- 고립 노드(orphan) 식별

**contradictions 탐지 방식**:
- 정규식으로 "use/prefer/always" vs "avoid/never/don't" 지시어 추출
- 동일 subject에 대한 긍정/부정 지시어 쌍을 충돌로 판정
- 정확 일치 시 confidence 0.9, 부분 일치 시 0.6

**quality 점수 산정** (100점 기준 감점):

| 조건 | 감점 |
|---|---|
| 50자 미만 | -30 |
| 50~150자 | -10 |
| 500자 초과 + 구조화 없음 | -15 |
| 부정적 피드백 우세 (3건 이상) | -25 |
| 60일 이상 미접근 | -20 |
| 30~60일 미접근 | -10 |
| TODO/FIXME 마커 포함 | -15 |

---

### 3.3 memory_lifecycle

**파일**: `tools/handlers/memory-lifecycle.ts` (L11-426)
**설명**: 메모리 수명주기 관리 -- 정리, 피드백, 잠금, 건강도

| 액션 | 설명 | 주요 파라미터 |
|---|---|---|
| `cleanup` | 만료된 메모리 제거 | 없음 |
| `suggest_cleanup` | 정리 제안 | `staleDays?`, `limit?` |
| `lifecycle_run` | 수명주기 정책 실행 | `policies`, `healthThreshold?`, `maxVersionsPerMemory?`, `activityLogMaxAgeDays?`, `archivePurgeDays?`, `mergedBranches?`, `sessionLogMaxAgeDays?` |
| `lifecycle_schedule` | 예약 수명주기 실행 | `sessionLogMaxAgeDays?`, `accessThreshold?`, `feedbackThreshold?` |
| `validate_references` | 파일 참조 유효성 검증 | 없음 (git ls-files 사용) |
| `prune_stale` | 부실 참조 정리 | `archiveStale?` |
| `feedback` | 피드백 기록 | `key`, `helpful` (boolean) |
| `analytics` | 분석 데이터 조회 | 없음 |
| `lock` | 메모리 잠금 | `key`, `lockedBy?`, `ttlSeconds?` (기본 60, 최대 600) |
| `unlock` | 메모리 잠금 해제 | `key`, `lockedBy?` |
| `health` | 건강도 점수 조회 | `limit?` |
| `policy_get` | 정리 정책 조회 | 없음 |
| `policy_set` | 정리 정책 설정 | `policyConfig` |

**lifecycle_run 정책 목록**:

```typescript
z.enum([
  "archive_merged_branches",   // 병합된 브랜치 메모리 아카이브
  "cleanup_expired",           // 만료 메모리 제거
  "cleanup_session_logs",      // 오래된 세션 로그 제거
  "auto_promote",              // 자주 접근되는 메모리 우선순위 상향
  "auto_demote",               // 부정 피드백 메모리 우선순위 하향
  "auto_prune",                // 자동 정리
  "auto_archive_unhealthy",    // 건강도 낮은 메모리 아카이브
  "cleanup_old_versions",      // 오래된 버전 제거
  "cleanup_activity_logs",     // 오래된 활동 로그 제거
  "cleanup_expired_locks",     // 만료된 잠금 제거
  "purge_archived",            // 아카이브된 메모리 완전 삭제
])
```

**policyConfig 구조** (`policy_set`용):

```typescript
z.object({
  autoCleanupOnBootstrap: z.boolean().optional(),     // 기본값: true
  maxStaleDays: z.number().int().min(1).max(365).optional(),  // 기본값: 30
  autoArchiveHealthBelow: z.number().min(0).max(100).optional(),  // 기본값: 15
  maxMemories: z.number().int().min(1).optional(),    // 기본값: 500
})
```

정책 저장 위치: 메모리 키 `agent/config/cleanup_policy` (priority 100, 태그 `system:config`)

---

### 3.4 context

**파일**: `tools/handlers/context.ts` (L26-203, 전체 1190행)
**설명**: 에이전트 컨텍스트 연산 -- bootstrap, functionality CRUD, 스마트 검색

| 액션 | 설명 | 주요 파라미터 |
|---|---|---|
| `bootstrap` | 전체 컨텍스트 로드 + 유지보수 | `includeContent?`, `types?`, `branch?` |
| `bootstrap_compact` | 컴팩트 모드 bootstrap (내용 제외) | 없음 |
| `bootstrap_delta` | 시점 이후 변경분만 로드 | `since` |
| `functionality_get` | 기능 컨텍스트 조회 | `type`, `id?`, `includeContent?`, `followLinks?` |
| `functionality_set` | 기능 컨텍스트 저장 | `type`, `id`, `content`, `title?`, `metadata?`, `priority?`, `tags?` |
| `functionality_delete` | 기능 컨텍스트 삭제 | `type`, `id` |
| `functionality_list` | 기능 컨텍스트 목록 | `type?`, `includeContentPreview?`, `limitPerType?` |
| `context_for` | 파일별 관련 컨텍스트 조회 | `filePaths`, `types?` |
| `budget` | 토큰 예산 기반 컨텍스트 선택 | `maxTokens`, `types?`, `includeKeys?` |
| `compose` | 작업 기반 컨텍스트 구성 | `task`, `maxTokens?`, `includeRelated?` |
| `smart_retrieve` | 의도 기반 스마트 검색 | `intent`, `files?`, `maxResults?`, `followLinks?` |
| `search_org` | 조직 전체 검색 | `query`, `limit?` |
| `rules_evaluate` | 조건부 규칙 평가 | `filePaths?`, `taskType?`, `branch?` |
| `thread` | 세션 스레드 분석 | `sessionCount?`, `branch?` |

**bootstrap 내부 처리**:
1. 정리 정책 확인 (`autoCleanupOnBootstrap` 플래그)
2. 병렬 실행:
   - 자동 정리 (500ms 타임아웃, fire-and-forget)
   - `listAllMemories(client)`
   - `getBranchInfo()`
   - `client.getMemoryCapacity()`
   - `getAllContextTypeInfo(client)`
3. 메모리에서 agent context 항목 추출 (`extractAgentContextEntries`)
4. 타입별/브랜치별 필터링 및 정렬
5. 브랜치 플랜 조회
6. 신규 프로젝트 시 org defaults 힌트 제공

**context_for 관련도 스코어링**:
- 파일 경로의 각 디렉토리/확장자 부분 매칭: +10
- 전체 파일 경로 매칭: +50
- 기본 priority 값 가산
- 대상 타입: architecture, coding_style, testing, constraints, file_map, folder_structure

**smart_retrieve 스코어링 가중치** (intent 분류 기반):
- `ftsBoost`: 키워드 매칭 부스트
- `priorityBoost`: 우선순위 부스트
- `recencyBoost`: 최근 접근 부스트
- `graphBoost`: 관계 그래프 부스트

---

### 3.5 context_config

**파일**: `tools/handlers/context-config.ts` (L12-206)
**설명**: 컨텍스트 타입 설정 관리

| 액션 | 설명 | 주요 파라미터 |
|---|---|---|
| `type_create` | 커스텀 타입 생성 | `slug`, `label`, `description` |
| `type_list` | 모든 타입 목록 (빌트인+커스텀) | 없음 |
| `type_delete` | 커스텀 타입 삭제 | `slug` |
| `template_get` | 타입별 템플릿 조회 | `type` |

**빌트인 타입에 대한 보호**: `BUILTIN_AGENT_CONTEXT_TYPES`에 포함된 slug는 생성/삭제 불가.

**template_get 내장 템플릿 목록**:

| 타입 | 설명 |
|---|---|
| `coding_style` | 코딩 규칙 및 스타일 가이드 |
| `architecture` | 시스템 아키텍처 및 설계 결정 |
| `testing` | 테스트 전략 및 요구사항 |
| `constraints` | 엄격한 규칙 및 안전 제한 |
| `lessons_learned` | 함정 및 네거티브 지식 |
| `workflow` | 개발 워크플로우 및 프로세스 |
| `folder_structure` | 저장소 구조 |
| `file_map` | 주요 파일 위치 |
| `user_ideas` | 기능 요청 및 개선 아이디어 |
| `known_issues` | 알려진 버그, 우회 방법, 환경 문제 |
| `decisions` | 근거 있는 설계 결정 |

---

### 3.6 branch

**파일**: `tools/handlers/branch.ts` (L17-208)
**설명**: 브랜치 컨텍스트 관리

| 액션 | 설명 | 주요 파라미터 |
|---|---|---|
| `get` | 브랜치 플랜 조회 | `branch?`, `includeRelatedContext?` |
| `set` | 브랜치 플랜 저장 | `branch?`, `content`, `metadata?`, `status?`, `checklist?` |
| `delete` | 브랜치 플랜 삭제 | `branch?` |

**status 옵션**: `planning`, `in_progress`, `review`, `merged`

**checklist 스키마**:

```typescript
z.array(z.object({ item: z.string(), done: z.boolean() }))
```

**get 시 추가 기능**:
- `includeRelatedContext=true` 시 브랜치명 토큰으로 관련 메모리 검색 (branch_plan 타입 제외)
- 메타데이터에서 `planStatus`, `checklist`, `completedItems`, `totalItems` 추출

**키 형식**: `buildBranchPlanKey(branch)` 함수로 결정 (예: `agent/branch_plan/<branch-name>`)

---

### 3.7 session

**파일**: `tools/handlers/session.ts` (L8-244)
**설명**: 세션 수명주기 및 충돌 관리

| 액션 | 설명 | 주요 파라미터 | Rate Limited |
|---|---|---|---|
| `end` | 세션 종료 + 요약 저장 | `sessionId?`, `summary?`, `keysRead?`, `keysWritten?`, `toolsUsed?` | No |
| `history` | 최근 세션 목록 | `limit?`, `branch?` | No |
| `claims_check` | 메모리 키 충돌 확인 | `keys` (필수), `excludeSession?` | No |
| `claim` | 메모리 키 예약 | `keys` (필수), `sessionId?`, `ttlMinutes?` | Yes |
| `rate_status` | 쓰기 Rate limit 상태 | 없음 | No |

**end 시 tracker 데이터 병합**:
- `params.keysWritten` + `tracker.writtenKeys` -> 중복 제거 후 병합
- `params.keysRead` + `tracker.readKeys` -> 중복 제거 후 병합
- `params.toolsUsed` + `tracker.toolActions` -> 중복 제거 후 병합
- `tracker.endedExplicitly = true` 설정

**claims_check 동작**:
1. `agent/claims/` 프리픽스로 검색 (태그: `session-claim`)
2. 만료된 claim 제외
3. `excludeSession` 제외
4. 요청된 keys와 겹치는 claim이 있으면 충돌 보고

**claim 저장 형식**:
- 키: `agent/claims/<sessionId>`
- 내용: 예약된 키 배열 JSON
- 메타데이터: `{ sessionId, claimedAt }`
- 태그: `["session-claim"]`
- 만료: `ttlMinutes` (기본 30분)

---

### 3.8 import_export

**파일**: `tools/handlers/import-export.ts` (L15-203)
**설명**: 외부 형식 가져오기/내보내기

| 액션 | 설명 | 주요 파라미터 |
|---|---|---|
| `agents_md_import` | AGENTS.md 파일 가져오기 | `content`, `dryRun?`, `overwrite?` |
| `cursorrules_import` | Cursor Rules / Copilot 파일 가져오기 | `content`, `dryRun?`, `overwrite?`, `source?` |
| `export_agents_md` | AGENTS.md / cursorrules / JSON 형식 내보내기 | `format?` |
| `export_memories` | 전체 메모리 내보내기 | `format?` |

**source 옵션** (`cursorrules_import`): `cursorrules`, `copilot`

**format 옵션** (`export_*`): `agents_md`, `cursorrules`, `json`

**import 처리 흐름**:
1. `parseAgentsMd(content)` -> 섹션 파싱
2. `dryRun=true` 시 미리보기만 반환
3. 각 섹션에 대해 `buildAgentContextKey(type, id)` 키 생성
4. `overwrite=false`면 기존 키 건너뛰기
5. `client.storeMemory()` 호출 (메타데이터에 `importedFrom` 기록)

---

### 3.9 repo

**파일**: `tools/handlers/repo.ts` (L12-387)
**설명**: 저장소 연산 -- 스캔, 온보딩

| 액션 | 설명 | 주요 파라미터 |
|---|---|---|
| `scan` | 저장소 파일 스캔 | `maxFiles?`, `includePatterns?`, `excludePatterns?`, `saveAsContext?` |
| `scan_check` | 저장된 file map 변경 확인 | 없음 |
| `onboard` | 프로젝트 자동 온보딩 | `apply?` |

**scan 동작**:
- `git ls-files --cached --others --exclude-standard` 실행
- include/exclude glob 패턴 필터링 (`matchGlob()` 사용)
- 디렉토리별 / 확장자별 분류
- `saveAsContext=true` 시 `agent/context/file_map/auto-scan` 키에 저장

**onboard 자동 감지 항목**:

| 감지 대상 | 방법 |
|---|---|
| 패키지 매니저 | `pnpm-lock.yaml` / `bun.lockb` / `yarn.lock` 존재 확인 |
| 프레임워크 | `package.json` dependencies 분석 (Next.js, Nuxt, SvelteKit, React, Vue, Express, Fastify, Hono) |
| 테스트 러너 | `vitest`, `jest`, `mocha` 의존성 확인 |
| 언어 | TypeScript vs JavaScript |
| 린터/포매터 | ESLint, Biome, Prettier |
| 모노레포 | workspaces, pnpm-workspace.yaml |
| Docker | Dockerfile 존재 |
| CI | `.github/workflows/ci.yml` 존재 |

`apply=true` 시 감지 결과를 `coding_style`, `testing`, `architecture`, `workflow`, `folder_structure` 타입으로 자동 저장.

---

### 3.10 org

**파일**: `tools/handlers/org.ts` (L7-234)
**설명**: 조직 수준 연산 -- 기본값, 프로젝트 비교, 템플릿

| 액션 | 설명 | 주요 파라미터 |
|---|---|---|
| `defaults_list` | 조직 기본 메모리 목록 | 없음 |
| `defaults_set` | 조직 기본 메모리 설정 | `key`, `content`, `metadata?`, `priority?`, `tags?` |
| `defaults_apply` | 조직 기본값을 현재 프로젝트에 적용 | 없음 |
| `context_diff` | 두 프로젝트 간 컨텍스트 비교 | `projectA`, `projectB` |
| `template_list` | 템플릿 목록 | 없음 |
| `template_apply` | 템플릿 적용 | `templateId` |
| `template_create` | 템플릿 생성 | `name`, `description?`, `data` |

**template_create의 data 스키마**:

```typescript
z.array(z.object({
  key: z.string(),
  content: z.string(),
  metadata: z.record(z.unknown()).optional(),
  priority: z.number().optional(),
  tags: z.array(z.string()).optional(),
}))
```

**context_diff 출력**: `stats.onlyInA`, `stats.onlyInB`, `stats.contentDiffers` 통계와 함께 정렬 힌트 제공.

---

### 3.11 activity

**파일**: `tools/handlers/activity.ts` (L7-253)
**설명**: 활동 로깅 및 에이전트 간 메모

| 액션 | 설명 | 주요 파라미터 |
|---|---|---|
| `log` | 활동 로그 조회 | `limit?`, `sessionId?`, `branch?` |
| `generate_git_hooks` | Git hook 스크립트 생성 | `hooks` (배열: `pre-commit`, `post-checkout`, `prepare-commit-msg`) |
| `memo_leave` | 다음 세션을 위한 메모 남기기 | `message`, `urgency?`, `relatedKeys?` |
| `memo_read` | 이전 세션 메모 읽기 | 없음 |

**memo_leave 우선순위 매핑**:

| urgency | priority | TTL |
|---|---|---|
| `info` | 30 | 3일 |
| `warning` | 60 | 3일 |
| `blocker` | 90 | 7일 |

**memo 저장 형식**:
- 키: `agent/memo/<base36-timestamp>`
- 태그: `["memo", "<urgency>"]`
- 만료: urgency에 따른 TTL

**memo_read 정렬**: blocker -> warning -> info 순서, 각 그룹 내 최신순.

---

## 4. 요청 처리 플로우

### MCP 메시지에서 응답까지의 전체 경로

```
MCP Client (AI 에이전트)
  |
  | tools/call { name: "memory", arguments: { action: "store", key: "...", content: "..." } }
  v
McpServer (SDK)
  |
  | 등록된 tool handler 호출
  v
memory tool handler (memory.ts L132-161)
  |
  +-- (1) onToolCall("memory", "store")       // 세션 추적 + bootstrap 힌트 확인
  |         |
  |         +-- recordToolAction(tracker, "memory", "store")
  |         +-- bootstrap 여부 확인 -> 필요 시 힌트 반환
  |
  +-- (2) switch (params.action)
  |         case "store": handleStore(client, rl, params)
  |
  v
handleStore() (memory.ts L214-388)
  |
  +-- (3) rl.checkRateLimit()                 // Rate limit 확인
  +-- (4) rl.incrementWriteCount()            // 쓰기 카운트 증가
  +-- (5) 파라미터 검증 (key, content 필수)
  +-- (6) Hard limit 검사 (16,384자)
  +-- (7) isGenericCapabilityNoise() 필터
  +-- (8) Soft limit 경고 (4,096자)
  +-- (9) getBranchInfo() -> autoBranch 태그
  +-- (10) TTL 해석
  +-- (11) client.findSimilar() -> dedupAction 처리
  +-- (12) client.findSimilar() -> link 힌트
  +-- (13) client.getMemoryCapacity() -> auto-eviction
  |
  +-- (14) client.storeMemory(key, content, metadata, opts)
  |           |
  |           +-- ApiClient.onRequest({ method, path, body })
  |           |     -> trackApiCall(tracker, ...)
  |           |
  |           +-- HTTP POST /api/v1/memories
  |           |
  |           +-- ApiClient.onMutation()
  |                 -> invalidateMemoriesCache()
  |
  +-- (15) textResponse("Memory stored with key: ...")
  |
  v
MCP 응답 -> AI 에이전트
  { content: [{ type: "text", text: "Memory stored with key: ..." }] }
```

### 읽기 vs 쓰기 경로 차이

```
읽기 경로 (get, search, list, bulk_get):
  onToolCall -> handler -> client.xxxMemory() -> textResponse(JSON)
  * Rate limit 확인 없음
  * 캐시 무효화 없음

쓰기 경로 (store, update, delete):
  onToolCall -> rl.checkRateLimit() -> rl.incrementWriteCount()
  -> handler -> client.xxxMemory()
  -> ApiClient.onMutation() -> invalidateMemoriesCache()
  -> textResponse("...")
  * Rate limit 확인/증가 필수
  * 캐시 자동 무효화
```

---

## 5. 응답 포맷팅

**파일**: `tools/response.ts` (L1-67)

### textResponse()

```typescript
export function textResponse(text: string, freshness?: Freshness)
```

| Freshness | 설명 |
|---|---|
| `"fresh"` | API에서 방금 가져온 데이터 |
| `"cached"` | 로컬 캐시에서 가져온 데이터 |
| `"stale"` | 캐시되었지만 오래된 데이터 |
| `"offline"` | 오프라인 모드 캐시 데이터 |

**freshness 주입 로직**:

```
textResponse(text, freshness)
  |
  +-- freshness 없음?
  |     -> { content: [{ type: "text", text }] }
  |
  +-- text가 JSON?
  |     -> 파싱 후 _meta.freshness 필드 주입
  |        { content: [{ type: "text", text: JSON.stringify({...parsed, _meta: { freshness }}) }] }
  |
  +-- text가 JSON이 아님?
        -> 문자열 끝에 [freshness: xxx] 접미사 추가
           { content: [{ type: "text", text: `${text}\n[freshness: ${freshness}]` }] }
```

### errorResponse()

```typescript
export function errorResponse(prefix: string, error: unknown)
// -> { content: [{ type: "text", text: `${prefix}: ${message}` }], isError: true }
```

`isError: true` 플래그로 MCP 클라이언트가 오류임을 인식할 수 있다.

### 보조 함수

| 함수 | 용도 |
|---|---|
| `hasMemoryFullError(error)` | 에러 메시지에 "memory limit reached" 포함 여부 |
| `toFiniteLimitText(limit)` | `Infinity` -> `"unlimited"`, 그 외 -> 숫자 문자열 |
| `formatCapacityGuidance(capacity)` | 용량 상태에 따른 가이드 메시지 |
| `matchGlob(filepath, pattern)` | glob 패턴 매칭 (`**`, `*`, `?` 지원) |

**formatCapacityGuidance 조건별 출력**:

```
isFull    -> "Project memory limit reached (used/limit). Delete or archive..."
isApproaching -> "Approaching project limit (used/limit). Consider archiving..."
otherwise -> "Memory available. Project: used/limit."
```

---

## 6. Rate Limiting

**파일**: `tools/rate-limit.ts` (L1-49)

### RateLimitState 인터페이스

```typescript
export interface RateLimitState {
  RATE_LIMIT: number;                     // 세션당 쓰기 한도 (기본 500)
  writeCallCount: number;                 // 현재 쓰기 호출 횟수
  checkRateLimit(): { allowed: boolean; warning?: string };
  incrementWriteCount(): void;
  getSessionWriteWarning(): string | null;
}
```

### 한도 설정

- 기본값: 500회
- 환경변수: `MEMCTL_RATE_LIMIT`으로 오버라이드 가능

### 상태 머신

```
  writeCallCount
       |
       v
  [0% ~ 79%]  ------>  checkRateLimit() = { allowed: true }
       |                (경고 없음)
       |
       v
  [80% ~ 99%] ------>  checkRateLimit() = { allowed: true, warning: "Approaching rate limit: N/500 (XX%)" }
       |                (경고 메시지 포함)
       |
       v
  [100%+]      ------>  checkRateLimit() = { allowed: false, warning: "Rate limit reached (N/500)..." }
                        (쓰기 차단)
```

```
                       +-------+
                       | 0-79% |  allowed=true, no warning
                       +---+---+
                           |
          writeCallCount >= 80% of RATE_LIMIT
                           |
                           v
                      +--------+
                      | 80-99% |  allowed=true, warning="Approaching..."
                      +---+----+
                          |
          writeCallCount >= 100% of RATE_LIMIT
                          |
                          v
                      +------+
                      | 100% |  allowed=false, warning="Rate limit reached..."
                      +------+
```

### getSessionWriteWarning()

15회 이상 쓰기 시 추가 경고:

```
writeCallCount >= 15 -> "Note: N writes this session. Consider consolidating..."
writeCallCount < 15  -> null
```

### Rate limit이 적용되는 액션 목록

| 도구 | 액션 |
|---|---|
| `memory` | `store`, `delete`, `update` |
| `memory_advanced` | `batch_mutate`, `consolidate`, `branch_merge` (조건부) |
| `session` | `claim` |

---

## 7. 상세 플로우 예시

### 7.1 memory action=store (10단계 전체 플로우)

**시나리오**: 에이전트가 "auth 마이크로서비스는 JWT를 사용한다"는 결정을 저장

```
입력:
  memory action=store
    key="decisions/auth-jwt"
    content="## Decision\nUse JWT for auth microservice.\n\n## Rationale\nStateless, scalable."
    priority=80
    tags=["auth","jwt"]
    ttl="permanent"
    dedupAction="warn"
```

**단계별 추적**:

```
[1] onToolCall("memory", "store")
    -> recordToolAction(tracker, "memory", "store")
    -> tracker.bootstrapped == true (이미 bootstrap 완료)
    -> return undefined (힌트 없음)

[2] switch -> handleStore(client, rl, params)

[3] rl.checkRateLimit()
    -> writeCallCount=5, RATE_LIMIT=500, pct=0.01
    -> { allowed: true } (경고 없음)

[4] rl.incrementWriteCount()
    -> writeCallCount = 6

[5] 파라미터 검증
    -> key="decisions/auth-jwt" (존재)
    -> content="## Decision..." (존재, 81자)
    -> 통과

[6] Hard limit 검사
    -> content.length=81 < 16,384
    -> 통과

[7] isGenericCapabilityNoise(content)
    -> "jwt", "auth", "microservice" 등 프로젝트 특화 신호 발견
    -> false (저장 허용)

[8] Soft limit 경고
    -> content.length=81 < 4,096
    -> sizeWarning = "" (경고 없음)

[9] getBranchInfo()
    -> branch="feature/auth-service" (main/master 아님)
    -> resolvedTags = ["auth", "jwt", "branch:feature/auth-service"]

[10] TTL 해석
    -> ttl="permanent"
    -> resolvedExpiry = undefined (만료 없음)

[11] 중복 검사 (dedupAction="warn")
    -> client.findSimilar(content, key, 0.7)
    -> similar = [] (유사 메모리 없음)
    -> dedupWarning = ""

[12] 관련 메모리 검색 (link 힌트)
    -> client.findSimilar(content, key, 0.4)
    -> similar = [{ key: "architecture/auth-overview", similarity: 0.55 }]
    -> linkHint = ' Related memories found: "architecture/auth-overview". Use memory_advanced action=link...'

[13] Auto-eviction 확인
    -> client.getMemoryCapacity() -> { isFull: false }
    -> evictionMsg = "" (필요 없음)

[14] API 호출
    -> client.storeMemory("decisions/auth-jwt", content, undefined, {
         scope: "project",
         priority: 80,
         tags: ["auth", "jwt", "branch:feature/auth-service"],
       })
    -> ApiClient.onRequest: trackApiCall(tracker, "POST", "/memories", ...)
    -> ApiClient.onMutation: invalidateMemoriesCache()

[15] 응답 생성
    -> textResponse("Memory stored with key: decisions/auth-jwt Related memories found: ...")

출력:
  {
    content: [{
      type: "text",
      text: "Memory stored with key: decisions/auth-jwt Related memories found: \"architecture/auth-overview\". Use memory_advanced action=link to connect them."
    }]
  }
```

---

### 7.2 context action=bootstrap

**시나리오**: 새 세션 시작, 에이전트가 프로젝트 컨텍스트 전체 로드

```
입력:
  context action=bootstrap
    includeContent=true
```

**단계별 추적**:

```
[1] onToolCall("context", "bootstrap")
    -> recordToolAction(tracker, "context", "bootstrap")
    -> tool="context", action="bootstrap" 감지
    -> tracker.bootstrapped = true   <<< 핵심: 이후 힌트 표시 안 함
    -> return undefined

[2] switch -> handleBootstrap(client, params)

[3] 정리 정책 확인 (fire-and-forget, 500ms 타임아웃)
    -> client.getMemory("agent/config/cleanup_policy")
    -> 정책 미발견 또는 autoCleanupOnBootstrap != false
    -> client.runLifecycle([
         "cleanup_expired",
         "cleanup_session_logs",
         "auto_archive_unhealthy",
         "cleanup_expired_locks",
       ], { healthThreshold: 15 })
    -> Promise.race([cleanupPromise, 500ms timeout])

[4] 병렬 데이터 로드
    -> Promise.all([
         listAllMemories(client),          // 전체 메모리 (캐시 활용)
         getBranchInfo(),                   // git branch 정보
         client.getMemoryCapacity(),        // 용량 현황
         getAllContextTypeInfo(client),     // 컨텍스트 타입 정보
       ])

[5] maintenancePromise await (정리 결과 또는 타임아웃)

[6] extractAgentContextEntries(allMemories)
    -> agent context 형식 메모리만 추출

[7] 타입별 항목 구성
    -> 각 selectedType에 대해:
       - 해당 타입 항목 필터링
       - branchTag 있으면 해당 브랜치 항목 우선 정렬
       - 항목별 { id, title, key, priority, tags, isPinned, scope, updatedAt, content }

[8] 브랜치 플랜 조회
    -> branchInfo.branch 존재 시 buildBranchPlanKey(branch) 키로 조회

[9] 용량 가이드 생성
    -> formatCapacityGuidance(capacity)
    -> 예: "Memory available. Project: 42/500."

[10] 신규 프로젝트 시 org defaults 힌트
    -> entries.length == 0인 경우 client.listOrgDefaults() 확인

[11] 응답 생성
    -> textResponse(JSON.stringify({
         functionalityTypes: [...],
         currentBranch: { branch: "feature/auth-service", ... },
         branchPlan: { ... } | null,
         memoryStatus: { used: 42, limit: 500, guidance: "..." },
         availableTypes: ["architecture", "coding_style", ...],
         orgDefaultsHint: null,
         maintenance: { results: { ... } },
       }))

출력:
  JSON 형식의 전체 프로젝트 컨텍스트 (타입별 항목, 브랜치 정보, 용량 상태)
```

---

### 7.3 session action=end

**시나리오**: 에이전트가 작업 완료 후 세션 종료

```
입력:
  session action=end
    summary="Implemented JWT auth for the auth microservice. Added token validation middleware. Open question: refresh token rotation strategy."
```

**단계별 추적**:

```
[1] onToolCall("session", "end")
    -> recordToolAction(tracker, "session", "end")
    -> tracker.bootstrapped == true
    -> return undefined (힌트 없음)

[2] switch -> case "end"

[3] sessionId 결정
    -> params.sessionId 미제공
    -> tracker.sessionId 사용 (예: "sess_abc123")

[4] tracker 데이터 수집
    -> trackerWritten = [...tracker.writtenKeys]
       예: ["decisions/auth-jwt", "architecture/auth-overview"]
    -> trackerRead = [...tracker.readKeys]
       예: ["constraints/security", "coding_style/typescript"]
    -> trackerTools = [...tracker.toolActions]
       예: ["memory.store", "context.bootstrap", "memory.get", "session.end"]

[5] 병합 (에이전트 제공 데이터 + tracker 자동 추적 데이터)
    -> mergedKeysWritten = Set합집합(params.keysWritten ?? [], trackerWritten)
    -> mergedKeysRead = Set합집합(params.keysRead ?? [], trackerRead)
    -> mergedToolsUsed = Set합집합(params.toolsUsed ?? [], trackerTools)

[6] 요약문 정리
    -> summary = params.summary.trim()
    -> "Implemented JWT auth for the auth microservice..."

[7] API 호출
    -> client.upsertSessionLog({
         sessionId: "sess_abc123",
         summary: "Implemented JWT auth...",
         keysRead: ["constraints/security", "coding_style/typescript"],
         keysWritten: ["decisions/auth-jwt", "architecture/auth-overview"],
         toolsUsed: ["memory.store", "context.bootstrap", "memory.get", "session.end"],
         endedAt: Date.now(),
         lastActivityAt: tracker.lastActivityAt,
       })

[8] tracker 상태 갱신
    -> tracker.endedExplicitly = true
    (이후 MCP 연결 종료 시 finalizeSession에서 이미 종료됨을 인식)

[9] 응답 생성
    -> textResponse("Session sess_abc123 ended. Handoff summary saved.")

출력:
  {
    content: [{
      type: "text",
      text: "Session sess_abc123 ended. Handoff summary saved."
    }]
  }
```

**연결 종료 시 후속 동작** (server.ts L126-128):

```
server.server.onclose = () => {
  void finalizeSession(client, tracker);
  // tracker.endedExplicitly == true 이므로
  // 중복 세션 로그 기록 방지
};
```

---

## 부록: 도구-핸들러 파일 매핑

| # | 도구명 | 핸들러 파일 | 등록 함수 | 액션 수 |
|---|---|---|---|---|
| 1 | `memory` | `tools/handlers/memory.ts` | `registerMemoryTool` | 11 |
| 2 | `memory_advanced` | `tools/handlers/memory-advanced.ts` | `registerMemoryAdvancedTool` | 27 |
| 3 | `memory_lifecycle` | `tools/handlers/memory-lifecycle.ts` | `registerMemoryLifecycleTool` | 13 |
| 4 | `context` | `tools/handlers/context.ts` | `registerContextTool` | 14 |
| 5 | `context_config` | `tools/handlers/context-config.ts` | `registerContextConfigTool` | 4 |
| 6 | `branch` | `tools/handlers/branch.ts` | `registerBranchTool` | 3 |
| 7 | `session` | `tools/handlers/session.ts` | `registerSessionTool` | 5 |
| 8 | `import_export` | `tools/handlers/import-export.ts` | `registerImportExportTool` | 4 |
| 9 | `repo` | `tools/handlers/repo.ts` | `registerRepoTool` | 3 |
| 10 | `org` | `tools/handlers/org.ts` | `registerOrgTool` | 7 |
| 11 | `activity` | `tools/handlers/activity.ts` | `registerActivityTool` | 4 |
| | | | **합계** | **94** |
