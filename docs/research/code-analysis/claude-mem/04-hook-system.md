# claude-mem -- 훅 시스템 분석

## 1. 훅 시스템 개요

claude-mem의 훅 시스템은 Claude Code 및 Cursor IDE의 이벤트 라이프사이클에 연결되어 세션 초기화, observation 수집, 요약 생성, 컨텍스트 주입 등을 자동으로 수행하는 핵심 아키텍처이다. 두 개의 독립적인 `hooks.json` 파일이 각 플랫폼의 훅 인터페이스를 정의한다.

### 1.1 Claude Code hooks.json 구조 (`plugin/hooks/hooks.json`)

Claude Code 플러그인용 `hooks.json`은 6개의 훅 이벤트를 정의한다:

```json
{
  "description": "Claude-mem memory system hooks",
  "hooks": {
    "Setup": [...],
    "SessionStart": [...],
    "UserPromptSubmit": [...],
    "PostToolUse": [...],
    "Stop": [...],
    "SessionEnd": [...]
  }
}
```

각 이벤트는 `matcher`와 `hooks` 배열을 포함하는 객체의 배열이다. 개별 훅은 `type: "command"` 형식으로, 셸 명령어와 `timeout` (초 단위)을 지정한다. 모든 명령어는 `CLAUDE_PLUGIN_ROOT` 환경변수를 기반으로 경로를 결정하며, fallback으로 `$HOME/.claude/plugins/marketplaces/thedotmack/plugin`을 사용한다.

**이벤트별 훅 구성:**

| 이벤트 | matcher | 훅 수 | 주요 동작 |
|--------|---------|-------|----------|
| `Setup` | `*` | 1 | `setup.sh` 실행 (초기 환경 설정) |
| `SessionStart` | `startup\|clear\|compact` | 3 | smart-install, worker 시작, context 훅 |
| `UserPromptSubmit` | (없음) | 1 | session-init 훅 |
| `PostToolUse` | `*` | 1 | observation 훅 (timeout: 120s) |
| `Stop` | (없음) | 1 | summarize 훅 (timeout: 120s) |
| `SessionEnd` | (없음) | 1 | session-complete 훅 (timeout: 30s) |

`SessionStart` 이벤트는 3단계 파이프라인을 구성한다: (1) `smart-install.js`로 의존성 확인 및 설치, (2) `worker-service.cjs start`로 worker daemon 시작, (3) `hook claude-code context`로 컨텍스트 주입. 이 모든 명령어는 `bun-runner.js`를 통해 Bun 런타임으로 실행된다.

### 1.2 Cursor hooks.json 구조 (`cursor-hooks/hooks.json`)

Cursor IDE용 `hooks.json`은 더 단순한 구조를 가진다:

```json
{
  "version": 1,
  "hooks": {
    "beforeSubmitPrompt": [...],
    "afterMCPExecution": [...],
    "afterShellExecution": [...],
    "afterFileEdit": [...],
    "stop": [...]
  }
}
```

Cursor 훅은 셸 스크립트(`.sh` 파일)를 직접 호출하며, 상대경로(`./cursor-hooks/`)를 사용한다. `beforeSubmitPrompt`은 `session-init.sh`와 `context-inject.sh` 두 개의 훅을 순서대로 실행한다. `afterMCPExecution`과 `afterShellExecution`은 `save-observation.sh`를 호출하고, `afterFileEdit`는 `save-file-edit.sh`를 호출한다.

### 1.3 실행 모델

Claude Code와 Cursor 모두 훅 실행 시 stdin을 통해 JSON 입력을 제공하고, stdout으로 JSON 응답을 기대한다. 핵심 차이점:

- **Claude Code**: `bun-runner.js`를 통해 `worker-service.cjs hook claude-code <event>` 명령을 실행. 종료 코드 의미가 엄격함 (0=성공, 2=blocking error).
- **Cursor**: 셸 스크립트를 직접 실행하거나 `bun worker-service.cjs hook cursor <command>` 통합 CLI를 사용. 더 단순한 응답 형식 (`{ continue: true }`).

---

## 2. 훅 실행 엔진 (`hook-command.ts`, 112L)

`hook-command.ts`는 모든 훅 실행의 중앙 진입점이다. `hookCommand(platform, event, options)` 함수가 전체 훅 라이프사이클을 관리한다.

### 2.1 실행 흐름

```
hookCommand(platform, event)
  -> stderr 억제 (process.stderr.write를 no-op으로 교체)
  -> getPlatformAdapter(platform)   // 플랫폼별 어댑터 선택
  -> getEventHandler(event)         // 이벤트 핸들러 선택
  -> readJsonFromStdin()            // stdin에서 JSON 입력 읽기
  -> adapter.normalizeInput(raw)    // 플랫폼별 입력 정규화
  -> handler.execute(input)         // 이벤트 핸들러 실행
  -> adapter.formatOutput(result)   // 플랫폼별 출력 포맷팅
  -> console.log(JSON.stringify())  // stdout으로 결과 출력
  -> process.exit(exitCode)         // 종료 코드로 프로세스 종료
```

### 2.2 stderr 억제 메커니즘

Claude Code는 exit code에 따라 stderr을 다르게 처리한다. exit 1이면 stderr을 사용자에게 표시하고, exit 2면 stderr을 Claude에게 피드백한다. 이 동작이 훅에서 의도치 않은 에러 UI를 발생시키는 문제(#1181)를 방지하기 위해, `hookCommand`는 실행 시작 시 `process.stderr.write`를 `() => true`로 교체하여 모든 stderr 출력을 억제한다. 진단 정보는 logger를 통해 파일에 기록된다. finally 블록에서 원래 stderr.write를 복원하여, `skipExit: true` 옵션 사용 시 (worker 프로세스로 계속 실행) stderr이 정상 동작하도록 한다.

### 2.3 에러 분류 (`isWorkerUnavailableError`)

`isWorkerUnavailableError` 함수는 에러를 두 가지 카테고리로 분류한다:

**graceful degradation (exit 0):**
- Transport 실패: `ECONNREFUSED`, `ECONNRESET`, `EPIPE`, `ETIMEDOUT`, `fetch failed`, `socket hang up` 등
- Timeout 에러: `timed out`, `timeout` 포함
- HTTP 5xx 서버 에러
- HTTP 429 rate limit (일시적 불가용으로 처리)

**blocking error (exit 2):**
- HTTP 4xx 클라이언트 에러 (코드 버그)
- 프로그래밍 에러 (`TypeError`, `ReferenceError`, `SyntaxError`)
- 기타 알 수 없는 에러 (보수적으로 버그로 간주)

이 분류는 worker가 사용 불가능할 때 사용자 경험을 차단하지 않으면서도, 실제 코드 버그는 개발자에게 노출되도록 설계되었다.

### 2.4 HookCommandOptions

```typescript
export interface HookCommandOptions {
  skipExit?: boolean;  // true이면 process.exit() 호출 안 함
}
```

`skipExit`은 worker-service가 훅 핸들링 후에도 계속 실행되어야 할 때 사용된다 (예: 훅 처리 후 HTTP 서버로 전환).

---

## 3. stdin 리더 (`stdin-reader.ts`, 178L)

### 3.1 핵심 문제

Claude Code는 훅 입력을 stdin에 쓴 후 stdin을 닫지 않는다 (#727). 따라서 `stdin.on('end')`가 절대 발생하지 않으며, 일반적인 stdin 읽기 방식으로는 훅이 무한 대기 상태에 빠진다.

### 3.2 해결 전략: JSON 자기 구분(self-delimiting) 특성 활용

JSON은 자기 구분 형식이므로, 완전한 JSON 문서를 수신하면 EOF를 기다리지 않고 즉시 파싱할 수 있다. `readJsonFromStdin`은 매 청크 수신 후 `JSON.parse`를 시도하여 완전한 JSON이 수신되면 즉시 resolve한다.

### 3.3 stdin 가용성 확인 (`isStdinAvailable`)

Bun 런타임에서 `process.stdin` 접근 시 `EINVAL` 에러로 크래시할 수 있다 (#646). `isStdinAvailable`은 다음을 안전하게 확인한다:

1. `process.stdin.isTTY`가 `true`면 대화형 모드이므로 `false` 반환
2. `stdin.readable` 접근으로 Bun의 lazy initialization 트리거 -- 예외 발생 시 stdin 불가용

### 3.4 파싱 전략

```
readJsonFromStdin()
  -> isStdinAvailable() 확인
  -> Promise 생성:
     -> data 이벤트마다:
        -> 입력 축적 (input += chunk)
        -> 즉시 JSON.parse 시도 (tryParseJson)
        -> 성공 시 즉시 resolve
        -> 실패 시 50ms 지연 후 재시도 (PARSE_DELAY_MS)
     -> end 이벤트: 최종 파싱 시도
     -> error 이벤트: undefined로 resolve (graceful)
     -> 30초 safety timeout (SAFETY_TIMEOUT_MS):
        -> 데이터 있으면 에러로 reject
        -> 데이터 없으면 undefined로 resolve
```

### 3.5 타임아웃 상수

| 상수 | 값 | 용도 |
|------|-----|------|
| `SAFETY_TIMEOUT_MS` | 30,000ms | 불완전한 JSON에 대한 최종 안전망 |
| `PARSE_DELAY_MS` | 50ms | 다중 청크 도착 시 파싱 지연 |

`resolveWith`와 `rejectWith` 헬퍼는 중복 resolve/reject를 방지하기 위해 `resolved` 플래그를 사용하며, 모든 경로에서 타이머와 리스너를 정리한다.

---

## 4. 세션 초기화 훅 (`session-init.ts`, 128L)

### 4.1 역할

`sessionInitHandler`는 `UserPromptSubmit` 이벤트에서 실행되며, 사용자가 프롬프트를 제출할 때마다 세션 초기화 및 SDK agent 시작을 담당한다.

### 4.2 실행 흐름

```
execute(input)
  1. ensureWorkerRunning() -- worker 프로세스 가동 확인
  2. sessionId 유효성 검사 (없으면 skip, Codex CLI 호환 #744)
  3. 프로젝트 제외 목록 확인 (isProjectExcluded)
  4. 빈 프롬프트 처리 ('[media prompt]'로 대체)
  5. POST /api/sessions/init 호출:
     - contentSessionId, project, prompt 전송
     - sessionDbId, promptNumber, skipped, contextInjected 수신
  6. privacy 확인: skipped && reason==='private' 이면 종료
  7. contextInjected 확인: 이미 주입됨이면 SDK agent 재초기화 건너뜀 (#1079)
  8. 플랫폼 확인: Cursor가 아닌 경우에만 SDK agent 초기화
  9. POST /sessions/{sessionDbId}/init 호출:
     - userPrompt (슬래시 명령 접두사 제거), promptNumber 전송
```

### 4.3 플랫폼별 분기

`input.platform`이 `'cursor'`이면 SDK agent 초기화를 건너뛴다. Cursor는 SDK agent를 사용하지 않으며, 세션/observation 저장만 수행한다. Claude Code에서만 SDK agent가 시작되어 메모리 처리를 수행한다.

### 4.4 에러 처리 원칙

모든 HTTP 실패는 로깅하되 예외를 던지지 않는다. 반환값은 항상 `{ continue: true, suppressOutput: true }`로, worker 문제가 사용자의 프롬프트를 차단하지 않도록 설계되었다. `exitCode`는 명시적으로 `HOOK_EXIT_CODES.SUCCESS` (0)을 사용하여 Claude Code가 정상적으로 진행하도록 한다.

### 4.5 프롬프트 정제

슬래시 명령(`/review 101`)은 메모리 agent에 전달 시 슬래시를 제거하여 더 의미적인 형태(`review 101`)로 변환한다.

---

## 5. 훅 상수 (`hook-constants.ts`)

### 5.1 HOOK_TIMEOUTS

```typescript
export const HOOK_TIMEOUTS = {
  DEFAULT: 300000,              // 5분 (느린 시스템을 위한 표준 HTTP 타임아웃)
  HEALTH_CHECK: 3000,           // 3초 (정상 worker는 <100ms에 응답)
  POST_SPAWN_WAIT: 5000,        // daemon 시작 후 대기
  READINESS_WAIT: 30000,        // DB + search 초기화 후 대기 (보통 <5s)
  PORT_IN_USE_WAIT: 3000,       // 포트 점유 시 대기
  WORKER_STARTUP_WAIT: 1000,    // worker 시작 대기
  PRE_RESTART_SETTLE_DELAY: 2000, // 재시작 전 파일 동기화 대기
  POWERSHELL_COMMAND: 10000,    // PowerShell 프로세스 열거
  WINDOWS_MULTIPLIER: 1.5       // Windows 플랫폼 타임아웃 배수
} as const;
```

### 5.2 HOOK_EXIT_CODES

```typescript
export const HOOK_EXIT_CODES = {
  SUCCESS: 0,          // 성공. SessionStart에서 stdout이 context에 추가됨.
  FAILURE: 1,          // 실패. verbose 모드에서만 stderr 표시.
  BLOCKING_ERROR: 2,   // 차단 에러. SessionStart에서 stderr이 사용자에게 표시.
  USER_MESSAGE_ONLY: 3 // Cursor 전용. 사용자에게만 stderr 표시.
} as const;
```

### 5.3 getTimeout 헬퍼

`getTimeout(baseTimeout)`는 Windows에서 `WINDOWS_MULTIPLIER` (1.5x)를 적용하여 플랫폼별 타임아웃을 조정한다. Windows의 프로세스 생성 및 I/O가 Unix보다 느리기 때문이다.

---

## 6. 트랜스크립트 감시 (`watcher.ts`, 224L)

### 6.1 아키텍처 개요

`TranscriptWatcher`는 JSONL 형식의 트랜스크립트 파일을 실시간으로 감시하여, 외부 AI 도구(Codex CLI 등)의 세션 활동을 claude-mem의 메모리 시스템에 통합한다. 이것은 Claude Code의 훅 시스템과는 별개로, 파일 기반의 이벤트 소스를 처리한다.

### 6.2 FileTailer 클래스

`FileTailer`는 개별 파일을 tail하는 내부 클래스이다:

```
FileTailer(filePath, initialOffset, onLine, onOffset)
  -> start(): fs.watch로 파일 변경 감지
  -> readNewData():
     1. 파일 존재 및 크기 확인
     2. offset < size 인 경우만 읽기
     3. createReadStream(start: offset, end: size-1)으로 새 데이터 읽기
     4. offset 갱신 후 onOffset 콜백 호출
     5. partial line 관리 (마지막 불완전 줄 보존)
     6. 완전한 줄마다 onLine 콜백 호출
```

파일 크기가 offset보다 작아지면(파일이 truncate된 경우) offset을 0으로 리셋한다.

### 6.3 TranscriptWatcher 클래스

```typescript
class TranscriptWatcher {
  private processor = new TranscriptEventProcessor();
  private tailers = new Map<string, FileTailer>();
  private state: TranscriptWatchState;
  private rescanTimers: Array<NodeJS.Timeout> = [];
}
```

**초기화 흐름:**

1. `loadWatchState(statePath)`로 이전 offset 상태 로드
2. 각 `WatchTarget`에 대해 `setupWatch()` 호출
3. `resolveSchema(watch)`로 스키마 해석 (문자열이면 `config.schemas`에서 조회)
4. `resolveWatchFiles(path)`로 대상 파일 목록 결정:
   - glob 패턴이면 `globSync`으로 확장
   - 디렉토리면 `**/*.jsonl` 패턴으로 확장
   - 단일 파일이면 그대로 사용
5. 각 파일에 대해 `addTailer()` 호출
6. `rescanIntervalMs` (기본 5000ms) 간격으로 새 파일 감시

### 6.4 세션 ID 추출

`extractSessionIdFromPath(filePath)`는 파일 경로에서 UUID 패턴(`[0-9a-f]{8}-[0-9a-f]{4}-...`)을 추출한다. Codex CLI 등은 세션별로 별도 JSONL 파일을 생성하므로, 파일 경로에서 세션 ID를 유추할 수 있다.

### 6.5 startAtEnd 옵션

`WatchTarget.startAtEnd`이 `true`이면 파일의 현재 끝에서 감시를 시작한다. 기존 이력을 건너뛰고 새로운 이벤트만 처리하는 데 유용하다.

### 6.6 상태 영속화

`TranscriptWatchState`는 파일별 offset을 JSON 파일에 저장한다. 각 파일의 새 데이터를 읽을 때마다 `saveWatchState`를 호출하여 offset을 갱신한다. worker 재시작 시 이전 offset에서 이어서 읽을 수 있다.

---

## 7. 트랜스크립트 처리 (`processor.ts`, 369L)

### 7.1 TranscriptEventProcessor

`TranscriptEventProcessor`는 JSONL 엔트리를 파싱하여 claude-mem의 이벤트 핸들러로 라우팅하는 핵심 처리 엔진이다. 세션 상태를 메모리에 유지하며 다양한 이벤트 액션을 처리한다.

### 7.2 세션 상태 관리

```typescript
interface SessionState {
  sessionId: string;
  cwd?: string;
  project?: string;
  lastUserMessage?: string;
  lastAssistantMessage?: string;
  pendingTools: Map<string, { name?: string; input?: unknown }>;
}
```

세션은 `watch.name:sessionId` 키로 관리된다. `pendingTools` Map은 tool_use 이벤트와 tool_result 이벤트를 매칭하기 위해 사용된다 -- tool_use 시 tool ID와 이름/입력을 저장하고, tool_result 수신 시 해당 정보를 조회하여 완전한 observation을 생성한다.

### 7.3 이벤트 처리 파이프라인

```
processEntry(entry, watch, schema, sessionIdOverride)
  -> schema.events 순회:
     -> matchesRule(entry, event.match, schema) 확인
     -> handleEvent(entry, watch, schema, event, sessionIdOverride)
        -> resolveSessionId() -- 세션 ID 결정
        -> getOrCreateSession() -- 세션 상태 획득/생성
        -> resolveCwd() / resolveProject() -- 컨텍스트 결정
        -> resolveFields() -- 이벤트별 필드 해석
        -> event.action에 따른 분기:
```

### 7.4 지원하는 액션 (EventAction)

| 액션 | 동작 |
|------|------|
| `session_context` | cwd, project 등 세션 컨텍스트 갱신 |
| `session_init` | `sessionInitHandler.execute()` 호출, AGENTS.md 갱신 |
| `user_message` | `lastUserMessage` 갱신 |
| `assistant_message` | `lastAssistantMessage` 갱신 |
| `tool_use` | pendingTools에 저장, apply_patch이면 file_edit 전송 |
| `tool_result` | pendingTools에서 매칭, observation 전송 |
| `observation` | `observationHandler.execute()` 직접 호출 |
| `file_edit` | `fileEditHandler.execute()` 호출 |
| `session_end` | 요약 큐잉, session-complete, AGENTS.md 갱신, 세션 정리 |

### 7.5 tool_use 특수 처리

`apply_patch` 도구는 특별히 처리된다. 입력 문자열에서 `*** Update File:`, `*** Add File:`, `*** Delete File:`, `*** Move to:`, `+++ ` 패턴을 파싱하여 영향받는 파일 경로를 추출하고, 각 파일에 대해 `sendFileEdit`을 호출한다.

### 7.6 세션 종료 처리

`handleSessionEnd`는 다음 순서를 실행한다:
1. `queueSummary()` -- `POST /api/sessions/summarize` 호출
2. `sessionCompleteHandler.execute()` -- 세션 완료 훅 실행
3. `updateContext()` -- AGENTS.md 갱신 (watch.context 설정 시)
4. `pendingTools.clear()` -- 미완료 도구 정리
5. 세션을 `sessions` Map에서 제거

### 7.7 컨텍스트 갱신 (`updateContext`)

`watch.context.mode`가 `'agents'`이면, worker의 `/api/context/inject` 엔드포인트에서 컨텍스트를 가져와 `AGENTS.md` (또는 지정된 경로) 파일에 기록한다. 이를 통해 Codex CLI 등의 외부 도구가 claude-mem의 메모리를 활용할 수 있다.

---

## 8. 트랜스크립트 설정 (`config.ts`, 137L)

### 8.1 기본 경로

```typescript
export const DEFAULT_CONFIG_PATH = join(homedir(), '.claude-mem', 'transcript-watch.json');
export const DEFAULT_STATE_PATH = join(homedir(), '.claude-mem', 'transcript-watch-state.json');
```

### 8.2 샘플 스키마: Codex

`CODEX_SAMPLE_SCHEMA`는 Codex CLI의 JSONL 형식에 대한 완전한 스키마를 정의한다:

| 이벤트 | match 조건 | 액션 | 주요 필드 |
|--------|-----------|------|----------|
| `session-meta` | `type === 'session_meta'` | `session_context` | sessionId, cwd |
| `turn-context` | `type === 'turn_context'` | `session_context` | cwd |
| `user-message` | `payload.type === 'user_message'` | `session_init` | prompt |
| `assistant-message` | `payload.type === 'agent_message'` | `assistant_message` | message |
| `tool-use` | `payload.type in [function_call, ...]` | `tool_use` | toolId, toolName (coalesce), toolInput (coalesce) |
| `tool-result` | `payload.type in [function_call_output, ...]` | `tool_result` | toolId, toolResponse |
| `session-end` | `payload.type === 'turn_aborted'` | `session_end` | (없음) |

`tool-use`의 `toolName` 필드는 `coalesce` 전략을 사용하여 `payload.name`을 시도하고, 없으면 `{ value: 'web_search' }` 기본값을 사용한다.

### 8.3 샘플 설정 (`SAMPLE_CONFIG`)

```typescript
{
  version: 1,
  schemas: { codex: CODEX_SAMPLE_SCHEMA },
  watches: [{
    name: 'codex',
    path: '~/.codex/sessions/**/*.jsonl',
    schema: 'codex',
    startAtEnd: true,
    context: {
      mode: 'agents',
      path: '~/.codex/AGENTS.md',
      updateOn: ['session_start', 'session_end']
    }
  }],
  stateFile: DEFAULT_STATE_PATH
}
```

### 8.4 설정 로드 및 검증

`loadTranscriptWatchConfig(path)`는 JSON 파일을 읽고 `version`과 `watches` 필드의 존재를 확인한다. `stateFile`이 없으면 기본값을 적용한다. `expandHomePath`는 `~` 접두사를 `homedir()`로 확장한다.

### 8.5 `writeSampleConfig`

지정된 경로에 샘플 설정 파일을 생성한다. 디렉토리가 없으면 `mkdirSync({ recursive: true })`로 생성한다.

---

## 9. 필드 유틸리티 (`field-utils.ts`, 151L)

### 9.1 FieldSpec 타입 시스템

필드 스펙은 두 가지 형태를 가진다:

```typescript
type FieldSpec =
  | string                           // 단순 경로 (예: 'payload.name')
  | {
      path?: string;                 // JSON 경로
      value?: unknown;               // 리터럴 값
      coalesce?: FieldSpec[];        // 첫 번째 유효한 값 사용
      default?: unknown;             // 기본값
    };
```

### 9.2 경로 파싱 (`parsePath`)

JSON 경로 문자열을 토큰 배열로 변환한다:
- `$.` 접두사 제거
- `.`으로 분할
- `[0]` 같은 배열 인덱스를 숫자 토큰으로 변환
- 예: `payload.items[0].name` -> `['payload', 'items', 0, 'name']`

### 9.3 `getValueByPath`

토큰 배열을 순회하며 중첩 객체에서 값을 추출한다. `null` 또는 `undefined`를 만나면 즉시 `undefined`를 반환한다.

### 9.4 컨텍스트 변수 해석 (`resolveFromContext`)

특수 접두사로 시작하는 경로는 컨텍스트 객체에서 값을 가져온다:

| 접두사 | 소스 |
|--------|------|
| `$watch.` | WatchTarget 객체 |
| `$schema.` | TranscriptSchema 객체 |
| `$session.` | 세션 상태 객체 |
| `$cwd` | `watch.workspace` |
| `$project` | `watch.project` |

### 9.5 `resolveFieldSpec` 해석 순서

1. `undefined`이면 `undefined` 반환
2. 문자열이면: 컨텍스트 변수 확인 -> `getValueByPath`로 엔트리에서 추출
3. `coalesce`이면: 각 후보를 순서대로 시도, 첫 번째 비어있지 않은 값 반환
4. `path`이면: 컨텍스트 변수 -> 엔트리 경로 순으로 시도
5. `value`이면: 리터럴 값 반환
6. `default`이면: 기본값 반환

### 9.6 매칭 규칙 (`matchesRule`)

`MatchRule`은 다섯 가지 조건을 지원한다:

| 조건 | 동작 |
|------|------|
| `exists` | 값이 undefined/null/'' 이 아닌지 확인 |
| `equals` | 정확한 값 일치 |
| `in` | 배열 내 포함 여부 |
| `contains` | 문자열 부분 일치 |
| `regex` | 정규표현식 매칭 |

규칙이 없으면(`undefined`) 항상 `true`를 반환한다. `path`가 지정되지 않으면 `schema.eventTypePath` 또는 기본값 `'type'`을 사용한다.

---

## 10. Cursor 훅 설치 (`CursorHooksInstaller.ts`, 675L)

### 10.1 아키텍처

`CursorHooksInstaller`는 Cursor IDE와 claude-mem을 통합하는 모든 로직을 담당한다. 원래 `worker-service.ts` monolith에서 추출되었으며, 다음을 관리한다:

- Cursor hooks 설치/제거
- MCP 서버 설정
- 컨텍스트 파일 생성
- 프로젝트 레지스트리 관리

### 10.2 플랫폼 감지

```typescript
detectPlatform(): 'windows' | 'unix'
getScriptExtension(): '.ps1' | '.sh'
```

### 10.3 프로젝트 레지스트리

`cursor-projects.json` 파일로 프로젝트 목록을 관리한다:

```typescript
interface CursorProjectRegistry {
  [projectName: string]: {
    workspacePath: string;
    installedAt: string;  // ISO timestamp
  };
}
```

`registerCursorProject`와 `unregisterCursorProject`로 프로젝트를 등록/해제한다. 등록된 프로젝트는 SDK agent가 요약을 저장한 후 자동으로 컨텍스트가 갱신된다.

### 10.4 `updateCursorContextForProject`

SDK agent가 요약을 저장한 후 호출된다:
1. 레지스트리에서 프로젝트 항목 조회
2. `GET /api/context/inject?project={name}`로 컨텍스트 획득
3. `writeContextFile(workspacePath, context)`로 `.cursor/rules/claude-mem-context.mdc` 파일 갱신

### 10.5 경로 탐색

세 가지 경로 탐색 함수가 있다:

- **`findMcpServerPath()`**: marketplace 설치, 소스 위치 순으로 `mcp-server.cjs` 탐색
- **`findWorkerServicePath()`**: marketplace 설치, 소스 위치 순으로 `worker-service.cjs` 탐색
- **`findBunPath()`**: `~/.bun/bin/bun`, `/usr/local/bin/bun` 등 공통 위치 탐색. Windows에서는 `.exe` 확장자를 추가로 확인. 찾지 못하면 `'bun'`을 반환하여 PATH에 의존.

### 10.6 설치 대상 (`getTargetDir`)

```
project   -> {cwd}/.cursor
user      -> ~/.cursor
enterprise -> macOS: /Library/Application Support/Cursor
              Linux: /etc/cursor
              Windows: C:\ProgramData\Cursor
```

### 10.7 MCP 설정 (`configureCursorMcp`)

`{targetDir}/mcp.json`에 claude-mem MCP 서버를 등록한다:

```json
{
  "mcpServers": {
    "claude-mem": {
      "command": "node",
      "args": ["{mcpServerPath}"]
    }
  }
}
```

기존 설정이 있으면 병합하고, 손상된 설정은 새로 생성한다.

### 10.8 훅 설치 (`installCursorHooks`)

통합 CLI 모드로 hooks.json을 생성한다:

```json
{
  "version": 1,
  "hooks": {
    "beforeSubmitPrompt": [
      { "command": "\"{bunPath}\" \"{workerServicePath}\" hook cursor session-init" },
      { "command": "\"{bunPath}\" \"{workerServicePath}\" hook cursor context" }
    ],
    "afterMCPExecution": [
      { "command": "\"{bunPath}\" \"{workerServicePath}\" hook cursor observation" }
    ],
    "afterShellExecution": [
      { "command": "\"{bunPath}\" \"{workerServicePath}\" hook cursor observation" }
    ],
    "afterFileEdit": [
      { "command": "\"{bunPath}\" \"{workerServicePath}\" hook cursor file-edit" }
    ],
    "stop": [
      { "command": "\"{bunPath}\" \"{workerServicePath}\" hook cursor summarize" }
    ]
  }
}
```

Windows에서는 backslash 이스케이프 처리(`\\`)를 수행한다.

project-level 설치 시 추가로:
1. `.cursor/rules/` 디렉토리 생성
2. worker에서 초기 컨텍스트 획득 시도
3. 실패 시 placeholder 컨텍스트 파일 생성
4. 프로젝트 레지스트리에 등록

### 10.9 훅 제거 (`uninstallCursorHooks`)

1. 레거시 셸 스크립트 제거 (bash/PowerShell)
2. `hooks.json` 제거
3. project-level이면: 컨텍스트 파일 제거, 레지스트리에서 해제

### 10.10 상태 확인 (`checkCursorHooksStatus`)

project, user, enterprise 세 위치를 확인하여:
- `hooks.json` 존재 여부
- 통합 CLI 모드인지 레거시 셸 모드인지 확인
- 플랫폼 (bash/PowerShell) 감지
- 컨텍스트 파일 상태 (project만)

### 10.11 handleCursorCommand

CLI 진입점으로 `install`, `uninstall`, `status`, `setup` 서브커맨드를 라우팅한다.

---

## 11. CLI 어댑터 (`cli/adapters/`)

### 11.1 어댑터 아키텍처

어댑터는 `PlatformAdapter` 인터페이스를 구현하여 플랫폼별 stdin/stdout 프로토콜을 정규화한다:

```typescript
interface PlatformAdapter {
  normalizeInput(raw: unknown): NormalizedHookInput;
  formatOutput(result: HookResult): unknown;
}
```

`getPlatformAdapter(platform)`이 팩토리 역할을 하며, `'claude-code'`, `'cursor'`, `'raw'` 중 선택한다. 알 수 없는 플랫폼은 `rawAdapter`를 사용한다.

### 11.2 Claude Code 어댑터 (`claude-code.ts`)

**입력 정규화:**
- `session_id` -> `sessionId` (snake_case에서 camelCase로)
- `tool_name`, `tool_input`, `tool_response` 매핑
- `cwd` 기본값: `process.cwd()`
- `SessionStart` 훅은 stdin이 없으므로 `raw ?? {}` 처리

**출력 포맷팅:**
- `hookSpecificOutput`이 있으면 포함 (컨텍스트 주입용)
- `systemMessage`가 있으면 포함
- 인식되지 않는 필드를 포함하면 Stop 훅에서 "JSON validation failed" 에러 발생하므로, 최소한의 필드만 출력

### 11.3 Cursor 어댑터 (`cursor.ts`)

**입력 정규화 -- 필드 매핑이 Claude Code와 다름:**

| Cursor 필드 | NormalizedHookInput 필드 | 비고 |
|-------------|-------------------------|------|
| `conversation_id` / `generation_id` / `id` | `sessionId` | Cursor 버전에 따라 다름 (#838, #1049) |
| `workspace_roots[0]` / `cwd` | `cwd` | |
| `prompt` / `query` / `input` / `message` | `prompt` | 버전/훅 타입에 따라 다름 |
| `tool_name` | `toolName` | 셸 명령이면 `'Bash'`로 고정 |
| `command` / `output` | `toolInput` / `toolResponse` | 셸 명령 특수 처리 |
| `result_json` | `toolResponse` | Claude Code의 `tool_response`와 다른 이름 |
| `file_path` | `filePath` | afterFileEdit 전용 |
| `edits` | `edits` | afterFileEdit 전용 |

셸 명령 감지: `r.command`가 존재하고 `r.tool_name`이 없으면 셸 명령으로 판단한다.

**출력 포맷팅:**
- 단순히 `{ continue: result.continue ?? true }` 반환

### 11.4 Raw 어댑터 (`raw.ts`)

camelCase와 snake_case 모두 수용하는 범용 어댑터:
- `sessionId` 또는 `session_id` 모두 허용
- 출력은 `HookResult` 객체를 그대로 반환
- Codex CLI 및 기타 호환 플랫폼에서 사용

### 11.5 이벤트 핸들러 레지스트리 (`handlers/index.ts`)

7개의 이벤트 핸들러가 등록되어 있다:

| EventType | 핸들러 | 트리거 |
|-----------|--------|--------|
| `context` | `contextHandler` | SessionStart -- 컨텍스트 주입 |
| `session-init` | `sessionInitHandler` | UserPromptSubmit -- 세션 초기화 |
| `observation` | `observationHandler` | PostToolUse -- observation 저장 |
| `summarize` | `summarizeHandler` | Stop -- 요약 생성 (phase 1) |
| `session-complete` | `sessionCompleteHandler` | Stop -- 세션 완료 (phase 2, #842 fix) |
| `user-message` | `userMessageHandler` | SessionStart (병렬) -- 사용자 메시지 |
| `file-edit` | `fileEditHandler` | Cursor afterFileEdit |

`getEventHandler(eventType)`는 알 수 없는 이벤트 타입에 대해 예외를 던지지 않고 no-op 핸들러를 반환한다 (#984). Claude Code가 새로운 이벤트 타입을 추가해도 기존 플러그인이 BLOCKING_ERROR로 실패하지 않도록 하기 위함이다.

---

## 부록: 타입 정의 (`cli/types.ts`)

### NormalizedHookInput

```typescript
interface NormalizedHookInput {
  sessionId: string;
  cwd: string;
  platform?: string;      // 'claude-code' | 'cursor'
  prompt?: string;
  toolName?: string;
  toolInput?: unknown;
  toolResponse?: unknown;
  transcriptPath?: string;
  filePath?: string;      // Cursor afterFileEdit
  edits?: unknown[];      // Cursor afterFileEdit
}
```

### HookResult

```typescript
interface HookResult {
  continue?: boolean;
  suppressOutput?: boolean;
  hookSpecificOutput?: { hookEventName: string; additionalContext: string };
  systemMessage?: string;
  exitCode?: number;
}
```

`hookSpecificOutput`는 Claude Code의 `SessionStart` 훅에서 컨텍스트를 주입하기 위한 특수 필드이다. `continue: true`와 `suppressOutput: true`는 훅 실행 후 Claude Code가 정상적으로 계속 진행하도록 한다.

### hook-response.ts

```typescript
export const STANDARD_HOOK_RESPONSE = JSON.stringify({
  continue: true,
  suppressOutput: true
});
```

`SessionStart` 이외의 대부분의 훅에서 사용하는 표준 응답이다. `SessionStart`는 `context-hook.ts`에서 `hookSpecificOutput`을 포함한 별도의 응답을 구성한다.

---

## 부록: 트랜스크립트 타입 정의 (`transcripts/types.ts`)

### TranscriptSchema

```typescript
interface TranscriptSchema {
  name: string;
  version?: string;
  description?: string;
  eventTypePath?: string;    // 이벤트 타입 판별 경로 (기본: 'type')
  sessionIdPath?: string;    // 세션 ID 추출 경로
  cwdPath?: string;          // CWD 추출 경로
  projectPath?: string;      // 프로젝트 이름 추출 경로
  events: SchemaEvent[];     // 이벤트 정의 배열
}
```

### WatchTarget

```typescript
interface WatchTarget {
  name: string;              // 감시 대상 이름
  path: string;              // glob 패턴 또는 파일/디렉토리 경로
  schema: string | TranscriptSchema;  // 스키마 이름 또는 인라인 정의
  workspace?: string;        // 작업 디렉토리 (cwd 기본값)
  project?: string;          // 프로젝트 이름 (기본값)
  context?: WatchContextConfig;  // 컨텍스트 갱신 설정
  rescanIntervalMs?: number; // 새 파일 탐색 간격 (기본 5000ms)
  startAtEnd?: boolean;      // 기존 이력 건너뛰기
}
```

### TranscriptWatchConfig

```typescript
interface TranscriptWatchConfig {
  version: 1;
  schemas?: Record<string, TranscriptSchema>;
  watches: WatchTarget[];
  stateFile?: string;        // 상태 파일 경로
}
```
