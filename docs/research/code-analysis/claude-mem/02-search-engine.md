# claude-mem -- 검색 엔진 분석

## 1. 검색 아키텍처

### 1.1 SearchManager.ts -- 메인 오케스트레이터 (1,884L)

SearchManager는 claude-mem 검색 시스템의 최상위 진입점으로, MCP 도구 핸들러에서 직접 호출되는 클래스이다. 1,884줄에 달하는 이 파일은 현재 **이중 구조**를 가지고 있다: 리팩터링된 모듈형 검색 인프라(SearchOrchestrator 등)와 레거시 인라인 구현이 공존한다.

**생성자 의존성:**

```typescript
constructor(
  private sessionSearch: SessionSearch,    // SQLite FTS5 검색
  private sessionStore: SessionStore,      // SQLite 데이터 hydration
  private chromaSync: ChromaSync | null,   // ChromaDB 벡터 동기화 (nullable)
  private formatter: FormattingService,    // 레거시 포맷팅
  private timelineService: TimelineService // 레거시 타임라인
)
```

생성자 내부에서 SearchOrchestrator와 TimelineBuilder를 추가로 초기화한다. chromaSync가 null이면 벡터 검색 없이 SQLite 전용 모드로 동작한다.

**공개 메서드 목록:**

| 메서드 | 설명 | 내부 경로 |
|--------|------|-----------|
| `search(args)` | 통합 검색 (observations + sessions + prompts) | PATH 1: filter-only, PATH 2: Chroma semantic, PATH 3: Chroma 미사용 |
| `timeline(args)` | 앵커 기반 또는 쿼리 기반 타임라인 | MODE 1: query-based, MODE 2: anchor-based |
| `decisions(args)` | decision 타입 관측 검색 | Chroma semantic + type filter |
| `changes(args)` | change 관련 관측 검색 | type + concept 병합, 중복 제거 |
| `howItWorks(args)` | "how-it-works" 개념 관측 검색 | metadata-first + semantic ranking |
| `searchObservations(args)` | 관측 전용 시맨틱 검색 | Chroma top-100 -> recency filter -> hydrate |
| `searchSessions(args)` | 세션 요약 시맨틱 검색 | Chroma + doc_type=session_summary |
| `searchUserPrompts(args)` | 사용자 프롬프트 시맨틱 검색 | Chroma + doc_type=user_prompt |
| `findByConcept(args)` | 개념 태그 기반 검색 | metadata-first + semantic ranking |
| `findByFile(args)` | 파일 경로 기반 검색 | observations + sessions 병합 |
| `findByType(args)` | 관측 타입 기반 검색 | metadata-first + semantic ranking |
| `getRecentContext(args)` | 최근 세션 컨텍스트 | SQLite 직접 쿼리 |
| `getContextTimeline(args)` | 앵커 주변 타임라인 | observation/session/timestamp 앵커 |
| `getTimelineByQuery(args)` | 쿼리 -> 타임라인 자동 연결 | auto/interactive 모드 |

**내부 상태:**

- `orchestrator: SearchOrchestrator` -- 모듈형 검색 인프라 (신규)
- `timelineBuilder: TimelineBuilder` -- 타임라인 구성 (신규)
- `sessionSearch: SessionSearch` -- SQLite FTS5 검색 (레거시 직접 사용)
- `sessionStore: SessionStore` -- SQLite 데이터 저장소 (레거시 직접 사용)
- `chromaSync: ChromaSync | null` -- ChromaDB 동기화 (레거시 직접 사용)

**핵심 설계 특징:** SearchManager의 대부분의 메서드는 아직 SearchOrchestrator로 위임되지 않고, 동일한 로직을 인라인으로 직접 구현하고 있다. `search()` 메서드만이 orchestrator를 부분적으로 활용 가능한 구조이며, 나머지 메서드들은 `queryChroma()` 프라이빗 메서드를 통해 ChromaSync에 직접 접근한다.

### 1.2 normalizeParams -- 파라미터 정규화

SearchManager와 SearchOrchestrator 모두 `normalizeParams()` 메서드를 가지고 있으며, URL 쿼리 스트링에서 전달되는 파라미터를 내부 형식으로 변환한다.

```
concepts="a,b,c"  ->  concepts: ["a", "b", "c"]
files="x,y"       ->  files: ["x", "y"]
obs_type="a,b"    ->  obsType: ["a", "b"]
type="a,b"        ->  type: ["a", "b"]
dateStart/dateEnd  ->  dateRange: { start, end }
isFolder="true"    ->  isFolder: true
```

SearchManager 버전은 추가적으로 `filePath -> files` 매핑과 `isFolder` boolean 파싱을 수행한다.

### 1.3 검색 경로 결정 트리

통합 `search()` 메서드의 3가지 경로:

```
query 없음?
  -> PATH 1: SQLite 직접 필터링 (날짜, 프로젝트, 타입)
query 있음 + Chroma 사용 가능?
  -> PATH 2: Chroma 시맨틱 검색
     1. queryChroma(query, 100, whereFilter)
     2. 날짜 범위 필터링 (사용자 지정 또는 90일 기본값)
     3. doc_type별 ID 분류 (observation/session/prompt)
     4. SQLite에서 hydration + 추가 필터 적용
query 있음 + Chroma 미사용?
  -> PATH 3: chromaFailed=true, 빈 결과 + 안내 메시지
```

---

## 2. 검색 전략 패턴

### 2.1 SearchStrategy 인터페이스

`src/services/worker/search/strategies/SearchStrategy.ts`에 정의된 Strategy 패턴 구현:

```typescript
export interface SearchStrategy {
  search(options: StrategySearchOptions): Promise<StrategySearchResult>;
  canHandle(options: StrategySearchOptions): boolean;
  readonly name: string;
}
```

**BaseSearchStrategy** 추상 클래스가 공통 기능을 제공한다:

```typescript
export abstract class BaseSearchStrategy implements SearchStrategy {
  protected emptyResult(strategy: 'chroma' | 'sqlite' | 'hybrid'): StrategySearchResult {
    return {
      results: { observations: [], sessions: [], prompts: [] },
      usedChroma: strategy === 'chroma' || strategy === 'hybrid',
      fellBack: false,
      strategy
    };
  }
}
```

`emptyResult()`에서 주목할 점: `usedChroma` 플래그가 strategy 이름에 따라 자동 설정되어, chroma 또는 hybrid 전략이 빈 결과를 반환해도 "Chroma를 사용했다"는 의미를 전달한다.

### 2.2 전략 선택 로직

SearchOrchestrator는 전략 선택을 `executeWithFallback()` 메서드 내에서 명시적 조건 분기로 수행한다. `canHandle()` 메서드가 존재하지만, orchestrator에서는 사용하지 않고 자체적인 결정 트리를 사용한다:

1. **query 없음** -> SQLiteSearchStrategy
2. **query 있음 + Chroma 사용 가능** -> ChromaSearchStrategy, 실패 시 SQLiteSearchStrategy (query 제거)
3. **query 있음 + Chroma 미사용** -> 빈 결과 반환

특수 검색 메서드(`findByConcept`, `findByType`, `findByFile`)는 Chroma 사용 가능 시 HybridSearchStrategy로, 불가능 시 SQLiteSearchStrategy로 직접 위임한다.

### 2.3 StrategySearchResult 구조

모든 전략이 반환하는 통합 결과 타입:

```typescript
interface StrategySearchResult {
  results: SearchResults;         // { observations, sessions, prompts }
  usedChroma: boolean;            // Chroma 사용 여부
  fellBack: boolean;              // 폴백 발생 여부
  strategy: SearchStrategyHint;   // 'chroma' | 'sqlite' | 'hybrid' | 'auto'
}
```

---

## 3. SQLite 검색 -- SQLiteSearchStrategy.ts (131L)

### 3.1 역할과 적용 범위

SQLiteSearchStrategy는 **텍스트 쿼리 없이 필터만으로 검색**하는 경우와, Chroma가 실패했을 때의 **폴백 전략**으로 사용된다.

### 3.2 canHandle 판단

```typescript
canHandle(options: StrategySearchOptions): boolean {
  return !options.query || options.strategyHint === 'sqlite';
}
```

query가 없거나 전략 힌트가 명시적으로 'sqlite'인 경우에 처리 가능하다고 판단한다.

### 3.3 search() 메서드 동작

`searchType` 파라미터에 따라 3개 카테고리를 독립적으로 조회한다:

- `searchObservations`: `this.sessionSearch.searchObservations(undefined, obsOptions)` -- query를 undefined로 전달
- `searchSessions`: `this.sessionSearch.searchSessions(undefined, baseOptions)`
- `searchPrompts`: `this.sessionSearch.searchUserPrompts(undefined, baseOptions)`

query를 undefined로 전달하므로, SessionSearch 내부에서 FTS5 MATCH 절을 생략하고 WHERE 조건만으로 필터링한다. 이는 날짜 범위, 프로젝트, 타입 등의 메타데이터 필터만 적용됨을 의미한다.

**공통 옵션:**

```typescript
const baseOptions = { limit, offset, orderBy, project, dateRange };
```

기본값: `limit=20`, `offset=0`, `orderBy='date_desc'`

### 3.4 특수 검색 메서드

- `findByConcept(concept, options)` -> `sessionSearch.findByConcept()` 위임
- `findByType(type, options)` -> `sessionSearch.findByType()` 위임
- `findByFile(filePath, options)` -> `sessionSearch.findByFile()` 위임, `{ observations, sessions }` 반환

이 메서드들은 async가 아닌 동기 메서드이며, SQLite를 직접 쿼리한다. SessionSearch 내부에서 FTS5와 JSON 함수를 사용하여 concepts, files_read, files_modified 컬럼의 JSON 배열을 검색한다.

### 3.5 FTS5 사용 (SessionSearch 위임)

SQLiteSearchStrategy 자체에는 FTS5 쿼리 구성 로직이 없다. 실제 FTS5 쿼리는 SessionSearch 클래스에서 수행되며, SQLiteSearchStrategy는 이를 위임하는 얇은 래퍼이다. query가 없을 때는 FTS5를 사용하지 않고 일반 WHERE 절로 필터링한다.

---

## 4. Chroma 벡터 검색 -- ChromaSearchStrategy.ts (248L)

### 4.1 아키텍처

ChromaSearchStrategy는 ChromaDB를 통한 시맨틱 벡터 검색을 수행한다. 의존성으로 ChromaSync(벡터 DB 접근)와 SessionStore(SQLite hydration)를 주입받는다.

### 4.2 canHandle 판단

```typescript
canHandle(options: StrategySearchOptions): boolean {
  return !!options.query && !!this.chromaSync;
}
```

query 텍스트가 존재하고 ChromaSync가 초기화된 경우에만 처리 가능하다.

### 4.3 4단계 검색 파이프라인

**Step 1: Chroma 시맨틱 검색**

```typescript
const chromaResults = await this.chromaSync.queryChroma(
  query,
  SEARCH_CONSTANTS.CHROMA_BATCH_SIZE,  // 100
  whereFilter
);
```

`CHROMA_BATCH_SIZE = 100`으로 고정. whereFilter는 `buildWhereFilter()`로 구성된다.

**Step 2: 최신성 필터 (90일 윈도우)**

```typescript
private filterByRecency(chromaResults): Array<{ id: number; meta: ChromaMetadata }> {
  const cutoff = Date.now() - SEARCH_CONSTANTS.RECENCY_WINDOW_MS;  // 90일
  // 중복 제거된 ids와 metadatas 간의 정렬 불일치를 처리하기 위해
  // sqlite_id -> metadata 맵을 먼저 구성
  const metadataByIdMap = new Map<number, ChromaMetadata>();
  for (const meta of chromaResults.metadatas) {
    if (meta?.sqlite_id !== undefined && !metadataByIdMap.has(meta.sqlite_id)) {
      metadataByIdMap.set(meta.sqlite_id, meta);
    }
  }
  return chromaResults.ids
    .map(id => ({ id, meta: metadataByIdMap.get(id) }))
    .filter(item => item.meta && item.meta.created_at_epoch > cutoff);
}
```

**중요한 구현 세부사항:** ChromaSync.queryChroma()는 중복 제거된 `ids` 배열을 반환하지만, `metadatas` 배열은 하나의 sqlite_id에 대해 여러 항목을 포함할 수 있다 (하나의 observation이 narrative + 여러 facts를 별도 Chroma 문서로 저장하기 때문). 따라서 ids와 metadatas의 인덱스가 정렬되지 않을 수 있으며, Map을 통한 조회가 필요하다.

참고: SearchManager의 레거시 코드에서는 이 문제를 처리하지 않고 인덱스 기반으로 직접 매핑한다 (`chromaResults.metadatas[idx]`). 이는 잠재적 버그이다.

**Step 3: 문서 타입별 분류**

```typescript
private categorizeByDocType(items, options): { obsIds, sessionIds, promptIds }
```

`doc_type` 메타데이터를 기준으로 ID를 observation/session_summary/user_prompt 카테고리로 분류한다. `searchObservations`/`searchSessions`/`searchPrompts` 플래그에 따라 해당 카테고리만 수집한다.

**Step 4: SQLite Hydration**

분류된 ID들을 SessionStore의 `getObservationsByIds()`, `getSessionSummariesByIds()`, `getUserPromptsByIds()`로 전달하여 전체 레코드를 조회한다. 이 단계에서 추가적인 필터(obs_type, concepts, files)가 적용된다.

### 4.4 Chroma Where Filter 구성

```typescript
private buildWhereFilter(searchType, project): Record<string, any> | undefined
```

doc_type 필터와 project 필터를 조합한다:

- `searchType='observations'` -> `{ doc_type: 'observation' }`
- `searchType='sessions'` -> `{ doc_type: 'session_summary' }`
- project 추가 시 -> `{ $and: [docTypeFilter, { project }] }`
- 둘 다 없으면 -> `undefined` (전체 검색)

**project 필터의 중요성:** Chroma where 절에 project를 포함하지 않으면, 규모가 큰 프로젝트의 문서가 top-N 결과를 독점하여 작은 프로젝트의 결과가 밀려나는 문제가 발생한다. 이 필터는 벡터 검색 시점에서 프로젝트 범위를 한정한다.

### 4.5 에러 처리

Chroma 검색 실패 시 `usedChroma: false`를 반환하여, 호출자(SearchOrchestrator)가 SQLite 폴백을 시도할 수 있도록 한다.

---

## 5. 하이브리드 검색 -- HybridSearchStrategy.ts (270L)

### 5.1 Metadata-First + Semantic Ranking 패턴

HybridSearchStrategy는 **SQLite 메타데이터 필터 -> Chroma 시맨틱 랭킹 -> 교차점 -> Hydration** 4단계 패턴을 구현한다. ChromaSearchStrategy가 "Chroma-first"인 반면, HybridSearchStrategy는 "Metadata-first"이다.

### 5.2 canHandle 판단

```typescript
canHandle(options: StrategySearchOptions): boolean {
  return !!this.chromaSync && (
    !!options.concepts ||
    !!options.files ||
    (!!options.type && !!options.query) ||
    options.strategyHint === 'hybrid'
  );
}
```

concepts, files 필터가 있거나, type+query 조합이 있거나, 명시적 hybrid 힌트가 있을 때 처리 가능하다.

### 5.3 findByConcept 파이프라인

```
Step 1: SQLite 메타데이터 필터
  sessionSearch.findByConcept(concept, filterOptions)
  -> concepts JSON 배열에서 해당 개념을 가진 모든 observation 반환

Step 2: Chroma 시맨틱 랭킹
  chromaSync.queryChroma(concept, min(ids.length, 100))
  -> concept 텍스트에 대한 시맨틱 유사도 순위

Step 3: 교차점 (intersectWithRanking)
  Chroma 순위를 유지하면서 SQLite에서 필터된 ID만 보존

Step 4: Hydration
  sessionStore.getObservationsByIds(rankedIds, { limit })
  -> Chroma 시맨틱 순위 순서로 재정렬
```

### 5.4 intersectWithRanking 알고리즘

```typescript
private intersectWithRanking(metadataIds: number[], chromaIds: number[]): number[] {
  const metadataSet = new Set(metadataIds);
  const rankedIds: number[] = [];
  for (const chromaId of chromaIds) {
    if (metadataSet.has(chromaId) && !rankedIds.includes(chromaId)) {
      rankedIds.push(chromaId);
    }
  }
  return rankedIds;
}
```

Chroma의 ID 순서(시맨틱 유사도 순)를 유지하면서, SQLite 메타데이터 필터를 통과한 ID만 보존한다. `rankedIds.includes(chromaId)` 체크로 중복을 방지한다.

**성능 관찰:** `rankedIds.includes()`는 O(n) 탐색이므로, 결과가 많을 경우 O(n^2) 복잡도가 될 수 있다. 그러나 `CHROMA_BATCH_SIZE=100` 제한으로 인해 실질적으로 문제가 되지 않는다.

### 5.5 findByType 파이프라인

findByConcept과 동일한 4단계 패턴을 따른다. type이 배열일 경우 `type.join(', ')`로 결합하여 Chroma 쿼리 텍스트로 사용한다.

### 5.6 findByFile 파이프라인

파일 검색은 다른 특수 검색과 구별되는 특징이 있다:

- **Sessions는 시맨틱 랭킹을 건너뛴다:** 세션 요약은 이미 요약된 텍스트이므로 추가 시맨틱 랭킹이 불필요하다. metadata 필터 결과를 그대로 반환한다.
- **Observations만 4단계 패턴**을 적용한다.
- 반환 타입이 `{ observations, sessions, usedChroma }` 형태로, `StrategySearchResult`와 다르다.

### 5.7 에러 처리와 폴백

모든 `findBy*` 메서드는 try-catch로 감싸져 있으며, 실패 시 SQLite 전용 결과로 폴백한다. `fellBack: true`와 `usedChroma: false`를 설정하여 폴백이 발생했음을 표시한다.

---

## 6. 검색 오케스트레이터 -- SearchOrchestrator.ts (290L)

### 6.1 역할

SearchOrchestrator는 검색 전략 선택, 폴백 체인, 결과 포맷팅 위임을 담당하는 중앙 조율자이다. 모든 검색 관련 컴포넌트(전략, 포맷터, 타임라인 빌더)를 소유한다.

### 6.2 초기화

```typescript
constructor(sessionSearch, sessionStore, chromaSync) {
  this.sqliteStrategy = new SQLiteSearchStrategy(sessionSearch);
  if (chromaSync) {
    this.chromaStrategy = new ChromaSearchStrategy(chromaSync, sessionStore);
    this.hybridStrategy = new HybridSearchStrategy(chromaSync, sessionStore, sessionSearch);
  }
  this.resultFormatter = new ResultFormatter();
  this.timelineBuilder = new TimelineBuilder();
}
```

Chroma가 없으면 sqliteStrategy만 초기화되고, chromaStrategy와 hybridStrategy는 null로 남는다.

### 6.3 폴백 체인

`executeWithFallback()` 메서드의 3경로 폴백:

```
PATH 1: query 없음 -> SQLite 직접
PATH 2: Chroma 사용 가능
  -> ChromaSearchStrategy.search()
  -> usedChroma가 true면 결과 반환 (0건이어도)
  -> usedChroma가 false면 (Chroma 실패)
     -> SQLite 폴백 (query 제거, fellBack: true)
PATH 3: Chroma 미사용 -> 빈 결과
```

**중요 설계 결정:** Chroma가 0건을 반환한 경우, 이를 "올바른 결과"로 간주하고 SQLite 폴백을 하지 않는다. 이는 FTS5 폴백이 시맨틱 검색의 의도를 왜곡할 수 있기 때문이다.

### 6.4 전용 검색 메서드

- `findByConcept(concept, args)` -> HybridSearchStrategy 또는 SQLiteSearchStrategy
- `findByType(type, args)` -> HybridSearchStrategy 또는 SQLiteSearchStrategy
- `findByFile(filePath, args)` -> HybridSearchStrategy 또는 SQLiteSearchStrategy

### 6.5 타임라인 위임

```typescript
getTimeline(timelineData, anchorId, anchorEpoch, depthBefore, depthAfter): TimelineItem[]
formatTimeline(items, anchorId, options): string
formatSearchResults(results, query, chromaFailed): string
```

### 6.6 접근자

`getFormatter()`와 `getTimelineBuilder()`로 내부 컴포넌트에 직접 접근할 수 있다.

---

## 7. 결과 포맷팅 -- ResultFormatter.ts (301L)

### 7.1 역할

ResultFormatter는 검색 결과를 Markdown 테이블 형태로 포맷팅한다. 통합 결과 정렬, 날짜별 그룹핑, 파일별 그룹핑, 테이블 렌더링을 담당한다.

### 7.2 formatSearchResults -- 메인 포맷팅 메서드

```typescript
formatSearchResults(results: SearchResults, query: string, chromaFailed: boolean): string
```

동작 순서:
1. 총 결과 수 계산 (observations + sessions + prompts)
2. 0건 + chromaFailed -> `formatChromaFailureMessage()` (벡터 검색 설치 안내)
3. 0건 -> `No results found matching "query"` 메시지
4. `combineResults()`로 3개 카테고리를 통합, epoch 기준 내림차순 정렬
5. `groupByDate()`로 날짜별 그룹핑
6. 각 날짜 내에서 파일별 추가 그룹핑 (observation만 파일 정보 보유, 나머지는 'General')
7. 테이블 헤더 + 각 행 렌더링

### 7.3 combineResults -- 통합 결과 구성

```typescript
combineResults(results: SearchResults): CombinedResult[]
```

3개 카테고리를 `CombinedResult` 유니언 타입으로 변환:
```typescript
{ type: 'observation' | 'session' | 'prompt', data, epoch, created_at }
```

### 7.4 테이블 포맷

**검색 테이블 (Search):**
```
| ID | Time | T | Title | Read |
|----|------|---|-------|------|
```

**인덱스 테이블 (Index):**
```
| ID | Time | T | Title | Read | Work |
|-----|------|---|-------|------|------|
```

차이점: 인덱스 테이블에는 Work 컬럼이 추가된다. Work 컬럼은 `discovery_tokens` 값을 표시한다.

### 7.5 행 포맷팅 메서드

**Observation 행:**
```typescript
formatObservationSearchRow(obs, lastTime): { row, time }
```
- ID: `#123` 형식
- Time: 이전 행과 같으면 `"` (ditto mark)으로 표시
- T: `ModeManager.getInstance().getTypeIcon(obs.type)` -- 타입별 이모지
- Title: `obs.title || 'Untitled'`
- Read: `~tokens` (title + subtitle + narrative + facts의 문자 수 / 4)

**Session 행:**
- ID: `#S123` 형식
- T: target 이모지
- Title: `session.request || 'Session {id}'`

**Prompt 행:**
- ID: `#P123` 형식
- T: speech bubble 이모지
- Title: 60자 초과 시 57자 + '...'로 잘림

### 7.6 토큰 추정

```typescript
private estimateReadTokens(obs): number {
  const size = (title.length + subtitle.length + narrative.length + facts.length);
  return Math.ceil(size / CHARS_PER_TOKEN_ESTIMATE);  // CHARS_PER_TOKEN_ESTIMATE = 4
}
```

4자를 1토큰으로 근사 추정한다.

### 7.7 검색 팁

```typescript
formatSearchTips(): string
```

사용법 가이드를 포함하는 풋터 텍스트:
- 인덱스로 제목/날짜/ID 확인
- 타임라인으로 관심 결과 주변 컨텍스트 확인
- `get_observations(ids=[...])` 로 상세 정보 일괄 조회
- 필터 예시: `obs_type="bugfix,feature"`, `dateStart="2025-01-01"`

---

## 8. 타임라인 빌더 -- TimelineBuilder.ts (303L)

### 8.1 역할

TimelineBuilder는 검색 결과를 시간순으로 정렬하고, 앵커 포인트 주변의 깊이 윈도우를 적용하여 타임라인 뷰를 구성한다.

### 8.2 TimelineItem 및 TimelineData 인터페이스

```typescript
interface TimelineItem {
  type: 'observation' | 'session' | 'prompt';
  data: ObservationSearchResult | SessionSummarySearchResult | UserPromptSearchResult;
  epoch: number;
}

interface TimelineData {
  observations: ObservationSearchResult[];
  sessions: SessionSummarySearchResult[];
  prompts: UserPromptSearchResult[];
}
```

### 8.3 buildTimeline -- 타임라인 구성

```typescript
buildTimeline(data: TimelineData): TimelineItem[]
```

3개 카테고리를 TimelineItem으로 변환하고, `epoch` 기준 오름차순(시간순)으로 정렬한다. 검색 결과의 내림차순 정렬과 반대임에 주목.

### 8.4 filterByDepth -- 깊이 윈도우 적용

```typescript
filterByDepth(items, anchorId, anchorEpoch, depthBefore, depthAfter): TimelineItem[]
```

1. `findAnchorIndex()`로 앵커 위치 탐색
2. `startIndex = max(0, anchorIndex - depthBefore)`
3. `endIndex = min(items.length, anchorIndex + depthAfter + 1)`
4. `items.slice(startIndex, endIndex)` 반환

### 8.5 findAnchorIndex -- 앵커 탐색

3가지 앵커 타입을 지원한다:

1. **숫자 (observation ID):** `item.type === 'observation' && item.data.id === anchorId`
2. **문자열 "S숫자" (session ID):** `item.type === 'session' && item.data.id === sessionNum`
3. **타임스탬프:** `item.epoch >= anchorEpoch`인 첫 항목, 없으면 마지막 항목

### 8.6 formatTimeline -- Markdown 타임라인 렌더링

```typescript
formatTimeline(items, anchorId, options): string
```

렌더링 구조:
```
# Timeline for query: "query" / # Timeline around anchor: anchorId
**Anchor:** Observation #id - title
**Window:** N records before -> M records after | **Items:** count

### Dec 14, 2025

**Target #S123** Session title (Dec 14, 7:30 PM)

**Speech #1** User prompt text (Dec 14, 7:31 PM)
> truncated prompt text...

**src/file.ts**
| ID | Time | T | Title | Tokens |
|----|------|---|-------|--------|
| #456 | 7:32 PM | icon | Title <- **ANCHOR** | ~120 |
```

**렌더링 특징:**
- 날짜별 그룹핑 (시간순 정렬)
- 파일별 서브그룹핑 (observation만)
- Session과 Prompt는 테이블 밖에 독립 블록으로 렌더링
- 파일이 바뀌면 이전 테이블을 닫고 새 테이블 시작
- 같은 시간의 연속 행은 ditto mark(`"`)로 시간 표시 생략
- 앵커 항목에 `<- **ANCHOR**` 마커 추가

### 8.7 그룹핑 유틸리티

- `groupByDay(items)` -> `Map<string, TimelineItem[]>` -- 날짜별 그룹화
- `sortDaysChronologically(dayMap)` -> `Array<[string, TimelineItem[]]>` -- 날짜 순 정렬
- `isAnchorItem(item, anchorId)` -> `boolean` -- 앵커 식별

---

## 9. 검색 모드/파라미터

### 9.1 상수 정의

```typescript
const SEARCH_CONSTANTS = {
  RECENCY_WINDOW_DAYS: 90,
  RECENCY_WINDOW_MS: 90 * 24 * 60 * 60 * 1000,  // 7,776,000,000ms
  DEFAULT_LIMIT: 20,
  CHROMA_BATCH_SIZE: 100
};
```

### 9.2 검색 타입 (searchType)

| 값 | 설명 |
|----|------|
| `'all'` | observations + sessions + prompts 모두 검색 (기본값) |
| `'observations'` | observations만 검색 |
| `'sessions'` | session summaries만 검색 |
| `'prompts'` | user prompts만 검색 |

### 9.3 관측 타입 (obsType / type)

```typescript
type ObservationType = 'decision' | 'bugfix' | 'feature' | 'refactor' | 'discovery' | 'change';
```

쉼표로 구분된 문자열 또는 배열로 전달 가능: `"bugfix,feature"` -> `["bugfix", "feature"]`

### 9.4 검색 전략 힌트 (strategyHint)

```typescript
type SearchStrategyHint = 'chroma' | 'sqlite' | 'hybrid' | 'auto';
```

명시적으로 전략을 강제할 수 있다. `canHandle()` 메서드에서 `strategyHint === 'sqlite'` 또는 `'hybrid'` 체크에 사용된다.

### 9.5 정렬 옵션 (orderBy)

```typescript
orderBy?: 'relevance' | 'date_desc' | 'date_asc';
```

- `'date_desc'`: 최신순 (기본값)
- `'date_asc'`: 오래된 순
- `'relevance'`: FTS5 관련도 순 (SQLite 전용)

### 9.6 날짜 범위 (dateRange)

```typescript
interface DateRange {
  start?: string | number;  // ISO 문자열 또는 epoch 밀리초
  end?: string | number;
}
```

URL 파라미터에서는 `dateStart`/`dateEnd`로 전달되고, normalizeParams에서 `dateRange` 객체로 변환된다.

### 9.7 출력 형식 (format)

```typescript
format?: 'text' | 'json';
```

`'json'`이면 raw 데이터를 그대로 반환한다. 기본값은 `'text'`로 Markdown 포맷 문자열을 반환한다.

### 9.8 기타 파라미터

| 파라미터 | 타입 | 기본값 | 설명 |
|----------|------|--------|------|
| `limit` | number | 20 | 결과 수 제한 |
| `offset` | number | 0 | 페이지네이션 오프셋 |
| `project` | string | - | 프로젝트 필터 |
| `concepts` | string/string[] | - | 개념 태그 필터 |
| `files` | string/string[] | - | 파일 경로 필터 |
| `isFolder` | boolean | - | 폴더 모드 (직접 자식만 매칭) |

### 9.9 필터 유틸리티

`src/services/worker/search/filters/` 디렉토리에 3개의 필터 유틸리티가 있다:

**DateFilter.ts:**
- `parseDateRange(dateRange)` -> `{ startEpoch?, endEpoch? }` -- 날짜 범위 파싱
- `isWithinDateRange(epoch, dateRange)` -> boolean
- `isRecent(epoch)` -> boolean -- 90일 이내 여부
- `filterResultsByDate(results, dateRange)` -> 필터링된 결과
- `getDateBoundaries(range)` -> DateRange -- 'today'/'week'/'month'/'90days' 프리셋

**ProjectFilter.ts:**
- `getCurrentProject()` -> `basename(process.cwd())` -- 현재 프로젝트명
- `normalizeProject(project)` -> 정규화된 프로젝트명
- `matchesProject(resultProject, filterProject)` -> boolean
- `filterResultsByProject(results, project)` -> 필터링된 결과

**TypeFilter.ts:**
- `OBSERVATION_TYPES` -- 유효한 타입 목록
- `normalizeType(type)` -> `ObservationType[] | undefined`
- `matchesType(resultType, filterTypes)` -> boolean
- `filterObservationsByType(observations, types)` -> 필터링된 결과
- `parseTypeString(typeString)` -> `ObservationType[]`

---

## 10. HTTP 검색 엔드포인트 -- SearchRoutes.ts (370L)

### 10.1 라우트 구성

SearchRoutes는 `BaseRouteHandler`를 확장하며, Express 애플리케이션에 모든 검색 관련 HTTP 엔드포인트를 등록한다.

### 10.2 통합 엔드포인트 (신규 API)

| 엔드포인트 | 메서드 | 핸들러 | 설명 |
|-----------|--------|--------|------|
| `GET /api/search` | GET | `handleUnifiedSearch` | 통합 검색 |
| `GET /api/timeline` | GET | `handleUnifiedTimeline` | 통합 타임라인 |
| `GET /api/decisions` | GET | `handleDecisions` | decision 관측 검색 |
| `GET /api/changes` | GET | `handleChanges` | change 관련 관측 검색 |
| `GET /api/how-it-works` | GET | `handleHowItWorks` | how-it-works 관측 검색 |

### 10.3 하위 호환 엔드포인트

| 엔드포인트 | 핸들러 | 설명 |
|-----------|--------|------|
| `GET /api/search/observations` | `handleSearchObservations` | 관측 전용 검색 |
| `GET /api/search/sessions` | `handleSearchSessions` | 세션 전용 검색 |
| `GET /api/search/prompts` | `handleSearchPrompts` | 프롬프트 전용 검색 |
| `GET /api/search/by-concept` | `handleSearchByConcept` | 개념별 검색 |
| `GET /api/search/by-file` | `handleSearchByFile` | 파일별 검색 |
| `GET /api/search/by-type` | `handleSearchByType` | 타입별 검색 |

### 10.4 컨텍스트 엔드포인트

| 엔드포인트 | 핸들러 | 설명 |
|-----------|--------|------|
| `GET /api/context/recent` | `handleGetRecentContext` | 최근 세션 컨텍스트 |
| `GET /api/context/timeline` | `handleGetContextTimeline` | 앵커 주변 타임라인 |
| `GET /api/context/preview` | `handleContextPreview` | 컨텍스트 미리보기 (터미널 ANSI 출력) |
| `GET /api/context/inject` | `handleContextInject` | 컨텍스트 주입 (hooks용) |

### 10.5 기타 엔드포인트

| 엔드포인트 | 핸들러 | 설명 |
|-----------|--------|------|
| `GET /api/timeline/by-query` | `handleGetTimelineByQuery` | 쿼리 -> 타임라인 |
| `GET /api/search/help` | `handleSearchHelp` | API 도움말 JSON |

### 10.6 핸들러 구현 패턴

모든 핸들러는 `this.wrapHandler()`로 감싸져 있으며, `req.query`를 직접 SearchManager 메서드에 전달한다:

```typescript
private handleUnifiedSearch = this.wrapHandler(async (req, res) => {
  const result = await this.searchManager.search(req.query);
  res.json(result);
});
```

파라미터 검증은 SearchManager의 `normalizeParams()`에서 수행되며, 라우트 레벨에서는 별도 검증이 없다.

### 10.7 컨텍스트 미리보기/주입 특수 처리

`handleContextPreview`와 `handleContextInject`는 다른 핸들러와 달리:

1. `generateContext()`를 동적으로 import한다 (worker 프로세스에서 실행되므로 DB 접근 가능)
2. JSON이 아닌 `text/plain` 형식으로 응답한다
3. `useColors` 옵션으로 ANSI 색상 코드 포함 여부를 제어한다

**컨텍스트 주입 (handleContextInject):**
- `projects` 또는 `project` 파라미터 지원 (하위 호환)
- 쉼표 구분 프로젝트 목록 지원 (worktree: "main,worktree-branch")
- `colors=true`로 터미널 ANSI 출력 활성화
- 마지막 프로젝트를 primary project로 사용

### 10.8 도움말 엔드포인트

`GET /api/search/help`는 모든 검색 API의 문서를 JSON으로 반환한다. 각 엔드포인트의 경로, HTTP 메서드, 설명, 파라미터를 포함하며, curl 예시도 제공한다. 기본 포트는 37777이다.

### 10.9 타입 시스템 요약

검색 시스템에서 사용되는 핵심 타입 계층:

```
SearchOptions (sqlite/types.ts)
  <- ExtendedSearchOptions (search/types.ts)
       <- StrategySearchOptions (search/types.ts)

ObservationRow (sqlite/types.ts)
  <- ObservationSearchResult (sqlite/types.ts)  // + rank, score

SearchResults = {
  observations: ObservationSearchResult[],
  sessions: SessionSummarySearchResult[],
  prompts: UserPromptSearchResult[]
}

StrategySearchResult = {
  results: SearchResults,
  usedChroma: boolean,
  fellBack: boolean,
  strategy: SearchStrategyHint
}

ChromaMetadata = {
  sqlite_id, doc_type, memory_session_id, project,
  created_at_epoch, type?, title?, subtitle?, concepts?,
  files_read?, files_modified?, field_type?, prompt_number?
}

CombinedResult = {
  type: 'observation' | 'session' | 'prompt',
  data: SearchResult,
  epoch: number,
  created_at: string
}
```
