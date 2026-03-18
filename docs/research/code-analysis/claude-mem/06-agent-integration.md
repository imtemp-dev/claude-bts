# claude-mem -- 에이전트 통합 분석

## 1. 에이전트 통합 아키텍처

claude-mem은 관찰(observation) 추출을 위해 세 가지 LLM 프로바이더를 지원하는 멀티 에이전트 아키텍처를 채택한다. 핵심 설계 원칙은 **프로바이더 교체 가능성**과 **자동 폴백(fallback)**이다.

### 1.1 공통 인터페이스 설계

세 에이전트(SDKAgent, GeminiAgent, OpenRouterAgent)는 동일한 시그니처를 공유한다:

```typescript
async startSession(session: ActiveSession, worker?: WorkerRef): Promise<void>
```

`ActiveSession`은 세션 상태 전체를 포함하며, `WorkerRef`는 SSE 브로드캐스트를 위한 선택적 참조이다. 모든 에이전트는 동일한 의존성을 주입받는다:

- `DatabaseManager` -- SQLite 저장소 접근
- `SessionManager` -- 메시지 이터레이터와 세션 상태 관리

### 1.2 프로바이더 간 대화 이력 공유

에이전트 간 전환을 가능하게 하는 핵심 메커니즘은 `session.conversationHistory` 배열이다. 이 배열은 `ConversationMessage[]` 타입이며, `{ role: 'user' | 'assistant', content: string }` 형태의 메시지를 누적한다. SDKAgent가 시작한 세션이 Gemini로 전환되더라도 이 이력이 보존되어 문맥이 유지된다.

### 1.3 응답 처리 통합

`agents/index.ts`가 모듈 진입점으로, 다음을 통합 re-export한다:

| 모듈 | 역할 |
|------|------|
| `ResponseProcessor.ts` | XML 파싱, DB 저장, Chroma 동기화, SSE 브로드캐스트 |
| `ObservationBroadcaster.ts` | SSE 이벤트 전송 |
| `SessionCleanupHelper.ts` | 처리 완료 메시지 정리 |
| `FallbackErrorHandler.ts` | 에러 분류, 폴백 판단 |
| `types.ts` | 공유 타입 정의 |

`processAgentResponse()` 함수가 세 에이전트 모두에서 호출되는 단일 응답 처리 경로를 제공하여, 150줄 이상의 중복 코드를 제거했다.

### 1.4 폴백 에이전트 패턴

GeminiAgent와 OpenRouterAgent는 `FallbackAgent` 인터페이스를 통해 Claude SDK로의 폴백을 지원한다:

```typescript
export interface FallbackAgent {
  startSession(session: ActiveSession, worker?: WorkerRef): Promise<void>;
}
```

폴백을 트리거하는 에러 패턴은 `FALLBACK_ERROR_PATTERNS`에 정의된다:

```typescript
export const FALLBACK_ERROR_PATTERNS = [
  '429',           // Rate limit
  '500',           // Internal server error
  '502',           // Bad gateway
  '503',           // Service unavailable
  'ECONNREFUSED',  // Connection refused
  'ETIMEDOUT',     // Timeout
  'fetch failed',  // Network failure
] as const;
```

`shouldFallbackToClaude()`는 에러 메시지에서 이 패턴을 검색하여 boolean을 반환하는 순수 판별 함수이다. 실제 폴백 호출(`this.fallbackAgent.startSession(session, worker)`)은 각 에이전트의 catch 블록에서 이 반환값을 기반으로 수행한다. `session.conversationHistory`가 공유되므로 문맥 손실 없이 프로바이더가 전환된다.

---

## 2. SDK Agent (SDKAgent.ts, 489줄)

SDKAgent는 `@anthropic-ai/claude-agent-sdk`의 `query()` 함수를 사용하여 Claude Code를 서브프로세스로 실행하는 에이전트이다. 이벤트 기반(event-driven) 쿼리 루프를 통해 폴링 없이 메시지를 처리한다.

### 2.1 세션 시작 흐름

`startSession()` 메서드는 다음 순서로 실행된다:

1. **cwdTracker 초기화** -- 워크트리 지원을 위한 mutable 객체 `{ lastCwd: undefined }` 생성. 제너레이터가 메시지의 `cwd`를 업데이트하면 응답 처리 시 반영된다.

2. **Claude 실행 파일 탐색** -- `findClaudeExecutable()`가 다음 순서로 검색:
   - `settings.CLAUDE_CODE_PATH` (사용자 설정)
   - Windows: `where claude.cmd` (PATHEXT 해결)
   - Unix: `which claude`
   - 모두 실패 시 명확한 에러 메시지와 함께 throw

3. **도구 제한 설정** -- 메모리 에이전트는 **순수 관찰자(OBSERVER ONLY)**로, 12개 도구를 모두 차단한다: `Bash`, `Read`, `Write`, `Edit`, `Grep`, `Glob`, `WebFetch`, `WebSearch`, `Task`, `NotebookEdit`, `AskUserQuestion`, `TodoWrite`. 이렇게 함으로써 무한 루프나 사이드 이펙트를 방지한다.

4. **Resume 판단 로직** -- 세션 재개 여부를 세 가지 조건으로 판단:
   ```
   shouldResume = hasRealMemorySessionId && session.lastPromptNumber > 1 && !session.forceInit
   ```
   - `memorySessionId`가 이전 SDK 응답에서 캡처된 것이어야 함
   - `lastPromptNumber > 1`이어야 함 (첫 프롬프트가 아님)
   - `forceInit` 플래그가 설정되지 않아야 함 (스테일 세션 복구 시 설정됨)
   - **중요**: `contentSessionId`를 resume에 사용하면 안 됨 -- 사용자의 트랜스크립트에 메시지가 주입될 위험

5. **동시 실행 슬롯 대기** -- `waitForSlot(maxConcurrent)`로 `CLAUDE_MEM_MAX_CONCURRENT_AGENTS` 설정값(기본 2)만큼의 슬롯을 대기. 이는 과도한 서브프로세스 생성을 방지한다.

6. **격리된 환경 구축** -- `sanitizeEnv(buildIsolatedEnv())`로 `~/.claude-mem/.env`에서만 인증 정보를 로드. 이는 Issue #733(프로젝트 `.env` 파일의 API 키 오염)을 방지한다.

7. **Agent SDK query() 호출** -- 최종적으로 `query()` 함수 호출:
   ```typescript
   const queryResult = query({
     prompt: messageGenerator,
     options: {
       model: modelId,
       cwd: OBSERVER_SESSIONS_DIR,  // 사용자의 resume 목록 오염 방지
       ...(shouldResume && { resume: session.memorySessionId }),
       disallowedTools,
       abortController: session.abortController,
       pathToClaudeCodeExecutable: claudePath,
       spawnClaudeCodeProcess: createPidCapturingSpawn(session.sessionDbId),
       env: isolatedEnv
     }
   });
   ```

### 2.2 메시지 제너레이터

`createMessageGenerator()`는 AsyncIterableIterator를 반환하는 비동기 제너레이터 함수이다. 두 가지 프롬프트 경로가 존재한다:

- **Prompt #1** (`lastPromptNumber === 1`): `buildInitPrompt()` -- 전체 초기화 프롬프트, 시스템 ID, 관찰자 역할, 출력 형식 포함
- **Prompt #2+** (`lastPromptNumber > 1`): `buildContinuationPrompt()` -- 세션 계속 프롬프트, 컨텍스트 유지

초기 프롬프트 이후, `sessionManager.getMessageIterator()`를 통해 이벤트 기반으로 메시지를 소비한다. 메시지 유형에 따라:

- **`observation`**: `buildObservationPrompt()`로 도구 사용 관찰을 SDK에 전달
- **`summarize`**: `buildSummaryPrompt()`로 진행 요약을 요청

모든 메시지는 `session.conversationHistory`에도 추가되어 프로바이더 간 호환성을 보장한다.

### 2.3 응답 처리 루프

`for await (const message of queryResult)` 루프에서:

1. **memorySessionId 캡처** -- SDK 응답의 `message.session_id`가 현재와 다르면 즉시 DB에 등록 (`ensureMemorySessionIdRegistered()`). FK 제약 조건 준수를 위해 observation 저장 **전**에 반드시 실행. 등록 후 SELECT로 검증까지 수행.

2. **assistant 메시지 처리**:
   - 텍스트 콘텐츠 추출 (배열 또는 문자열)
   - context overflow 감지 (`prompt is too long`, `context window`)
   - Invalid API key 감지 -- SDK가 에러가 아닌 응답 텍스트로 반환하므로 명시적 throw
   - 토큰 사용량 추적 (input, output, cache_creation)
   - `processAgentResponse()`로 파싱 및 저장 위임

3. **서브프로세스 종료 보장** -- `finally` 블록에서 `getProcessBySession()`으로 추적된 프로세스를 확인하고, 아직 실행 중이면 `ensureProcessExit(tracked, 5000)` (5초 타임아웃)로 종료. 좀비 프로세스 축적(Issue #737)을 방지한다.

### 2.4 모델 설정

`getModelId()`는 `~/.claude-mem/settings.json`의 `CLAUDE_MEM_MODEL` 값을 반환한다. 별도의 유효성 검증 없이 SDK에 직접 전달된다.

---

## 3. Gemini Agent (GeminiAgent.ts, 471줄)

GeminiAgent는 Google Gemini REST API를 직접 호출하여 관찰을 추출하는 대안 에이전트이다. SDK 의존성 없이 순수 HTTP 통신으로 동작한다.

### 3.1 지원 모델 및 요금 제한

지원 모델은 타입으로 정의된다:

```typescript
export type GeminiModel =
  | 'gemini-2.5-flash-lite'
  | 'gemini-2.5-flash'
  | 'gemini-2.5-pro'
  | 'gemini-2.0-flash'
  | 'gemini-2.0-flash-lite'
  | 'gemini-3-flash'
  | 'gemini-3-flash-preview';
```

무료 티어 RPM 제한이 모델별로 정의된다:

| 모델 | RPM |
|------|-----|
| `gemini-2.0-flash-lite` | 30 |
| `gemini-2.0-flash` | 15 |
| `gemini-2.5-flash-lite` | 10 |
| `gemini-2.5-flash` | 10 |
| `gemini-3-flash` | 10 |
| `gemini-2.5-pro` | 5 |
| `gemini-3-flash-preview` | 5 |

`enforceRateLimitForModel()` 함수가 요청 간 최소 지연을 강제한다: `Math.ceil(60000 / rpm) + 100ms`. 유료 사용자는 `CLAUDE_MEM_GEMINI_RATE_LIMITING_ENABLED=false`로 비활성화할 수 있다.

### 3.2 세션 관리

SDKAgent와 달리 Gemini는 상태 비저장(stateless) API이므로:

- **합성 memorySessionId 생성**: `gemini-${contentSessionId}-${timestamp}` 형태로 생성하여 DB에 등록
- **멀티턴 대화**: `session.conversationHistory` 전체를 매 요청 시 전송하여 문맥 유지
- **역할 매핑**: Gemini API는 `model` 역할을 사용하므로 `conversationToGeminiContents()`에서 `assistant` -> `model`로 변환

### 3.3 API 호출

`queryGeminiMultiTurn()`이 핵심 API 호출을 수행한다:

```
POST https://generativelanguage.googleapis.com/v1/models/${model}:generateContent?key=${apiKey}
```

요청 본문:
```json
{
  "contents": [/* 전체 대화 이력 */],
  "generationConfig": {
    "temperature": 0.3,
    "maxOutputTokens": 4096
  }
}
```

v1beta가 아닌 **v1 (stable)** 엔드포인트를 사용한다. v1beta는 `gemini-3-flash` 같은 최신 모델을 지원하지 않기 때문이다.

### 3.4 토큰 추적

Gemini의 `usageMetadata.totalTokenCount`를 사용하되, input/output 분리가 불가하므로 70/30 비율로 추정한다:

```typescript
session.cumulativeInputTokens += Math.floor(tokensUsed * 0.7);
session.cumulativeOutputTokens += Math.floor(tokensUsed * 0.3);
```

### 3.5 설정 소스

`getGeminiConfig()`가 다음 우선순위로 API 키를 탐색한다:
1. `settings.CLAUDE_MEM_GEMINI_API_KEY`
2. `getCredential('GEMINI_API_KEY')` (중앙화된 `~/.claude-mem/.env`)
3. 빈 문자열 (에러 발생)

기본 모델은 `gemini-2.5-flash`이며, 잘못된 모델명이 설정되면 경고와 함께 기본값으로 폴백한다.

### 3.6 가용성 확인

모듈 수준 함수 두 개가 외부에서 Gemini 상태를 확인할 수 있다:
- `isGeminiAvailable()` -- API 키 존재 여부
- `isGeminiSelected()` -- `CLAUDE_MEM_PROVIDER === 'gemini'` 여부

---

## 4. OpenRouter Agent (OpenRouterAgent.ts, 474줄)

OpenRouterAgent는 OpenRouter의 통합 API를 통해 100개 이상의 모델에 접근하는 에이전트이다. OpenAI 호환 인터페이스를 사용한다.

### 4.1 API 통신

엔드포인트: `https://openrouter.ai/api/v1/chat/completions`

요청 헤더:
```
Authorization: Bearer ${apiKey}
HTTP-Referer: ${siteUrl || 'https://github.com/thedotmack/claude-mem'}
X-Title: ${appName || 'claude-mem'}
Content-Type: application/json
```

요청 본문:
```json
{
  "model": "xiaomi/mimo-v2-flash:free",
  "messages": [/* OpenAI 형식 대화 이력 */],
  "temperature": 0.3,
  "max_tokens": 4096
}
```

기본 모델은 `xiaomi/mimo-v2-flash:free`이며, `CLAUDE_MEM_OPENROUTER_MODEL` 설정으로 변경 가능하다.

### 4.2 컨텍스트 윈도우 관리

Gemini와 달리 OpenRouterAgent는 **대화 이력 잘라내기(truncation)**를 구현한다. 비용 폭주를 방지하기 위해:

```typescript
const DEFAULT_MAX_CONTEXT_MESSAGES = 20;
const DEFAULT_MAX_ESTIMATED_TOKENS = 100000;
const CHARS_PER_TOKEN_ESTIMATE = 4;
```

`truncateHistory()` 메서드가 슬라이딩 윈도우 방식으로 동작한다:
1. 가장 최근 메시지부터 역순으로 처리
2. `MAX_CONTEXT_MESSAGES` 또는 `MAX_ESTIMATED_TOKENS` 초과 시 이전 메시지 삭제
3. 토큰 추정: `text.length / 4`

이 제한값은 설정으로 조정 가능하다:
- `CLAUDE_MEM_OPENROUTER_MAX_CONTEXT_MESSAGES`
- `CLAUDE_MEM_OPENROUTER_MAX_TOKENS`

### 4.3 비용 추적

`queryOpenRouterMultiTurn()`에서 실제 토큰 사용량을 로깅한다:

```typescript
const estimatedCost = (inputTokens / 1000000 * 3) + (outputTokens / 1000000 * 15);
```

50,000 토큰 초과 시 경고 로그를 출력한다.

### 4.4 대화 이력 특이점

OpenRouterAgent에서는 `session.conversationHistory`에 assistant 응답을 추가하는 코드가 주석 처리되어 있다:

```typescript
// session.conversationHistory.push({ role: 'assistant', content: obsResponse.content });
```

이는 SDKAgent, GeminiAgent와 다른 패턴으로, OpenRouter 호출 시 이전 assistant 응답이 이력에 포함되지 않는다는 의미이다. 컨텍스트 비용 절감을 위한 의도적 설계로 보인다.

### 4.5 설정 소스

`getOpenRouterConfig()`가 반환하는 설정:
- `apiKey`: `CLAUDE_MEM_OPENROUTER_API_KEY` 또는 `getCredential('OPENROUTER_API_KEY')`
- `model`: `CLAUDE_MEM_OPENROUTER_MODEL` 또는 `'xiaomi/mimo-v2-flash:free'`
- `siteUrl`: `CLAUDE_MEM_OPENROUTER_SITE_URL` (분석 헤더용)
- `appName`: `CLAUDE_MEM_OPENROUTER_APP_NAME` (기본 `'claude-mem'`)

---

## 5. 응답 처리 (ResponseProcessor.ts, 330줄)

`processAgentResponse()`는 세 에이전트 공통의 응답 처리 파이프라인이다. 9개 파라미터(8개 필수 + 1개 선택적 `projectRoot?`)를 받아 6단계 처리를 수행한다.

### 5.1 함수 시그니처

```typescript
export async function processAgentResponse(
  text: string,
  session: ActiveSession,
  dbManager: DatabaseManager,
  sessionManager: SessionManager,
  worker: WorkerRef | undefined,
  discoveryTokens: number,
  originalTimestamp: number | null,
  agentName: string,
  projectRoot?: string
): Promise<void>
```

### 5.2 처리 파이프라인

**단계 1: 제너레이터 활동 추적**
```typescript
session.lastGeneratorActivity = Date.now();
```
Issue #1099의 스테일 감지를 위해 마지막 활동 시간을 기록한다.

**단계 2: 대화 이력 추가**
assistant 응답을 `session.conversationHistory`에 추가하여 프로바이더 간 호환성을 유지한다.

**단계 3: XML 파싱**
```typescript
const observations = parseObservations(text, session.contentSessionId);
const summary = parseSummary(text, session.sessionDbId);
```

**단계 4: 원자적 DB 트랜잭션**

memorySessionId 검증 후, `storeObservations()`를 단일 트랜잭션으로 호출한다:

```typescript
const result = sessionStore.storeObservations(
  session.memorySessionId,
  session.project,
  observations,
  summaryForStore,
  session.lastPromptNumber,
  discoveryTokens,
  originalTimestamp ?? undefined
);
```

**단계 5: Claim-Confirm 큐 처리**

저장 성공 후, 처리 중이던 메시지들을 확인(confirm)한다:

```typescript
for (const messageId of session.processingMessageIds) {
  pendingStore.confirmProcessed(messageId);
}
session.processingMessageIds = [];
```

이 패턴은 제너레이터 크래시 시 메시지 손실을 방지한다.

**단계 6: 비동기 후처리**

DB 커밋 이후 안전하게 실행되는 비동기 작업들:
- `syncAndBroadcastObservations()`: Chroma 동기화 (fire-and-forget), SSE 브로드캐스트, 폴더 CLAUDE.md 업데이트
- `syncAndBroadcastSummary()`: 요약 Chroma 동기화, SSE 브로드캐스트, Cursor 컨텍스트 파일 업데이트
- `cleanupProcessedMessages()`: 세션 상태 정리

### 5.3 Summary 정규화

`normalizeSummaryForStorage()`가 null 필드를 빈 문자열로 변환한다. `notes`만 null 허용:

```typescript
{
  request: summary.request || '',
  investigated: summary.investigated || '',
  learned: summary.learned || '',
  completed: summary.completed || '',
  next_steps: summary.next_steps || '',
  notes: summary.notes  // null 허용
}
```

### 5.4 Chroma 동기화 전략

관찰과 요약 모두 fire-and-forget 패턴으로 Chroma에 동기화된다:

```typescript
dbManager.getChromaSync()?.syncObservation(...)
  .then(() => { /* 성공 로깅 */ })
  .catch((error) => { /* 에러 로깅, 계속 진행 */ });
```

Chroma 실패가 관찰 저장을 차단하지 않는다는 점이 핵심이다.

### 5.5 폴더 CLAUDE.md 업데이트

`CLAUDE_MEM_FOLDER_CLAUDEMD_ENABLED` 설정이 `true`일 때만 실행된다 (기본 `false`). 관찰에 포함된 `files_read`와 `files_modified`의 경로에서 폴더를 추출하여 해당 폴더의 CLAUDE.md를 업데이트한다.

### 5.6 Cursor 컨텍스트 업데이트

요약 저장 후 `updateCursorContextForProject()`를 fire-and-forget으로 호출하여, 등록된 Cursor 프로젝트의 컨텍스트 파일을 갱신한다.

---

## 6. SDK 모듈 -- 파서 (sdk/parser.ts, 212줄)

`parser.ts`는 에이전트 응답에서 관찰(observation)과 요약(summary) XML 블록을 파싱하는 모듈이다.

### 6.1 데이터 타입

```typescript
export interface ParsedObservation {
  type: string;
  title: string | null;
  subtitle: string | null;
  facts: string[];
  narrative: string | null;
  concepts: string[];
  files_read: string[];
  files_modified: string[];
}

export interface ParsedSummary {
  request: string | null;
  investigated: string | null;
  learned: string | null;
  completed: string | null;
  next_steps: string | null;
  notes: string | null;
}
```

### 6.2 관찰 파싱 (`parseObservations`)

정규식 `/<observation>([\s\S]*?)<\/observation>/g`로 모든 `<observation>` 블록을 추출한다. 각 블록에서:

1. `extractField()` -- 단일 필드 추출: `<fieldName>[\s\S]*?</fieldName>` 패턴. 비탐욕(non-greedy) 매칭으로 중첩 태그와 코드 스니펫을 처리한다 (Issue #798).
2. `extractArrayElements()` -- 배열 필드 추출: `<facts><fact>...</fact></facts>` 형태에서 개별 요소를 추출.

**타입 유효성 검증**: ModeManager에서 활성 모드의 `observation_types`를 조회하여, 파싱된 타입이 유효한지 검증한다. 유효하지 않으면 모드의 첫 번째 타입을 fallback으로 사용한다.

**핵심 원칙** (코드 주석에서 명시): "ALWAYS save observations - never skip." 모든 필드가 nullable이므로, 타입이 누락되더라도 관찰을 반드시 저장한다.

**Concepts 정리**: 관찰의 concepts 배열에서 타입 ID와 동일한 값이 있으면 제거한다. 타입과 컨셉은 별개의 차원이기 때문이다.

### 6.3 요약 파싱 (`parseSummary`)

1. `<skip_summary reason="..."/>` 태그를 먼저 확인하여, 건너뛰기가 명시되면 null 반환
2. `<summary>[\s\S]*?</summary>` 패턴으로 요약 블록 추출
3. 6개 필드 추출: request, investigated, learned, completed, next_steps, notes

코드 주석에서 개발자가 강하게 명시한 원칙: "100% of the time we must SAVE the summary, even if fields are missing. NEVER DO THIS NONSENSE AGAIN." -- 이전에 필수 필드 누락 시 null을 반환하던 코드가 주석 처리되어 있다.

### 6.4 프롬프트 컨디셔닝 진단

요약 파싱 시 `<summary>` 대신 `<observation>` 태그가 발견되면 경고 로그를 출력한다 (Issue #1312). 이는 프롬프트 컨디셔닝이 충분히 강하지 않아 에이전트가 잘못된 형식으로 응답하는 경우를 진단하기 위함이다.

---

## 7. SDK 프롬프트 (sdk/prompts.ts, 237줄)

`prompts.ts`는 에이전트에 전송할 프롬프트 템플릿을 생성하는 모듈이다. 모든 프롬프트는 `ModeConfig`에서 텍스트를 가져와 조합한다.

### 7.1 프롬프트 유형

#### `buildInitPrompt(project, sessionId, userPrompt, mode)`

첫 번째 프롬프트 (promptNumber === 1)에서 사용. 구조:

```
{system_identity}

<observed_from_primary_session>
  <user_request>{userPrompt}</user_request>
  <requested_at>{날짜}</requested_at>
</observed_from_primary_session>

{observer_role}
{spatial_awareness}
{recording_focus}
{skip_guidance}
{output_format_header}

```xml
<observation>
  <type>[ type1 | type2 | ... ]</type>
  <title>...</title>
  <subtitle>...</subtitle>
  <facts>...</facts>
  <narrative>...</narrative>
  <concepts>...</concepts>
  <files_read>...</files_read>
  <files_modified>...</files_modified>
</observation>
```

{format_examples}
{footer}
{header_memory_start}
```

핵심 설계: 프롬프트의 모든 텍스트 조각이 `mode.prompts.*`에서 오므로, 모드 JSON 파일만 바꾸면 전혀 다른 도메인(법학, 이메일 조사 등)의 관찰 에이전트를 구현할 수 있다.

#### `buildObservationPrompt(obs: Observation)`

도구 사용 관찰을 에이전트에 전달하는 프롬프트:

```xml
<observed_from_primary_session>
  <what_happened>{tool_name}</what_happened>
  <occurred_at>{ISO timestamp}</occurred_at>
  <working_directory>{cwd}</working_directory>
  <parameters>{tool_input JSON}</parameters>
  <outcome>{tool_output JSON}</outcome>
</observed_from_primary_session>
```

`tool_input`과 `tool_output`은 이중 파싱을 시도한다 -- 이미 JSON 문자열인 경우 파싱 후 다시 `JSON.stringify(_, null, 2)`로 포매팅한다. 파싱 실패 시 원본 문자열을 그대로 사용한다.

#### `buildSummaryPrompt(session, mode)`

진행 요약 요청 프롬프트. 명확한 모드 전환 지시를 포함한다:

```
--- MODE SWITCH: PROGRESS SUMMARY ---
Do NOT output <observation> tags. This is a summary request, not an observation request.
Your response MUST use <summary> tags ONLY. Any <observation> output will be discarded.
```

이 강한 지시는 에이전트가 `<observation>` 대신 `<summary>` 태그를 사용하도록 강제한다. `mode.prompts.summary_instruction`과 `summary_footer`가 추가 지침을 제공한다.

#### `buildContinuationPrompt(userPrompt, promptNumber, contentSessionId, mode)`

두 번째 이후 프롬프트에서 사용. `buildInitPrompt`과 유사하지만:
- `continuation_greeting`으로 시작 ("Hello memory agent, you are continuing to observe...")
- `continuation_instruction`으로 계속 관찰하도록 지시
- `header_memory_continued` 사용 (초기 프롬프트의 `header_memory_start` 대신)

**contentSessionId 파라미터의 중요성**: 이 함수의 주석에 상세히 기록되어 있다. contentSessionId는 hook의 session_id에서 유래하며, NEW hook (세션 생성), SAVE hook (관찰 저장), continuation prompt (세션 문맥 유지) 모두에서 동일한 값이 사용된다. 이것이 하나의 대화에서 모든 것이 연결되는 핵심 메커니즘이다.

### 7.2 타입 정의

```typescript
export interface Observation {
  id: number;
  tool_name: string;
  tool_input: string;
  tool_output: string;
  created_at_epoch: number;
  cwd?: string;
}

export interface SDKSession {
  id: number;
  memory_session_id: string | null;
  project: string;
  user_prompt: string;
  last_assistant_message?: string;
}
```

---

## 8. 에이전트 선택 로직

### 8.1 프로바이더 선택

에이전트 선택은 `CLAUDE_MEM_PROVIDER` 설정에 의해 결정된다:

| 설정값 | 에이전트 | 비고 |
|--------|----------|------|
| `'claude'` (기본) | SDKAgent | Claude Agent SDK 사용 |
| `'gemini'` | GeminiAgent | Google Gemini REST API |
| `'openrouter'` | OpenRouterAgent | OpenRouter 통합 API |

각 에이전트의 가용성은 모듈 수준 함수로 확인된다:
- `isGeminiAvailable()` / `isGeminiSelected()`
- `isOpenRouterAvailable()` / `isOpenRouterSelected()`

### 8.2 폴백 체인

폴백은 단방향이다:

```
GeminiAgent ---[API 실패]--> SDKAgent (Claude)
OpenRouterAgent ---[API 실패]--> SDKAgent (Claude)
SDKAgent ---[실패]--> (폴백 없음, 에러 전파)
```

폴백 에이전트는 생성 후 `setFallbackAgent()`로 주입된다. 순환 의존성을 피하기 위해 생성자가 아닌 별도 메서드로 설정한다.

### 8.3 동시성 제어

SDKAgent만 `ProcessRegistry`를 통한 동시성 제어를 구현한다:
- `waitForSlot(maxConcurrent)` -- 슬롯 대기
- `createPidCapturingSpawn()` -- PID 캡처하여 좀비 방지
- `getProcessBySession()` / `ensureProcessExit()` -- 프로세스 생명주기 관리

Gemini와 OpenRouter는 HTTP 기반이므로 서브프로세스 관리가 불필요하다. 대신 Gemini는 RPM 제한, OpenRouter는 컨텍스트 윈도우 잘라내기로 리소스를 관리한다.

### 8.4 인증 격리

세 에이전트 모두 Issue #733 방지를 위해 `~/.claude-mem/.env` 또는 `~/.claude-mem/settings.json`에서만 인증 정보를 로드한다. `process.env`를 직접 사용하지 않으며, `getCredential()` 함수가 중앙화된 `.env` 파일에서만 값을 읽는다. SDKAgent는 추가로 `sanitizeEnv(buildIsolatedEnv())`를 통해 서브프로세스 환경에서 오염된 환경 변수를 제거한다.
