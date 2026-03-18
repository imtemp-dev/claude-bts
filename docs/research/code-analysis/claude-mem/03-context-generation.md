# claude-mem -- 컨텍스트 생성 분석

## 1. 컨텍스트 생성 아키텍처

### 1.1 전체 파이프라인 개요

claude-mem의 컨텍스트 생성 시스템은 SQLite 데이터베이스에 저장된 observations과 session summaries를 조회하여, Claude Code 세션 시작 시 주입되는 포맷된 컨텍스트 문자열을 생성한다. 파이프라인은 다음과 같다:

```
generateContext(input, useColors)
  1. loadContextConfig()          -- 설정 로드
  2. getProjectName(cwd)          -- 프로젝트명 추출
  3. initializeDatabase()         -- SQLite 연결
  4. queryObservations(db, ...)   -- 관측 데이터 조회
  5. querySummaries(db, ...)      -- 세션 요약 조회
  6. calculateTokenEconomics()    -- 토큰 경제성 계산
  7. renderHeader()               -- 헤더 섹션 렌더링
  8. buildTimeline()              -- 타임라인 구성
  9. renderTimeline()             -- 타임라인 렌더링
  10. renderSummaryFields()       -- 최근 요약 렌더링
  11. renderPreviouslySection()   -- 이전 어시스턴트 메시지
  12. renderFooter()              -- 풋터 렌더링
  -> output.join('\n').trimEnd()
```

### 1.2 진입점

**주 진입점:** `src/services/context/ContextBuilder.ts`의 `generateContext()` 함수

```typescript
export async function generateContext(
  input?: ContextInput,
  useColors: boolean = false
): Promise<string>
```

**호출 경로:**
- `src/services/context-generator.ts` -- 하위 호환 re-export (DEPRECATED 주석)
- `src/services/Context.ts` -- 명명 re-export facade
- `src/services/context/index.ts` -- 모듈 공개 API
- SearchRoutes의 `handleContextPreview` 및 `handleContextInject` 핸들러에서 동적 import

### 1.3 이중 포맷 지원

모든 렌더링 함수는 `useColors: boolean` 파라미터를 받아, 두 가지 출력 형식을 지원한다:

- `useColors = false`: Markdown 포맷 (MCP 도구, 파일 저장용)
- `useColors = true`: ANSI 색상 코드 포함 터미널 출력 (context injection hook용)

### 1.4 Worktree 지원

단일 프로젝트뿐 아니라 여러 프로젝트의 데이터를 통합 조회할 수 있다:

```typescript
const projects = input?.projects || [project];
const observations = projects.length > 1
  ? queryObservationsMulti(db, projects, config)
  : queryObservations(db, project, config);
```

Git worktree 사용 시 부모 저장소와 worktree 양쪽의 observations/summaries를 시간순으로 인터리빙하여 통합 타임라인을 생성한다.

---

## 2. ContextBuilder -- 컨텍스트 빌더 (170L)

### 2.1 구조

ContextBuilder.ts는 클래스가 아닌 **함수 기반** 모듈이다. `generateContext()` 단일 async 함수가 전체 파이프라인을 오케스트레이션한다.

### 2.2 데이터베이스 초기화

```typescript
function initializeDatabase(): SessionStore | null {
  try {
    return new SessionStore();
  } catch (error: any) {
    if (error.code === 'ERR_DLOPEN_FAILED') {
      // Native module 오류 시 버전 마커 파일 삭제 후 null 반환
      unlinkSync(VERSION_MARKER_PATH);
      return null;
    }
    throw error;
  }
}
```

`ERR_DLOPEN_FAILED`는 better-sqlite3 네이티브 모듈이 현재 Node.js 버전과 호환되지 않을 때 발생한다. 이 경우 `~/.claude/plugins/marketplaces/thedotmack/plugin/.install-version` 마커 파일을 삭제하여 다음 시작 시 자동 재빌드를 유도한다. null 반환 시 빈 문자열이 출력된다.

### 2.3 빈 상태 처리

```typescript
if (observations.length === 0 && summaries.length === 0) {
  return renderEmptyState(project, useColors);
}
```

데이터가 없으면 프로젝트명과 현재 시각만 포함하는 간단한 메시지를 반환한다.

### 2.4 buildContextOutput -- 핵심 빌드 함수

```typescript
function buildContextOutput(
  project, observations, summaries, config, cwd, sessionId, useColors
): string
```

이 함수가 실제 컨텍스트 문자열 조립을 수행한다:

1. **토큰 경제성 계산:** `calculateTokenEconomics(observations)` -- 전체 관측의 read/discovery 토큰 집계
2. **헤더 렌더링:** `renderHeader(project, economics, config, useColors)` -- 프로젝트명, 범례, 컬럼 키, 컨텍스트 인덱스 안내, 토큰 경제성
3. **타임라인 준비:**
   - `displaySummaries = summaries.slice(0, config.sessionCount)` -- 표시할 요약 수 제한
   - `prepareSummariesForTimeline(displaySummaries, summaries)` -- 요약에 displayEpoch 할당
   - `buildTimeline(observations, summariesForTimeline)` -- 시간순 정렬
   - `getFullObservationIds(observations, config.fullObservationCount)` -- 전체 표시할 관측 ID 선정
4. **타임라인 렌더링:** `renderTimeline(timeline, fullObservationIds, config, cwd, useColors)`
5. **요약 표시:** `shouldShowSummary()` 조건 충족 시 `renderSummaryFields()`
6. **이전 메시지:** `getPriorSessionMessages()` + `renderPreviouslySection()`
7. **풋터:** `renderFooter(economics, config, useColors)`

최종 출력은 `output.join('\n').trimEnd()`로 조합된다.

---

## 3. ObservationCompiler -- 관측 컴파일러 (262L)

### 3.1 역할

ObservationCompiler는 SQLite 데이터베이스에서 observations과 summaries를 조회하고, 타임라인 구성 및 트랜스크립트 추출을 담당하는 데이터 계층이다.

### 3.2 queryObservations -- 관측 조회

```typescript
export function queryObservations(
  db: SessionStore, project: string, config: ContextConfig
): Observation[]
```

SQLite 직접 쿼리:
```sql
SELECT id, memory_session_id, type, title, subtitle, narrative,
       facts, concepts, files_read, files_modified, discovery_tokens,
       created_at, created_at_epoch
FROM observations
WHERE project = ?
  AND type IN (?, ?, ...)          -- config.observationTypes
  AND EXISTS (
    SELECT 1 FROM json_each(concepts)
    WHERE value IN (?, ?, ...)     -- config.observationConcepts
  )
ORDER BY created_at_epoch DESC
LIMIT ?                            -- config.totalObservationCount
```

**필터링 로직:**
- `project` 필터: 정확 매칭
- `type IN (...)`: ModeManager에서 정의한 활성 모드의 관측 타입만 포함
- `EXISTS (SELECT 1 FROM json_each(concepts) WHERE value IN (...))`: concepts JSON 배열 내에 활성 모드의 관측 개념이 하나라도 포함된 행만 선택. SQLite의 `json_each()` 테이블 값 함수를 사용하여 JSON 배열을 행으로 전개한다.
- `ORDER BY created_at_epoch DESC`: 최신순
- `LIMIT`: `config.totalObservationCount`

### 3.3 queryObservationsMulti -- 다중 프로젝트 관측 조회

```typescript
export function queryObservationsMulti(
  db: SessionStore, projects: string[], config: ContextConfig
): Observation[]
```

`WHERE project IN (?, ?, ...)` 절로 여러 프로젝트를 동시 조회한다. SELECT에 `project` 컬럼이 추가된다. 결과는 시간순으로 인터리빙되어 반환된다.

### 3.4 querySummaries -- 요약 조회

```typescript
export function querySummaries(
  db: SessionStore, project: string, config: ContextConfig
): SessionSummary[]
```

```sql
SELECT id, memory_session_id, request, investigated, learned,
       completed, next_steps, created_at, created_at_epoch
FROM session_summaries
WHERE project = ?
ORDER BY created_at_epoch DESC
LIMIT ?    -- config.sessionCount + SUMMARY_LOOKAHEAD (= +1)
```

`SUMMARY_LOOKAHEAD = 1`로, 설정된 세션 수보다 1개 더 조회한다. 이 추가 요약은 `prepareSummariesForTimeline()`에서 표시 시점(displayEpoch) 계산에 사용된다.

### 3.5 querySummariesMulti -- 다중 프로젝트 요약 조회

queryObservationsMulti와 동일한 패턴으로 `WHERE project IN (...)` 절을 사용한다.

### 3.6 prepareSummariesForTimeline -- 타임라인용 요약 준비

```typescript
export function prepareSummariesForTimeline(
  displaySummaries: SessionSummary[],
  allSummaries: SessionSummary[]
): SummaryTimelineItem[]
```

각 요약에 타임라인 표시 정보를 추가한다:

- `displayEpoch`: 첫 번째 요약이 아닌 경우, **한 단계 이전 요약의 epoch**를 사용. 이렇게 하면 요약이 해당 세션의 관측들 앞에 위치하게 된다.
- `displayTime`: displayEpoch에 대응하는 시간 문자열
- `shouldShowLink`: 가장 최근 요약이 아닌 경우 true (링크 표시 여부)

**설계 의도:** 세션 요약은 세션이 끝날 때 생성되므로 created_at이 해당 세션의 마지막이지만, 타임라인에서는 세션의 시작 시점에 표시하는 것이 자연스럽다. 이전 요약의 timestamp를 대리 값으로 사용하여 이를 구현한다.

### 3.7 buildTimeline -- 통합 타임라인 구성

```typescript
export function buildTimeline(
  observations: Observation[],
  summaries: SummaryTimelineItem[]
): TimelineItem[]
```

observations(`created_at_epoch`)와 summaries(`displayEpoch`)를 하나의 배열로 병합하고, epoch 기준 오름차순(시간순)으로 정렬한다. 타입 태깅으로 `'observation'` 또는 `'summary'`를 구분한다.

### 3.8 getFullObservationIds -- 전체 표시 관측 선정

```typescript
export function getFullObservationIds(
  observations: Observation[], count: number
): Set<number>
```

가장 최근 `count`개 관측의 ID를 Set으로 반환한다. 이 관측들은 타임라인에서 제목만이 아닌 narrative 또는 facts 전문이 표시된다.

### 3.9 extractPriorMessages -- 트랜스크립트 추출

```typescript
export function extractPriorMessages(transcriptPath: string): PriorMessages
```

Claude Code의 JSONL 트랜스크립트 파일에서 마지막 어시스턴트 메시지를 추출한다:

1. 파일 존재 여부 확인
2. 줄 단위로 역순 탐색
3. `"type":"assistant"` 포함 줄 찾기 (빠른 사전 필터)
4. JSON 파싱 후 `message.content` 배열에서 text 블록 추출
5. `<system-reminder>` 태그 제거
6. 첫 유효 어시스턴트 메시지 반환

### 3.10 getPriorSessionMessages -- 이전 세션 메시지 조회

```typescript
export function getPriorSessionMessages(
  observations, config, currentSessionId, cwd
): PriorMessages
```

조건:
- `config.showLastMessage`가 true여야 함
- observations가 있어야 함
- 현재 세션이 아닌 이전 세션의 관측이 있어야 함

이전 세션의 `memory_session_id`를 사용하여 트랜스크립트 파일 경로를 구성한다:
```
~/.claude/projects/{dashedCwd}/{priorSessionId}.jsonl
```

여기서 `dashedCwd`는 cwd의 `/`를 `-`로 치환한 형태이다. `CLAUDE_CONFIG_DIR` 환경 변수로 커스텀 설정 디렉토리를 지원한다.

---

## 4. TokenCalculator -- 토큰 계산기 (78L)

### 4.1 역할

TokenCalculator는 관측의 토큰 수를 추정하고, 컨텍스트 경제성(read tokens vs discovery tokens) 지표를 계산한다.

### 4.2 calculateObservationTokens -- 개별 관측 토큰 추정

```typescript
export function calculateObservationTokens(obs: Observation): number {
  const obsSize = (obs.title?.length || 0) +
                  (obs.subtitle?.length || 0) +
                  (obs.narrative?.length || 0) +
                  JSON.stringify(obs.facts || []).length;
  return Math.ceil(obsSize / CHARS_PER_TOKEN_ESTIMATE);  // CHARS_PER_TOKEN_ESTIMATE = 4
}
```

4문자를 1토큰으로 근사한다. title, subtitle, narrative, facts(JSON 직렬화 포함)의 총 문자 수를 기반으로 한다.

### 4.3 calculateTokenEconomics -- 토큰 경제성 계산

```typescript
export function calculateTokenEconomics(observations: Observation[]): TokenEconomics
```

**계산 항목:**

| 지표 | 계산 방법 |
|------|-----------|
| `totalObservations` | observations 배열 길이 |
| `totalReadTokens` | 모든 관측의 `calculateObservationTokens()` 합계 |
| `totalDiscoveryTokens` | 모든 관측의 `discovery_tokens` 합계 |
| `savings` | `totalDiscoveryTokens - totalReadTokens` |
| `savingsPercent` | `Math.round((savings / totalDiscoveryTokens) * 100)` |

**경제성 모델:** "이 관측을 처음 발견하는 데 X 토큰을 소비했지만, 압축된 형태로 읽으면 Y 토큰만 필요하다. Z 토큰(P%)을 절약한다"는 ROI 메시지를 전달한다.

### 4.4 formatObservationTokenDisplay -- 개별 관측 토큰 표시

```typescript
export function formatObservationTokenDisplay(
  obs: Observation, config: ContextConfig
): { readTokens, discoveryTokens, discoveryDisplay, workEmoji }
```

- `readTokens`: 읽기 비용 (추정)
- `discoveryTokens`: `obs.discovery_tokens || 0` -- 원래 발견에 소비된 토큰
- `workEmoji`: `ModeManager.getInstance().getWorkEmoji(obs.type)` -- 타입별 작업 이모지
- `discoveryDisplay`: `discoveryTokens > 0 ? "${workEmoji} ${discoveryTokens.toLocaleString()}" : '-'`

### 4.5 shouldShowContextEconomics -- 경제성 표시 여부

```typescript
export function shouldShowContextEconomics(config: ContextConfig): boolean {
  return config.showReadTokens || config.showWorkTokens ||
         config.showSavingsAmount || config.showSavingsPercent;
}
```

4개 설정 중 하나라도 true이면 경제성 섹션을 표시한다.

---

## 5. 설정 로더 -- ContextConfigLoader.ts

### 5.1 설정 소스

설정은 3단계 우선순위로 로드된다:

```
~/.claude-mem/settings.json > 환경 변수 > 기본값
```

`SettingsDefaultsManager.loadFromFile(settingsPath)`가 이 우선순위를 처리한다.

### 5.2 loadContextConfig 반환 구조

```typescript
export function loadContextConfig(): ContextConfig
```

| 설정 키 | 타입 | 설명 |
|---------|------|------|
| `totalObservationCount` | number | 컨텍스트에 포함할 총 관측 수 |
| `fullObservationCount` | number | narrative/facts 전문을 표시할 최근 관측 수 |
| `sessionCount` | number | 표시할 세션 요약 수 |
| `showReadTokens` | boolean | Read 토큰 컬럼 표시 여부 |
| `showWorkTokens` | boolean | Work 토큰 컬럼 표시 여부 |
| `showSavingsAmount` | boolean | 절약 토큰 수 표시 여부 |
| `showSavingsPercent` | boolean | 절약 백분율 표시 여부 |
| `observationTypes` | Set\<string\> | 활성 모드의 관측 타입 집합 |
| `observationConcepts` | Set\<string\> | 활성 모드의 관측 개념 집합 |
| `fullObservationField` | 'narrative' \| 'facts' | 전문 표시 시 사용할 필드 |
| `showLastSummary` | boolean | 최근 세션 요약 표시 여부 |
| `showLastMessage` | boolean | 이전 어시스턴트 메시지 표시 여부 |

### 5.3 모드 기반 필터링

`observationTypes`와 `observationConcepts`는 설정 파일이 아닌 **ModeManager의 활성 모드 정의**에서 읽어온다:

```typescript
const mode = ModeManager.getInstance().getActiveMode();
const observationTypes = new Set(mode.observation_types.map(t => t.id));
const observationConcepts = new Set(mode.observation_concepts.map(c => c.id));
```

이를 통해 모드를 전환하면 컨텍스트에 포함되는 관측의 종류가 자동으로 변경된다.

### 5.4 환경 변수 매핑

설정 파일의 키는 환경 변수와 동일한 이름을 사용한다:

- `CLAUDE_MEM_CONTEXT_OBSERVATIONS` -> `totalObservationCount`
- `CLAUDE_MEM_CONTEXT_FULL_COUNT` -> `fullObservationCount`
- `CLAUDE_MEM_CONTEXT_SESSION_COUNT` -> `sessionCount`
- `CLAUDE_MEM_CONTEXT_SHOW_READ_TOKENS` -> `showReadTokens`
- `CLAUDE_MEM_CONTEXT_SHOW_WORK_TOKENS` -> `showWorkTokens`
- `CLAUDE_MEM_CONTEXT_SHOW_SAVINGS_AMOUNT` -> `showSavingsAmount`
- `CLAUDE_MEM_CONTEXT_SHOW_SAVINGS_PERCENT` -> `showSavingsPercent`
- `CLAUDE_MEM_CONTEXT_FULL_FIELD` -> `fullObservationField`
- `CLAUDE_MEM_CONTEXT_SHOW_LAST_SUMMARY` -> `showLastSummary`
- `CLAUDE_MEM_CONTEXT_SHOW_LAST_MESSAGE` -> `showLastMessage`

---

## 6. 섹션 렌더러

### 6.1 HeaderRenderer.ts

`renderHeader()` 함수는 5개 하위 섹션을 순서대로 렌더링한다:

1. **메인 헤더:** 프로젝트명 + 현재 시각
2. **범례 (Legend):** 관측 타입별 이모지 + 이름 (ModeManager에서 조회)
3. **컬럼 키 (Column Key):** Read/Work 컬럼의 의미 설명
4. **컨텍스트 인덱스 안내:** 사용 방법 가이드
5. **토큰 경제성:** `shouldShowContextEconomics(config)` 조건부 표시

각 하위 섹션은 `useColors`에 따라 `Color.*` 또는 `Markdown.*` 함수를 호출한다.

### 6.2 SummaryRenderer.ts

**shouldShowSummary 판단 로직:**

```typescript
export function shouldShowSummary(
  config, mostRecentSummary, mostRecentObservation
): boolean
```

3가지 조건을 모두 충족해야 표시된다:
1. `config.showLastSummary`가 true
2. `mostRecentSummary`가 존재하고, investigated/learned/completed/next_steps 중 하나라도 값이 있음
3. mostRecentSummary의 `created_at_epoch`가 mostRecentObservation의 `created_at_epoch`보다 큼 (요약이 관측보다 최신)

**설계 의도:** 요약은 세션이 끝날 때 생성된다. 만약 가장 최근 관측이 요약보다 새로우면, 현재 진행 중인 세션에서 새 관측이 생성된 것이므로 오래된 요약을 표시하지 않는다.

**renderSummaryFields:**

4개 필드를 순서대로 렌더링한다:
- Investigated (blue)
- Learned (yellow)
- Completed (green)
- Next Steps (magenta)

각 필드는 값이 있을 때만 표시된다.

### 6.3 TimelineRenderer.ts (170L)

**groupTimelineByDay:**

```typescript
export function groupTimelineByDay(timeline: TimelineItem[]): Map<string, TimelineItem[]>
```

타임라인 항목을 날짜별로 그룹핑한다. observation은 `created_at`, summary는 `displayTime`에서 날짜를 추출한다. 결과는 시간순으로 정렬된다.

**renderDayTimeline -- 일별 타임라인 렌더링:**

하나의 날짜에 속하는 항목들을 렌더링한다. 핵심 로직:

1. **Summary 항목:** 열린 테이블이 있으면 닫고, 독립 블록으로 렌더링. `renderColorSummaryItem` 또는 `renderMarkdownSummaryItem` 호출.

2. **Observation 항목:**
   - 파일이 이전과 다르면: 이전 테이블 닫기 -> 새 파일 헤더 + 테이블 헤더
   - `fullObservationIds`에 포함되면: **전체 표시** (narrative 또는 facts 포함)
   - 그렇지 않으면: **테이블 행** (제목만)

3. **시간 중복 처리:** 같은 시간의 연속 행은 시간 표시를 생략한다 (Markdown에서는 빈 문자열, Color에서는 같은 너비의 공백).

**getDetailField:**

```typescript
function getDetailField(obs: Observation, config: ContextConfig): string | null {
  if (config.fullObservationField === 'narrative') return obs.narrative;
  return obs.facts ? parseJsonArray(obs.facts).join('\n') : null;
}
```

`fullObservationField` 설정에 따라 narrative 또는 facts 배열(줄바꿈으로 결합)을 반환한다.

**renderTimeline -- 전체 타임라인 렌더링:**

```typescript
export function renderTimeline(
  timeline, fullObservationIds, config, cwd, useColors
): string[]
```

`groupTimelineByDay()`로 그룹핑 후, 각 날짜에 대해 `renderDayTimeline()`을 호출한다.

### 6.4 FooterRenderer.ts

**renderPreviouslySection:**

이전 어시스턴트 메시지가 있으면 "Previously" 섹션을 렌더링한다:

- Markdown: `**Previously**` + `A: {message}`
- Color: magenta 볼드 "Previously" + dim 텍스트

**renderFooter:**

토큰 절약 정보를 표시한다. 조건: `shouldShowContextEconomics(config)` + discovery tokens > 0 + savings > 0.

```
Access Nk tokens of past research & decisions for just Mt. Use the claude-mem skill to access memories by ID.
```

여기서 N은 `Math.round(totalDiscoveryTokens / 1000)`, M은 `totalReadTokens.toLocaleString()`이다.

---

## 7. Markdown 포맷터 -- MarkdownFormatter.ts (241L)

### 7.1 역할

MarkdownFormatter는 `useColors = false`일 때 사용되는 포맷터로, 순수 Markdown 구문으로 컨텍스트를 생성한다. MCP 도구를 통한 텍스트 응답이나 파일 저장에 적합하다.

### 7.2 헤더 포맷

```typescript
export function renderMarkdownHeader(project: string): string[] {
  return [`# [${project}] recent context, ${formatHeaderDateTime()}`, ''];
}
```

날짜 형식: `YYYY-MM-DD h:mmam/pm TZ` (예: `2025-12-14 7:30pm PST`)

### 7.3 범례

```typescript
export function renderMarkdownLegend(): string[] {
  const typeLegendItems = mode.observation_types.map(t => `${t.emoji} ${t.id}`).join(' | ');
  return [`**Legend:** session-request | ${typeLegendItems}`, ''];
}
```

ModeManager의 활성 모드에서 타입 목록을 동적으로 구성한다.

### 7.4 컬럼 키

```
**Column Key**:
- **Read**: Tokens to read this observation (cost to learn it now)
- **Work**: Tokens spent on work that produced this record (research, building, deciding)
```

### 7.5 컨텍스트 인덱스 안내

LLM에게 컨텍스트 사용 방법을 지시하는 텍스트:

```
**Context Index:** This semantic index (titles, types, files, tokens) is usually sufficient...
When you need implementation details, rationale, or debugging context:
- Fetch by ID: get_observations([IDs]) for observations visible in this index
- Search history: Use the mem-search skill for past decisions, bugs, and deeper research
- Trust this index over re-reading code for past decisions and learnings
```

### 7.6 토큰 경제성

```typescript
export function renderMarkdownContextEconomics(economics, config): string[]
```

```
**Context Economics**:
- Loading: N observations (M tokens to read)
- Work investment: K tokens spent on research, building, and decisions
- Your savings: S tokens (P% reduction from reuse)
```

savings 줄은 `config.showSavingsAmount`와 `config.showSavingsPercent` 설정에 따라 3가지 변형이 있다.

### 7.7 테이블 구조

**파일 헤더 + 테이블:**
```
**src/services/context/ContextBuilder.ts**
| ID | Time | T | Title | Read | Work |
|----|------|---|-------|------|------|
```

**테이블 행:**
```typescript
export function renderMarkdownTableRow(obs, timeDisplay, config): string {
  return `| #${obs.id} | ${timeDisplay || '"'} | ${icon} | ${title} | ${readCol} | ${workCol} |`;
}
```

`readCol`과 `workCol`은 각각 `config.showReadTokens`와 `config.showWorkTokens` 설정에 따라 표시된다. 표시하지 않으면 빈 문자열이다.

### 7.8 전체 관측 표시

```typescript
export function renderMarkdownFullObservation(
  obs, timeDisplay, detailField, config
): string[]
```

테이블 행 대신 독립 블록으로 렌더링:
```
**#123** 7:30 PM icon **Title**

narrative or facts content here

Read: ~120, Work: emoji 5,000
```

### 7.9 요약 항목

```
**#S45** Session request text (Dec 14, 7:30 PM)
```

### 7.10 Previously 섹션

```
---

**Previously**

A: Last assistant message text here
```

### 7.11 풋터

```
Access Nk tokens of past research & decisions for just Mt. Use the claude-mem skill to access memories by ID.
```

### 7.12 빈 상태

```
# [project] recent context, YYYY-MM-DD h:mmam/pm TZ

No previous sessions found for this project yet.
```

---

## 8. Color 포맷터 -- ColorFormatter.ts (238L)

### 8.1 역할

ColorFormatter는 `useColors = true`일 때 사용되는 포맷터로, ANSI 이스케이프 코드를 사용하여 터미널에서 색상이 적용된 출력을 생성한다. context injection hook에서 터미널 표시용으로 사용된다.

### 8.2 ANSI 색상 코드

`types.ts`에 정의된 색상 상수:

```typescript
export const colors = {
  reset: '\x1b[0m',
  bright: '\x1b[1m',     // 볼드
  dim: '\x1b[2m',        // 흐린
  cyan: '\x1b[36m',
  green: '\x1b[32m',
  yellow: '\x1b[33m',
  blue: '\x1b[34m',
  magenta: '\x1b[35m',
  gray: '\x1b[90m',
  red: '\x1b[31m',
};
```

### 8.3 헤더

```typescript
export function renderColorHeader(project: string): string[] {
  return [
    '',
    `${colors.bright}${colors.cyan}[${project}] recent context, ${formatHeaderDateTime()}${colors.reset}`,
    `${colors.gray}${'─'.repeat(60)}${colors.reset}`,   // 60자 구분선 (U+2500 BOX DRAWINGS LIGHT HORIZONTAL)
    ''
  ];
}
```

빈 줄 -> cyan 볼드 헤더 -> gray 구분선 -> 빈 줄. Markdown 헤더와 달리 `#` 접두사가 없다.

### 8.4 범례

```typescript
`${colors.dim}Legend: session-request | ${typeLegendItems}${colors.reset}`
```

dim 처리로 시각적 중요도를 낮춘다.

### 8.5 테이블 행 포맷

Markdown과 달리 테이블 구문(|)을 사용하지 않고, 공백으로 정렬된 인라인 포맷:

```typescript
export function renderColorTableRow(obs, time, showTime, config): string {
  const timePart = showTime
    ? `${colors.dim}${time}${colors.reset}`
    : ' '.repeat(time.length);  // 시간이 같으면 동일 너비 공백
  return `  ${colors.dim}#${obs.id}${colors.reset}  ${timePart}  ${icon}  ${title} ${readPart} ${discoveryPart}`;
}
```

- ID: dim
- Time: dim (시간이 이전과 같으면 공백으로 대체)
- Icon: 이모지 (색상 코드 없음)
- Title: 일반 텍스트
- Read/Work: dim 괄호 형식 `(~120t)`, `(emoji 5,000t)`

### 8.6 전체 관측 표시

```typescript
export function renderColorFullObservation(obs, time, showTime, detailField, config): string[]
```

- 제목: bright (볼드)
- 상세 필드: dim 처리, 4칸 들여쓰기
- 토큰 정보: dim 처리, 4칸 들여쓰기

### 8.7 요약 항목

```typescript
`${colors.yellow}#S${summary.id}${colors.reset} ${summaryTitle}`
```

세션 ID가 yellow로 강조된다.

### 8.8 요약 필드

```typescript
export function renderColorSummaryField(label: string, value: string | null, color: string): string[] {
  if (!value) return [];
  return [`${color}${label}:${colors.reset} ${value}`, ''];
}
```

필드별 색상이 다르다:
- Investigated: blue
- Learned: yellow
- Completed: green
- Next Steps: magenta

### 8.9 Previously 섹션

```
---
[bright][magenta]Previously[reset]
[dim]A: message text[reset]
```

### 8.10 풋터

```typescript
`${colors.dim}Access Nk tokens of past research & decisions for just Mt. Use the claude-mem skill to access memories by ID.${colors.reset}`
```

전체가 dim 처리된다.

### 8.11 빈 상태

한 줄로 연결된 ANSI 출력: 빈 줄 + cyan 볼드 헤더 + gray 구분선 + 빈 줄 + dim 안내 메시지 + 빈 줄.

---

## 9. 타입 정의 -- types.ts (137L)

### 9.1 ContextInput

```typescript
export interface ContextInput {
  session_id?: string;              // 현재 Claude Code 세션 ID
  transcript_path?: string;         // 트랜스크립트 파일 경로
  cwd?: string;                     // 작업 디렉토리
  hook_event_name?: string;         // 훅 이벤트 이름
  source?: "startup" | "resume" | "clear" | "compact";  // 호출 출처
  projects?: string[];              // 다중 프로젝트 (worktree 지원)
  [key: string]: any;               // 확장 필드
}
```

`source` 필드의 의미:
- `"startup"`: 새 세션 시작
- `"resume"`: 세션 재개
- `"clear"`: 컨텍스트 클리어 후 재로드
- `"compact"`: 컨텍스트 압축 후 재로드

### 9.2 ContextConfig

```typescript
export interface ContextConfig {
  totalObservationCount: number;     // 관측 총 표시 수
  fullObservationCount: number;      // 전문 표시 관측 수
  sessionCount: number;              // 세션 요약 표시 수
  showReadTokens: boolean;           // Read 토큰 표시
  showWorkTokens: boolean;           // Work 토큰 표시
  showSavingsAmount: boolean;        // 절약 토큰 수 표시
  showSavingsPercent: boolean;       // 절약 백분율 표시
  observationTypes: Set<string>;     // 활성 관측 타입 필터
  observationConcepts: Set<string>;  // 활성 관측 개념 필터
  fullObservationField: 'narrative' | 'facts';  // 전문 표시 필드
  showLastSummary: boolean;          // 최근 요약 표시
  showLastMessage: boolean;          // 이전 메시지 표시
}
```

### 9.3 Observation

```typescript
export interface Observation {
  id: number;
  memory_session_id: string;
  type: string;                     // decision, bugfix, feature, refactor, discovery, change
  title: string | null;
  subtitle: string | null;
  narrative: string | null;         // 서술형 설명
  facts: string | null;             // JSON 배열 문자열
  concepts: string | null;          // JSON 배열 문자열
  files_read: string | null;        // JSON 배열 문자열
  files_modified: string | null;    // JSON 배열 문자열
  discovery_tokens: number | null;  // 발견에 소비된 토큰
  created_at: string;               // ISO 문자열
  created_at_epoch: number;         // epoch 밀리초
  project?: string;                 // 다중 프로젝트 쿼리에서만 포함
}
```

### 9.4 SessionSummary

```typescript
export interface SessionSummary {
  id: number;
  memory_session_id: string;
  request: string | null;          // 사용자 요청
  investigated: string | null;     // 조사한 내용
  learned: string | null;          // 배운 내용
  completed: string | null;        // 완료한 내용
  next_steps: string | null;       // 다음 단계
  created_at: string;
  created_at_epoch: number;
  project?: string;
}
```

### 9.5 SummaryTimelineItem

```typescript
export interface SummaryTimelineItem extends SessionSummary {
  displayEpoch: number;            // 타임라인 표시 시점 (이전 세션의 epoch)
  displayTime: string;             // 표시 시점의 시간 문자열
  shouldShowLink: boolean;         // 가장 최근 요약이 아니면 true
}
```

### 9.6 TimelineItem

```typescript
export type TimelineItem =
  | { type: 'observation'; data: Observation }
  | { type: 'summary'; data: SummaryTimelineItem };
```

discriminated union으로 타입 안전한 분기 처리를 가능하게 한다. 검색 시스템의 TimelineItem(`'observation' | 'session' | 'prompt'`)과는 다른 타입이다.

### 9.7 TokenEconomics

```typescript
export interface TokenEconomics {
  totalObservations: number;       // 전체 관측 수
  totalReadTokens: number;         // 총 읽기 토큰
  totalDiscoveryTokens: number;    // 총 발견 토큰
  savings: number;                 // 절약 토큰 (discovery - read)
  savingsPercent: number;          // 절약 백분율
}
```

### 9.8 PriorMessages

```typescript
export interface PriorMessages {
  userMessage: string;             // 사용되지 않음 (항상 빈 문자열)
  assistantMessage: string;        // 이전 세션의 마지막 어시스턴트 메시지
}
```

`userMessage`는 인터페이스에 정의되어 있지만, 실제로는 항상 빈 문자열로 설정된다. `extractPriorMessages()`에서 `userMessage: ''`를 반환한다.

### 9.9 상수

```typescript
export const CHARS_PER_TOKEN_ESTIMATE = 4;   // 토큰 추정 비율 (4자 = 1토큰)
export const SUMMARY_LOOKAHEAD = 1;          // displayEpoch 계산을 위한 추가 요약 수
```

### 9.10 ANSI 색상 상수

```typescript
export const colors = {
  reset: '\x1b[0m',
  bright: '\x1b[1m',
  dim: '\x1b[2m',
  cyan: '\x1b[36m',
  green: '\x1b[32m',
  yellow: '\x1b[33m',
  blue: '\x1b[34m',
  magenta: '\x1b[35m',
  gray: '\x1b[90m',
  red: '\x1b[31m',
};
```

---

## 10. 토큰 예산 관리

### 10.1 예산 모델 개요

claude-mem의 토큰 예산 관리는 **하드 예산 할당 방식이 아닌 관측 수 제한 방식**이다. 토큰 수를 직접 제한하지 않고, `totalObservationCount`와 `fullObservationCount` 설정으로 포함할 관측의 수를 제한한다. 결과적으로 토큰 소비량은 관측의 평균 크기에 비례하여 간접적으로 제어된다.

### 10.2 섹션별 토큰 분배

컨텍스트의 각 섹션이 차지하는 토큰 비율은 고정 할당이 아니라 데이터에 의해 결정된다:

**고정 비용 섹션 (데이터와 무관):**
- Header: 프로젝트명 + 시간 (~10 토큰)
- Legend: 타입 목록 (~20-30 토큰)
- Column Key: 설명 텍스트 (~40 토큰)
- Context Index: 사용 안내 (~60 토큰)
- Footer: 절약 메시지 (~30 토큰)

**가변 비용 섹션 (데이터 의존):**
- Context Economics: 토큰 수치 (~30-50 토큰)
- Timeline observations (요약 행): 관측 수 x ~15-20 토큰/행
- Timeline observations (전체 표시): `fullObservationCount`개 x (narrative 또는 facts 크기)
- Session summaries: `sessionCount`개 x ~20-30 토큰/요약
- Summary fields: investigated + learned + completed + next_steps 총 크기
- Previously section: 이전 어시스턴트 메시지 전체 크기

### 10.3 관측 수 제한 메커니즘

```
totalObservationCount (설정값)
  |
  v
queryObservations() -- SQL LIMIT 절로 최신 N개 제한
  |
  v
fullObservationCount (설정값)
  |
  v
getFullObservationIds() -- 최신 M개를 Set으로 선정
  |
  v
renderTimeline()
  - fullObservationIds에 포함된 관측: narrative/facts 전체 렌더링
  - 나머지: 테이블 행 (제목만)
```

### 10.4 세션 요약 수 제한

```
sessionCount (설정값)
  |
  v
querySummaries() -- SQL LIMIT = sessionCount + 1 (SUMMARY_LOOKAHEAD)
  |
  v
displaySummaries = summaries.slice(0, sessionCount)  -- 표시용
allSummaries                                          -- displayEpoch 계산용
```

### 10.5 잘림 전략 (Truncation)

명시적인 잘림 전략은 없다. 대신:

1. **관측 수 제한:** `totalObservationCount`로 SQL 쿼리 단계에서 제한
2. **전문 표시 제한:** `fullObservationCount`개만 전체 내용 표시
3. **세션 수 제한:** `sessionCount`로 요약 표시 수 제한
4. **조건부 섹션:** `showLastSummary`, `showLastMessage` 등으로 섹션 자체를 제거

토큰 예산 초과 시 자동으로 관측을 잘라내는 메커니즘은 없다. 사용자가 설정값을 조절하여 간접적으로 토큰 사용량을 제어해야 한다.

### 10.6 토큰 경제성 표시와 ROI

토큰 경제성은 예산 관리가 아닌 **정보 표시** 목적이다:

```
Context Economics:
- Loading: 45 observations (2,340 tokens to read)
- Work investment: 128,500 tokens spent on research, building, and decisions
- Your savings: 126,160 tokens (98% reduction from reuse)
```

이 메시지는 LLM에게 "이 컨텍스트가 128k 토큰의 작업을 2.3k 토큰으로 압축한 것"이라는 가치 인식을 전달한다. 예산 제한이나 잘림과는 무관하다.

### 10.7 우선순위 레벨

관측의 우선순위는 **시간순** (created_at_epoch DESC)으로 결정된다. 타입이나 중요도에 따른 가중치는 없다. 가장 최근 관측이 항상 우선적으로 포함되며, `fullObservationCount`에 의해 가장 최근 N개만 전문이 표시된다.

유일한 필터 기반 우선순위는 `observationTypes`와 `observationConcepts` 설정으로, 활성 모드에 속하지 않는 관측 타입/개념은 SQL 쿼리 단계에서 완전히 제외된다.

### 10.8 Worktree에서의 예산 분배

다중 프로젝트(worktree) 모드에서는 모든 프로젝트의 관측이 동일한 `totalObservationCount` 한도 내에서 시간순으로 경쟁한다. 프로젝트별 별도 할당은 없으며, 가장 최근 활동이 많은 프로젝트의 관측이 자연스럽게 더 많이 포함된다.
