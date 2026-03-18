# memctl -- 컨텍스트 주입 분석

> 분석 대상 소스:
> - `packages/cli/src/tools/handlers/context.ts` (1190 lines)
> - `packages/cli/src/agent-context.ts` (527 lines)
> - `packages/cli/src/hook-adapter.ts` (689 lines)
> - `packages/cli/src/intent.ts` (187 lines)
> - `packages/cli/src/tools/response.ts` (67 lines)

---

## 1. 컨텍스트 부트스트랩 플로우

### 1.1 handleBootstrap() 전체 Step-by-Step

`handleBootstrap()`는 세션 시작 시 에이전트에게 프로젝트의 전체 컨텍스트를 전달하는 핵심 함수이다. `context.ts:206-338`에 정의되어 있으며, 다음과 같은 단계를 순차적/병렬적으로 수행한다.

```
SessionStart
  |
  v
[Phase 1] Fire-and-forget maintenance (500ms timeout)
  |
  v
[Phase 2] Parallel fetches (4개 동시)
  |       +-- listAllMemories(client)
  |       +-- getBranchInfo()
  |       +-- client.getMemoryCapacity()
  |       +-- getAllContextTypeInfo(client)
  |
  v
[Phase 3] await maintenancePromise (Race: cleanup vs 500ms)
  |
  v
[Phase 4] extractAgentContextEntries(allMemories)
  |
  v
[Phase 5] Branch filtering + type organization
  |
  v
[Phase 6] Branch plan lookup (optional)
  |
  v
[Phase 7] Capacity guidance + org defaults hint
  |
  v
[Phase 8] JSON 응답 조립 -> textResponse()
```

#### Phase 1: Maintenance (Fire-and-Forget)

```typescript
// context.ts:215-246
const cleanupPromise = (async () => {
  // 1. cleanup_policy 확인 (autoCleanupOnBootstrap === false 면 스킵)
  const POLICY_KEY = "agent/config/cleanup_policy";
  // ...
  // 2. 4가지 lifecycle 작업 실행
  return await client.runLifecycle(
    [
      "cleanup_expired",
      "cleanup_session_logs",
      "auto_archive_unhealthy",
      "cleanup_expired_locks",
    ],
    { healthThreshold: 15 },
  );
})();
const cleanupTimeout = new Promise<null>((resolve) =>
  setTimeout(() => resolve(null), 500),
);
const maintenancePromise = Promise.race([cleanupPromise, cleanupTimeout]);
```

| Lifecycle 작업 | 목적 |
|---|---|
| `cleanup_expired` | TTL이 만료된 메모리 삭제 |
| `cleanup_session_logs` | 오래된 세션 로그 정리 |
| `auto_archive_unhealthy` | healthThreshold(15) 미만인 메모리 자동 아카이브 |
| `cleanup_expired_locks` | 만료된 잠금(lock) 해제 |

핵심 설계: `Promise.race`로 cleanup이 500ms 내에 완료되지 않으면 `null`을 반환하고 부트스트랩을 계속 진행한다. 사용자 체감 지연을 최소화하는 전략이다.

#### Phase 2: 병렬 데이터 페치

```typescript
// context.ts:248-253
const [allMemories, branchInfo, capacity, allTypeInfo] = await Promise.all([
  listAllMemories(client),    // 전체 메모리 목록 (최대 2000개, 100개씩 페이지네이션)
  getBranchInfo(),             // git rev-parse로 현재 branch/commit/dirty 상태
  client.getMemoryCapacity().catch(() => null),  // 용량 정보 (실패 시 null)
  getAllContextTypeInfo(client),  // builtin 12개 + custom types 병합
]);
```

4개의 비동기 요청을 `Promise.all`로 동시에 발사한다. `getMemoryCapacity()`는 실패해도 전체 부트스트랩을 중단시키지 않도록 `.catch(() => null)` 처리되어 있다.

#### Phase 3: Maintenance 결과 수거

```typescript
// context.ts:255
const maintenance = await maintenancePromise;
```

Phase 1에서 시작한 cleanup의 결과 (또는 timeout으로 인한 `null`)를 수거한다.

#### Phase 4: 엔트리 추출

```typescript
// context.ts:257
const entries = extractAgentContextEntries(allMemories);
```

전체 메모리 레코드에서 `agent/context/<type>/<id>` 패턴의 키만 필터링하여 `AgentContextEntry[]`로 변환한다 (상세 분석은 섹션 3 참조).

#### Phase 5: Branch Filtering + Type Organization

```typescript
// context.ts:258-300
const allTypeSlugs = Object.keys(allTypeInfo);
const selectedTypes = types ?? allTypeSlugs;
const branchFilter =
  branch ??
  (branchInfo?.branch &&
  branchInfo.branch !== "main" &&
  branchInfo.branch !== "master"
    ? branchInfo.branch
    : null);
const branchTag = branchFilter ? `branch:${branchFilter}` : null;
```

Branch 필터링 규칙:

| 조건 | branchFilter 값 |
|---|---|
| `params.branch`가 명시적으로 전달됨 | 해당 branch 이름 |
| 현재 branch가 `main` 또는 `master` | `null` (필터 없음) |
| 현재 branch가 feature branch | 해당 branch 이름 |

타입별 엔트리 조직화:

```typescript
const functionalityTypes = selectedTypes.map((type) => {
  let typeEntries = entries.filter((e) => e.type === type);
  if (branchTag) {
    typeEntries = typeEntries.sort((a, b) => {
      const aHasBranch = a.tags.includes(branchTag!) ? 1 : 0;
      const bHasBranch = b.tags.includes(branchTag!) ? 1 : 0;
      if (aHasBranch !== bHasBranch) return bHasBranch - aHasBranch;
      return b.priority - a.priority;
    });
  }
  // ...
});
```

정렬 우선순위:
1. 현재 branch 태그가 있는 엔트리가 최상위
2. 동일 branch 여부 내에서는 priority 내림차순

#### Phase 6-8: Branch Plan, Capacity, 응답 조립

```typescript
// context.ts:302-337
let branchPlan = null;
if (branchInfo?.branch) {
  const planKey = buildBranchPlanKey(branchInfo.branch);
  branchPlan = await client.getMemory(planKey).catch(() => null);
}

const memoryStatus = capacity
  ? { ...capacity, guidance: formatCapacityGuidance(capacity) }
  : null;

// org defaults hint (entries가 0일 때만)
```

최종 응답 JSON 구조:

```json
{
  "functionalityTypes": [
    {
      "type": "coding_style",
      "label": "Coding Style",
      "description": "...",
      "count": 3,
      "items": [
        {
          "id": "naming-conventions",
          "title": "Naming Conventions",
          "key": "agent/context/coding_style/naming-conventions",
          "priority": 80,
          "tags": ["branch:feature-x"],
          "isPinned": false,
          "scope": "project",
          "updatedAt": "2026-03-15T...",
          "content": "..."
        }
      ]
    }
  ],
  "currentBranch": { "branch": "feature-x", "commit": "abc123", "dirty": false },
  "branchPlan": null,
  "memoryStatus": { "used": 45, "limit": 200, "guidance": "Memory available..." },
  "availableTypes": ["coding_style", "folder_structure", ...],
  "orgDefaultsHint": null,
  "maintenance": null
}
```

### 1.2 handleBootstrapCompact()

`bootstrap_compact` (`context.ts:340-421`)은 `bootstrap`의 경량 버전이다. 주요 차이점:

| 항목 | bootstrap | bootstrap_compact |
|---|---|---|
| content 포함 | `includeContent` 파라미터로 제어 (기본 true) | content 미포함, `contentLength`만 제공 |
| feedbackScore | 미포함 | `helpfulCount - unhelpfulCount` 계산 |
| 용도 | 세션 첫 시작 시 전체 컨텍스트 로드 | compaction 후 재로드, 또는 토큰 절약이 필요할 때 |
| hint | 없음 | `"Use context functionality_get to load full content."` |

응답에 `mode: "compact"` 필드가 추가되며, `types`는 count > 0인 것만 포함한다.

---

## 2. Agent Context 타입 시스템

### 2.1 12개 Built-in Types

`agent-context.ts:9-22`에 정의된 `BUILTIN_AGENT_CONTEXT_TYPES`:

| Slug | Label | Description | 용도 |
|---|---|---|---|
| `coding_style` | Coding Style | Conventions, naming rules, formatting, and review expectations | 코드 스타일 가이드, 네이밍 규칙 |
| `folder_structure` | Folder Structure | How the repository is organized and where core domains live | 디렉터리 구조 설명 |
| `file_map` | File Map | Quick index of where to find key features, APIs, and configs | 주요 파일 위치 인덱스 |
| `architecture` | Architecture | Core system design, module boundaries, and data flow decisions | 시스템 아키텍처 설계 |
| `workflow` | Workflow | Branching, PR flow, deployment process, and team working norms | 개발 프로세스, CI/CD |
| `testing` | Testing | Test strategy, required checks, and where tests are located | 테스트 전략 및 위치 |
| `branch_plan` | Branch Plan | What needs to be implemented in a specific git branch | branch별 구현 계획 |
| `constraints` | Constraints | Hard requirements, non-goals, and safety limits for agent changes | 에이전트 제약사항 |
| `lessons_learned` | Lessons Learned | Pitfalls, gotchas, and negative knowledge | 실패 경험, 회피 사항 |
| `user_ideas` | User Ideas | Feature requests, enhancement ideas | 사용자 아이디어/요청 |
| `known_issues` | Known Issues | Known bugs, workarounds, environment gotchas, and flaky behavior | 알려진 버그/workaround |
| `decisions` | Decisions | Explicit design decisions with rationale and alternatives considered | 설계 결정 및 근거 |

### 2.2 Custom Types

`agent-context.ts:96-157`에 정의된 custom type 시스템:

```typescript
export interface CustomContextType {
  slug: string;
  label: string;
  description: string;
  schema?: string | null;
  icon?: string | null;
}
```

Custom type은 서버 API (`client.listContextTypes()`)를 통해 동적으로 로드되며, 60초 TTL 캐시가 적용된다:

```typescript
let cachedCustomTypes: CustomContextType[] | null = null;
let customTypesCacheTime = 0;
const CUSTOM_TYPES_CACHE_TTL = 60_000; // 1 minute
```

`getAllContextTypeInfo()` (`agent-context.ts:139-150`)는 builtin과 custom을 병합하여 반환한다:

```typescript
export async function getAllContextTypeInfo(client: ApiClient) {
  const customTypes = await getCustomContextTypes(client);
  const all = { ...AGENT_CONTEXT_TYPE_INFO };
  for (const ct of customTypes) {
    all[ct.slug] = { label: ct.label, description: ct.description };
  }
  return all;
}
```

Custom type의 slug이 builtin type과 동일하면 custom이 builtin을 오버라이드한다 (spread 순서상).

### 2.3 Type alias 호환성

```typescript
// agent-context.ts:28-29
export const AGENT_CONTEXT_TYPES = BUILTIN_AGENT_CONTEXT_TYPES;
export type AgentContextType = string; // Now accepts both built-in and custom types
```

`AgentContextType`이 `string`으로 확장되어 있어 custom type slug도 타입 안전하게 사용할 수 있다.

---

## 3. 컨텍스트 엔트리 추출

### 3.1 extractAgentContextEntries()

`agent-context.ts:309-341`에 정의된 이 함수는 전체 `MemoryRecord[]`에서 에이전트 컨텍스트 엔트리만 추출한다.

```
MemoryRecord[]
  |
  v
[각 record에 대해]
  |
  +-- parseAgentContextKey(memory.key)
  |     +-- key를 "/" 로 split
  |     +-- parts.length === 4 확인
  |     +-- parts[0] === "agent" && parts[1] === "context" 확인
  |     +-- { type: parts[2], id: parts[3] } 반환
  |     +-- 실패시 null -> skip
  |
  +-- parseMetadata(memory.metadata)
  |     +-- string이면 JSON.parse 시도
  |     +-- object이면 그대로 사용
  |     +-- 실패시 null
  |
  +-- parseTags(memory.tags)
  |     +-- string이면 JSON.parse (배열 기대)
  |     +-- Array이면 그대로 사용
  |     +-- 실패시 []
  |
  +-- title 결정
  |     +-- metadata.title이 유효한 string이면 사용
  |     +-- 아니면 parsed.id를 title로 사용
  |
  +-- AgentContextEntry 생성
  |
  v
priority 내림차순 정렬
  |
  v
AgentContextEntry[]
```

### 3.2 Key 구조 분석

키 형식: `agent/context/<type>/<id>`

```
agent/context/coding_style/naming-conventions
  ^     ^         ^              ^
  |     |         |              |
  |     |         |              +-- id (slugified)
  |     |         +-- type (builtin 또는 custom slug)
  |     +-- "context" 고정 prefix
  +-- "agent" 고정 prefix
```

`parseAgentContextKey()` (`agent-context.ts:217-226`)는 정확히 4개의 "/" 분리 세그먼트를 요구한다:

```typescript
export function parseAgentContextKey(key: string) {
  const parts = key.split("/");
  if (parts.length !== 4) return null;
  if (parts[0] !== "agent" || parts[1] !== "context") return null;
  const type = parts[2];
  if (!type) return null;
  const id = parts[3];
  if (!id) return null;
  return { type, id };
}
```

### 3.3 ID 정규화

`normalizeAgentContextId()` (`agent-context.ts:199-207`):

```typescript
export function normalizeAgentContextId(value: string) {
  return value
    .trim()
    .toLowerCase()
    .replace(/\s+/g, "-")           // 공백 -> 하이픈
    .replace(/[^a-z0-9._%-]/g, "-") // 허용 문자 외 -> 하이픈
    .replace(/-+/g, "-")            // 연속 하이픈 축소
    .replace(/^-|-$/g, "");         // 양쪽 하이픈 제거
}
```

허용 문자: `a-z`, `0-9`, `.`, `_`, `%`, `-`

예시 변환:

| 입력 | 출력 |
|---|---|
| `"Naming Conventions"` | `"naming-conventions"` |
| `"API v2 Design"` | `"api-v2-design"` |
| `"__test__"` | `"test"` |
| `"feature/auth"` | `"feature-auth"` |

### 3.4 MemoryRecord 인터페이스

`agent-context.ts:167-184`에 정의된 전체 구조:

```typescript
export interface MemoryRecord {
  key: string;
  content?: string | null;
  metadata?: unknown;
  scope?: string;
  priority?: number | null;
  tags?: string | null;          // JSON 문자열 (["tag1", "tag2"])
  relatedKeys?: string | null;   // JSON 문자열 (["key1", "key2"])
  pinnedAt?: unknown;
  archivedAt?: unknown;
  expiresAt?: unknown;
  accessCount?: number;
  lastAccessedAt?: unknown;
  helpfulCount?: number;
  unhelpfulCount?: number;
  updatedAt?: unknown;
  createdAt?: unknown;
}
```

### 3.5 AgentContextEntry 인터페이스

`agent-context.ts:186-197`에 정의:

```typescript
export interface AgentContextEntry {
  type: string;      // context type slug
  id: string;        // normalized ID
  key: string;       // 전체 키 (agent/context/type/id)
  title: string;     // metadata.title 또는 id
  content: string;   // 메모리 본문
  metadata: Record<string, unknown> | null;
  priority: number;  // 0-100 정수
  tags: string[];    // 파싱된 태그 배열
  updatedAt: unknown;
  createdAt: unknown;
}
```

### 3.6 listAllMemories() 캐싱

`agent-context.ts:266-307`에 정의된 TTL 기반 캐시:

```typescript
let cachedAllMemories: MemoryRecord[] | null = null;
let allMemoriesCacheTime = 0;
const ALL_MEMORIES_CACHE_TTL = 5_000; // 5 seconds
```

페이지네이션 로직: 100개씩 최대 2000개까지 반복 fetch한다.

```
[요청] listAllMemories(client, maxMemories=2000)
  |
  +-- 캐시 유효? (5초 TTL) -> 캐시된 결과 반환
  |
  +-- 캐시 무효 -> 페이지네이션 시작
        |
        +-- offset=0, pageSize=100
        +-- batch.length < pageSize 이면 종료
        +-- all.length >= maxMemories 이면 종료
        +-- 결과를 캐시에 저장
```

---

## 4. MEMCTL_REMINDER

### 4.1 리마인더 전문

`hook-adapter.ts:49`에 정의된 단일 문자열:

```
Use memctl MCP tools for ALL persistent memory. Do NOT use built-in auto memory
or MEMORY.md files. Do NOT store code, git output, file contents, or command
results in memory. Session start: context action=bootstrap, activity
action=memo_read, branch action=get. Before editing: context action=context_for
filePaths=[files], context action=smart_retrieve intent=<what you need>. Store
decisions/lessons/issues: context action=functionality_set type=<type> id=<id>
content=<content>. Search before storing: memory action=search query=<query>.
MANDATORY SESSION END: After fully responding to the user, you MUST run:
1) activity action=memo_leave message=<handoff note>,
2) session action=end summary=<what was accomplished, key decisions, open
questions, files modified>. Never skip session end. Never store code or
transient data.
```

### 4.2 리마인더 구조 분석

이 리마인더는 에이전트의 행동을 강제하는 지시문 집합이다:

| 섹션 | 지시 내용 | 목적 |
|---|---|---|
| 금지 사항 | built-in auto memory, MEMORY.md 사용 금지 | memctl을 유일한 메모리 채널로 강제 |
| 금지 사항 | code, git output, file contents 저장 금지 | 불필요한 대용량 데이터 저장 방지 |
| 세션 시작 | `context bootstrap`, `activity memo_read`, `branch get` | 부트스트랩 3-call 패턴 강제 |
| 편집 전 | `context_for filePaths=[...]`, `smart_retrieve intent=...` | 관련 컨텍스트 조회 후 편집 강제 |
| 저장 | `functionality_set type=<type> id=<id>` | 구조화된 저장 강제 |
| 중복 방지 | `memory search query=<query>` 먼저 실행 | 검색 후 저장 패턴 강제 |
| 세션 종료 | `activity memo_leave` + `session end` 필수 | 핸드오프 노트와 세션 요약 강제 |

### 4.3 주입 시점

MEMCTL_REMINDER는 다음 3가지 시점에 주입된다:

| 시점 | Phase | 메커니즘 |
|---|---|---|
| 세션 시작 | `start` | stdout으로 직접 출력 (`printf '%s' "${MEMCTL_REMINDER}"`) |
| 사용자 프롬프트 제출 | `user` | `hookSpecificOutput.additionalContext`에 삽입 |
| Compaction 후 | `compact` | stdout으로 직접 재주입 |

`user` phase에서의 주입 형식 (hook-adapter.ts:172):

```bash
printf '{"hookSpecificOutput":{"hookEventName":"UserPromptSubmit",
  "additionalContext":"%s"}}' "$(json_escape "${MEMCTL_REMINDER}")"
```

이는 Claude Code의 hook 프로토콜에서 정의한 `hookSpecificOutput` 형식을 따른다. `additionalContext` 필드의 값은 에이전트의 system context에 삽입된다.

### 4.4 Compaction 시 재주입의 중요성

Context compaction(대화 압축)이 발생하면 이전에 주입된 REMINDER가 손실될 수 있다. `compact` phase에서 재주입함으로써 에이전트가 compaction 후에도 memctl 사용 패턴을 유지하도록 보장한다.

```
hooks.json에서:
"SessionStart" -> [
  { hooks: [{ command: "...dispatch.sh start" }] },
  { matcher: "compact", hooks: [{ command: "...dispatch.sh compact" }] }
]
```

`matcher: "compact"`는 SessionStart 이벤트 중 compaction에 의해 트리거된 경우를 선택적으로 처리한다.

---

## 5. Hook Adapter

### 5.1 DISPATCHER_SCRIPT 구조

`hook-adapter.ts:51-211`에 정의된 bash 스크립트이다. 이 스크립트는 `.memctl/hooks/memctl-hook-dispatch.sh`에 기록되며, Claude Code의 hook 시스템에서 실행된다.

#### 스크립트 전체 아키텍처

```
memctl-hook-dispatch.sh <phase>
  |
  +-- set -euo pipefail
  +-- PHASE 인자 파싱
  +-- ROOT_DIR 결정 (기본: .memctl/hooks)
  +-- 유틸리티 함수 정의
  |     +-- read_payload()     -- stdin에서 JSON 페이로드 읽기
  |     +-- json_escape()      -- JSON 문자열 이스케이프
  |     +-- extract_json_field() -- jq 또는 grep fallback으로 JSON 필드 추출
  |     +-- ensure_session_id()  -- 세션 ID 확보
  |     +-- send_hook_payload()  -- memctl hook --stdin으로 페이로드 전송
  +-- MEMCTL_REMINDER 상수 삽입
  +-- case문으로 phase 분기
```

#### extract_json_field() 상세

이 함수는 JSON 페이로드에서 phase별로 다른 키를 우선순위대로 탐색한다:

| Phase | 탐색 키 순서 |
|---|---|
| `user` | `prompt` -> `user_message` -> `message` -> `text` -> `content` |
| `assistant` | `response` -> `assistant_response` -> `output` -> `text` -> `content` |
| `summary` | `summary` -> `message` -> `reason` -> `text` -> `content` |

Fast path: `jq`가 설치되어 있으면 단일 jq 표현식으로 추출한다.
Fallback: `grep`/`sed` 조합으로 단순 JSON에서 추출한다.

#### ensure_session_id() 세션 관리

```
ensure_session_id()
  |
  +-- MEMCTL_SESSION_ID 환경변수 존재? -> 파일에 기록, 반환
  |
  +-- .memctl/hooks/session_id 파일 존재? -> 파일 내용 반환
  |
  +-- memctl hook start 실행 -> sessionId 추출 -> 파일에 기록
```

### 5.2 Phase별 처리 상세

#### start Phase

```bash
start)
  session_id="$(ensure_session_id)"
  printf '%s' "${MEMCTL_REMINDER}"
  ;;
```

- 세션 ID를 확보하고 파일에 기록
- MEMCTL_REMINDER를 stdout으로 출력 (Claude의 system context에 삽입됨)

#### user Phase

```bash
user)
  session_id="$(ensure_session_id)"
  # Reminder 재주입 (hookSpecificOutput 형식)
  printf '{"hookSpecificOutput":{"hookEventName":"UserPromptSubmit",
    "additionalContext":"%s"}}' "$(json_escape "${MEMCTL_REMINDER}")"
  # 사용자 메시지를 API로 전송 (background)
  user_message="$(extract_json_field "${payload}" "user")"
  if [[ -n "${user_message}" ]]; then
    send_hook_payload "{\"action\":\"turn\",\"sessionId\":\"...\",
      \"userMessage\":\"...\"}" &
  fi
  ;;
```

핵심: API 전송은 `&`로 백그라운드에서 수행하여 사용자 대기 시간을 최소화한다.

#### assistant Phase

```bash
assistant)
  session_id="$(ensure_session_id)"
  assistant_message="$(extract_json_field "${payload}" "assistant")"
  if [[ -n "${assistant_message}" ]]; then
    send_hook_payload "{\"action\":\"turn\",\"sessionId\":\"...\",
      \"assistantMessage\":\"...\"}" &
  fi
  ;;
```

- Assistant 응답 텍스트를 추출하여 `memctl hook --stdin`으로 전송
- 역시 백그라운드 실행

#### compact Phase

```bash
compact)
  printf '%s' "${MEMCTL_REMINDER}"
  ;;
```

- Compaction 후 리마인더만 재주입
- 세션 ID나 API 호출 없음

#### end Phase

```bash
end)
  session_id="$(ensure_session_id)"
  summary="$(extract_json_field "${payload}" "summary")"
  # 세션 종료 payload 전송 (foreground - 완료 보장)
  send_hook_payload "{\"action\":\"end\",\"sessionId\":\"...\",
    \"summary\":\"...\"}"
  rm -f "${SESSION_FILE}"
  ;;
```

- 세션 종료 시에는 foreground에서 전송 (완료 보장)
- 세션 ID 파일 삭제

### 5.3 지원 에이전트 목록

`hook-adapter.ts:34-47`에 정의:

| Agent | Hook 지원 | MCP 설정 파일 |
|---|---|---|
| `claude` | native hooks.json | `claude.settings.local.json` |
| `cursor` | MCP만 | `cursor.mcp.json` |
| `windsurf` | MCP만 | `windsurf.mcp_config.json` |
| `vscode` | MCP만 | `vscode.mcp.json` (servers 형식) |
| `continue` | MCP만 | `continue.config.yaml` |
| `zed` | MCP만 | `zed.settings.json` (context_servers) |
| `codex` | MCP만 | `codex.config.toml` |
| `cline` | MCP만 | `cline.mcp.json` |
| `roo` | MCP만 | `roo.mcp.json` |
| `amazonq` | MCP만 | `amazonq.mcp.json` |
| `opencode` | 플러그인 기반 | `opencode.mcp.json` |
| `generic` | 수동 | 수동 dispatch 안내 |

Claude만이 네이티브 hook 이벤트 API를 가지며, 나머지는 MCP 서버만 제공하고 hook은 수동/외부 자동화로 처리해야 한다.

### 5.4 hooks.json 이벤트 매핑

`plugins/memctl/hooks/hooks.json`에 정의:

```json
{
  "hooks": {
    "SessionStart":     [{ hooks: [{ command: "...dispatch.sh start" }] },
                         { matcher: "compact", hooks: [{ command: "...dispatch.sh compact" }] }],
    "UserPromptSubmit": [{ hooks: [{ command: "...dispatch.sh user" }] }],
    "Stop":             [{ hooks: [{ command: "...dispatch.sh assistant" }] }],
    "SessionEnd":       [{ hooks: [{ command: "...dispatch.sh end" }] }]
  }
}
```

| Claude 이벤트 | Dispatch Phase | 설명 |
|---|---|---|
| `SessionStart` | `start` | 세션 시작, REMINDER 주입 |
| `SessionStart` (compact) | `compact` | Compaction 후 REMINDER 재주입 |
| `UserPromptSubmit` | `user` | 사용자 입력 캡처 + REMINDER 재주입 |
| `Stop` | `assistant` | Assistant 응답 캡처 |
| `SessionEnd` | `end` | 세션 종료 처리 |

### 5.5 AdapterBundle 생성과 디스크 기록

`getAdapterBundle()` (`hook-adapter.ts:614-634`)는 선택된 에이전트(들)에 대해 필요한 파일 목록을 구성한다:

```typescript
function getAdapterBundle(agent: Agent, dir: string): AdapterBundle {
  const targetAgents = agent === "all" ? SUPPORTED_AGENTS : [agent];
  const byPath = new Map<string, AdapterFile>();

  // 공통: dispatcher script (항상 포함)
  const baseFile: AdapterFile = {
    path: join(dir, DISPATCHER_FILENAME),
    content: DISPATCHER_SCRIPT,
    executable: true,
  };
  byPath.set(baseFile.path, baseFile);

  // 에이전트별 preset 파일 추가
  for (const target of targetAgents) {
    const files = getPresetFiles(target, dir);
    for (const file of files) byPath.set(file.path, file);
  }
  return { agent, files: [...byPath.values()] };
}
```

`writeBundle()` (`hook-adapter.ts:636-649`)는 `--write` 플래그가 전달되었을 때만 실제로 파일을 디스크에 기록한다. `executable: true`인 파일(dispatcher)은 `chmod 0o755`로 실행 권한이 설정된다.

---

## 6. Delta Sync

### 6.1 handleBootstrapDelta()

`context.ts:423-446`에 정의된 증분 동기화 메커니즘이다.

```typescript
async function handleBootstrapDelta(client: ApiClient, params: Record<string, unknown>) {
  const since = params.since as number;
  if (!since) return errorResponse("Missing param", "since required");
  const delta = await client.getDelta(since);
  return textResponse(JSON.stringify({
    created: delta.created.length,
    updated: delta.updated.length,
    deleted: delta.deleted.length,
    since: new Date(delta.since).toISOString(),
    now: new Date(delta.now).toISOString(),
    createdMemories: delta.created,
    updatedMemories: delta.updated,
    deletedKeys: delta.deleted,
  }, null, 2));
}
```

동작 흐름:

```
에이전트 -> context action=bootstrap_delta since=<timestamp>
  |
  v
client.getDelta(since)
  |
  v
서버 응답:
  +-- created: 새로 생성된 메모리 목록
  +-- updated: 변경된 메모리 목록
  +-- deleted: 삭제된 메모리 키 목록
  +-- since: 요청된 기준 시점
  +-- now: 현재 서버 시간
```

사용 시나리오:
- 긴 세션에서 다른 에이전트나 사용자가 메모리를 변경했을 때
- 전체 bootstrap을 다시 수행하지 않고 변경 사항만 가져옴
- `since` 파라미터는 unix timestamp (밀리초)

---

## 7. Smart Retrieve

### 7.1 handleSmartRetrieve() 의도 기반 검색

`context.ts:910-1038`에 정의된 의도(intent) 기반 컨텍스트 검색 시스템이다.

#### 전체 파이프라인

```
intent 문자열
  |
  v
[1] classifySearchIntent(intent) -- intent.ts
  |    +-- intent 유형 판별 (entity/temporal/relationship/aspect/exploratory)
  |    +-- confidence 점수 산출
  |    +-- extractedTerms 추출
  |    +-- suggestedTypes 제안
  |
  v
[2] getIntentWeights(classification.intent) -- intent.ts
  |    +-- ftsBoost, vectorBoost, recencyBoost, priorityBoost, graphBoost
  |
  v
[3] 전체 메모리 스코어링
  |    +-- keyword 매칭 (ftsBoost 적용)
  |    +-- file path 매칭
  |    +-- priority 가중치 (priorityBoost 적용)
  |    +-- pinned 보너스 (+10)
  |    +-- accessCount 보너스 (최대 10)
  |    +-- recency 가중치 (recencyBoost 적용)
  |    +-- feedback score (helpful - unhelpful) * 2
  |    +-- graph boost (relatedKeys 존재 시)
  |    +-- suggestedTypes 매칭 (+8)
  |
  v
[4] 상위 maxResults개 선택 (기본 5)
  |
  v
[5] followLinks가 true면 relatedKeys를 수집하여 bulk fetch
  |
  v
[6] 응답 조립
```

### 7.2 Intent 분류 시스템

`intent.ts:110-182`에 정의된 `classifySearchIntent()`:

| Intent | 판별 기준 | 예시 쿼리 |
|---|---|---|
| `entity` | 경로(`/` 포함), 단일 PascalCase/snake_case 단어, 파일 확장자, 3단어 이하 | `"src/api/auth"`, `"UserService"`, `"config.yaml"` |
| `temporal` | `recently`, `latest`, `last week`, `changed`, `updated`, `since` 등 | `"recently updated testing rules"` |
| `relationship` | `related to`, `depends on`, `connected`, `linked`, `affects` 등 | `"related to auth middleware"` |
| `aspect` | `conventions`, `rules`, `how to`, `best practice`, `strategy` 등 | `"testing conventions for API routes"` |
| `exploratory` | 위 어느 것에도 해당하지 않음 | `"what are the main components"` |

### 7.3 Intent별 Weight 프로필

`intent.ts:26-62`에 정의:

| Intent | ftsBoost | vectorBoost | recencyBoost | priorityBoost | graphBoost |
|---|---|---|---|---|---|
| `entity` | 2.0 | 0.5 | 0.3 | 1.0 | 0 |
| `temporal` | 0.7 | 0.5 | **3.0** | 0.5 | 0 |
| `relationship` | 0.5 | 1.5 | 1.0 | 1.0 | **2.0** |
| `aspect` | 1.0 | 1.5 | 0.5 | **1.5** | 0 |
| `exploratory` | 1.0 | 1.2 | 1.0 | 1.0 | 0 |

설계 의도:
- `entity` 검색: 키워드 정확 매칭에 가장 높은 가중치 (ftsBoost=2.0)
- `temporal` 검색: 최근성에 가장 높은 가중치 (recencyBoost=3.0)
- `relationship` 검색: 그래프 연결에 가장 높은 가중치 (graphBoost=2.0)
- `aspect` 검색: priority에 가장 높은 가중치 (priorityBoost=1.5)

### 7.4 스코어링 공식 상세

`context.ts:934-984`의 스코어링 로직을 공식으로 표현하면:

```
score =
  (matchedKeywords.length * 10 * weights.ftsBoost)     // 키워드 매칭
  + (filePartMatchCount * 5)                             // 파일 경로 부분 매칭
  + (fullFilePathMatch * 15)                             // 파일 경로 완전 매칭
  + (priority * 0.3 * weights.priorityBoost)             // 우선순위
  + (isPinned ? 10 : 0)                                  // 고정 보너스
  + min(10, accessCount * 0.5)                           // 접근 횟수 (상한 10)
  + max(0, 5 - daysSinceAccess/7) * weights.recencyBoost // 최근성 (7주 기준 감쇠)
  + (helpfulCount - unhelpfulCount) * 2                  // 피드백 점수
  + (relatedKeys.length * weights.graphBoost)            // 그래프 연결
  + (suggestedTypeMatch * 8)                             // 타입 매칭 보너스
```

---

## 8. Context Budget

### 8.1 handleBudget() 토큰 예산 관리

`context.ts:692-797`에 정의된 토큰 예산 기반 컨텍스트 선택 시스템이다.

#### 핵심 상수

```typescript
const CHARS_PER_TOKEN = 4; // 1 토큰 ~= 4 문자
```

#### 파라미터

| 파라미터 | 필수 | 설명 | 범위 |
|---|---|---|---|
| `maxTokens` | O | 토큰 예산 상한 | 100 - 200,000 |
| `types` | X | 필터링할 타입 목록 | - |
| `includeKeys` | X | 반드시 포함할 키 목록 | - |

#### 예산 배분 알고리즘

```
[1] mustInclude 엔트리 확보 (includeKeys에 해당하는 것)
  |   -> 예산에서 차감
  |
  v
[2] 후보 엔트리 점수 산출
  |   totalValue = priority * 2
  |              + min(20, accessCount * 2)
  |              + max(0, helpful * 3)
  |              + max(0, 20 - daysSinceAccess / 3)
  |              + isPinned * 30
  |
  |   efficiency = totalValue / tokenEstimate
  |
  v
[3] efficiency 기준 정렬 (동점 시 totalValue 기준)
  |
  v
[4] 예산 내에서 탐욕적(greedy) 선택
  |   -> charLen > budgetRemaining이면 skip
  |   -> 예산 초과 시 중단
  |
  v
[5] 응답: budgetUsed, budgetMax, entriesIncluded, entriesTotal, entries[]
```

#### Value 산출 공식

| 요소 | 공식 | 최대 기여 |
|---|---|---|
| Priority | `priority * 2` | 200 (priority 100 기준) |
| Access count | `min(20, accessCount * 2)` | 20 |
| Feedback | `max(0, helpful * 3)` | 무제한 (positive feedback 비례) |
| Recency | `max(0, 20 - daysSinceAccess / 3)` | 20 (0일 기준) |
| Pinned | `isPinned * 30` | 30 |

Efficiency(효율)은 `totalValue / tokenEstimate`로 계산되므로, 작은 크기에 높은 가치를 가진 엔트리가 우선 선택된다.

### 8.2 handleCompose() 태스크 기반 구성

`context.ts:799-908`에 정의된 태스크 설명 기반 컨텍스트 구성이다.

```
task 문자열
  |
  v
[1] 태스크 단어 추출 (2자 초과, 영숫자만)
  |
  v
[2] 전체 엔트리 스코어링
  |   +-- pinned: +200
  |   +-- priority: 그대로
  |   +-- 태스크 단어 매칭: 단어당 +15
  |   +-- constraints 타입: +30
  |   +-- lessons_learned 타입: +20
  |   +-- coding_style 타입: +10
  |
  v
[3] score 내림차순 정렬
  |
  v
[4] 예산 내에서 탐욕적 선택
  |   +-- includeRelated이면 relatedKeys도 따라감 (score * 0.5)
  |
  v
[5] 응답: task, tokensUsed, tokenBudget, entriesSelected, entries[]
```

`compose`와 `budget`의 차이:

| 항목 | budget | compose |
|---|---|---|
| 선택 기준 | 효율 (value/token) | 태스크 관련성 |
| 기본 예산 | 필수 지정 | 8000 토큰 |
| related 추적 | 미지원 | `includeRelated` 파라미터 |
| 타입별 가산점 | 없음 | constraints +30, lessons +20, style +10 |

---

## 9. 전체 플로우 트레이스

### 9.1 세션 시작부터 컨텍스트 로드까지

```
[사용자가 Claude Code 세션 시작]
  |
  v
[Claude Code] SessionStart 이벤트 발생
  |
  v
[hooks.json] "SessionStart" -> dispatch.sh start 실행
  |
  v
[dispatch.sh start]
  +-- ensure_session_id() -> 세션 ID 확보/생성
  +-- printf MEMCTL_REMINDER -> Claude system context에 삽입
  |
  v
[Claude Agent] REMINDER를 읽고 지시에 따라 행동 시작
  |
  v
[Claude Agent] context action=bootstrap 호출 (MCP tool call)
  |
  v
[MCP Server] handleBootstrap() 실행
  +-- [Phase 1] Fire-and-forget cleanup (500ms 제한)
  +-- [Phase 2] 4개 병렬 fetch
  |     +-- listAllMemories() -> 전체 메모리 로드
  |     +-- getBranchInfo() -> git 상태 확인
  |     +-- getMemoryCapacity() -> 용량 확인
  |     +-- getAllContextTypeInfo() -> 타입 정보 로드
  +-- [Phase 3] cleanup 결과 수거
  +-- [Phase 4] extractAgentContextEntries() -> 컨텍스트 엔트리 추출
  +-- [Phase 5] branch 필터링 + 타입별 조직화
  +-- [Phase 6] branch plan 조회
  +-- [Phase 7-8] 응답 조립 -> JSON 반환
  |
  v
[Claude Agent] 부트스트랩 응답을 파싱하여 프로젝트 컨텍스트 이해
  |
  v
[Claude Agent] activity action=memo_read (메모 확인)
  |
  v
[Claude Agent] branch action=get (branch 상태 확인)
  |
  v
[준비 완료 - 사용자 요청 대기]
```

### 9.2 사용자 턴 처리 플로우

```
[사용자가 프롬프트 입력]
  |
  v
[Claude Code] UserPromptSubmit 이벤트 발생
  |
  v
[hooks.json] "UserPromptSubmit" -> dispatch.sh user 실행
  |
  v
[dispatch.sh user]
  +-- ensure_session_id() -> 세션 ID 확인
  +-- hookSpecificOutput 출력 -> REMINDER 재주입
  +-- extract_json_field(payload, "user") -> 사용자 메시지 추출
  +-- send_hook_payload (background) -> memctl hook --stdin 전송
  |
  v
[memctl hook turn] (hooks.ts의 handleHookTurn)
  +-- extractHookCandidates() -> 후보 추출 (상세: 05-hook-capture.md)
  +-- findSimilar() -> 중복 검사
  +-- storeMemory() -> 저장
  +-- upsertSessionLog() -> 세션 로그 갱신
  |
  v
[Claude Agent] 재주입된 REMINDER에 따라 행동
  +-- context_for filePaths=[작업 파일] -> 관련 컨텍스트 조회
  +-- smart_retrieve intent=... -> 필요한 정보 검색
  +-- [작업 수행]
  +-- functionality_set -> 결정/교훈 저장
```

### 9.3 Compaction 발생 시

```
[Claude Code 대화 컨텍스트가 한계에 도달]
  |
  v
[Claude Code] Context compaction 실행
  |
  v
[Claude Code] SessionStart (compact matcher) 이벤트 발생
  |
  v
[hooks.json] matcher: "compact" -> dispatch.sh compact 실행
  |
  v
[dispatch.sh compact]
  +-- printf MEMCTL_REMINDER -> REMINDER 재주입
  |
  v
[Claude Agent] 압축된 컨텍스트 + 새로 주입된 REMINDER
  +-- context action=bootstrap_compact 호출 (경량 버전)
  |     또는
  +-- context action=bootstrap_delta since=<last_known_timestamp>
  |
  v
[Claude Agent] 컨텍스트 복원 완료, 작업 계속
```

### 9.4 세션 종료 플로우

```
[사용자가 세션 종료 또는 /exit]
  |
  v
[Claude Agent] REMINDER 지시에 따라:
  +-- activity action=memo_leave message="핸드오프 노트"
  +-- session action=end summary="세션 요약"
  |
  v
[Claude Code] SessionEnd 이벤트 발생
  |
  v
[hooks.json] "SessionEnd" -> dispatch.sh end 실행
  |
  v
[dispatch.sh end]
  +-- ensure_session_id() -> 세션 ID 확인
  +-- extract_json_field(payload, "summary") -> 요약 추출
  +-- send_hook_payload (foreground) -> 세션 종료 API 전송
  +-- rm -f session_id 파일 -> 세션 파일 삭제
  |
  v
[memctl hook end] (hooks.ts의 handleHookEnd)
  +-- MCP managed 여부 확인
  +-- 세션 로그 최종 업데이트 (summary, keysRead, keysWritten, endedAt)
  |
  v
[세션 완전 종료]
```

### 9.5 context tool 전체 action 맵

`context.ts:37-52`에 정의된 12개 action의 전체 구조:

| Action | 핸들러 | 핵심 기능 | 주요 파라미터 |
|---|---|---|---|
| `bootstrap` | `handleBootstrap` | 전체 컨텍스트 부트스트랩 | `includeContent`, `types`, `branch` |
| `bootstrap_compact` | `handleBootstrapCompact` | 경량 부트스트랩 (content 미포함) | - |
| `bootstrap_delta` | `handleBootstrapDelta` | 증분 동기화 | `since` (timestamp) |
| `functionality_get` | `handleFunctionalityGet` | 단일/타입별 엔트리 조회 | `type`, `id`, `followLinks` |
| `functionality_set` | `handleFunctionalitySet` | 엔트리 저장/수정 | `type`, `id`, `content`, `priority`, `tags` |
| `functionality_delete` | `handleFunctionalityDelete` | 엔트리 삭제 | `type`, `id` |
| `functionality_list` | `handleFunctionalityList` | 타입별 엔트리 목록 | `type`, `limitPerType`, `includeContentPreview` |
| `context_for` | `handleContextFor` | 파일 경로 기반 관련 컨텍스트 | `filePaths`, `types` |
| `budget` | `handleBudget` | 토큰 예산 기반 선택 | `maxTokens`, `types`, `includeKeys` |
| `compose` | `handleCompose` | 태스크 기반 구성 | `task`, `maxTokens`, `includeRelated` |
| `smart_retrieve` | `handleSmartRetrieve` | 의도 기반 검색 | `intent`, `files`, `maxResults`, `followLinks` |
| `search_org` | `handleSearchOrg` | 조직 전체 검색 | `query`, `limit` |
| `rules_evaluate` | `handleRulesEvaluate` | 조건부 규칙 평가 | `filePaths`, `branch`, `taskType` |
| `thread` | `handleThread` | 세션 간 활동 분석 | `sessionCount`, `branch` |

### 9.6 context_for 파일 기반 스코어링

`context.ts:623-690`의 `handleContextFor()` 상세:

```
filePaths 입력
  |
  v
[1] 관련 타입 결정 (기본: architecture, coding_style, testing,
  |  constraints, file_map, folder_structure)
  |
  v
[2] 검색어 추출 (경로 세그먼트 + 확장자)
  |   예: "src/api/auth.ts" -> ["src", "api", "auth.ts", "ts"]
  |
  v
[3] 엔트리별 스코어링
  |   +-- 기본: entry.priority
  |   +-- 경로 세그먼트 매칭: 세그먼트당 +10
  |   +-- 전체 경로 매칭: 경로당 +50
  |
  v
[4] score > 0인 것만 필터링, score 내림차순 정렬
  |
  v
[5] 상위 20개 반환
```

### 9.7 rules_evaluate 조건부 규칙

`context.ts:1065-1131`의 `handleRulesEvaluate()`:

메모리 엔트리의 `metadata.conditions`에 정의된 규칙을 현재 상황과 대조한다:

```typescript
const cond = conditions as {
  filePatterns?: string[];    // glob 패턴
  branchPatterns?: string[];  // glob 패턴
  taskTypes?: string[];       // 완전 일치
};
```

평가 흐름:

```
각 엔트리의 metadata.conditions 확인
  |
  +-- filePatterns: filePaths 중 하나라도 glob 매칭?
  +-- branchPatterns: 현재 branch가 glob 매칭?
  +-- taskTypes: 현재 taskType이 목록에 포함?
  |
  v
matchedConditions.length > 0 이면 결과에 포함
```

glob 매칭은 `response.ts:60-67`의 `matchGlob()`을 사용한다:

```typescript
export function matchGlob(filepath: string, pattern: string): boolean {
  const regex = pattern
    .replace(/\*\*/g, "{{GLOBSTAR}}")
    .replace(/\*/g, "[^/]*")
    .replace(/\?/g, "[^/]")
    .replace(/{{GLOBSTAR}}/g, ".*");
  return new RegExp(`^${regex}$`).test(filepath);
}
```

### 9.8 thread 세션 간 활동 분석

`context.ts:1133-1189`의 `handleThread()`:

최근 N개 세션 로그를 분석하여 "hot memory" (자주 읽기/쓰기되는 메모리)를 파악한다.

```
세션 로그 (최근 3개 기본)
  |
  v
각 로그에서 keysRead, keysWritten 추출
  |
  v
키별 통계 집계: { reads, writes, lastSession }
  |
  v
활동 점수 계산: activity = writes * 3 + reads
  |
  v
결과 분류:
  +-- activelyEdited: writes > 0 (상위 10개)
  +-- frequentlyRead: writes === 0 (상위 10개)
```

---

## 참조 파일 경로 요약

| 파일 | 역할 |
|---|---|
| `packages/cli/src/tools/handlers/context.ts` | context tool 전체 구현 (12개 action) |
| `packages/cli/src/agent-context.ts` | 타입 시스템, 키 파싱, 메모리 캐싱, 엔트리 추출 |
| `packages/cli/src/hook-adapter.ts` | DISPATCHER_SCRIPT 생성, MEMCTL_REMINDER 정의, 에이전트별 설정 |
| `packages/cli/src/hooks.ts` | hook CLI 핸들러, 후보 추출, 저장, 세션 관리 |
| `packages/cli/src/intent.ts` | 검색 의도 분류, 가중치 시스템 |
| `packages/cli/src/tools/response.ts` | 응답 포맷팅, 용량 가이던스, glob 매칭 |
| `plugins/memctl/hooks/hooks.json` | Claude Code hook 이벤트 바인딩 정의 |
