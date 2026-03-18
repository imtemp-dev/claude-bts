# memctl -- 검색 엔진 분석

## 목차

1. [검색 아키텍처 개요](#1-검색-아키텍처-개요)
2. [FTS5 검색](#2-fts5-검색)
3. [벡터 검색](#3-벡터-검색)
4. [Reciprocal Rank Fusion (RRF)](#4-reciprocal-rank-fusion-rrf)
5. [인텐트 분류](#5-인텐트-분류)
6. [관련성 점수 계산](#6-관련성-점수-계산)
7. [인텐트 가중 랭킹](#7-인텐트-가중-랭킹)
8. [조직 간 검색](#8-조직-간-검색)
9. [유사도 검색](#9-유사도-검색)
10. [전체 플로우 트레이스](#10-전체-플로우-트레이스)

---

## 1. 검색 아키텍처 개요

memctl의 검색 엔진은 keyword 기반 검색과 semantic 기반 검색을 결합하는 **hybrid search** 파이프라인이다. 사용자 쿼리가 입력되면 인텐트 분류를 거쳐 FTS5와 벡터 검색이 병렬로 실행되고, Reciprocal Rank Fusion으로 결과를 병합한 후 인텐트별 가중치가 적용된 최종 랭킹을 반환한다.

```
  Query Input
       |
       v
  +-----------------------+
  | Intent Classification |  <-- packages/shared/src/intent.ts
  | (5 intents + weights) |
  +-----------+-----------+
              |
     +--------+--------+
     |                  |
     v                  v
+----------+    +---------------+
| FTS5     |    | Vector Search |
| (BM25)   |    | (cosine sim)  |
+----+-----+    +-------+-------+
     |                  |
     v                  v
  ftsIds[]          vectorIds[]
     |                  |
     +--------+---------+
              |
              v
  +-------------------------+
  | Reciprocal Rank Fusion  |  <-- mergeSearchResults()
  | (k=60)                  |
  +------------+------------+
               |
               v
  +----------------------------+
  | Intent-Weighted Ranking    |  <-- route.ts (scored composite)
  | priorityScore * pBoost     |
  | recencyScore * rBoost      |
  | graphScore * gBoost        |
  +------------+---------------+
               |
               v
  +----------------------------+
  | computeRelevanceScore()    |  <-- packages/shared/src/relevance.ts
  | (final relevance_score)    |
  +------------+---------------+
               |
               v
       Sorted Results + Pagination
```

### 핵심 소스 파일

| 파일 | 역할 |
|------|------|
| `apps/web/lib/fts.ts` | FTS5 virtual table 관리, keyword 검색, 벡터 검색, RRF 병합 |
| `apps/web/lib/embeddings.ts` | embedding 생성, cosine similarity, int8 quantization |
| `apps/web/app/api/v1/memories/route.ts` | 검색 API 엔드포인트, 인텐트 가중 랭킹 |
| `packages/shared/src/intent.ts` | 인텐트 분류, 감지 패턴, 가중치 정의 |
| `packages/shared/src/relevance.ts` | 관련성 점수 공식 (usage signal 기반) |
| `apps/web/app/api/v1/memories/similar/route.ts` | 유사도 검색 (dedup, linking) |
| `apps/web/app/api/v1/memories/search-org/route.ts` | 조직 간 cross-project 검색 |

---

## 2. FTS5 검색

### 2.1 Virtual Table 생성

`ensureFts()` 함수가 프로세스당 한 번 실행되어 FTS5 virtual table과 동기화 trigger를 생성한다. `ftsInitialized` flag로 중복 초기화를 방지한다.

```sql
CREATE VIRTUAL TABLE IF NOT EXISTS memories_fts USING fts5(
  key,
  content,
  tags,
  content='memories',
  content_rowid='rowid'
)
```

**content table 방식**을 사용한다. `content='memories'`는 FTS5가 `memories` 테이블의 데이터를 참조하되, 별도의 인덱스만 유지하는 contentless mode (external content)를 의미한다. 인덱싱 대상 컬럼은 `key`, `content`, `tags` 세 개다.

### 2.2 동기화 Trigger

FTS 인덱스와 원본 테이블의 동기화를 위해 세 개의 trigger가 등록된다.

| Trigger | 이벤트 | 동작 |
|---------|--------|------|
| `memories_ai` | AFTER INSERT | 새 row를 FTS 인덱스에 삽입. `COALESCE(NEW.tags, '')`로 null 처리 |
| `memories_ad` | AFTER DELETE | `'delete'` 명령으로 FTS 인덱스에서 제거 |
| `memories_au` | AFTER UPDATE | 기존 항목을 삭제 후 신규 항목을 재삽입 (delete + insert) |

FTS5 external content table에서의 삭제는 특수 구문을 사용한다:

```sql
INSERT INTO memories_fts(memories_fts, rowid, key, content, tags)
VALUES ('delete', OLD.rowid, OLD.key, OLD.content, COALESCE(OLD.tags, ''));
```

첫 번째 컬럼에 테이블 이름 자체를 넣고 값으로 `'delete'`를 전달하는 것이 FTS5의 delete 프로토콜이다.

### 2.3 쿼리 이스케이핑과 Phrase 검색 구성

`ftsSearch()` 함수는 두 단계로 쿼리를 변환한다.

**단계 1: 특수 문자 제거**

```
query.replace(/['"*(){}[\]^~\\:]/g, " ").trim()
```

FTS5 메타 문자(`*`, `"`, `()`, `^`, `~`, `:` 등)를 공백으로 치환하여 injection을 방지한다.

**단계 2: OR 구문 phrase 검색 생성**

```
safeQuery.split(/\s+/)
  .map((w) => `"${w}"`)
  .join(" OR ")
```

각 단어를 double-quote로 감싸 exact match phrase로 만들고, `OR`로 연결한다. 예를 들어 입력이 `"testing patterns"`이면 FTS 쿼리는 `"testing" OR "patterns"`가 된다. 이 방식은 partial match를 방지하고 단어 단위의 정확한 매칭을 보장한다.

### 2.4 Rank 정렬 및 필터링

```sql
SELECT m.id, rank
FROM memories m
JOIN memories_fts fts ON m.rowid = fts.rowid
WHERE memories_fts MATCH ${ftsQuery}
  AND m.project_id = ${projectId}
  AND m.archived_at IS NULL
ORDER BY rank
LIMIT ${limit}
```

FTS5의 `rank` 컬럼은 내부적으로 **BM25** 점수를 계산한다 (음수값, 작을수록 더 관련성이 높음). `ORDER BY rank`는 ascending 정렬이므로 가장 관련도 높은 결과가 먼저 나온다.

`project_id` 필터와 `archived_at IS NULL` 조건으로 현재 프로젝트의 활성 메모리만 검색한다.

### 2.5 Fallback 전략

FTS5를 사용할 수 없는 환경(테스트 등)에서는:

- `ensureFts()`가 silent하게 실패하고 `ftsInitialized`가 false로 유지
- `ftsSearch()`가 `null`을 반환
- route.ts에서 `null`을 감지하면 `LIKE %query%` 기반 fallback 검색을 실행 (key 검색 + content 검색 후 수동 병합/중복 제거)

---

## 3. 벡터 검색

### 3.1 Embedding 모델

```typescript
const { pipeline: createPipeline } = await import("@xenova/transformers");
pipeline = await createPipeline("feature-extraction", "Xenova/all-MiniLM-L6-v2");
```

| 항목 | 값 |
|------|-----|
| 모델 | `Xenova/all-MiniLM-L6-v2` (ONNX Runtime via `@xenova/transformers`) |
| 태스크 | `feature-extraction` |
| 차원 | 384 (`EMBEDDING_DIM = 384`) |
| Pooling | `mean` |
| Normalization | `normalize: true` |

`getEmbedder()`는 singleton 패턴으로 구현되어 있다. `pipeline` 변수에 캐시되고, 동시 호출 시 `pipelineLoading` Promise를 공유하여 중복 로딩을 방지한다. 모델 로드 실패 시 `null`을 반환하며 검색은 FTS만으로 계속 진행된다.

### 3.2 Embedding 생성

**단일 텍스트:**

```typescript
async function generateEmbedding(text: string): Promise<Float32Array | null>
```

**배치 처리:**

```typescript
async function generateEmbeddings(texts: string[]): Promise<(Float32Array | null)[]>
```

배치 함수는 pipeline에 배열을 전달하여 concatenated output을 받고, `EMBEDDING_DIM` 단위로 slicing한다. 배치 실패 시 sequential fallback으로 개별 처리한다.

### 3.3 Int8 Quantization

저장 공간 최적화를 위해 Float32 embedding을 Int8로 양자화한다.

```
quantizeEmbedding():
  min = min(emb)
  max = max(emb)
  range = max - min (|| 1)
  values[i] = round(((emb[i] - min) / range) * 255) - 128
  저장: { values: number[], min: number, max: number }
```

```
dequantizeEmbedding():
  result[i] = ((values[i] + 128) / 255) * range + min
```

JSON 직렬화 시 약 3-4KB에서 ~500 bytes로 압축된다. `deserializeEmbedding()`은 양자화 포맷(`{ values, min, max }`)과 레거시 Float32 배열 포맷 모두를 감지하여 처리한다.

### 3.4 Cosine Similarity 계산

```typescript
function cosineSimilarity(a: Float32Array, b: Float32Array): number {
  // dot(a, b) / (||a|| * ||b||)
}
```

```
cosine_similarity(a, b) = sum(a[i] * b[i]) / (sqrt(sum(a[i]^2)) * sqrt(sum(b[i]^2)))
```

길이가 다른 벡터는 0을 반환하고, denominator가 0인 경우에도 0을 반환한다.

### 3.5 벡터 검색 실행 (`vectorSearch`)

```typescript
async function vectorSearch(
  projectId: string, query: string, limit: number
): Promise<string[] | null>
```

동작 순서:

1. `generateEmbedding(query)`로 쿼리 벡터 생성
2. 해당 프로젝트의 embedding이 있는 모든 비아카이브 메모리 조회
3. 각 메모리의 embedding을 역양자화하여 cosine similarity 계산
4. **threshold 0.3** 이상인 결과만 필터링
5. similarity 내림차순 정렬 후 `limit`개 반환

주의: **in-memory 비교 방식**이다. 프로젝트의 모든 embedding을 로드한 후 JavaScript에서 순차적으로 비교한다. 벡터 DB(pgvector, FAISS 등)를 사용하지 않는 대신 외부 의존성이 없다. 메모리 수가 많은 프로젝트에서 성능 병목이 될 수 있다.

---

## 4. Reciprocal Rank Fusion (RRF)

### 4.1 공식

```
RRF_score(d) = sum_over_lists( 1 / (k + rank(d)) )
```

여기서:
- `d`: 문서 (memory ID)
- `k`: smoothing 파라미터 (기본값 60)
- `rank(d)`: 해당 리스트에서 문서의 0-based 순위

### 4.2 구현

```typescript
function mergeSearchResults(
  ftsIds: string[],
  vectorIds: string[],
  limit: number,
  k = 60
): string[]
```

**병합 로직:**

```
scores = Map<id, number>

// FTS 결과에서 RRF 점수 누적
for i in 0..ftsIds.length:
    scores[ftsIds[i]] += 1 / (k + i + 1)

// Vector 결과에서 RRF 점수 누적
for i in 0..vectorIds.length:
    scores[vectorIds[i]] += 1 / (k + i + 1)

// 점수 내림차순 정렬 후 limit개 반환
return sort(scores, desc).slice(0, limit).map(id)
```

### 4.3 k 파라미터의 의미

`k=60`은 표준적인 RRF 파라미터다.

- k가 크면: 순위 차이에 의한 점수 차이가 작아지고, 두 리스트의 결과가 더 균등하게 섞인다
- k가 작으면: 상위 순위 결과의 점수 차이가 커지고, 최상위 결과의 영향력이 강해진다

**예시 (k=60):**

```
1위 문서: 1 / (60 + 0 + 1) = 1/61 = 0.01639
2위 문서: 1 / (60 + 1 + 1) = 1/62 = 0.01613
10위 문서: 1 / (60 + 9 + 1) = 1/70 = 0.01429
```

두 리스트 모두에서 상위에 있는 문서는 점수가 합산되어 약 0.032가 되므로 단일 리스트에서만 1위인 문서(0.016)보다 높은 점수를 받는다.

### 4.4 route.ts에서의 적용

route.ts에서는 FTS/LIKE 검색 결과를 먼저 가져온 후 벡터 검색을 실행한다. 벡터 검색 결과가 있으면:

1. 기존 결과의 ID 배열을 `ftsIds`로 사용
2. `mergeSearchResults(ftsIds, vectorIds, limit)` 호출
3. 병합 결과에만 있는 ID를 추가로 DB에서 조회
4. 병합 순서대로 전체 결과를 재정렬

---

## 5. 인텐트 분류

### 5.1 5개 인텐트

| 인텐트 | 설명 | 예시 |
|--------|------|------|
| `entity` | 특정 파일, 식별자, 경로 등 구체적 대상 검색 | `UserService`, `src/lib/auth.ts` |
| `temporal` | 시간 기반 검색, 최근 변경 사항 | `recently updated`, `last week` |
| `relationship` | 연관 관계, 의존성 탐색 | `related to auth`, `depends on` |
| `aspect` | 규칙, 패턴, 관례 등 특정 관점 검색 | `testing conventions`, `best practice` |
| `exploratory` | 구조화되지 않은 탐색적 질의 (fallback) | `how does search work` |

### 5.2 감지 패턴

`classifySearchIntent(query)` 함수는 **규칙 기반** 분류기로, 우선순위대로 패턴을 검사한다.

**Entity 감지 (최우선):**

```
PATH_PATTERN        = /\//                          -- 경로 포함 ("src/lib")
IDENTIFIER_PATTERN  = /^[A-Z][a-zA-Z0-9]+$/         -- PascalCase
                    | /^[a-z]+(_[a-z]+)+$/           -- snake_case
FILE_EXT_PATTERN    = /\.\w{1,6}$/                   -- 파일 확장자 (".ts")
```

추가로, 3단어 이하이면서 질문형이 아니고 다른 패턴에도 매칭되지 않으면 entity로 분류한다.

**Temporal 감지:**

```
TEMPORAL_PATTERNS = /\b(recent(ly)?|latest|last\s+week|changed|
                       new(ly)?|updated|since|yesterday|today)\b/i
```

**Relationship 감지:**

```
RELATIONSHIP_PATTERNS = /\b(related\s+to|depends\s+on|connected|
                            linked|references|impacts|affects)\b/i
```

**Aspect 감지:**

```
ASPECT_PATTERNS = /\b(conventions?|rules?|patterns?|how\s+to|
                      best\s+practice|style|strategy)\b/i
```

Aspect는 정규식 패턴 외에 **ASPECT_TYPE_NAMES** set도 검사한다:

```
testing, architecture, coding_style, constraints, lessons_learned,
file_map, folder_structure, workflows, dependencies, deployment, security
```

쿼리에 이 이름들이 포함되면 `suggestedTypes` 필드에 해당 타입이 설정된다.

**Exploratory (fallback):**

위 패턴 어디에도 매칭되지 않으면 exploratory로 분류된다.

### 5.3 신뢰도 점수 (Confidence)

각 인텐트는 매칭된 패턴의 구체성에 따라 다른 confidence를 반환한다.

| 인텐트 | 조건 | confidence |
|--------|------|-----------|
| entity | 경로 패턴 | 0.9 |
| entity | PascalCase/snake_case 식별자 (단일 단어) | 0.85 |
| entity | 파일 확장자 | 0.8 |
| entity | 3단어 이하 비질문형 | 0.6 |
| temporal | 시간 패턴 매칭 | 0.85 |
| relationship | 관계 패턴 매칭 | 0.8 |
| aspect | 관점 패턴 매칭 | 0.75 |
| exploratory | fallback | 0.5 |

### 5.4 인텐트별 가중치

`INTENT_WEIGHTS` 테이블은 각 인텐트가 검색 파이프라인의 어떤 부분을 강조할지 정의한다.

| 인텐트 | ftsBoost | vectorBoost | recencyBoost | priorityBoost | graphBoost |
|--------|----------|-------------|--------------|---------------|------------|
| entity | 2.0 | 0.5 | 0.3 | 1.0 | 0 |
| temporal | 0.7 | 0.5 | **3.0** | 0.5 | 0 |
| relationship | 0.5 | 1.5 | 1.0 | 1.0 | **2.0** |
| aspect | 1.0 | **1.5** | 0.5 | 1.5 | 0 |
| exploratory | 1.0 | 1.2 | 1.0 | 1.0 | 0 |

설계 의도:
- **entity**: FTS가 2배 부스트. 정확한 키워드 매칭이 중요
- **temporal**: recency가 3배 부스트. 최근 업데이트된 메모리를 우선
- **relationship**: graph 연결이 2배 부스트. `relatedKeys`를 통한 관계 탐색
- **aspect**: vector 검색이 1.5배 부스트. 의미적 유사성이 중요
- **exploratory**: 모든 요소가 균등에 가까움

---

## 6. 관련성 점수 계산

`computeRelevanceScore()`는 검색과 독립적으로 각 메모리의 **사용 신호 기반 관련성 점수**를 0--100 범위로 계산한다.

### 6.1 전체 공식

```
relevanceScore = basePriority * usageFactor * timeFactor * feedbackFactor * pinBoost * MAX_SCORE
```

```
MAX_SCORE = 100
최종값 = min(100, max(0, round(raw * 100) / 100))
```

### 6.2 각 구성 요소

**basePriority:**

```
basePriority = max(priority, 1) / 100
```

- `priority` 필드는 0--100 정수. 0이어도 최소 1로 설정하여 전체 점수가 0이 되는 것을 방지
- 결과: 0.01 ~ 1.0 범위

**usageFactor:**

```
usageFactor = 1 + log(1 + accessCount)
```

- `accessCount`: 메모리 접근 횟수
- 로그 성장으로 반복 접근에 대해 **체감 수익(diminishing returns)** 적용
- 접근 0회: 1.0, 10회: ~3.4, 100회: ~5.6

**timeFactor:**

```
timeFactor = exp(-DECAY_RATE * daysSinceAccess)
```

```
DECAY_RATE = 0.03  (일 단위)
daysSinceAccess = (now - lastAccessedAt) / 86_400_000
```

- **지수 감쇠(exponential decay)** 모델
- 반감기: `ln(2) / 0.03 = ~23.1일` (23일 후 점수가 절반으로 감소)
- `lastAccessedAt`이 null이면 timeFactor = 1.0 (감쇠 없음)
- 예시: 7일 경과 = 0.81, 23일 경과 = 0.50, 60일 경과 = 0.17

**feedbackFactor:**

```
totalFeedback = helpfulCount + unhelpfulCount
if totalFeedback > 0:
    helpfulRatio = helpfulCount / totalFeedback
    feedbackFactor = 0.5 + helpfulRatio
else:
    feedbackFactor = 1.0
```

- 범위: 0.5 (모두 unhelpful) ~ 1.5 (모두 helpful)
- 피드백이 없으면 중립값 1.0

**pinBoost:**

```
PIN_BOOST = 1.5
pinBoost = pinnedAt !== null ? 1.5 : 1.0
```

- 고정(pin)된 메모리에 1.5배 승수 적용

### 6.3 관련성 등급 (Bucket)

```
score >= 60  -->  "excellent"
score >= 30  -->  "good"
score >= 10  -->  "fair"
score <  10  -->  "poor"
```

`computeRelevanceDistribution()`은 점수 배열을 받아 각 등급별 개수를 집계한다.

### 6.4 점수 시뮬레이션

| priority | accessCount | daysSince | helpful/unhelpful | pinned | score |
|----------|-------------|-----------|-------------------|--------|-------|
| 50 | 0 | 0 | 0/0 | no | 50.0 |
| 50 | 10 | 0 | 0/0 | no | ~170 -> 100 (capped) |
| 50 | 10 | 23 | 0/0 | no | ~85.1 |
| 10 | 5 | 30 | 3/1 | no | ~12.3 |
| 10 | 0 | 60 | 0/0 | yes | ~2.6 |
| 80 | 20 | 7 | 8/2 | yes | 100 (capped) |

---

## 7. 인텐트 가중 랭킹

route.ts에서 쿼리 검색 결과가 2개 이상일 때, 인텐트 가중치를 반영한 **복합 점수**로 재정렬한다. 이는 `computeRelevanceScore()`와 별개의 인라인 스코어링이다.

### 7.1 복합 점수 공식

```
_relevanceScore = priorityScore + accessScore + feedbackScore + recencyScore + pinBoost + graphScore
```

각 구성 요소:

```
priorityScore = priority * 0.3 * intentWeights.priorityBoost
accessScore   = min(25, accessCount * 2) * 0.2
feedbackScore = max(0, (helpfulCount - unhelpfulCount) * 3) * 0.15
recencyScore  = max(0, 25 - daysSinceAccess / 3) * 0.2 * intentWeights.recencyBoost
pinBoost      = isPinned ? 15 : 0
graphScore    = relatedKeys.length * intentWeights.graphBoost  (graphBoost > 0 일 때만)
```

여기서 `daysSinceAccess = (now - lastAccessedAt) / 86_400_000`이고, `lastAccessedAt`이 없으면 999로 설정한다.

### 7.2 인텐트별 영향

**Entity 인텐트:**
- `priorityScore = priority * 0.3 * 1.0` (기본)
- `recencyScore = ... * 0.2 * 0.3` (크게 감소 -- 시간은 중요하지 않음)
- `graphScore = 0` (관계 무시)

**Temporal 인텐트:**
- `priorityScore = priority * 0.3 * 0.5` (절반)
- `recencyScore = ... * 0.2 * 3.0` (3배 부스트)
- 추가로 **강제 시간순 재정렬**: 복합 점수 정렬 후 `updatedAt` 내림차순으로 다시 정렬

```typescript
if (resolvedIntent === "temporal") {
  scored.sort((a, b) => {
    const aTime = new Date(a.memory.updatedAt).getTime();
    const bTime = new Date(b.memory.updatedAt).getTime();
    return bTime - aTime;
  });
}
```

**Relationship 인텐트:**
- `graphScore = relatedKeys.length * 2.0` (관련 메모리가 많을수록 높은 점수)
- 추가로 **상위 5개 결과의 relatedKeys**를 추출하여 관련 메모리를 결과에 주입

```typescript
if (resolvedIntent === "relationship") {
  // 상위 5개 결과에서 relatedKeys 수집
  // DB에서 해당 key를 가진 메모리 조회 (최대 10개)
  // 기존 결과에 없으면 추가
}
```

### 7.3 최종 랭킹 파이프라인 (검색 시)

```
1. FTS5/LIKE 검색 -> 초기 결과
2. 인텐트 가중 복합 점수 정렬 (query && results.length > 1)
3. temporal이면 updatedAt 강제 재정렬
4. relationship이면 관련 메모리 주입
5. 벡터 검색 실행 -> RRF 병합으로 재정렬
6. computeRelevanceScore()로 각 메모리에 relevance_score 부여
7. sort === "relevance"이면 relevance_score 순 정렬
8. 응답 반환
```

주목할 점: 5단계(RRF 병합)가 2-4단계(인텐트 가중 정렬) 이후에 실행된다. 따라서 벡터 검색 결과가 있으면 **RRF 순서가 최종 정렬을 결정**하고, 없으면 인텐트 가중 복합 점수가 최종 정렬이 된다.

---

## 8. 조직 간 검색

### 8.1 엔드포인트

```
GET /api/v1/memories/search-org?q=...&limit=...
Header: X-Org-Slug
```

### 8.2 동작 방식

`search-org` 엔드포인트는 조직 내 모든 프로젝트를 대상으로 검색한다.

1. **인증 및 권한 확인**: `authenticateRequest()` + `requireOrgMembership()`
2. **접근 가능 프로젝트 필터링**: `getAccessibleProjectIds(userId, orgId, role)` -- role에 따라 접근 가능한 프로젝트 ID를 반환하고, `null`이면 모든 프로젝트에 접근 가능
3. **LIKE 기반 검색**: FTS나 벡터 검색을 사용하지 않고 `LIKE %query%`로 `key` + `content`를 검색
4. **프로젝트별 그룹핑**: 결과를 `projectSlug` 기준으로 그룹핑

### 8.3 응답 구조

```json
{
  "results": [
    {
      "key": "...",
      "contentPreview": "... (200자 제한)",
      "projectSlug": "...",
      "projectName": "...",
      "priority": 0,
      "tags": ["..."],
      "accessCount": 0,
      "updatedAt": "..."
    }
  ],
  "grouped": {
    "project-slug-1": [...],
    "project-slug-2": [...]
  },
  "projectsSearched": 3,
  "totalMatches": 15
}
```

### 8.4 프로젝트 내 검색과의 차이

| 항목 | 프로젝트 내 검색 (`/memories`) | 조직 간 검색 (`/search-org`) |
|------|-----|-----|
| 검색 범위 | 단일 프로젝트 + shared 메모리 | 조직 내 전체 프로젝트 |
| 검색 방식 | FTS5 + 벡터 검색 + RRF | LIKE 기반만 사용 |
| 인텐트 분류 | 적용 | 미적용 |
| 가중 랭킹 | 적용 | 미적용 |
| 결과 그룹핑 | 없음 | projectSlug 기준 |
| content 반환 | 전체 | 200자 preview |
| 중복 제거 | `Set<id>` 기반 | `Set<projectSlug::key>` 기반 |

---

## 9. 유사도 검색

### 9.1 엔드포인트

```
POST /api/v1/memories/similar
Body: { content: string, excludeKey?: string, threshold?: number }
```

### 9.2 용도

- **중복 검사 (dedup)**: 새 메모리 저장 전 유사한 기존 메모리가 있는지 확인
- **연결 제안 (linking)**: 관련 메모리를 `relatedKeys`로 연결할 후보 탐색
- CLI의 `check_duplicates` 액션에서 사용

### 9.3 동작 순서

**1차: 벡터 유사도 (preferred)**

```
1. generateEmbedding(content) -> queryEmbedding
2. 프로젝트 내 embedding이 있는 비아카이브 메모리 전체 조회
3. excludeKey가 있으면 해당 key 제외
4. 각 메모리와 cosineSimilarity() 계산
5. threshold(기본 0.6) 이상인 결과 필터링
6. similarity 내림차순 정렬, 최대 10개 반환
```

**2차: Jaccard Similarity (fallback)**

embedding이 없는 경우(모델 로드 실패 또는 embedding 미생성):

```
Jaccard(A, B) = |A intersection B| / |A union B|
```

```
extractWords(text):
  1. lowercase
  2. 특수문자를 공백으로 치환
  3. 공백 분할
  4. 2자 이하 단어 제거
  5. Set<string> 반환
```

### 9.4 threshold 값

| 용도 | 기본 threshold | 비고 |
|------|---------------|------|
| findSimilar API | 0.6 | 파라미터로 조절 가능 |
| vectorSearch (검색용) | 0.3 | 검색에서는 더 넓은 범위 |
| check_duplicates (CLI) | 0.6 | CLI에서 기본값 |

유사도 검색의 threshold(0.6)가 일반 벡터 검색(0.3)보다 높은 이유는 목적의 차이 때문이다. 유사도 검색은 실제로 의미적으로 동일하거나 매우 유사한 메모리를 찾는 것이 목적이고, 벡터 검색은 관련 결과를 폭넓게 수집하는 것이 목적이다.

### 9.5 응답 구조

```json
{
  "similar": [
    { "key": "auth/login-flow", "priority": 5, "similarity": 0.87 },
    { "key": "auth/session-mgmt", "priority": 3, "similarity": 0.72 }
  ]
}
```

---

## 10. 전체 플로우 트레이스

사용자가 `GET /api/v1/memories?q=testing+conventions`를 요청한 경우의 전체 실행 흐름이다.

### 단계 1: 요청 파싱

```
query = "testing conventions"
limit = 100 (기본값)
sortBy = "updated" (기본값)
intentParam = null
```

### 단계 2: 인텐트 분류

```
classifySearchIntent("testing conventions")

1. PATH_PATTERN.test("testing conventions") -> false
2. words.length === 1 -> false (2단어)
3. FILE_EXT_PATTERN -> false
4. words.length <= 3 -> true
   BUT ASPECT_PATTERNS.test("testing conventions") -> true ("conventions" 매칭)

결과: { intent: "aspect", confidence: 0.75, extractedTerms: ["testing", "conventions"] }
```

### 단계 3: 가중치 로드

```
getIntentWeights("aspect") =
  { ftsBoost: 1.0, vectorBoost: 1.5, recencyBoost: 0.5, priorityBoost: 1.5, graphBoost: 0 }

pBoost = 1.5
rBoost = 0.5
gBoost = 0
```

### 단계 4: FTS5 검색

```
ensureFts() -> FTS5 초기화 확인

ftsSearch(projectId, "testing conventions", 100):
  safeQuery = "testing conventions"
  ftsQuery = '"testing" OR "conventions"'

  SQL:
    SELECT m.id, rank
    FROM memories m JOIN memories_fts fts ON m.rowid = fts.rowid
    WHERE memories_fts MATCH '"testing" OR "conventions"'
      AND m.project_id = ?
      AND m.archived_at IS NULL
    ORDER BY rank
    LIMIT 100

  결과: ftsIds = ["mem_a1", "mem_b2", "mem_c3", ...]
```

### 단계 5: DB 조회

```
ftsIds가 존재하므로:
  SELECT * FROM memories WHERE id IN (ftsIds) AND archived_at IS NULL
  결과: results = [MemoryA, MemoryB, MemoryC, ...]
```

### 단계 6: 인텐트 가중 복합 점수

```
results.length > 1이므로 복합 점수 계산:

MemoryA (priority=8, accessCount=15, helpful=5, unhelpful=1, daysSince=3, pinned=false, relatedKeys=[]):
  priorityScore = 8 * 0.3 * 1.5 = 3.6
  accessScore   = min(25, 30) * 0.2 = 5.0
  feedbackScore = max(0, (5-1) * 3) * 0.15 = 1.8
  recencyScore  = max(0, 25 - 3/3) * 0.2 * 0.5 = 2.4
  pinBoost      = 0
  graphScore    = 0 (gBoost = 0)
  _relevanceScore = 12.8

MemoryB (priority=3, accessCount=2, helpful=0, unhelpful=0, daysSince=45, pinned=true, relatedKeys=[]):
  priorityScore = 3 * 0.3 * 1.5 = 1.35
  accessScore   = min(25, 4) * 0.2 = 0.8
  feedbackScore = 0
  recencyScore  = max(0, 25 - 45/3) * 0.2 * 0.5 = 1.0
  pinBoost      = 15
  _relevanceScore = 18.15

정렬: [MemoryB(18.15), MemoryA(12.8), ...]
```

### 단계 7: Temporal 재정렬 확인

```
resolvedIntent === "aspect" (temporal이 아니므로 스킵)
```

### 단계 8: Relationship 주입 확인

```
resolvedIntent === "aspect" (relationship이 아니므로 스킵)
```

### 단계 9: 벡터 검색 및 RRF 병합

```
vectorSearch(projectId, "testing conventions", 100):
  queryEmbedding = generateEmbedding("testing conventions")
    -> Float32Array(384) [0.023, -0.041, ...]

  프로젝트 내 embedding 있는 메모리 전체 조회
  각각 cosine similarity 계산
  threshold 0.3 이상 필터
  결과: vectorIds = ["mem_c3", "mem_d4", "mem_a1", ...]

mergeSearchResults(
  ftsIds=["mem_b2", "mem_a1", "mem_c3"],     // 인텐트 가중 정렬 순서
  vectorIds=["mem_c3", "mem_d4", "mem_a1"],
  limit=100,
  k=60
):
  scores:
    mem_b2: 1/61 = 0.01639
    mem_a1: 1/62 + 1/63 = 0.03200  (양쪽 모두 존재)
    mem_c3: 1/63 + 1/61 = 0.03228  (양쪽 모두 존재, vector 1위)
    mem_d4: 1/62 = 0.01613

  정렬: [mem_c3(0.03228), mem_a1(0.03200), mem_b2(0.01639), mem_d4(0.01613)]

mem_d4가 기존 results에 없으므로 DB에서 추가 조회
최종 results를 RRF 순서대로 재정렬
```

### 단계 10: computeRelevanceScore 부여

```
각 메모리에 대해 computeRelevanceScore() 호출:

MemoryC:
  basePriority = max(5, 1) / 100 = 0.05
  usageFactor = 1 + log(1 + 8) = 3.197
  timeFactor = exp(-0.03 * 10) = 0.741
  feedbackFactor = 1.0
  pinBoost = 1.0
  raw = 0.05 * 3.197 * 0.741 * 1.0 * 1.0 * 100 = 11.84
  relevance_score = 11.84

MemoryA:
  relevance_score = ...
```

### 단계 11: 최종 정렬 확인

```
sortBy === "updated" (기본값)이므로 relevance_score 기반 재정렬 없음
RRF 병합 순서가 최종 순서
```

### 단계 12: 응답

```json
{
  "memories": [
    { "id": "mem_c3", ..., "relevance_score": 11.84 },
    { "id": "mem_a1", ..., "relevance_score": 27.53 },
    { "id": "mem_b2", ..., "relevance_score": 42.10 },
    { "id": "mem_d4", ..., "relevance_score": 5.20 }
  ],
  "nextCursor": "mem_d4"
}
```

최종 정렬은 **RRF 병합 순서**이고, `relevance_score`는 참고용으로 각 메모리에 부착된다. 클라이언트가 `sort=relevance`를 명시적으로 요청하면 `relevance_score` 기준으로 재정렬된다.

---

## 부록: 설계 특성 정리

### Embedding 저장 전략

메모리 저장/수정 시 embedding 생성은 **fire-and-forget** 패턴으로 처리된다. 응답 반환 후 비동기로 embedding을 생성하여 DB에 저장한다. embedding 입력 텍스트는 `"${key} ${content} ${tags?.join(' ') ?? ''}"` 형태로 key, content, tags를 공백으로 연결한다.

### Shared 메모리 스코프

프로젝트 내 검색에서 `include_shared` 파라미터가 true(기본)이면 같은 조직 내 다른 프로젝트의 `scope: "shared"` 메모리도 검색 대상에 포함된다. 이를 통해 조직 차원의 공통 규칙이나 설정을 각 프로젝트에서 참조할 수 있다.

### 이중 스코어링 체계

memctl은 두 가지 독립적인 스코어링을 운영한다:

1. **인라인 복합 점수** (`_relevanceScore`): route.ts 내부에서 인텐트 가중치를 적용한 검색 시점 랭킹용. 선형 가산 모델
2. **computeRelevanceScore**: `packages/shared/src/relevance.ts`의 범용 관련성 점수. 곱셈 모델 (지수 감쇠 포함). 응답에 `relevance_score` 필드로 노출

두 점수의 구조적 차이:

| 항목 | 인라인 복합 점수 | computeRelevanceScore |
|------|-----------------|----------------------|
| 모델 | 가산적 (additive) | 곱셈적 (multiplicative) |
| 인텐트 가중치 | 적용 | 미적용 |
| 시간 감쇠 | 선형 (`25 - days/3`) | 지수 (`exp(-0.03 * days)`) |
| pinBoost | 고정값 15 | 배수 1.5 |
| 범위 | 비정규화 | 0--100 |
| 용도 | 검색 결과 내부 정렬 | API 응답 필드, 외부 소비용 |
