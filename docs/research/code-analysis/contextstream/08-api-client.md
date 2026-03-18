# ContextStream -- API 클라이언트 분석

> 소스: `src/client.ts` (6,673 lines)
> `ContextStreamClient` 클래스의 모든 API 메서드를 도메인별로 분류하여 분석한다. 각 메서드의 HTTP 메서드, 엔드포인트, 동작을 상세히 기술한다.

---

## 클래스 개요

```typescript
export class ContextStreamClient {
  constructor(private config: Config) {}
}
```

### 핵심 인프라

- **인증**: `hasEffectiveAuth()`로 유효한 API 키 또는 JWT 존재 여부를 확인한다. `getAuthOverride()`를 통해 런타임 인증 오버라이드를 지원한다.
- **기본값 관리**: `setDefaults()`로 런타임에 기본 workspace_id, project_id를 설정한다. `withDefaults()`가 모든 요청에서 자동 적용한다. 단, project_id는 workspace_id가 일치할 때만 기본값을 사용한다 (교차 워크스페이스 오염 방지).
- **UUID 유효성 검사**: `coerceUuid()`가 Zod `z.string().uuid()` 스키마로 UUID를 검증한다.
- **API 응답 언래핑**: `unwrapApiResponse<T>()`가 `{ success: boolean, data: T }` 형식의 래핑된 응답을 처리한다.
- **에러 판별**: `isBadRequestDeserialization()`가 역직렬화 에러를 감지하여 최소한의 페이로드로 재시도를 트리거한다.
- **글로벌 캐시**: `globalCache`를 통해 크레딧 잔액, 워크스페이스, 프로젝트, 세션 초기화 등의 결과를 TTL 기반으로 캐싱한다.

---

## 1. 인증 (Authentication)

| 메서드 | HTTP | 엔드포인트 | 설명 |
|--------|------|-----------|------|
| `me()` | GET | `/auth/me` | 현재 인증된 사용자 정보를 반환한다 |
| `startDeviceLogin()` | POST | `/auth/device/start` | 디바이스 로그인 플로우를 시작한다. 디바이스 코드와 인증 URL을 반환한다 |
| `pollDeviceLogin(input)` | POST | `/auth/device/token` | 디바이스 코드로 인증 완료를 폴링한다. 성공 시 JWT 토큰을 반환한다 |
| `createApiKey(input)` | POST | `/auth/api-keys` | 새 API 키를 생성한다. name, permissions, expires_at을 지정할 수 있다 |

---

## 2. 과금 (Billing / Credits)

| 메서드 | HTTP | 엔드포인트 | 설명 |
|--------|------|-----------|------|
| `getCreditBalance()` | GET | `/credits/balance` | 크레딧 잔액, 플랜 정보, 기능 목록을 반환한다. `globalCache`로 캐싱된다 (`CacheTTL.CREDIT_BALANCE`) |
| `getPlanName()` | -- | (내부적으로 getCreditBalance 호출) | `balance.plan.name`을 소문자로 반환한다. 에러 시 `null` |
| `getGraphTier()` | -- | (내부적으로 getCreditBalance 호출) | 그래프 접근 티어를 반환한다: `"none"`, `"lite"`, `"full"`. plan 필드와 plan name으로부터 다중 경로 탐색 |
| `isTeamPlan()` | -- | (내부적으로 getPlanName 호출) | 팀 플랜 여부를 반환한다. `"team"`, `"enterprise"`, `"business"` 포함 시 `true` |

---

## 3. 팀 (Team)

모든 팀 메서드는 팀 플랜 사용자만 접근 가능하다.

| 메서드 | HTTP | 엔드포인트 | 설명 |
|--------|------|-----------|------|
| `getTeamOverview()` | GET | `/team/overview` | 팀 개요 (좌석 수, 멤버, 설정) |
| `listTeamMembers(params?)` | GET | `/team/members` | 팀 멤버 목록. page, page_size 페이지네이션 지원 |
| `listTeamWorkspaces(params?)` | GET | `/team/workspaces` | 팀 전체 워크스페이스 목록. page, page_size 지원 |
| `listTeamProjects(params?)` | GET | `/team/projects` | 팀 전체 프로젝트 목록. page, page_size 지원 |

---

## 4. 워크스페이스 (Workspaces)

### 4.1 기본 CRUD

| 메서드 | HTTP | 엔드포인트 | 설명 |
|--------|------|-----------|------|
| `listWorkspaces(params?)` | GET | `/workspaces` | 워크스페이스 목록. page, page_size 페이지네이션 |
| `createWorkspace(input, options?)` | POST | `/workspaces` | 워크스페이스 생성. name, description, visibility |
| `getWorkspace(workspaceId)` | GET | `/workspaces/{id}` | 워크스페이스 상세 조회. `globalCache`로 캐싱 (`CacheTTL.WORKSPACE`) |
| `updateWorkspace(workspaceId, input)` | PUT | `/workspaces/{id}` | 워크스페이스 수정. name, description, visibility. 캐시 무효화 |
| `deleteWorkspace(workspaceId)` | DELETE | `/workspaces/{id}` | 워크스페이스 삭제. 캐시 무효화 |

### 4.2 확장 조회

| 메서드 | HTTP | 엔드포인트 | 설명 |
|--------|------|-----------|------|
| `workspaceOverview(workspaceId)` | GET | `/workspaces/{id}/overview` | 워크스페이스 개요 정보. 캐싱됨 (`CacheTTL.WORKSPACE`) |
| `workspaceAnalytics(workspaceId)` | GET | `/workspaces/{id}/analytics` | 워크스페이스 분석 데이터 |
| `workspaceContent(workspaceId)` | GET | `/workspaces/{id}/content` | 워크스페이스 콘텐츠 목록 |

### 4.3 인덱스 설정

| 메서드 | HTTP | 엔드포인트 | 설명 |
|--------|------|-----------|------|
| `getWorkspaceIndexSettings(workspaceId)` | GET | `/workspaces/{id}/index-settings` | 멀티 머신 동기화 설정 조회 |
| `updateWorkspaceIndexSettings(workspaceId, settings)` | PUT | `/workspaces/{id}/index-settings` | 인덱스 설정 업데이트. branch_policy, conflict_resolution, allowed_machines, auto_sync_enabled, max_machines |

---

## 5. 프로젝트 (Projects)

### 5.1 기본 CRUD

| 메서드 | HTTP | 엔드포인트 | 설명 |
|--------|------|-----------|------|
| `listProjects(params?)` | GET | `/projects` | 프로젝트 목록. workspace_id 필터, page, page_size |
| `createProject(input, options?)` | POST | `/projects` | 프로젝트 생성. name, description, workspace_id |
| `getProject(projectId)` | GET | `/projects/{id}` | 프로젝트 상세 조회. `globalCache`로 캐싱 (`CacheTTL.PROJECT`) |
| `updateProject(projectId, input)` | PUT | `/projects/{id}` | 프로젝트 수정. name, description. 캐시 무효화 |
| `deleteProject(projectId)` | DELETE | `/projects/{id}` | 프로젝트 삭제. 캐시 무효화 |

### 5.2 확장 조회 및 인덱싱

| 메서드 | HTTP | 엔드포인트 | 설명 |
|--------|------|-----------|------|
| `projectOverview(projectId)` | GET | `/projects/{id}/overview` | 프로젝트 개요. 캐싱됨 |
| `projectStatistics(projectId)` | GET | `/projects/{id}/statistics` | 프로젝트 통계 |
| `projectFiles(projectId)` | GET | `/projects/{id}/files` | 프로젝트 파일 목록 |
| `projectIndexStatus(projectId)` | GET | `/projects/{id}/index/status` | 인덱스 상태 (indexed_files, last_updated 등) |
| `projectIndexHistory(projectId, params?)` | GET | `/projects/{id}/index/history` | 인덱스 이력 감사 추적. machine_id, branch, since, until, path_pattern, sort_by, sort_order, page, limit 필터 |
| `indexProject(projectId)` | POST | `/projects/{id}/index` | 프로젝트 인덱싱 트리거 |
| `ingestFiles(projectId, files, options?)` | POST | `/projects/{id}/files/ingest` | 파일 배치 인덱싱. write_to_disk, overwrite, force 옵션 |
| `ingestLocal(opts)` | -- | (내부적으로 ingestFiles 호출) | 로컬 파일을 읽고 SHA-256 해시 필터링 후 배치 전송하는 고수준 메서드. cooldown/daily_limit 처리 |
| `checkIngestRecommendation(projectId, folderPath?)` | -- | (내부적으로 projectIndexStatus 호출) | 인덱싱 추천 여부 판단. not_indexed, stale, indexed, recently_indexed 상태 반환 |
| `checkAndIndexChangedFiles()` | -- | (내부적으로 readChangedFilesInBatches + ingestFiles) | 변경 파일 실시간 인덱싱. 10초 스로틀, 100파일 상한, fire-and-forget |

---

## 6. 검색 (Search)

7개 검색 메서드 모두 `POST` 방식이며, 공통 파라미터로 query, workspace_id, project_id, limit, offset, content_max_chars, context_lines, exact_match_boost, output_format을 지원한다.

| 메서드 | HTTP | 엔드포인트 | 설명 |
|--------|------|-----------|------|
| `searchSemantic(body)` | POST | `/search/semantic` | 의미론적 유사도 기반 검색. search_type="semantic" |
| `searchHybrid(body)` | POST | `/search/hybrid` | 의미론적 + 키워드 혼합 검색. search_type="hybrid" |
| `searchKeyword(body)` | POST | `/search/keyword` | 키워드 기반 검색. search_type="keyword" |
| `searchPattern(body)` | POST | `/search/pattern` | 패턴 매칭 검색. search_type="pattern" |
| `searchExhaustive(body)` | POST | `/search/exhaustive` | 전수 검색 (grep 유사). index_freshness 포함. search_type="exhaustive" |
| `searchRefactor(body)` | POST | `/search/refactor` | 리팩토링용 검색. 단어 경계 매칭, 파일별 line/col 위치 그룹핑. search_type="refactor" |
| `searchCrawl(body)` | POST | `/search/crawl` | 딥 멀티모달 검색, 더 큰 후보 풀 사용. search_type="crawl" |
| `searchSuggestions(body)` | POST | `/search/suggest` | 검색 제안 (자동완성) |

### 출력 형식

모든 검색 메서드는 `output_format` 파라미터를 지원한다:
- `"full"`: 전체 결과 (콘텐츠 포함)
- `"paths"`: 파일 경로만
- `"minimal"`: 최소 정보
- `"count"`: 매치 수만

---

## 7. Flash (명령 캐시)

Flash 시스템은 세션 간 지속되는 명령/지시 캐시이다. 모든 메서드는 session_id와 workspace_id를 요구한다.

| 메서드 | HTTP | 엔드포인트 | 설명 |
|--------|------|-----------|------|
| `flashBootstrap(params)` | POST | `/flash/bootstrap` | 세션 시작 시 캐시된 명령을 부트스트랩한다 |
| `flashGet(params)` | POST | `/flash/get` | 캐시된 명령 항목을 가져온다. limit 파라미터 지원 |
| `flashPush(params)` | POST | `/flash/push` | 새 항목을 푸시한다. entries 배열 (text, id, source, critical, surface, metadata), increment_turn, force_version_bump |
| `flashAck(params)` | POST | `/flash/ack` | 항목 수신 확인 (ACK). ids 배열 |
| `flashClear(params)` | POST | `/flash/clear` | 세션의 모든 Flash 항목을 삭제한다 |
| `flashStats(params)` | POST | `/flash/stats` | Flash 캐시 통계 |
| `flashCheckpoint(params)` | POST | `/flash/checkpoint` | 현재 상태의 체크포인트를 생성한다 |
| `flashVerify(params)` | POST | `/flash/verify` | expected_version과 비교하여 캐시 무결성을 검증한다 |

---

## 8. 메모리 & 지식 (Memory & Knowledge)

### 8.1 메모리 이벤트 (Memory Events)

| 메서드 | HTTP | 엔드포인트 | 설명 |
|--------|------|-----------|------|
| `createMemoryEvent(body)` | POST | `/memory/events` | 메모리 이벤트 생성. event_type, title, content, tags, metadata, provenance, code_refs |
| `bulkIngestEvents(body)` | POST | `/memory/events/ingest` | 벌크 이벤트 인제스트. events 배열 |
| `listMemoryEvents(params?)` | GET | `/memory/events/workspace/{workspace_id}` | 워크스페이스별 이벤트 목록. limit, project_id 필터 |
| `getMemoryEvent(eventId)` | GET | `/memory/events/{id}` | 단일 이벤트 조회 |
| `updateMemoryEvent(eventId, body)` | PUT | `/memory/events/{id}` | 이벤트 수정. title, content, metadata |
| `deleteMemoryEvent(eventId)` | DELETE | `/memory/events/{id}` | 이벤트 삭제 |
| `distillMemoryEvent(eventId)` | POST | `/memory/events/{id}/distill` | 이벤트를 지식 노드로 증류 (AI 기반 핵심 추출) |

### 8.2 지식 노드 (Knowledge Nodes)

| 메서드 | HTTP | 엔드포인트 | 설명 |
|--------|------|-----------|------|
| `createKnowledgeNode(body)` | POST | `/memory/nodes` | 지식 노드 생성. node_type (Fact/Decision/Preference/Constraint/Habit/Lesson), title(summary), content(details), relations |
| `listKnowledgeNodes(params?)` | GET | `/memory/nodes/workspace/{workspace_id}` | 워크스페이스별 노드 목록 |
| `getKnowledgeNode(nodeId)` | GET | `/memory/nodes/{id}` | 단일 노드 조회 |
| `updateKnowledgeNode(nodeId, body)` | PUT | `/memory/nodes/{id}` | 노드 수정. title->summary, content->details, relations->context.relations 매핑 |
| `deleteKnowledgeNode(nodeId)` | DELETE | `/memory/nodes/{id}` | 노드 삭제 |
| `supersedeKnowledgeNode(nodeId, body)` | POST | `/memory/nodes/{id}/supersede` | 기존 노드를 새 콘텐츠로 대체 (새 노드 생성 -> 기존 노드 supersede 표시). new_content, reason |

### 8.3 메모리 검색/분석

| 메서드 | HTTP | 엔드포인트 | 설명 |
|--------|------|-----------|------|
| `memorySearch(body)` | POST | `/memory/search` | 메모리 검색. query, workspace_id, project_id, limit |
| `memoryDecisions(params?)` | GET | `/memory/search/decisions` | 의사결정 검색. workspace_id, project_id, category, limit |
| `memoryTimeline(workspaceId)` | GET | `/memory/search/timeline/{workspace_id}` | 워크스페이스 메모리 타임라인 |
| `memorySummary(workspaceId)` | GET | `/memory/search/summary/{workspace_id}` | 워크스페이스 메모리 요약 |

---

## 9. 그래프 (Graph)

### 9.1 지식 그래프

| 메서드 | HTTP | 엔드포인트 | 설명 |
|--------|------|-----------|------|
| `graphRelated(body)` | POST | `/graph/knowledge/related` | 관련 노드 탐색. node_id, relation_types, max_depth |
| `graphPath(body)` | POST | `/graph/knowledge/path` | 두 노드 간 경로 탐색. from(source_id), to(target_id), max_depth |
| `graphDecisions(body?)` | POST | `/graph/knowledge/decisions` | 의사결정 그래프 조회. category (기본 "general"), from/to 날짜 범위 (기본 최근 5년) |
| `findContradictions(nodeId)` | GET | `/graph/knowledge/contradictions/{id}` | 특정 노드와 모순되는 지식 탐지 |

### 9.2 코드 그래프

| 메서드 | HTTP | 엔드포인트 | 설명 |
|--------|------|-----------|------|
| `graphDependencies(body)` | POST | `/graph/dependencies` | 의존성 분석. target.type (module/function/type/variable) 정규화, max_depth, include_transitive |
| `graphCallPath(body)` | POST | `/graph/call-paths` | 두 함수 간 호출 경로. from_function_id, to_function_id, max_depth |
| `graphImpact(body)` | POST | `/graph/impact-analysis` | 변경 영향 분석. change_type (기본 "modify_signature"), target_id, element_name |
| `graphIngest(body)` | POST | `/graph/ingest/{project_id}` | 프로젝트 코드 그래프 구축. wait 옵션 (동기/비동기) |
| `graphUsages(body)` | POST | `/graph/usages` | 심볼 사용처 검색. target_id, target_type (기본 "function"), project_id |
| `findCircularDependencies(projectId)` | GET | `/graph/circular-dependencies/{id}` | 순환 의존성 탐지 |
| `findUnusedCode(projectId)` | GET | `/graph/unused-code/{id}` | 미사용 코드 탐지 |

---

## 10. AI

| 메서드 | HTTP | 엔드포인트 | 설명 |
|--------|------|-----------|------|
| `aiContext(body)` | POST | `/ai/context` | AI 기반 컨텍스트 검색. query, project_id, include_code, include_docs, include_memory, limit. 역직렬화 에러 시 최소 페이로드로 재시도 |
| `aiEmbeddings(body)` | POST | `/ai/embeddings` | 텍스트 임베딩 생성. text |
| `aiPlan(body)` | POST | `/ai/plan/generate` | AI 구현 계획 생성. description, project_id, complexity. 타임아웃 50초, 재시도 0회 |
| `aiTasks(body)` | POST | `/ai/tasks/generate` | AI 태스크 분해. description, project_id, granularity (low/medium/high, coarse/fine 등 매핑). plan_id 미지원 |
| `aiEnhancedContext(body)` | POST | `/ai/context/enhanced` | AI 강화 컨텍스트. aiContext와 동일 파라미터, 향상된 결과. 역직렬화 에러 시 재시도 |

### AI 요청 빌더 (내부)

- `buildAiContextRequest()`: max_tokens, token_budget, token_soft_limit, include_dependencies, include_tests, max_sections (1-20 범위 제한) 처리.
- `buildAiPlanRequest()`: requirements(또는 description), project_id, max_steps, context, constraints 처리.
- `buildAiTasksRequest()`: plan(또는 description), project_id, granularity 정규화, max_tasks, include_estimates 처리.

---

## 11. 세션 & 컨텍스트 (Session & Context)

### 11.1 세션 초기화

| 메서드 | HTTP | 엔드포인트 | 설명 |
|--------|------|-----------|------|
| `initSession(params, ideRoots)` | 복합 | 다수 엔드포인트 조합 | 대화 세션 초기화. 복잡한 디스커버리 체인을 수행한다 (아래 상세 설명) |
| `_fetchSessionContextBatched(params)` | POST | `/session/init` | 배치 세션 컨텍스트 로드 (단일 API 호출). workspace, project, recent_memory, recent_decisions, relevant_context 반환. 캐싱됨 (`CacheTTL.SESSION_INIT`) |
| `_fetchSessionContextFallback(context, ...)` | 복합 | 다수 | 배치 엔드포인트 실패 시 개별 API 호출 폴백. `Promise.all`로 병렬 실행 (workspaceOverview, projectOverview, listMemoryEvents, memoryDecisions, memorySearch, getHighPriorityLessons) |

**initSession 디스커버리 체인:**

1. **Step 1: 워크스페이스 디스커버리**
   - 로컬 `.contextstream/config.json` 확인
   - 부모 폴더 히스토리 매핑 (`~/.contextstream-mappings.json`) 확인
   - API로 워크스페이스 목록 조회 + 폴더명 매칭 (정확/부분/프로젝트명 매칭)
   - 매칭 실패 시: `"requires_workspace_selection"` 상태로 후보 목록 반환
   - 워크스페이스 미존재 시: `"requires_workspace_name"` 상태 반환
2. **Step 2: 프로젝트 디스커버리**
   - 멀티 프로젝트 폴더 자동 감지 (`isMultiProjectFolder`)
   - 기존 프로젝트 매칭 또는 자동 생성
   - 인덱스 상태 확인 및 자동 인덱싱 (background fire-and-forget)
3. **Step 3: 컨텍스트 로드**
   - `_fetchSessionContextBatched`로 배치 로드 (실패 시 폴백)
   - 고우선순위 레슨 로드 (`getHighPriorityLessons`)
   - 사용자 기억 항목 로드 (`getHighPriorityRememberItems`)
4. **Step 4: 기존 프로젝트 인덱스 상태 확인**
   - 1시간 이상 오래된 인덱스는 자동 갱신 (증분 또는 전체)

### 11.2 워크스페이스 연결

| 메서드 | HTTP | 엔드포인트 | 설명 |
|--------|------|-----------|------|
| `associateWorkspace(params)` | -- | (로컬 파일 작업) | 폴더를 워크스페이스에 연결. `.contextstream/config.json`에 저장. 부모 폴더 매핑 선택적 생성 (`addGlobalMapping`). version, configured_editors, context_pack, api_url 메타데이터 지원 |

### 11.3 사용자 컨텍스트

| 메서드 | HTTP | 엔드포인트 | 설명 |
|--------|------|-----------|------|
| `getUserContext(params)` | -- | (내부적으로 memorySearch + memorySummary) | 사용자 선호도 및 코딩 스타일 정보. preferences (메모리 검색) + summary (메모리 요약) |

### 11.4 컨텍스트 캡처

| 메서드 | HTTP | 엔드포인트 | 설명 |
|--------|------|-----------|------|
| `captureContext(params)` | POST | `/memory/events` | 대화 컨텍스트 자동 캡처. event_type 매핑: conversation->chat, decision->decision, task->task, plan->plan, bug/feature->ticket, correction/lesson/warning/frustration->manual_note+lesson_system 태그. importance (low/medium/high/critical) |
| `captureMemoryEvent(params)` | POST | `/memory/events` | 직접 event_type으로 메모리 이벤트 캡처. 자동 저장 세션 스냅샷 등 시스템 이벤트용 |

### 11.5 스마트 검색 및 컨텍스트

| 메서드 | HTTP | 엔드포인트 | 설명 |
|--------|------|-----------|------|
| `smartSearch(params)` | -- | (내부적으로 memorySearch + searchSemantic + memoryDecisions) | 자동 컨텍스트 보강 검색. memory_results + code_results + related_decisions 반환 |
| `getContextDelta(params)` | -- | (내부적으로 listMemoryEvents) | 특정 타임스탬프 이후 변경된 컨텍스트. new_decisions, new_memory, items 반환 |
| `getContextSummary(params)` | -- | (내부적으로 getWorkspace + getProject + memoryDecisions + memorySearch + memorySummary) | 토큰 효율적 워크스페이스 요약. 목표 ~500 토큰. workspace_name, project_name, decision_count, memory_count |
| `getContextWithBudget(params)` | -- | (내부적으로 memoryDecisions + memorySearch + searchSemantic) | 토큰 예산 내 최적 컨텍스트. max_tokens 기반 우선순위 할당: decisions(40%) -> memory(70%) -> code(나머지) |
| `getSmartContext(params)` | POST | `/context/smart` | 핵심 컨텍스트 도구. 사용자 메시지 분석 후 관련 컨텍스트를 토큰 효율적 형식으로 반환. format: minified/readable/structured, mode: standard/pack. context_pressure 추적, semantic_intent 분류, save_exchange 지원. API 실패 시 로컬 폴백 (키워드 추출 + 관련도 순위) |
| `compressChat(params)` | -- | (내부적으로 captureContext 반복) | 채팅 히스토리를 구조화된 메모리로 압축. 패턴 기반 추출: decisions, preferences, insights, tasks, code_patterns. 각 카테고리 최대 5개 |

### 11.6 피드백 및 추적

| 메서드 | HTTP | 엔드포인트 | 설명 |
|--------|------|-----------|------|
| `submitContextFeedback(body)` | POST | `/context/smart/feedback` | 컨텍스트 피드백 제출. item_id, item_type (memory_event/knowledge_node/code_chunk), feedback_type (relevant/irrelevant/pin) |
| `decisionTrace(body)` | POST | `/memory/search/decisions/trace` | 의사결정 추적. query, include_impact |
| `sessionRemember(params)` | POST | `/session/remember` | 간단한 기억 인터페이스. content, importance, await_indexing, tags |
| `trackTokenSavings(body)` | POST | `/analytics/token-savings` | 토큰 절약 이벤트 기록. candidate_chars, context_chars, max_tokens |

### 11.7 고우선순위 항목

| 메서드 | HTTP | 엔드포인트 | 설명 |
|--------|------|-----------|------|
| `getHighPriorityLessons(params)` | -- | (내부적으로 memorySearch) | 높은 심각도(critical/high) 교훈 검색. lesson/lesson_system 태그 + severity 태그 필터링. title, severity, category, prevention 반환 |
| `getHighPriorityRememberItems(params)` | -- | (내부적으로 memorySearch) | 사용자 지정 기억 항목 검색. user_remember/always_surface 태그 필터링. content, importance, created_at 반환 |

---

## 12. 통합 (Integrations)

### 12.1 통합 상태

| 메서드 | HTTP | 엔드포인트 | 설명 |
|--------|------|-----------|------|
| `integrationsStatus(params)` | GET | `/integrations/workspaces/{workspace_id}/integrations/status` | 모든 프로바이더 통합 상태. provider, status, last_sync_at, next_sync_at, error_message, resources_synced |

### 12.2 Slack 통합 (10개 메서드)

| 메서드 | HTTP | 엔드포인트 | 설명 |
|--------|------|-----------|------|
| `slackStats(params)` | GET | `/integrations/workspaces/{wid}/slack/stats` | Slack 통계 개요. summary (total_messages, total_threads, active_users, channels_synced), channels, activity, sync_status |
| `slackUsers(params)` | GET | `/integrations/workspaces/{wid}/slack/users` | Slack 사용자 목록. page, per_page 페이지네이션 |
| `slackChannels(params)` | GET | `/integrations/workspaces/{wid}/slack/channels` | 채널 목록 및 통계 (message_count, thread_count, last_message_at) |
| `slackActivity(params)` | GET | `/integrations/workspaces/{wid}/slack/activity` | 최근 활동 피드. limit, offset, channel_id 필터 |
| `slackDiscussions(params)` | GET | `/integrations/workspaces/{wid}/slack/discussions` | 고참여 토론 스레드. reply_count, reaction_count, participant_count |
| `slackContributors(params)` | GET | `/integrations/workspaces/{wid}/slack/contributors` | 상위 기여자 목록. message_count, last_message_at |
| `slackSyncUsers(params)` | POST | `/integrations/workspaces/{wid}/slack/sync-users` | 사용자 프로필 동기화 트리거. synced_users, auto_mapped 반환 |
| `slackSearch(params)` | GET | `/integrations/workspaces/{wid}/slack/search` | 메시지 검색. q (쿼리), limit |
| `slackKnowledge(params)` | GET | `/integrations/workspaces/{wid}/slack/knowledge` | Slack에서 추출된 지식. node_type 필터. id, node_type, title, summary, confidence, source_type, occurred_at, tags |
| `slackSummary(params)` | GET | `/slack/summary` | Slack 요약. days, channel 필터 |

### 12.3 GitHub 통합 (8개 메서드)

| 메서드 | HTTP | 엔드포인트 | 설명 |
|--------|------|-----------|------|
| `githubStats(params)` | GET | `/integrations/workspaces/{wid}/github/stats` | GitHub 통계 개요. summary (total_issues, total_prs, total_releases, total_comments, repos_synced, contributors), repos, activity, sync_status |
| `githubRepos(params)` | GET | `/integrations/workspaces/{wid}/github/repos` | 리포지토리 통계 목록 (issue_count, pr_count, release_count, comment_count) |
| `githubActivity(params)` | GET | `/integrations/workspaces/{wid}/github/activity` | 최근 활동 피드. limit, offset, repo, type 필터 |
| `githubIssues(params)` | GET | `/integrations/workspaces/{wid}/github/issues` | 이슈/PR 목록. limit, offset, state, repo 필터 |
| `githubContributors(params)` | GET | `/integrations/workspaces/{wid}/github/contributors` | 상위 기여자. username, contribution_count, avatar_url |
| `githubSearch(params)` | GET | `/integrations/workspaces/{wid}/github/search` | GitHub 콘텐츠 검색. q, limit. items + total 반환 |
| `githubKnowledge(params)` | GET | `/integrations/workspaces/{wid}/github/knowledge` | GitHub에서 추출된 지식. limit, node_type 필터 |
| `githubSummary(params)` | GET | `/github/summary` | GitHub 요약. days, repo 필터 |

### 12.4 Notion 통합 (11개 메서드)

| 메서드 | HTTP | 엔드포인트 | 설명 |
|--------|------|-----------|------|
| `createNotionPage(params)` | POST | `/integrations/notion/pages` | 페이지 생성. title, content, parent_database_id, parent_page_id. workspace_id는 쿼리 파라미터 |
| `notionStats(params)` | GET | `/integrations/workspaces/{wid}/notion/stats` | Notion 통계 개요. summary (total_pages, total_databases, synced_pages), databases |
| `notionActivity(params)` | GET | `/integrations/workspaces/{wid}/notion/activity` | 최근 활동 피드. limit, database_id 필터 |
| `notionKnowledge(params)` | GET | `/integrations/workspaces/{wid}/notion/knowledge` | Notion에서 추출된 지식. limit, node_type 필터 |
| `notionSummary(params)` | GET | `/integrations/workspaces/{wid}/notion/summary` | Notion 요약. days, database_id 필터. period, stats, highlights 반환 |
| `notionListDatabases(params)` | GET | `/integrations/workspaces/{wid}/notion/databases` | 데이터베이스 목록. id, title, description, icon, url, page_count |
| `notionCreateDatabase(params)` | POST | `/integrations/notion/databases` | 데이터베이스 생성. title, parent_page_id, description |
| `notionSearchPages(params)` | GET | `/integrations/workspaces/{wid}/notion/pages` | 페이지 검색. query, database_id, limit, event_type, status, priority, has_due_date, tags 필터 |
| `notionGetPage(params)` | GET | `/integrations/workspaces/{wid}/notion/pages/{page_id}` | 특정 페이지 조회 (콘텐츠, 속성 포함) |
| `notionQueryDatabase(params)` | POST | `/integrations/workspaces/{wid}/notion/databases/{db_id}/query` | 데이터베이스 쿼리. filter, sorts, limit (page_size). has_more, next_cursor 페이지네이션 |
| `notionUpdatePage(params)` | PATCH | `/integrations/workspaces/{wid}/notion/pages/{page_id}` | 페이지 업데이트. title, content, properties |

### 12.5 교차 통합 (Cross-Integration, 4개 메서드)

| 메서드 | HTTP | 엔드포인트 | 설명 |
|--------|------|-----------|------|
| `integrationsSearch(params)` | GET | `/integrations/search` | 교차 소스 검색. query, sources (배열), days, sort_by, limit |
| `integrationsSummary(params)` | GET | `/integrations/summary` | 교차 소스 요약. workspace_id, days |
| `integrationsKnowledge(params)` | GET | `/integrations/knowledge` | 교차 소스 지식. knowledge_type, query, sources, limit |
| `integrationsStatus(params)` | GET | `/integrations/workspaces/{wid}/integrations/status` | (12.1 참조) |

---

## 13. 기타 도메인 (Miscellaneous)

### 13.1 리마인더 (Reminders, 7개 메서드)

| 메서드 | HTTP | 엔드포인트 | 설명 |
|--------|------|-----------|------|
| `remindersList(params?)` | GET | `/reminders` | 리마인더 목록. workspace_id, project_id, status, priority, limit 필터 |
| `remindersActive(params?)` | GET | `/reminders/active` | 활성 리마인더 (대기, 만료, 임박). context 파라미터로 관련성 필터링. overdue_count 포함 |
| `remindersCreate(params)` | POST | `/reminders` | 리마인더 생성. title, content, remind_at, priority, keywords, recurrence, memory_event_id |
| `remindersSnooze(params)` | POST | `/reminders/{id}/snooze` | 리마인더 스누즈. until (날짜/시간) |
| `remindersComplete(params)` | POST | `/reminders/{id}/complete` | 리마인더 완료 표시 |
| `remindersDismiss(params)` | POST | `/reminders/{id}/dismiss` | 리마인더 무시 |
| `remindersDelete(params)` | DELETE | `/reminders/{id}` | 리마인더 삭제 |

### 13.2 계획 (Plans, 6개 메서드)

| 메서드 | HTTP | 엔드포인트 | 설명 |
|--------|------|-----------|------|
| `createPlan(params)` | POST | `/plans` | 구현 계획 생성. title, content, description, goals, steps (id, title, description, order, estimated_effort), status (draft/active/completed/archived/abandoned), tags, due_at, source_tool, is_personal |
| `listPlans(params?)` | GET | `/plans` | 계획 목록. workspace_id, project_id, status, is_personal, limit, offset 필터 |
| `getPlan(params)` | GET | `/plans/{id}` | 계획 조회. include_tasks (기본 true) |
| `updatePlan(params)` | PATCH | `/plans/{id}` | 계획 수정. title, content, goals, steps, status, tags, due_at |
| `deletePlan(params)` | DELETE | `/plans/{id}` | 계획 삭제 |
| `getPlanTasks(params)` | GET | `/plans/{id}/tasks` | 계획 내 태스크 목록 |

### 13.3 태스크 (Tasks, 7개 메서드)

| 메서드 | HTTP | 엔드포인트 | 설명 |
|--------|------|-----------|------|
| `createPlanTask(params)` | POST | `/plans/{plan_id}/tasks` | 계획 내 태스크 생성. title, content, status (pending/in_progress/completed/blocked/cancelled), priority (low/medium/high/urgent), order, plan_step_id, code_refs, tags |
| `reorderPlanTasks(params)` | PATCH | `/plans/{plan_id}/tasks/reorder` | 계획 내 태스크 재정렬. task_ids 배열 |
| `createTask(params)` | POST | `/tasks` | 독립 태스크 생성 (선택적 plan_id 연결). workspace_id 필수. is_personal 지원 |
| `listTasks(params?)` | GET | `/tasks` | 태스크 목록. workspace_id, project_id, plan_id, status, priority, is_personal, limit, offset 필터 |
| `getTask(params)` | GET | `/tasks/{id}` | 태스크 조회 |
| `updateTask(params)` | PATCH | `/tasks/{id}` | 태스크 수정. plan_id (null 설정으로 연결 해제 가능), blocked_reason |
| `deleteTask(params)` | DELETE | `/tasks/{id}` | 태스크 삭제 |

### 13.4 할일 (Todos, 7개 메서드)

| 메서드 | HTTP | 엔드포인트 | 설명 |
|--------|------|-----------|------|
| `todosList(params?)` | GET | `/todos` | 할일 목록. workspace_id, project_id, status (pending/completed), priority (low/medium/high/urgent), is_personal, page, per_page |
| `todosCreate(params)` | POST | `/todos` | 할일 생성. title, description, priority, due_at, is_personal |
| `todosGet(params)` | GET | `/todos/{id}` | 할일 조회 |
| `todosUpdate(params)` | PATCH | `/todos/{id}` | 할일 수정. title, description, priority, due_at, completed, status |
| `todosDelete(params)` | DELETE | `/todos/{id}` | 할일 삭제 |
| `todosComplete(params)` | PATCH | `/todos/{id}` | 할일 완료 표시 (내부적으로 todosUpdate 호출, completed=true, status="completed") |
| `todosIncomplete(params)` | PATCH | `/todos/{id}` | 할일 미완료로 되돌리기 (내부적으로 todosUpdate 호출, completed=false, status="pending") |

### 13.5 다이어그램 (Diagrams, 5개 메서드)

| 메서드 | HTTP | 엔드포인트 | 설명 |
|--------|------|-----------|------|
| `diagramsList(params?)` | GET | `/diagrams` | 다이어그램 목록. workspace_id, project_id, diagram_type (flowchart/sequence/class/er/gantt/mindmap/pie/other), is_personal, page, per_page |
| `diagramsCreate(params)` | POST | `/diagrams` | 다이어그램 생성. title, diagram_type, content (Mermaid 문법), metadata, is_personal |
| `diagramsGet(params)` | GET | `/diagrams/{id}` | 다이어그램 조회 |
| `diagramsUpdate(params)` | PATCH | `/diagrams/{id}` | 다이어그램 수정. title, diagram_type, content, metadata |
| `diagramsDelete(params)` | DELETE | `/diagrams/{id}` | 다이어그램 삭제 |

### 13.6 문서 (Docs, 6개 메서드)

| 메서드 | HTTP | 엔드포인트 | 설명 |
|--------|------|-----------|------|
| `docsList(params?)` | GET | `/docs` | 문서 목록. workspace_id, project_id, doc_type (roadmap/spec/general), is_personal, page, per_page |
| `docsCreate(params)` | POST | `/docs` | 문서 생성. title, content, doc_type, metadata, is_personal |
| `docsCreateRoadmap(params)` | POST | `/docs/roadmap` | 로드맵 문서 생성 (템플릿). title, milestones (title, description, target_date, status), is_personal |
| `docsGet(params)` | GET | `/docs/{id}` | 문서 조회 |
| `docsUpdate(params)` | PATCH | `/docs/{id}` | 문서 수정. title, content, doc_type, metadata |
| `docsDelete(params)` | DELETE | `/docs/{id}` | 문서 삭제 |

### 13.7 트랜스크립트 (Transcripts, 4개 메서드)

| 메서드 | HTTP | 엔드포인트 | 설명 |
|--------|------|-----------|------|
| `listTranscripts(params?)` | GET | `/transcripts` | 트랜스크립트 목록. workspace_id, project_id, session_id, client_name, started_after, started_before, limit, page, per_page |
| `getTranscript(transcript_id)` | GET | `/transcripts/{id}` | 트랜스크립트 조회 |
| `searchTranscripts(params)` | GET | `/transcripts/search` | 트랜스크립트 검색. query, workspace_id, limit |
| `deleteTranscript(transcript_id)` | DELETE | `/transcripts/{id}` | 트랜스크립트 삭제 |

### 13.8 미디어 (Media/Content, 7개 메서드)

| 메서드 | HTTP | 엔드포인트 | 설명 |
|--------|------|-----------|------|
| `mediaInitUpload(params)` | POST | `/workspaces/{wid}/content/uploads/init` | 미디어 업로드 초기화. filename, size_bytes, content_type (video/audio/image/document/text/code/other), mime_type, title, tags. presigned URL, headers, 만료 시간 반환 |
| `mediaCompleteUpload(params)` | POST | `/workspaces/{wid}/content/{id}/complete-upload` | 업로드 완료 및 인덱싱 트리거 |
| `mediaGetContent(params)` | GET | `/workspaces/{wid}/content/{id}` | 콘텐츠 상태 조회 (인덱싱 진행률, 에러, 메타데이터 포함) |
| `mediaListContent(params)` | GET | `/workspaces/{wid}/content` | 콘텐츠 목록. content_type, status, limit, offset 필터 |
| `mediaSearchContent(params)` | GET | `/workspaces/{wid}/content/search` | 시맨틱 콘텐츠 검색 (트랜스크립트, 설명 등). query, content_type, limit, offset. score, match_type, match_text, timestamp_start/end 반환 |
| `mediaGetClip(params)` | GET | `/workspaces/{wid}/content/{id}/clip` | 인덱싱된 콘텐츠에서 클립/세그먼트 추출. start_time, end_time, format (json/remotion/ffmpeg). transcript, keyframes, remotion_props, ffmpeg_command 반환 |
| `mediaDeleteContent(params)` | DELETE | `/workspaces/{wid}/content/{id}` | 콘텐츠 삭제 |

### 13.9 추천 규칙 (Suggested Rules, 4개 메서드)

| 메서드 | HTTP | 엔드포인트 | 설명 |
|--------|------|-----------|------|
| `listSuggestedRules(params?)` | GET | `/suggested-rules` | 추천 규칙 목록. workspace_id, status (pending/accepted/rejected/modified), source_type (global/user/pattern), min_confidence, limit, offset |
| `getSuggestedRulesPendingCount(params?)` | GET | `/suggested-rules/pending-count` | 대기 중 추천 규칙 수 |
| `getSuggestedRulesStats(params?)` | GET | `/suggested-rules/stats` | 추천 규칙 피드백 통계 |
| `suggestedRuleAction(params)` | POST | `/suggested-rules/{id}/action` | 추천 규칙에 대한 작업 수행. action (accept/reject/modify), modified_keywords, modified_instruction |

---

## 메서드 총 수 요약

| 도메인 | 메서드 수 |
|--------|----------|
| 인증 | 4 |
| 과금 | 4 |
| 팀 | 4 |
| 워크스페이스 | 9 |
| 프로젝트 | 12 |
| 검색 | 8 |
| Flash | 8 |
| 메모리 이벤트 | 7 |
| 지식 노드 | 6 |
| 메모리 검색/분석 | 4 |
| 그래프 (지식) | 4 |
| 그래프 (코드) | 7 |
| AI | 5 |
| 세션 초기화 | 3 |
| 컨텍스트 캡처/검색 | 11 |
| 피드백/추적 | 4 |
| 고우선순위 항목 | 2 |
| 통합 상태 | 1 |
| Slack | 10 |
| GitHub | 8 |
| Notion | 11 |
| 교차 통합 | 4 |
| 리마인더 | 7 |
| 계획 | 6 |
| 태스크 | 7 |
| 할일 | 7 |
| 다이어그램 | 5 |
| 문서 | 6 |
| 트랜스크립트 | 4 |
| 미디어 | 7 |
| 추천 규칙 | 4 |
| **총계** | **~176** |

---

## 인프라 참고사항

### 캐싱 전략

캐시 대상과 TTL:
- `CacheKeys.creditBalance()` -> `CacheTTL.CREDIT_BALANCE`
- `CacheKeys.workspace(id)` -> `CacheTTL.WORKSPACE`
- `CacheKeys.project(id)` -> `CacheTTL.PROJECT`
- `CacheKeys.sessionInit(workspaceId, projectId?)` -> `CacheTTL.SESSION_INIT`
- `workspace_overview:{id}` -> `CacheTTL.WORKSPACE`
- `project_overview:{id}` -> `CacheTTL.PROJECT`

캐시 무효화: `updateWorkspace`, `deleteWorkspace`, `updateProject`, `deleteProject` 호출 시 관련 캐시 항목을 즉시 삭제한다.

### HTTP 요청 래퍼

모든 API 호출은 `request(config, path, options)` 함수를 통해 이루어진다. 옵션:
- `method`: HTTP 메서드 (기본 POST for body, GET otherwise)
- `body`: 요청 본문 (자동 JSON 직렬화)
- `timeoutMs`: 요청 타임아웃 (기본값은 서버 설정, aiPlan은 50초)
- `retries`: 재시도 횟수 (기본값은 서버 설정, aiPlan은 0회)
- `workspaceId`: 일부 AI 엔드포인트에서 별도 전달

### 역직렬화 에러 재시도

`aiContext`, `aiPlan`, `aiTasks`, `aiEnhancedContext`는 API가 `BAD_REQUEST` + `"deserialize"` 에러를 반환할 때 최소한의 페이로드(필수 필드만)로 자동 재시도한다. 이는 API 서버의 스키마 불일치 문제에 대한 방어적 처리이다.

### 멀티 프로젝트 폴더 감지

`isMultiProjectFolder(rootPath)` 함수가 상위 폴더에서 여러 프로젝트를 자동 감지한다:
- `.git`, `package.json`, `Cargo.toml`, `pyproject.toml`, `go.mod`, `pom.xml`, `build.gradle`, `Gemfile`, `composer.json`, `.contextstream` 마커 확인
- 루트에 `.git`이 없고 1개 이상의 프로젝트 하위 디렉토리가 있으면 멀티 프로젝트
- 루트에 `.git`이 있어도 2개 이상의 프로젝트 하위 디렉토리가 있으면 멀티 프로젝트 (모노레포)
