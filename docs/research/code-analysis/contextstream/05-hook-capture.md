# ContextStream -- 자동 캡처 (Hook) 분석

## 1. 훅 시스템 아키텍처

### 1.1 디렉토리 구조와 파일 목록

`src/hooks/` 디렉토리에는 27개의 TypeScript 파일과 테스트 파일이 존재한다.

| 파일 | 역할 | 분류 |
|------|------|------|
| `runner.ts` | 단일 진입점, 훅 이름으로 동적 dispatch | 인프라 |
| `common.ts` | 공용 유틸리티 (readHookInput, writeHookOutput, loadHookConfig, apiRequest 등) | 인프라 |
| `noop.ts` | 비활성 훅을 위한 no-op 핸들러 | 인프라 |
| `prompt-state.ts` | 영속 상태 관리 (~/.contextstream/prompt-state.json) | 인프라 |
| `session-init.ts` | 세션 시작 시 전체 컨텍스트 주입 | 세션 생명주기 |
| `session-end.ts` | 세션 종료 시 트랜스크립트 저장 및 마무리 | 세션 생명주기 |
| `stop.ts` | 응답 완료 체크포인트 기록 | 세션 생명주기 |
| `pre-tool-use.ts` | discovery 도구 차단 및 ContextStream 검색으로 리다이렉트 | 도구 인터셉트 |
| `post-write.ts` | Edit/Write/NotebookEdit 후 실시간 파일 인덱싱 | 도구 인터셉트 |
| `post-tool-use-failure.ts` | 도구 실패 시 메모리 이벤트 기록 및 반복 실패 lesson 생성 | 도구 인터셉트 |
| `pre-compact.ts` | 컨텍스트 컴팩션 전 스냅샷 저장 | 컴팩션 |
| `post-compact.ts` | 컨텍스트 컴팩션 후 상태 복원 | 컴팩션 |
| `user-prompt-submit.ts` | 매 메시지마다 ContextStream 규칙 리마인더 주입 | 프롬프트 |
| `on-save-intent.ts` | 문서 저장 의도 감지 후 ContextStream 스토리지로 리다이렉트 | 프롬프트 |
| `media-aware.ts` | 미디어 관련 프롬프트 감지 후 미디어 도구 안내 주입 | 프롬프트 |
| `auto-rules.ts` | init/context 도구 실행 후 규칙 파일 자동 업데이트 | 자동 유지보수 |
| `on-bash.ts` | Bash 명령 캡처 및 오류 lesson 제안 | 활동 캡처 |
| `on-read.ts` | Read/Glob/Grep 파일 탐색 추적 | 활동 캡처 |
| `on-web.ts` | WebFetch/WebSearch 리서치 캡처 | 활동 캡처 |
| `on-task.ts` | Task 에이전트 호출 캡처 | 활동 캡처 |
| `subagent-start.ts` | 서브에이전트 시작 시 컨텍스트 주입 | 에이전트 생명주기 |
| `subagent-stop.ts` | 서브에이전트 종료 시 플랜/태스크 자동 생성 | 에이전트 생명주기 |
| `task-completed.ts` | 태스크 완료 시 상태 업데이트 및 lesson 생성 | 에이전트 생명주기 |
| `teammate-idle.ts` | 팀메이트 유휴 시 대기 중인 태스크로 리다이렉트 | 에이전트 생명주기 |
| `notification.ts` | 알림 이벤트를 메모리에 기록 | 이벤트 캡처 |
| `permission-request.ts` | 권한 요청 기록 및 고위험 명령 경고 | 이벤트 캡처 |
| `prompt-state.test.ts` | prompt-state 단위 테스트 | 테스트 |

### 1.2 Lazy-Loading 패턴

`runner.ts`는 ContextStream 훅 시스템의 단일 진입점이다. `contextstream-hook <hook-name> [args...]` 형태로 호출되며, 모든 훅 핸들러를 정적으로 import하지 않고 `Record<string, () => Promise<unknown>>` 맵을 통해 동적 `import()`를 수행한다.

```typescript
const hooks: Record<string, () => Promise<unknown>> = {
  "pre-tool-use": async () => (await import("./pre-tool-use.js")).runPreToolUseHook(),
  "post-tool-use": async () => (await import("./post-write.js")).runPostWriteHook(),
  "user-prompt-submit": async () => (await import("./user-prompt-submit.js")).runUserPromptSubmitHook(),
  // ... 21개 더
};
```

이 패턴의 핵심 설계 의도:
- **전체 MCP 서버 로딩 회피**: 훅 실행 시 풀 서버를 초기화하는 오버헤드를 제거한다.
- **선택적 로딩**: 호출된 훅만 import하여 cold start 시간을 최소화한다.
- **하위 호환성**: 알 수 없는 훅 이름이 들어오면 exit 1 대신 exit 0으로 종료하여 구버전 바이너리가 신버전 훅 이름을 만나도 에디터에서 "hook error"가 표시되지 않도록 한다.
- **실패 안전**: `handler().catch(() => process.exit(0))`으로 모든 예외를 흡수한다.

특이 사항으로, `"post-tool-use"`와 `"post-write"`가 동일한 `runPostWriteHook()`으로 매핑되어 있다. 또한 `"on-bash"`, `"on-task"`, `"on-read"`, `"on-web"`, `"auto-rules"`, `"media-aware"` 등 다수의 훅이 runner에서는 `noop.js`의 `runNoopHook()`으로 매핑되어 있는데, 이는 이 훅들이 runner 경로(esbuild 번들)가 아닌 직접 실행 경로(`node dist/hooks/on-bash.js` 또는 `npx` 경유)로만 활성화되는 레거시/선택적 훅임을 의미한다.

### 1.3 esbuild 별도 번들링

훅들은 메인 MCP 서버와 별도로 번들링된다. `getHookCommand()` 함수 (hooks-config.ts)가 실행 명령을 결정하는데, 3단계 우선순위를 가진다:

1. **바이너리 설치 경로** (가장 빠름): Unix에서 `/usr/local/bin/contextstream-mcp`, Windows에서 `%LOCALAPPDATA%\ContextStream\contextstream-mcp.exe` -- Node.js 오버헤드 없음
2. **직접 node 실행**: 설치된 패키지의 `dist/index.js`를 직접 `node` 명령으로 실행
3. **npx 폴백**: `npx @contextstream/mcp-server hook <name>` -- 항상 동작하지만 가장 느림

각 훅 파일은 하단에 자동 실행 가드가 있어 직접 실행도 가능하다:

```typescript
const isDirectRun = process.argv[1]?.includes("pre-tool-use") || process.argv[2] === "pre-tool-use";
if (isDirectRun) {
  runPreToolUseHook().catch(() => process.exit(0));
}
```

---

## 2. 훅 실행 모델

### 2.1 입출력 프로토콜

모든 훅은 동일한 입출력 모델을 따른다:

**입력**: stdin으로 JSON을 수신한다. `common.ts`의 `readHookInput<T>()` 함수가 `fs.readFileSync(0, "utf8")`으로 fd 0(stdin)을 동기적으로 읽는다. 파싱 실패 시 빈 객체 `{}` 를 반환한다.

**출력**: stdout으로 JSON을 출력한다. `common.ts`의 `writeHookOutput()` 함수가 `console.log(JSON.stringify(payload))`을 수행한다. 출력 페이로드는 에디터 포맷에 따라 다르다:

- **Claude Code**: `{ hookSpecificOutput: { hookEventName, additionalContext }, blocked?, reason? }`
- **Cline/Roo/Kilo**: `{ cancel: boolean, errorMessage?, contextModification? }`
- **Cursor**: `{ decision: "allow"|"deny", reason? }`

**종료 코드**: 항상 exit 0. 에디터가 non-zero exit code를 "hook error"로 표시하는 것을 방지하기 위함이다.

### 2.2 설정 로딩: loadHookConfig()

`common.ts`의 `loadHookConfig(cwd)` 함수는 다단계 설정 탐색을 수행한다:

1. **환경 변수 우선**: `CONTEXTSTREAM_API_URL`, `CONTEXTSTREAM_API_KEY`, `CONTEXTSTREAM_JWT`, `CONTEXTSTREAM_WORKSPACE_ID`, `CONTEXTSTREAM_PROJECT_ID`
2. **디렉토리 상향 탐색**: cwd부터 최대 6 레벨 상위까지 `.mcp.json` 파일을 탐색하여 `mcpServers.contextstream.env`에서 설정을 추출한다. 동시에 `.contextstream/config.json`에서 `workspace_id`와 `project_id`를 로드한다.
3. **홈 디렉토리 폴백**: `~/.mcp.json`에서 API 키와 URL을 최종 검색한다.

반환 타입 `HookApiConfig`에는 `apiUrl`, `apiKey`, `jwt`, `workspaceId`, `projectId`, `sessionId?`가 포함된다.

### 2.3 API 요청: apiRequest()

`common.ts`의 `apiRequest()` 함수는 `fetch()` 기반 HTTP 클라이언트로, `authHeaders()`가 API 키면 `X-API-Key`, JWT면 `Authorization: Bearer` 헤더를 설정한다. 응답이 `{ data: ... }` 구조면 `data` 필드를 언래핑하여 반환한다.

### 2.4 공용 헬퍼 함수

`common.ts`는 훅들이 공통으로 사용하는 고수준 API 호출 헬퍼를 제공한다:

- `postMemoryEvent()`: `/memory/events` POST -- 이벤트 기록
- `createPlan()`: `/plans` POST -- 플랜 생성
- `createTask()`: `/tasks` POST -- 태스크 생성
- `updateTaskStatus()`: `/tasks/:id` PATCH -- 태스크 상태 변경
- `listPendingTasks()`: `/tasks?status=pending` GET -- 대기 중 태스크 목록
- `fetchFastContext()`: `/context/hook` POST -- Redis 캐시된 빠른 컨텍스트 조회 (~20-50ms)

---

## 3. 세션 초기화 훅 (session-init)

### 3.1 개요

`session-init.ts`는 SessionStart 훅으로 등록되어 새 세션이 시작될 때 실행된다. `CONTEXTSTREAM_SESSION_INIT_ENABLED` 환경 변수로 비활성화 가능하다.

### 3.2 실행 흐름

```
stdin (JSON) → loadConfigFromMcpJson(cwd) → cleanupStale(360) → markInitRequired(cwd)
  → Promise.all([
      fetchSessionContext(),        // 5초 타임아웃, GET /api/v1/context
      attemptAutoUpdate(),          // 자동 업데이트 시도
      getUpdateNotice()             // 버전 알림 확인
    ])
  → formatContext(context, options)
  → stdout (hookSpecificOutput.additionalContext)
  → exit 0
```

### 3.3 컨텍스트 페치

`fetchSessionContext()`는 `/api/v1/context` 엔드포인트에 GET 요청을 보내며 5초 `AbortController` 타임아웃을 적용한다. 쿼리 파라미터로 `workspace_id`, `project_id`, `include_rules=true`, `include_lessons=true`, `include_decisions=true`, `include_plans=true`, `limit=5`를 전달한다.

응답 `ContextResponse`에는 다음이 포함된다:
- `rules`: 동적 규칙 문자열
- `lessons`: 과거 실수에서 배운 교훈 배열 (title, trigger, prevention)
- `recent_decisions`: 최근 결정 사항
- `active_plans`: 활성 플랜
- `pending_tasks`: 대기 중 태스크

### 3.4 자동 업데이트 확인

세션 시작 시 컨텍스트 페치와 병렬로 자동 업데이트를 시도한다:

1. `checkUpdateMarker()`: 이전 업데이트가 완료되었는지 마커 파일 확인. 완료된 경우 `regenerateRuleFiles(cwd)`로 에디터 규칙 파일을 갱신한 뒤 마커를 클리어한다.
2. `attemptAutoUpdate()`: 새 버전이 있으면 백그라운드 업데이트를 시작한다.
3. `getUpdateNotice()`: 현재 버전과 최신 버전을 비교한다.

### 3.5 출력 포맷

`formatContext()`는 섹션별로 구조화된 텍스트를 생성한다:

```
⬡ ContextStream — Smart Context & Memory

## ⚠️ Lessons from Past Mistakes
- **제목**: 예방법

## 📋 Active Plans
- 플랜 제목 (상태)

## ✅ Pending Tasks
- 태스크 제목

## 📝 Recent Decisions
- **결정 제목**

---
On the first message in a new session call `mcp__contextstream__init(...)` then `mcp__contextstream__context(user_message="...")`. After that, call `mcp__contextstream__context(user_message="...")` on every message.
```

이 텍스트는 `hookSpecificOutput.additionalContext`로 AI의 컨텍스트에 주입되어, 세션 시작 시 과거 컨텍스트가 자동으로 복원된다.

---

## 4. Pre-Tool-Use 훅

### 4.1 설계 목적

`pre-tool-use.ts`는 ContextStream의 핵심 행동 강제 메커니즘이다. 프로젝트가 인덱싱된 상태라면 discovery 도구(Glob, Grep, Search, Explore, Task, EnterPlanMode)를 차단하고 ContextStream 검색으로 리다이렉트한다.

### 4.2 3가지 에디터 포맷 지원

훅 입력의 필드명으로 에디터를 감지한다:

```typescript
function detectEditorFormat(input: HookInput): "claude" | "cline" | "cursor" {
  if (input.hookName !== undefined || input.toolName !== undefined) return "cline";
  if (input.hook_event_name !== undefined || input.tool_name !== undefined) return "claude";
  return "claude";
}
```

각 에디터별 차단 출력:
- **Claude Code**: `hookSpecificOutput.additionalContext`에 `[CONTEXTSTREAM]` 접두 메시지 주입 -- 하드 블로킹 대신 가이던스 방식 사용
- **Cline/Roo/Kilo**: `{ cancel: true, errorMessage, contextModification }`
- **Cursor**: `{ decision: "deny", reason }`

### 4.3 Discovery 패턴 감지

```typescript
const DISCOVERY_PATTERNS = ["**/*", "**/", "src/**", "lib/**", "app/**", "components/**"];
```

`isDiscoveryGlob(pattern)`은 위 패턴 포함 여부, `**/*.` 또는 `**/` 시작 여부, `**` 또는 `*/` 포함 여부를 검사한다. `isDiscoveryGrep(filePath)`은 경로가 `.`, `./`, `*`, `**`이거나 와일드카드를 포함하면 true를 반환한다.

### 4.4 인덱스 상태 추적

인덱스 상태는 `~/.contextstream/indexed-projects.json` 파일로 추적된다:

```typescript
interface IndexStatusFile {
  version: number;
  projects: Record<string, IndexedProjectInfo>;  // key: 프로젝트 절대 경로
}

interface IndexedProjectInfo {
  indexed_at: string;       // ISO 날짜
  project_id?: string;
  project_name?: string;
}
```

`isProjectIndexed(cwd)`는 현재 작업 디렉토리가 인덱싱된 프로젝트의 경로이거나 하위 디렉토리인지 확인한다. **Stale 체크**: `indexed_at` 시점으로부터 7일(`STALE_THRESHOLD_DAYS`)이 경과하면 `isStale: true`를 반환하지만, 현재 구현에서는 stale 상태여도 차단 로직은 동일하게 적용된다.

프로젝트가 인덱싱되지 않은 경우(`isIndexed === false`), 모든 로컬 도구를 허용하고 즉시 exit한다.

### 4.5 초기화/컨텍스트 필수 확인

Pre-tool-use 훅은 도구 차단 이전에 세션 초기화 상태를 검증한다:

1. `isInitRequired(cwd)`: session-init 훅이 `markInitRequired`를 설정한 후, `mcp__contextstream__init()`이 아직 호출되지 않았으면 다른 모든 도구를 차단하고 init 호출을 요구하는 메시지를 주입한다.
2. `isContextRequired(cwd)`: user-prompt-submit 훅이 매 프롬프트마다 `markContextRequired`를 설정하며, `mcp__contextstream__context()`가 호출될 때까지 다른 도구를 차단한다.

**Narrow bypass**: 컨텍스트가 freshness 기준(120초) 내이고 상태 변경이 없었다면, read-only ContextStream 호출은 context() 없이도 허용된다.

### 4.6 도구별 차단 및 리다이렉트

| 도구 | 조건 | 리다이렉트 메시지 |
|------|------|-------------------|
| Glob | `isDiscoveryGlob(pattern)` | `search(mode="auto", query="<pattern>")` 사용 권장 |
| Grep/Search | 패턴이 있고 경로가 discovery 패턴 | 특정 파일이면 `Read("<path>")`, 아니면 `search(mode="auto")` |
| Explore | 무조건 | `search(mode="auto", output_format="paths")` |
| Task | `subagent_type`에 "explore" 포함 | `search(mode="auto")` |
| Task | `subagent_type`에 "plan" 포함 | `search` + `session(action="capture_plan")` + `memory(action="create_task")` |
| EnterPlanMode | 무조건 | `session(action="capture_plan")` + `memory(action="create_task")` |
| list_files/search_files | Cline/Cursor 전용 도구, discovery 패턴 | `search(mode="auto")` |

### 4.7 상태 변경 추적

`isLikelyStateChangingTool()` 함수가 도구가 상태를 변경하는지 판단한다. ContextStream read-only 호출(context, init, list 계열)과 로컬 읽기 도구(read, grep, glob 등)는 상태 변경으로 간주하지 않는다. write/edit/create/delete/bash/run 등의 키워드가 도구 이름에 포함되면 상태 변경으로 판단하여 `markStateChanged(cwd)`를 호출한다. 이는 prompt-state의 freshness 추적에 사용된다.

---

## 5. Post-Write 훅

### 5.1 설계 목적

`post-write.ts`는 Edit, Write, NotebookEdit 도구 실행 후 호출되어 변경된 파일을 실시간으로 ContextStream 인덱스에 반영한다. `CONTEXTSTREAM_POSTWRITE_ENABLED` 환경 변수로 비활성화 가능하다.

### 5.2 4가지 에디터 포맷별 경로 추출

`extractFilePath(input)` 함수가 에디터별로 다른 필드에서 파일 경로를 추출한다:

| 에디터 | 경로 필드 |
|--------|-----------|
| Claude Code | `tool_input.file_path`, `tool_input.notebook_path`, `tool_input.path` |
| Cursor | `parameters.path`, `parameters.file_path` |
| Cline/Roo/Kilo | `toolParameters.path` |
| Windsurf | `file_path` (직접 필드) |

### 5.3 파일 크기 및 확장자 필터링

인덱싱 대상 판단:

**확장자 허용 목록** (`INDEXABLE_EXTENSIONS`): `.ts`, `.tsx`, `.js`, `.jsx`, `.py`, `.rs`, `.go`, `.java`, `.c`, `.cpp`, `.h`, `.rb`, `.php`, `.swift`, `.sh`, `.sql`, `.html`, `.css`, `.json`, `.yaml`, `.md`, `.vue`, `.svelte`, `.astro`, `.tf`, `.prisma`, `.proto` 등 약 46개의 확장자. 확장자 없는 특수 파일(`Dockerfile`, `Makefile`, `Rakefile`, `Gemfile`, `Procfile`)도 허용한다.

**크기 제한**: 최대 5MB (`MAX_FILE_SIZE = 5 * 1024 * 1024`). `fs.statSync()`로 크기를 확인하여 초과 시 인덱싱을 건너뛴다.

### 5.4 Fire-and-Forget 인덱싱

인덱싱 요청은 비차단(fire-and-forget) 방식으로 수행된다:

```typescript
async function indexFile(filePath, projectId, apiUrl, apiKey, projectRoot) {
  const content = fs.readFileSync(filePath, "utf-8");
  const relativePath = path.relative(projectRoot, filePath);
  const payload = {
    files: [{ path: relativePath, content, language: detectLanguage(filePath) }],
  };
  await fetch(`${apiUrl}/api/v1/projects/${projectId}/files/ingest`, {
    method: "POST",
    headers: { "Content-Type": "application/json", "X-API-Key": apiKey },
    body: JSON.stringify(payload),
    signal: AbortSignal.timeout(10000),
  });
}
```

`detectLanguage()` 함수가 확장자 기반으로 `"typescript"`, `"python"`, `"rust"` 등의 언어를 매핑한다. API 응답에서 `cooldown` 또는 `daily_limit_exceeded` 상태가 반환되면 조용히 건너뛴다.

프로젝트 루트 탐색은 `.contextstream/config.json` 파일을 디렉토리를 상향 탐색하여 찾는다 (최대 10 레벨). `project_id`가 없으면 인덱싱을 건너뛴다.

---

## 6. Prompt State 관리

### 6.1 개요

`prompt-state.ts`는 워크스페이스별 프롬프트 상태를 영속적으로 추적하는 모듈이다. 상태 파일은 `~/.contextstream/prompt-state.json`에 저장된다.

### 6.2 데이터 구조

```typescript
type PromptStateFile = {
  workspaces: Record<string, PromptStateEntry>;  // key: 워크스페이스 절대 경로
};

type PromptStateEntry = {
  require_context: boolean;       // context() 호출 필요 여부
  require_init?: boolean;         // init() 호출 필요 여부
  last_context_at?: string;       // 마지막 context() 호출 시각 (ISO)
  last_state_change_at?: string;  // 마지막 상태 변경 도구 실행 시각
  updated_at: string;             // 엔트리 마지막 업데이트 시각
};
```

### 6.3 워크스페이스 매칭

`workspacePathsMatch(a, b)`는 두 경로가 동일하거나, 하나가 다른 하나의 하위 디렉토리인 경우 true를 반환한다. 이를 통해 `/project` 경로로 기록된 상태를 `/project/src` 에서 조회할 때도 매칭된다.

### 6.4 주요 함수

| 함수 | 호출 위치 | 동작 |
|------|-----------|------|
| `markContextRequired(cwd)` | user-prompt-submit | 새 프롬프트마다 context 필수 플래그 설정 |
| `clearContextRequired(cwd)` | pre-tool-use (context() 감지) | 플래그 해제, `last_context_at` 기록 |
| `markInitRequired(cwd)` | session-init, user-prompt-submit (새 세션) | init 필수 플래그 설정 |
| `clearInitRequired(cwd)` | pre-tool-use (init() 감지) | init 필수 플래그 해제 |
| `markStateChanged(cwd)` | pre-tool-use (상태 변경 도구) | `last_state_change_at` 업데이트 |
| `isContextFreshAndClean(cwd, maxAgeSeconds)` | pre-tool-use | context가 maxAge(120초) 이내이고 이후 상태 변경이 없으면 true |
| `cleanupStale(maxAgeSeconds)` | session-init (360초), pre-tool-use (180초), user-prompt-submit (180초) | 오래된 엔트리 삭제 |

### 6.5 동작 흐름

```
user-prompt-submit → markContextRequired → markInitRequired (새 세션)
                                              ↓
pre-tool-use (도구 호출) → isInitRequired? → 차단 (init 요구)
                           ↓ init 완료
                         isContextRequired? → 차단 (context 요구)
                           ↓ context 완료
                         도구 실행 → isLikelyStateChangingTool? → markStateChanged
```

---

## 7. Pre/Post Compact 훅

### 7.1 Pre-Compact 훅

`pre-compact.ts`는 `/compact` 명령 또는 자동 컴팩션 이전에 실행된다. `CONTEXTSTREAM_PRECOMPACT_ENABLED`와 `CONTEXTSTREAM_PRECOMPACT_AUTO_SAVE` 환경 변수로 제어된다.

**트랜스크립트 파싱**: `parseTranscript(transcriptPath)`가 JSONL 형식의 트랜스크립트를 파싱하여 다음을 추출한다:
- `activeFiles`: Read/Write/Edit/NotebookEdit/Glob에서 참조된 파일 (최대 20개)
- `toolCallCount`: 총 도구 호출 수
- `messageCount`: 메시지 수
- `lastTools`: 마지막 10개 도구 이름
- `messages`: 전체 메시지 배열 (role, content, timestamp, tool_calls/tool_results)
- `startedAt`: 세션 시작 시각

**저장 전략**: 2단계 폴백
1. `saveFullTranscript()`: `/api/v1/transcripts` POST -- 전체 메시지와 메타데이터 (10초 타임아웃)
2. `saveSnapshot()`: 트랜스크립트 저장 실패 시 `/api/v1/memory/events` POST -- 경량 스냅샷 (5초 타임아웃)

**출력**: AI에게 주입하는 컨텍스트에 활성 파일 목록, 도구 호출 수, 자동 저장 상태, "compaction 후 `session_init(is_post_compact=true)` 호출" 안내를 포함한다.

### 7.2 Post-Compact 훅

`post-compact.ts`는 컴팩션 완료 후 실행된다. `CONTEXTSTREAM_POSTCOMPACT_ENABLED` 환경 변수로 제어된다.

**컨텍스트 복원**: `fetchLastTranscript(sessionId)`가 `/api/v1/transcripts?session_id=<id>&limit=1&sort=created_at:desc`로 가장 최근 저장된 트랜스크립트를 조회한다.

`formatTranscriptSummary()`가 복원된 트랜스크립트에서 다음을 추출하여 요약한다:
- 활성 파일 목록 (최대 10개)
- 최근 3개 사용자 메시지 (100자 프리뷰)
- 마지막 어시스턴트 응답 (300자 프리뷰)
- 세션 통계 (도구 호출 수, 메시지 수, 저장 시각)

이 요약이 `hookSpecificOutput.additionalContext`로 주입되어, 컴팩션 후 AI가 이전 작업 맥락을 복원할 수 있다.

---

## 8. 기타 훅

### 8.1 user-prompt-submit

`user-prompt-submit.ts`는 모든 사용자 메시지마다 실행되는 가장 빈번한 훅이다. 에디터별로 전혀 다른 경로를 탄다.

**Claude Code (빠른 경로, ~20-50ms)**:
- `/api/v1/context/hook` POST로 Redis 캐시된 컨텍스트를 가져온다 (2초 타임아웃).
- preferences, lessons, core rules가 포함된 compact 문자열을 반환한다.
- 실패 시 정적 `REMINDER` 문자열을 폴백으로 사용한다.
- JSONL 트랜스크립트 파일이나 session.messages에서 이전 교환(user + assistant)을 추출하여 `/api/v1/transcripts/exchange`로 비동기 저장한다 ("lagging" 캡처 패턴).

**비-Claude 에디터 (향상된 경로)**:
- SessionStart, PostToolUse, PreCompact, Stop 훅이 없는 에디터를 보상하기 위해 더 많은 작업을 수행한다.
- 새 세션 감지, 전체 규칙, 인덱스 상태 확인 가이드, lessons, plans, tasks, reminders, preferences를 포함하는 향상된 리마인더를 생성한다.
- 버전 알림 확인도 포함한다.

### 8.2 on-bash

`on-bash.ts`는 Bash 도구 실행 후 호출된다. 명령어와 출력을 `/memory/events`에 캡처하고, 오류 발생 시 패턴 매칭으로 lesson을 제안한다:

| 에러 패턴 | Lesson |
|-----------|--------|
| `command not found` | 패키지 설치 필요 |
| `permission denied` | sudo 또는 파일 권한 확인 |
| `no such file or directory` | 경로 확인 필요 |
| `EADDRINUSE` | 포트 충돌, 기존 프로세스 종료 필요 |
| `npm ERR!` | `--legacy-peer-deps` 시도 |
| `not a git repository` | `git init` 또는 올바른 디렉토리 확인 |

### 8.3 on-read

`on-read.ts`는 Read, Glob, Grep 도구 실행 후 호출되어 파일 탐색 활동을 `/memory/events`에 `file_exploration` 이벤트로 기록한다. 60초 중복 방지 윈도우(`recentCaptures` Set)가 있어 동일 도구+대상의 반복 기록을 억제한다.

### 8.4 on-web

`on-web.ts`는 WebFetch, WebSearch 도구 실행 후 웹 리서치를 `/memory/events`에 `web_research` 이벤트로 기록한다. URL, 검색 쿼리, 상위 3개 결과를 저장한다.

### 8.5 on-task

`on-task.ts`는 Task 도구 실행 후 에이전트 호출 정보를 `/memory/events`에 `task_agent` 이벤트로 기록한다. description, prompt, agent_type, result을 저장한다.

### 8.6 notification

`notification.ts`는 에디터 알림 이벤트를 `/memory/events`에 기록하는 간단한 훅이다. `common.ts`의 `postMemoryEvent()`를 사용한다.

### 8.7 permission-request

`permission-request.ts`는 권한 요청을 기록하고, 고위험 명령(`rm -rf`, `git reset --hard`, `mkfs`, `dd if=`, `shutdown`, `reboot`)이 감지되면 `additionalContext`로 주의 메시지를 주입한다.

### 8.8 subagent-start

`subagent-start.ts`는 서브에이전트 시작 시 호출된다. `/context/hook`에서 빠른 컨텍스트를 가져오고, 에이전트 유형에 따라 추가 프로토콜을 주입한다:
- **Explore 에이전트**: "SEARCH-FIRST PROTOCOL" -- ContextStream 검색을 먼저 수행하라는 강력한 지시
- **Plan 에이전트**: "PLAN MODE: SEARCH-FIRST" + ContextStream 플랜 저장 안내
- **공통**: SEARCH_PROTOCOL -- ContextStream search, keyword, graph 도구 사용 안내

### 8.9 subagent-stop

`subagent-stop.ts`는 서브에이전트 종료 시 호출된다. 트랜스크립트를 파싱하여 assistant 메시지를 추출하고:
- **Plan 에이전트**: 요약에서 플랜 제목을 도출(`derivePlanTitle`), 목록 항목에서 태스크를 추출(`extractPlanTasks`, 최대 20개), 각각 `createPlan()`과 `createTask()`로 자동 생성한다.
- **기타 에이전트**: 요약을 메모리 이벤트로 기록한다.

### 8.10 task-completed

`task-completed.ts`는 태스크 완료 시 호출된다. 기존 태스크 ID가 있으면 `updateTaskStatus()`로 완료 처리하고, 없으면 `createTask(status: "completed")`로 새로 생성한다. description에 "error", "failure", "retry" 등의 복구 관련 키워드가 있으면 추가로 lesson 이벤트를 생성한다.

### 8.11 teammate-idle

`teammate-idle.ts`는 팀메이트 에이전트가 유휴 상태가 될 때 호출된다. `listPendingTasks()`로 대기 중 태스크를 조회하고, 태스크가 있으면 첫 번째 태스크로 리다이렉트하는 `blocked` 응답을 반환한다.

### 8.12 post-tool-use-failure

`post-tool-use-failure.ts`는 도구 실행 실패 후 호출된다. 에러 텍스트의 fingerprint(도구명 + 에러 축약)를 생성하여 `~/.contextstream/hook-failure-counts.json`에 실패 횟수를 누적한다. 3회 이상 반복되면 자동으로 "recurring failure lesson" 메모리 이벤트를 생성한다.

### 8.13 on-save-intent

`on-save-intent.ts`는 사용자 프롬프트에서 "save", "store", "document", "remember" 등의 저장 의도를 정규식으로 감지하면, ContextStream의 `session(action="capture")`, `docs(action="create")`, `session(action="capture_plan")` 등을 사용하도록 안내하는 가이던스를 주입한다. 로컬 파일 저장을 감지하는 별도 패턴(`LOCAL_FILE_PATTERNS`)도 있다.

### 8.14 session-end

`session-end.ts`는 세션 종료 시 호출된다. 트랜스크립트를 파싱하여 메시지 수, 도구 호출 수, 세션 시간, 수정된 파일 목록을 집계한다. `CONTEXTSTREAM_SESSION_END_SAVE_TRANSCRIPT` 설정에 따라 전체 트랜스크립트를 `/api/v1/transcripts`에 저장하고, 세션 요약 이벤트를 `/api/v1/memory/events`에 기록한다.

### 8.15 stop

`stop.ts`는 AI 응답 완료 시 호출되는 간단한 체크포인트 훅이다. 세션 ID, 종료 사유, 도구명, 모델 정보를 `/memory/events`에 `"Stop checkpoint"` 이벤트로 기록한다.

### 8.16 media-aware / auto-rules / noop

- `media-aware.ts`: 미디어 관련 프롬프트 패턴(video, clips, Remotion, image, audio 등)을 감지하면 미디어 도구 사용 안내를 주입한다. runner에서는 noop으로 매핑되어 비활성이며, hooks-config에서 `includeMediaAware: true`로 명시 설정해야 활성화된다.
- `auto-rules.ts`: init/context 도구 실행 후 `rules_notice.status`가 "behind"이면 `installClaudeCodeHooks()`를 호출하여 훅 설정을 자동 갱신한다. 4시간 쿨다운이 적용된다. 레거시 Python 훅도 감지하여 Node.js 훅으로 업그레이드한다. runner에서 noop 매핑.
- `noop.ts`: 단순히 빈 Promise를 반환하는 핸들러. `auto-rules`, `on-bash`, `on-task`, `on-read`, `on-web`, `media-aware` 등이 runner에서 이 핸들러로 매핑된다.

---

## 9. 훅 설치

### 9.1 hooks-config.ts 개요

`src/hooks-config.ts` (약 2,100줄)는 훅 설치, 설정 빌드, 에디터별 통합을 담당하는 대규모 모듈이다. 레거시 Python 스크립트가 문자열 상수로 내장되어 있지만, 현재는 모든 훅이 Node.js 기반으로 실행된다.

### 9.2 installClaudeCodeHooks

```typescript
export async function installClaudeCodeHooks(options: {
  scope: "user" | "project" | "both";
  projectPath?: string;
  dryRun?: boolean;
  includePreCompact?: boolean;
  includeMediaAware?: boolean;
  includePostWrite?: boolean;
  includeAutoRules?: boolean;
}): Promise<{ scripts: string[]; settings: string[] }>
```

`buildHooksConfig()`로 훅 설정을 생성한 뒤, `readClaudeSettings()` -> `mergeHooksIntoSettings()` -> `writeClaudeSettings()`로 `~/.claude/settings.json` (user scope) 또는 `<project>/.claude/settings.json` (project scope)을 업데이트한다.

`mergeHooksIntoSettings()`는 기존 설정에서 `contextstream`이 포함된 명령을 가진 훅만 제거하고 새 훅을 추가하여, 사용자의 다른 훅 설정을 보존한다.

### 9.3 buildHooksConfig() 훅 등록 현황

`buildHooksConfig()`가 생성하는 Claude Code 훅 설정의 전체 목록:

| 훅 이벤트 | Matcher | 명령 | 타임아웃 | 기본 설치 |
|-----------|---------|------|----------|-----------|
| PreToolUse | `*` | `pre-tool-use` | 5초 | 예 |
| UserPromptSubmit | `*` | `user-prompt-submit` | 5초 | 예 |
| UserPromptSubmit | `*` | `on-save-intent` | 5초 | 예 |
| UserPromptSubmit | `*` | `media-aware` | 5초 | 아니오 (명시적) |
| PreCompact | `*` | `pre-compact` | 10초 | 예 |
| SessionStart | `startup\|resume\|compact` | `session-start` | 10초 | 예 |
| Stop | `*` | `stop` | 15초 | 예 |
| SessionEnd | `*` | `session-end` | 10초 | 예 |
| PostToolUse | `Edit\|Write\|NotebookEdit` | `post-write` | 10초 | 예 |
| PostToolUse | `mcp__contextstream__init\|...context` | `auto-rules` | 15초 | 아니오 (명시적) |
| PostToolUse | `Bash` | `on-bash` | 5초 | 아니오 (명시적) |
| PostToolUse | `Task` | `on-task` | 5초 | 아니오 (명시적) |
| PostToolUse | `Read\|Glob\|Grep` | `on-read` | 5초 | 아니오 (명시적) |
| PostToolUse | `WebFetch\|WebSearch` | `on-web` | 5초 | 아니오 (명시적) |
| PostToolUseFailure | `*` | `post-tool-use-failure` | 10초 | 예 |
| SubagentStart | `Explore\|Plan\|general-purpose\|custom` | `subagent-start` | 10초 | 예 |
| SubagentStop | `Plan` | `subagent-stop` | 15초 | 예 |
| TaskCompleted | `*` | `task-completed` | 10초 | 예 |
| TeammateIdle | `*` | `teammate-idle` | 10초 | 예 |
| Notification | `*` | `notification` | 10초 | 예 |
| PermissionRequest | `*` | `permission-request` | 10초 | 예 |

### 9.4 installEditorHooks -- 다중 에디터 지원

`installEditorHooks()` 함수는 `SupportedEditor` 타입에 따라 에디터별 설치 함수를 dispatch한다:

| 에디터 | 설치 함수 | 훅 디렉토리 |
|--------|-----------|-------------|
| `"claude"` | `installClaudeCodeHooks` -> `settings.json` | `~/.claude/settings.json` |
| `"cline"` | `installClineHookScripts` -> 실행 스크립트 | `~/Documents/Cline/Rules/Hooks/` |
| `"roo"` | `installRooCodeHookScripts` | `~/.roo/hooks/` |
| `"kilo"` | `installKiloCodeHookScripts` | `~/.kilocode/hooks/` |
| `"cursor"` | `installCursorHookScripts` -> `hooks.json` | `~/.cursor/hooks/` |

Cline/Roo/Kilo는 실행 가능한 wrapper 스크립트를 디스크에 작성한다. `getHookWrapperScript(hookName)`이 플랫폼에 맞는 스크립트를 생성한다:
- Unix: `#!/bin/bash` 스크립트 (확장자 없음), `exec` 으로 hook command 실행
- Windows: `.cmd` 배치 파일

`installAllEditorHooks()`는 기본적으로 5개 에디터 전체에 대해 순차적으로 `installEditorHooks()`를 호출하며, 개별 에디터 실패는 로그 후 계속 진행한다.

### 9.5 인덱스 상태 관리

`hooks-config.ts`는 인덱스 상태 파일(`~/.contextstream/indexed-projects.json`) 관리 함수도 제공한다:

- `readIndexStatus()` / `writeIndexStatus()`: 파일 읽기/쓰기
- `markProjectIndexed(projectPath, options?)`: 프로젝트를 인덱싱 완료로 표시
- `unmarkProjectIndexed(projectPath)`: 프로젝트 인덱스 상태 제거
- `clearProjectIndex(projectPath, projectId?)`: 인덱스 상태와 해시 매니페스트를 함께 제거 (실패 시 롤백용)

이 함수들은 프로젝트 인덱싱 도구에서 호출되며, pre-tool-use 훅이 이 파일을 읽어 discovery 도구 차단 여부를 결정한다.
