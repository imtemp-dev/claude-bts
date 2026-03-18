# claude-mem -- Worker Service 분석

## 1. Worker Service 아키텍처 (`worker-service.ts`, 1,251L)

### 1.1 설계 철학

`WorkerService`는 원래 2,000줄 이상의 monolith에서 ~300줄의 slim orchestrator로 리팩토링되었다. 비즈니스 로직을 전문화된 모듈에 위임하고, 자신은 초기화 조정과 서비스 간 연결만 담당한다.

**위임 대상:**
- `src/services/server/` -- HTTP 서버, 미들웨어, 에러 처리
- `src/services/infrastructure/` -- 프로세스 관리, 헬스 모니터링, 종료 처리
- `src/services/integrations/` -- IDE 통합 (Cursor)
- `src/services/worker/` -- 비즈니스 로직, 라우트, 에이전트

### 1.2 클래스 구조

```typescript
export class WorkerService {
  // 핵심 인프라
  private server: Server;
  private mcpClient: Client;

  // 초기화 플래그
  private mcpReady: boolean = false;
  private initializationCompleteFlag: boolean = false;
  private isShuttingDown: boolean = false;

  // 서비스 레이어
  private dbManager: DatabaseManager;
  private sessionManager: SessionManager;
  private sseBroadcaster: SSEBroadcaster;
  private sdkAgent: SDKAgent;
  private geminiAgent: GeminiAgent;
  private openRouterAgent: OpenRouterAgent;
  private paginationHelper: PaginationHelper;
  private settingsManager: SettingsManager;
  private sessionEventBroadcaster: SessionEventBroadcaster;

  // 라우트 핸들러
  private searchRoutes: SearchRoutes | null = null;

  // Chroma MCP 관리자
  private chromaMcpManager: ChromaMcpManager | null = null;

  // 프로세스 관리
  private stopOrphanReaper: (() => void) | null = null;
  private staleSessionReaperInterval: ReturnType<typeof setInterval> | null = null;

  // AI 상호작용 추적
  private lastAiInteraction: { timestamp, success, provider, error? } | null = null;
}
```

### 1.3 생성자 초기화 순서

1. `initializationComplete` Promise 생성 (background init 완료 시 resolve)
2. 서비스 레이어 초기화:
   - `DatabaseManager` -> `SessionManager(dbManager)` -> `SSEBroadcaster`
   - `SDKAgent(dbManager, sessionManager)` -> `GeminiAgent(...)` -> `OpenRouterAgent(...)`
   - `PaginationHelper(dbManager)` -> `SettingsManager(dbManager)`
   - `SessionEventBroadcaster(sseBroadcaster, this)`
3. `SessionManager.setOnSessionDeleted` 콜백 설정 (processing status 브로드캐스트)
4. MCP Client 초기화 (`worker-search-proxy`)
5. HTTP Server 생성 (`Server` 인스턴스, 콜백 주입)
6. 라우트 핸들러 등록 (`registerRoutes`)
7. 시그널 핸들러 등록 (`registerSignalHandlers`)

### 1.4 라우트 등록 순서

라우트 등록은 Express의 처리 순서를 고려하여 다음 순서로 수행된다:

1. **Early handler**: `/api/context/inject` -- 초기화 전이면 빈 응답 반환 (fail-open)
2. **Guard middleware**: `/api/*` -- 초기화 전이면 30초까지 대기, 타임아웃 시 503 반환
3. **ViewerRoutes** -- SSE, 웹 UI
4. **SessionRoutes** -- 세션 라이프사이클
5. **DataRoutes** -- 데이터 CRUD
6. **SettingsRoutes** -- 설정 관리
7. **LogsRoutes** -- 로그 조회
8. **MemoryRoutes** -- 메모리 관리
9. **SearchRoutes** -- 검색 (background init 완료 후 동적 등록)

`SearchRoutes`는 DB와 검색 인덱스 초기화가 완료된 후 `initializeBackground()`에서 동적으로 등록된다.

### 1.5 start() 메서드

```
start()
  -> startSupervisor()                    // Supervisor 프로세스 관리자 시작
  -> server.listen(port, host)            // HTTP 서버 즉시 시작
  -> writePidFile({pid, port, startedAt}) // PID 파일 기록
  -> getSupervisor().registerProcess()    // worker 프로세스 등록
  -> initializeBackground()              // 비차단 배경 초기화 시작
```

HTTP 서버를 먼저 시작하여 훅이 즉시 연결할 수 있도록 하고, 느린 초기화(DB, 검색, MCP)는 배경에서 수행한다.

### 1.6 배경 초기화 (`initializeBackground`)

```
initializeBackground()
  -> aggressiveStartupCleanup()          // 이전 인스턴스 정리
  -> ModeManager.loadMode()              // 모드 설정 로드
  -> runOneTimeChromaMigration()          // Chroma 마이그레이션 (local 모드만)
  -> ChromaMcpManager.getInstance()       // Chroma MCP 관리자 초기화
  -> dbManager.initialize()              // SQLite 데이터베이스 초기화
  -> PendingMessageStore.resetStaleProcessingMessages(0)  // 미처리 메시지 리셋
  -> SearchManager 초기화                 // 검색 서비스 구성
  -> SearchRoutes 등록                    // 검색 라우트 동적 등록
  -> initializationCompleteFlag = true    // 초기화 완료 마킹
  -> ChromaSync.backfillAllProjects()     // Chroma 백필 (fire-and-forget)
  -> MCP 서버 연결                         // StdioClientTransport로 MCP 연결
  -> startOrphanReaper()                  // 좀비 프로세스 수거기 시작 (30초 간격)
  -> staleSessionReaperInterval 시작      // 오래된 세션 수거 (2분 간격)
```

MCP 서버 연결에는 5분 타임아웃이 적용된다. 연결 실패 시 `transport.close()`로 서브프로세스를 정리한다. MCP 서버 프로세스는 Supervisor에 등록되어 exit 이벤트 시 자동 해제된다.

### 1.7 Windows 스폰 쿨다운

Windows에서 반복적인 스폰 실패로 인한 팝업 문제(#921)를 방지하기 위해, 2분 쿨다운 메커니즘을 사용한다. `.worker-start-attempted` 잠금 파일의 수정 시각을 확인하여, 최근 2분 이내에 시도했으면 스폰을 건너뛴다.

### 1.8 StatusOutput 인터페이스

```typescript
interface StatusOutput {
  continue: true;
  suppressOutput: true;
  status: 'ready' | 'error';
  message?: string;
}
```

`buildStatusOutput` 함수는 훅 프레임워크와의 통신을 위한 표준화된 상태 출력을 생성한다.

---

## 2. Worker 타입 (`worker-types.ts`, 211L)

### 2.1 ActiveSession -- 핵심 런타임 상태

```typescript
interface ActiveSession {
  sessionDbId: number;
  contentSessionId: string;        // Claude Code 세션 ID
  memorySessionId: string | null;  // 메모리 에이전트 세션 ID (resume용)
  project: string;
  userPrompt: string;
  pendingMessages: PendingMessage[];    // deprecated (FK 제약 이후 비어있음)
  abortController: AbortController;
  generatorPromise: Promise<void> | null;
  lastPromptNumber: number;
  startTime: number;
  cumulativeInputTokens: number;
  cumulativeOutputTokens: number;
  earliestPendingTimestamp: number | null;
  conversationHistory: ConversationMessage[];  // 프로바이더 전환 시 공유 히스토리
  currentProvider: 'claude' | 'gemini' | 'openrouter' | null;
  consecutiveRestarts: number;         // 무한 재시작 방지
  forceInit?: boolean;                 // 강제 신규 세션 (resume 건너뛰기)
  idleTimedOut?: boolean;              // idle 타임아웃으로 종료 표시
  lastGeneratorActivity: number;       // stale 감지용 타임스탬프 (#1099)
  processingMessageIds: number[];      // CLAIM-CONFIRM 패턴 메시지 추적
}
```

`conversationHistory`는 `ConversationMessage[]` 형태로 프로바이더 간 컨텍스트를 공유한다:
```typescript
interface ConversationMessage {
  role: 'user' | 'assistant';
  content: string;
}
```

### 2.2 PendingMessage / PendingMessageWithId

```typescript
interface PendingMessage {
  type: 'observation' | 'summarize';
  tool_name?: string;
  tool_input?: any;
  tool_response?: any;
  prompt_number?: number;
  cwd?: string;
  last_assistant_message?: string;
}

interface PendingMessageWithId extends PendingMessage {
  _persistentId: number;        // DB에서의 고유 ID (처리 완료 확인용)
  _originalTimestamp: number;    // 최초 큐잉 시각 (정확한 관측 타임스탬프용)
}
```

### 2.3 데이터베이스 레코드 타입

**Observation:**
```typescript
interface Observation {
  id: number;
  memory_session_id: string;
  project: string;
  type: string;              // 'discovery', 'decision', 'bugfix', 'feature', 'refactor'
  title: string;
  subtitle: string | null;
  text: string | null;
  narrative: string | null;
  facts: string | null;
  concepts: string | null;
  files_read: string | null;     // JSON 배열 문자열
  files_modified: string | null; // JSON 배열 문자열
  prompt_number: number;
  created_at: string;
  created_at_epoch: number;
}
```

**Summary:**
```typescript
interface Summary {
  id: number;
  session_id: string;       // content_session_id (JOIN)
  project: string;
  request: string | null;
  investigated: string | null;
  learned: string | null;
  completed: string | null;
  next_steps: string | null;
  notes: string | null;
  created_at: string;
  created_at_epoch: number;
}
```

**DBSession:**
```typescript
interface DBSession {
  id: number;
  content_session_id: string;
  project: string;
  user_prompt: string;
  memory_session_id: string | null;
  status: 'active' | 'completed' | 'failed';
  started_at: string;
  started_at_epoch: number;
  completed_at: string | null;
  completed_at_epoch: number | null;
}
```

### 2.4 페이지네이션 타입

```typescript
interface PaginatedResult<T> {
  items: T[];
  hasMore: boolean;
  offset: number;
  limit: number;
}

interface PaginationParams {
  offset: number;
  limit: number;
  project?: string;
}
```

### 2.5 SSE 타입

```typescript
interface SSEEvent {
  type: string;
  timestamp?: number;
  [key: string]: any;
}

type SSEClient = Response;  // Express Response 객체
```

### 2.6 ParsedObservation / ParsedSummary

SDK agent가 AI 응답에서 파싱한 결과 타입:

```typescript
interface ParsedObservation {
  type: string;
  title: string;
  subtitle: string | null;
  text: string;
  concepts: string[];
  files: string[];
}

interface ParsedSummary {
  request: string | null;
  investigated: string | null;
  learned: string | null;
  completed: string | null;
  next_steps: string | null;
  notes: string | null;
}
```

### 2.7 DatabaseStats

```typescript
interface DatabaseStats {
  totalObservations: number;
  totalSessions: number;
  totalPrompts: number;
  totalSummaries: number;
  projectCounts: Record<string, {
    observations: number;
    sessions: number;
    prompts: number;
    summaries: number;
  }>;
}
```

---

## 3. 세션 관리 (`SessionManager.ts`, 503L)

### 3.1 역할

`SessionManager`는 활성 세션의 전체 라이프사이클을 관리하는 이벤트 기반 세션 관리자이다. HTTP 요청과 SDK agent 사이의 메시지 큐를 조정하며, 폴링 없이 zero-latency 이벤트 알림을 제공한다.

### 3.2 내부 상태

```typescript
class SessionManager {
  private dbManager: DatabaseManager;
  private sessions: Map<number, ActiveSession> = new Map();
  private sessionQueues: Map<number, EventEmitter> = new Map();
  private onSessionDeletedCallback?: () => void;
  private pendingStore: PendingMessageStore | null = null;  // lazy init
}
```

`PendingMessageStore`는 lazy initialization으로 순환 의존성을 방지한다.

### 3.3 세션 초기화 (`initializeSession`)

```
initializeSession(sessionDbId, currentUserPrompt?, promptNumber?)
  -> 기존 활성 세션 확인:
     -> 있으면: 프로젝트 DB에서 갱신, userPrompt 갱신, 반환
     -> 없으면:
        -> dbManager.getSessionById()로 DB에서 로드
        -> CRITICAL: memorySessionId를 null로 설정 (#817)
           (worker 재시작 시 stale SDK 컨텍스트 방지)
        -> ActiveSession 객체 생성
        -> sessions Map에 등록
        -> EventEmitter 생성 (sessionQueues Map에 등록)
```

**Issue #817 핵심:** DB에 저장된 `memory_session_id`는 worker가 재시작되면 SDK 컨텍스트가 사라지므로 stale 상태가 된다. 이를 로드하면 "No conversation found" 크래시가 발생한다. 따라서 새 in-memory 세션 생성 시 항상 `memorySessionId: null`로 시작하고, SDK agent가 첫 응답에서 새 ID를 캡처한다.

### 3.4 observation 큐잉 (`queueObservation`)

```
queueObservation(sessionDbId, data)
  -> sessions Map에 없으면 initializeSession()으로 자동 초기화
  -> PendingMessageStore.enqueue()로 DB에 먼저 영속화 (crash-safe)
  -> EventEmitter.emit('message')로 generator에 즉시 알림
```

DB 영속화를 먼저 수행하여 worker 크래시 시에도 observation이 유실되지 않도록 한다. DB 쓰기 실패 시 예외를 상위로 전파하여 in-memory 큐에 반영 없이 실패 처리한다.

### 3.5 세션 삭제 (`deleteSession`)

5단계 정리 프로세스:

1. **AbortController.abort()** -- SDK agent에 중단 시그널 전송
2. **Generator 대기** -- `generatorPromise`가 있으면 30초 타임아웃으로 대기 (#1099)
3. **서브프로세스 종료 확인** -- `getProcessBySession()`로 추적된 프로세스 확인, 5초 타임아웃으로 `ensureProcessExit()` 호출 (#737)
4. **Supervisor reap** -- `getSupervisor().getRegistry().reapSession()`으로 supervisor-tracked 프로세스 정리 (#1351)
5. **Map 정리** -- sessions, sessionQueues에서 제거, 콜백 호출

### 3.6 `removeSessionImmediate`

SDK resume 실패 시 deadlock을 방지하기 위한 즉시 제거 메서드. `deleteSession()`이 generator promise를 await하면, generator 내부에서 호출 시 deadlock이 발생한다. 이 메서드는 Map에서 즉시 제거하고 콜백을 호출한다.

### 3.7 Stale 세션 수거 (`reapStaleSessions`)

```typescript
static readonly MAX_SESSION_IDLE_MS = 15 * 60 * 1000;  // 15분
```

generator가 없고 pending work가 없으며 15분 이상 경과한 세션을 수거한다. 이는 orphan reaper가 "활성" 세션의 프로세스를 건너뛰는 문제(#1168)를 해결한다.

### 3.8 메시지 이터레이터 (`getMessageIterator`)

```typescript
async *getMessageIterator(sessionDbId): AsyncIterableIterator<PendingMessageWithId>
```

SDKAgent가 소비하는 이벤트 기반 비동기 이터레이터:
- `SessionQueueProcessor`를 사용하여 robust iterator 생성
- `onIdleTimeout` 콜백으로 idle 시 abort 트리거 (좀비 프로세스 방지)
- 각 메시지의 `_originalTimestamp`로 `earliestPendingTimestamp` 추적
- `lastGeneratorActivity`를 갱신하여 stale 감지 지원 (#1099)

---

## 4. 브랜치 관리 (`BranchManager.ts`, 315L)

### 4.1 역할

`BranchManager`는 설치된 플러그인의 git 브랜치를 관리하여, 사용자가 UI에서 stable/beta 브랜치 간 전환할 수 있도록 한다. 설치된 플러그인(`~/.claude/plugins/marketplaces/thedotmack/`)이 git 저장소이므로 이를 활용한다.

### 4.2 보안

**Branch name validation:**
```typescript
function isValidBranchName(branchName: string): boolean {
  const validBranchRegex = /^[a-zA-Z0-9][a-zA-Z0-9._/-]*$/;
  return validBranchRegex.test(branchName) && !branchName.includes('..');
}
```

**Command injection 방지:**
```typescript
function execGit(args: string[]): string {
  const result = spawnSync('git', args, {
    cwd: INSTALLED_PLUGIN_PATH,
    shell: false  // CRITICAL: 사용자 입력에 shell 사용 금지
  });
}
```

`spawnSync`를 `shell: false`와 배열 기반 인수로 호출하여 command injection을 원천 차단한다.

### 4.3 타임아웃 상수

| 상수 | 값 | 용도 |
|------|-----|------|
| `GIT_COMMAND_TIMEOUT_MS` | 300,000ms (5분) | git 명령 타임아웃 |
| `NPM_INSTALL_TIMEOUT_MS` | 600,000ms (10분) | npm install 타임아웃 |
| `DEFAULT_SHELL_TIMEOUT_MS` | 60,000ms (1분) | 기본 셸 타임아웃 |

### 4.4 BranchInfo 인터페이스

```typescript
interface BranchInfo {
  branch: string | null;   // 현재 브랜치 이름
  isBeta: boolean;         // beta 접두사 여부
  isGitRepo: boolean;      // .git 디렉토리 존재 여부
  isDirty: boolean;        // uncommitted 변경 존재
  canSwitch: boolean;      // 전환 가능 여부
  error?: string;
}
```

### 4.5 브랜치 전환 흐름 (`switchBranch`)

```
switchBranch(targetBranch)
  1. isValidBranchName() 검증
  2. getBranchInfo()로 현재 상태 확인
  3. 이미 대상 브랜치면 즉시 반환
  4. git checkout -- .    // 로컬 변경 폐기
  5. git clean -fd         // 미추적 파일 제거
  6. git fetch origin
  7. git checkout {branch}  // 실패 시 -b origin/{branch}로 리모트 추적
  8. git pull origin {branch}
  9. .install-version 마커 삭제
  10. npm install
  11. 실패 시 원래 브랜치로 복구 시도
```

로컬 변경 폐기는 안전하다 -- 사용자 데이터는 `~/.claude-mem/`에 별도 저장된다.

### 4.6 업데이트 풀 (`pullUpdates`)

현재 브랜치의 최신 변경을 가져온다. `switchBranch`와 유사하지만 브랜치 전환 없이 `fetch` -> `pull` -> `npm install`만 수행한다.

---

## 5. 타임라인 서비스 (`TimelineService.ts`, 263L)

### 5.1 역할

`TimelineService`는 observations, sessions, prompts를 통합하여 시간순 타임라인을 생성하고 포맷팅한다.

### 5.2 TimelineItem 타입

```typescript
interface TimelineItem {
  type: 'observation' | 'session' | 'prompt';
  data: ObservationSearchResult | SessionSummarySearchResult | UserPromptSearchResult;
  epoch: number;  // 정렬용 타임스탬프
}
```

### 5.3 타임라인 구축 (`buildTimeline`)

세 가지 데이터 소스를 `TimelineItem` 배열로 변환하고 `epoch` 기준으로 오름차순 정렬한다.

### 5.4 깊이 필터링 (`filterByDepth`)

앵커 포인트를 기준으로 전후 N개 레코드를 선택한다:

- **숫자 앵커**: observation ID로 매칭
- **S 접두사 앵커**: `S123` 형태로 session ID 매칭
- **타임스탬프 앵커**: epoch 기준 가장 가까운 항목 찾기

### 5.5 타임라인 포맷팅 (`formatTimeline`)

Markdown 테이블 형식으로 포맷팅한다:

1. **헤더**: 쿼리 정보, 앵커 포인트, 윈도우 범위
2. **범례**: 관측 타입별 아이콘 설명
3. **일별 그룹화**: `dayMap`으로 같은 날의 항목을 묶음
4. **항목별 포맷팅**:
   - Session: 굵은 텍스트로 요약 표시
   - Prompt: 인용문 형태로 사용자 프롬프트 표시 (100자 절삭)
   - Observation: 테이블 행으로 ID, 시간, 타입 아이콘, 제목, 토큰 수 표시
5. **시간 중복 최적화**: 같은 시간의 연속 항목은 ditto mark(`"`)를 사용

### 5.6 토큰 추정

```typescript
private estimateTokens(text: string | null): number {
  if (!text) return 0;
  return Math.ceil(text.length / 4);  // ~4 chars per token
}
```

---

## 6. 포맷팅 서비스 (`FormattingService.ts`, 171L)

### 6.1 역할

`FormattingService`는 검색 결과를 일관된 테이블 형식으로 포맷팅한다. context-generator와 동일한 스타일을 사용하여 시각적 일관성을 유지한다.

### 6.2 인덱스 포맷

**Observation 인덱스:**
```
| #ID | Time | TypeIcon | Title | ~ReadTokens | WorkEmoji WorkTokens |
```

**Session 인덱스:**
```
| #S{ID} | Time | Target | Title | - | - |
```

**Prompt 인덱스:**
```
| #P{ID} | Time | Message | PromptText (60자 절삭) | - | - |
```

### 6.3 검색 결과 포맷 (Work 컬럼 없음)

검색 결과용 테이블은 Work 컬럼을 제거한 단순화된 형태:
```
| ID | Time | T | Title | Read |
```

### 6.4 Read 토큰 추정

```typescript
private estimateReadTokens(obs: ObservationSearchResult): number {
  const size = (obs.title?.length || 0) +
               (obs.subtitle?.length || 0) +
               (obs.narrative?.length || 0) +
               (obs.facts?.length || 0);
  return Math.ceil(size / CHARS_PER_TOKEN_ESTIMATE);  // 4 chars/token
}
```

### 6.5 시간 중복 최적화

모든 검색/테이블 포맷 함수는 `lastTime` 매개변수를 받아, 이전 행과 같은 시간이면 ditto mark(`"`)를 표시한다. 이는 동일 시간대의 연속 observation에서 시각적 중복을 줄인다.

### 6.6 검색 팁

`formatSearchTips()`는 검색 전략 가이드를 반환한다:
1. 인덱스 검색으로 제목, 날짜, ID 확인
2. 타임라인으로 관심 있는 결과 주변 컨텍스트 확인
3. `get_observations(ids=[...])`로 상세 정보 일괄 조회

---

## 7. 페이지네이션 (`PaginationHelper.ts`, 197L)

### 7.1 역할

`PaginationHelper`는 observations, summaries, prompts에 대한 페이지네이션을 DRY 원칙으로 구현한다.

### 7.2 LIMIT+1 트릭

`COUNT(*)` 쿼리를 회피하기 위해, 요청한 `limit + 1`개의 결과를 가져온다. 결과가 `limit`보다 많으면 `hasMore: true`를 설정하고, 반환 시 `slice(0, limit)`으로 정확한 개수만 반환한다.

```typescript
private paginate<T>(table, columns, offset, limit, project?): PaginatedResult<T> {
  const query = `SELECT ${columns} FROM ${table}
    ${project ? 'WHERE project = ?' : ''}
    ORDER BY created_at_epoch DESC
    LIMIT ? OFFSET ?`;
  params.push(limit + 1, offset);
  // ...
  return {
    items: results.slice(0, limit),
    hasMore: results.length > limit,
    offset, limit
  };
}
```

### 7.3 Summaries 페이지네이션

Summaries는 `session_summaries`와 `sdk_sessions` 테이블을 JOIN하여 `content_session_id`를 포함한다:

```sql
SELECT ss.id, s.content_session_id as session_id,
       ss.request, ss.investigated, ss.learned, ss.completed,
       ss.next_steps, ss.project, ss.created_at, ss.created_at_epoch
FROM session_summaries ss
JOIN sdk_sessions s ON ss.memory_session_id = s.memory_session_id
```

### 7.4 Prompts 페이지네이션

```sql
SELECT up.id, up.content_session_id, s.project,
       up.prompt_number, up.prompt_text, up.created_at, up.created_at_epoch
FROM user_prompts up
JOIN sdk_sessions s ON up.content_session_id = s.content_session_id
```

### 7.5 프로젝트 경로 정규화

`stripProjectPath(filePath, projectName)`는 절대 경로에서 프로젝트 이름 이후 부분만 추출한다:
- `/Users/user/project/src/file.ts` -> `src/file.ts`
- 프로젝트 이름이 경로에 없으면 원본 반환

`stripProjectPaths`는 JSON 배열 문자열의 각 경로에 이 처리를 적용한다.

---

## 8. 프로세스 레지스트리 (`ProcessRegistry.ts`, 463L)

### 8.1 문제 배경 (Issue #737)

SDK의 `SpawnedProcess` 인터페이스가 서브프로세스 PID를 숨기고, `deleteSession()`이 서브프로세스 종료를 확인하지 않으며, `abort()`가 fire-and-forget이어서, 좀비 프로세스가 누적되는 문제가 있었다. 사용자 보고에 따르면 155개 프로세스 / 51GB RAM이 사용되기도 했다.

### 8.2 TrackedProcess 인터페이스

```typescript
interface TrackedProcess {
  pid: number;
  sessionDbId: number;
  spawnedAt: number;
  process: ChildProcess;
}
```

### 8.3 Supervisor 통합

`ProcessRegistry`는 자체 Map 대신 `Supervisor`의 레지스트리를 사용한다:

```typescript
function getTrackedProcesses(): TrackedProcess[] {
  return getSupervisor().getRegistry()
    .getAll()
    .filter(record => record.type === 'sdk')
    .map(record => {
      const processRef = getSupervisor().getRegistry().getRuntimeProcess(record.id);
      // ...
    });
}
```

### 8.4 프로세스 등록/해제

```typescript
registerProcess(pid, sessionDbId, process):
  -> getSupervisor().registerProcess(`sdk:${sessionDbId}:${pid}`, {...}, process)

unregisterProcess(pid):
  -> getSupervisor().unregisterProcess(record.id)
  -> notifySlotAvailable()  // pool waiter 알림
```

### 8.5 프로세스 풀 관리

**Hard cap:** `TOTAL_PROCESS_HARD_CAP = 10` -- pool accounting과 무관하게 절대 상한.

```typescript
waitForSlot(maxConcurrent, timeoutMs = 60_000):
  -> activeCount >= TOTAL_PROCESS_HARD_CAP 이면 즉시 에러
  -> activeCount < maxConcurrent 이면 즉시 반환
  -> slotWaiters 배열에 콜백 등록
  -> 프로세스 종료 시 notifySlotAvailable()이 waiter 해제
  -> timeoutMs 경과 시 에러
```

### 8.6 프로세스 종료 보장 (`ensureProcessExit`)

```
ensureProcessExit(tracked, timeoutMs = 5000)
  -> proc.exitCode !== null 이면 즉시 unregister
  -> exit 이벤트 대기 (event-based, polling 아님)
  -> timeout 경과:
     -> SIGKILL 전송
     -> 1초 추가 대기
     -> unregister
```

**핵심:** `proc.killed` 대신 `proc.exitCode`만 신뢰한다. `proc.killed`는 Node가 시그널을 보냈다는 것만 의미하며, 프로세스가 실제로 종료되었는지는 보장하지 않는다.

### 8.7 PID 캡처 스폰 (`createPidCapturingSpawn`)

SDK의 `spawnClaudeCodeProcess` 옵션에 주입되는 커스텀 스폰 함수:

```typescript
createPidCapturingSpawn(sessionDbId):
  return (spawnOptions) => {
    getSupervisor().assertCanSpawn('claude sdk');
    const child = spawn(command, args, {
      stdio: ['pipe', 'pipe', 'pipe'],
      signal: spawnOptions.signal,  // AbortController 연결
      windowsHide: true
    });
    registerProcess(child.pid, sessionDbId, child);
    child.on('exit', () => unregisterProcess(child.pid));
    return SDK-compatible interface;
  };
```

Windows에서는 `.cmd` 파일을 위해 `cmd.exe` 래퍼를 사용한다. 환경변수는 `sanitizeEnv()`로 정제된다.

### 8.8 좀비 프로세스 수거

**3단계 수거 전략:**

1. **Registry-based orphan 수거** (`reapOrphanedProcesses`):
   - 활성 세션에 속하지 않는 등록된 프로세스를 SIGKILL

2. **System-level orphan 수거** (`killSystemOrphans`):
   - `ppid=1`인 claude 프로세스 탐색 (부모가 죽은 고아 프로세스)
   - `ps -eo pid,ppid,args | grep claude.*haiku|claude.*output-format`

3. **Daemon children 수거** (`killIdleDaemonChildren`):
   - worker-service daemon의 자식 중 idle 상태인 claude 프로세스
   - CPU 0%, 1분 이상 경과한 프로세스 대상
   - `ps -eo pid,ppid,%cpu,etime,comm | grep "claude$"`

### 8.9 Orphan Reaper

```typescript
startOrphanReaper(getActiveSessionIds, intervalMs = 30_000):
  -> setInterval로 30초마다 reapOrphanedProcesses 실행
  -> 정리 함수 반환 (clearInterval)
```

---

## 9. HTTP 라우트 -- 세션 (`SessionRoutes.ts`, 780L)

### 9.1 개요

`SessionRoutes`는 세션 라이프사이클의 전체를 관리하는 가장 큰 라우트 핸들러이다. 세션 초기화, observation 큐잉, 요약 요청, 세션 완료를 처리한다.

### 9.2 엔드포인트 목록

**Legacy 엔드포인트 (sessionDbId 기반):**

| 메서드 | 경로 | 핸들러 | 설명 |
|--------|------|--------|------|
| POST | `/sessions/:sessionDbId/init` | `handleSessionInit` | 세션 초기화 + SDK agent 시작 |
| POST | `/sessions/:sessionDbId/observations` | `handleObservations` | Observation 큐잉 |
| POST | `/sessions/:sessionDbId/summarize` | `handleSummarize` | 요약 요청 큐잉 |
| GET | `/sessions/:sessionDbId/status` | `handleSessionStatus` | 세션 상태 조회 |
| DELETE | `/sessions/:sessionDbId` | `handleSessionDelete` | 세션 삭제 |
| POST | `/sessions/:sessionDbId/complete` | `handleSessionComplete` | 세션 완료 (cleanup-hook 호환) |

**신규 엔드포인트 (contentSessionId 기반):**

| 메서드 | 경로 | 핸들러 | 설명 |
|--------|------|--------|------|
| POST | `/api/sessions/init` | `handleSessionInitByClaudeId` | DB 작업 + 프라이버시 검사 |
| POST | `/api/sessions/observations` | `handleObservationsByClaudeId` | 도구 필터링 + observation 큐잉 |
| POST | `/api/sessions/summarize` | `handleSummarizeByClaudeId` | 프라이버시 검사 + 요약 큐잉 |
| POST | `/api/sessions/complete` | `handleCompleteByClaudeId` | 활성 세션 맵에서 제거 (#842) |

### 9.3 프로바이더 선택 (`getActiveAgent`)

```
isOpenRouterSelected() && isOpenRouterAvailable() -> OpenRouterAgent
isGeminiSelected() && isGeminiAvailable()         -> GeminiAgent
otherwise                                         -> SDKAgent (Claude)
```

프로바이더가 선택되었지만 API 키가 없으면 에러를 던진다 (silent fallback 없음).

### 9.4 Generator 관리 (`ensureGeneratorRunning`)

SDK agent generator의 라이프사이클을 관리하는 핵심 메서드:

```
ensureGeneratorRunning(sessionDbId, source)
  -> 세션 존재 확인
  -> spawnInProgress 중복 방지
  -> generator가 없으면 시작
  -> generator가 있으면:
     -> stale 검사: 30초간 활동 없으면 abort 후 재시작 (#1099)
     -> 프로바이더 변경 검사: 자연스러운 종료 후 전환
```

**Stale generator 감지 (Issue #1099):**
```typescript
static readonly STALE_GENERATOR_THRESHOLD_MS = 30_000;
```
`lastGeneratorActivity`가 30초 이상 갱신되지 않으면 generator가 stalled된 것으로 판단하고 abort -> 재시작한다.

### 9.5 Generator 실행 및 복구

`startGeneratorWithProvider`는 agent의 `startSession`을 호출하고 `.finally()`에서 복잡한 복구 로직을 수행한다:

1. 서브프로세스 종료 확인 (`ensureProcessExit`)
2. abort 여부에 따른 분기:
   - abort: 정상 종료
   - 비정상 종료: pending work 확인 -> crash recovery
3. **Crash recovery:**
   - `MAX_CONSECUTIVE_RESTARTS = 3` 제한
   - 초과 시 "CRITICAL: 비용 폭주 방지"로 중단
   - Exponential backoff: 1s, 2s, 4s
   - `crashRecoveryScheduled` Set으로 중복 방지
4. Pending work 없으면: abort -> `consecutiveRestarts` 리셋

### 9.6 `/api/sessions/init` 상세

```
handleSessionInitByClaudeId(req, res)
  1. contentSessionId 필수, project/prompt는 선택적 (Cursor 호환)
  2. store.createSDKSession() -- 멱등적 INSERT OR IGNORE
  3. getPromptNumberFromUserPrompts()로 프롬프트 번호 결정
  4. stripMemoryTagsFromPrompt()로 프라이버시 태그 제거
  5. 전체 프롬프트가 private이면 skipped: true 반환
  6. store.saveUserPrompt()로 정제된 프롬프트 저장
  7. contextInjected 확인: 이미 활성 세션이면 true
  8. 응답: { sessionDbId, promptNumber, skipped, contextInjected }
```

### 9.7 `/api/sessions/observations` 상세

```
handleObservationsByClaudeId(req, res)
  1. CLAUDE_MEM_SKIP_TOOLS 설정에서 제외 도구 목록 로드
  2. 제외 도구면 skipped 반환
  3. session-memory 파일에 대한 메타 관측 건너뛰기
  4. createSDKSession()으로 세션 획득/생성
  5. PrivacyCheckValidator로 프라이버시 검사
  6. stripMemoryTagsFromJson()으로 tool_input/tool_response 정제
  7. queueObservation() + ensureGeneratorRunning()
  8. 에러 시에도 200 반환 (훅 중단 방지)
```

---

## 10. HTTP 라우트 -- 검색 (`SearchRoutes.ts`, 370L)

### 10.1 엔드포인트 목록

**통합 엔드포인트 (신규 API):**

| 메서드 | 경로 | 설명 |
|--------|------|------|
| GET | `/api/search` | 통합 검색 (observations + sessions + prompts) |
| GET | `/api/timeline` | 통합 타임라인 (앵커 또는 쿼리 기반) |
| GET | `/api/decisions` | decision observation 시맨틱 단축키 |
| GET | `/api/changes` | change observation 시맨틱 단축키 |
| GET | `/api/how-it-works` | "how it works" 설명 시맨틱 단축키 |

**하위 호환 엔드포인트:**

| 메서드 | 경로 | 설명 |
|--------|------|------|
| GET | `/api/search/observations` | observation 전문 검색 |
| GET | `/api/search/sessions` | 세션 요약 전문 검색 |
| GET | `/api/search/prompts` | 사용자 프롬프트 전문 검색 |
| GET | `/api/search/by-concept` | 개념 태그 기반 검색 |
| GET | `/api/search/by-file` | 파일 경로 기반 검색 |
| GET | `/api/search/by-type` | observation 타입 기반 검색 |

**컨텍스트 엔드포인트:**

| 메서드 | 경로 | 설명 |
|--------|------|------|
| GET | `/api/context/recent` | 최근 세션 컨텍스트 |
| GET | `/api/context/timeline` | 앵커 주변 타임라인 |
| GET | `/api/context/preview` | 설정 모달용 컨텍스트 미리보기 |
| GET | `/api/context/inject` | 훅용 컨텍스트 주입 |

**타임라인 및 도움말:**

| 메서드 | 경로 | 설명 |
|--------|------|------|
| GET | `/api/timeline/by-query` | 쿼리 기반 타임라인 |
| GET | `/api/search/help` | 검색 API 문서 |

### 10.2 주요 파라미터

**`/api/search`:** `query`, `type` (observations/sessions/prompts), `limit` (기본 20)

**`/api/timeline`:** `anchor` (observation ID 또는 `S{sessionId}`), `query`

**`/api/context/inject`:** `projects` (콤마 구분 프로젝트 목록) 또는 `project` (레거시), `colors` (ANSI 색상 여부). worktree에서는 `projects=main,worktree-branch` 형태로 통합 타임라인을 생성한다.

### 10.3 SearchManager 위임

모든 엔드포인트는 `SearchManager`의 메서드를 직접 호출한다. `SearchRoutes`는 순수한 HTTP 계층으로, 비즈니스 로직은 `SearchManager`에 완전히 위임된다.

### 10.4 `/api/search/help`

JSON 형태의 API 문서를 반환한다. 각 엔드포인트의 경로, 메서드, 설명, 파라미터를 포함하며, curl 예시도 제공한다.

---

## 11. HTTP 라우트 -- 데이터 (`DataRoutes.ts`, 476L)

### 11.1 엔드포인트 목록

**페이지네이션 엔드포인트:**

| 메서드 | 경로 | 설명 |
|--------|------|------|
| GET | `/api/observations` | 페이지네이션된 observations |
| GET | `/api/summaries` | 페이지네이션된 summaries |
| GET | `/api/prompts` | 페이지네이션된 prompts |

**ID 기반 조회:**

| 메서드 | 경로 | 설명 |
|--------|------|------|
| GET | `/api/observation/:id` | 단일 observation |
| POST | `/api/observations/batch` | 다수 observation (IDs 배열) |
| GET | `/api/session/:id` | 단일 session |
| POST | `/api/sdk-sessions/batch` | 다수 SDK sessions |
| GET | `/api/prompt/:id` | 단일 prompt |

**메타데이터:**

| 메서드 | 경로 | 설명 |
|--------|------|------|
| GET | `/api/stats` | DB 통계 + worker 메타데이터 |
| GET | `/api/projects` | 프로젝트 목록 |

**처리 상태:**

| 메서드 | 경로 | 설명 |
|--------|------|------|
| GET | `/api/processing-status` | 현재 처리 상태 |
| POST | `/api/processing` | 처리 상태 브로드캐스트 |

**대기열 관리:**

| 메서드 | 경로 | 설명 |
|--------|------|------|
| GET | `/api/pending-queue` | 대기열 내용 조회 |
| POST | `/api/pending-queue/process` | 대기열 처리 시작 |
| DELETE | `/api/pending-queue/failed` | 실패 메시지 삭제 |
| DELETE | `/api/pending-queue/all` | 전체 대기열 삭제 |

**가져오기:**

| 메서드 | 경로 | 설명 |
|--------|------|------|
| POST | `/api/import` | 메모리 가져오기 |

### 11.2 페이지네이션 파라미터

```typescript
parsePaginationParams(req): { offset, limit, project? }
  offset: parseInt(query.offset) || 0
  limit: Math.min(parseInt(query.limit) || 20, 100)  // 최대 100
  project: query.project (선택적)
```

### 11.3 `/api/observations/batch` 상세

```
Body: { ids: number[], orderBy?: 'date_desc' | 'date_asc', limit?: number, project?: string }
```

MCP 클라이언트가 문자열 인코딩된 배열을 보낼 수 있으므로(`"[1,2,3]"` 또는 `"1,2,3"`), JSON.parse 후 실패하면 `split(',').map(Number)`로 fallback한다.

### 11.4 `/api/stats` 상세

worker 메타데이터와 DB 통계를 결합하여 반환한다:

```json
{
  "worker": {
    "version": "...",
    "uptime": 3600,
    "activeSessions": 2,
    "sseClients": 1,
    "port": 37777
  },
  "database": {
    "path": "~/.claude-mem/claude-mem.db",
    "size": 1048576,
    "observations": 1500,
    "sessions": 200,
    "summaries": 150
  }
}
```

### 11.5 `/api/pending-queue` 상세

대기열의 전체 상태를 반환한다:
- `queue.messages`: 모든 대기/처리중/실패 메시지
- `queue.totalPending/Processing/Failed`: 상태별 개수
- `queue.stuckCount`: 5분 이상 처리 중인 메시지 수
- `recentlyProcessed`: 최근 30분 내 처리된 메시지 (최대 20개)
- `sessionsWithPendingWork`: 대기 메시지가 있는 세션 목록

### 11.6 `/api/import` 상세

```json
Body: {
  "sessions": [...],     // 세션 먼저 (의존성)
  "summaries": [...],    // 세션에 의존
  "observations": [...], // 세션에 의존
  "prompts": [...]       // 세션에 의존
}
```

각 항목은 `store.importXxx()` 메서드로 멱등적으로 가져온다. 이미 존재하면 건너뛰고 `imported/skipped` 카운트를 반환한다.

---

## 12. HTTP 라우트 -- 설정 (`SettingsRoutes.ts`, 415L)

### 12.1 엔드포인트 목록

| 메서드 | 경로 | 설명 |
|--------|------|------|
| GET | `/api/settings` | 현재 설정 조회 |
| POST | `/api/settings` | 설정 업데이트 |
| GET | `/api/mcp/status` | MCP 서버 활성화 상태 |
| POST | `/api/mcp/toggle` | MCP 서버 토글 |
| GET | `/api/branch/status` | 현재 브랜치 정보 |
| POST | `/api/branch/switch` | 브랜치 전환 |
| POST | `/api/branch/update` | 현재 브랜치 업데이트 |

### 12.2 설정 키 목록

`handleUpdateSettings`가 처리하는 설정 키들:

**AI 프로바이더:**
- `CLAUDE_MEM_MODEL` -- Claude 모델 선택
- `CLAUDE_MEM_PROVIDER` -- claude / gemini / openrouter
- `CLAUDE_MEM_GEMINI_API_KEY`, `CLAUDE_MEM_GEMINI_MODEL`
- `CLAUDE_MEM_GEMINI_RATE_LIMITING_ENABLED`
- `CLAUDE_MEM_OPENROUTER_API_KEY`, `CLAUDE_MEM_OPENROUTER_MODEL`
- `CLAUDE_MEM_OPENROUTER_SITE_URL`, `CLAUDE_MEM_OPENROUTER_APP_NAME`
- `CLAUDE_MEM_OPENROUTER_MAX_CONTEXT_MESSAGES`, `CLAUDE_MEM_OPENROUTER_MAX_TOKENS`

**시스템:**
- `CLAUDE_MEM_CONTEXT_OBSERVATIONS` -- 컨텍스트에 포함할 observation 수
- `CLAUDE_MEM_WORKER_PORT`, `CLAUDE_MEM_WORKER_HOST`
- `CLAUDE_MEM_DATA_DIR`, `CLAUDE_MEM_LOG_LEVEL`
- `CLAUDE_MEM_PYTHON_VERSION`, `CLAUDE_CODE_PATH`

**토큰 경제:**
- `CLAUDE_MEM_CONTEXT_SHOW_READ_TOKENS` -- 읽기 토큰 표시
- `CLAUDE_MEM_CONTEXT_SHOW_WORK_TOKENS` -- 작업 토큰 표시
- `CLAUDE_MEM_CONTEXT_SHOW_SAVINGS_AMOUNT/PERCENT` -- 절약량 표시

**관측 필터링:**
- `CLAUDE_MEM_CONTEXT_OBSERVATION_TYPES` -- 포함할 관측 타입
- `CLAUDE_MEM_CONTEXT_OBSERVATION_CONCEPTS` -- 포함할 관측 개념

**표시 설정:**
- `CLAUDE_MEM_CONTEXT_FULL_COUNT` -- 전체 텍스트 표시할 관측 수
- `CLAUDE_MEM_CONTEXT_FULL_FIELD` -- narrative 또는 facts
- `CLAUDE_MEM_CONTEXT_SESSION_COUNT` -- 표시할 세션 수

**기능 토글:**
- `CLAUDE_MEM_CONTEXT_SHOW_LAST_SUMMARY` -- 마지막 요약 표시
- `CLAUDE_MEM_CONTEXT_SHOW_LAST_MESSAGE` -- 마지막 메시지 표시
- `CLAUDE_MEM_FOLDER_CLAUDEMD_ENABLED` -- CLAUDE.md 폴더 기능

### 12.3 설정 유효성 검사

`validateSettings`는 단일 진실 소스(single source of truth)로서 모든 설정을 검증한다:

| 설정 | 검증 규칙 |
|------|----------|
| `CLAUDE_MEM_PROVIDER` | claude / gemini / openrouter 중 하나 |
| `CLAUDE_MEM_GEMINI_MODEL` | gemini-2.5-flash-lite, gemini-2.5-flash, gemini-3-flash-preview |
| `CLAUDE_MEM_CONTEXT_OBSERVATIONS` | 1-200 정수 |
| `CLAUDE_MEM_WORKER_PORT` | 1024-65535 정수 |
| `CLAUDE_MEM_WORKER_HOST` | 유효한 IP 주소 패턴 |
| `CLAUDE_MEM_LOG_LEVEL` | DEBUG, INFO, WARN, ERROR, SILENT |
| `CLAUDE_MEM_PYTHON_VERSION` | 3.X 또는 3.XX 형식 |
| boolean 설정들 | "true" 또는 "false" 문자열 |
| `CLAUDE_MEM_CONTEXT_FULL_COUNT` | 0-20 정수 |
| `CLAUDE_MEM_CONTEXT_SESSION_COUNT` | 1-50 정수 |
| `CLAUDE_MEM_CONTEXT_FULL_FIELD` | narrative 또는 facts |
| `CLAUDE_MEM_OPENROUTER_MAX_CONTEXT_MESSAGES` | 1-100 |
| `CLAUDE_MEM_OPENROUTER_MAX_TOKENS` | 1000-1000000 |
| `CLAUDE_MEM_OPENROUTER_SITE_URL` | 유효한 URL |

observation types/concepts는 모드별로 자체 타입을 정의하므로 검증을 건너뛴다.

### 12.4 MCP 토글 메커니즘

`.mcp.json` 파일의 이름을 변경하여 MCP 서버를 활성화/비활성화한다:
- 활성화: `.mcp.json.disabled` -> `.mcp.json`
- 비활성화: `.mcp.json` -> `.mcp.json.disabled`

### 12.5 브랜치 전환

허용된 브랜치: `main`, `beta/7.0`, `feature/bun-executable`. 전환 성공 후 1초 후에 `process.exit(0)`을 호출하여 worker를 재시작한다 (PM2가 재시작 처리).

### 12.6 설정 파일 보장

`ensureSettingsFile`은 `~/.claude-mem/settings.json`이 없으면 `SettingsDefaultsManager.getAllDefaults()`의 기본값으로 생성한다.

---

## 13. HTTP 라우트 -- 로그 (`LogsRoutes.ts`, 165L)

### 13.1 엔드포인트

| 메서드 | 경로 | 설명 |
|--------|------|------|
| GET | `/api/logs` | 오늘의 로그 파일 조회 |
| POST | `/api/logs/clear` | 오늘의 로그 파일 초기화 |

### 13.2 로그 파일 경로

```typescript
getLogFilePath(): string {
  const dataDir = SettingsDefaultsManager.get('CLAUDE_MEM_DATA_DIR');
  const date = new Date().toISOString().split('T')[0];
  return join(dataDir, 'logs', `claude-mem-${date}.log`);
}
```

날짜별 로그 파일 (`claude-mem-YYYY-MM-DD.log`)을 사용한다.

### 13.3 `readLastLines` -- 효율적인 역방향 읽기

전체 파일을 메모리에 로드하지 않고 마지막 N줄만 읽는다:

```
readLastLines(filePath, lineCount)
  1. 초기 청크 크기: 64KB
  2. 파일 끝에서 청크 읽기
  3. 줄바꿈 수 확인
  4. 부족하면 청크 크기를 2배로 확대 (최대 10MB)
  5. 충분한 줄이 수집되면 마지막 N줄 반환
  6. totalEstimate 계산:
     - 전체 파일을 읽었으면 정확한 줄 수
     - 부분 읽기면 평균 줄 길이로 추정
```

### 13.4 `/api/logs` 파라미터

- `lines`: 반환할 줄 수 (기본 1000, 최대 10000)

응답:
```json
{
  "logs": "...",          // 로그 텍스트
  "path": "...",          // 로그 파일 경로
  "exists": true,         // 파일 존재 여부
  "totalLines": 50000,    // 추정 총 줄 수
  "returnedLines": 1000   // 반환된 줄 수
}
```

---

## 14. 미들웨어 (`middleware.ts`, 135L)

### 14.1 `createMiddleware`

Express 미들웨어 스택을 구성하는 팩토리 함수:

**1. JSON 파싱:**
```typescript
express.json({ limit: '50mb' })
```
50MB 제한 -- 대용량 tool_response를 수용한다.

**2. CORS:**
```typescript
cors({
  origin: (origin, callback) => {
    if (!origin ||
        origin.startsWith('http://localhost:') ||
        origin.startsWith('http://127.0.0.1:')) {
      callback(null, true);
    } else {
      callback(new Error('CORS not allowed'));
    }
  },
  methods: ['GET', 'HEAD', 'POST', 'PUT', 'PATCH', 'DELETE'],
  allowedHeaders: ['Content-Type', 'Authorization', 'X-Requested-With'],
  credentials: false
})
```

Origin 헤더가 없는 요청(훅, curl, CLI 도구)과 localhost 요청만 허용한다.

**3. 요청/응답 로깅:**

다음은 로깅에서 제외된다:
- 정적 자산 (`.html`, `.js`, `.css`, `.svg`, `.png` 등)
- 헬스 체크 (`/health`)
- 루트 경로 (`/`)
- 폴링 엔드포인트 (`/api/logs` -- 자동 새로고침 노이즈 방지)

`res.send`를 감싸서 응답 시 duration을 포함한 로그를 남긴다.

**4. 정적 파일 서빙:**
```typescript
express.static(path.join(packageRoot, 'plugin', 'ui'))
```
웹 UI 파일 (viewer-bundle.js, 로고, 폰트 등)을 서빙한다.

### 14.2 `requireLocalhost`

admin 엔드포인트를 localhost에서만 접근할 수 있도록 제한하는 가드 미들웨어:

```typescript
const isLocalhost =
  clientIp === '127.0.0.1' ||
  clientIp === '::1' ||
  clientIp === '::ffff:127.0.0.1' ||
  clientIp === 'localhost';
```

0.0.0.0에 바인딩된 경우에도 admin 기능이 외부에서 접근되지 않도록 한다. 실패 시 403 Forbidden을 반환하고 보안 로그를 남긴다.

### 14.3 `summarizeRequestBody`

요청 본문을 로깅용으로 요약하여 민감한 데이터나 대용량 페이로드가 로그에 노출되지 않도록 한다:

- `/init` 경로: 빈 문자열 (프롬프트 내용 비노출)
- `/observations` 경로: `tool={toolSummary}` 형태로 도구 정보만 표시
- `/summarize` 경로: `'requesting summary'`
- 기타: 빈 문자열

---

## 부록: BaseRouteHandler

모든 라우트 핸들러의 기반 클래스:

```typescript
abstract class BaseRouteHandler {
  protected wrapHandler(handler): (req, res) => void
    // try-catch 래핑, async 에러 처리
  protected parseIntParam(req, res, paramName): number | null
    // 정수 파라미터 검증, 실패 시 400
  protected validateRequired(req, res, params): boolean
    // 필수 body 파라미터 확인, 실패 시 400
  protected badRequest(res, message): void   // 400
  protected notFound(res, message): void     // 404
  protected handleError(res, error): void    // 500 + 로깅
}
```

`wrapHandler`는 동기/비동기 핸들러를 모두 처리하며, `res.headersSent`를 확인하여 중복 응답을 방지한다.
