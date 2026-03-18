# ContextStream -- 구독 & 과금 분석

> 소스: `src/tools.ts` (lines 2527-2975), `src/client.ts` (lines 367-431)
> ContextStream MCP 서버의 구독 티어 기반 도구 접근 제어, 그래프 티어, 통합 도구 Auto-Hide, 크레딧 시스템 전반을 분석한다.

---

## 1. 구독 티어 (Subscription Tiers)

ContextStream은 5개 구독 티어를 지원한다: **Free**, **Pro**, **Elite**, **Team**, **Enterprise**.

### 1.1 티어 감지 메커니즘

티어 정보는 `/credits/balance` API 엔드포인트에서 가져오며, `ContextStreamClient`의 세 가지 메서드로 분류한다.

#### getPlanName()

```typescript
async getPlanName(): Promise<string | null>
```

- `getCreditBalance()`를 호출하여 `balance.plan.name`을 추출한다.
- 반환값은 소문자로 정규화된다 (예: `"free"`, `"pro"`, `"elite"`, `"team"`, `"enterprise"`).
- 에러 시 `null`을 반환한다.

#### getGraphTier()

```typescript
async getGraphTier(): Promise<GraphTier>  // "none" | "lite" | "full"
```

- `getCreditBalance()` 응답에서 graph_tier 정보를 다중 경로로 탐색한다:
  1. `plan.graph_tier` 또는 `plan.graphTier`
  2. `plan.features.graph_tier` 또는 `plan.features.graphTier`
  3. `balance.graph_tier` 또는 `balance.graphTier`
- 명시적 tier 값이 없으면 plan name 기반으로 추론한다:
  - `"elite"`, `"team"`, `"enterprise"`, `"business"` 포함 -> `"full"`
  - `"pro"` 포함 -> `"lite"`
  - `"free"` 포함 -> `"lite"` (Free도 Pro와 동일한 Graph-Lite 접근 제공, 단 제한된 작업)
- `normalizeGraphTier()` 헬퍼는 `"full"`, `"elite"`, `"team"` 등을 `"full"`로, `"lite"`, `"light"`, `"basic"`, `"module"` 등을 `"lite"`로, `"none"`, `"off"`, `"disabled"`, `"free"` 등을 `"none"`으로 정규화한다.

#### isTeamPlan()

```typescript
async isTeamPlan(): Promise<boolean>
```

- `getPlanName()` 결과에 `"team"`, `"enterprise"`, `"business"` 중 하나가 포함되어 있으면 `true`를 반환한다.
- 팀 전용 MCP 도구 동작을 게이팅하는 데 사용된다.

### 1.2 크레딧 잔액 캐싱

`getCreditBalance()`는 글로벌 캐시(`globalCache`)를 활용한다. 캐시 키는 `CacheKeys.creditBalance()`이며, TTL은 `CacheTTL.CREDIT_BALANCE`에 정의되어 있다. API 호출은 `GET /credits/balance`로 수행된다.

---

## 2. Pro 도구 게이팅 (Pro Tool Gating)

### 2.1 defaultProTools 집합

Free 티어에서 접근이 차단되는 Pro 전용 도구 목록이다. 총 **31개** 도구가 기본 Pro 도구로 지정되어 있다:

| 카테고리 | 도구명 |
|---------|--------|
| **AI 엔드포인트** (크레딧 과금) | `ai_context`, `ai_enhanced_context`, `ai_context_budget`, `ai_embeddings`, `ai_plan`, `ai_tasks` |
| **Slack 통합** | `slack_stats`, `slack_channels`, `slack_contributors`, `slack_activity`, `slack_discussions`, `slack_search`, `slack_sync_users` |
| **GitHub 통합** | `github_stats`, `github_repos`, `github_contributors`, `github_activity`, `github_issues`, `github_search` |
| **Notion 통합** | `notion_create_page`, `notion_list_databases`, `notion_search_pages`, `notion_get_page`, `notion_query_database`, `notion_update_page`, `notion_stats`, `notion_activity`, `notion_knowledge`, `notion_summary` |
| **미디어 작업** (크레딧 과금) | `media_index`, `media_search` |

### 2.2 환경변수 오버라이드

```typescript
const proTools = (() => {
  const raw = process.env.CONTEXTSTREAM_PRO_TOOLS;
  if (!raw) return defaultProTools;
  const parsed = raw.split(",").map((t) => t.trim()).filter(Boolean);
  return parsed.length > 0 ? new Set(parsed) : defaultProTools;
})();
```

`CONTEXTSTREAM_PRO_TOOLS` 환경변수를 쉼표로 구분된 도구명으로 설정하면 기본 Pro 도구 목록을 완전히 대체할 수 있다.

### 2.3 getToolAccessTier()

```typescript
function getToolAccessTier(toolName: string): "free" | "pro"
```

도구 이름이 `proTools` 집합에 포함되면 `"pro"`, 아니면 `"free"`를 반환한다.

### 2.4 gateIfProTool()

```typescript
async function gateIfProTool(toolName: string): Promise<ToolTextResult | null>
```

**게이팅 로직:**

1. `getToolAccessTier(toolName)`이 `"pro"`가 아니면 `null` 반환 (통과).
2. `client.getPlanName()`을 호출하여 플랜명을 확인한다.
3. 플랜명이 `"free"`가 아니면 `null` 반환 (Pro/Elite/Team/Enterprise 모두 통과).
4. Free 사용자가 Pro 도구 호출 시 접근 거부 메시지를 반환한다:
   ```
   Access denied: `<toolName>` requires ContextStream PRO.
   Upgrade: <upgradeUrl>
   ```

### 2.5 업그레이드 URL

```typescript
const upgradeUrl = process.env.CONTEXTSTREAM_UPGRADE_URL || "https://contextstream.io/pricing";
```

기본값은 `https://contextstream.io/pricing`이며, `CONTEXTSTREAM_UPGRADE_URL` 환경변수로 변경할 수 있다.

---

## 3. 그래프 티어 (Graph Tier Gating)

### 3.1 그래프 도구별 필요 티어

`graphToolTiers` 맵이 각 그래프 도구의 최소 요구 티어를 정의한다:

| 도구명 | 필요 티어 | 설명 |
|--------|----------|------|
| `graph_dependencies` | `lite` | 의존성 분석 (Lite: module-only, max_depth=1) |
| `graph_impact` | `lite` | 영향 분석 (Lite: module-only, max_depth=1) |
| `graph_ingest` | `lite` | 그래프 구축/인덱싱 (Pro는 모듈 수준, Elite는 전체 그래프) |
| `graph_related` | `full` | 관련 노드 탐색 |
| `graph_decisions` | `full` | 의사결정 그래프 |
| `graph_path` | `full` | 노드 간 경로 |
| `graph_call_path` | `full` | 함수 호출 경로 |
| `graph_circular_dependencies` | `full` | 순환 의존성 탐지 |
| `graph_unused_code` | `full` | 미사용 코드 탐지 |
| `graph_contradictions` | `full` | 지식 모순 탐지 |

### 3.2 Graph-Lite 제약사항

Graph-Lite (`"lite"` 티어)는 다음과 같은 제약을 적용한다:

- **모듈 전용**: `target.type`이 `"module"`, `"file"`, `"path"` 중 하나여야 한다.
- **최대 깊이 1**: `max_depth`는 `graphLiteMaxDepth = 1`을 초과할 수 없다.
- **전이적 의존성 불가**: `graph_dependencies`에서 `include_transitive = true`는 허용되지 않는다.

`isModuleTargetType()` 함수가 유효한 모듈 타입인지 확인한다:
```typescript
function isModuleTargetType(value: string): boolean {
  return value === "module" || value === "file" || value === "path";
}
```

### 3.3 gateIfGraphTool()

```typescript
async function gateIfGraphTool(toolName: string, input?: any): Promise<ToolTextResult | null>
```

**게이팅 로직:**

1. `graphToolTiers`에 도구가 없으면 `null` 반환 (비그래프 도구는 통과).
2. `client.getGraphTier()`를 호출하여 사용자의 그래프 티어를 확인한다.
3. `"full"` 티어면 모든 도구에 대해 `null` 반환 (전체 접근).
4. `"lite"` 티어일 때:
   - `full` 필요 도구 호출 시: `"Access denied: <toolName> requires Elite or Team (Full Graph)."` 에러 반환.
   - `graph_dependencies`: target.type이 모듈이 아니면 에러, max_depth > 1이면 에러, include_transitive = true이면 에러.
   - `graph_impact`: target.type이 모듈이 아니면 에러, max_depth > 1이면 에러.
   - 조건 충족 시 `null` 반환 (통과).
5. `"none"` 또는 기타 티어: `"Access denied: <toolName> requires ContextStream Pro (Graph-Lite) or Elite/Team (Full Graph)."` 에러 반환.

### 3.4 제약 위반 에러 메시지

`graphLiteConstraintError()` 함수가 통일된 형식의 에러를 생성한다:
```
Access denied: `<toolName>` is limited to Graph-Lite (module-level, 1-hop queries).
<detail>
Upgrade to Elite or Team for full graph access: <upgradeUrl>
```

---

## 4. 통합 도구 Auto-Hide

### 4.1 AUTO_HIDE_INTEGRATIONS 설정

```typescript
const AUTO_HIDE_INTEGRATIONS = process.env.CONTEXTSTREAM_AUTO_HIDE_INTEGRATIONS !== "false";
```

- 기본값: `true` (활성화).
- `CONTEXTSTREAM_AUTO_HIDE_INTEGRATIONS=false`로 설정 시 비활성화.
- 활성화 시 연결되지 않은 통합 도구는 MCP 도구 목록에서 숨겨진다.

### 4.2 통합 상태 추적

상태 캐시 구조:
```typescript
let integrationStatus: {
  checked: boolean;
  checkedAt: number;
  slack: boolean;
  github: boolean;
  notion: boolean;
  workspaceId?: string;
} = { checked: false, checkedAt: 0, slack: false, github: false, notion: false };
```

**캐시 TTL**: 5분 (`INTEGRATION_CACHE_TTL_MS = 5 * 60 * 1000`).

### 4.3 checkIntegrationStatus()

```typescript
async function checkIntegrationStatus(
  workspaceId?: string
): Promise<{ slack: boolean; github: boolean; notion: boolean }>
```

- 동일 workspaceId에 대해 TTL 내 캐시된 결과를 반환한다.
- workspaceId가 없으면 모든 통합이 `false`로 반환된다.
- `client.integrationsStatus({ workspace_id })` API를 호출하여 각 프로바이더의 상태를 확인한다.
- **연결 상태 판정**: `status === "connected" || status === "syncing"` -- 두 값 모두 "연결됨"으로 간주한다.

### 4.4 updateIntegrationStatus()

```typescript
function updateIntegrationStatus(
  status: { slack: boolean; github: boolean; notion: boolean },
  workspaceId?: string
)
```

- `session_init` 또는 `integrations_status` 도구에서 호출된다.
- **tools/list_changed 알림**: AUTO_HIDE_INTEGRATIONS가 활성화된 상태에서 이전에 미연결이던 통합이 새로 연결되면 `server.sendToolsListChanged()`를 호출하여 클라이언트에 도구 목록 변경을 알린다.
- `toolsListChangedNotified` 플래그로 중복 알림을 방지한다.

### 4.5 gateIfIntegrationTool()

```typescript
async function gateIfIntegrationTool(toolName: string): Promise<ToolTextResult | null>
```

**게이팅 로직 (Lazy Evaluation - Option A):**

1. `AUTO_HIDE_INTEGRATIONS`가 비활성이면 `null` 반환 (통과).
2. 도구가 어떤 통합 집합에도 속하지 않으면 `null` 반환.
3. 현재 세션의 workspaceId를 가져온다.
4. `checkIntegrationStatus(workspaceId)`를 호출한다.
5. 게이팅 결과:
   - **Slack 도구** + Slack 미연결: 에러 (Slack 연결 필요 안내, 설정 URL 제공).
   - **GitHub 도구** + GitHub 미연결: 에러 (GitHub 연결 필요 안내).
   - **Notion 도구** + Notion 미연결: 에러 (Notion 연결 필요 안내).
   - **Cross-integration 도구** + 모든 통합 미연결: 에러 (최소 1개 통합 연결 필요).
6. 각 에러 메시지에는 설정 페이지 URL (`https://contextstream.io/settings/integrations`)이 포함된다.
7. Slack/GitHub 에러 메시지에는 추가 안내가 포함된다: `context_smart`와 `session_smart_search`가 통합 연결 시 자동으로 관련 컨텍스트를 포함한다.

### 4.6 shouldRegisterIntegrationTool()

```typescript
function shouldRegisterIntegrationTool(toolName: string): boolean
```

MCP 서버 시작 시 도구 등록 여부를 결정한다:

- `AUTO_HIDE_INTEGRATIONS`가 비활성이면 항상 `true`.
- 아직 통합 상태 확인 전(`!integrationStatus.checked`)이면 통합 도구가 아닌 경우만 `true`.
- Slack 도구: `integrationStatus.slack`이 `true`일 때만 등록.
- GitHub 도구: `integrationStatus.github`이 `true`일 때만 등록.
- Notion 도구: `integrationStatus.notion`이 `true`일 때만 등록.
- Cross-integration 도구: Slack 또는 GitHub 중 하나라도 연결 시 등록.

---

## 5. 통합 도구 세트 (Integration Tool Sets)

### 5.1 SLACK_TOOLS (9개)

| 도구명 | 설명 |
|--------|------|
| `slack_stats` | Slack 통합 통계 및 개요 |
| `slack_channels` | 채널 목록 및 통계 |
| `slack_search` | Slack 메시지 검색 |
| `slack_discussions` | 고참여 토론 스레드 |
| `slack_activity` | 최근 활동 피드 |
| `slack_contributors` | 상위 기여자 |
| `slack_knowledge` | Slack에서 추출된 지식 (결정, 교훈, 인사이트) |
| `slack_summary` | Slack 요약 |
| `slack_sync_users` | 사용자 프로필 동기화 |

### 5.2 GITHUB_TOOLS (8개)

| 도구명 | 설명 |
|--------|------|
| `github_stats` | GitHub 통합 통계 및 개요 |
| `github_repos` | 리포지토리 통계 |
| `github_search` | GitHub 콘텐츠 검색 |
| `github_issues` | 이슈 및 PR 목록 |
| `github_activity` | 최근 활동 피드 |
| `github_contributors` | 상위 기여자 |
| `github_knowledge` | GitHub에서 추출된 지식 |
| `github_summary` | GitHub 요약 |

### 5.3 NOTION_TOOLS (10개)

| 도구명 | 설명 |
|--------|------|
| `notion_create_page` | Notion 페이지 생성 |
| `notion_list_databases` | 데이터베이스 목록 |
| `notion_search_pages` | 페이지 검색 (스마트 타입 감지 필터링 포함) |
| `notion_get_page` | 특정 페이지 조회 (콘텐츠 포함) |
| `notion_query_database` | 데이터베이스 쿼리 (필터/정렬) |
| `notion_update_page` | 페이지 업데이트 |
| `notion_stats` | Notion 통합 통계 |
| `notion_activity` | 최근 활동 피드 |
| `notion_knowledge` | Notion에서 추출된 지식 |
| `notion_summary` | Notion 요약 |

### 5.4 CROSS_INTEGRATION_TOOLS (4개)

| 도구명 | 설명 |
|--------|------|
| `integrations_status` | 모든 프로바이더의 통합 상태 |
| `integrations_search` | 교차 소스 검색 (Slack + GitHub + Notion) |
| `integrations_summary` | 교차 소스 요약 |
| `integrations_knowledge` | 교차 소스 지식 |

### 5.5 ALL_INTEGRATION_TOOLS

위 4개 집합의 합집합으로, 총 **31개** 통합 도구를 포함한다.

---

## 6. 크레딧 & 일일 제한 (Credits & Daily Limits)

### 6.1 크레딧 잔액 추적

`getCreditBalance()`는 `GET /credits/balance`를 호출하여 다음 정보를 반환한다:
- `plan.name`: 현재 구독 플랜명
- `plan.features`: 플랜 기능 목록
- `plan.graph_tier` / `plan.graphTier`: 그래프 접근 티어
- 크레딧 잔액 및 사용량 정보

결과는 `globalCache`에 `CacheTTL.CREDIT_BALANCE` 기간 동안 캐싱된다.

### 6.2 daily_limit_exceeded 처리

파일 인덱싱(`ingestLocal()`) 과정에서 API가 반환하는 상태 값에 따라 동작이 달라진다:

| API 상태 | 동작 |
|---------|------|
| `"completed"` | 정상 완료, 다음 배치 계속 |
| `"partial"` | 부분 완료, 다음 배치 계속 |
| `"up_to_date"` | 이미 최신, 다음 배치 계속 |
| `"cooldown"` | 쿨다운 상태, 배치 처리 즉시 중단 (`abortedEarly = true`) |
| `"daily_limit_exceeded"` | 일일 한도 초과, 배치 처리 즉시 중단 (`abortedEarly = true`) |

### 6.3 쿨다운 상태

`ingestLocal()` 결과의 `status` 필드:

```typescript
interface IngestLocalResult {
  status: "success" | "cooldown" | "daily_limit" | "partial" | "error";
  abortedEarly: boolean;
  // ...
}
```

- `"cooldown"`: 서버 측 쿨다운으로 인해 조기 중단.
- `"daily_limit"`: 일일 한도 초과로 인해 조기 중단.
- 두 경우 모두 해시 매니페스트는 갱신된다 (이미 전송된 파일의 해시는 유효하므로).

### 6.4 플랜 제한 에러

도구 호출 시 API가 `FORBIDDEN` + `"plan limit reached"` 에러를 반환하면, `safeHandler`의 에러 핸들러가 자동으로 업그레이드 URL을 포함한 힌트를 추가한다:
```typescript
const isPlanLimit =
  String(errorCode).toUpperCase() === "FORBIDDEN" &&
  String(errorMessage).toLowerCase().includes("plan limit reached");
const upgradeHint = isPlanLimit ? `\nUpgrade: ${upgradeUrl}` : "";
```

---

## 7. 티어별 기능 매트릭스

### 7.1 도구 접근 매트릭스

| 기능/도구 | Free | Pro | Elite | Team/Enterprise |
|-----------|------|-----|-------|-----------------|
| **기본 도구** (session_init, search, memory, workspace, project) | O | O | O | O |
| **AI 엔드포인트** (ai_context, ai_plan, ai_tasks 등 6개) | X | O | O | O |
| **Slack 통합** (7개) | X | O | O | O |
| **GitHub 통합** (6개) | X | O | O | O |
| **Notion 통합** (10개) | X | O | O | O |
| **미디어** (media_index, media_search) | X | O | O | O |
| **Graph-Lite** (graph_dependencies, graph_impact, graph_ingest) | X* | O | O | O |
| **Graph-Full** (graph_related, graph_path, graph_decisions 등 7개) | X | X | O | O |
| **팀 관리** (team_overview, team_members 등) | X | X | X | O |

> *주: `getGraphTier()`의 코드에서 Free 티어도 `"lite"`를 반환하지만, Pro 도구 게이팅(`gateIfProTool`)이 먼저 적용되므로 Free 사용자는 사실상 그래프 도구에 접근할 수 없다. 이는 소스 코드에 `"Free has same graph access as Pro, just limited operations"` 주석이 달려 있으나, 실제로는 gateIfProTool이 선행하므로 차단된다.

### 7.2 그래프 기능 제약 매트릭스

| 그래프 기능 | Free | Pro (Lite) | Elite/Team (Full) |
|------------|------|------------|-------------------|
| graph_dependencies (모듈 레벨, depth=1) | X | O | O |
| graph_dependencies (함수/타입 레벨) | X | X | O |
| graph_dependencies (depth > 1) | X | X | O |
| graph_dependencies (include_transitive) | X | X | O |
| graph_impact (모듈 레벨, depth=1) | X | O | O |
| graph_impact (함수/타입 레벨) | X | X | O |
| graph_impact (depth > 1) | X | X | O |
| graph_ingest | X | O | O |
| graph_related | X | X | O |
| graph_path | X | X | O |
| graph_call_path | X | X | O |
| graph_decisions | X | X | O |
| graph_circular_dependencies | X | X | O |
| graph_unused_code | X | X | O |
| graph_contradictions | X | X | O |

### 7.3 통합 기능 매트릭스

| 통합 기능 | 연결 필수 | Auto-Hide | 게이팅 레벨 |
|-----------|----------|-----------|-------------|
| Slack 도구 (9개) | Slack 연결 | O | Pro + Slack 연결 |
| GitHub 도구 (8개) | GitHub 연결 | O | Pro + GitHub 연결 |
| Notion 도구 (10개) | Notion 연결 | O | Pro + Notion 연결 |
| Cross-integration (4개) | 최소 1개 | O | 최소 1개 통합 연결 |

### 7.4 팀 상태 추적

팀 플랜 상태는 별도로 캐싱된다:

```typescript
let teamStatus: {
  checked: boolean;
  isTeamPlan: boolean;
} = { checked: false, isTeamPlan: false };
```

- `checkTeamStatus()`: `client.isTeamPlan()`을 호출하고 결과를 캐싱한다.
- `updateTeamStatus(isTeam)`: `session_init`에서 호출하여 상태를 갱신한다.
- `isTeamPlanCached()`: 캐시된 팀 상태를 동기적으로 반환한다 (미확인 시 `false`).

---

## 8. 게이팅 실행 순서

모든 도구 호출은 `safeHandler` 래퍼를 통해 다음 순서로 게이팅을 수행한다:

```
1. gateIfProTool(name)         -- Free 사용자의 Pro 도구 접근 차단
2. gateIfIntegrationTool(name) -- 미연결 통합 도구 접근 차단
3. handler(input, extra)       -- 실제 도구 로직 실행
   (내부에서 gateIfGraphTool 호출 가능)
```

`gateIfGraphTool()`은 개별 도구 핸들러 내부에서 호출되며, Pro 게이팅을 먼저 통과한 후에만 실행된다.

---

## 9. 환경변수 요약

| 환경변수 | 기본값 | 설명 |
|---------|-------|------|
| `CONTEXTSTREAM_PRO_TOOLS` | (없음 -- defaultProTools 사용) | Pro 전용 도구 목록 오버라이드 (쉼표 구분) |
| `CONTEXTSTREAM_AUTO_HIDE_INTEGRATIONS` | `"true"` | 미연결 통합 도구 자동 숨김 |
| `CONTEXTSTREAM_UPGRADE_URL` | `"https://contextstream.io/pricing"` | 업그레이드 안내 URL |
