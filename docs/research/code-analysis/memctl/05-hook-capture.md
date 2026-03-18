# memctl -- 자동 캡처 (Hook) 분석

> 분석 대상 소스:
> - `packages/cli/src/hooks.ts` (569 lines)
> - `plugins/memctl/hooks/hooks.json` (53 lines)
> - `packages/cli/src/hook-adapter.ts` (689 lines) -- DISPATCHER_SCRIPT 참조

---

## 1. 훅 시스템 개요

### 1.1 hooks.json 이벤트 정의

`plugins/memctl/hooks/hooks.json`은 Claude Code의 hook 시스템에 등록되는 이벤트 바인딩을 정의한다. 총 4개의 lifecycle 이벤트에 5개의 hook 엔트리가 매핑되어 있다.

```json
{
  "hooks": {
    "SessionStart": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "${CLAUDE_PLUGIN_ROOT}/hooks/memctl-hook-dispatch.sh start"
          }
        ]
      },
      {
        "matcher": "compact",
        "hooks": [
          {
            "type": "command",
            "command": "${CLAUDE_PLUGIN_ROOT}/hooks/memctl-hook-dispatch.sh compact"
          }
        ]
      }
    ],
    "UserPromptSubmit": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "${CLAUDE_PLUGIN_ROOT}/hooks/memctl-hook-dispatch.sh user"
          }
        ]
      }
    ],
    "Stop": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "${CLAUDE_PLUGIN_ROOT}/hooks/memctl-hook-dispatch.sh assistant"
          }
        ]
      }
    ],
    "SessionEnd": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "${CLAUDE_PLUGIN_ROOT}/hooks/memctl-hook-dispatch.sh end"
          }
        ]
      }
    ]
  }
}
```

### 1.2 Lifecycle 이벤트 상세

| Claude Code 이벤트 | Dispatch Phase | 트리거 시점 | stdin 페이로드 |
|---|---|---|---|
| `SessionStart` | `start` | 새 세션 시작 | 없음 |
| `SessionStart` (compact) | `compact` | context compaction 후 재시작 | 없음 |
| `UserPromptSubmit` | `user` | 사용자가 프롬프트 제출 | `{"prompt": "사용자 메시지"}` |
| `Stop` | `assistant` | 에이전트 응답 완료 | `{"response": "에이전트 응답"}` |
| `SessionEnd` | `end` | 세션 종료 | `{"summary": "세션 요약"}` (선택적) |

### 1.3 Hook 데이터 흐름 (전체)

```
Claude Code (이벤트)
  |
  v
hooks.json (이벤트 -> 커맨드 매핑)
  |
  v
memctl-hook-dispatch.sh (bash dispatcher)
  |  +-- phase별 분기
  |  +-- JSON 페이로드 파싱
  |  +-- MEMCTL_REMINDER 주입 (start/user/compact)
  |  +-- memctl hook --stdin 호출
  |
  v
hooks.ts (runHookCommand)
  |  +-- parseHookPayload() -- 입력 파싱
  |  +-- handleHookStart() / handleHookTurn() / handleHookEnd()
  |
  v
[turn 경로만]
  extractHookCandidates()
    |  +-- splitLines()
    |  +-- isGenericCapabilityNoise()
    |  +-- isSelfContainedKnowledge()
    |  +-- classifyCandidate()
    |
    v
  findSimilar() -- 중복 검사
    |
    v
  storeMemory() -- 저장
    |
    v
  upsertSessionLog() -- 세션 로그 갱신
```

### 1.4 HookPayload 타입

`hooks.ts:8-18`에 정의:

```typescript
type HookPayload = {
  action: HookAction;          // "start" | "turn" | "end"
  sessionId?: string;
  userMessage?: string;
  assistantMessage?: string;
  summary?: string;
  keysRead?: string[];
  keysWritten?: string[];
  toolsUsed?: string[];
  forceStore?: boolean;
};
```

### 1.5 HookCandidate 타입

`hooks.ts:20-36`에 정의:

```typescript
type HookCandidate = {
  type:
    | "architecture"
    | "constraints"
    | "workflow"
    | "testing"
    | "lessons_learned"
    | "user_ideas"
    | "known_issues"
    | "decisions";
  title: string;
  content: string;
  id: string;
  priority: number;
  tags: string[];
  score: number;
};
```

hook 캡처가 생성할 수 있는 타입은 12개 builtin 중 8개로 제한되어 있다. `coding_style`, `folder_structure`, `file_map`, `branch_plan`은 자동 캡처 대상이 아니다 (이들은 에이전트가 명시적으로 저장하는 구조적 정보).

---

## 2. 후보 추출 파이프라인

### 2.1 extractHookCandidates() 전체 흐름

`hooks.ts:242-278`에 정의된 메인 추출 함수:

```
extractHookCandidates(payload)
  |
  v
[1] source 텍스트 조합
  |   userMessage + "\n" + assistantMessage
  |   (빈 문자열 필터링)
  |
  v
[2] splitLines(source)
  |   +-- \r\n -> \n 정규화
  |   +-- \n으로 라인 분할
  |   +-- 문장 경계 (. ! ?) 기준 추가 분할
  |   +-- sanitizeLine()으로 마크다운 정리
  |   +-- 20자 이상, 320자 이하 필터링
  |
  v
[3] 라인별 처리 루프
  |   for (const line of lines) {
  |     +-- isGenericCapabilityNoise(line) -> skip (forceStore 제외)
  |     +-- 중복 확인 (seen Set) -> skip
  |     +-- classifyCandidate(line) -> null이면 skip
  |     +-- HookCandidate 생성
  |   }
  |
  v
[4] score 내림차순 정렬
  |
  v
[5] 상위 MAX_HOOK_CANDIDATES(5)개 반환
```

### 2.2 splitLines() 상세

`hooks.ts:74-81`:

```typescript
function splitLines(value: string): string[] {
  return value
    .replace(/\r\n/g, "\n")                        // [1] CR+LF 정규화
    .split("\n")                                     // [2] 라인 분할
    .flatMap((line) => line.split(/(?<=[.!?])\s+/))  // [3] 문장 경계 분할
    .map((line) => sanitizeLine(line))               // [4] 마크다운 제거
    .filter((line) => line.length >= 20 && line.length <= 320); // [5] 길이 필터
}
```

단계별 처리 예시:

| 단계 | 입력/출력 |
|---|---|
| 원본 | `"- We decided to use Drizzle ORM. It handles migrations well.\n## Testing"` |
| [2] 라인 분할 | `["- We decided to use Drizzle ORM. It handles migrations well.", "## Testing"]` |
| [3] 문장 분할 | `["- We decided to use Drizzle ORM.", "It handles migrations well.", "## Testing"]` |
| [4] sanitize | `["We decided to use Drizzle ORM.", "It handles migrations well.", "Testing"]` |
| [5] 길이 필터 | `["We decided to use Drizzle ORM.", "It handles migrations well."]` -- "Testing" (7자)은 제거 |

### 2.3 sanitizeLine() 상세

`hooks.ts:66-72`:

```typescript
function sanitizeLine(value: string): string {
  return value
    .replace(/^[-*]\s+/, "")     // [1] 마크다운 리스트 접두사 제거 ("- ", "* ")
    .replace(/^#{1,6}\s+/, "")   // [2] 마크다운 헤딩 제거 ("## ", "### ")
    .replace(/`+/g, "")          // [3] 인라인 코드 백틱 제거
    .trim();                      // [4] 양쪽 공백 제거
}
```

### 2.4 isGenericCapabilityNoise()

`hooks.ts:83-96`에 정의된 노이즈 필터이다.

```typescript
function isGenericCapabilityNoise(content: string): boolean {
  const normalized = content.toLowerCase();
  const hasGenericCapabilityPhrase =
    /(scan(ning)? files?|search(ing)? (for )?patterns?|use (rg|ripgrep|grep)|
      read files?|find files?|use terminal commands?)/.test(normalized);
  const hasProjectSpecificSignal =
    /[/_-]/.test(normalized) ||
    /\b[a-z0-9_-]+\.[a-z0-9_-]+\b/.test(normalized) ||
    /(api|schema|migration|component|endpoint|workflow|billing|auth|branch|test|
      typescript|next\.js|drizzle|turso|docker|mcp|file|function|module|config|
      page|layout|server|client|database|query|type|interface|class|method|
      middleware|handler|service|model|controller)/.test(normalized);
  return hasGenericCapabilityPhrase && !hasProjectSpecificSignal;
}
```

판별 로직:

```
입력 텍스트
  |
  v
[조건 A] "일반적 능력 표현"이 포함되어 있는가?
  |  - "scanning files", "search for patterns"
  |  - "use rg", "use ripgrep", "use grep"
  |  - "read files", "find files"
  |  - "use terminal commands"
  |
  v
[조건 B] "프로젝트 고유 신호"가 포함되어 있는가?
  |  - 경로 구분자 (`/`, `_`, `-`)
  |  - 파일명 패턴 (예: `auth.ts`)
  |  - 기술 키워드 (api, schema, migration 등 30+개)
  |
  v
결과: A이면서 !B일 때만 노이즈로 판정
```

이 필터의 목적: 에이전트가 자신의 도구 사용 능력을 설명하는 문장 (예: "I can scan files using ripgrep")은 프로젝트 지식이 아니므로 캡처하지 않는다. 단, 프로젝트 고유 신호가 포함된 경우 (예: "I'll scan the auth module files")는 통과시킨다.

예시:

| 입력 | Generic? | Project-specific? | 결과 |
|---|---|---|---|
| `"I can scan files to find patterns"` | O | X | **노이즈 (제거)** |
| `"Scanning auth.ts for error patterns"` | O | O (`auth.ts`) | **통과** |
| `"Use ripgrep to search"` | O | X | **노이즈 (제거)** |
| `"Use ripgrep to search the API routes"` | O | O (`api`) | **통과** |
| `"Decided to use Drizzle ORM"` | X | - | **통과** (조건 A 불충족) |

### 2.5 isSelfContainedKnowledge()

`hooks.ts:104-131`에 정의된 자기 완결성(self-containedness) 검사이다.

이 함수의 핵심 목적: 원래 대화 컨텍스트 없이도 의미가 통하는 지식만 저장한다. "it", "that thing" 같은 대명사에 의존하는 문장은 나중에 읽었을 때 무의미하다.

#### 구체성 검사 (Concrete Specific)

다음 중 하나라도 매칭되면 구체적이라고 판단한다:

| 패턴 | 정규식 | 예시 |
|---|---|---|
| 파일 확장자 | `/[a-z0-9_-]+\.[a-z]{1,5}\b/` | `game.ts`, `route.ts`, `config.yaml` |
| 경로 세그먼트 | `/[a-z0-9_-]+\/[a-z0-9_-]+/` | `src/party`, `api/auth` |
| PascalCase/camelCase 식별자 | `/\b[A-Z][a-z]+[A-Z]/` | `UserService`, `getData` |
| snake_case 식별자 | `/\b[a-z]+_[a-z]+\b/` | `user_id`, `error_handler` |
| 에러 코드/상수 | `/\b(0x[0-9a-f]+\|E[A-Z]{2,}\|[A-Z_]{4,})\b/` | `0xff00`, `ENOENT`, `MAX_RETRY` |
| 인라인 코드 참조 | `` /`[^`]+`/ `` | `` `useState` ``, `` `api/v2` `` |
| 다중 하이픈 용어 | `/\b[a-z]+-[a-z]+-[a-z]+\b/` | `cursor-based-pagination` |
| 기술 명사 | `/(api\|endpoint\|schema\|migration\|component\|middleware\|database\|dockerfile\|webhook\|cron\|pipeline\|queue\|cache)\b/` | `database`, `middleware` |

#### 모호성 검사 (Vague References)

구체성 검사를 통과한 후, 모호한 대명사/참조어의 비율을 확인한다:

```typescript
const vagueRefs = (text.match(
  /\b(it|this|that|these|those|something|stuff|thing|things|
    what you|recently|somehow)\b/gi
) ?? []).length;
const words = text.split(/\s+/).filter(Boolean).length;
if (words > 0 && vagueRefs / words > 0.2) return false;
```

모호한 단어가 전체 단어의 20%를 초과하면 거부한다.

예시:

| 입력 | 구체성 | 모호 비율 | 결과 |
|---|---|---|---|
| `"The UserService handles authentication and session management"` | O (PascalCase) | 0/7 = 0% | **통과** |
| `"It does that thing with the stuff we talked about"` | X (구체 요소 없음) | - | **거부** (구체성 실패) |
| `"That thing in the database config is somehow broken"` | O (`database`) | 3/9 = 33% | **거부** (모호 비율 초과) |
| `"The auth.ts middleware validates JWT tokens"` | O (파일명) | 0/6 = 0% | **통과** |

---

## 3. 시맨틱 분류 상세

### 3.1 classifyCandidate() 구조

`hooks.ts:133-235`에 정의된 시맨틱 분류 함수이다.

#### 게이트 조건 (사전 필터)

```typescript
const words = text.split(/\s+/).filter(Boolean).length;
if (words < 8) return null;                        // [1] 최소 8단어
if (!isSelfContainedKnowledge(content)) return null; // [2] 자기 완결성
// [3] 하나 이상의 시맨틱 시그널 필요 (아래 7가지 중)
```

8단어 미만이거나 자기 완결적이지 않거나 어떤 시맨틱 시그널도 감지되지 않으면 `null`을 반환한다.

### 3.2 7가지 시맨틱 시그널

각 시그널은 독립적으로 검사되며, 복수 시그널이 동시에 감지될 수 있다. 마지막으로 감지된 시그널이 최종 `type`을 결정한다 (코드 순서상 후순위가 우선).

#### Signal 1: Decision

```typescript
const hasDecision =
  /\b(decided|decision|chose|chosen|opted|selected|tradeoff|trade-off|approach)\b/.test(text);
```

| 항목 | 값 |
|---|---|
| 키워드 | `decided`, `decision`, `chose`, `chosen`, `opted`, `selected`, `tradeoff`, `trade-off`, `approach` |
| 할당 type | `decisions` |
| priority | 72 |
| score 가산 | +4 |
| tag | `signal:decision` |

예시: `"We decided to use cursor-based pagination for the API endpoints"`

#### Signal 2: Constraint

```typescript
const hasConstraint =
  /\b(must|must not|cannot|can't|do not|required|requirement|should not|only)\b/.test(text);
```

| 항목 | 값 |
|---|---|
| 키워드 | `must`, `must not`, `cannot`, `can't`, `do not`, `required`, `requirement`, `should not`, `only` |
| 할당 type | `constraints` |
| priority | 78 |
| score 가산 | +4 |
| tag | `signal:constraint` |

예시: `"API responses must not include internal database IDs"`

#### Signal 3: Outcome

```typescript
const hasOutcome =
  /\b(fixed|implemented|added|updated|refactored|migrated|resolved|shipped|
      created|removed|changed|modified|deleted|moved|renamed|replaced|
      configured|deployed|installed|fixing|implementing|adding|updating|
      creating|removing|changing|modifying|deleting|renaming|replacing|
      configuring|deploying)\b/.test(text);
```

| 항목 | 값 |
|---|---|
| 키워드 | 과거형 20개 + 현재진행형 12개 = 32개의 행위 동사 |
| 할당 type | `workflow` (다른 시그널이 먼저 설정하지 않은 경우에만) |
| priority | 66 |
| score 가산 | +3 |
| tag | `signal:outcome` |

주의: outcome 시그널은 type이 아직 `workflow`(기본값)일 때만 type을 설정한다. 이미 다른 시그널이 type을 변경했으면 type은 변경하지 않고 score와 tag만 추가한다.

예시: `"Implemented cursor-based-pagination in the transactions endpoint"`

#### Signal 4: Issue

```typescript
const hasIssue =
  /\b(error|failed|failure|blocked|issue|bug|regression|not working|broke)\b/.test(text);
```

| 항목 | 값 |
|---|---|
| 키워드 | `error`, `failed`, `failure`, `blocked`, `issue`, `bug`, `regression`, `not working`, `broke` |
| 할당 type | `lessons_learned` |
| priority | **82** (최고) |
| score 가산 | **+5** (최고) |
| tag | `signal:issue` |

예시: `"The auth middleware failed when JWT tokens contained special characters"`

#### Signal 5: Testing

```typescript
const hasTesting = /\b(test|coverage|assert|vitest|jest|e2e)\b/.test(text);
```

| 항목 | 값 |
|---|---|
| 키워드 | `test`, `coverage`, `assert`, `vitest`, `jest`, `e2e` |
| 할당 type | `testing` |
| priority | 68 |
| score 가산 | +3 |
| tag | `signal:testing` |

예시: `"The vitest e2e suite must run against the staging database for auth tests"`

#### Signal 6: Idea

```typescript
const hasIdea =
  /\b(want to|should add|would be nice|idea:|feature request|enhancement|plan to add)\b/.test(text);
```

| 항목 | 값 |
|---|---|
| 키워드 | `want to`, `should add`, `would be nice`, `idea:`, `feature request`, `enhancement`, `plan to add` |
| 할당 type | `user_ideas` |
| priority | 64 |
| score 가산 | +3 |
| tag | `signal:idea` |

예시: `"We should add rate limiting to the webhook endpoint"`

#### Signal 7: Known Issue

```typescript
const hasKnownIssue =
  /\b(workaround|gotcha|caveat|known issue|breaks when|flaky|intermittent|hack:)\b/.test(text);
```

| 항목 | 값 |
|---|---|
| 키워드 | `workaround`, `gotcha`, `caveat`, `known issue`, `breaks when`, `flaky`, `intermittent`, `hack:` |
| 할당 type | `known_issues` |
| priority | 76 |
| score 가산 | +4 |
| tag | `signal:known-issue` |

예시: `"Gotcha: the Drizzle migration CLI breaks when the database URL contains special characters"`

### 3.3 시그널 우선순위와 오버라이드 순서

시그널 검사는 코드 순서대로 수행되며, 각 시그널이 true이면 `type`을 덮어쓴다. 따라서 코드 하단에 위치한 시그널이 최종 type을 결정할 확률이 높다.

실행 순서와 최종 type 결정:

```
[초기값]  type = "workflow", priority = 60, score = 0
  |
  v
[1] hasDecision?  -> type = "decisions",      priority = 72, score += 4
[2] hasConstraint? -> type = "constraints",    priority = 78, score += 4
[3] hasTesting?   -> type = "testing",         priority = 68, score += 3
[4] hasOutcome?   -> type 유지(workflow일때만), priority = 66, score += 3
[5] hasIdea?      -> type = "user_ideas",      priority = 64, score += 3
[6] hasKnownIssue? -> type = "known_issues",   priority = 76, score += 4
[7] hasIssue?     -> type = "lessons_learned",  priority = 82, score += 5
```

따라서 "issue" 시그널이 가장 높은 우선권을 가진다 (코드 마지막에 위치하므로 항상 다른 type을 덮어씀).

복합 시그널 예시:

| 문장 | 감지된 시그널 | 최종 type | 최종 priority | 총 score |
|---|---|---|---|---|
| `"We decided to add e2e tests for auth"` | decision, testing, outcome | `testing` (3번째) | 68 | 10 |
| `"Must fix the flaky migration test"` | constraint, testing, known_issue, issue | `lessons_learned` (7번째) | 82 | 16 |
| `"Added a workaround for the auth.ts bug"` | outcome, known_issue, issue | `lessons_learned` (7번째) | 82 | 12 |
| `"Want to add rate limiting, it's a known issue"` | idea, known_issue, issue | `lessons_learned` (7번째) | 82 | 12 |

### 3.4 전체 시그널 요약 테이블

| 시그널 | type | priority | score | 코드 순서 (오버라이드 강도) |
|---|---|---|---|---|
| decision | `decisions` | 72 | +4 | 1 (약) |
| constraint | `constraints` | 78 | +4 | 2 |
| testing | `testing` | 68 | +3 | 3 |
| outcome | `workflow` | 66 | +3 | 4 (type 변경 제한적) |
| idea | `user_ideas` | 64 | +3 | 5 |
| known_issue | `known_issues` | 76 | +4 | 6 |
| issue | `lessons_learned` | **82** | **+5** | **7 (최강)** |

---

## 4. 노이즈 필터링

### 4.1 필터링 파이프라인 전체

`extractHookCandidates()` 내에서 라인이 거부되는 경로:

```
라인 입력
  |
  +-- [Gate 1] splitLines()에서 길이 필터
  |     20자 미만 -> 제거
  |     320자 초과 -> 제거
  |
  +-- [Gate 2] isGenericCapabilityNoise()
  |     일반적 능력 표현 + 프로젝트 고유 신호 없음 -> 제거
  |     (forceStore=true이면 이 게이트 건너뜀)
  |
  +-- [Gate 3] 중복 검사 (seen Set)
  |     동일 문장(lowercase)이 이미 처리됨 -> 제거
  |
  +-- [Gate 4] classifyCandidate() 내부
  |     +-- 8단어 미만 -> null -> 제거
  |     +-- isSelfContainedKnowledge() 실패 -> null -> 제거
  |     +-- 시맨틱 시그널 0개 -> null -> 제거
  |
  +-- [Gate 5] MAX_HOOK_CANDIDATES(5) 초과
        score 하위 -> 잘림
```

### 4.2 거부 이유별 분류

| 거부 이유 | 게이트 | 예시 |
|---|---|---|
| 너무 짧음 (< 20자) | Gate 1 | `"Use Drizzle"`, `"Fixed bug"` |
| 너무 김 (> 320자) | Gate 1 | 긴 코드 블록, 긴 설명문 |
| 일반적 능력 진술 | Gate 2 | `"I can scan files to find patterns"` |
| 중복 문장 | Gate 3 | 동일 문장 반복 |
| 단어 수 부족 (< 8) | Gate 4-a | `"Auth module needs fixing soon"` (5단어) |
| 구체성 부족 | Gate 4-b | `"We should fix that thing somehow"` |
| 모호 비율 초과 (> 20%) | Gate 4-b | `"It does this and that for those things"` |
| 시맨틱 시그널 없음 | Gate 4-c | `"The UserService processes HTTP requests"` (행위 동사 없음) |
| 후보 수 초과 | Gate 5 | 6번째 이후 후보 |

### 4.3 forceStore 바이패스

`hooks.ts:258`:

```typescript
if (!forceStore && isGenericCapabilityNoise(line)) continue;
```

`forceStore: true`가 설정되면 Gate 2 (노이즈 필터)만 건너뛴다. 나머지 게이트 (길이, 중복, 자기 완결성, 시맨틱 시그널)는 여전히 적용된다.

---

## 5. 중복 검출

### 5.1 인메모리 중복 제거

`extractHookCandidates()` 내부에서 동일 턴 내의 중복을 제거한다:

```typescript
// hooks.ts:254,260-262
const seen = new Set<string>();
// ...
const normalized = line.toLowerCase();
if (seen.has(normalized)) continue;
seen.add(normalized);
```

대소문자를 무시한 정확 일치(exact match) 기반이다.

### 5.2 서버 측 유사도 검사

`handleHookTurn()` (`hooks.ts:446-489`)에서 저장 직전에 수행:

```typescript
// hooks.ts:461-466
const key = `agent/context/${candidate.type}/hook_${candidate.id}`;
let similarExists: boolean;
try {
  const similar = await client.findSimilar(candidate.content, key, 0.88);
  similarExists = similar.similar.some((s) => s.similarity >= 0.9);
} catch {
  similarExists = false;
}
if (similarExists) {
  skippedAsDuplicate.push(key);
  continue;
}
```

| 파라미터 | 값 | 설명 |
|---|---|---|
| `findSimilar` threshold | 0.88 | 서버에 전달되는 최소 유사도 임계값 |
| 실제 거부 기준 | 0.90 | 반환된 결과 중 0.9 이상이 하나라도 있으면 중복 |
| API 실패 시 | `similarExists = false` | 유사도 검사 실패하면 저장 진행 (안전 측) |

두 단계 임계값의 이유:
- 0.88로 후보군을 넓게 가져오되 (`findSimilar`의 검색 범위)
- 0.90으로 실제 거부 여부를 엄격하게 판단한다

### 5.3 중복 검출 전체 흐름

```
후보 1개
  |
  v
[1] 키 생성: agent/context/<type>/hook_<slugified_title>
  |
  v
[2] client.findSimilar(content, key, 0.88) 호출
  |   서버가 의미적 유사도 기반으로 유사 메모리 검색
  |
  v
[3] 반환된 유사 메모리 중 similarity >= 0.9인 것이 존재?
  |
  +-- Yes -> skippedAsDuplicate에 추가, 저장 건너뜀
  +-- No  -> storeMemory() 진행
```

---

## 6. 저장 플로우

### 6.1 handleHookTurn() 상세

`hooks.ts:446-518`에 정의된 턴 처리의 전체 흐름:

```
handleHookTurn(client, payload)
  |
  v
[1] resolveSessionId(payload.sessionId)
  |   +-- 명시적 ID > 파일 ID > 폴백 생성
  |
  v
[2] extractHookCandidates({
  |     userMessage, assistantMessage, forceStore
  |   })
  |   -> HookCandidate[] (최대 5개)
  |
  v
[3] 각 후보에 대해:
  |   +-- 키 생성: agent/context/<type>/hook_<id>
  |   +-- findSimilar() -> 중복 검사
  |   +-- 중복이면 skip, 아니면 storeMemory()
  |
  v
[4] 세션 로그 갱신 (저장/읽기/쓰기 키가 있을 때만)
  |
  v
[5] 결과 반환
```

### 6.2 키 형식

```
agent/context/<candidate.type>/hook_<candidate.id>
```

예시:

| 후보 내용 | type | id (slugified) | 최종 키 |
|---|---|---|---|
| `"We decided to use Drizzle ORM"` | `decisions` | `we_decided_to_use_drizzle_orm` | `agent/context/decisions/hook_we_decided_to_use_drizzle_orm` |
| `"Auth middleware failed on special chars"` | `lessons_learned` | `auth_middleware_failed_on_special_chars` | `agent/context/lessons_learned/hook_auth_middleware_failed_on_special_chars` |

`hook_` 접두사는 자동 캡처된 항목임을 표시한다. 이를 통해 에이전트가 명시적으로 저장한 항목과 구분할 수 있다.

### 6.3 storeMemory() 호출 상세

`hooks.ts:472-488`:

```typescript
await client.storeMemory(
  key,                          // agent/context/<type>/hook_<id>
  candidate.content,            // 원문 텍스트
  {
    scope: "agent_functionality",
    type: candidate.type,
    id: `hook_${candidate.id}`,
    title: candidate.title,
    source: "hook.turn",         // 자동 캡처 출처 표시
    capturedAt: new Date().toISOString(),
  },
  {
    priority: candidate.priority,
    tags: mergeUnique(candidate.tags, ["quality:high"]),
  },
);
```

메타데이터 구조:

| 필드 | 값 | 목적 |
|---|---|---|
| `scope` | `"agent_functionality"` | 에이전트 기능성 스코프 |
| `type` | 시맨틱 분류 결과 | 컨텍스트 타입 |
| `id` | `hook_<slugified_title>` | 고유 식별자 |
| `title` | 72자 이내 제목 | 사람이 읽을 수 있는 제목 |
| `source` | `"hook.turn"` | 자동 캡처 출처 |
| `capturedAt` | ISO 8601 타임스탬프 | 캡처 시점 |

태그:

| 태그 | 출처 |
|---|---|
| `hook:auto` | 모든 자동 캡처 항목에 기본 부여 |
| `signal:decision` / `signal:constraint` / ... | 감지된 시맨틱 시그널 |
| `quality:high` | 모든 저장 항목에 추가 |

### 6.4 titleFromContent()

`hooks.ts:237-240`:

```typescript
function titleFromContent(content: string): string {
  const trimmed = content.replace(/\.$/, "");
  return trimmed.length <= 72 ? trimmed : `${trimmed.slice(0, 69)}...`;
}
```

- 마침표 제거
- 72자 이하면 전체 사용
- 72자 초과면 69자 + `...`으로 절단

### 6.5 slugify()

`hooks.ts:57-64`:

```typescript
function slugify(value: string): string {
  const slug = value
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "_")   // 비영숫자 -> 언더스코어
    .replace(/^_+|_+$/g, "")        // 양쪽 언더스코어 제거
    .slice(0, 64);                   // 최대 64자
  return slug || "note";             // 빈 문자열이면 "note"
}
```

---

## 7. 세션 로그 갱신

### 7.1 세션 ID 해석 (resolveSessionId)

`hooks.ts:403-413`:

```
resolveSessionId(explicit?)
  |
  +-- explicit 값이 있음?
  |     +-- readSessionFile() 확인
  |     +-- 파일 내용 === explicit -> source: "file" (MCP managed)
  |     +-- 파일 내용 !== explicit -> source: "explicit"
  |
  +-- explicit 없음?
        +-- readSessionFile() 성공 -> source: "file"
        +-- 파일 없음 -> generateFallbackSessionId() -> source: "fallback"
```

| Source | 의미 | MCP managed? |
|---|---|---|
| `"file"` | MCP 서버가 관리하는 세션 | O |
| `"explicit"` | 외부에서 명시적으로 전달 | X |
| `"fallback"` | 폴백 생성 (hook-<timestamp>-<random>) | X |

### 7.2 세션 스냅샷 조회

`hooks.ts:379-396`의 `getSessionSnapshot()`:

```typescript
async function getSessionSnapshot(client: ApiClient, sessionId: string) {
  const logs = await client.getSessionLogs(50);
  const found = logs.sessionLogs.find((log) => log.sessionId === sessionId);
  if (!found) return { keysRead: [], keysWritten: [], toolsUsed: [] };
  return {
    keysRead: found.keysRead ? JSON.parse(found.keysRead) : [],
    keysWritten: found.keysWritten ? JSON.parse(found.keysWritten) : [],
    toolsUsed: found.toolsUsed ? JSON.parse(found.toolsUsed) : [],
  };
}
```

최근 50개 세션 로그에서 현재 세션 ID를 찾아 기존 키 목록을 가져온다. 이 정보는 세션 로그 갱신 시 기존 데이터와 병합하기 위해 필요하다.

### 7.3 세션 로그 업데이트

`hooks.ts:492-506`:

```typescript
if (storedKeys.length > 0 || (payload.keysRead?.length ?? 0) > 0 ||
    (payload.keysWritten?.length ?? 0) > 0) {
  const current = await getSessionSnapshot(client, resolved.sessionId);
  await client.upsertSessionLog({
    sessionId: resolved.sessionId,
    keysRead: mergeUnique(current.keysRead, payload.keysRead ?? []),
    keysWritten: mergeUnique(current.keysWritten, [
      ...(payload.keysWritten ?? []),
      ...storedKeys,                    // 이번 턴에서 자동 저장된 키들
    ]),
    toolsUsed: mergeUnique(current.toolsUsed, [
      ...(payload.toolsUsed ?? []),
      "hook.turn",                      // hook 사용 기록
    ]),
  });
}
```

`mergeUnique()` (`hooks.ts:375-377`)는 두 배열을 중복 없이 병합한다:

```typescript
function mergeUnique(first: string[] = [], second: string[] = []): string[] {
  return [...new Set([...first, ...second])];
}
```

업데이트 조건: 저장된 키, 읽은 키, 쓴 키 중 하나라도 있을 때만 세션 로그를 갱신한다. 아무 변경도 없으면 API 호출을 건너뛴다.

### 7.4 세션 종료 시 로그

`hooks.ts:520-554`의 `handleHookEnd()`:

```
handleHookEnd(client, payload)
  |
  v
resolveSessionId()
  |
  +-- MCP managed (source === "file")?
  |     -> MCP 서버가 세션 종료 처리 -> 여기서는 아무것도 하지 않음
  |     -> { action: "end", mcpManaged: true }
  |
  +-- MCP managed 아님?
        -> getSessionSnapshot() -> 현재 상태 조회
        -> summary 결정 (payload.summary 또는 자동 생성)
        -> upsertSessionLog({
        |     sessionId, summary,
        |     keysRead, keysWritten, toolsUsed,
        |     endedAt: Date.now()
        |   })
        -> { action: "end", mcpManaged: false, summary }
```

자동 생성 summary 형식:

```
Hook session ended. <N> key(s) written, <M> key(s) read.
```

### 7.5 handleHookTurn() 응답 구조

```json
{
  "action": "turn",
  "sessionId": "hook-m1abc-xyz123",
  "mcpManaged": false,
  "extracted": 3,
  "stored": 2,
  "storedKeys": [
    "agent/context/decisions/hook_use_drizzle_orm",
    "agent/context/constraints/hook_no_internal_ids_in_api"
  ],
  "skippedAsDuplicate": 1,
  "skippedAsLowSignal": false
}
```

| 필드 | 의미 |
|---|---|
| `extracted` | 후보 추출 수 (classifyCandidate 통과) |
| `stored` | 실제 저장된 수 (중복 제외) |
| `storedKeys` | 저장된 메모리 키 목록 |
| `skippedAsDuplicate` | 유사도 검사로 건너뛴 수 |
| `skippedAsLowSignal` | 후보가 0개인 경우 true |

---

## 8. 한계점 분석

### 8.1 키워드 휴리스틱의 근본적 한계

memctl의 hook 캡처 시스템은 LLM을 사용하지 않고 순수 정규식 기반 키워드 매칭으로 동작한다. 이는 의도적 설계 결정이지만, 다음과 같은 한계를 수반한다.

#### 8.1.1 False Negative (놓치는 지식)

키워드에 의존하므로, 동일한 의미를 다른 표현으로 전달하면 캡처되지 않는다:

| 놓치는 사례 | 이유 |
|---|---|
| `"We went with PostgreSQL instead of MongoDB"` | `decided`, `chose` 등의 키워드 없음 (decision 미감지) |
| `"This API always returns JSON"` | `must`, `required` 등의 키워드 없음 (constraint 미감지) |
| `"The build breaks on M1 Macs occasionally"` | `breaks when`은 있지만 `breaks on`은 패턴에 없음 |
| `"Remember: never call the payment API in dev mode"` | `never`는 constraint 키워드 목록에 없음 |

#### 8.1.2 False Positive (잘못 캡처하는 지식)

키워드 존재만으로 판단하므로, 맥락과 무관하게 캡처될 수 있다:

| 잘못 캡처되는 사례 | 이유 |
|---|---|
| `"The test file already has the correct assertion setup"` | `test`, `assert` -> testing 시그널 감지 |
| `"We only need to update the package.json version field"` | `only`, `update` -> constraint + outcome 감지 |
| `"This error message should not be shown to users"` | `error`, `should not` -> issue + constraint 감지 |

#### 8.1.3 시그널 오버라이드 문제

마지막 시그널이 type을 결정하는 구조 때문에, 의미적으로 더 정확한 이전 시그널이 무시될 수 있다:

```
"We decided to add a workaround for the flaky auth test"
  |
  +-- hasDecision: true  -> type = "decisions" (priority 72)
  +-- hasOutcome: true   -> type stays (workflow일 때만)
  +-- hasKnownIssue: true -> type = "known_issues" (priority 76)
  +-- hasTesting: true   -> [검사 순서상 3번째이므로 known_issues에 의해 덮어씌워짐]
  +-- hasIssue 키워드 없음
  |
  v
최종: type = "known_issues"
```

이 문장은 실제로는 "decision"에 가깝지만 `known_issues`로 분류된다.

### 8.2 LLM 추출과의 비교

| 측면 | 키워드 휴리스틱 (현재) | LLM 기반 추출 (대안) |
|---|---|---|
| 지연 시간 | 매우 낮음 (< 1ms) | 높음 (1-10초) |
| 비용 | 0 (로컬 계산) | API 호출 비용 |
| 정확도 | 중간 (패턴 의존) | 높음 (의미 이해) |
| 재현성 | 결정적 (동일 입력 = 동일 출력) | 비결정적 |
| 컨텍스트 이해 | 단일 문장만 분석 | 대화 전체 흐름 이해 가능 |
| 배치 처리 | 불필요 | 필요 (토큰 제한) |
| 오프라인 동작 | 가능 | 불가능 |

### 8.3 문장 단위 분석의 한계

`splitLines()`에서 문장 경계 기준으로 분할하므로, 여러 문장에 걸쳐 표현되는 지식은 캡처되지 않는다:

```
원문: "After extensive testing, we found that the connection pool
       needs to be limited to 10 connections. This is because the
       staging database has a hard limit of 15, and we need
       headroom for admin connections."

splitLines() 결과:
  [1] "After extensive testing, we found that the connection pool
       needs to be limited to 10 connections"
  [2] "This is because the staging database has a hard limit of 15,
       and we need headroom for admin connections"

분석:
  [1] -> constraint("needs to be limited") + testing("testing")
         -> 저장됨 (type: constraints)
  [2] -> isSelfContainedKnowledge 실패 ("This is because"는 구체적이지만
         "staging database"가 concrete specific을 통과할 수 있음)
         -> 저장될 수도 있지만, 근거가 [1]과 분리됨
```

결정과 그 근거가 별도 메모리 항목으로 분리되어 저장될 수 있다.

### 8.4 320자 상한의 영향

```typescript
.filter((line) => line.length >= 20 && line.length <= 320);
```

320자는 약 50-60 단어에 해당한다. 상세한 기술적 결정이나 복잡한 workaround 설명은 이 한계를 쉽게 초과하여 캡처되지 않는다. 그러나 이 제한이 없으면 코드 블록이나 긴 로그 출력이 캡처되는 문제가 발생할 수 있으므로, 의도적 트레이드오프이다.

### 8.5 MAX_HOOK_CANDIDATES 제한

```typescript
const MAX_HOOK_CANDIDATES = 5;
```

한 턴에서 최대 5개의 후보만 저장한다. 대화 한 턴에 많은 결정이나 교훈이 포함된 경우 (예: 코드 리뷰 피드백), 하위 score 항목은 잘린다. score 기준 정렬 후 상위 5개만 취하므로, issue (score +5)가 idea (score +3)보다 항상 우선된다.

### 8.6 잠재적 개선 방향

현재 구현에서 관찰되는 개선 가능 지점들:

| 영역 | 현재 상태 | 개선 가능성 |
|---|---|---|
| `never` 키워드 | constraint 패턴에 미포함 | `never` 추가 시 constraint 감지율 향상 |
| `breaks on` 패턴 | `breaks when`만 감지 | `breaks (on\|when\|if\|with)` 로 확장 |
| 복수 문장 통합 | 지원하지 않음 | 인접 문장 결합 후 분석 |
| 시그널 오버라이드 | 마지막 시그널이 우선 | 점수 가중치 기반 선택으로 전환 |
| 코드 참조 감지 | 없음 | 함수명/변수명 패턴 인식 추가 |
| outcome 타입 | workflow 고정 (제한적) | 문맥에 따라 architecture/testing 등으로 분류 |

---

## 참조 파일 경로 요약

| 파일 | 역할 |
|---|---|
| `packages/cli/src/hooks.ts` | hook CLI 진입점, 후보 추출, 분류, 저장, 세션 관리 |
| `plugins/memctl/hooks/hooks.json` | Claude Code hook 이벤트 정의 |
| `packages/cli/src/hook-adapter.ts` | DISPATCHER_SCRIPT bash 생성, MEMCTL_REMINDER, 에이전트별 설정 |
| `packages/cli/src/agent-context.ts` | 타입 시스템, 키 구조, 메모리 레코드 (hook과 공유) |
