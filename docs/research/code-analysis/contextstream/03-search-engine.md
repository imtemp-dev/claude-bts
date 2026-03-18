# ContextStream -- 검색 엔진 분석

## 개요

ContextStream의 검색 엔진은 인덱싱된 코드베이스와 메모리에 대해 8가지 검색 모드를 제공하며, 사용자의 쿼리 특성에 따라 최적의 모드를 자동으로 선택하는 스마트 라우팅 시스템을 포함한다. 통합 도구(consolidated tool) 구조에서는 단일 `search` 도구가 기존의 `search_semantic`, `search_hybrid`, `search_keyword`, `search_pattern` 등 개별 도구를 대체하며, `mode` 파라미터로 검색 방식을 전환한다.

핵심 소스 파일:
- `src/tools.ts` -- 검색 도구 등록, 모드 추천 로직, 실행 디스패치, 폴백 체인
- `src/client.ts` -- API 엔드포인트 호출 (`/search/*`)
- `src/hooks/pre-tool-use.ts` -- PreToolUse 훅에서 discovery 패턴 가로채기

---

## 1. 8가지 검색 모드

ContextStream은 다음 8가지 검색 모드를 지원한다. 각 모드는 고유한 API 엔드포인트에 매핑되며, 서로 다른 검색 전략을 사용한다.

### 1.1 semantic

- **API 엔드포인트**: `POST /search/semantic`
- **설명**: 벡터 임베딩 기반의 의미론적 검색. 쿼리의 의미적 유사성을 기준으로 결과를 반환한다.
- **사용 사례**: 자연어 질의, 개념적 검색. "how does authentication work?" 같은 서술형 질문에 적합하다.
- **클라이언트 호출**: `client.searchSemantic(params)` -- `search_type: "semantic"`을 body에 포함하여 전송한다.
- **자동 선택 조건**: 쿼리가 question word(how, what, where, why 등)로 시작하거나, `?`로 끝나거나, 단어 수가 3개 이상인 경우.

### 1.2 hybrid

- **API 엔드포인트**: `POST /search/hybrid`
- **설명**: 시맨틱 검색과 키워드 검색을 결합한 하이브리드 방식. 의미 유사성과 정확한 키워드 매칭을 모두 활용한다.
- **사용 사례**: 범용 검색. 특별한 패턴이 감지되지 않을 때 기본(default) 모드로 사용된다. `mode="auto"` 에서 다른 조건에 해당하지 않으면 hybrid가 선택된다.
- **클라이언트 호출**: `client.searchHybrid(params)` -- `search_type: "hybrid"`.
- **특이 사항**: 이전 버전과의 호환을 위해 `hybrid`는 `auto`의 backward-compatible alias로도 작동한다.

### 1.3 keyword

- **API 엔드포인트**: `POST /search/keyword`
- **설명**: 정확한 키워드 매칭 기반 검색. 인덱스에서 문자열 일치를 기준으로 결과를 반환한다.
- **사용 사례**: 따옴표로 감싼 정확한 문자열 검색. `"handleAuth"` 같은 쿼리.
- **클라이언트 호출**: `client.searchKeyword(params)` -- `search_type: "keyword"`.
- **자동 선택 조건**: 쿼리가 큰따옴표(`"..."`) 또는 작은따옴표(`'...'`)로 감싸져 있는 경우 (`extractQuotedLiteral()`이 값을 반환할 때).

### 1.4 pattern

- **API 엔드포인트**: `POST /search/pattern`
- **설명**: 정규 표현식(regex) 또는 glob 패턴 기반 검색.
- **사용 사례**: `import.*from\s+['"]react['"]` 같은 정규식 패턴 검색. 파일 경로 패턴 탐색.
- **클라이언트 호출**: `client.searchPattern(params)` -- `search_type: "pattern"`.
- **자동 선택 조건**: 쿼리에 regex 문자(`^$+{}[]|()\`)가 포함되거나 glob 패턴(`*`, `?`)이 감지된 경우. `hasRegexCharacters()` 또는 `isGlobLike()` 함수가 true를 반환할 때.

### 1.5 exhaustive

- **API 엔드포인트**: `POST /search/exhaustive`
- **설명**: 인덱스 전체에서 모든 일치 항목을 반환하는 완전 검색. grep과 유사한 완전성을 제공한다. `index_freshness` 필드를 통해 결과의 신뢰도를 함께 반환한다.
- **사용 사례**: "모든 사용처 찾기(find all occurrences)", "모든 매치 보기(all matches)" 같은 완전성이 요구되는 검색.
- **클라이언트 호출**: `client.searchExhaustive(params)` -- `search_type: "exhaustive"`.
- **자동 선택 조건**: 쿼리에 `ALL_MATCH_KEYWORDS` 패턴이 포함된 경우. 예: "all occurrences", "find all", "every usage", "all matches", "every occurrence", "all usages".

### 1.6 refactor

- **API 엔드포인트**: `POST /search/refactor`
- **설명**: 리팩토링 및 심볼 리네이밍에 최적화된 검색. 단어 경계(word-boundary) 매칭을 사용하여 정밀한 심볼 검색을 제공한다. 결과는 파일별로 그룹화되며 라인/컬럼 위치를 포함한다.
- **사용 사례**: 변수명 변경, 함수명 일괄 치환 등. `isIdentifierQuery()`가 true인 경우 -- 즉 쿼리가 단일 단어이며 camelCase, snake_case, ALL_CAPS 패턴인 경우.
- **클라이언트 호출**: `client.searchRefactor(params)` -- `search_type: "refactor"`.
- **자동 선택 조건**: 쿼리가 identifier 형태일 때 (공백 없는 단일 토큰, mixed case 또는 underscore 포함, 3자 이상 ALL_CAPS). `isIdentifierQuery()` 함수로 판별한다.

### 1.7 crawl

- **API 엔드포인트**: `POST /search/crawl`
- **설명**: 심층 멀티모달 검색. 더 큰 후보 풀에서 깊이 있는 검색을 수행한다.
- **사용 사례**: workspace의 `default_search_mode`가 `crawl`로 설정된 경우 자동 활성화. 대규모 코드베이스에서의 포괄적 검색에 적합하다.
- **클라이언트 호출**: `client.searchCrawl(params)`.
- **자동 선택 조건**: `normalizeConfiguredSearchMode()`을 통해 workspace/project의 `default_search_mode`가 `crawl`로 설정되어 있을 때.

### 1.8 team

- **구현**: 별도 API 엔드포인트 없음. 다수의 workspace에 대해 `hybrid` 검색을 순차적으로 실행하고 결과를 병합한다.
- **설명**: 팀 구독(Team plan) 전용. 여러 프로젝트/워크스페이스에 걸친 교차 검색을 수행한다.
- **사용 사례**: "team-wide", "cross-project", "across projects" 같은 키워드가 포함된 쿼리.
- **제한**: `client.isTeamPlan()`이 true인 경우에만 사용 가능. 그렇지 않으면 에러를 반환한다.
- **자동 선택 조건**: `TEAM_QUERY_KEYWORDS` 중 하나가 쿼리에 포함된 경우. 키워드: "team-wide", "teamwide", "cross-project", "cross project", "across projects", "all workspaces", "all projects".

### 출력 형식(output_format)

모든 검색 모드는 4가지 출력 형식을 지원한다:

| format | 설명 | 토큰 절감 |
|--------|------|-----------|
| `full` | 기본값. 콘텐츠 전체 포함 | -- |
| `paths` | 파일 경로만 반환 | ~80% |
| `minimal` | 압축된 형태 | ~60% |
| `count` | 매치 수만 반환 | ~90% |

`suggestOutputFormat()` 함수는 쿼리 특성에 따라 적절한 출력 형식을 자동 추천한다. count 쿼리("how many", "count of" 등)에는 `count`를, identifier 쿼리에는 `paths` 또는 `minimal`을 추천한다.

---

## 2. 스마트 모드 선택 -- recommendSearchMode()

`recommendSearchMode(query)` 함수는 주어진 쿼리 문자열을 분석하여 최적의 검색 모드와 선택 이유를 반환한다. 반환 타입은 `{ mode: string; reason: string }`이다.

### 2.1 기본 모드 결정

workspace 또는 project 수준에서 `default_search_mode`가 설정되어 있으면 해당 값을 기본값으로 사용한다. 설정이 없으면 `"hybrid"`가 기본 폴백 모드이다. `normalizeConfiguredSearchMode()`는 `sessionManager.getDefaultSearchMode()`에서 가져온 값을 8가지 유효한 모드 중 하나로 정규화한다.

### 2.2 20+ 조건 평가 순서

아래는 `recommendSearchMode()` 내부의 조건 평가 순서이다. 첫 번째로 매칭되는 조건이 모드를 결정한다:

1. **빈 쿼리**: `resolvedDefault` 모드 반환. "Defaulted to fallback mode for broad discovery."
2. **팀 쿼리**: `isTeamQuery(lower)` -- `TEAM_QUERY_KEYWORDS` 중 매칭 시 `"team"` 반환
3. **전체 매칭 쿼리**: `isAllMatchesQuery(lower)` -- `ALL_MATCH_KEYWORDS`("all occurrences", "find all", "every usage" 등) 매칭 시 `"exhaustive"` 반환
4. **따옴표 리터럴**: `extractQuotedLiteral(trimmed)` -- 큰따옴표/작은따옴표로 감싼 쿼리 시 `"keyword"` 반환
5. **정규식/glob 패턴**: `isGlobLike(trimmed) || hasRegexCharacters(trimmed)` -- `"pattern"` 반환
6. **crawl 기본 모드**: `crawlIsDefault === true`이면 `"crawl"` 반환
7. **식별자 쿼리**: `isIdentifierQuery(trimmed)` -- 공백 없는 camelCase/snake_case/ALL_CAPS 토큰 시 `"refactor"` 반환
8. **자연어 질의**: question word로 시작하거나, `?`로 끝나거나, 단어 수 >= 3 시 `"semantic"` 반환
9. **폴백**: 위 조건 없으면 `"hybrid"` 반환. "Hybrid mode provides balanced coverage."

### 2.3 보조 분류 함수

- `isIdentifierQuery(query)`: 공백 없는 2자 이상, `[A-Za-z0-9_:]`만 포함, mixed case/underscore/ALL_CAPS 중 하나 충족
- `hasRegexCharacters(query)`: `^$+{}[]|()\` 포함 여부. 단, 쿼리 끝에만 `?`가 있는 경우는 제외(자연어 질문으로 간주)
- `isGlobLike(query)`: `*` 포함 또는 중간에 `?` 포함
- `isCountQuery(queryLower)`: "how many", "count", "count of", "number of", "total" 프리픽스 확인
- `isAllMatchesQuery(queryLower)`: 6개의 all-match 키워드 확인
- `isTeamQuery(queryLower)`: 7개의 team-related 키워드 확인
- `isDocLookupQuery(query)`: 문서 관련 키워드 + 조회 동사 조합 감지 (코드 관련 확장자나 경로가 포함되면 제외)

### 2.4 hybrid에서 semantic으로의 폴백 (confidence < 0.35)

`shouldRetrySemanticFallback()` 함수는 다음 조건을 모두 만족할 때 hybrid 결과를 semantic으로 재시도한다:

```
조건:
1. 현재 모드가 "hybrid"
2. recommendSearchMode(query).mode === "semantic" (자연어로 판정)
3. hybrid 결과가 0건이거나 최고 점수 < HYBRID_LOW_CONFIDENCE_SCORE (0.35)
```

재시도 후 `shouldPreferSemanticResults()`가 semantic 결과의 최고 점수가 hybrid 최고 점수보다 `SEMANTIC_SWITCH_MIN_IMPROVEMENT` (0.08) 이상 높으면 semantic 결과를 채택한다.

이 메커니즘은 `HYBRID_LOW_CONFIDENCE_SCORE = 0.35` 상수로 제어된다. hybrid 검색이 자연어 쿼리에 대해 낮은 신뢰도 결과를 반환하면 semantic 검색이 더 나은 결과를 제공할 가능성이 높기 때문이다.

---

## 3. 검색 실행 플로우

### 3.1 executeSearchMode() 디스패치

`executeSearchMode(mode, params)` 함수는 주어진 모드에 따라 적절한 client 메서드를 호출한다:

```typescript
async function executeSearchMode(
  mode: "semantic" | "hybrid" | "keyword" | "pattern" | "exhaustive" | "refactor" | "crawl",
  params: ReturnType<typeof normalizeSearchParams>
): Promise<any> {
  switch (mode) {
    case "hybrid":    return client.searchHybrid(params);
    case "semantic":  return client.searchSemantic(params);
    case "keyword":   return client.searchKeyword(params);
    case "pattern":   return client.searchPattern(params);
    case "exhaustive": return client.searchExhaustive(params);
    case "refactor":  return client.searchRefactor(params);
    case "crawl":     return client.searchCrawl(params);
    default:          return client.searchHybrid(params);
  }
}
```

`team` 모드는 `executeSearchMode()`를 거치지 않고 `runSearchForMode()` 내에서 별도 처리된다.

### 3.2 normalizeSearchParams()

검색 파라미터를 정규화한다. 주요 기본값:

| 파라미터 | 기본값 | 범위 |
|----------|--------|------|
| `limit` | `DEFAULT_SEARCH_LIMIT` (env: `CONTEXTSTREAM_SEARCH_LIMIT`, 기본 3) | 1-100 |
| `content_max_chars` | `DEFAULT_SEARCH_CONTENT_MAX_CHARS` (env: `CONTEXTSTREAM_SEARCH_MAX_CHARS`, 기본 400) | 50-10000 |
| `context_lines` | undefined | 0-10 |
| `exact_match_boost` | undefined | 1-10 |

### 3.3 통합 search 도구의 전체 실행 흐름

`search` 통합 도구가 호출되면 다음 단계를 거친다:

1. **인증 확인**: `getSearchAuthError()` -- API 키 또는 JWT 존재 여부 확인
2. **변경 파일 인덱싱**: `client.checkAndIndexChangedFiles()` -- fire-and-forget
3. **모드 결정**: `mode="auto"`이면 `recommendSearchMode(query)`로 자동 선택
4. **프로젝트 ID 후보 목록 구성**: 다음 순서로 후보 프로젝트를 수집
   - explicit project_id (사용자 입력)
   - folder_path에서 해석된 project_id (`.contextstream/config.json`)
   - local index에서 매핑된 project_id (`~/.contextstream/indexed-projects.json`)
   - session에서 가져온 project_id
   - workspace 전체 범위 (project_id = undefined)
5. **후보 순회 검색**: `candidateProjectIds`를 순서대로 시도, 결과가 있으면 즉시 중단
6. **폴백 체인**: 각 모드별로 결과가 0건이면 다른 모드로 자동 재시도 (아래 3.4절 참조)
7. **문서 폴백**: `isDocLookupQuery()`가 true이고 검색 결과 0건이면 `findDocsFallback()`으로 ContextStream 메모리의 docs를 조회
8. **결과 조립 및 반환**: 결과 수, 모드 정보, 폴백 노트, 전체 결과 데이터를 조합

### 3.4 자동 폴백 체인

`runSearchForMode()` 함수는 초기 검색 결과가 비어 있을 때 다단계 폴백을 수행한다:

**hybrid 모드 폴백:**
- hybrid 결과가 저신뢰(< 0.35) && 자연어 쿼리 -> semantic 재시도
- semantic 점수가 hybrid보다 0.08 이상 높으면 채택

**keyword 모드 폴백 (결과 0건 시, 따옴표 쿼리):**
1. 따옴표 제거 후 keyword 재시도
2. 리터럴을 regex로 이스케이프하여 pattern 재시도
3. exhaustive 재시도

**keyword 모드 폴백 (결과 0건 시, identifier 쿼리):**
1. refactor 재시도 (단어 경계 매칭)
2. exhaustive 재시도

**keyword 모드 폴백 (결과 0건 시, 자연어):**
1. semantic 재시도
2. hybrid 폴백 (최후 수단)

**refactor/exhaustive 모드 폴백 (결과 0건 시):**
1. keyword 재시도

모든 폴백 시도에서 발생하는 에러는 무시하고(catch empty) 원래 결과를 유지한다.

---

## 4. 검색 규칙 리마인더 -- SEARCH_RULES_REMINDER

### 4.1 목적

AI 어시스턴트는 대화가 길어지면 초기 지시사항을 점진적으로 "잊어버리는" 현상(instruction decay)을 보인다. ContextStream은 이를 방지하기 위해 `context` 도구 및 `init` 도구의 응답에 검색 우선 사용 규칙을 반복 주입한다.

### 4.2 리마인더 내용

```
[SEARCH] Use search(mode="auto") before Glob/Grep/Read/Explore/Task/EnterPlanMode.
Never use EnterPlanMode or Task(Explore) for file-by-file discovery. Local tools only if 0 results.
```

### 4.3 활성화/비활성화

- 환경 변수 `CONTEXTSTREAM_SEARCH_REMINDER=false`로 비활성화 가능
- `SEARCH_RULES_REMINDER_ENABLED` 상수로 체크
- 기본적으로 활성화 상태

### 4.4 주입 위치

- `context` 도구 응답의 footer 영역 (`searchRulesLine`)
- `init` 도구 응답에도 동일하게 주입
- `user-prompt-submit.ts` 훅에서도 유사한 규칙을 프롬프트에 주입:

```
SEARCH-FIRST: Use mcp__contextstream__search(mode="auto") before
Glob/Grep/Read/Explore/Task/EnterPlanMode. In planning, never use
EnterPlanMode or Task(Explore) for file-by-file discovery.
```

### 4.5 컨텍스트 호출 리마인더

검색 규칙 외에 `context()` 호출 자체를 강제하는 리마인더도 존재한다:

```
[CONTEXT] Call context(user_message="...") at start of EVERY response. This is MANDATORY.
```

이 리마인더는 `CONTEXT_CALL_REMINDER` 상수로 정의되며, `context` 도구 응답에 항상 포함된다.

---

## 5. Pre-Tool-Use 훅 연동

### 5.1 개요

`src/hooks/pre-tool-use.ts`는 Claude Code, Cursor, Cline/Roo/Kilo 등 다양한 AI 에디터에서 도구가 실행되기 전에 가로채는(intercept) 훅이다. 인덱싱된 프로젝트에서 discovery 성격의 로컬 도구 호출을 감지하면 ContextStream search로 리다이렉트한다.

### 5.2 동작 조건

**전제 조건**: 프로젝트가 인덱싱되어 있어야 한다. `isProjectIndexed(cwd)`가 `~/.contextstream/indexed-projects.json` 파일을 읽어 현재 작업 디렉토리가 인덱싱된 프로젝트에 속하는지 확인한다. 인덱싱되지 않은 프로젝트에서는 모든 로컬 도구를 허용한다.

**stale 임계값**: 인덱싱 후 7일(`STALE_THRESHOLD_DAYS`)이 경과하면 stale로 판정한다.

### 5.3 가로채는 도구와 판정 기준

| 도구 | 가로채기 조건 | 리다이렉트 메시지 |
|------|-------------|------------------|
| **Glob** | `isDiscoveryGlob(pattern)` -- `**/*`, `**/`, `src/**`, `**/*.ts` 등 broad 패턴 | `search(mode="auto", query="<pattern>")` 사용 권장 |
| **Grep/Search** | 패턴이 있고 `filePath`가 없거나 broad인 경우 | `search(mode="auto", query="<pattern>")` 또는 `search(mode="keyword")` |
| **Explore** | 무조건 | `search(mode="auto", output_format="paths")` 사용 권장 |
| **Task(Explore)** | `subagent_type`이 "explore" 포함 | `search(mode="auto")` 사용 권장 |
| **Task(Plan)/EnterPlanMode** | `subagent_type`이 "plan" 포함 또는 EnterPlanMode | `search` + `session(action="capture_plan")` 조합 권장 |
| **list_files/search_files** | discovery glob/grep 패턴 감지 시 | `search(mode="auto", query="<pattern>")` 사용 권장 |

### 5.4 isDiscoveryGlob() / isDiscoveryGrep() 판정

`isDiscoveryGlob(pattern)`:
- `DISCOVERY_PATTERNS` 리스트: `["**/*", "**/", "src/**", "lib/**", "app/**", "components/**"]`
- `**/*.` 또는 `**/`로 시작하는 패턴
- `**` 또는 `*/`를 포함하는 모든 패턴

`isDiscoveryGrep(filePath)`:
- 경로가 `.`, `./`, `*`, `**` 이거나
- `*` 또는 `**`를 포함하는 경우

### 5.5 에디터 포맷별 응답

PreToolUse 훅은 3가지 에디터 포맷을 자동 감지하여 각각에 맞는 응답을 생성한다:

- **Claude Code**: `hookSpecificOutput.additionalContext`에 메시지 주입 (hard block 대신 guidance)
- **Cline/Roo/Kilo**: `{ cancel: true, errorMessage: "...", contextModification: "..." }` JSON
- **Cursor**: `{ decision: "deny", reason: "..." }` JSON

### 5.6 init/context 호출 강제

PreToolUse 훅은 프로젝트 인덱싱 여부와 무관하게 다음 상태를 추적한다:

- `isInitRequired(cwd)`: 세션에서 `init` 도구가 아직 호출되지 않은 경우 다른 ContextStream 도구 호출을 차단하고 `init` 호출을 요구
- `isContextRequired(cwd)`: 각 프롬프트 시작 시 `context` 도구가 호출되지 않은 경우 차단. 단, `init`과 read-only ContextStream 연산은 bypass 허용 (context가 fresh하고 state 변경이 없는 경우)

상태 관리는 `prompt-state.ts`에서 수행하며, `markStateChanged(cwd)`, `isContextFreshAndClean(cwd, 120)` 등으로 freshness를 120초 기준으로 판단한다.

---

## 6. 팀 검색

### 6.1 동작 방식

team 모드는 `executeSearchMode()`를 통하지 않고 `runSearchForMode()` 함수 내에서 별도 분기로 처리된다:

1. `client.isTeamPlan()` 확인 -- false이면 구독 업그레이드 메시지와 함께 에러 반환
2. `client.listTeamWorkspaces({ page_size: 100 })`으로 팀 워크스페이스 목록 조회
3. 최대 10개 워크스페이스에 대해 각각 `client.searchHybrid()` 실행
4. 모든 결과를 score 기준 내림차순으로 정렬
5. 각 결과에 `workspace_name`과 `workspace_id`를 첨부하여 출처를 표시
6. `input.limit`(기본 20)만큼 잘라서 반환

### 6.2 per-workspace limit

전체 limit를 워크스페이스 수로 나눈 값을 각 워크스페이스별 limit로 사용한다:

```typescript
const perWorkspaceLimit = input.limit
  ? Math.ceil(input.limit / Math.min(workspacesForSearch.length, 10))
  : 5;
```

### 6.3 에러 처리

개별 워크스페이스 검색 실패는 무시하고(catch empty) 다음 워크스페이스로 진행한다. 전체 실패 시에만 에러를 반환한다.

---

## 7. 검색 제안 -- searchSuggestions()

### 7.1 도구 등록

`search_suggestions` 도구는 부분 쿼리를 기반으로 검색 제안을 반환한다:

```typescript
registerTool("search_suggestions", {
  title: "Search suggestions",
  description: "Get search suggestions based on partial query",
  inputSchema: z.object({
    query: z.string(),
    workspace_id: z.string().uuid().optional(),
    project_id: z.string().uuid().optional(),
  }),
}, async (input) => {
  const result = await client.searchSuggestions(input);
  return { content: [{ type: "text", text: formatContent(result) }] };
});
```

### 7.2 API 호출

`client.searchSuggestions(body)`는 서버에 `{ query, workspace_id, project_id }` 를 전달하고, 서버 측에서 자동완성 및 관련 쿼리 제안을 반환한다. 클라이언트 측에서는 추가적인 쿼리 가공을 수행하지 않는다.

---

## 8. 문서 폴백 메커니즘

검색 결과가 0건이고 쿼리가 문서 조회 패턴(`isDocLookupQuery()`)에 해당하면, `findDocsFallback()` 함수가 ContextStream 메모리에 저장된 docs를 조회한다:

1. `candidateProjectIds`를 순회하며 `client.docsList()`를 호출
2. `rankDocsForQuery()`로 쿼리 키워드와 문서 제목/내용 간 매칭 점수를 계산
3. 점수가 0보다 큰 문서를 limit 수만큼 반환
4. 결과가 있으면 `[memory_docs_fallback]` 섹션으로 사용자에게 표시

`tokenizeForDocMatch()`는 stop words("the", "and", "for", "docs", "list", "show", "find" 등)를 제거하고 3자 이상의 토큰만 추출한다. `scoreDocMatch()`는 문서 제목과 내용에서 각 토큰의 존재 여부를 카운트하여 점수를 산출한다.

---

## 9. 보조 유틸리티

### 9.1 extractSearchEnvelope()

API 응답에서 결과 배열과 총 수를 추출하는 헬퍼 함수:

```typescript
function extractSearchEnvelope(result: any): { results: any[]; total: number } {
  const data = result?.data ?? result ?? {};
  const results = Array.isArray(data?.results) ? data.results : [];
  const total = typeof data?.total === "number" ? data.total : results.length;
  return { results, total };
}
```

### 9.2 maxResultScore()

검색 결과 중 최고 score를 반환한다. 폴백 판정에 사용된다.

### 9.3 escapeRegexLiteral()

리터럴 문자열을 regex 패턴으로 안전하게 변환한다. keyword 검색에서 pattern 폴백 시 사용된다.

### 9.4 indexedProjectIdForFolder()

`~/.contextstream/indexed-projects.json`에서 주어진 폴더 경로에 매핑된 프로젝트 ID를 반환한다. 7일 이내에 인덱싱된 프로젝트만 유효하다. 경로 매칭은 정확 일치 또는 상위/하위 디렉토리 관계를 허용하며, 가장 긴 경로 매칭을 우선한다.

---

## 10. 환경 변수 정리

| 환경 변수 | 용도 | 기본값 |
|-----------|------|--------|
| `CONTEXTSTREAM_SEARCH_LIMIT` | 검색 결과 기본 limit | 3 |
| `CONTEXTSTREAM_SEARCH_MAX_CHARS` | 결과별 최대 content 문자 수 | 400 |
| `CONTEXTSTREAM_SEARCH_REMINDER` | 검색 규칙 리마인더 활성화 | true (`"false"`로 비활성화) |
| `CONTEXTSTREAM_CONSOLIDATED` | 통합 도구 모드 | true (`"false"`로 비활성화) |
| `CONTEXTSTREAM_HOOK_ENABLED` | PreToolUse 훅 활성화 | true (`"false"`로 비활성화) |
| `CONTEXTSTREAM_SHOW_TIMING` | 검색 응답에 소요 시간 표시 | false |
