# memctl -- 세션 관리 분석

## 목차

1. [세션 트래커 구조](#1-세션-트래커-구조)
2. [세션 생성](#2-세션-생성)
3. [세션 시작](#3-세션-시작)
4. [API 호출 추적](#4-api-호출-추적)
5. [Claims 메커니즘](#5-claims-메커니즘)
6. [세션 종료](#6-세션-종료)
7. [주기적 플러시](#7-주기적-플러시)
8. [핸드오프](#8-핸드오프)
9. [Memo 시스템](#9-memo-시스템)
10. [프로세스 종료 처리](#10-프로세스-종료-처리)

---

## 1. 세션 트래커 구조

> 소스: `packages/cli/src/session-tracker.ts:17-33`

`SessionTracker`는 단일 에이전트 세션의 전체 상태를 관리하는 핵심 타입이다. MCP 서버가 생성될 때 인스턴스가 만들어지며, 세션 종료 시점까지 모든 API 활동과 도구 사용 이력을 추적한다.

### 타입 정의

```typescript
export type SessionTracker = {
  sessionId: string;
  branch: string | null;
  handoff: SessionHandoff | null;
  readKeys: Set<string>;
  writtenKeys: Set<string>;
  toolActions: Set<string>;
  areas: Set<string>;
  apiCallCount: number;
  dirty: boolean;
  closed: boolean;
  startedAt: number;
  lastActivityAt: number;
  endedExplicitly: boolean;
  bootstrapped: boolean;
  bootstrapHintShown: boolean;
};
```

### 필드 상세

| 필드 | 타입 | 설명 |
|------|------|------|
| `sessionId` | `string` | 세션 고유 식별자. `auto-{base36 timestamp}-{random 6자}` 형식 |
| `branch` | `string \| null` | 현재 Git branch 이름. `startSessionLifecycle()`에서 비동기로 감지 |
| `handoff` | `SessionHandoff \| null` | 직전 세션의 요약 정보. 세션 간 컨텍스트 이전에 사용 |
| `readKeys` | `Set<string>` | 세션 중 읽은 memory key 집합 |
| `writtenKeys` | `Set<string>` | 세션 중 작성/수정한 memory key 집합 |
| `toolActions` | `Set<string>` | 호출된 tool.action 조합 (예: `memory.search`, `context.bootstrap`) |
| `areas` | `Set<string>` | 접근한 API 영역 (URL 경로의 첫 번째 세그먼트: `memories`, `contexts` 등) |
| `apiCallCount` | `number` | 총 API 호출 횟수 (health, session-logs 제외) |
| `dirty` | `boolean` | 마지막 flush 이후 변경이 있는지 나타내는 플래그 |
| `closed` | `boolean` | 세션 종료 처리 완료 여부 (중복 종료 방지) |
| `startedAt` | `number` | 세션 시작 시각 (`Date.now()`) |
| `lastActivityAt` | `number` | 마지막 API 활동 시각 |
| `endedExplicitly` | `boolean` | 에이전트가 `session.end` 도구를 호출하여 명시적으로 종료했는지 여부 |
| `bootstrapped` | `boolean` | `context.bootstrap`가 실행되었는지 여부 |
| `bootstrapHintShown` | `boolean` | bootstrap 미실행 힌트가 이미 표시되었는지 여부 |

### Set 사용 이유

`readKeys`, `writtenKeys`, `toolActions`, `areas` 모두 `Set<string>` 타입이다. 동일한 key에 대한 반복 접근을 중복 없이 추적하기 위함이며, 최종 요약(summary) 생성 시 `[...set].sort()`로 정렬된 배열로 변환한다.

---

## 2. 세션 생성

> 소스: `packages/cli/src/session-tracker.ts:35-55`

### createSessionTracker()

```typescript
export function createSessionTracker(): SessionTracker {
  const now = Date.now();
  const rand = Math.random().toString(36).slice(2, 8);
  return {
    sessionId: `${AUTO_SESSION_PREFIX}-${now.toString(36)}-${rand}`,
    // ... 초기값 설정
  };
}
```

### 세션 ID 생성 형식

| 구성 요소 | 형식 | 예시 |
|-----------|------|------|
| Prefix | 고정 문자열 | `auto` |
| Timestamp | `Date.now().toString(36)` (base-36 인코딩) | `m3k7xp9c` |
| Random | `Math.random().toString(36).slice(2, 8)` (6자리 랜덤) | `f7k2nq` |
| 최종 형식 | `auto-{timestamp}-{random}` | `auto-m3k7xp9c-f7k2nq` |

base-36 인코딩을 사용하는 이유는 타임스탬프를 짧고 URL-safe한 문자열로 압축하기 위함이다. 13자리 밀리초 타임스탬프가 8자 정도의 문자열로 줄어든다.

### 초기 상태

모든 Set은 비어 있고, `apiCallCount`는 0, `dirty`는 `false`, `closed`는 `false`로 시작한다. `branch`와 `handoff`는 `null`로 초기화되며, `startSessionLifecycle()` 내부의 비동기 처리에서 채워진다.

### 호출 지점

`createSessionTracker()`는 `server.ts:104`의 `createServer()` 함수에서 호출된다:

```typescript
// packages/cli/src/server.ts:104
const tracker = createSessionTracker();
```

MCP 서버 인스턴스당 하나의 tracker가 생성되며, 이 tracker는 모든 도구 핸들러와 API 클라이언트에 공유된다.

---

## 3. 세션 시작

> 소스: `packages/cli/src/session-tracker.ts:286-380`

### startSessionLifecycle()

이 함수는 세션의 전체 생명주기를 초기화한다. 동기적 부분과 비동기적 부분으로 나뉜다.

```
startSessionLifecycle(client, tracker)
    |
    +-- [동기] writeSessionFile(tracker.sessionId)
    |       .memctl/hooks/session_id 파일 생성
    |
    +-- [비동기 IIFE] (async () => { ... })()
    |       |
    |       +-- getBranchInfo() -> tracker.branch 설정
    |       |
    |       +-- client.upsertSessionLog() -> 서버에 초기 세션 로그 생성
    |       |
    |       +-- client.getSessionLogs(5) -> 최근 5개 세션 조회
    |       |
    |       +-- Stale session cleanup (2시간 초과)
    |       |
    |       +-- Build handoff from most recent session
    |
    +-- setInterval(flushSession, 30_000)
    |       30초마다 주기적 플러시
    |
    +-- process.once("beforeExit" | "SIGINT" | "SIGTERM")
            종료 시 finalizeSession 호출
    |
    +-- return { cleanup: () => void }
```

### 세션 파일 생성

> 소스: `packages/cli/src/session-tracker.ts:272-282`

```typescript
const SESSION_FILE_DIR = join(".memctl", "hooks");
const SESSION_FILE_PATH = join(SESSION_FILE_DIR, "session_id");
```

세션 파일은 `.memctl/hooks/session_id`에 기록된다. 이 파일은 hook dispatcher가 현재 활성 세션 ID를 알 수 있도록 하기 위한 것이다. `mkdirSync`로 디렉토리를 재귀적으로 생성하고, `writeFileSync`로 세션 ID를 동기적으로 기록한다. 비동기 생명주기 초기화가 완료되기 전에도 hooks가 세션 ID를 읽을 수 있어야 하므로 동기적 쓰기가 중요하다.

### Branch 감지

> 소스: `packages/cli/src/agent-context.ts:354-388`

`getBranchInfo()`는 `git rev-parse --abbrev-ref HEAD` 등의 git 명령을 실행하여 현재 branch 정보를 수집한다:

| 필드 | 출처 | 설명 |
|------|------|------|
| `branch` | `git rev-parse --abbrev-ref HEAD` | 현재 branch 이름 |
| `branchKeyId` | `encodeURIComponent(branch)` | URL 인코딩된 branch 식별자 |
| `commit` | `git rev-parse HEAD` | 현재 commit SHA |
| `dirty` | `git status --porcelain` | 워킹 디렉토리 변경 여부 |
| `upstream` | `git rev-parse --abbrev-ref ... @{upstream}` | 추적 중인 upstream branch |
| `ahead`/`behind` | `git rev-list --left-right --count` | upstream 대비 ahead/behind 커밋 수 |

branch 감지 실패 시(`.catch(() => null)`) `tracker.branch`는 `null`로 유지된다.

### Stale Session Cleanup (2시간 비활성 세션 정리)

> 소스: `packages/cli/src/session-tracker.ts:306-331`

```typescript
const TWO_HOURS_MS = 2 * 60 * 60 * 1000;
const now = Date.now();
for (const log of recentSessions.sessionLogs) {
  if (log.endedAt) continue;                              // 이미 종료된 세션 스킵
  if (log.sessionId === tracker.sessionId) continue;       // 현재 세션 스킵
  const lastActivity = parseTimestamp(log.lastActivityAt)
    || parseTimestamp(log.startedAt)
    || 0;
  if (!lastActivity || now - lastActivity < TWO_HOURS_MS) continue;  // 2시간 이내면 스킵
  // Auto-close
  await client.upsertSessionLog({
    sessionId: log.sessionId,
    summary: log.summary || "Auto-closed: session exceeded 2-hour inactivity limit.",
    endedAt: now,
  });
}
```

정리 로직의 흐름:

```
최근 5개 세션 로그 조회
    |
    +-- for each log:
           |
           +-- endedAt 존재? -> 스킵
           +-- 현재 세션? -> 스킵
           +-- lastActivityAt 또는 startedAt 파싱
           +-- (현재시각 - lastActivity) < 2시간? -> 스킵
           +-- upsertSessionLog(endedAt: now) 호출하여 자동 종료
```

`parseTimestamp()` 함수(`session-tracker.ts:168-175`)는 숫자와 ISO 8601 문자열 모두를 처리한다.

### 에러 복구

비동기 초기화 전체가 try-catch로 감싸져 있다. 실패 시 최소한 `upsertSessionLog({ sessionId })` 호출을 시도하여 서버에 세션 로그가 존재하도록 보장한다. 이는 best-effort 방식으로, 네트워크 오류 시에도 CLI가 정상 작동하도록 한다.

---

## 4. API 호출 추적

> 소스: `packages/cli/src/session-tracker.ts:66-166`

모든 API 호출은 `ApiClient`의 `onRequest` 콜백을 통해 `trackApiCall()`로 전달된다.

### trackApiCall() 흐름

> 소스: `packages/cli/src/session-tracker.ts:146-166`

```
trackApiCall(tracker, method, path, body)
    |
    +-- getAreaFromPath(path) -> area 추출
    |       path에서 첫 번째 세그먼트 (예: "memories", "contexts")
    |       "health" 또는 "session-logs"이면 전체 스킵 (return)
    |
    +-- tracker.apiCallCount += 1
    +-- tracker.lastActivityAt = Date.now()
    +-- area가 있으면 tracker.areas.add(area)
    |
    +-- extractKeyFromPath(method, path)
    |       URL 경로에서 memory key 추출
    |
    +-- extractKeyFromBody(method, path, body)
    |       요청 body에서 memory key 추출
    |
    +-- readKey가 있으면 tracker.readKeys.add(readKey)
    +-- writtenKey가 있으면 tracker.writtenKeys.add(writtenKey)
    +-- tracker.dirty = true
```

### 호출 위치

> 소스: `packages/cli/src/server.ts:112-114`

```typescript
const client = new ApiClient({
  ...config,
  onRequest: ({ method, path, body }) => {
    trackApiCall(tracker, method, path, body);
  },
});
```

`ApiClient`의 모든 HTTP 요청마다 콜백이 호출되므로, 도구 핸들러가 아닌 내부 호출(periodic flush, lifecycle 등)도 추적된다.

### extractKeyFromPath()

> 소스: `packages/cli/src/session-tracker.ts:87-113`

URL 경로 패턴 `/memories/{key}`에서 memory key를 추출한다.

| HTTP Method | 결과 |
|-------------|------|
| `GET` | `{ readKey: key }` |
| `POST` | `{ writtenKey: key }` |
| `PATCH` | `{ writtenKey: key }` |
| `DELETE` | `{ writtenKey: key }` |
| 기타 | `{}` |

경로 정규식: `/^\/memories\/([^/]+)$/`

key는 `decodeURIComponent()`로 디코딩되며, `shouldTrackMemoryKey()` 필터를 통과해야 한다.

### extractKeyFromBody()

> 소스: `packages/cli/src/session-tracker.ts:115-144`

body에서 key를 추출하는 경우 두 가지:

| 조건 | 경로 | Body 필드 | 결과 |
|------|------|-----------|------|
| `POST /memories` | `/memories` | `body.key` | `{ writtenKey: key }` |
| `POST /memories/bulk` | `/memories/bulk` | `body.keys[0]` (첫 번째) | `{ readKey: first }` |

bulk 조회의 경우 첫 번째 key만 추적하는 이유는 간결성을 위한 것이다. 완전한 추적보다는 세션 요약에 대략적인 정보를 제공하는 것이 목적이다.

### shouldTrackMemoryKey() 필터링

> 소스: `packages/cli/src/session-tracker.ts:80-85`

```typescript
export function shouldTrackMemoryKey(key: string): boolean {
  if (!key) return false;
  if (key.startsWith("agent/claims/")) return false;
  if (key.startsWith("auto:")) return false;
  return true;
}
```

| 조건 | 추적 여부 | 이유 |
|------|-----------|------|
| 빈 문자열 | 제외 | 유효하지 않은 key |
| `agent/claims/` prefix | 제외 | 세션 claim 메타데이터이므로 사용자에게 무의미 |
| `auto:` prefix | 제외 | 자동 생성 key이므로 세션 요약에 불필요 |
| 기타 모든 key | 추적 | 사용자 관련 memory |

### getAreaFromPath()

> 소스: `packages/cli/src/session-tracker.ts:73-78`

```typescript
export function getAreaFromPath(path: string): string | null {
  const clean = getPathWithoutQuery(path).replace(/^\/+/, "");
  if (!clean) return null;
  const area = clean.split("/")[0];
  return area || null;
}
```

query string을 제거하고 선행 슬래시를 벗긴 뒤, 첫 번째 경로 세그먼트를 반환한다. 예: `/memories/some-key?q=1` -> `memories`.

### recordToolAction()

> 소스: `packages/cli/src/session-tracker.ts:57-64`

```typescript
export function recordToolAction(
  tracker: SessionTracker,
  tool: string,
  action: string,
): void {
  tracker.toolActions.add(`${tool}.${action}`);
  tracker.dirty = true;
}
```

각 도구 핸들러의 `onToolCall` 콜백에서 호출되어, `tool.action` 형식의 문자열을 `toolActions` Set에 추가한다. 예: `session.end`, `memory.search`, `context.bootstrap`.

---

## 5. Claims 메커니즘

> 소스: `packages/cli/src/tools/handlers/session.ts:114-219`

Claims 시스템은 동시에 활성화된 여러 에이전트 세션 간의 memory key 충돌을 방지하기 위한 잠금(locking) 메커니즘이다.

### 아키텍처 개요

```
Session A                         서버 (memory store)                    Session B
   |                                    |                                    |
   +-- claim(keys=["k1","k2"])          |                                    |
   |   -> store agent/claims/{sesA}     |                                    |
   |      content: ["k1","k2"]          |                                    |
   |      TTL: 30분                     |                                    |
   |                                    |                                    |
   |                                    |    claims_check(keys=["k1"]) <-----+
   |                                    |    -> search agent/claims/ prefix   |
   |                                    |    -> k1 conflict detected!         |
   |                                    +-----> 충돌 응답 ------------------>+
   |                                    |                                    |
```

### claim 액션

> 소스: `packages/cli/src/tools/handlers/session.ts:182-218`

```typescript
case "claim": {
  const sessionId = params.sessionId ?? tracker.sessionId;
  const claimKey = `agent/claims/${sessionId}`;
  const ttlMinutes = params.ttlMinutes ?? 30;
  const expiresAt = Date.now() + ttlMinutes * 60 * 1000;
  await client.storeMemory(
    claimKey,
    JSON.stringify(params.keys),
    { sessionId, claimedAt: Date.now() },
    { tags: ["session-claim"], expiresAt, priority: 0 },
  );
}
```

| 파라미터 | 타입 | 기본값 | 설명 |
|----------|------|--------|------|
| `keys` | `string[]` | (필수) | 잠금할 memory key 목록 |
| `sessionId` | `string` | `tracker.sessionId` | 소유 세션 ID |
| `ttlMinutes` | `number` | 30 | 잠금 유효 시간 (분) |

Claim은 memory store에 `agent/claims/{sessionId}` key로 저장된다:

| 속성 | 값 |
|------|------|
| Key | `agent/claims/{sessionId}` |
| Content | `JSON.stringify(params.keys)` (예: `["k1","k2"]`) |
| Metadata | `{ sessionId, claimedAt: Date.now() }` |
| Tags | `["session-claim"]` |
| Priority | `0` (최저 -- 정리 대상 우선) |
| ExpiresAt | `Date.now() + ttlMinutes * 60 * 1000` |

### claims_check 액션

> 소스: `packages/cli/src/tools/handlers/session.ts:114-181`

```
claims_check(keys=["k1", "k2"])
    |
    +-- searchMemories("agent/claims/", 100, { tags: "session-claim" })
    |       prefix 기반 검색으로 모든 active claim 조회
    |
    +-- for each claim:
    |       |
    |       +-- expiresAt 확인 -> 만료된 claim 스킵
    |       +-- excludeSession 확인 -> 자기 자신 제외
    |       +-- claim.content 파싱 -> claimed key 목록
    |       +-- 입력 keys와 교집합 계산 -> conflicts
    |
    +-- 응답:
          {
            checkedKeys: ["k1", "k2"],
            activeSessions: 2,
            conflicts: ["k1"],
            details: [{ sessionId, claimedKeys, expiresAt, conflicts }],
            hint: "1 key(s) claimed by other sessions."
          }
```

### TTL 기반 잠금의 특성

| 속성 | 설명 |
|------|------|
| 자동 만료 | TTL이 지나면 서버가 자동으로 claim을 무시/삭제 |
| 비관적이지 않음 | claim이 있어도 쓰기를 차단하지는 않고, 에이전트에게 충돌 경고만 제공 |
| Soft lock | 에이전트의 판단에 의존 (충돌 시 쓰기를 포기할지 진행할지는 에이전트가 결정) |
| 갱신 가능 | 동일 sessionId로 다시 claim하면 기존 claim을 덮어씀 |

### rate_status 액션

> 소스: `packages/cli/src/tools/handlers/session.ts:220-235`

claim 시 rate limit을 확인하여 쓰기 제한을 넘지 않도록 한다. `rate_status`로 현재 상태를 확인 가능:

```json
{
  "callsMade": 15,
  "limit": 50,
  "remaining": 35,
  "percentageUsed": 30,
  "status": "ok"       // "ok" | "warning" (>=80%) | "blocked" (>=100%)
}
```

---

## 6. 세션 종료

### 명시적 종료 (session.end)

> 소스: `packages/cli/src/tools/handlers/session.ts:63-106`

에이전트가 `session action=end`를 호출하면 다음이 수행된다:

```
session.end(summary, keysRead, keysWritten, toolsUsed)
    |
    +-- tracker에서 자동 수집된 데이터와 에이전트 제공 데이터 병합
    |       mergedKeysWritten = Set([params.keysWritten, tracker.writtenKeys])
    |       mergedKeysRead = Set([params.keysRead, tracker.readKeys])
    |       mergedToolsUsed = Set([params.toolsUsed, tracker.toolActions])
    |
    +-- client.upsertSessionLog({
    |       sessionId,
    |       summary: params.summary || "Session ended without explicit summary.",
    |       keysRead: mergedKeysRead,
    |       keysWritten: mergedKeysWritten,
    |       toolsUsed: mergedToolsUsed,
    |       endedAt: Date.now(),
    |       lastActivityAt: tracker.lastActivityAt,
    |   })
    |
    +-- tracker.endedExplicitly = true
    |       -> finalizeSession()에서 중복 종료 방지
    |
    +-- return "Session {sessionId} ended. Handoff summary saved."
```

데이터 병합이 핵심이다. 에이전트가 제공한 `keysWritten` 목록에 tracker가 자동 수집한 key들이 합쳐져 누락 없이 모든 활동이 기록된다.

### 자동 종료 (finalizeSession)

> 소스: `packages/cli/src/session-tracker.ts:214-240`

프로세스 종료, MCP 연결 해제, 또는 flush의 `final=true` 호출 시 실행된다:

```typescript
export async function finalizeSession(
  client: ApiClient,
  tracker: SessionTracker,
): Promise<void> {
  if (tracker.closed) return;      // 중복 호출 방지
  tracker.closed = true;

  if (tracker.endedExplicitly) return;  // session.end 이미 호출된 경우 스킵

  try {
    await client.upsertSessionLog({
      sessionId: tracker.sessionId,
      summary: buildSummary(tracker, { autoClose: true }),
      keysRead: [...tracker.readKeys],
      keysWritten: [...tracker.writtenKeys],
      toolsUsed: [...tracker.toolActions],
      endedAt: Date.now(),
      lastActivityAt: tracker.lastActivityAt,
    });
  } catch {
    // Best effort only.
  }
}
```

### buildSummary()

> 소스: `packages/cli/src/session-tracker.ts:179-210`

자동 종료 시 요약 문자열을 생성한다:

```
[auto-closed] Auto-captured: 15 min, 42 API calls.
Keys written: agent/context/architecture/main, config/settings.
Keys read: agent/context/coding_style/general.
Tools: context.bootstrap, memory.search, session.end.
Note: bootstrap was not run this session.
```

| 구성 요소 | 조건 | 예시 |
|-----------|------|------|
| 기본 라인 | 항상 포함 | `Auto-captured: 15 min, 42 API calls.` |
| `[auto-closed]` prefix | `options?.autoClose === true`일 때만 | `[auto-closed] Auto-captured: ...` |
| Keys written | `writtenKeys.length > 0` | `Keys written: k1, k2.` |
| Keys read | `readKeys.length > 0` | `Keys read: k3.` |
| Tools | `toolActions.length > 0` | `Tools: memory.search, context.bootstrap.` |
| Bootstrap 미실행 경고 | `!tracker.bootstrapped` | `Note: bootstrap was not run this session.` |

소요 시간은 `Math.max(1, Math.round(durationMs / 60_000))`으로 계산하여 최소 1분으로 표시한다.

### 세션 파일과 종료의 관계

> 소스: `packages/cli/src/session-tracker.ts:220-223` (주석)

```typescript
// Do NOT remove the session file here. The hook dispatcher reads it
// during SessionEnd and removes it itself. Removing early causes a race
// where the hook can't find the file and generates a duplicate session.
```

`finalizeSession()`은 세션 파일(`.memctl/hooks/session_id`)을 의도적으로 삭제하지 않는다. Hook dispatcher가 `SessionEnd` 이벤트 처리 중 이 파일을 읽고, 자체적으로 삭제한다. 조기 삭제 시 race condition이 발생하여 중복 세션 로그가 생성될 수 있다.

### MCP 연결 해제 시 종료

> 소스: `packages/cli/src/server.ts:126-128`

```typescript
server.server.onclose = () => {
  void finalizeSession(client, tracker);
};
```

MCP 연결이 끊기면(호스트 프로세스 종료, 네트워크 단절 등) `finalizeSession()`이 호출된다.

---

## 7. 주기적 플러시

> 소스: `packages/cli/src/session-tracker.ts:242-268`

### flushSession()

```typescript
export async function flushSession(
  client: ApiClient,
  tracker: SessionTracker,
  final: boolean,
): Promise<void> {
  if (tracker.closed) return;
  if (!tracker.dirty && !final) return;

  if (final) {
    await finalizeSession(client, tracker);
    return;
  }

  try {
    await client.upsertSessionLog({
      sessionId: tracker.sessionId,
      summary: buildSummary(tracker),
      keysRead: [...tracker.readKeys],
      keysWritten: [...tracker.writtenKeys],
      toolsUsed: [...tracker.toolActions],
      lastActivityAt: tracker.lastActivityAt,
    });
    tracker.dirty = false;
  } catch {
    // Best effort only.
  }
}
```

### 플러시 흐름도

```
setInterval(30초)
    |
    +-- flushSession(client, tracker, false)
           |
           +-- tracker.closed? -> return (세션 이미 종료)
           |
           +-- !tracker.dirty && !final? -> return (변경 없음)
           |
           +-- final === true? -> finalizeSession() (최종 종료)
           |
           +-- upsertSessionLog() 호출 (interim update)
           |       endedAt 없음 -> 세션이 아직 활성임을 서버에 알림
           |
           +-- tracker.dirty = false (플래그 초기화)
```

### 주요 특성

| 속성 | 값 | 설명 |
|------|------|------|
| 주기 | `FLUSH_INTERVAL_MS = 30_000` (30초) | `session-tracker.ts:7` |
| dirty flag | 변경 시 `true`, 플러시 후 `false` | 불필요한 네트워크 호출 방지 |
| `interval.unref()` | `session-tracker.ts:361` | Node.js 이벤트 루프가 이 타이머만으로 유지되지 않도록 함 |
| 에러 처리 | `catch {}` (무시) | best-effort. 실패해도 다음 주기에 재시도 |

### dirty flag 동작

dirty flag가 `true`로 설정되는 시점:

| 함수 | 소스 위치 |
|------|-----------|
| `recordToolAction()` | `session-tracker.ts:63` |
| `trackApiCall()` | `session-tracker.ts:165` |

dirty flag가 `false`로 초기화되는 시점:

| 함수 | 소스 위치 |
|------|-----------|
| `flushSession()` (성공 후) | `session-tracker.ts:264` |
| `createSessionTracker()` (초기값) | `session-tracker.ts:47` |

### Interim Update vs Final Update

| 구분 | Interim (주기적) | Final (종료) |
|------|-----------------|-------------|
| `endedAt` | 없음 | `Date.now()` |
| `summary` prefix | 없음 | `[auto-closed]` |
| dirty 확인 | 필요 (`!dirty`이면 스킵) | 불필요 (무조건 실행) |
| `tracker.closed` 설정 | 하지 않음 | `true`로 설정 |

---

## 8. 핸드오프

> 소스: `packages/cli/src/session-tracker.ts:9-15`, `334-347`

### SessionHandoff 구조

```typescript
export type SessionHandoff = {
  previousSessionId: string;
  summary: string | null;
  branch: string | null;
  keysWritten: string[];
  endedAt: unknown;
};
```

| 필드 | 설명 |
|------|------|
| `previousSessionId` | 직전 세션의 ID |
| `summary` | 직전 세션의 요약 (에이전트가 작성하거나 auto-generated) |
| `branch` | 직전 세션이 작업한 Git branch |
| `keysWritten` | 직전 세션이 작성/수정한 memory key 목록 |
| `endedAt` | 직전 세션 종료 시각 |

### 핸드오프 빌드 과정

> 소스: `packages/cli/src/session-tracker.ts:334-347`

```typescript
const lastSession = recentSessions.sessionLogs.find(
  (s) => s.sessionId !== tracker.sessionId,
);
if (lastSession) {
  tracker.handoff = {
    previousSessionId: lastSession.sessionId,
    summary: lastSession.summary,
    branch: lastSession.branch,
    keysWritten: lastSession.keysWritten
      ? JSON.parse(lastSession.keysWritten)
      : [],
    endedAt: lastSession.endedAt,
  };
}
```

최근 세션 로그에서 현재 세션이 아닌 가장 최근 세션을 찾아 핸드오프 정보를 구성한다. `keysWritten`은 서버에 JSON 문자열로 저장되어 있으므로 `JSON.parse()`로 역직렬화한다.

### 핸드오프 활용

핸드오프 데이터는 `tracker.handoff`에 저장되어 도구 핸들러에서 참조할 수 있다. 에이전트가 `session.history`를 호출하거나 `context.bootstrap`를 실행할 때, 직전 세션에서 어떤 key가 수정되었는지, 어떤 작업이 수행되었는지를 파악하여 컨텍스트 연속성을 유지한다.

### 핸드오프 흐름

```
세션 B 시작
    |
    +-- startSessionLifecycle()
    |       |
    |       +-- getSessionLogs(5) 호출
    |       |       [세션A(종료됨), 세션B(현재), ...]
    |       |
    |       +-- find(s => s.sessionId !== currentId)
    |       |       -> 세션 A 발견
    |       |
    |       +-- tracker.handoff = {
    |               previousSessionId: "auto-xxx-yyy",
    |               summary: "Fixed auth bug, updated test cases",
    |               branch: "fix/auth-issue",
    |               keysWritten: ["agent/context/architecture/auth"],
    |               endedAt: "2026-03-17T10:30:00.000Z"
    |           }
    |
    +-- 에이전트가 handoff 정보를 활용하여 작업 계속
```

---

## 9. Memo 시스템

> 소스: `packages/cli/src/tools/handlers/activity.ts:157-244`

Memo 시스템은 에이전트 세션 간 메모를 남기고 읽는 기능이다. 핸드오프가 자동 수집 데이터에 기반한다면, memo는 에이전트가 의도적으로 남기는 자유 형식 메시지이다.

### memo_leave 액션

> 소스: `packages/cli/src/tools/handlers/activity.ts:157-188`

```
memo_leave(message, urgency, relatedKeys)
    |
    +-- id = Date.now().toString(36)
    +-- key = "agent/memo/{id}"
    |
    +-- priority 결정:
    |       info -> 30, warning -> 60, blocker -> 90
    |
    +-- TTL 결정:
    |       blocker -> 7일, info/warning -> 3일
    |
    +-- client.storeMemory(key, message, metadata, options)
    |       metadata: { urgency, relatedKeys, createdAt }
    |       options: { priority, tags: ["memo", urgency], expiresAt }
    |
    +-- return "Memo left (warning): \"Fix the auth endpoint before...\""
```

| 파라미터 | 타입 | 기본값 | 설명 |
|----------|------|--------|------|
| `message` | `string` | (필수) | 메모 내용 |
| `urgency` | `"info" \| "warning" \| "blocker"` | `"info"` | 긴급도 |
| `relatedKeys` | `string[]` | `[]` | 관련 memory key 목록 |

### Urgency 별 설정

| Urgency | Priority | TTL | Tags |
|---------|----------|-----|------|
| `info` | 30 | 3일 (259,200,000ms) | `["memo", "info"]` |
| `warning` | 60 | 3일 | `["memo", "warning"]` |
| `blocker` | 90 | 7일 (604,800,000ms) | `["memo", "blocker"]` |

Priority가 높을수록 `memo_read`에서 먼저 표시되며, blocker는 TTL이 2배 이상 길어 더 오래 유지된다.

### memo_read 액션

> 소스: `packages/cli/src/tools/handlers/activity.ts:189-244`

```
memo_read()
    |
    +-- client.searchMemories("agent/memo/", 50)
    |       prefix 검색으로 모든 memo 조회 (최대 50개)
    |
    +-- filter: key.startsWith("agent/memo/")
    |       정확한 prefix 매칭 (search 결과에 다른 key가 포함될 수 있으므로)
    |
    +-- metadata 파싱 (JSON string 또는 object)
    |
    +-- urgency별 분류 및 정렬:
    |       blockers: urgency === "blocker", 최신순
    |       warnings: urgency === "warning", 최신순
    |       infos: urgency === "info", 최신순
    |
    +-- 응답:
          {
            totalMemos: 5,
            blockers: 1,
            warnings: 2,
            infos: 2,
            memos: [...blockers, ...warnings, ...infos],
            hint: "1 BLOCKER(s) require attention before proceeding."
          }
```

### 정렬 순서

응답의 `memos` 배열은 urgency 우선순위에 따라 정렬된다:

1. **blockers** (최우선) -- `createdAt` 내림차순 (최신 먼저)
2. **warnings** -- `createdAt` 내림차순
3. **infos** (최하위) -- `createdAt` 내림차순

### hint 메시지 로직

| 조건 | hint |
|------|------|
| `items.length === 0` | `"No memos from previous sessions."` |
| `blockers.length > 0` | `"{n} BLOCKER(s) require attention before proceeding."` |
| 기타 | `"Review memos and proceed."` |

### Memo vs Handoff 비교

| 특성 | Memo | Handoff |
|------|------|---------|
| 생성 주체 | 에이전트가 명시적으로 작성 | 시스템이 자동 수집 |
| 내용 | 자유 형식 메시지 | 구조화된 세션 메타데이터 |
| 저장 위치 | memory store (`agent/memo/` prefix) | `tracker.handoff` (in-memory) |
| TTL | 3~7일 (urgency에 따라) | 세션 로그에 영구 보관 |
| 목적 | 다음 세션 에이전트에게 지시/경고 | 세션 연속성 데이터 |

---

## 10. 프로세스 종료 처리

> 소스: `packages/cli/src/session-tracker.ts:358-380`

### 시그널 핸들러 등록

```typescript
const finalize = () => {
  clearInterval(interval);
  void finalizeSession(client, tracker);
};

process.once("beforeExit", finalize);
process.once("SIGINT", finalize);
process.once("SIGTERM", finalize);
```

| 이벤트 | 발생 시점 | 설명 |
|--------|-----------|------|
| `beforeExit` | 이벤트 루프가 비어있을 때 | 정상 종료 시. 비동기 작업이 남아있으면 발생하지 않을 수 있음 |
| `SIGINT` | Ctrl+C 또는 `kill -2` | 사용자 인터럽트 |
| `SIGTERM` | `kill` 또는 시스템 종료 | 프로세스 종료 요청 |

모든 핸들러는 `process.once()`로 등록되어 한 번만 실행된다.

### finalize 함수 동작

```
프로세스 종료 시그널 수신
    |
    +-- clearInterval(interval)
    |       30초 주기 flush 타이머 정리
    |
    +-- void finalizeSession(client, tracker)
    |       |
    |       +-- tracker.closed? -> return (이미 종료됨)
    |       +-- tracker.closed = true
    |       +-- tracker.endedExplicitly? -> return (session.end 이미 호출)
    |       +-- upsertSessionLog({
    |               summary: buildSummary({ autoClose: true }),
    |               endedAt: Date.now(),
    |               ...
    |           })
    |
    +-- [프로세스 종료]
```

`void` 키워드는 `finalizeSession()`의 Promise 반환을 의도적으로 무시한다. 프로세스 종료 시그널 핸들러에서는 비동기 작업의 완료를 보장할 수 없으므로, best-effort로 요약을 서버에 전송하되 실패해도 프로세스가 정상적으로 종료되도록 한다.

### cleanup 함수

> 소스: `packages/cli/src/session-tracker.ts:372-379`

`startSessionLifecycle()`은 `{ cleanup: () => void }` 객체를 반환한다:

```typescript
return {
  cleanup: () => {
    clearInterval(interval);
    process.removeListener("beforeExit", finalize);
    process.removeListener("SIGINT", finalize);
    process.removeListener("SIGTERM", finalize);
  },
};
```

cleanup은 타이머와 모든 시그널 핸들러를 제거한다. 테스트 환경에서 리소스 누수를 방지하거나, 서버 인스턴스를 명시적으로 해제할 때 사용할 수 있다. 현재 프로덕션 코드에서는 `createServer()`가 이 반환값을 사용하지 않지만, 향후 graceful shutdown 구현에 활용할 수 있다.

### MCP onclose 핸들러

> 소스: `packages/cli/src/server.ts:126-128`

```typescript
server.server.onclose = () => {
  void finalizeSession(client, tracker);
};
```

MCP 연결 해제 시에도 `finalizeSession()`이 호출된다. 이는 프로세스 종료 시그널과 별개의 경로로, 호스트가 MCP 연결만 끊고 프로세스는 유지하는 경우를 처리한다.

### 종료 경로 정리

| 종료 경로 | 트리거 | 핸들러 | 비고 |
|-----------|--------|--------|------|
| 에이전트 명시적 종료 | `session action=end` | `session.ts:64-106` | `endedExplicitly = true` 설정 |
| 프로세스 정상 종료 | `beforeExit` | `session-tracker.ts:368` | 이벤트 루프 비어있을 때 |
| 사용자 인터럽트 | `SIGINT` | `session-tracker.ts:369` | Ctrl+C |
| 시스템 종료 | `SIGTERM` | `session-tracker.ts:370` | kill 시그널 |
| MCP 연결 해제 | `server.onclose` | `server.ts:126-128` | 호스트 프로세스 종료 |
| flush final | `flushSession(_, _, true)` | `session-tracker.ts:250-253` | `finalizeSession()` 호출 |

모든 경로에서 `tracker.closed` 플래그가 중복 종료를 방지한다. 또한 `endedExplicitly` 플래그가 설정된 경우 자동 종료 로직이 스킵되어, 에이전트가 작성한 요약이 auto-generated 요약으로 덮어쓰이는 것을 방지한다.

---

## 부록: 전체 세션 생명주기 요약

```
[1] createServer()
     |
     +-- createSessionTracker()
     |       sessionId: "auto-{ts36}-{rand6}"
     |
     +-- new ApiClient({ onRequest: trackApiCall })
     |
     +-- startSessionLifecycle(client, tracker)
     |       |
     |       +-- writeSessionFile()           [동기]
     |       +-- getBranchInfo()              [비동기]
     |       +-- upsertSessionLog(initial)    [비동기]
     |       +-- cleanup stale sessions       [비동기]
     |       +-- build handoff                [비동기]
     |       +-- setInterval(flush, 30s)
     |       +-- register SIGINT/SIGTERM/beforeExit
     |
     +-- registerTools(server, client, tracker)
     +-- registerResources(server, client, tracker)
     +-- server.onclose = finalizeSession

[2] 세션 활성 중
     |
     +-- 에이전트 도구 호출 -> recordToolAction()
     +-- API 요청 -> trackApiCall() -> dirty = true
     +-- 30초마다 -> flushSession() -> upsertSessionLog(interim) -> dirty = false

[3] 세션 종료
     |
     +-- 경로 A: session action=end (명시적)
     |       -> merge agent + tracker data
     |       -> upsertSessionLog(endedAt)
     |       -> endedExplicitly = true
     |
     +-- 경로 B: SIGINT/SIGTERM/beforeExit/onclose (자동)
             -> finalizeSession()
             -> closed = true
             -> if (!endedExplicitly) upsertSessionLog(autoClose summary)
```
