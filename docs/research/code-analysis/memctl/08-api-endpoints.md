# memctl -- API 엔드포인트 분석

## 1. API 아키텍처 개요

memctl의 API는 Next.js App Router 기반으로 구성되며, `apps/web/app/api/` 디렉토리 아래 파일 시스템 라우팅을 따른다. 전체 API는 크게 네 가지 최상위 경로로 분리된다.

```
apps/web/app/api/
  auth/[...all]/        -- better-auth 인증 핸들러
  search/               -- fumadocs 문서 검색 (공개)
  stripe/webhook/       -- Stripe webhook 수신
  v1/                   -- 버전화된 핵심 API
```

**소스 경로**: `apps/web/app/api/`

### 1.1 인증 미들웨어 파이프라인

파일: `apps/web/lib/api-middleware.ts`

`authenticateRequest()` 함수가 모든 v1 API 엔드포인트의 인증을 담당한다. 세 가지 인증 경로를 순서대로 시도한다.

| 순서 | 인증 방식 | 대상 클라이언트 | 헤더 |
|------|-----------|----------------|------|
| 1 | JWT Bearer token | CLI, MCP 서버 | `Authorization: Bearer <jwt>` |
| 2 | API token (SHA-256 해시 검증) | 외부 API 클라이언트 | `Authorization: Bearer mctl_...` |
| 3 | Cookie session (better-auth) | 웹 대시보드 | 브라우저 쿠키 |

JWT 검증 실패 시 API 토큰으로 폴백하며, API 토큰은 SHA-256 해시를 LRU 캐시(1000개, TTL 60초)로 관리하여 반복 DB 조회를 최소화한다.

### 1.2 공통 헤더 규약

| 헤더 | 용도 | 필수 여부 |
|------|------|-----------|
| `X-Org-Slug` | 대상 조직 식별 | 대부분의 v1 엔드포인트에서 필수 |
| `X-Project-Slug` | 대상 프로젝트 식별 | 메모리 CRUD 관련 엔드포인트에서 필수 |
| `X-Request-Id` | 요청 추적용 ID | 선택 (미들웨어에서 자동 생성) |
| `Authorization` | 인증 토큰 | 필수 |
| `If-Match` / `If-None-Match` | ETag 기반 조건부 요청 | 선택 (낙관적 동시성 제어) |

### 1.3 Rate Limiting

`checkRateLimit()` 함수가 조직의 `apiRatePerMinute` 설정을 기반으로 분당 요청 수를 제한한다. POST 엔드포인트(쓰기 작업)에 주로 적용된다.

### 1.4 오류 응답 형식

모든 API 엔드포인트는 동일한 오류 형식을 사용한다:

```json
{
  "error": "오류 메시지 문자열"
}
```

유효성 검증 실패 시 추가 필드가 포함될 수 있다:

```json
{
  "error": "Content validation failed",
  "details": ["필드별 오류 목록"]
}
```

HTTP 상태 코드 규약:

| 코드 | 의미 |
|------|------|
| 200 | 성공 |
| 201 | 리소스 생성 완료 |
| 304 | 변경 없음 (ETag 일치) |
| 400 | 잘못된 요청 (파라미터 누락/형식 오류) |
| 401 | 인증 실패 |
| 403 | 권한 부족 |
| 404 | 리소스 미발견 |
| 409 | 충돌 (중복 키, 낙관적 잠금 실패) |
| 422 | 컨텐츠 스키마 검증 실패 |
| 429 | 속도 제한 초과 |

---

## 2. 인증 엔드포인트

### 2.1 /api/auth/[...all]

파일: `apps/web/app/api/auth/[...all]/route.ts`

better-auth 라이브러리의 catch-all 핸들러. GitHub OAuth 로그인/회원가입, 세션 관리 등 모든 인증 플로우를 처리한다.

| Method | Path | 용도 | Auth | 파라미터 |
|--------|------|------|------|----------|
| GET/POST | `/api/auth/*` | better-auth 인증 플로우 전체 | 불필요 | better-auth 내부 규약 |

### 2.2 /api/v1/auth/token

파일: `apps/web/app/api/v1/auth/token/route.ts`

웹 대시보드 세션을 CLI/MCP용 JWT로 교환하는 엔드포인트. 사용자가 특정 조직의 멤버임을 검증한 후 JWT를 발급한다.

| Method | Path | 용도 | Auth | 파라미터 |
|--------|------|------|------|----------|
| POST | `/api/v1/auth/token` | Session -> JWT 교환 | Cookie session 필수 | Body: `{ orgId: string }` |

응답: `{ token: string }`

---

## 3. 메모리 API (`/api/v1/memories/*`)

메모리 API는 memctl의 핵심이며, 총 30개 이상의 엔드포인트로 구성된다. 모든 엔드포인트는 `X-Org-Slug`과 `X-Project-Slug` 헤더를 필수로 요구하며, Bearer 토큰 또는 Cookie 인증이 필요하다.

### 3.1 핵심 CRUD

파일: `apps/web/app/api/v1/memories/route.ts`, `apps/web/app/api/v1/memories/[key]/route.ts`

| Method | Path | 용도 | Auth | 주요 파라미터 |
|--------|------|------|------|---------------|
| GET | `/api/v1/memories` | 메모리 목록 조회 (검색/필터/정렬) | 필수 | Query: `q`, `limit`, `offset`, `tags`, `sort`, `include_archived`, `include_shared`, `after`, `intent` |
| POST | `/api/v1/memories` | 메모리 생성/Upsert | 필수 | Body: `{ key, content, metadata?, scope?, priority?, tags?, expiresAt? }` |
| GET | `/api/v1/memories/[key]` | 단일 메모리 조회 (key 기반) | 필수 | Path: `key` |
| PATCH | `/api/v1/memories/[key]` | 메모리 부분 수정 | 필수 | Path: `key`, Body: `{ content?, metadata?, priority?, tags?, expiresAt? }` |
| DELETE | `/api/v1/memories/[key]` | 메모리 삭제 | 필수 | Path: `key` |

**GET /api/v1/memories 상세 동작:**
- `q` 파라미터 지정 시 FTS5 전문 검색 -> LIKE 폴백 -> 벡터 검색 하이브리드 방식으로 검색
- `intent` 파라미터 또는 자동 분류를 통해 검색 의도(temporal, relationship 등)에 따른 가중치 적용
- 결과에 `relevance_score` 필드 포함
- ETag 헤더로 조건부 응답 (304 Not Modified) 지원
- 커서 기반 페이지네이션: `after` 파라미터로 다음 페이지 요청, 응답에 `nextCursor` 포함

**POST /api/v1/memories 상세 동작:**
- 동일 key 존재 시 자동으로 update (upsert)
- 버전 히스토리 자동 생성
- 컨텍스트 타입 스키마 검증 (metadata.contextType 지정 시)
- 용량 한도 초과 시 409 반환
- 임베딩 비동기 생성 (fire-and-forget)
- 활동 로그 자동 기록

**PATCH /api/v1/memories/[key] 상세 동작:**
- `If-Match` 헤더로 낙관적 동시성 제어
- 변경 시 버전 히스토리 자동 생성
- 컨텐츠 변경 시 임베딩 재생성

**GET /api/v1/memories/[key] 상세 동작:**
- `accessCount` 자동 증가, `lastAccessedAt` 갱신
- 만료 24시간 이내인 메모리 접근 시 TTL 자동 24시간 연장

### 3.2 검색 및 발견

| Method | Path | 용도 | Auth | 주요 파라미터 |
|--------|------|------|------|---------------|
| GET | `/api/v1/memories/search-org` | 조직 전체 프로젝트 대상 크로스 검색 | 필수 | Query: `q` (필수), `limit` ; Header: `X-Org-Slug` (X-Project-Slug 불필요) |
| POST | `/api/v1/memories/similar` | 유사 메모리 탐색 (벡터 코사인/Jaccard) | 필수 | Body: `{ content, excludeKey?, threshold? }` |
| GET | `/api/v1/memories/co-accessed` | 동시 접근 패턴 기반 관련 메모리 추천 | 필수 | Query: `key` (필수), `limit` |
| GET | `/api/v1/memories/traverse` | 관련 메모리 그래프 BFS 탐색 | 필수 | Query: `key` (필수), `depth` (기본 2, 최대 5) |

파일:
- `apps/web/app/api/v1/memories/search-org/route.ts`
- `apps/web/app/api/v1/memories/similar/route.ts`
- `apps/web/app/api/v1/memories/co-accessed/route.ts`
- `apps/web/app/api/v1/memories/traverse/route.ts`

**search-org** 응답은 프로젝트별로 그룹핑된 결과를 반환하며, `contentPreview` (200자 이내)를 포함한다.

**similar** 엔드포인트는 먼저 벡터 임베딩 코사인 유사도를 시도하고, 임베딩이 없으면 Jaccard 단어 유사도로 폴백한다. 기본 threshold는 0.6이며, 상위 10개까지 반환한다.

**traverse** 엔드포인트는 `relatedKeys` 필드를 따라 BFS 탐색을 수행하며, `nodes`와 `edges` 배열, `maxDepthReached` 플래그를 반환한다.

### 3.3 버전 관리

| Method | Path | 용도 | Auth | 주요 파라미터 |
|--------|------|------|------|---------------|
| GET | `/api/v1/memories/versions` | 메모리 버전 이력 조회 | 필수 | Query: `key` (필수), `limit` |
| POST | `/api/v1/memories/versions` | 특정 버전으로 복원 | 필수 | Body: `{ key, version }` |
| POST | `/api/v1/memories/rollback` | N 단계 이전으로 롤백 | 필수 | Body: `{ key, steps? }` (기본 1, 최대 50) |
| GET | `/api/v1/memories/changes` | 특정 시점 이후 변경 요약 | 필수 | Query: `since` (unix ms, 필수), `limit` |
| GET | `/api/v1/memories/diff` | 두 버전 간 라인별 diff | 필수 | Query: `key`, `v1` (필수), `v2` (생략 시 현재 버전과 비교) |
| GET | `/api/v1/memories/delta` | 델타 부트스트랩 (변경분만 조회) | 필수 | Query: `since` (unix ms, 필수) |
| GET | `/api/v1/memories/org-diff` | 두 프로젝트 간 메모리 비교 | 필수 | Query: `project_a`, `project_b` (필수) ; Header: `X-Org-Slug` |

파일:
- `apps/web/app/api/v1/memories/versions/route.ts`
- `apps/web/app/api/v1/memories/rollback/route.ts`
- `apps/web/app/api/v1/memories/changes/route.ts`
- `apps/web/app/api/v1/memories/diff/route.ts`
- `apps/web/app/api/v1/memories/delta/route.ts`
- `apps/web/app/api/v1/memories/org-diff/route.ts`

**versions POST**: 복원 전 현재 상태를 새 버전으로 저장 (changeType: "restored")한 후 대상 버전의 content/metadata를 적용한다.

**rollback**: `steps` 매개변수로 여러 단계를 한 번에 롤백할 수 있으며, 현재 상태를 새 버전으로 저장 후 복원한다.

**changes**: activity_logs에서 `memory_write`, `memory_delete` 액션을 집계하여 `{ created, updated, deleted, total }` 요약과 상세 변경 목록을 반환한다.

**diff**: LCS 기반 라인별 diff를 계산하며, 각 라인에 `type` ("add", "remove", "same")과 `lineNumber`를 포함한다.

**delta**: 캐시 무효화/동기화 용도. `created`, `updated`, `deleted` 배열로 분리하여 반환한다. 삭제된 메모리는 activity_logs의 "memory_delete" 액션에서 추적한다.

**org-diff**: 두 프로젝트의 비아카이브 메모리를 key 기준으로 비교. `onlyInA`, `onlyInB`, `common` (contentMatch 포함) 배열과 통계를 반환한다.

### 3.4 라이프사이클 및 건강 상태

| Method | Path | 용도 | Auth | 주요 파라미터 |
|--------|------|------|------|---------------|
| POST | `/api/v1/memories/lifecycle` | 라이프사이클 정책 실행 | 필수 | Body: `{ policies[], sessionLogMaxAgeDays?, accessThreshold?, feedbackThreshold?, mergedBranches?, relevanceThreshold?, healthThreshold?, maxVersionsPerMemory?, activityLogMaxAgeDays?, archivePurgeDays? }` |
| POST | `/api/v1/memories/lifecycle/schedule` | 스케줄 기반 정책 자동 실행 (cron용) | 필수 | Body: `{ sessionLogMaxAgeDays?, accessThreshold?, feedbackThreshold? }` |
| GET | `/api/v1/memories/suggest-cleanup` | 정리 대상 메모리 추천 | 필수 | Query: `limit`, `stale_days` (기본 30) |
| GET | `/api/v1/memories/health` | 메모리별 건강 점수 (0-100) | 필수 | Query: `limit` (기본 50, 최대 200) |
| GET | `/api/v1/memories/freshness` | 경량 신선도 체크 (해시 기반) | 필수 | -- |
| POST | `/api/v1/memories/validate` | 파일 경로 참조 검증 | 필수 | Body: `{ repoFiles: string[] }` |

파일:
- `apps/web/app/api/v1/memories/lifecycle/route.ts`
- `apps/web/app/api/v1/memories/lifecycle/schedule/route.ts`
- `apps/web/app/api/v1/memories/suggest-cleanup/route.ts`
- `apps/web/app/api/v1/memories/health/route.ts`
- `apps/web/app/api/v1/memories/freshness/route.ts`
- `apps/web/app/api/v1/memories/validate/route.ts`

**lifecycle 지원 정책:**

| 정책 이름 | 동작 |
|-----------|------|
| `archive_merged_branches` | 병합된 브랜치의 branch_plan 메모리 아카이브 |
| `cleanup_expired` | expiresAt이 지난 메모리 삭제 |
| `cleanup_session_logs` | N일 이전 세션 로그 삭제 |
| `auto_promote` | 접근 빈도 높은 메모리 우선순위 자동 상향 (+10, 최대 100) |
| `auto_demote` | 부정적 피드백 많은 메모리 우선순위 자동 하향 (-10, 최소 0) |
| `auto_prune` | relevance score 기준 미달 메모리 아카이브 (태그: "auto:pruned") |
| `auto_archive_unhealthy` | health score 기준 미달 메모리 아카이브 (태그: "auto:decayed") |
| `cleanup_old_versions` | 메모리당 최대 N개 버전만 유지 (기본 50개) |
| `cleanup_activity_logs` | N일 이전 활동 로그 삭제 (기본 90일) |
| `cleanup_expired_locks` | 만료된 잠금 삭제 |
| `purge_archived` | N일 이상 아카이브된 메모리 영구 삭제 (기본 90일, 고정된 메모리 제외) |

**health 점수 산출 (각 0-25, 총 0-100):**
- age: 신규 메모리일수록 높음 (`max(0, 25 - ageDays / 14)`)
- access: 접근 횟수가 많을수록 높음 (`min(25, accessCount * 2.5)`)
- feedback: 긍정 피드백 비율이 높을수록 높음
- freshness: 최근 접근일수록 높음 (`max(0, 25 - daysSinceAccess / 7)`)

**freshness**: 전체 메모리의 `count`, `latestUpdate`, `latestCreate`와 키+업데이트 시간을 조합한 해시를 반환. 에이전트가 캐시 무효화 판단에 활용한다.

**validate**: 리포지토리 파일 목록을 받아 메모리 내용에서 참조하는 파일 경로 중 존재하지 않는 것을 식별한다.

### 3.5 관리 기능

| Method | Path | 용도 | Auth | 주요 파라미터 |
|--------|------|------|------|---------------|
| POST | `/api/v1/memories/pin` | 메모리 고정/고정 해제 | 필수 | Body: `{ key, pin: boolean }` |
| POST | `/api/v1/memories/archive` | 메모리 아카이브/복원 | 필수 | Body: `{ key, archive: boolean }` |
| DELETE | `/api/v1/memories/archive` | 만료된 메모리 일괄 정리 | 필수 | -- |
| POST | `/api/v1/memories/lock` | 메모리 키 잠금 획득 (분산 잠금) | 필수 | Body: `{ key, lockedBy?, ttlSeconds? }` (기본 TTL 60초) |
| DELETE | `/api/v1/memories/lock` | 메모리 키 잠금 해제 | 필수 | Body: `{ key, lockedBy? }` |
| POST | `/api/v1/memories/link` | 메모리 간 양방향 연결/해제 | 필수 | Body: `{ key, relatedKey, unlink? }` |
| POST | `/api/v1/memories/watch` | 특정 키들의 변경 여부 확인 | 필수 | Body: `{ keys: string[], since: number }` |
| POST | `/api/v1/memories/feedback` | 메모리 유용성 피드백 | 필수 | Body: `{ key, helpful: boolean }` |

파일:
- `apps/web/app/api/v1/memories/pin/route.ts`
- `apps/web/app/api/v1/memories/archive/route.ts`
- `apps/web/app/api/v1/memories/lock/route.ts`
- `apps/web/app/api/v1/memories/link/route.ts`
- `apps/web/app/api/v1/memories/watch/route.ts`
- `apps/web/app/api/v1/memories/feedback/route.ts`

**pin**: 고정된 메모리는 부트스트랩 시 항상 포함되며, 정리 추천에서 제외된다.

**lock**: 동일 프로젝트+키에 대해 하나의 잠금만 존재할 수 있다. 기존 잠금이 만료되었으면 자동 교체. 미만료 잠금 존재 시 409 Conflict 반환.

**link**: `relatedKeys` JSON 배열 필드를 양방향으로 갱신한다. 자기 자신 연결은 거부.

**watch**: 최대 100개 키를 모니터링하여 `changed`와 `unchanged` 배열로 분류. 동시 수정 감지에 활용.

### 3.6 대량 작업

| Method | Path | 용도 | Auth | 주요 파라미터 |
|--------|------|------|------|---------------|
| POST | `/api/v1/memories/bulk` | 다수 메모리 키로 일괄 조회 | 필수 | Body: `{ keys: string[] }` |
| POST | `/api/v1/memories/batch` | 다수 메모리에 대한 일괄 변환 | 필수 | Body: `{ keys: string[], action, value? }` (최대 100개) |
| GET | `/api/v1/memories/export` | 에이전트 컨텍스트 메모리 내보내기 | 필수 | Query: `format` (agents_md, cursorrules, json) |
| GET/POST | `/api/v1/memories/snapshots` | 프로젝트 메모리 스냅샷 관리 | 필수 | GET: `limit` ; POST Body: `{ name, description? }` |

파일:
- `apps/web/app/api/v1/memories/bulk/route.ts`
- `apps/web/app/api/v1/memories/batch/route.ts`
- `apps/web/app/api/v1/memories/export/route.ts`
- `apps/web/app/api/v1/memories/snapshots/route.ts`

**bulk**: 키 배열로 한 번에 조회. 결과는 `{ memories: Record<key, memory>, found, requested }` 형태의 맵으로 반환.

**batch 지원 액션:**

| 액션 | value 타입 | 설명 |
|------|-----------|------|
| `archive` | -- | 일괄 아카이브 |
| `unarchive` | -- | 일괄 복원 |
| `delete` | -- | 일괄 삭제 |
| `pin` | -- | 일괄 고정 |
| `unpin` | -- | 일괄 고정 해제 |
| `set_priority` | `number` (0-100) | 일괄 우선순위 설정 |
| `add_tags` | `string[]` | 일괄 태그 추가 (기존 태그와 병합) |
| `set_scope` | `"project"` 또는 `"shared"` | 일괄 스코프 변경 |

**export**: `agent/context/*` 패턴의 메모리를 타입별로 그룹핑하여 AGENTS.md, .cursorrules, 또는 JSON 형식으로 내보낸다. 우선순위 내림차순 정렬.

**snapshots**: 프로젝트의 모든 비아카이브 메모리를 JSON 스냅샷으로 저장. 목록 조회 시 data 필드는 제외되어 경량 응답.

### 3.7 분석 및 용량

| Method | Path | 용도 | Auth | 주요 파라미터 |
|--------|------|------|------|---------------|
| GET | `/api/v1/memories/analytics` | 프로젝트 메모리 사용 분석 | 필수 | -- |
| GET | `/api/v1/memories/capacity` | 메모리 용량 및 relevance 분포 | 필수 | -- |

파일:
- `apps/web/app/api/v1/memories/analytics/route.ts`
- `apps/web/app/api/v1/memories/capacity/route.ts`

**analytics 응답 필드:**
- `totalMemories`, `totalAccessCount`, `averagePriority`, `averageHealthScore`
- `mostAccessed` (상위 10), `leastAccessed` (하위 10), `neverAccessed`
- `byScope` (project/shared 분포), `byTag` (태그별 개수)
- `pinnedCount`, `avgAge` (일 단위)

**capacity 응답 필드:**
- `used`, `limit`, `isFull`, `isApproaching`, `usageRatio`
- `relevanceDistribution` (relevance score 분포)

---

## 4. 조직 API (`/api/v1/orgs/*`)

### 4.1 조직 CRUD

| Method | Path | 용도 | Auth | 주요 파라미터 |
|--------|------|------|------|---------------|
| GET | `/api/v1/orgs` | 현재 사용자의 조직 목록 | Session 필수 | -- |
| POST | `/api/v1/orgs` | 조직 생성 | Session 필수 | Body: `{ name, slug }` |
| GET | `/api/v1/orgs/[slug]` | 조직 상세 조회 | Session 필수 (멤버) | Path: `slug` |
| PATCH | `/api/v1/orgs/[slug]` | 조직 정보 수정 | Session 필수 (admin/owner) | Path: `slug`, Body: `{ name? }` |

파일:
- `apps/web/app/api/v1/orgs/route.ts`
- `apps/web/app/api/v1/orgs/[slug]/route.ts`

조직 생성 시 Self-hosted 모드에서는 거부된다. 사용자당 무료 조직 수 제한(`FREE_ORG_LIMIT_PER_USER`)이 적용되며, 결제 활성화 시 Stripe 고객이 자동 생성된다.

### 4.2 멤버 관리

| Method | Path | 용도 | Auth | 주요 파라미터 |
|--------|------|------|------|---------------|
| GET | `/api/v1/orgs/[slug]/members` | 조직 멤버 목록 (프로젝트 할당 포함) | Session (멤버) | -- |
| PATCH | `/api/v1/orgs/[slug]/members` | 멤버 역할 변경 | Session (admin/owner) | Body: `{ memberId, role }` |
| DELETE | `/api/v1/orgs/[slug]/members` | 멤버 제거 | Session (admin/owner) | Body: `{ memberId }` |
| GET | `/api/v1/orgs/[slug]/members/[memberId]/projects` | 멤버의 프로젝트 할당 조회 | Session (멤버) | -- |
| PUT | `/api/v1/orgs/[slug]/members/[memberId]/projects` | 멤버의 프로젝트 할당 설정 | Session (admin/owner) | Body: `{ projectIds: string[] }` |

파일:
- `apps/web/app/api/v1/orgs/[slug]/members/route.ts`
- `apps/web/app/api/v1/orgs/[slug]/members/[memberId]/projects/route.ts`

멤버 제거 시 프로젝트 할당도 함께 삭제되며, Stripe 좌석 수가 동기화된다. owner 역할 변경은 불가하며, 유일한 owner 삭제는 거부된다. 모든 역할 변경과 멤버 추가/제거는 감사 로그에 기록된다.

### 4.3 초대

| Method | Path | 용도 | Auth | 주요 파라미터 |
|--------|------|------|------|---------------|
| GET | `/api/v1/orgs/[slug]/invitations` | 대기 중인 초대 목록 | Session (admin/owner) | -- |
| POST | `/api/v1/orgs/[slug]/invitations` | 이메일로 사용자 초대 | Session (admin/owner) | Body: `{ email, role?, expiresInDays? }` |
| DELETE | `/api/v1/orgs/[slug]/invitations` | 초대 취소 | Session (admin/owner) | Body: `{ invitationId }` |

파일: `apps/web/app/api/v1/orgs/[slug]/invitations/route.ts`

이미 계정이 있는 사용자를 초대하면 즉시 멤버로 추가된다. 일일 초대 횟수 제한(`INVITATIONS_PER_DAY`)과 최대 대기 초대 수(`MAX_PENDING_INVITATIONS`)가 적용된다. Self-hosted 모드에서는 이 제한이 해제된다. 좌석 수 초과 시 Stripe 자동 좌석 추가(`ensureSeatForAdditionalMember`)가 실행된다.

### 4.4 결제 관련

| Method | Path | 용도 | Auth | 주요 파라미터 |
|--------|------|------|------|---------------|
| POST | `/api/v1/orgs/[slug]/checkout` | Stripe Checkout 세션 생성 | Session (admin/owner) | Body: `{ planId, promoCode? }` |
| POST | `/api/v1/orgs/[slug]/portal` | Stripe Customer Portal 세션 생성 | Session (admin/owner) | Body: `{ planId? }` (플랜 변경 시) |
| GET | `/api/v1/orgs/[slug]/invoices` | 청구서 목록 조회 | Session (admin/owner) | -- |
| POST | `/api/v1/orgs/[slug]/validate-promo` | 프로모션 코드 유효성 검증 | Session (멤버) | Body: `{ code, planId? }` |

파일:
- `apps/web/app/api/v1/orgs/[slug]/checkout/route.ts`
- `apps/web/app/api/v1/orgs/[slug]/portal/route.ts`
- `apps/web/app/api/v1/orgs/[slug]/invoices/route.ts`
- `apps/web/app/api/v1/orgs/[slug]/validate-promo/route.ts`

**validate-promo**: 10가지 검증 조건을 순차적으로 확인 (활성, 시작일, 만료일, 최대 사용 횟수, 조직당 최대 사용 횟수, 조직 제한, 적용 플랜, 최소 플랜 티어, 첫 구독 전용, 이전 프로모 미사용). LRU 캐시 기반 사용자당 분당 10회 속도 제한 적용.

### 4.5 통계 및 활동

| Method | Path | 용도 | Auth | 주요 파라미터 |
|--------|------|------|------|---------------|
| GET | `/api/v1/orgs/[slug]/stats` | 조직 통계 (프로젝트/멤버/메모리/토큰 수) | Session (멤버) | -- |
| GET | `/api/v1/orgs/[slug]/activity` | 조직 활동 로그 + 감사 로그 | Session (멤버) | Query: `cursor`, `limit`, `action`, `from`, `to`, `search`, `type` |
| GET | `/api/v1/orgs/[slug]/activity/sessions` | 조직 세션 로그 | Session (멤버) | Query: `cursor`, `limit` |

파일:
- `apps/web/app/api/v1/orgs/[slug]/stats/route.ts`
- `apps/web/app/api/v1/orgs/[slug]/activity/route.ts`
- `apps/web/app/api/v1/orgs/[slug]/activity/sessions/route.ts`

**activity**: `type` 파라미터로 `all`, `activity`, `audit` 필터링 가능. member 역할은 할당된 프로젝트만 열람 가능. 커서 기반 페이지네이션.

### 4.6 프로젝트별 활동 (조직 하위)

| Method | Path | 용도 | Auth | 주요 파라미터 |
|--------|------|------|------|---------------|
| GET | `/api/v1/orgs/[slug]/projects/[projectSlug]/activity` | 프로젝트 활동/감사 로그 | Session (멤버+할당) | Query: `cursor`, `limit`, `action`, `from`, `to`, `search`, `type` |
| GET | `/api/v1/orgs/[slug]/projects/[projectSlug]/activity/sessions` | 프로젝트 세션 로그 | Session (멤버+할당) | Query: `cursor`, `limit` |
| GET | `/api/v1/orgs/[slug]/projects/[projectSlug]/hygiene` | 프로젝트 메모리 위생 리포트 | Session (멤버+할당) | -- |
| GET | `/api/v1/orgs/[slug]/projects/[projectSlug]/members` | 프로젝트 멤버 목록 (할당 여부 포함) | Session (admin/owner) | -- |
| GET | `/api/v1/orgs/[slug]/projects/[projectSlug]/memories` | 프로젝트 메모리 전체 목록 (대시보드용) | Session (멤버+할당) | -- |

파일:
- `apps/web/app/api/v1/orgs/[slug]/projects/[projectSlug]/activity/route.ts`
- `apps/web/app/api/v1/orgs/[slug]/projects/[projectSlug]/activity/sessions/route.ts`
- `apps/web/app/api/v1/orgs/[slug]/projects/[projectSlug]/hygiene/route.ts`
- `apps/web/app/api/v1/orgs/[slug]/projects/[projectSlug]/members/route.ts`
- `apps/web/app/api/v1/orgs/[slug]/projects/[projectSlug]/memories/route.ts`

**memories**: 프로젝트에 속한 전체 메모리를 조회하며, 웹 대시보드 메모리 목록 화면에서 사용된다. 아카이브되지 않은 메모리를 반환하고, 태그/우선순위/접근 통계 등 관리에 필요한 필드를 포함한다.

**hygiene**: 건강 점수 분포(critical/low/medium/healthy), 비활성 메모리, 만료 임박 메모리, 주간 성장 추이, 테이블 크기(versions/activityLogs/expiredLocks)를 포함하는 종합 위생 리포트.

---

## 5. 프로젝트 API (`/api/v1/projects/*`)

| Method | Path | 용도 | Auth | 주요 파라미터 |
|--------|------|------|------|---------------|
| GET | `/api/v1/projects` | 프로젝트 목록 (페이지네이션) | Session (멤버) | Query: `org` (필수), `page`, `per_page` |
| POST | `/api/v1/projects` | 프로젝트 생성 | Session (admin/owner) | Query: `org` (필수), Body: `{ name, slug, description? }` |
| GET | `/api/v1/projects/[slug]` | 프로젝트 상세 조회 | Session (멤버+할당) | Query: `org` (필수) |
| PATCH | `/api/v1/projects/[slug]` | 프로젝트 수정 | Session (admin/owner) | Query: `org` (필수), Body: `{ name?, description? }` |
| DELETE | `/api/v1/projects/[slug]` | 프로젝트 삭제 (연쇄 삭제) | Session (admin/owner) | Query: `org` (필수) |

파일:
- `apps/web/app/api/v1/projects/route.ts`
- `apps/web/app/api/v1/projects/[slug]/route.ts`

member 역할 사용자는 할당된 프로젝트만 조회 가능. 프로젝트 삭제 시 연쇄 순서: memory_locks -> memory_snapshots -> activity_logs -> session_logs -> project_members -> memories -> project. 프로젝트 수 제한은 조직의 `projectLimit` 설정에 따른다. 프로젝트 생성자는 자동으로 프로젝트 멤버로 할당된다.

---

## 6. 토큰 API (`/api/v1/tokens`)

파일: `apps/web/app/api/v1/tokens/route.ts`

| Method | Path | 용도 | Auth | 주요 파라미터 |
|--------|------|------|------|---------------|
| GET | `/api/v1/tokens` | API 토큰 목록 조회 | Session (멤버) | Query: `org` (필수), `all` (owner만 전체 조회 가능) |
| POST | `/api/v1/tokens` | API 토큰 생성 | Session (멤버) | Query: `org` (필수), Body: `{ name, expiresAt? }` |
| DELETE | `/api/v1/tokens` | API 토큰 폐기 (soft delete) | Session (멤버/owner) | Query: `id` (필수), `org` (owner 전체 관리 시) |

토큰 형식: `mctl_<random_id>`. SHA-256 해시만 DB에 저장되며, 원본 토큰은 생성 시 1회만 반환된다. owner는 조직 전체 토큰을 관리할 수 있고, 일반 멤버는 자신의 토큰만 관리 가능하다.

---

## 7. 컨텍스트 타입 API (`/api/v1/context-types/*`)

| Method | Path | 용도 | Auth | 주요 파라미터 |
|--------|------|------|------|---------------|
| GET | `/api/v1/context-types` | 조직의 컨텍스트 타입 목록 | 필수 | Header: `X-Org-Slug` |
| POST | `/api/v1/context-types` | 컨텍스트 타입 생성 | 필수 | Header: `X-Org-Slug`, Body: `{ slug, label, description, schema?, icon? }` |
| GET | `/api/v1/context-types/[slug]` | 컨텍스트 타입 상세 조회 | 필수 | Header: `X-Org-Slug` |
| PATCH | `/api/v1/context-types/[slug]` | 컨텍스트 타입 수정 | 필수 | Header: `X-Org-Slug`, Body: `{ label?, description?, schema?, icon? }` |
| DELETE | `/api/v1/context-types/[slug]` | 컨텍스트 타입 삭제 | 필수 | Header: `X-Org-Slug` |

파일:
- `apps/web/app/api/v1/context-types/route.ts`
- `apps/web/app/api/v1/context-types/[slug]/route.ts`

컨텍스트 타입은 조직 수준에서 관리되며, 메모리 저장 시 `metadata.contextType`으로 참조하여 `schema` 필드 기반 컨텐츠 유효성 검증을 수행한다.

---

## 8. 세션/활동 로그 API

### 8.1 세션 로그

파일: `apps/web/app/api/v1/session-logs/route.ts`

| Method | Path | 용도 | Auth | 주요 파라미터 |
|--------|------|------|------|---------------|
| GET | `/api/v1/session-logs` | 에이전트 세션 로그 조회 | 필수 | Header: `X-Org-Slug`, `X-Project-Slug` ; Query: `limit`, `branch` |
| POST | `/api/v1/session-logs` | 에이전트 세션 로그 기록/갱신 (upsert) | 필수 | Header: `X-Org-Slug`, `X-Project-Slug` ; Body: `{ sessionId, branch?, summary?, keysRead?, keysWritten?, toolsUsed?, endedAt?, lastActivityAt? }` |

동일 `sessionId`가 이미 존재하면 기존 레코드를 갱신한다.

### 8.2 활동 로그

파일: `apps/web/app/api/v1/activity-logs/route.ts`

| Method | Path | 용도 | Auth | 주요 파라미터 |
|--------|------|------|------|---------------|
| GET | `/api/v1/activity-logs` | 활동 로그 조회 | 필수 | Header: `X-Org-Slug`, `X-Project-Slug` ; Query: `limit`, `session_id`, `branch` |
| POST | `/api/v1/activity-logs` | 활동 로그 기록 | 필수 | Header: `X-Org-Slug`, `X-Project-Slug` ; Body: `{ action, sessionId?, toolName?, memoryKey?, details? }` |

`branch` 파라미터 지정 시 해당 브랜치의 세션을 먼저 조회한 후 해당 세션들의 활동 로그만 필터링한다.

---

## 9. 관리자 API (`/api/v1/admin/*`)

모든 관리자 API는 `requireAdmin()` 미들웨어를 통해 `users.isAdmin = true`인 사용자만 접근 가능하다.

### 9.1 사용자 관리

| Method | Path | 용도 | Auth | 주요 파라미터 |
|--------|------|------|------|---------------|
| GET | `/api/v1/admin/users` | 사용자 목록 (검색/필터/정렬) | Admin | Query: `search`, `admin` (yes/no), `hasOrgs` (yes/no), `sort`, `order`, `limit`, `offset` |
| PATCH | `/api/v1/admin/users/[id]` | 관리자 권한 토글 | Admin | Body: `{ isAdmin: boolean }` |

파일:
- `apps/web/app/api/v1/admin/users/route.ts`
- `apps/web/app/api/v1/admin/users/[id]/route.ts`

### 9.2 조직 관리

| Method | Path | 용도 | Auth | 주요 파라미터 |
|--------|------|------|------|---------------|
| GET | `/api/v1/admin/organizations` | 조직 목록 (검색/필터/정렬) | Admin | Query: `search`, `plan`, `status`, `sort`, `order`, `limit`, `offset` |
| GET | `/api/v1/admin/organizations/[slug]` | 조직 상세 (소유자/멤버/프로젝트 수 포함) | Admin | -- |
| PATCH | `/api/v1/admin/organizations/[slug]` | 조직 관리 액션 실행 | Admin | Body: 액션별 상이 (아래 표 참조) |
| GET | `/api/v1/admin/organizations/[slug]/actions` | 조직 관리 액션 이력 | Admin | -- |

파일:
- `apps/web/app/api/v1/admin/organizations/route.ts`
- `apps/web/app/api/v1/admin/organizations/[slug]/route.ts`
- `apps/web/app/api/v1/admin/organizations/[slug]/actions/route.ts`

**관리 액션 목록:**

| 액션 | 설명 | 추가 필드 |
|------|------|-----------|
| `suspend` | 조직 일시 중단 | `reason` |
| `ban` | 조직 차단 | `reason` |
| `reactivate` | 조직 재활성화 | `reason` |
| `override_plan` | 플랜 강제 변경 | `planId` |
| `override_limits` | 개별 한도 수동 설정 | `projectLimit?`, `memberLimit?`, `memoryLimitPerProject?`, `apiRatePerMinute?` |
| `reset_limits` | 플랜 기본 한도로 초기화 | -- |
| `transfer_ownership` | 소유권 이전 | `newOwnerId` |
| `update_notes` | 관리자 메모 수정 | `notes` |
| `start_trial` | 엔터프라이즈 체험 시작 | `durationDays` |
| `end_trial` | 체험 종료 (무료 플랜 복원) | -- |
| `set_expiry` | 플랜 만료일 설정 | `expiresAt` |
| `clear_expiry` | 플랜 만료일 제거 | -- |
| `create_subscription` | 커스텀 Stripe 구독 생성 | `priceInCents`, `interval` |
| `cancel_subscription` | Stripe 구독 취소 | -- |
| `update_contract` | 계약 정보 수정 | `contractValue?`, `contractNotes?`, `contractStartDate?`, `contractEndDate?` |
| `apply_template` | 플랜 템플릿 적용 | `templateId`, `createSubscription?`, `subscriptionInterval?` |

모든 액션은 `admin_actions` 테이블에 기록되며, 관리자 이름/이메일과 함께 이력을 조회할 수 있다.

### 9.3 블로그 관리

| Method | Path | 용도 | Auth | 주요 파라미터 |
|--------|------|------|------|---------------|
| GET | `/api/v1/admin/blog` | 블로그 포스트 관리 목록 | Admin | Query: `search`, `status`, `sort`, `order`, `limit`, `offset` |

파일: `apps/web/app/api/v1/admin/blog/route.ts`

### 9.4 변경 로그 관리

| Method | Path | 용도 | Auth | 주요 파라미터 |
|--------|------|------|------|---------------|
| GET | `/api/v1/admin/changelog` | 변경 로그 관리 목록 (변경 항목 수 포함) | Admin | Query: `search`, `status`, `sort`, `order`, `limit`, `offset` |

파일: `apps/web/app/api/v1/admin/changelog/route.ts`

### 9.5 프로모션 코드 관리

| Method | Path | 용도 | Auth | 주요 파라미터 |
|--------|------|------|------|---------------|
| GET | `/api/v1/admin/promo-codes` | 프로모션 코드 목록 | Admin | Query: `campaign`, `active`, `search`, `sort`, `order`, `limit`, `offset` |
| POST | `/api/v1/admin/promo-codes` | 프로모션 코드 생성 (단일/대량) | Admin | Body: 코드 상세 (대량 시 `bulkPrefix`, `bulkCount`) |
| GET | `/api/v1/admin/promo-codes/[id]` | 프로모션 코드 상세 + 사용 이력 | Admin | -- |
| PATCH | `/api/v1/admin/promo-codes/[id]` | 프로모션 코드 수정 | Admin | Body: 수정 가능 필드들 |
| DELETE | `/api/v1/admin/promo-codes/[id]` | 프로모션 코드 비활성화 (soft delete) | Admin | -- |
| POST | `/api/v1/admin/promo-codes/[id]/clone` | 프로모션 코드 복제 | Admin | Body: `{ code }` |

파일:
- `apps/web/app/api/v1/admin/promo-codes/route.ts`
- `apps/web/app/api/v1/admin/promo-codes/[id]/route.ts`
- `apps/web/app/api/v1/admin/promo-codes/[id]/clone/route.ts`

프로모션 코드 생성/수정/비활성화 시 Stripe Coupon/Promotion Code도 동기화된다. 대량 생성 시 prefix + 3자리 숫자(001-100)로 코드가 자동 생성된다.

### 9.6 플랜 템플릿 관리

| Method | Path | 용도 | Auth | 주요 파라미터 |
|--------|------|------|------|---------------|
| GET | `/api/v1/admin/plan-templates` | 활성 플랜 템플릿 목록 | Admin | -- |
| POST | `/api/v1/admin/plan-templates` | 플랜 템플릿 생성 | Admin | Body: 템플릿 상세 |
| GET | `/api/v1/admin/plan-templates/[id]` | 플랜 템플릿 상세 | Admin | -- |
| PATCH | `/api/v1/admin/plan-templates/[id]` | 플랜 템플릿 수정 | Admin | Body: 수정 필드 |
| DELETE | `/api/v1/admin/plan-templates/[id]` | 플랜 템플릿 아카이브 (soft delete) | Admin | -- |

파일:
- `apps/web/app/api/v1/admin/plan-templates/route.ts`
- `apps/web/app/api/v1/admin/plan-templates/[id]/route.ts`

### 9.7 시스템 통계

| Method | Path | 용도 | Auth | 주요 파라미터 |
|--------|------|------|------|---------------|
| GET | `/api/v1/admin/stats` | 전역 통계 (사용자/조직/프로젝트/메모리 수) | Admin | -- |

파일: `apps/web/app/api/v1/admin/stats/route.ts`

---

## 10. 기타 엔드포인트

### 10.1 상태 확인

| Method | Path | 용도 | Auth | 주요 파라미터 |
|--------|------|------|------|---------------|
| GET | `/api/v1/health` | API 서버 상태 확인 | 불필요 | -- |

파일: `apps/web/app/api/v1/health/route.ts`

응답: `{ ok: true }`

### 10.2 온보딩

| Method | Path | 용도 | Auth | 주요 파라미터 |
|--------|------|------|------|---------------|
| POST | `/api/v1/onboarding` | 온보딩 완료 (설문 + 조직 생성) | Session 필수 | Body: `{ heardFrom, role, teamSize, useCase, orgName, orgSlug }` |

파일: `apps/web/app/api/v1/onboarding/route.ts`

온보딩 응답을 `onboarding_responses` 테이블에 저장하고, 조직을 생성하며, 사용자의 `onboardingCompleted` 플래그를 true로 설정한다.

### 10.3 조직 기본 메모리

| Method | Path | 용도 | Auth | 주요 파라미터 |
|--------|------|------|------|---------------|
| GET | `/api/v1/org-defaults` | 조직 기본 메모리 목록 | 필수 | Header: `X-Org-Slug` |
| POST | `/api/v1/org-defaults` | 조직 기본 메모리 생성/수정 (upsert) | 필수 | Header: `X-Org-Slug`, Body: `{ key, content, metadata?, priority?, tags? }` |
| DELETE | `/api/v1/org-defaults` | 조직 기본 메모리 삭제 | 필수 | Header: `X-Org-Slug`, Body: `{ key }` |
| POST | `/api/v1/org-defaults/apply` | 조직 기본 메모리를 프로젝트에 적용 | 필수 | Header: `X-Org-Slug`, `X-Project-Slug` |

파일:
- `apps/web/app/api/v1/org-defaults/route.ts`
- `apps/web/app/api/v1/org-defaults/apply/route.ts`

apply 시 기존 메모리와 키가 동일하면 업데이트(버전 생성 포함), 없으면 새로 생성한다.

### 10.4 프로젝트 템플릿

| Method | Path | 용도 | Auth | 주요 파라미터 |
|--------|------|------|------|---------------|
| GET | `/api/v1/project-templates` | 프로젝트 템플릿 목록 | 필수 | Header: `X-Org-Slug`, `X-Project-Slug` |
| POST | `/api/v1/project-templates` | 템플릿 생성 또는 적용 | 필수 | 생성: `{ name, description?, data: [{key, content, ...}] }` ; 적용: `{ apply: true, templateId }` |
| DELETE | `/api/v1/project-templates` | 템플릿 삭제 | 필수 | Body: `{ templateId }` |

파일: `apps/web/app/api/v1/project-templates/route.ts`

### 10.5 배치 API

| Method | Path | 용도 | Auth | 주요 파라미터 |
|--------|------|------|------|---------------|
| POST | `/api/v1/batch` | 다수 API 요청 병렬 실행 (최대 20개) | 필수 | Body: `{ operations: [{ method, path, body? }, ...] }` |

파일: `apps/web/app/api/v1/batch/route.ts`

각 operation의 `path`는 `/`로 시작해야 하며, 내부적으로 `{baseUrl}/api/v1{path}`로 fetch한다. 원본 요청의 Authorization, X-Org-Slug, X-Project-Slug 헤더를 전달한다.

### 10.6 Stripe Webhook

| Method | Path | 용도 | Auth | 주요 파라미터 |
|--------|------|------|------|---------------|
| POST | `/api/stripe/webhook` | Stripe 이벤트 수신 | Stripe 서명 | Header: `stripe-signature` |

파일: `apps/web/app/api/stripe/webhook/route.ts`

처리하는 이벤트:

| 이벤트 | 동작 |
|--------|------|
| `checkout.session.completed` | 구독 연결, 플랜/한도 업데이트, 프로모 코드 사용 추적 |
| `customer.subscription.created` | 구독 ID 연결, 플랜 업데이트 |
| `customer.subscription.updated` | 플랜/한도 업데이트 |
| `customer.subscription.deleted` | 무료 플랜으로 복원 |
| `invoice.payment_failed` | 로깅 |
| `customer.updated` | 조직 결제 프로필 동기화 (이름, 주소) |
| `customer.tax_id.created/deleted` | 세금 ID 동기화 |

구독 변경 시 `enforceSeatComplianceStatus()`로 좌석 수 초과 여부를 확인하고, 초과 시 조직을 자동으로 suspended 상태로 전환한다.

### 10.7 문서 검색

| Method | Path | 용도 | Auth | 주요 파라미터 |
|--------|------|------|------|---------------|
| GET | `/api/search` | fumadocs 문서 검색 | 불필요 | fumadocs 규약 |

파일: `apps/web/app/api/search/route.ts`

### 10.8 블로그 (공개)

| Method | Path | 용도 | Auth | 주요 파라미터 |
|--------|------|------|------|---------------|
| GET | `/api/v1/blog` | 블로그 포스트 목록 (published) | 불필요 (Admin이면 draft도 조회) | Query: `status`, `page`, `limit` |
| POST | `/api/v1/blog` | 블로그 포스트 작성 | Admin | Body: `{ title, slug?, excerpt?, content, coverImageUrl?, status? }` |
| GET | `/api/v1/blog/[slug]` | 블로그 포스트 상세 | 불필요 (draft는 Admin만) | -- |
| PUT | `/api/v1/blog/[slug]` | 블로그 포스트 수정 | Admin | Body: 수정 필드 |
| DELETE | `/api/v1/blog/[slug]` | 블로그 포스트 삭제 | Admin | -- |

파일:
- `apps/web/app/api/v1/blog/route.ts`
- `apps/web/app/api/v1/blog/[slug]/route.ts`

### 10.9 변경 로그 (공개)

| Method | Path | 용도 | Auth | 주요 파라미터 |
|--------|------|------|------|---------------|
| GET | `/api/v1/changelog` | 변경 로그 목록 (published, items 포함) | 불필요 (Admin이면 draft도 조회) | Query: `status`, `page`, `limit` |
| POST | `/api/v1/changelog` | 변경 로그 항목 작성 | Admin | Body: `{ version, title, summary?, releaseDate, status?, items: [{category, description, sortOrder?}] }` |
| GET | `/api/v1/changelog/[version]` | 변경 로그 상세 (items 포함) | 불필요 (draft는 Admin만) | -- |
| PUT | `/api/v1/changelog/[version]` | 변경 로그 수정 | Admin | Body: 수정 필드 |
| DELETE | `/api/v1/changelog/[version]` | 변경 로그 삭제 | Admin | -- |

파일:
- `apps/web/app/api/v1/changelog/route.ts`
- `apps/web/app/api/v1/changelog/[version]/route.ts`

### 10.10 사용자 프로필 / 세션

| Method | Path | 용도 | Auth | 주요 파라미터 |
|--------|------|------|------|---------------|
| PATCH | `/api/v1/user` | 사용자 이름 변경 | Session 필수 | Body: `{ name }` |
| GET | `/api/v1/user/sessions` | 활성 세션 목록 | Session 필수 | -- |
| DELETE | `/api/v1/user/sessions` | 현재 세션 외 모두 폐기 | Session 필수 | -- |

파일:
- `apps/web/app/api/v1/user/route.ts`
- `apps/web/app/api/v1/user/sessions/route.ts`

---

## 11. 요청/응답 패턴 요약

### 11.1 인증 흐름 요약

```
CLI/MCP:  POST /api/v1/auth/token { orgId } -> { token: JWT }
          이후 모든 요청에 Authorization: Bearer <jwt>

API Token: mctl_<id> 직접 사용
          Authorization: Bearer mctl_...

Dashboard: better-auth cookie 자동 처리
```

### 11.2 필수 헤더 매트릭스

| 엔드포인트 그룹 | X-Org-Slug | X-Project-Slug | Authorization |
|----------------|------------|----------------|---------------|
| /api/v1/memories/* | 필수 | 필수 | 필수 |
| /api/v1/memories/search-org | 필수 | 불필요 | 필수 |
| /api/v1/memories/org-diff | 필수 | 불필요 | 필수 |
| /api/v1/context-types/* | 필수 | 불필요 | 필수 |
| /api/v1/org-defaults/* | 필수 | 선택 (apply 시 필수) | 필수 |
| /api/v1/session-logs | 필수 | 필수 | 필수 |
| /api/v1/activity-logs | 필수 | 필수 | 필수 |
| /api/v1/orgs/* | 불필요 (path에 slug 포함) | 불필요 | Session 필수 |
| /api/v1/projects/* | 불필요 (query에 org) | 불필요 | Session 필수 |
| /api/v1/tokens | 불필요 (query에 org) | 불필요 | Session 필수 |
| /api/v1/admin/* | 불필요 | 불필요 | Admin Session 필수 |
| /api/v1/health | 불필요 | 불필요 | 불필요 |

### 11.3 페이지네이션 패턴

API에서 두 가지 페이지네이션 방식이 사용된다:

**커서 기반 (주로 메모리/활동 로그)**
```json
// 요청
GET /api/v1/memories?limit=20&after=<memory_id>

// 응답
{
  "memories": [...],
  "nextCursor": "abc123"
}
```

**오프셋 기반 (주로 관리자/프로젝트 목록)**
```json
// 요청
GET /api/v1/projects?org=myorg&page=2&per_page=20

// 응답
{
  "projects": [...],
  "pagination": { "page": 2, "perPage": 20, "total": 45, "totalPages": 3 }
}
```

### 11.4 ETag / 조건부 요청

메모리 조회(GET) 엔드포인트는 ETag 헤더를 반환하며, 클라이언트가 `If-None-Match` 헤더로 조건부 요청을 보내면 변경 없을 시 304 Not Modified를 반환한다.

메모리 수정(PATCH) 및 삭제(DELETE) 시 `If-Match` 헤더로 낙관적 동시성 제어가 가능하다. ETag 불일치 시 409 Conflict를 반환한다.

### 11.5 전체 엔드포인트 수

코드베이스에는 83개의 `route.ts` 파일이 존재하며, 하나의 route 파일이 여러 HTTP 메서드 핸들러(GET, POST, PATCH, DELETE 등)를 포함할 수 있다. 아래 표는 개별 HTTP 메서드 핸들러 기준으로 집계한 수치이다.

| 카테고리 | HTTP 메서드 핸들러 수 |
|----------|---------------------|
| 인증 | 2 |
| 메모리 CRUD | 5 |
| 메모리 검색/발견 | 4 |
| 메모리 버전 관리 | 7 |
| 메모리 라이프사이클 | 6 |
| 메모리 관리 기능 | 8 |
| 메모리 대량 작업 | 4 |
| 메모리 분석/용량 | 2 |
| 조직 | 16 |
| 프로젝트 | 5 |
| 토큰 | 3 |
| 컨텍스트 타입 | 5 |
| 세션/활동 로그 | 4 |
| 관리자 | 15 |
| 기타 (health, onboarding, blog, changelog, batch 등) | 16 |
| **합계** | **83개 route 파일, 약 102개 HTTP 메서드 핸들러** |
