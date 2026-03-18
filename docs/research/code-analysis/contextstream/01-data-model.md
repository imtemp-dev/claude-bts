# ContextStream -- 데이터 모델 분석

> **분석 대상**: `~/Workspace/context-sync-research/contextstream/` (TypeScript MCP 서버)
> **핵심 특성**: 로컬 데이터베이스 없음. 모든 데이터는 `https://api.contextstream.io` REST API를 통해 클라우드에 저장된다.

---

## 목차

1. [클라우드 API 연결](#1-클라우드-api-연결)
2. [핵심 데이터 엔티티](#2-핵심-데이터-엔티티)
3. [타입 정의](#3-타입-정의)
4. [캐시 시스템](#4-캐시-시스템)
5. [HTTP 클라이언트](#5-http-클라이언트)
6. [파일 인덱싱](#6-파일-인덱싱)
7. [자격증명 저장](#7-자격증명-저장)
8. [워크스페이스 설정](#8-워크스페이스-설정)

---

## 1. 클라우드 API 연결

### 1.1 API 기본 URL 및 경로 구조

`config.ts`에서 기본 API URL이 정의된다.

```typescript
const DEFAULT_API_URL = "https://api.contextstream.io";
```

`http.ts`의 `request()` 함수에서 모든 요청 경로에 `/api/v1` prefix가 자동으로 부여된다. 경로가 이미 `/api/`로 시작하면 prefix를 추가하지 않는다.

```typescript
const rawPath = path.startsWith("/") ? path : `/${path}`;
const apiPath = rawPath.startsWith("/api/") ? rawPath : `/api/v1${rawPath}`;
```

따라서 클라이언트 코드에서 `/workspaces`를 호출하면 실제 요청 URL은 `https://api.contextstream.io/api/v1/workspaces`가 된다.

### 1.2 인증 방식

두 가지 인증 방식을 지원한다.

**API Key 인증**: `X-API-Key` 헤더를 사용한다. 환경변수 `CONTEXTSTREAM_API_KEY`로 설정한다.

```typescript
if (apiKey) headers["X-API-Key"] = apiKey;
```

**JWT 인증**: `Authorization: Bearer <token>` 헤더를 사용한다. 환경변수 `CONTEXTSTREAM_JWT`로 설정한다.

```typescript
if (jwt) headers["Authorization"] = `Bearer ${jwt}`;
```

두 인증 방식 모두 `config.ts`의 `loadConfig()` 함수에서 Zod 스키마로 검증된다. API Key와 JWT가 모두 없으면 `CONTEXTSTREAM_ALLOW_HEADER_AUTH=true`가 아닌 이상 오류를 발생시킨다.

```typescript
if (!parsed.data.apiKey && !parsed.data.jwt && !parsed.data.allowHeaderAuth) {
  throw new Error(MISSING_CREDENTIALS_ERROR);
}
```

또한 `auth-context.ts`의 `getAuthOverride()`를 통해 런타임에 인증 정보를 동적으로 재정의할 수 있다. 이 override는 API Key, JWT, workspace ID를 포함할 수 있으며, 설정된 경우 환경변수 기반 인증보다 우선한다.

인증이 필요 없는 엔드포인트도 존재한다.

```typescript
const unauthenticatedEndpoints = ["/api/v1/auth/device/start", "/api/v1/auth/device/token"];
```

이 엔드포인트는 Device Flow 기반 로그인에 사용되며, API Key나 JWT 없이 접근 가능하다.

### 1.3 재시도 로직

`http.ts`에서 재시도 가능한 HTTP 상태 코드와 최대 재시도 횟수가 정의된다.

```typescript
const RETRYABLE_STATUSES = new Set([408, 429, 500, 502, 503, 504]);
const MAX_RETRIES = 3;
const BASE_DELAY = 1000;
```

재시도 대상 상태 코드의 의미는 다음과 같다.

| 상태 코드 | 내부 코드 | 의미 |
|-----------|----------|------|
| 408 | `REQUEST_TIMEOUT` | 요청 타임아웃 |
| 429 | `RATE_LIMITED` | 속도 제한 초과 |
| 500 | `INTERNAL_ERROR` | 서버 내부 오류 |
| 502 | `BAD_GATEWAY` | 게이트웨이 오류 |
| 503 | `SERVICE_UNAVAILABLE` | 서비스 불가 |
| 504 | `GATEWAY_TIMEOUT` | 게이트웨이 타임아웃 |

재시도 간격은 **지수 백오프(exponential backoff)** 방식을 사용한다. 서버가 `Retry-After` 헤더를 반환하면 해당 값을 우선 사용한다.

```typescript
const retryAfter = response.headers.get("retry-after");
const delay = retryAfter
  ? parseInt(retryAfter, 10) * 1000
  : baseDelay * Math.pow(2, attempt);
```

네트워크 오류(fetch 자체 실패)도 재시도 대상이다. 이 경우 `HttpError` status 0으로 처리된다.

### 1.4 타임아웃

기본 타임아웃은 **180초(3분)**이다.

```typescript
const timeoutMs =
  typeof options.timeoutMs === "number" && options.timeoutMs > 0 ? options.timeoutMs : 180_000;
```

`AbortController`를 사용하여 타임아웃을 구현하며, 사용자가 전달한 `signal`과 타임아웃 signal을 결합한다. AI Plan 관련 요청은 별도로 50초 타임아웃(`AI_PLAN_TIMEOUT_MS = 50_000`)과 재시도 0회가 적용된다.

---

## 2. 핵심 데이터 엔티티

ContextStream은 로컬 데이터베이스를 사용하지 않으며, 모든 엔티티가 클라우드 API에 저장된다. `client.ts`의 `ContextStreamClient` 클래스가 각 엔티티에 대한 CRUD 메서드를 제공한다.

### 2.1 Workspaces

워크스페이스는 프로젝트, 메모리, 통합 등을 묶는 최상위 조직 단위이다.

| 필드 | 타입 | 설명 |
|------|------|------|
| `id` | `string` (UUID) | 고유 식별자 |
| `name` | `string` | 워크스페이스 이름 |
| `description` | `string?` | 설명 (선택) |
| `visibility` | `string?` | 공개 범위 (선택) |

주요 API 메서드:

- `listWorkspaces(params?)` -- `GET /workspaces` (페이지네이션 지원)
- `createWorkspace(input)` -- `POST /workspaces`
- `updateWorkspace(workspaceId, input)` -- `PUT /workspaces/:id` (캐시 무효화 포함)
- `deleteWorkspace(workspaceId)` -- `DELETE /workspaces/:id`

워크스페이스 ID는 모든 하위 엔티티 요청에서 `X-Workspace-Id` 헤더로 전달되어 workspace-pooled rate limiting에 활용된다. 워크스페이스 해석(resolution)은 다음 우선순위 체인을 따른다.

1. 요청 body의 `workspace_id`
2. URL 경로에서 추출한 workspace ID
3. 쿼리 파라미터의 `workspace_id`
4. `config.defaultWorkspaceId`

### 2.2 Projects

프로젝트는 워크스페이스 내에서 코드베이스 단위로 구분되는 엔티티이다.

| 필드 | 타입 | 설명 |
|------|------|------|
| `id` | `string` (UUID) | 고유 식별자 |
| `name` | `string` | 프로젝트 이름 |
| `description` | `string?` | 설명 (선택) |
| `workspace_id` | `string` (UUID) | 소속 워크스페이스 |
| 인덱싱 상태 | `string` | `started`, `completed` 등 |

주요 API 메서드:

- `listProjects(params?)` -- `GET /projects` (workspace_id로 필터링)
- `createProject(input)` -- `POST /projects`
- `updateProject(projectId, input)` -- `PUT /projects/:id`
- `deleteProject(projectId)` -- `DELETE /projects/:id`
- `indexProject(projectId)` -- `POST /projects/:id/index`

프로젝트는 코드 인덱싱 대상이며, `indexProject()`를 통해 클라우드 측 인덱싱을 트리거할 수 있다.

### 2.3 Memory Events

메모리 이벤트는 대화 중 발생하는 사실, 결정, 인사이트 등을 기록하는 핵심 엔티티이다.

| 필드 | 타입 | 설명 |
|------|------|------|
| `id` | 자동 생성 | 고유 식별자 |
| `workspace_id` | `string` (UUID) | 소속 워크스페이스 (필수) |
| `project_id` | `string?` (UUID) | 소속 프로젝트 (선택) |
| `event_type` | `string` | 이벤트 유형 |
| `title` | `string` | 제목 |
| `content` | `string` | 본문 내용 (비어 있을 수 없음) |
| `tags` | `string[]?` | 태그 목록 (정규화 후 저장) |
| `metadata` | `Record<string, unknown>?` | 메타데이터 |
| `provenance` | `Record<string, unknown>?` | 출처 정보 |
| `code_refs` | `Array<{file_path, symbol_id?, symbol_name?}>?` | 코드 참조 |

**이벤트 유형 매핑**: `captureContext()` 메서드에서 고수준 타입이 API event_type으로 변환된다.

| 고수준 타입 | API event_type | 추가 태그 |
|-----------|---------------|----------|
| `conversation` | `chat` | -- |
| `decision` | `decision` | `decision` |
| `insight`, `preference`, `note`, `implementation` | `manual_note` | 해당 타입명 |
| `task` | `task` | -- |
| `plan` | `plan` | -- |
| `bug`, `feature` | `ticket` | 해당 타입명 |
| `correction`, `lesson`, `warning`, `frustration` | `manual_note` | 해당 타입명 + `lesson_system` |
| `session_snapshot` | (자동 체크포인트용) | `session_snapshot`, `checkpoint` |

모든 이벤트에는 `importance` 수준(`low`, `medium`, `high`, `critical`)과 `captured_at` 타임스탬프가 메타데이터로 포함된다.

주요 API 메서드:

- `createMemoryEvent(body)` -- `POST /memory/events`
- `bulkIngestEvents(body)` -- `POST /memory/events/ingest`
- `listMemoryEvents(params?)` -- `GET /memory/events/workspace/:workspace_id`
- `memorySearch(body)` -- `POST /memory/search`
- `memoryDecisions(params?)` -- `GET /memory/search/decisions`
- `captureContext(params)` -- 고수준 메모리 캡처 (내부적으로 `createMemoryEvent` 호출)
- `sessionRemember(params)` -- `POST /session/remember`

### 2.4 Knowledge Nodes

지식 노드는 구조화된 지식 그래프의 노드이다. Memory Events와 별개로 관리된다.

| 필드 | 타입 | 설명 |
|------|------|------|
| `id` | 자동 생성 | 고유 식별자 |
| `workspace_id` | `string` (UUID) | 소속 워크스페이스 (필수) |
| `project_id` | `string?` (UUID) | 소속 프로젝트 (선택) |
| `node_type` | `string` | 노드 유형 |
| `summary` | `string` | 요약 (title에서 파생) |
| `details` | `string?` | 상세 내용 (content에서 파생) |
| `valid_from` | `string` (ISO 8601) | 유효 시작 시각 |
| `relations` | `Array<{type, target_id}>?` | 관계 목록 (context에 저장) |

**노드 유형**: `normalizeNodeType()` 함수에서 입력을 정규화한다.

| 입력값 | 정규화 결과 |
|--------|-----------|
| `fact`, `insight`, `note` | `Fact` |
| `decision` | `Decision` |
| `preference` | `Preference` |
| `constraint` | `Constraint` |
| `habit` | `Habit` |
| `lesson` | `Lesson` |

주요 API 메서드:

- `createKnowledgeNode(body)` -- `POST /memory/nodes`
- `listKnowledgeNodes(params?)` -- `GET /memory/nodes/workspace/:workspace_id`
- `graphRelated(body)` -- `POST /graph/knowledge/related`
- `graphPath(body)` -- `POST /graph/knowledge/path`
- `graphDecisions(body)` -- `POST /graph/knowledge/decisions`
- `graphDependencies(body)` -- `POST /graph/dependencies`
- `graphCallPath(body)` -- `POST /graph/call-paths`
- `graphImpact(body)` -- `POST /graph/impact-analysis`
- `graphIngest(body)` -- `POST /graph/ingest/:projectId`

### 2.5 Sessions

세션은 MCP 연결 단위로 관리되며, `session-manager.ts`의 `SessionManager` 클래스가 담당한다.

| 필드 | 타입 | 설명 |
|------|------|------|
| `session_id` | `string` | `mcp-{UUID}` 형식으로 생성 |
| `workspace_id` | `string?` (UUID) | 해석된 워크스페이스 |
| `project_id` | `string?` (UUID) | 해석된 프로젝트 |
| `initialized_at` | `string` (ISO 8601) | 초기화 시각 |
| `ide_roots` | `string[]` | IDE에서 감지된 프로젝트 루트 경로 |
| `folder_path` | `string?` | 현재 작업 폴더 경로 |

세션 초기화는 `initSession()` 메서드를 통해 수행되며, 워크스페이스 해석 체인을 실행한다. "First-Tool Interceptor" 패턴을 사용하여 첫 번째 도구 호출 시 자동으로 컨텍스트를 초기화한다.

SessionManager는 다음 상태를 추가로 추적한다.

- **토큰 추적**: `sessionTokens`, `conversationTurns`를 통해 컨텍스트 압력(context pressure)을 추정한다. 턴당 약 3,000 토큰을 가정하고, 임계값 기본값은 70,000이다.
- **연속 체크포인트**: 도구 호출 20회마다 자동으로 체크포인트를 저장한다 (`CONTEXTSTREAM_CHECKPOINT_ENABLED=true` 시).
- **Post-compaction 복구**: 컨텍스트 압축 후 토큰이 급격히 감소하면 자동으로 컨텍스트를 복원한다.

### 2.6 Tasks

독립형 또는 Plan에 연결된 작업 항목이다.

| 필드 | 타입 | 설명 |
|------|------|------|
| `task_id` | `string` (UUID) | 고유 식별자 |
| `workspace_id` | `string` (UUID) | 소속 워크스페이스 |
| `project_id` | `string?` (UUID) | 소속 프로젝트 |
| `plan_id` | `string?` (UUID) | 연결된 Plan |
| `plan_step_id` | `string?` | Plan 내 단계 ID |
| `title` | `string` | 제목 |
| `content` | `string?` | 내용 |
| `description` | `string?` | 설명 |
| `status` | `"pending" \| "in_progress" \| "completed" \| "blocked" \| "cancelled"` | 상태 |
| `priority` | `"low" \| "medium" \| "high" \| "urgent"` | 우선순위 |
| `order` | `number?` | 정렬 순서 |
| `code_refs` | `Array<{file_path, symbol_name?, line_range?}>?` | 코드 참조 |
| `tags` | `string[]?` | 태그 |
| `is_personal` | `boolean?` | 개인 태스크 여부 |
| `blocked_reason` | `string?` | 차단 사유 |

주요 API 엔드포인트: `POST /tasks`, `GET /tasks`, `GET /tasks/:id`, `PATCH /tasks/:id`, `DELETE /tasks/:id`.

### 2.7 Plans

구현 계획을 관리하는 엔티티이다. 내부에 Steps와 Tasks를 포함할 수 있다.

| 필드 | 타입 | 설명 |
|------|------|------|
| `plan_id` | `string` (UUID) | 고유 식별자 |
| `workspace_id` | `string` (UUID) | 소속 워크스페이스 |
| `project_id` | `string?` (UUID) | 소속 프로젝트 |
| `title` | `string` | 제목 |
| `content` | `string?` | Markdown 내용 |
| `description` | `string?` | 설명 |
| `goals` | `string[]?` | 목표 목록 |
| `steps` | `Array<{id, title, description?, order, estimated_effort?}>?` | 단계 목록 |
| `status` | `"draft" \| "active" \| "completed" \| "archived" \| "abandoned"` | 상태 |
| `tags` | `string[]?` | 태그 |
| `due_at` | `string?` | 기한 (ISO 8601) |
| `source_tool` | `string?` | 생성 도구 |
| `is_personal` | `boolean?` | 개인 계획 여부 |

주요 API 엔드포인트: `POST /plans`, `GET /plans`, `GET /plans/:id`, `PATCH /plans/:id`, `DELETE /plans/:id`, `GET /plans/:id/tasks`, `POST /plans/:id/tasks`, `PATCH /plans/:id/tasks/reorder`.

### 2.8 Todos

Plans/Tasks와 별개로 운영되는 단순 할일 관리 시스템이다.

| 필드 | 타입 | 설명 |
|------|------|------|
| `todo_id` | `string` (UUID) | 고유 식별자 |
| `workspace_id` | `string` (UUID) | 소속 워크스페이스 |
| `project_id` | `string?` (UUID) | 소속 프로젝트 |
| `title` | `string` | 제목 |
| `description` | `string?` | 설명 |
| `status` | `"pending" \| "completed"` | 상태 |
| `priority` | `"low" \| "medium" \| "high" \| "urgent"` | 우선순위 |
| `due_at` | `string?` | 기한 |
| `is_personal` | `boolean?` | 개인 할일 여부 |

주요 API 엔드포인트: `GET /todos`, `POST /todos`, `GET /todos/:id`, `PATCH /todos/:id`, `DELETE /todos/:id`. 완료/미완료 토글은 `todosComplete()`/`todosIncomplete()` 편의 메서드로 제공된다.

### 2.9 Diagrams

Mermaid 다이어그램을 클라우드에 저장하고 관리한다.

| 필드 | 타입 | 설명 |
|------|------|------|
| `diagram_id` | `string` (UUID) | 고유 식별자 |
| `workspace_id` | `string` (UUID) | 소속 워크스페이스 |
| `project_id` | `string?` (UUID) | 소속 프로젝트 |
| `title` | `string` | 제목 |
| `diagram_type` | `"flowchart" \| "sequence" \| "class" \| "er" \| "gantt" \| "mindmap" \| "pie" \| "other"` | 다이어그램 유형 |
| `content` | `string` | Mermaid 문법 내용 |
| `metadata` | `Record<string, unknown>?` | 메타데이터 |
| `is_personal` | `boolean?` | 개인 다이어그램 여부 |

주요 API 엔드포인트: `GET /diagrams`, `POST /diagrams`, `GET /diagrams/:id`, `PATCH /diagrams/:id`, `DELETE /diagrams/:id`.

### 2.10 Docs

Markdown 문서를 클라우드에 저장한다. Roadmap 특화 템플릿도 지원한다.

| 필드 | 타입 | 설명 |
|------|------|------|
| `doc_id` | `string` (UUID) | 고유 식별자 |
| `workspace_id` | `string` (UUID) | 소속 워크스페이스 |
| `project_id` | `string?` (UUID) | 소속 프로젝트 |
| `title` | `string` | 제목 |
| `content` | `string` | Markdown 내용 |
| `doc_type` | `"roadmap" \| "spec" \| "general"` | 문서 유형 |
| `metadata` | `Record<string, unknown>?` | 메타데이터 |
| `is_personal` | `boolean?` | 개인 문서 여부 |

주요 API 엔드포인트: `GET /docs`, `POST /docs`, `POST /docs/roadmap`, `GET /docs/:id`, `PATCH /docs/:id`, `DELETE /docs/:id`.

Roadmap 생성 시 `milestones` 배열(`{title, description?, target_date?, status?}`)을 전달하여 구조화된 로드맵 문서를 생성할 수 있다.

### 2.11 Transcripts

대화 세션의 전사본(transcript)을 저장하고 검색한다.

| 필드 | 타입 | 설명 |
|------|------|------|
| `transcript_id` | `string` (UUID) | 고유 식별자 |
| `workspace_id` | `string?` (UUID) | 소속 워크스페이스 |
| `project_id` | `string?` (UUID) | 소속 프로젝트 |
| `session_id` | `string?` | 세션 식별자 |
| `client_name` | `string?` | 클라이언트 이름 (예: `cursor`, `claude-code`) |
| `started_after` | `string?` | 시작 시간 필터 (ISO 8601) |
| `started_before` | `string?` | 종료 시간 필터 (ISO 8601) |

주요 API 엔드포인트: `GET /transcripts` (페이지네이션 지원), `GET /transcripts/:id`, `GET /transcripts/search` (쿼리 기반 의미 검색).

### 2.12 Reminders

시간 기반 알림을 관리한다. Memory Event와 연결 가능하다.

| 필드 | 타입 | 설명 |
|------|------|------|
| `id` | `string` (UUID) | 고유 식별자 |
| `workspace_id` | `string?` (UUID) | 소속 워크스페이스 |
| `project_id` | `string?` (UUID) | 소속 프로젝트 |
| `title` | `string` | 제목 |
| `content` | `string` | 내용 |
| `remind_at` | `string` (ISO 8601) | 알림 시각 |
| `priority` | `string` | 우선순위 (기본값: `"normal"`) |
| `status` | `string` | 상태 |
| `keywords` | `string[]` | 키워드 목록 |
| `recurrence` | `string?` | 반복 규칙 |
| `memory_event_id` | `string?` (UUID) | 연결된 Memory Event |
| `created_at` | `string` (ISO 8601) | 생성 시각 |

주요 API 엔드포인트:

- `GET /reminders` -- 목록 조회
- `GET /reminders/active` -- 활성 알림 (pending, overdue, due soon; `overdue_count` 포함)
- `POST /reminders` -- 생성
- `POST /reminders/:id/snooze` -- 다시 알림
- `POST /reminders/:id/complete` -- 완료
- `POST /reminders/:id/dismiss` -- 해제
- `DELETE /reminders/:id` -- 삭제

### 2.13 Integrations (GitHub, Slack, Notion)

외부 서비스 통합으로, 워크스페이스 단위로 관리된다. 모든 통합 엔드포인트는 `/integrations/workspaces/:workspace_id/` 경로 아래에 위치한다.

#### GitHub 통합

| 엔드포인트 | 설명 |
|-----------|------|
| `GET .../github/stats` | 저장소 수, 커밋 수, PR 수, 이슈 수 등 통계 |
| `GET .../github/repos` | 연결된 저장소 목록 (id, name, full_name, language, stars 등) |
| `GET .../github/activity` | 커밋, PR, 이슈 활동 내역 (기간 필터링) |
| `GET .../github/issues` | 이슈 목록 (state, label 필터링) |
| `GET .../github/contributors` | 기여자 목록 (commits, prs, issues 카운트) |
| `GET .../github/search` | GitHub 데이터 검색 |
| `GET .../github/knowledge` | GitHub 기반 지식 추출 (topic 필터링) |

#### Slack 통합

| 엔드포인트 | 설명 |
|-----------|------|
| `GET .../slack/stats` | 메시지 수, 채널 수, 활성 사용자 수 등 통계 (기간 지정) |
| `GET .../slack/users` | Slack 사용자 목록 (slack_user_id, display_name, email 등) |
| `GET .../slack/channels` | 채널 목록 (channel_id, name, is_private 등) |
| `GET .../slack/activity` | 메시지 활동 내역 (channel_id, user_id 필터링) |
| `GET .../slack/discussions` | 토론 목록 |
| `GET .../slack/contributors` | 기여자 목록 (messages_count, reactions_count) |
| `POST .../slack/sync-users` | 사용자 동기화 |
| `GET .../slack/search` | Slack 메시지 검색 |
| `GET .../slack/knowledge` | Slack 기반 지식 추출 |

#### Notion 통합

| 엔드포인트 | 설명 |
|-----------|------|
| `GET .../notion/stats` | 페이지 수, 데이터베이스 수 등 통계 |
| `GET .../notion/activity` | 최근 활동 |
| `GET .../notion/knowledge` | Notion 기반 지식 추출 |
| `GET .../notion/summary` | Notion 요약 |
| `GET .../notion/databases` | 데이터베이스 목록 |
| `POST .../notion/databases` | 데이터베이스 생성 |
| `GET .../notion/pages` | 페이지 검색 |
| `GET .../notion/pages/:page_id` | 특정 페이지 조회 |
| `POST .../notion/databases/:db_id/query` | 데이터베이스 쿼리 |
| `PATCH .../notion/pages/:page_id` | 페이지 업데이트 |

#### 통합 상태 및 크로스소스 검색

- `GET .../integrations/status` -- 통합 연결 상태 확인 (각 provider의 `connected` 여부)
- `GET /integrations/search` -- 모든 통합에 걸쳐 크로스소스 검색
- `GET /integrations/summary` -- 모든 통합의 크로스소스 요약
- `GET /integrations/knowledge` -- 모든 통합의 크로스소스 지식 추출

통합이 연결되지 않은 상태에서 관련 엔드포인트에 접근하면 404가 반환되며, `http.ts`의 `rewriteNotFoundMessage()`가 사용자에게 친화적인 메시지로 재작성한다.

```typescript
// 예: "GitHub integration is not connected for this workspace.
//      Connect GitHub in workspace integrations and retry."
```

### 2.14 Media Content

비디오, 오디오, 이미지, 문서 등 미디어 파일을 클라우드에 업로드하고 인덱싱한다.

| 필드 | 타입 | 설명 |
|------|------|------|
| `id` | `string` (UUID) | 콘텐츠 고유 식별자 |
| `workspace_id` | `string` (UUID) | 소속 워크스페이스 |
| `content_type` | `"video" \| "audio" \| "image" \| "document" \| "text" \| "code" \| "other"` | 콘텐츠 유형 |
| `filename` | `string` | 파일명 |
| `size_bytes` | `number` | 파일 크기 |
| `mime_type` | `string?` | MIME 유형 |
| `title` | `string?` | 제목 |
| `tags` | `string[]` | 태그 |
| `status` | `string` | 처리 상태 |
| `indexing_progress` | `number?` | 인덱싱 진행률 |
| `indexing_error` | `string?` | 인덱싱 오류 메시지 |
| `metadata` | `Record<string, unknown>?` | 메타데이터 |
| `created_at` | `string` (ISO 8601) | 생성 시각 |
| `updated_at` | `string` (ISO 8601) | 수정 시각 |

업로드 프로세스는 2단계로 이루어진다.

1. **초기화** (`POST /workspaces/:id/content/uploads/init`): presigned URL과 헤더를 반환받는다.
2. **완료** (`POST /workspaces/:id/content/:content_id/complete-upload`): 업로드 후 인덱싱을 트리거한다.

추가 기능:

- `mediaGetContent()` -- 콘텐츠 상태 조회 (인덱싱 진행률 포함)
- `mediaListContent()` -- 워크스페이스 내 콘텐츠 목록 (content_type, status 필터링)
- `mediaSearchContent()` -- 의미 검색 (transcript, description 기반)
- `mediaGetClip()` -- 특정 구간 클립 추출 (`json`, `remotion`, `ffmpeg` 포맷 지원)
- `mediaDeleteContent()` -- 콘텐츠 삭제

---

## 3. 타입 정의

`client.ts`에서 정의되는 주요 TypeScript 타입과 인터페이스를 정리한다.

### 3.1 SemanticIntent

SmartRouter에서 반환하는 의미적 의도 분류 결과이다.

```typescript
interface SemanticIntent {
  intent_type: string;
  risk_level: "none" | "low" | "medium" | "high" | "critical";
  confidence: number;
  decision_detected: boolean;
  capture_worthy: boolean;
  suggested_capture_type?: string;
  suggested_capture_title?: string;
  extracted_entities?: string[];
  explanation?: string;
}
```

- `intent_type`: 의도 유형 (예: 질문, 구현 요청 등)
- `risk_level`: 작업의 위험 수준 (5단계)
- `confidence`: 분류 신뢰도 (0~1)
- `decision_detected`: 결정 사항 감지 여부
- `capture_worthy`: 메모리에 캡처할 가치가 있는지 여부
- `suggested_capture_type`/`suggested_capture_title`: 자동 캡처 시 제안되는 유형과 제목
- `extracted_entities`: 추출된 엔티티 목록

### 3.2 GraphTier

사용자 플랜에 따른 지식 그래프 접근 수준이다.

```typescript
type GraphTier = "none" | "lite" | "full";
```

`normalizeGraphTier()` 함수가 다양한 입력을 정규화한다.

| 입력 키워드 | 결과 |
|-----------|------|
| `full`, `elite`, `team` | `"full"` |
| `lite`, `light`, `basic`, `module` | `"lite"` |
| `none`, `off`, `disabled`, `free` | `"none"` |

플랜 이름으로부터의 자동 매핑:

- `elite`, `team`, `enterprise`, `business` -> `"full"`
- `pro`, `free` -> `"lite"`

### 3.3 IngestStatus 및 IngestRecommendation

파일 인덱싱 상태와 추천 정보를 나타낸다.

```typescript
type IngestStatus =
  | "not_indexed"
  | "indexed"
  | "stale"
  | "recently_indexed"
  | "auto_started"
  | "auto_refreshing"
  | "disabled";

interface IngestRecommendation {
  recommended: boolean;
  status: IngestStatus;
  indexed_files?: number;
  last_indexed?: string;
  reason: string;
  benefits?: string[];
  command?: string;
}
```

인덱싱 추천 시 사용자에게 보여줄 혜택 목록(`INGEST_BENEFITS`)이 미리 정의되어 있다.

```typescript
const INGEST_BENEFITS = [
  "Enable semantic code search across your entire codebase",
  "Get AI-powered code understanding and context for your questions",
  "Unlock dependency analysis and impact assessment",
  "Allow the AI assistant to find relevant code without manual file navigation",
  "Build a searchable knowledge base of your codebase structure",
];
```

### 3.4 FileToIngest

인덱싱 대상 파일의 인터페이스이다. `files.ts`에서 정의된다.

```typescript
interface FileToIngest {
  path: string;
  content: string;
  language?: string;

  // 버전 메타데이터 (다중 머신 동기화용)
  git_commit_sha?: string;
  git_commit_timestamp?: string;
  source_modified_at?: string;
  machine_id?: string;

  // 브랜치 메타데이터
  git_branch?: string;
  git_default_branch?: string;
  is_default_branch?: boolean;
}
```

주요 특징:

- `path`는 프로젝트 루트 기준 상대 경로이다.
- `machine_id`는 호스트명의 SHA-256 해시 앞 12자로 생성되어 프라이버시를 보장한다.
- Git 정보는 리포지토리별로 캐시(`gitContextCache`)되어 반복 명령을 방지한다.
- 브랜치 메타데이터를 포함하여 다중 머신에서의 동기화를 지원한다.

### 3.5 IngestApiData 및 IngestLocalResult

인덱싱 API 응답과 로컬 인덱싱 결과를 나타낸다.

```typescript
interface IngestApiData {
  files_received?: number;
  files_indexed?: number;
  files_skipped?: number;
  credits_used?: number;
  status?: "cooldown" | "up_to_date" | "completed" | "partial" | "daily_limit_exceeded";
  message?: string;
}

interface IngestLocalResult {
  totalFiles: number;
  filesChanged: number;
  filesIndexed: number;
  filesSkipped: number;
  apiSkipped: number;
  status: "success" | "cooldown" | "daily_limit" | "partial" | "error";
  abortedEarly: boolean;
}
```

### 3.6 Config (Zod 스키마)

`config.ts`에서 Zod로 정의되는 설정 스키마이다.

```typescript
const configSchema = z.object({
  apiUrl: z.string().url().default(DEFAULT_API_URL),
  apiKey: z.string().min(1).optional(),
  jwt: z.string().min(1).optional(),
  defaultWorkspaceId: z.string().uuid().optional(),
  defaultProjectId: z.string().uuid().optional(),
  userAgent: z.string().default(`contextstream-mcp/${VERSION}`),
  allowHeaderAuth: z.boolean().optional(),
  contextPackEnabled: z.boolean().default(true),
  showTiming: z.boolean().default(false),
  toolSurfaceProfile: z.enum(["default", "openai_agentic"]).default("default"),
});
```

각 필드의 환경변수 매핑:

| 필드 | 환경변수 |
|------|---------|
| `apiUrl` | `CONTEXTSTREAM_API_URL` |
| `apiKey` | `CONTEXTSTREAM_API_KEY` |
| `jwt` | `CONTEXTSTREAM_JWT` |
| `defaultWorkspaceId` | `CONTEXTSTREAM_WORKSPACE_ID` |
| `defaultProjectId` | `CONTEXTSTREAM_PROJECT_ID` |
| `userAgent` | `CONTEXTSTREAM_USER_AGENT` |
| `allowHeaderAuth` | `CONTEXTSTREAM_ALLOW_HEADER_AUTH` |
| `contextPackEnabled` | `CONTEXTSTREAM_CONTEXT_PACK` 또는 `CONTEXTSTREAM_CONTEXT_PACK_ENABLED` |
| `showTiming` | `CONTEXTSTREAM_SHOW_TIMING` |
| `toolSurfaceProfile` | `CONTEXTSTREAM_TOOL_SURFACE_PROFILE` |

---

## 4. 캐시 시스템

`cache.ts`에서 구현되는 인메모리 캐시로, 클라우드 API에 대한 HTTP 요청 횟수를 줄이기 위해 사용된다. 로컬 데이터베이스가 없으므로, 이 캐시가 유일한 로컬 데이터 저장 계층이다 (휘발성).

### 4.1 MemoryCache 클래스

```typescript
class MemoryCache {
  private cache = new Map<string, CacheEntry<unknown>>();
  private cleanupInterval: NodeJS.Timeout | null = null;

  constructor(cleanupIntervalMs = 60_000) { /* ... */ }
}
```

`Map<string, CacheEntry<T>>` 기반의 TTL 캐시이다. 각 엔트리는 `value`와 `expiresAt` 타임스탬프를 포함한다.

주요 메서드:

| 메서드 | 설명 |
|--------|------|
| `get<T>(key)` | 만료되지 않은 값 반환. 만료 시 삭제 후 `undefined` 반환 |
| `set<T>(key, value, ttlMs)` | TTL과 함께 값 저장 |
| `delete(key)` | 특정 키 삭제 |
| `deleteByPrefix(prefix)` | prefix로 시작하는 모든 키 삭제 |
| `clear()` | 전체 캐시 초기화 |
| `destroy()` | 정리 타이머 중지 및 캐시 초기화 |

### 4.2 TTL 설정

`CacheTTL` 상수가 각 데이터 유형별 캐시 수명을 정의한다.

| 캐시 키 | TTL | 근거 |
|---------|-----|------|
| `WORKSPACE` | 5분 (300,000ms) | 워크스페이스 정보는 거의 변경되지 않음 |
| `PROJECT` | 5분 (300,000ms) | 프로젝트 정보도 거의 변경되지 않음 |
| `SESSION_INIT` | 60초 (60,000ms) | 세션 초기화 컨텍스트 |
| `MEMORY_EVENTS` | 30초 (30,000ms) | 메모리 이벤트는 더 자주 변경됨 |
| `SEARCH` | 60초 (60,000ms) | 검색 결과 |
| `USER_PREFS` | 5분 (300,000ms) | 사용자 환경설정 |
| `CREDIT_BALANCE` | 60초 (60,000ms) | 크레딧/플랜 잔액 (업그레이드 반영을 위해 짧게 유지) |

### 4.3 캐시 키 패턴

`CacheKeys` 객체가 키 생성 함수를 제공한다.

```typescript
const CacheKeys = {
  workspace:     (id: string)     => `workspace:${id}`,
  workspaceList: (userId: string) => `workspaces:${userId}`,
  project:       (id: string)     => `project:${id}`,
  projectList:   (workspaceId: string) => `projects:${workspaceId}`,
  sessionInit:   (workspaceId?, projectId?) =>
                   `session_init:${workspaceId || ""}:${projectId || ""}`,
  memoryEvents:  (workspaceId: string) => `memory:${workspaceId}`,
  search:        (query: string, workspaceId?) =>
                   `search:${workspaceId || ""}:${query}`,
  creditBalance: () => "credits:balance",
};
```

### 4.4 자동 정리

기본적으로 **60초 간격**으로 만료된 엔트리를 정리한다. `setInterval`로 설정되며, `unref()`를 호출하여 프로세스가 캐시 정리 타이머 때문에 종료되지 않도록 한다 (예: `--version` 같은 일회성 명령).

### 4.5 캐시 무효화

워크스페이스나 프로젝트를 업데이트/삭제할 때 관련 캐시가 명시적으로 무효화된다.

```typescript
// updateWorkspace 내부:
globalCache.delete(CacheKeys.workspace(workspaceId));
globalCache.delete(`workspace_overview:${workspaceId}`);

// updateProject 내부:
globalCache.delete(CacheKeys.project(projectId));
globalCache.delete(`project_overview:${projectId}`);
```

---

## 5. HTTP 클라이언트

`http.ts`는 ContextStream API와의 모든 HTTP 통신을 담당하는 모듈이다.

### 5.1 HttpError 클래스

API 오류를 표현하는 커스텀 에러 클래스이다.

```typescript
class HttpError extends Error {
  status: number;    // HTTP 상태 코드 (0 = 네트워크 오류)
  body: any;         // 응답 본문
  code: string;      // 내부 에러 코드
}
```

`statusToCode()` 함수가 HTTP 상태 코드를 내부 코드로 매핑한다.

| HTTP 상태 | 코드 |
|-----------|------|
| 0 | `NETWORK_ERROR` |
| 400 | `BAD_REQUEST` |
| 401 | `UNAUTHORIZED` |
| 403 | `FORBIDDEN` |
| 404 | `NOT_FOUND` |
| 409 | `CONFLICT` |
| 422 | `VALIDATION_ERROR` |
| 429 | `RATE_LIMITED` |
| 500 | `INTERNAL_ERROR` |
| 502 | `BAD_GATEWAY` |
| 503 | `SERVICE_UNAVAILABLE` |
| 504 | `GATEWAY_TIMEOUT` |
| 기타 | `UNKNOWN_ERROR` |

API 응답에 자체 에러 코드가 포함된 경우(`{ error: { code, message } }` 형식) 해당 코드가 `HttpError.code`를 덮어쓴다.

### 5.2 RequestOptions 인터페이스

```typescript
interface RequestOptions {
  method?: "GET" | "POST" | "PUT" | "DELETE" | "PATCH";
  body?: any;
  signal?: AbortSignal;
  retries?: number;
  retryDelay?: number;
  timeoutMs?: number;
  workspaceId?: string;
}
```

`body`가 제공되면 기본 method는 `POST`, 없으면 `GET`이다.

### 5.3 Rate Limit 헤더

서버에서 반환하는 rate limit 관련 헤더를 파싱하여 에러 응답에 첨부한다.

```typescript
type RateLimitHeaders = {
  limit: number;       // X-RateLimit-Limit
  remaining: number;   // X-RateLimit-Remaining
  reset: number;       // X-RateLimit-Reset
  scope: string;       // X-RateLimit-Scope
  plan: string;        // X-RateLimit-Plan
  group: string;       // X-RateLimit-Group
  retryAfter?: number; // Retry-After
};
```

요청 시에는 `X-Workspace-Id` 헤더를 전송하여 workspace-pooled rate limiting을 지원한다. 워크스페이스 ID는 다음 우선순위로 추론된다.

1. `authOverride.workspaceId`
2. `options.workspaceId`
3. 요청 body의 `workspace_id` 필드 (`inferWorkspaceIdFromBody`)
4. URL 경로에서 추출 (`inferWorkspaceIdFromPath`) -- `/workspaces/:uuid` 또는 `/workspace/:uuid` 패턴 매칭
5. 쿼리 파라미터의 `workspace_id`
6. `config.defaultWorkspaceId`

### 5.4 요청 흐름

`request<T>()` 함수의 전체 흐름은 다음과 같다.

1. 인증 확인 (unauthenticated 엔드포인트 예외 처리)
2. URL 조립 (`apiUrl` + `/api/v1` + path)
3. 헤더 설정 (`Content-Type`, `User-Agent`, `X-API-Key` 또는 `Authorization`, `X-Workspace-Id`)
4. 최대 `maxRetries + 1`회 반복:
   a. `AbortController`로 타임아웃 설정
   b. `fetch()` 실행
   c. 네트워크 오류 시 지수 백오프 후 재시도
   d. 응답 파싱 (`application/json` 또는 text)
   e. 비정상 응답(4xx/5xx) 시 rate limit 헤더 첨부, 에러 메시지 추출
   f. 재시도 가능한 상태 코드(408/429/500-504)면 `Retry-After` 또는 지수 백오프 후 재시도
5. 최종 실패 시 `HttpError` throw

### 5.5 에러 메시지 추출

API 응답에서 에러 메시지를 추출하는 우선순위:

1. `payload.error.message` (중첩 에러 객체)
2. `payload.message`
3. `payload.error` (문자열)
4. `payload.detail`
5. `response.statusText` (fallback)

---

## 6. 파일 인덱싱

`files.ts`는 로컬 파일 시스템에서 코드 파일을 읽어 클라우드 인덱싱에 전달하는 기능을 담당한다.

### 6.1 지원 언어 및 확장자

`CODE_EXTENSIONS` Set에 60여 개 확장자가 정의되어 있다.

| 카테고리 | 확장자 |
|---------|--------|
| Rust | `rs` |
| TypeScript/JavaScript | `ts`, `tsx`, `js`, `jsx`, `mjs`, `cjs` |
| Python | `py`, `pyi` |
| Go | `go` |
| Java/Kotlin | `java`, `kt`, `kts` |
| C/C++ | `c`, `h`, `cpp`, `hpp`, `cc`, `cxx` |
| C# | `cs` |
| Ruby | `rb` |
| PHP | `php` |
| Swift | `swift` |
| Scala | `scala` |
| Shell | `sh`, `bash`, `zsh` |
| Config/Data | `json`, `yaml`, `yml`, `toml`, `xml` |
| SQL | `sql` |
| Markdown/Docs | `md`, `markdown`, `rst`, `txt` |
| HTML/CSS | `html`, `htm`, `css`, `scss`, `sass`, `less` |
| 기타 | `graphql`, `proto`, `dockerfile`, `dart` |

### 6.2 파일 읽기 함수

**`readFilesFromDirectory(rootPath, options)`**: 디렉토리에서 인덱싱 가능한 파일을 재귀적으로 읽는다. 기본 최대 200파일(`MAX_FILES_PER_BATCH`), 최대 파일 크기 5MB(`MAX_FILE_SIZE`).

**`readAllFilesInBatches(rootPath, options)`**: async generator로 모든 파일을 크기 기반 배치로 반환한다. 배치 제한:

| 설정 | 기본값 | 설명 |
|------|--------|------|
| `MAX_BATCH_BYTES` | 10MB | 배치당 최대 바이트 |
| `LARGE_FILE_THRESHOLD` | 2MB | 이보다 큰 파일은 개별 배치로 처리 |
| `MAX_FILES_PER_BATCH` | 200 | 배치당 최대 파일 수 (보조 제한) |
| `MAX_FILE_SIZE` | 5MB | 이보다 큰 파일은 건너뜀 |

**`readChangedFilesInBatches(rootPath, sinceTimestamp, options)`**: 증분 인덱싱용. `mtime`이 `sinceTimestamp` 이후인 파일만 포함한다. 동일한 크기 기반 배치 로직을 사용한다.

**`countIndexableFiles(rootPath, options)`**: 인덱싱 가능한 파일이 있는지 빠르게 확인한다. 기본적으로 1개 발견 시 즉시 중단한다.

### 6.3 무시 규칙

세 가지 무시 메커니즘이 동작한다.

**하드코딩된 디렉토리 무시** (`IGNORE_DIRS`): `node_modules`, `.git`, `.svn`, `.hg`, `target`, `dist`, `build`, `out`, `.next`, `.nuxt`, `__pycache__`, `.pytest_cache`, `.mypy_cache`, `venv`, `.venv`, `env`, `.env`, `vendor`, `coverage`, `.coverage`, `.idea`, `.vscode`, `.vs`.

**하드코딩된 파일 무시** (`IGNORE_FILES`): `.DS_Store`, `Thumbs.db`, `.gitignore`, `.gitattributes`, `package-lock.json`, `yarn.lock`, `pnpm-lock.yaml`, `Cargo.lock`, `poetry.lock`, `Gemfile.lock`, `composer.lock`.

**`.contextstream/ignore` 파일**: `ignore.ts`에서 구현되며, gitignore 문법을 지원한다 (`ignore` npm 패키지 사용). 프로젝트 루트의 `.contextstream/ignore` 파일에 정의된 패턴을 로드하고, 기본 무시 패턴(위 하드코딩된 디렉토리 목록)을 자동으로 추가한다.

### 6.4 SHA-256 중복 제거

파일 내용의 SHA-256 해시를 계산하여 변경되지 않은 파일의 재인덱싱을 방지한다.

```typescript
function sha256Hex(content: string): string {
  return crypto.createHash("sha256").update(content).digest("hex");
}
```

**해시 매니페스트**: 프로젝트별로 `~/.contextstream/file-hashes/{projectId}.json`에 저장된다. `Map<relativePath, sha256Hex>` 형태이다.

- `readHashManifest(projectId)` -- 매니페스트 로드 (오류 시 빈 Map 반환)
- `writeHashManifest(projectId, hashes)` -- 매니페스트 저장 (best-effort)
- `deleteHashManifest(projectId)` -- 매니페스트 삭제

인덱싱 시 현재 파일 해시와 매니페스트의 이전 해시를 비교하여 변경된 파일만 API에 전송한다. 자동 인덱싱 시 최대 10,000파일(`AUTO_INDEX_FILE_CAP`)로 제한된다.

### 6.5 Git 메타데이터

각 파일에 대해 다음 Git 정보를 수집한다 (리포지토리 단위로 캐시).

| 정보 | 수집 방법 |
|------|----------|
| `git_branch` | `git branch --show-current` |
| `git_default_branch` | `git symbolic-ref refs/remotes/origin/HEAD`, `git config --get init.defaultBranch`, `git branch --list main master` (3단계 fallback) |
| `is_default_branch` | 현재 브랜치와 기본 브랜치 비교 |
| `git_commit_sha` | `git log -1 --format="%H %ct" -- <file>` |
| `git_commit_timestamp` | 위 명령의 Unix timestamp를 ISO 8601로 변환 |
| `machine_id` | `os.hostname()`의 SHA-256 앞 12자 |
| `source_modified_at` | `stat.mtime`의 ISO 8601 변환 |

### 6.6 언어 감지

`detectLanguage()` 함수가 파일 확장자를 기반으로 언어를 판별한다.

```typescript
const langMap: Record<string, string> = {
  rs: "rust", ts: "typescript", tsx: "typescript",
  js: "javascript", jsx: "javascript", py: "python",
  go: "go", java: "java", kt: "kotlin",
  c: "c", h: "c", cpp: "cpp", hpp: "cpp",
  cs: "csharp", rb: "ruby", php: "php",
  swift: "swift", scala: "scala", sql: "sql",
  dart: "dart", md: "markdown", json: "json",
  yaml: "yaml", yml: "yaml", toml: "toml",
  html: "html", css: "css", sh: "shell",
};
```

---

## 7. 자격증명 저장

`credentials.ts`에서 구현되며, API 키를 로컬 파일 시스템에 안전하게 저장한다.

### 7.1 파일 경로 및 형식

경로: `~/.contextstream/credentials.json`

```typescript
type SavedCredentialsV1 = {
  version: 1;
  api_url: string;
  api_key: string;
  email?: string;
  created_at: string;
  updated_at: string;
};
```

- `version`: 스키마 버전 (현재 1)
- `api_url`: API 서버 URL (후행 슬래시 정규화)
- `api_key`: API 키
- `email`: 사용자 이메일 (선택)
- `created_at`: 최초 생성 시각 (ISO 8601)
- `updated_at`: 최종 수정 시각 (ISO 8601)

### 7.2 보안

파일 권한은 소유자만 읽기/쓰기 가능하도록 **mode 0o600**으로 설정된다.

```typescript
await fs.writeFile(filePath, body, { encoding: "utf8", mode: 0o600 });
try {
  await fs.chmod(filePath, 0o600);
} catch {
  // Best-effort only (e.g., Windows).
}
```

`writeFile`에서 mode를 지정하고, `chmod`도 추가로 호출한다 (이중 보호). Windows 환경에서는 `chmod`가 실패할 수 있으므로 best-effort로 처리한다.

### 7.3 주요 함수

| 함수 | 설명 |
|------|------|
| `credentialsFilePath()` | `~/.contextstream/credentials.json` 경로 반환 |
| `normalizeApiUrl(input)` | 후행 슬래시 제거 |
| `readSavedCredentials()` | 파일 읽기 (version 1 검증, api_url/api_key 유효성 확인) |
| `writeSavedCredentials(input)` | 파일 쓰기 (기존 `created_at` 유지, `updated_at` 갱신) |
| `deleteSavedCredentials()` | 파일 삭제 (best-effort) |

읽기 시 검증 단계:

1. JSON 파싱
2. `version === 1` 확인
3. `api_url`이 비어 있지 않은 문자열인지 확인 (정규화 적용)
4. `api_key`가 비어 있지 않은 문자열인지 확인
5. 하나라도 실패하면 `null` 반환

---

## 8. 워크스페이스 설정

`workspace-config.ts`에서 구현되며, 로컬 프로젝트와 클라우드 워크스페이스 간의 매핑을 관리한다.

### 8.1 로컬 설정 (`WorkspaceConfig`)

각 리포지토리의 `.contextstream/config.json`에 저장된다.

```typescript
interface WorkspaceConfig {
  workspace_id: string;
  workspace_name?: string;
  project_id?: string;
  project_name?: string;
  associated_at?: string;
  version?: string;
  configured_editors?: string[];
  context_pack?: boolean;
  api_url?: string;
  updated_at?: string;
  indexing_enabled?: boolean;
}
```

주요 함수:

- `readLocalConfig(repoPath)` -- 리포지토리의 `.contextstream/config.json` 읽기
- `writeLocalConfig(repoPath, config)` -- 디렉토리 생성 포함하여 설정 저장

### 8.2 글로벌 부모 폴더 매핑 (`ParentMapping`)

`~/.contextstream-mappings.json`에 저장된다. 부모 폴더 패턴과 워크스페이스를 연결한다.

```typescript
interface ParentMapping {
  pattern: string;         // 예: "/home/user/dev/projects/*"
  workspace_id: string;
  workspace_name: string;
}
```

와일드카드(`*`) 패턴을 지원한다. 예를 들어 `/home/user/dev/projects/*` 패턴은 해당 디렉토리 아래의 모든 하위 프로젝트를 같은 워크스페이스에 매핑한다.

주요 함수:

- `readGlobalMappings()` -- 매핑 목록 읽기
- `writeGlobalMappings(mappings)` -- 매핑 목록 쓰기
- `addGlobalMapping(mapping)` -- 매핑 추가 (동일 패턴 존재 시 교체)
- `findMatchingMapping(repoPath)` -- 리포지토리 경로에 매칭되는 매핑 검색

### 8.3 워크스페이스 해석 체인 (`resolveWorkspace`)

리포지토리 경로에서 워크스페이스를 해석하는 3단계 체인이다.

```
1. 로컬 설정 (.contextstream/config.json) --> source: "local_config"
2. 글로벌 부모 매핑 (~/.contextstream-mappings.json) --> source: "parent_mapping"
3. 해석 불가 --> source: "ambiguous" (사용자 선택 필요)
```

이 체인은 `session-manager.ts`의 `autoInitialize()`에서 호출되며, 해석 실패 시 `initSession()`의 워크스페이스 발견 체인이 추가로 동작한다.

`initSession()`의 확장된 워크스페이스 발견 체인:

1. 환경변수/설정의 `workspace_id`
2. `resolveWorkspace()` (로컬 설정 -> 부모 매핑)
3. API를 통한 이름 매칭:
   - 폴더명과 정확히 일치하는 워크스페이스
   - 폴더명을 포함하는/포함되는 워크스페이스
   - 프로젝트 이름으로 워크스페이스 역추적
4. 단일 워크스페이스만 존재하면 자동 선택
5. 해석 실패 시 `requires_workspace_name` 또는 `requires_workspace_selection` 상태 반환

### 8.4 멀티 프로젝트 감지

`isMultiProjectFolder()` 함수가 디렉토리가 여러 프로젝트를 포함하는 상위 폴더인지 감지한다. 프로젝트 마커(`.git`, `package.json`, `Cargo.toml`, `pyproject.toml`, `go.mod`, `pom.xml`, `build.gradle`, `Gemfile`, `composer.json`, `.contextstream`)를 확인하여, 2개 이상의 하위 디렉토리가 프로젝트 마커를 포함하면 멀티 프로젝트 폴더로 판단한다. 루트에 `.git`이 있으면 모노레포로 간주하여 더 높은 기준(2개 이상)을 적용한다.
