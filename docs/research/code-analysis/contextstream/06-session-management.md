# ContextStream -- 세션 관리 분석

## 1. SessionManager 클래스

### 1.1 개요

`src/session-manager.ts` (865줄)에 정의된 `SessionManager` 클래스는 MCP 연결당 하나의 인스턴스로 존재하며, 자동 컨텍스트 로딩, 토큰 추적, 연속 체크포인팅, 컴팩션 후 복원을 관리한다. "First-Tool Interceptor" 패턴을 구현하여 모든 MCP 클라이언트(Windsurf, Cursor, Claude Desktop, VS Code 등)에서 동일하게 동작한다.

### 1.2 전체 프로퍼티

```typescript
class SessionManager {
  // 세션 식별
  private sessionId: string;                    // "mcp-{UUID}" 형식의 고유 세션 ID

  // 초기화 상태
  private initialized = false;                  // 세션 자동 초기화 완료 여부
  private initializationPromise: Promise<unknown> | null = null;  // 동시 초기화 방지용

  // 컨텍스트
  private context: Record<string, unknown> | null = null;  // initSession() 응답 전체
  private ideRoots: string[] = [];              // IDE/에디터에서 감지된 워크스페이스 루트 경로
  private folderPath: string | null = null;     // 현재 프로젝트 폴더 경로
  private defaultSearchMode: string | null = null;  // 워크스페이스/프로젝트 기본 검색 모드

  // 컨텍스트 호출 추적
  private contextSmartCalled = false;           // context_smart가 호출되었는지 여부
  private warningShown = false;                 // context_smart 미호출 경고 표시 여부

  // 토큰 추적
  private sessionTokens = 0;                    // 실제 추적된 토큰 수
  private contextThreshold = 70000;             // 컨텍스트 압력 임계값 (100k 윈도우 기준 보수적)
  private conversationTurns = 0;                // 대화 턴 카운터
  static readonly TOKENS_PER_TURN_ESTIMATE = 3000;  // 턴당 추정 토큰 수

  // 연속 체크포인팅
  private toolCallCount = 0;                    // 총 도구 호출 수
  private checkpointInterval = 20;              // 체크포인트 저장 간격 (도구 호출 수)
  private lastCheckpointAt = 0;                 // 마지막 체크포인트의 toolCallCount
  private activeFiles: Set<string> = new Set(); // 현재 세션에서 작업 중인 파일 (최대 30개)
  private recentToolCalls: Array<{name: string; timestamp: number}> = [];  // 최근 도구 호출 기록 (최대 50개)
  private checkpointEnabled: boolean;           // CONTEXTSTREAM_CHECKPOINT_ENABLED === "true"

  // 컴팩션 후 복원 추적
  private lastHighPressureAt: number | null = null;  // 마지막 high/critical 압력 기록 시각
  private lastHighPressureTokens = 0;                // 그 시점의 토큰 수
  private postCompactRestoreCompleted = false;        // 복원 완료 여부 (세션 내 1회 제한)
}
```

### 1.3 생성자

```typescript
constructor(
  private server: McpServer,     // MCP SDK 서버 인스턴스
  private client: ContextStreamClient  // ContextStream HTTP 클라이언트
) {
  this.sessionId = `mcp-${randomUUID()}`;
}
```

`McpServer`는 MCP 프로토콜의 서버 측 구현으로, listRoots 등 클라이언트 capability 접근에 사용된다. `ContextStreamClient`는 ContextStream 백엔드 API 클라이언트다.

---

## 2. 자동 초기화 -- First-Tool Interceptor 패턴

### 2.1 withAutoContext 래퍼

`withAutoContext<T, R>()` 함수는 모든 MCP 도구 핸들러를 래핑하는 고차 함수다. 이 함수가 "First-Tool Interceptor" 패턴의 핵심이다.

```typescript
export function withAutoContext<T, R extends { content: Array<{ type: string; text: string }> }>(
  sessionManager: SessionManager,
  toolName: string,
  handler: ToolHandler<T, R>
): ToolHandler<T, R> {
  return async (input: T): Promise<R> => {
    const skipAutoInit = toolName === "session_init";
    let contextPrefix = "";

    if (!skipAutoInit) {
      const autoInitResult = await sessionManager.autoInitialize();
      if (autoInitResult) {
        contextPrefix = autoInitResult.contextSummary + "\n\n";
      }
    }

    const result = await handler(input);

    // 연속 체크포인팅을 위한 도구 호출 추적
    sessionManager.trackToolCall(toolName, input as Record<string, unknown>);

    // 자동 초기화된 경우 컨텍스트 요약을 응답 앞에 prepend
    if (contextPrefix && result.content?.length > 0) {
      const firstContent = result.content[0];
      if (firstContent.type === "text") {
        result.content[0] = {
          ...firstContent,
          text: contextPrefix + "--- Original Tool Response ---\n\n" + firstContent.text,
        };
      }
    }

    return result;
  };
}
```

동작 원리:
1. 첫 번째 도구 호출 시 `autoInitialize()`가 실행되어 세션 컨텍스트를 로드한다.
2. 초기화 결과(컨텍스트 요약)를 원래 도구 응답 앞에 prepend하여, AI가 워크스페이스 정보, 최근 결정, lessons 등을 즉시 인식하게 한다.
3. `session_init` 도구 자체는 자체 초기화를 수행하므로 skipAutoInit으로 제외된다.
4. 후속 도구 호출에서는 `initialized === true`이므로 `autoInitialize()`가 즉시 `null`을 반환한다.
5. 모든 도구 호출 후 `trackToolCall()`로 체크포인팅 추적이 수행된다.

이 패턴이 MCP 표준의 Tools primitive만을 사용하기 때문에 모든 MCP 클라이언트에서 보편적으로 동작한다.

### 2.2 IDE 루트 감지

`autoInitialize()` 내부에서 3단계 방식으로 워크스페이스 경로를 감지한다:

**Method 1 -- MCP listRoots**:
```typescript
const capabilities = this.server.server.getClientCapabilities();
if (capabilities?.roots) {
  const rootsResponse = await this.server.server.listRoots();
  this.ideRoots = rootsResponse.roots.map(r => r.uri.replace("file://", ""));
}
```
MCP 클라이언트가 `roots` capability를 지원하면 `listRoots()`를 호출하여 정확한 워크스페이스 경로를 획득한다. 이는 가장 신뢰할 수 있는 방법이다.

**Method 2 -- 환경 변수**:
```typescript
const envWorkspace = process.env.WORKSPACE_FOLDER
  || process.env.VSCODE_WORKSPACE_FOLDER
  || process.env.PROJECT_DIR
  || process.env.PWD;
```
IDE가 프로세스 환경에 워크스페이스 경로를 설정한 경우 사용한다. `HOME` 디렉토리는 제외한다.

**Method 3 -- cwd 프로젝트 감지**:
```typescript
const projectIndicators = [".git", "package.json", "Cargo.toml", "pyproject.toml", ".contextstream"];
const hasProjectIndicator = projectIndicators.some(f => fs.existsSync(`${cwd}/${f}`));
```
현재 작업 디렉토리에 프로젝트 지시자 파일이 존재하면 해당 경로를 사용한다.

모든 방법이 실패하면 `folderPath` 힌트(도구에서 전달된 경로)를 사용한다.

### 2.3 client.initSession() 호출

IDE 루트가 결정되면 `_doInitialize()`에서 ContextStream 클라이언트를 통해 세션을 초기화한다:

```typescript
const context = await this.client.initSession(
  {
    auto_index: true,
    include_recent_memory: true,
    include_decisions: true,
  },
  this.ideRoots
);
```

반환된 `context` 객체에서:
- `workspace_id`, `project_id`를 클라이언트 기본값으로 설정 (`client.setDefaults()`)
- `workspace_name`, `workspace_source`, `workspace_created`, `project_created`, `indexing_status` 등을 컨텍스트 요약에 포함
- `recent_decisions`, `recent_memory`, `lessons`, `lessons_warning` 등을 AI용 요약으로 포맷팅

### 2.4 buildContextSummary()

초기화 컨텍스트를 AI가 읽기 쉬운 텍스트로 변환한다. 워크스페이스 상태에 따라 3가지 분기가 있다:

1. **`requires_workspace_name`**: 매칭되는 워크스페이스가 없는 경우 -- 사용자에게 이름을 물어보라는 안내와 `workspace_bootstrap` 도구 사용법을 포함한다.
2. **`requires_workspace_selection`**: 여러 후보 워크스페이스가 존재하는 경우 -- 후보 목록을 표시하고 `workspace_associate` 도구 사용을 안내한다.
3. **정상 초기화**: 워크스페이스명, 프로젝트 정보, 인덱싱 상태, 최근 결정, 최근 컨텍스트, lessons, IDE 루트를 포맷된 텍스트로 출력한다.

### 2.5 동시 초기화 방지

```typescript
if (this.initializationPromise) {
  await this.initializationPromise;
  return null;
}
this.initializationPromise = this._doInitialize();
```

여러 도구가 동시에 호출되는 경우 `initializationPromise`를 통해 단일 초기화만 진행되며, 나머지 호출은 대기 후 `null`을 반환한다. 초기화 실패 시에도 `initialized = true`로 설정하여 무한 재시도 루프를 방지한다.

---

## 3. 토큰 추적

### 3.1 세션 토큰 모델

MCP 서버는 AI의 실제 토큰 사용량(응답, 사고 과정, 시스템 프롬프트)을 직접 관찰할 수 없다. ContextStream은 두 가지 소스를 결합한 추정 모델을 사용한다:

```typescript
getSessionTokens(): number {
  const turnEstimate = this.conversationTurns * SessionManager.TOKENS_PER_TURN_ESTIMATE;
  return this.sessionTokens + turnEstimate;
}
```

1. **실제 추적 토큰** (`sessionTokens`): `addTokens()` 메서드를 통해 도구 응답의 토큰을 직접 추적한다. 문자열 입력 시 `Math.ceil(text.length / 4)`로 추정한다.
2. **턴 기반 추정** (`conversationTurns * 3000`): 각 대화 턴에는 사용자 메시지(~500), AI 응답(~1500), 시스템 프롬프트 오버헤드(~500), 추론(~1500)이 포함된다고 가정하여 턴당 3,000 토큰으로 추정한다.

### 3.2 컨텍스트 압력 계산

`contextThreshold`는 기본 70,000 토큰으로 설정되어 있다 (100k 컨텍스트 윈도우 기준 보수적 값). `setContextThreshold()`로 클라이언트가 모델 정보를 전달하면 조정 가능하다.

4단계 압력 레벨은 `tools.ts`의 `context_smart` 도구에서 계산된다 (session-manager.ts에서는 토큰 수만 제공):

| 레벨 | 조건 | 의미 |
|------|------|------|
| `low` | 사용량 < 50% | 정상 운영 |
| `moderate` | 50% <= 사용량 < 70% | 주의 필요 |
| `high` | 70% <= 사용량 < 90% | 곧 컴팩션 필요, 응답 간결화 권장 |
| `critical` | 사용량 >= 90% | 즉시 컴팩션 필요 |

`markContextSmartCalled()`가 호출될 때마다 `conversationTurns`가 증가하여 턴 기반 추정이 갱신된다.

### 3.3 토큰 리셋

`resetTokenCount()`는 `sessionTokens`와 `conversationTurns`를 모두 0으로 리셋한다. 컴팩션 후 또는 새 세션 시작 시 호출된다.

---

## 4. 연속 체크포인팅

### 4.1 개요

연속 체크포인팅은 세션 중 주기적으로 세션 상태를 ContextStream에 저장하여, 비정상 종료나 컴팩션 시 복원 기반을 제공한다. `CONTEXTSTREAM_CHECKPOINT_ENABLED=true` 환경 변수로 활성화된다.

### 4.2 도구 호출 추적

`trackToolCall(toolName, input?)`이 모든 도구 호출 후 실행된다 (`withAutoContext` 래퍼에 의해 자동 호출):

```typescript
trackToolCall(toolName: string, input?: Record<string, unknown>): void {
  this.toolCallCount++;
  this.recentToolCalls.push({ name: toolName, timestamp: Date.now() });

  // 최근 50개만 유지
  if (this.recentToolCalls.length > 50) {
    this.recentToolCalls = this.recentToolCalls.slice(-50);
  }

  // 파일 경로 추적 (file_path, notebook_path, path 필드)
  if (input) {
    const filePath = input.file_path || input.notebook_path || input.path;
    if (filePath && typeof filePath === "string") {
      this.activeFiles.add(filePath);
      // 최근 30개만 유지
      if (this.activeFiles.size > 30) {
        const arr = Array.from(this.activeFiles);
        this.activeFiles = new Set(arr.slice(-30));
      }
    }
  }

  this.maybeCheckpoint();
}
```

### 4.3 주기적 체크포인트

`maybeCheckpoint()`는 마지막 체크포인트 이후 `checkpointInterval` (기본 20) 이상의 도구 호출이 발생하면 `saveCheckpoint("periodic")`를 실행한다.

### 4.4 체크포인트 저장

```typescript
async saveCheckpoint(trigger: "periodic" | "milestone" | "manual"): Promise<boolean>
```

`client.captureContext()`를 호출하여 다음 데이터를 `/memory/events`에 `session_snapshot` 이벤트로 저장한다:

```json
{
  "trigger": "periodic",
  "checkpoint_number": 3,
  "tool_call_count": 60,
  "session_tokens": 15000,
  "active_files": ["/src/index.ts", "/src/utils.ts"],
  "recent_tools": ["Read", "Edit", "Grep", "Edit"],
  "captured_at": "2026-03-17T10:30:00.000Z",
  "auto_captured": true
}
```

- `trigger` 유형: `periodic` (주기적), `milestone` (중요 이정표), `manual` (명시적 요청)
- `importance`: periodic은 "low", milestone/manual은 "medium"
- 태그: `["session_snapshot", "checkpoint", trigger]`

### 4.5 설정 API

- `setCheckpointEnabled(enabled)`: 런타임에서 체크포인팅 활성화/비활성화
- `setCheckpointInterval(interval)`: 체크포인트 간격 설정 (최소 5, 스팸 방지)

---

## 5. 컴팩션 후 복원

### 5.1 컴팩션 감지: shouldRestorePostCompact()

MCP 서버는 에디터의 컨텍스트 컴팩션 이벤트를 직접 수신하지 못한다. 대신 토큰 추적 데이터의 급격한 변화를 통해 컴팩션 발생을 추론하는 휴리스틱을 사용한다.

```typescript
shouldRestorePostCompact(): boolean {
  // 이미 복원 완료됨
  if (this.postCompactRestoreCompleted) return false;

  // high pressure 기록 없음
  if (!this.lastHighPressureAt) return false;

  // high pressure가 10분 이전 기록 (너무 오래됨)
  const elapsed = Date.now() - this.lastHighPressureAt;
  if (elapsed > 10 * 60 * 1000) return false;

  // 토큰 수가 충분히 감소했는지 확인
  const currentTokens = this.getSessionTokens();
  const tokenDrop = this.lastHighPressureTokens - currentTokens;

  // 50% 이상 감소 AND 현재 10k 미만
  if (currentTokens > 10000 || tokenDrop < this.lastHighPressureTokens * 0.5) return false;

  return true;
}
```

감지 조건 요약:
1. `markHighContextPressure()`가 이전에 호출되어 high/critical 압력이 기록되었어야 한다.
2. 기록 시점으로부터 10분 이내여야 한다.
3. 현재 토큰 수가 10,000 미만이어야 한다.
4. 이전 high pressure 시점 대비 50% 이상의 토큰 감소가 있어야 한다.

### 5.2 복원 실행

`markPostCompactRestoreCompleted()`가 호출되면:
- `postCompactRestoreCompleted = true` 설정 (세션 내 재시도 방지)
- `lastHighPressureAt = null` 및 `lastHighPressureTokens = 0` 리셋

실제 복원 로직은 `tools.ts`의 `context_smart` 도구에서 수행된다. `shouldRestorePostCompact()`가 true를 반환하면, ContextStream API에서 가장 최근 스냅샷을 가져와 AI 응답에 주입한다.

### 5.3 압력 기록

`markHighContextPressure()`는 `context_smart` 도구가 high 또는 critical 압력 레벨을 감지할 때 호출된다:

```typescript
markHighContextPressure() {
  this.lastHighPressureAt = Date.now();
  this.lastHighPressureTokens = this.getSessionTokens();
}
```

이 데이터가 이후 `shouldRestorePostCompact()`의 비교 기준이 된다.

---

## 6. 스코프 업데이트

### 6.1 updateScope()

```typescript
updateScope(input: { workspace_id?: string; project_id?: string; folder_path?: string })
```

이 메서드는 세션 ID를 유지하면서 활성 워크스페이스/프로젝트 스코프만 변경한다. 도구가 로컬 인덱스 컨텍스트에서 스코프를 자동 해결할 때 사용된다.

동작:
1. 비어있거나 공백인 입력은 무시한다.
2. `this.context`에 새 workspace_id, project_id, folder_path를 설정한다.
3. workspace_id 또는 project_id가 변경되면 `initialized = true`로 표시하고 `client.setDefaults()`를 호출하여 이후 API 호출에 반영한다.

### 6.2 markInitialized()

```typescript
markInitialized(context: Record<string, unknown>)
```

명시적 `session_init` 도구 호출 시 사용된다. 자동 초기화와 달리 전달된 context 전체를 저장하고, workspace/project 기본값을 설정하며, `extractDefaultSearchMode()`로 워크스페이스/프로젝트의 기본 검색 모드를 추출한다.

### 6.3 검색 모드 해결

`extractDefaultSearchMode()`는 context에서 `workspace.default_search_mode` 또는 `project.default_search_mode`를 추출한다. 워크스페이스 레벨 설정이 프로젝트 레벨보다 우선한다. 이 값은 `getDefaultSearchMode()`로 검색 도구에 전달된다.

---

## 7. 세션 라이프사이클

### 7.1 전체 흐름

아래는 ContextStream 세션의 시작부터 종료까지의 전체 라이프사이클이다.

```
[IDE 시작]
     |
     v
[MCP 서버 프로세스 생성]
     |
     v
new SessionManager(server, client)
  - sessionId = "mcp-{UUID}" 생성
  - initialized = false
  - checkpointEnabled 환경 변수 확인
     |
     v
[session-init 훅 실행] (SessionStart 이벤트)
  - cleanupStale(360) -- 6분 이상 된 prompt-state 정리
  - markInitRequired(cwd)
  - fetchSessionContext() -- rules, lessons, plans, tasks 로드 (5초 타임아웃)
  - attemptAutoUpdate() -- 자동 업데이트 시도 (병렬)
  - getUpdateNotice() -- 버전 확인 (병렬)
  - formatContext() -> hookSpecificOutput.additionalContext
     |
     v
[사용자 첫 메시지]
     |
     v
[user-prompt-submit 훅 실행]
  - cleanupStale(180)
  - markContextRequired(cwd)
  - markInitRequired(cwd) (새 세션 감지 시)
  - Claude: /context/hook -> 빠른 컨텍스트 (Redis 캐시)
  - 비-Claude: /context/smart -> 향상된 리마인더
     |
     v
[첫 도구 호출] -- withAutoContext 래퍼가 인터셉트
     |
     v
[pre-tool-use 훅]
  - isInitRequired? -> init 아니면 차단
  - isContextRequired? -> context 아니면 차단
  - isProjectIndexed? -> discovery 도구 차단/리다이렉트
     |
     v
sessionManager.autoInitialize()  -- 첫 도구에서만 실행
  |
  +-- Method 1: listRoots (MCP capability)
  +-- Method 2: 환경 변수 (WORKSPACE_FOLDER 등)
  +-- Method 3: cwd 프로젝트 지시자 (.git, package.json 등)
  |
  v
client.initSession({auto_index: true, ...}, ideRoots)
  - 워크스페이스 매칭/생성
  - 프로젝트 매칭/생성
  - 자동 인덱싱 시작
  - 최근 결정, 메모리, lessons 로드
  |
  v
contextSummary -> 도구 응답에 prepend
initialized = true
client.setDefaults({workspace_id, project_id})
     |
     v
[이후 메시지마다]
  |
  +-- user-prompt-submit: markContextRequired, 컨텍스트 리마인더 주입
  +-- pre-tool-use: init/context 필수 확인, discovery 차단
  +-- 도구 실행: withAutoContext -> handler() -> trackToolCall()
  |
  v
[매 도구 호출 후]
  - trackToolCall() -> activeFiles, recentToolCalls 업데이트
  - maybeCheckpoint() -> 20회마다 periodic 체크포인트
  |
  +-- [Edit/Write/NotebookEdit 후] post-write 훅 -> 실시간 인덱싱
  +-- [Bash 후] on-bash 훅 -> 명령 캡처, 에러 lesson
  +-- [실패 후] post-tool-use-failure -> fingerprint 추적
  |
  v
[context_smart 호출마다]
  - markContextSmartCalled() -> conversationTurns++
  - addTokens(응답 크기)
  - 컨텍스트 압력 계산
    - high/critical -> markHighContextPressure()
  - shouldRestorePostCompact()? -> 스냅샷 복원
     |
     v
[컨텍스트 압력 증가]
  |
  v
[컴팩션 발생] -- 수동(/compact) 또는 자동
  |
  +-- pre-compact 훅: 트랜스크립트 파싱, 스냅샷/전체 트랜스크립트 저장
  |
  v
[컴팩션 완료]
  |
  +-- post-compact 훅: 마지막 트랜스크립트 페치, 상태 요약 주입
  |
  v
[다음 context_smart 호출]
  - shouldRestorePostCompact() == true
    (10분 내 + 50%+ 토큰 감소 + 현재 < 10k)
  - 스냅샷에서 컨텍스트 복원
  - markPostCompactRestoreCompleted()
     |
     v
[세션 계속...]
     |
     v
[세션 종료]
  |
  +-- stop 훅: 체크포인트 메모리 이벤트 기록
  +-- session-end 훅: 전체 트랜스크립트 저장, 세션 요약 이벤트
```

### 7.2 컨텍스트 미호출 경고

`warnIfContextSmartNotCalled(toolName)`은 세션이 초기화된 후 `context_smart`가 한 번도 호출되지 않은 상태에서 다른 도구가 호출되면 stderr에 경고를 출력한다. `session_init`, `context_smart`, `session_recall`, `session_remember` 도구는 경고를 건너뛴다. 경고는 세션당 최대 1회만 표시된다.

### 7.3 폴더 경로 관리

- `setFolderPath(path)`: 도구가 워크스페이스 경로를 알고 있을 때 힌트로 설정
- `getFolderPath()`: 현재 폴더 경로 반환
- `autoInitialize()` 내부에서 ideRoots가 비어있을 때 폴더 경로 힌트를 사용

### 7.4 스코프 업데이트와 초기화의 차이

| 측면 | autoInitialize() / markInitialized() | updateScope() |
|------|--------------------------------------|---------------|
| sessionId | 변경 안 됨 | 변경 안 됨 |
| initialized | false -> true | 이미 true (또는 true로 설정) |
| context 전체 | 전체 교체 | 부분 업데이트 |
| client.setDefaults | 호출됨 | 호출됨 |
| IDE 루트 감지 | 수행됨 | 수행 안 됨 |
| 용도 | 세션 최초 설정 | 워크스페이스 전환 |

### 7.5 토큰 추적과 세션 라이프사이클의 관계

토큰 추적은 세션의 "건강 상태"를 모니터링하는 핵심 메커니즘이다:

1. **도구 응답**: `addTokens(content)` -- 실제 텍스트 기반 토큰 추적
2. **대화 턴**: `markContextSmartCalled()` -- 턴 카운터 증가, 보이지 않는 토큰(AI 응답, 시스템 프롬프트) 추정
3. **압력 모니터링**: `getSessionTokens()` / `contextThreshold` 비교
4. **컴팩션 감지**: high/critical 압력 후 급격한 토큰 감소
5. **리셋**: 컴팩션 후 `resetTokenCount()`

이 전체 사이클이 `context_smart` 도구 내부에서 매 호출마다 평가되어, 적절한 시점에 컴팩션 권고나 자동 복원이 이루어진다.
