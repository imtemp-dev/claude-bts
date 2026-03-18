# ContextStream -- 컨텍스트 주입 분석

## 개요

ContextStream의 컨텍스트 주입 시스템은 매 AI 응답 전에 사용자 메시지를 분석하여 관련 컨텍스트를 검색하고, 토큰 효율적인 형식으로 AI에게 전달하는 핵심 메커니즘이다. `context` 도구(이전 `context_smart`)는 매 메시지마다 반드시 호출되어야 하며, SessionManager를 통한 토큰 추적, 컨텍스트 압력 계산, 사전 레슨 주입, 시맨틱 인텐트 분석 등 다층적 기능을 수행한다.

핵심 소스 파일:
- `src/tools.ts` -- `context` 도구 등록 및 출력 조립 (라인 ~8543-8979)
- `src/session-manager.ts` -- SessionManager 클래스: 토큰 추적, 컨텍스트 압력, post-compaction 복원
- `src/client.ts` -- `getSmartContext()` API 호출, `getHighPriorityLessons()`, SemanticIntent 타입

---

## 1. context 도구 핵심

### 1.1 도구 설명

`context` 도구(이전명 `context_smart`)는 ContextStream에서 가장 빈번하게 호출되는 도구이다. 도구 설명에 명시된 바와 같이 **모든 AI 응답 전에 반드시 호출**되어야 한다:

> "**CALL THIS BEFORE EVERY AI RESPONSE** to get relevant context."

### 1.2 user_message 파라미터

핵심 입력은 `user_message`(필수)이다. 사용자의 현재 메시지를 전달하면 서버가 이를 분석하여 관련 컨텍스트를 검색한다:

```
1. 사용자가 "how should I implement auth?"를 물으면
2. AI가 context(user_message="how should I implement auth?") 호출
3. 반환: "W:Maker|P:contextstream|D:Use JWT for auth|D:No session cookies|M:Auth API at /auth/..."
4. AI가 관련 컨텍스트를 이미 로드한 상태로 응답
```

### 1.3 포맷 옵션

`format` 파라미터로 3가지 출력 형식을 선택할 수 있다:

| format | 설명 | 토큰 소비 |
|--------|------|-----------|
| `minified` | 울트라 컴팩트. 파이프(`\|`)로 구분된 타입 코드 형식. 기본값. | ~200 tokens |
| `readable` | 라인 구분, 라벨 포함 형식 | 상대적으로 많음 |
| `structured` | JSON 유사 그룹화 형식 | 상대적으로 많음 |

타입 코드 규약: `W`=Workspace, `P`=Project, `D`=Decision, `M`=Memory, `I`=Insight, `T`=Task, `L`=Lesson.

minified 형식 예시: `W:Maker|P:contextstream|D:Use JWT for auth|D:No session cookies|M:Auth API at /auth/...`

### 1.4 standard/pack 모드

`mode` 파라미터로 2가지 컨텍스트 깊이를 선택한다:

| mode | 설명 | 비용 |
|------|------|------|
| `standard` | 메모리/결정/인사이트 기반 컨텍스트만 반환 | 기본 |
| `pack` | standard + 코드 컨텍스트 + distillation 포함. 프로젝트가 설정되어 있으면 자동으로 pack 모드 사용 | 더 높은 credit 비용 |

pack 모드의 자동 선택 로직 (`client.ts`):

```typescript
const usePackDefault = this.config.contextPackEnabled !== false && !!withDefaults.project_id;
const mode = params.mode || (usePackDefault ? "pack" : "standard");
```

### 1.5 추가 파라미터

| 파라미터 | 설명 |
|----------|------|
| `max_tokens` | 컨텍스트 최대 토큰 수 (기본: 800) |
| `session_tokens` | 누적 세션 토큰 수 (컨텍스트 압력 계산용) |
| `context_threshold` | 커스텀 컨텍스트 윈도우 임계값 (기본: 70k) |
| `save_exchange` | 이 교환을 transcript에 저장할지 여부 (background task) |
| `session_id` | transcript 연동용 세션 ID |
| `client_name` | transcript 메타데이터용 클라이언트 이름 (예: "claude", "cursor") |
| `assistant_message` | 이전 AI 응답 (완전한 교환 캡처용) |
| `distill` | pack 모드에서 distillation 사용 여부 (기본: true) |

---

## 2. 토큰 추적 -- SessionManager

### 2.1 MCP 서버의 한계

MCP 서버는 실제 AI 토큰 사용량(응답, thinking, system prompt 등)을 직접 관측할 수 없다. 따라서 SessionManager는 추적 가능한 토큰과 턴(turn) 기반 추정을 조합하여 근사 토큰 사용량을 계산한다.

### 2.2 TOKENS_PER_TURN_ESTIMATE

```typescript
private static readonly TOKENS_PER_TURN_ESTIMATE = 3000;
```

각 대화 턴에서 예상되는 토큰 소비:
- 사용자 메시지: ~500 tokens
- AI 응답: ~1500 tokens
- system prompt 오버헤드: ~500 tokens
- reasoning: ~1500 tokens
- 보수적 합산: **3000 tokens/turn**

### 2.3 토큰 추적 메커니즘

```typescript
private sessionTokens = 0;           // 추적된 실제 토큰 (도구 입출력)
private conversationTurns = 0;       // context 도구 호출 횟수 (= 대화 턴 수)
```

세션 토큰 총계 계산:

```typescript
getSessionTokens(): number {
  const turnEstimate = this.conversationTurns * SessionManager.TOKENS_PER_TURN_ESTIMATE;
  return this.sessionTokens + turnEstimate;
}
```

토큰 추가 방법:

```typescript
addTokens(tokens: number | string) {
  if (typeof tokens === "number") {
    this.sessionTokens += tokens;
  } else {
    // 텍스트에서 추정: ~4 chars per token
    this.sessionTokens += Math.ceil(tokens.length / 4);
  }
}
```

### 2.4 contextThreshold

```typescript
private contextThreshold = 70000; // 100k 컨텍스트 윈도우에 대한 보수적 기본값
```

이 값은 `setContextThreshold(threshold)`로 클라이언트가 모델 정보를 제공할 때 조정할 수 있다.

### 2.5 context 도구에서의 토큰 추적

`context` 도구 핸들러 내부에서 수행하는 토큰 관련 작업:

1. `sessionManager.markContextSmartCalled()` -- `conversationTurns++` 수행
2. SessionManager에서 `sessionTokens`와 `contextThreshold`를 가져옴 (입력으로 제공되지 않은 경우)
3. `sessionManager.addTokens(input.user_message)` -- 사용자 메시지 토큰 추적
4. API 응답 수신 후 `sessionManager.addTokens(result.token_estimate)` -- 응답 토큰 추적

---

## 3. 컨텍스트 압력

### 3.1 4단계 압력 수준

서버 API(`/context/smart`)가 `context_pressure` 객체를 반환하며, 4단계 수준으로 분류된다:

| 수준 | 설명 | 임계값 | 제안 동작 |
|------|------|--------|-----------|
| `low` | 정상 상태 | ~50% 미만 | `"none"` |
| `medium` | 주의 필요 | ~50-70% | 자동 저장 시작 |
| `high` | 위험 수준 | ~70-85% | `"prepare_save"` -- 중요 결정/상태 저장 고려 |
| `critical` | 즉시 조치 필요 | ~85%+ | `"save_now"` -- 즉시 상태 저장, compaction 임박 |

압력 수준은 `usage_percent = (session_tokens / threshold) * 100`으로 계산된다.

### 3.2 context_pressure 응답 구조

```typescript
context_pressure?: {
  level: "low" | "medium" | "high" | "critical";
  session_tokens: number;
  threshold: number;
  usage_percent: number;
  threshold_warning: boolean;
  suggested_action: "none" | "prepare_save" | "save_now";
};
```

### 3.3 압력 수준별 경고 메시지

**critical:**
```
[CONTEXT PRESSURE: CRITICAL] 92% of context used (64400/70000 tokens)
Action: SAVE STATE NOW - Call session(action="capture") to preserve conversation state
before compaction. The conversation may compact soon. Save important decisions,
insights, and progress immediately.
```

**high:**
```
[CONTEXT PRESSURE: HIGH] 78% of context used (54600/70000 tokens)
Action: Consider saving important decisions and conversation state soon.
```

### 3.4 자동 저장(Auto-save) 메커니즘

medium 이상의 압력에서 자동으로 세션 스냅샷을 저장한다. 스팸 방지를 위한 3가지 제어 변수:

```typescript
const AUTO_SAVE_MIN_INTERVAL_MS = 2 * 60 * 1000;  // 최소 2분 간격
const AUTO_SAVE_CALL_INTERVAL = 10;                 // 10회 context 호출마다
let lastAutoSaveTime = 0;
let lastAutoSavePressureLevel = "";
let contextCallsSinceLastSave = 0;
```

자동 저장 트리거 조건 (OR):
1. **압력 수준 증가**: `medium -> high` 또는 `high -> critical`로 상승 && 최소 간격(2분) 경과
2. **호출 횟수**: medium+ 상태에서 context 호출이 10회 누적
3. **시간 폴백**: 최소 간격의 2.5배(5분) 경과

저장 내용은 `session_snapshot` 이벤트 타입으로 캡처하며, 트리거 종류, 압력 상태, 토큰 수, 사용자 메시지 미리보기(100자)를 포함한다.

### 3.5 SessionManager에서의 압력 추적

```typescript
markHighContextPressure() {
  this.lastHighPressureAt = Date.now();
  this.lastHighPressureTokens = this.getSessionTokens();
}
```

high 또는 critical 압력이 감지되면 `markHighContextPressure()`를 호출하여 타임스탬프와 당시 토큰 수를 기록한다. 이 정보는 post-compaction 복원에 사용된다.

---

## 4. 사전 레슨 주입

### 4.1 위험 키워드 감지

`detectRiskyActions(userMessage)` 함수는 사용자 메시지에서 위험한 작업을 나타내는 키워드를 탐지한다. `RISKY_ACTION_KEYWORDS` 배열에 정의된 키워드:

| 카테고리 | 키워드 |
|----------|--------|
| 코드 변경 | refactor, rewrite, restructure, reorganize, migrate |
| 삭제 | delete, remove, drop, deprecate |
| 데이터베이스 | database, migration, schema, sql |
| 배포 | deploy, release, production, prod |
| API 변경 | api, endpoint, breaking change |
| 아키텍처 | architecture, design, pattern |
| 테스팅 | test, testing |
| 보안 | auth, security, permission, credential, access, token, secret |
| 버전 관리 | git, commit, merge, rebase, push, force |
| 인프라 | config, environment, env, docker, kubernetes, k8s |
| 성능 | performance, optimize, cache, memory |

총 30+ 키워드가 대소문자 무관하게 매칭된다.

### 4.2 getHighPriorityLessons() 호출

위험 키워드가 1개 이상 감지되면 서버에서 관련 레슨을 사전 조회한다:

```typescript
const lessons = await client.getHighPriorityLessons({
  workspace_id: workspaceId,
  project_id: projectId,
  context_hint: riskyKeywords.join(" "),  // 감지된 키워드를 검색 힌트로 전달
  limit: 5,
});
```

`getHighPriorityLessons()`는 내부적으로 `memorySearch()`를 호출하여 "lesson warning prevention mistake" 키워드와 `context_hint`를 조합한 쿼리로 검색한다. 반환된 결과에서 `lesson` 또는 `lesson_system` 태그가 있고, severity가 `critical` 또는 `high`인 항목만 필터링한다.

### 4.3 severity 배지

레슨 항목의 severity에 따라 시각적 배지가 부여된다:

| severity | 배지 |
|----------|------|
| critical | `(critical badge)` |
| high | `(warning badge)` |
| medium 이하 | `(note badge)` |

### 4.4 LESSONS_WARNING 포맷

레슨이 발견되면 다음과 같은 블록이 context 응답에 주입된다:

```
[LESSONS_WARNING] Relevant Lessons for "refactor, auth"
(separator line)
IMPORTANT: You MUST tell the user about these lessons before proceeding.
These are past mistakes that may be relevant to the current task.

1. (severity badge) Lesson Title: Prevention description (max 100 chars)
2. (severity badge) Another Lesson: How to avoid this...

Action: Review each lesson and explain to the user how you will avoid these mistakes.
(separator line)
```

### 4.5 폴백: 컨텍스트 내 레슨 감지

사전 레슨 조회가 0건이더라도, 반환된 컨텍스트 문자열에 `|L:`, `L:`, 또는 "lesson" 키워드가 포함되어 있으면 간략한 경고를 추가한다:

```
[LESSONS_WARNING] Lessons found in context - review the L: items above before making changes.
```

레슨 사전 조회 실패(API 에러 등)는 무시하며 context 도구의 응답을 차단하지 않는다.

---

## 5. Post-Compaction 복원

### 5.1 shouldRestorePostCompact()

AI 대화 도구(Claude, Cursor 등)는 컨텍스트 윈도우가 가득 차면 대화를 자동으로 압축(compaction)한다. 이때 이전 대화 내용이 크게 손실된다. ContextStream은 이를 감지하고 저장된 스냅샷에서 핵심 컨텍스트를 복원한다.

감지 휴리스틱 (`shouldRestorePostCompact()`):

```
조건 1: postCompactRestoreCompleted === false (이미 복원하지 않음)
조건 2: lastHighPressureAt !== null (이전에 high/critical 압력 기록 있음)
조건 3: 경과 시간 < 10분 (high 압력 기록이 10분 이내)
조건 4: 현재 토큰 < 10,000 (토큰이 크게 감소)
조건 5: 토큰 감소량 >= lastHighPressureTokens * 50% (50% 이상 감소)
```

핵심 판정: 이전에 높은 압력(많은 토큰)을 기록했는데 갑자기 토큰이 크게 떨어지면 compaction이 발생한 것으로 판단한다.

### 5.2 복원 프로세스

`context` 도구 핸들러에서 `shouldRestorePostCompact()`가 true를 반환하면:

1. `client.listMemoryEvents()`로 최근 20개 이벤트를 조회
2. `event_type === "session_snapshot"` 또는 태그에 `"session_snapshot"` 포함된 이벤트 검색
3. 스냅샷 content를 JSON 파싱 (실패 시 전체를 `conversation_summary`로 처리)
4. 다음 필드를 추출하여 복원 컨텍스트 구성:
   - `conversation_summary` / `summary`
   - `key_decisions` (최대 5개)
   - `unfinished_work` / `pending_tasks` (최대 3개)
   - `active_files` (최대 5개)
5. `[POST-COMPACTION CONTEXT RESTORED]` 헤더와 함께 컨텍스트 앞에 삽입
6. `markPostCompactRestoreCompleted()` 호출하여 세션 내 중복 복원 방지

### 5.3 복원 후 상태 초기화

```typescript
markPostCompactRestoreCompleted() {
  this.postCompactRestoreCompleted = true;
  this.lastHighPressureAt = null;
  this.lastHighPressureTokens = 0;
}
```

복원 완료 후 압력 추적 상태를 초기화하여 동일 세션에서 재복원을 방지한다.

---

## 6. 시맨틱 인텐트 -- SmartRouter

### 6.1 SemanticIntent 타입

서버 측 SmartRouter가 사용자 메시지를 분석하여 반환하는 의미 분류 결과:

```typescript
interface SemanticIntent {
  intent_type: string;              // 인텐트 유형 (예: "code_change", "question", "deployment")
  risk_level: "none" | "low" | "medium" | "high" | "critical";
  confidence: number;               // 0-1 범위의 분류 신뢰도
  decision_detected: boolean;       // 사용자가 의사결정을 내리고 있는지
  capture_worthy: boolean;          // 캡처할 가치가 있는 내용인지
  suggested_capture_type?: string;  // 권장 캡처 이벤트 타입 (예: "decision", "insight")
  suggested_capture_title?: string; // 권장 캡처 제목
  extracted_entities?: string[];    // 추출된 엔티티
  explanation?: string;             // 분류 근거 설명
}
```

### 6.2 risk_level 경고

`context` 도구 핸들러에서 risk_level이 `high` 또는 `critical`이면 경고를 생성한다:

```typescript
if (si.risk_level === "high" || si.risk_level === "critical") {
  hints.push(`[RISK:${si.risk_level.toUpperCase()}] ${si.explanation || "Proceed with caution"}`);
}
```

### 6.3 캡처 제안

의사결정 또는 캡처 가치가 감지되면 AI에게 캡처를 제안한다:

```typescript
// decision_detected && capture_worthy인 경우:
`[CAPTURE] Decision detected - consider session(action="capture", event_type="decision", title="...")`

// capture_worthy만인 경우:
`[CAPTURE] Consider capturing this as insight`
```

이 힌트들은 context 응답의 `semanticHints` 영역에 포함된다.

---

## 7. 동적 지시 주입

### 7.1 서버 제공 instructions

SmartRouter가 사용자 메시지의 의미에 따라 동적 지시사항을 반환할 수 있다. `result.instructions` 필드가 존재하면 다음과 같이 주입한다:

```typescript
const instructionsLine = result.instructions ? `\n\n[INSTRUCTIONS] ${result.instructions}` : "";
```

이 지시는 workspace/project 수준에서 설정된 규칙이나, SmartRouter가 현재 대화 맥락에 맞게 생성한 행동 지침일 수 있다.

### 7.2 규칙 업데이트 경고

프로젝트의 에디터 규칙 파일(.cursorrules, CLAUDE.md 등)이 최신 버전이 아니면 경고를 주입한다:

```
[RULES_NOTICE] Rules 0.4.50 -> 0.4.57. Run generate_rules(overwrite_existing=true) to update.
```

또는 규칙 파일이 없는 경우:

```
[RULES_NOTICE] Rules missing. Run generate_rules() to install.
```

### 7.3 버전 업데이트 경고

MCP 서버 버전이 최신이 아닌 경우:

```
[VERSION_NOTICE] MCP Server Update Available!
Version: 0.4.50 -> 0.4.57
IMPORTANT: You MUST tell the user about this update IMMEDIATELY.
Update command: `npm update -g @contextstream/mcp-server`
```

### 7.4 suggested rules 알림

ML 기반으로 감지된 반복 패턴에서 규칙 제안이 있으면:

```
[SUGGESTED_RULES] ContextStream detected recurring patterns and generated rule suggestions.
1. [category] instruction text (confidence: 85%, seen 12x)
   Keywords: keyword1, keyword2
   Rule ID: uuid
```

사용자에게 표시하고 `session(action="suggested_rule_action", rule_id="...", rule_action="accept/reject")` 으로 처리하도록 안내한다.

---

## 8. 출력 구조

### 8.1 전체 출력 조립

`context` 도구의 최종 출력은 다음 요소들을 순서대로 조합한다:

```
[1] postCompactContext       -- post-compaction 복원 데이터 (해당 시)
[2] result.context           -- 핵심 컨텍스트 데이터 (minified/readable/structured)
[3] footer                   -- 소스 수, 토큰 추정치, 포맷 정보
[4] serverWarningsLine       -- 서버 제공 경고 (OR lessonsWarningLine)
[5] rulesWarningLine         -- 규칙 업데이트 경고
[6] versionWarningLine       -- 버전 업데이트 경고
[7] suggestedRulesLine       -- ML 규칙 제안
[8] contextPressureWarning   -- 컨텍스트 압력 경고
[9] semanticHints            -- 시맨틱 인텐트 힌트 (risk, capture)
[10] instructionsLine        -- 동적 지시사항
[11] contextRulesLine        -- CONTEXT_CALL_REMINDER
[12] searchRulesLine         -- SEARCH_RULES_REMINDER
[13] fullDataSection         -- "--- Full Response Data ---" + 전체 JSON
```

### 8.2 footer 형식

```typescript
const footer = `\n---\n(target icon) ${result.sources_used} sources | ~${result.token_estimate} tokens | format: ${result.format}${timingStr}`;
```

예시: `--- (target icon) 5 sources | ~187 tokens | format: minified | 234ms`

### 8.3 경고 우선순위

서버 제공 경고(`result.warnings`)가 있으면 클라이언트 측 레슨 감지(`lessonsWarningLine`)보다 우선한다:

```typescript
const allWarnings = [
  serverWarningsLine || lessonsWarningLine,  // 서버 OR 클라이언트 폴백
  rulesWarningLine,
  versionWarningLine,
  suggestedRulesLine,
  contextPressureWarning,
  semanticHints,
  instructionsLine,
  contextRulesLine,
  searchRulesLine,
].filter(Boolean).join("");
```

### 8.4 remember items 주입

서버 응답에 `remember_items`(사용자가 명시적으로 저장한 선호사항)가 있으면 별도 블록으로 주입:

```
USER PREFERENCES - MUST FOLLOW
These are user-specified preferences that MUST be checked and followed.
IMPORTANT: Always verify your actions align with these preferences.

1. (importance badge) preference content (max 150 chars)
```

---

## 9. 레슨 시스템

### 9.1 capture_lesson -- 레슨 캡처

`session(action="capture_lesson")` 액션으로 과거 실수에서 학습한 레슨을 저장한다.

**필수 파라미터:**
- `title`: 레슨 제목
- `trigger`: 문제를 유발한 원인
- `impact`: 무엇이 잘못되었는지
- `prevention`: 미래에 어떻게 방지할지

**선택 파라미터:**
- `category`: "workflow" | "code_quality" | "verification" | "communication" | "project_specific"
- `severity`: "low" | "medium" | "high" | "critical" (기본: "medium")
- `keywords`: 매칭에 사용될 키워드 배열

**저장 형식:**

capture_lesson은 내부적으로 markdown 형식의 content를 구성하여 `captureContext()`로 저장한다:

```markdown
## {title}
**Severity:** {severity}
**Category:** {category}
### Trigger
{trigger}
### Impact
{impact}
### Prevention
{prevention}
```

이벤트 타입은 `"lesson"`, importance는 severity에 따라 매핑된다:
- critical -> critical
- high -> high
- 그 외 -> medium

### 9.2 2분 중복 방지(dedup)

동일한 레슨이 반복 캡처되는 것을 방지하기 위해 2분(120초) 윈도우 내의 중복을 감지한다:

```typescript
const LESSON_DEDUP_WINDOW_MS = 2 * 60 * 1000;  // 2분
const recentLessonCaptures = new Map<string, number>();  // signature -> timestamp
```

`buildLessonSignature()` 함수는 다음 필드를 파이프(`|`)로 연결한 서명을 생성한다:
- workspaceId
- projectId (없으면 "global")
- category
- title
- trigger
- impact
- prevention

각 필드는 `normalizeLessonField()`로 정규화(소문자 변환, 공백 정리 등)된다.

`isDuplicateLessonCapture(signature)` 함수가 호출되면:
1. 만료된 항목(2분 초과) 정리
2. 동일 서명이 2분 이내에 존재하면 `true` 반환 (중복)
3. 존재하지 않으면 현재 타임스탬프를 기록하고 `false` 반환

중복 감지 시 반환:

```json
{ "deduplicated": true, "message": "Lesson already captured recently" }
```

### 9.3 get_lessons -- 레슨 조회

`session(action="get_lessons")` 액션은 `client.getHighPriorityLessons()`를 호출한다.

`query` 파라미터가 제공되면 `context_hint`로 전달되어 관련성 높은 레슨을 우선 반환한다. 결과가 0건이면 `getEmptyStateHint("get_lessons")`로 빈 상태 안내를 제공한다.

### 9.4 사전 주입 흐름 (proactive injection)

전체적인 레슨 사전 주입 흐름:

```
[사용자 메시지 수신]
     |
[context(user_message="...") 호출]
     |
[detectRiskyActions(userMessage)] -- 위험 키워드 탐지
     |
     +-- 키워드 발견 시 -->
     |   [client.getHighPriorityLessons(context_hint=keywords)]
     |        |
     |        +-- 레슨 발견 --> [LESSONS_WARNING] 블록 생성
     |        +-- 0건 --> 폴백 감지 (컨텍스트 내 L: 존재 확인)
     |
     +-- 키워드 미발견 시 -->
         [컨텍스트 내 레슨 키워드 확인]
              |
              +-- 존재 --> 간략 경고
              +-- 미존재 --> 레슨 경고 없음
     |
[context 응답 조립] -- lessonsWarningLine 포함
```

---

## 10. 부가 메커니즘

### 10.1 토큰 절감 추적

`context` 도구 호출마다 `trackToolTokenSavings()`가 fire-and-forget으로 호출된다. 이 함수는 전체 컨텍스트 대비 실제 전달된 토큰의 절감량을 서버에 보고한다:

```typescript
trackToolTokenSavings(client, "context_smart", result.context, {
  workspace_id: workspaceId,
  project_id: projectId,
  max_tokens: input.max_tokens,
});
```

### 10.2 변경 파일 자동 인덱싱

`context` 도구 핸들러 시작 시 `client.checkAndIndexChangedFiles()` 를 fire-and-forget으로 호출한다. 이는 에디터 훅이 없는 환경에서 변경된 파일을 자동으로 재인덱싱하는 폴백 메커니즘이다.

### 10.3 context_feedback 도구

`context_feedback` 도구는 `context` 도구가 반환한 개별 항목에 대해 관련성 피드백을 제출한다:

- `item_id`: context가 반환한 항목 ID
- `item_type`: "memory_event" | "knowledge_node" | "code_chunk"
- `feedback_type`: "relevant" | "irrelevant" | "pin"
- `query_text`: 원래 쿼리 (선택)

이 피드백은 서버 측의 검색 랭킹 최적화에 활용된다.

### 10.4 warnIfContextSmartNotCalled()

SessionManager의 `warnIfContextSmartNotCalled(toolName)` 메서드는 세션이 초기화되었지만 `context`가 아직 한 번도 호출되지 않은 상태에서 다른 도구가 실행될 때 경고를 출력한다:

```
[ContextStream] Warning: search called without context_smart.
[ContextStream] For best results, call context_smart(user_message="...") before other tools.
```

예외 도구: `session_init`, `context_smart`, `session_recall`, `session_remember`는 경고를 건너뛴다. 경고는 세션당 1회만 표시된다.

### 10.5 Continuous Checkpointing

SessionManager는 도구 호출을 지속적으로 추적하며, N회(기본 20회) 호출마다 주기적으로 체크포인트를 저장한다:

```typescript
private checkpointInterval = 20;
private checkpointEnabled = process.env.CONTEXTSTREAM_CHECKPOINT_ENABLED?.toLowerCase() === "true";
```

체크포인트 데이터에는 도구 호출 수, 세션 토큰, 활성 파일 목록(최대 30개), 최근 도구 이름(최대 10개)이 포함된다. `"periodic"`, `"milestone"`, `"manual"` 3가지 트리거 유형을 지원한다.

---

## 11. 환경 변수 정리

| 환경 변수 | 용도 | 기본값 |
|-----------|------|--------|
| `CONTEXTSTREAM_SEARCH_REMINDER` | context 응답에 검색 규칙 리마인더 포함 | true |
| `CONTEXTSTREAM_CHECKPOINT_ENABLED` | 연속 체크포인팅 활성화 | false |
| `CONTEXTSTREAM_SHOW_TIMING` | 응답 시간 표시 | false |
| `CONTEXTSTREAM_LOG_LEVEL` | 로그 수준 (quiet/normal/verbose) | normal |
| `MCP_CONTEXT_SMART_TIMEOUT_SECS` | context API 타임아웃 | 45초 (5-55 범위) |
