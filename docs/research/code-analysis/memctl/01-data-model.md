# memctl -- 데이터 모델 분석

> 분석 대상: `~/Workspace/context-sync-research/memctl/`
> 핵심 소스 파일:
> - `packages/db/src/schema.ts` -- Drizzle ORM 스키마 정의
> - `packages/db/src/client.ts` -- 데이터베이스 클라이언트 팩토리
> - `packages/db/drizzle.config.ts` -- Drizzle Kit 설정
> - `packages/shared/src/types.ts` -- 공유 TypeScript 인터페이스
> - `packages/shared/src/validators.ts` -- Zod 유효성 검증 스키마
> - `packages/shared/src/constants.ts` -- 플랜/역할/온보딩 상수
> - `apps/web/lib/embeddings.ts` -- 벡터 임베딩 생성 및 직렬화
> - `apps/web/lib/fts.ts` -- FTS5 전문 검색 설정

---

## 1. 데이터베이스 구성

### 1.1 Turso / libSQL

memctl은 **Turso**(libSQL 호스팅 서비스)를 주 데이터베이스로 사용한다. SQLite 호환 프로토콜을 따르며, HTTP 기반 원격 접속을 지원한다.

환경 변수:

| 변수명 | 용도 |
|---|---|
| `TURSO_DATABASE_URL` | 기본 DB URL (우선순위 1) |
| `DATABASE_URL` | 대체 DB URL (우선순위 2) |
| `TURSO_AUTH_TOKEN` | 인증 토큰 |

### 1.2 Drizzle ORM 설정

ORM으로 **Drizzle ORM** (`drizzle-orm@^0.41.0`)을 사용하며, 스키마 기반 type-safe 쿼리를 지원한다.

**클라이언트 팩토리** (`packages/db/src/client.ts`):

```typescript
import { drizzle } from "drizzle-orm/libsql/web";
import { createClient } from "@libsql/client/web";

export function createDb(url?: string, authToken?: string) {
  const resolvedUrl = resolveDbUrl(url);
  const client = createClient({
    url: resolvedUrl,
    authToken: authToken ?? process.env.TURSO_AUTH_TOKEN,
  });
  return drizzle(client, { schema });
}
```

- `@libsql/client/web` 패키지의 `createClient`로 HTTP 기반 libSQL 클라이언트를 생성한다.
- `drizzle()` 호출 시 `{ schema }` 전체를 전달하여 관계형 쿼리(relational queries)를 활성화한다.
- URL 해석 순서: 인자 > `TURSO_DATABASE_URL` > `DATABASE_URL`. 모두 없으면 에러를 던진다.
- `Database` 타입은 `ReturnType<typeof createDb>`로 추론된다.

### 1.3 Drizzle Kit 설정 및 마이그레이션

`packages/db/drizzle.config.ts`:

```typescript
export default defineConfig({
  schema: "./src/schema.ts",
  out: "./src/migrations",
  dialect: "turso",
  dbCredentials: { url, ...(authToken ? { authToken } : {}) },
  tablesFilter: ["!memories_fts*"],
});
```

핵심 사항:
- **dialect**: `"turso"` -- Turso 전용 드라이버를 사용한다.
- **마이그레이션 출력 경로**: `packages/db/src/migrations/`
- **tablesFilter**: `["!memories_fts*"]` -- FTS5 가상 테이블(`memories_fts`)을 마이그레이션 대상에서 제외한다. FTS 테이블은 런타임에 동적으로 생성되기 때문이다.
- 사용 가능한 스크립트: `generate` (DDL 생성), `migrate` (마이그레이션 적용), `push` (스키마 직접 푸시), `studio` (Drizzle Studio UI).

패키지 의존성:
- `@libsql/client@^0.14.0`
- `drizzle-orm@^0.41.0`
- `drizzle-kit@^0.30.4` (devDependencies)

---

## 2. 전체 테이블 목록

총 **28개 테이블** (+ 1개 FTS5 가상 테이블)이 정의되어 있다. 아래에서 도메인별로 분류하여 모든 컬럼을 나열한다.

모든 테이블에서 공통적으로 사용하는 패턴:
- **Primary key**: `text("id")` -- 어플리케이션 레벨에서 생성하는 문자열 ID
- **Timestamp**: `integer(..., { mode: "timestamp" })` -- Unix epoch를 정수로 저장하며 Drizzle가 `Date` 객체로 자동 변환한다.
- **Boolean**: `integer(..., { mode: "boolean" })` -- SQLite에 0/1로 저장된다.
- **JSON 필드**: `text` 타입으로 JSON 문자열을 저장한다.

---

### 2.1 사용자 / 인증 (Users & Authentication)

#### 2.1.1 `users`

사용자 계정 정보를 저장한다.

| 컬럼 | DB 컬럼명 | 타입 | 제약 조건 | 기본값 | 설명 |
|---|---|---|---|---|---|
| id | id | text | PRIMARY KEY | -- | 사용자 고유 ID |
| name | name | text | NOT NULL | -- | 사용자 표시 이름 |
| email | email | text | NOT NULL, UNIQUE | -- | 이메일 주소 |
| emailVerified | email_verified | integer (boolean) | NOT NULL | `false` | 이메일 인증 여부 |
| avatarUrl | avatar_url | text | nullable | -- | 프로필 이미지 URL |
| githubId | github_id | text | UNIQUE, nullable | -- | GitHub OAuth 연동 ID |
| onboardingCompleted | onboarding_completed | integer (boolean) | nullable | `false` | 온보딩 완료 여부 |
| isAdmin | is_admin | integer (boolean) | nullable | `false` | 시스템 관리자 여부 |
| createdAt | created_at | integer (timestamp) | NOT NULL | `new Date()` | 생성 시각 |
| updatedAt | updated_at | integer (timestamp) | NOT NULL | `new Date()` | 수정 시각 |

#### 2.1.2 `sessions`

사용자 인증 세션을 관리한다.

| 컬럼 | DB 컬럼명 | 타입 | 제약 조건 | 기본값 | 설명 |
|---|---|---|---|---|---|
| id | id | text | PRIMARY KEY | -- | 세션 고유 ID |
| userId | user_id | text | NOT NULL, FK -> users.id | -- | 소속 사용자 |
| token | token | text | NOT NULL, UNIQUE | -- | 세션 토큰 |
| expiresAt | expires_at | integer (timestamp) | NOT NULL | -- | 만료 시각 |
| ipAddress | ip_address | text | nullable | -- | 접속 IP |
| userAgent | user_agent | text | nullable | -- | 접속 User-Agent |
| createdAt | created_at | integer (timestamp) | NOT NULL | `new Date()` | 생성 시각 |
| updatedAt | updated_at | integer (timestamp) | NOT NULL | `new Date()` | 수정 시각 |

#### 2.1.3 `accounts`

OAuth 프로바이더 연동 계정 정보를 저장한다 (better-auth 호환 구조).

| 컬럼 | DB 컬럼명 | 타입 | 제약 조건 | 기본값 | 설명 |
|---|---|---|---|---|---|
| id | id | text | PRIMARY KEY | -- | 계정 레코드 ID |
| userId | user_id | text | NOT NULL, FK -> users.id | -- | 소속 사용자 |
| accountId | account_id | text | NOT NULL | -- | 프로바이더 측 계정 ID |
| providerId | provider_id | text | NOT NULL | -- | 프로바이더 식별자 (예: `"github"`) |
| accessToken | access_token | text | nullable | -- | OAuth access token |
| refreshToken | refresh_token | text | nullable | -- | OAuth refresh token |
| idToken | id_token | text | nullable | -- | OIDC ID token |
| accessTokenExpiresAt | access_token_expires_at | integer (timestamp) | nullable | -- | Access token 만료 시각 |
| refreshTokenExpiresAt | refresh_token_expires_at | integer (timestamp) | nullable | -- | Refresh token 만료 시각 |
| scope | scope | text | nullable | -- | OAuth scope 문자열 |
| password | password | text | nullable | -- | 비밀번호 (credential 로그인 시) |
| expiresAt | expires_at | integer (timestamp) | nullable | -- | 계정 만료 시각 |
| createdAt | created_at | integer (timestamp) | NOT NULL | `new Date()` | 생성 시각 |
| updatedAt | updated_at | integer (timestamp) | NOT NULL | `new Date()` | 수정 시각 |

#### 2.1.4 `verifications`

이메일 인증, 비밀번호 재설정 등의 검증 토큰을 저장한다.

| 컬럼 | DB 컬럼명 | 타입 | 제약 조건 | 기본값 | 설명 |
|---|---|---|---|---|---|
| id | id | text | PRIMARY KEY | -- | 검증 레코드 ID |
| identifier | identifier | text | NOT NULL | -- | 검증 대상 식별자 (이메일 등) |
| value | value | text | NOT NULL | -- | 검증 코드/토큰 |
| expiresAt | expires_at | integer (timestamp) | NOT NULL | -- | 만료 시각 |
| createdAt | created_at | integer (timestamp) | nullable | `new Date()` | 생성 시각 |
| updatedAt | updated_at | integer (timestamp) | nullable | `new Date()` | 수정 시각 |

#### 2.1.5 `apiTokens`

API 접근용 토큰을 관리한다. 사용자 + 조직 단위로 발급된다.

| 컬럼 | DB 컬럼명 | 타입 | 제약 조건 | 기본값 | 설명 |
|---|---|---|---|---|---|
| id | id | text | PRIMARY KEY | -- | 토큰 레코드 ID |
| userId | user_id | text | NOT NULL, FK -> users.id | -- | 발급 사용자 |
| orgId | org_id | text | NOT NULL, FK -> organizations.id | -- | 소속 조직 |
| name | name | text | nullable | -- | 토큰 표시 이름 |
| tokenHash | token_hash | text | NOT NULL | -- | 토큰 해시값 (평문 미저장) |
| lastUsedAt | last_used_at | integer (timestamp) | nullable | -- | 마지막 사용 시각 |
| expiresAt | expires_at | integer (timestamp) | nullable | -- | 만료 시각 |
| createdAt | created_at | integer (timestamp) | NOT NULL | `new Date()` | 생성 시각 |
| revokedAt | revoked_at | integer (timestamp) | nullable | -- | 폐기 시각 |

---

### 2.2 조직 / 프로젝트 (Organizations & Projects)

#### 2.2.1 `organizations`

조직(팀/회사) 정보를 저장한다. 과금, 상태 관리, 커스텀 제한, 계약 정보 등 다양한 필드를 포함한다.

| 컬럼 | DB 컬럼명 | 타입 | 제약 조건 | 기본값 | 설명 |
|---|---|---|---|---|---|
| id | id | text | PRIMARY KEY | -- | 조직 고유 ID |
| name | name | text | NOT NULL | -- | 조직 표시 이름 |
| slug | slug | text | NOT NULL, UNIQUE | -- | URL 슬러그 |
| ownerId | owner_id | text | NOT NULL, FK -> users.id | -- | 조직 소유자 |
| planId | plan_id | text | NOT NULL | `"free"` | 구독 플랜 ID |
| stripeCustomerId | stripe_customer_id | text | nullable | -- | Stripe 고객 ID |
| stripeSubscriptionId | stripe_subscription_id | text | nullable | -- | Stripe 구독 ID |
| projectLimit | project_limit | integer | NOT NULL | `3` | 프로젝트 수 제한 |
| memberLimit | member_limit | integer | NOT NULL | `1` | 멤버 수 제한 |
| companyName | company_name | text | nullable | -- | 법인명 (청구서용) |
| taxId | tax_id | text | nullable | -- | 사업자 등록 번호 |
| billingAddress | billing_address | text | nullable | -- | 청구 주소 |
| status | status | text | NOT NULL | `"active"` | 조직 상태 (`"active"` / `"suspended"` / `"banned"`) |
| statusReason | status_reason | text | nullable | -- | 상태 변경 사유 |
| statusChangedAt | status_changed_at | integer (timestamp) | nullable | -- | 상태 변경 시각 |
| statusChangedBy | status_changed_by | text | nullable | -- | 상태 변경자 |
| adminNotes | admin_notes | text | nullable | -- | 관리자 메모 |
| planOverride | plan_override | text | nullable | -- | 플랜 수동 오버라이드 |
| memoryLimitPerProject | memory_limit_per_project | integer | nullable | -- | 프로젝트당 메모리 제한 (커스텀) |
| apiRatePerMinute | api_rate_per_minute | integer | nullable | -- | 분당 API 호출 제한 (커스텀) |
| customLimits | custom_limits | integer (boolean) | nullable | `false` | 커스텀 제한 활성화 여부 |
| planExpiresAt | plan_expires_at | integer (timestamp) | nullable | -- | 플랜 만료 시각 |
| trialEndsAt | trial_ends_at | integer (timestamp) | nullable | -- | 트라이얼 종료 시각 |
| stripeMeteredItemId | stripe_metered_item_id | text | nullable | -- | Stripe 종량제 항목 ID |
| meteredBilling | metered_billing | integer (boolean) | nullable | `false` | 종량제 과금 여부 |
| contractValue | contract_value | integer | nullable | -- | 계약 금액 (cents) |
| contractNotes | contract_notes | text | nullable | -- | 계약 메모 |
| contractStartDate | contract_start_date | integer (timestamp) | nullable | -- | 계약 시작일 |
| contractEndDate | contract_end_date | integer (timestamp) | nullable | -- | 계약 종료일 |
| planTemplateId | plan_template_id | text | nullable | -- | 적용된 플랜 템플릿 ID |
| createdAt | created_at | integer (timestamp) | NOT NULL | `new Date()` | 생성 시각 |
| updatedAt | updated_at | integer (timestamp) | NOT NULL | `new Date()` | 수정 시각 |

#### 2.2.2 `organizationMembers`

조직과 사용자의 다대다 관계를 표현한다.

| 컬럼 | DB 컬럼명 | 타입 | 제약 조건 | 기본값 | 설명 |
|---|---|---|---|---|---|
| id | id | text | PRIMARY KEY | -- | 레코드 ID |
| orgId | org_id | text | NOT NULL, FK -> organizations.id | -- | 소속 조직 |
| userId | user_id | text | NOT NULL, FK -> users.id | -- | 소속 사용자 |
| role | role | text | NOT NULL | `"member"` | 역할 (`"owner"` / `"admin"` / `"member"`) |
| createdAt | created_at | integer (timestamp) | NOT NULL | `new Date()` | 가입 시각 |

**UNIQUE 제약**: `(org_id, user_id)` -- 한 사용자는 한 조직에 한 번만 가입할 수 있다.

#### 2.2.3 `projects`

프로젝트는 조직 하위에 속하며, 메모리의 최상위 컨테이너 역할을 한다.

| 컬럼 | DB 컬럼명 | 타입 | 제약 조건 | 기본값 | 설명 |
|---|---|---|---|---|---|
| id | id | text | PRIMARY KEY | -- | 프로젝트 고유 ID |
| orgId | org_id | text | NOT NULL, FK -> organizations.id | -- | 소속 조직 |
| name | name | text | NOT NULL | -- | 프로젝트 이름 |
| slug | slug | text | NOT NULL | -- | URL 슬러그 |
| description | description | text | nullable | -- | 프로젝트 설명 |
| createdAt | created_at | integer (timestamp) | NOT NULL | `new Date()` | 생성 시각 |
| updatedAt | updated_at | integer (timestamp) | NOT NULL | `new Date()` | 수정 시각 |

**UNIQUE 제약**: `(org_id, slug)` -- 조직 내에서 슬러그는 고유해야 한다.

#### 2.2.4 `projectMembers`

프로젝트와 사용자의 다대다 관계를 표현한다. 프로젝트별 접근 제어에 사용된다.

| 컬럼 | DB 컬럼명 | 타입 | 제약 조건 | 기본값 | 설명 |
|---|---|---|---|---|---|
| id | id | text | PRIMARY KEY | -- | 레코드 ID |
| projectId | project_id | text | NOT NULL, FK -> projects.id (CASCADE) | -- | 소속 프로젝트 |
| userId | user_id | text | NOT NULL, FK -> users.id | -- | 소속 사용자 |
| createdAt | created_at | integer (timestamp) | NOT NULL | `new Date()` | 할당 시각 |

**UNIQUE 제약**: `(project_id, user_id)`.
**CASCADE**: 프로젝트 삭제 시 멤버 레코드도 함께 삭제된다.

#### 2.2.5 `orgInvitations`

조직 초대 정보를 저장한다. 이메일 기반으로 초대하며 만료 시각을 가진다.

| 컬럼 | DB 컬럼명 | 타입 | 제약 조건 | 기본값 | 설명 |
|---|---|---|---|---|---|
| id | id | text | PRIMARY KEY | -- | 초대 레코드 ID |
| orgId | org_id | text | NOT NULL, FK -> organizations.id | -- | 초대 대상 조직 |
| email | email | text | NOT NULL | -- | 초대 대상 이메일 |
| role | role | text | NOT NULL | `"member"` | 초대 역할 |
| invitedBy | invited_by | text | NOT NULL, FK -> users.id | -- | 초대한 사용자 |
| acceptedAt | accepted_at | integer (timestamp) | nullable | -- | 수락 시각 |
| expiresAt | expires_at | integer (timestamp) | NOT NULL | -- | 초대 만료 시각 |
| createdAt | created_at | integer (timestamp) | NOT NULL | `new Date()` | 생성 시각 |

**UNIQUE 제약**: `(org_id, email)` -- 같은 이메일로 중복 초대를 방지한다.
**인덱스**: `org_invitations_email` on `(email)`.

---

### 2.3 메모리 핵심 (Memory Core)

#### 2.3.1 `memories`

메모리(컨텍스트 항목)의 핵심 테이블이다. 프로젝트에 속하며 key-value 구조로 저장된다.

| 컬럼 | DB 컬럼명 | 타입 | 제약 조건 | 기본값 | 설명 |
|---|---|---|---|---|---|
| id | id | text | PRIMARY KEY | -- | 메모리 고유 ID |
| projectId | project_id | text | NOT NULL, FK -> projects.id | -- | 소속 프로젝트 |
| key | key | text | NOT NULL | -- | 메모리 키 (프로젝트 내 고유) |
| content | content | text | NOT NULL | -- | 메모리 본문 (최대 16,384자) |
| metadata | metadata | text | nullable | -- | JSON 메타데이터 (contextType 등) |
| scope | scope | text | NOT NULL | `"project"` | 범위: `"project"` 또는 `"shared"` |
| priority | priority | integer | nullable | `0` | 우선순위 (0~100) |
| tags | tags | text | nullable | -- | JSON 배열 (태그 문자열 목록) |
| relatedKeys | related_keys | text | nullable | -- | JSON 배열 (관련 메모리 키 목록) |
| pinnedAt | pinned_at | integer (timestamp) | nullable | -- | 고정 시각 (null이면 미고정) |
| archivedAt | archived_at | integer (timestamp) | nullable | -- | 아카이브 시각 (null이면 활성) |
| expiresAt | expires_at | integer (timestamp) | nullable | -- | 만료 시각 (자동 삭제용) |
| accessCount | access_count | integer | NOT NULL | `0` | 조회 횟수 |
| lastAccessedAt | last_accessed_at | integer (timestamp) | nullable | -- | 마지막 조회 시각 |
| helpfulCount | helpful_count | integer | NOT NULL | `0` | "유용함" 피드백 횟수 |
| unhelpfulCount | unhelpful_count | integer | NOT NULL | `0` | "유용하지 않음" 피드백 횟수 |
| embedding | embedding | text | nullable | -- | 벡터 임베딩 (JSON 직렬화, Int8 양자화 또는 레거시 Float32) |
| createdBy | created_by | text | nullable, FK -> users.id | -- | 생성자 |
| createdAt | created_at | integer (timestamp) | NOT NULL | `new Date()` | 생성 시각 |
| updatedAt | updated_at | integer (timestamp) | NOT NULL | `new Date()` | 수정 시각 |

**UNIQUE 제약**: `(project_id, key)` -- 프로젝트 내에서 키는 고유하다 (upsert 패턴 사용).

#### 2.3.2 `memoryVersions`

메모리의 변경 이력을 추적한다. 메모리 수정 전 이전 상태를 버전으로 저장한다.

| 컬럼 | DB 컬럼명 | 타입 | 제약 조건 | 기본값 | 설명 |
|---|---|---|---|---|---|
| id | id | text | PRIMARY KEY | -- | 버전 레코드 ID |
| memoryId | memory_id | text | NOT NULL, FK -> memories.id (CASCADE) | -- | 대상 메모리 |
| version | version | integer | NOT NULL | -- | 버전 번호 (1부터 순차 증가) |
| content | content | text | NOT NULL | -- | 해당 버전의 content 스냅샷 |
| metadata | metadata | text | nullable | -- | 해당 버전의 metadata 스냅샷 |
| changedBy | changed_by | text | nullable, FK -> users.id | -- | 변경자 |
| changeType | change_type | text | NOT NULL | -- | 변경 유형: `"created"` / `"updated"` / `"restored"` |
| createdAt | created_at | integer (timestamp) | NOT NULL | `new Date()` | 버전 생성 시각 |

**CASCADE**: 메모리 삭제 시 모든 버전도 함께 삭제된다.

#### 2.3.3 `memorySnapshots`

프로젝트 전체 메모리의 시점별 스냅샷을 저장한다.

| 컬럼 | DB 컬럼명 | 타입 | 제약 조건 | 기본값 | 설명 |
|---|---|---|---|---|---|
| id | id | text | PRIMARY KEY | -- | 스냅샷 ID |
| projectId | project_id | text | NOT NULL, FK -> projects.id | -- | 대상 프로젝트 |
| name | name | text | NOT NULL | -- | 스냅샷 이름 |
| description | description | text | nullable | -- | 스냅샷 설명 |
| data | data | text | NOT NULL | -- | JSON: 전체 메모리 데이터 직렬화 |
| memoryCount | memory_count | integer | NOT NULL | -- | 스냅샷 시점의 메모리 개수 |
| createdBy | created_by | text | nullable, FK -> users.id | -- | 생성자 |
| createdAt | created_at | integer (timestamp) | NOT NULL | `new Date()` | 생성 시각 |

#### 2.3.4 `memoryLocks`

메모리에 대한 동시 쓰기 충돌을 방지하기 위한 잠금 테이블이다.

| 컬럼 | DB 컬럼명 | 타입 | 제약 조건 | 기본값 | 설명 |
|---|---|---|---|---|---|
| id | id | text | PRIMARY KEY | -- | 잠금 레코드 ID |
| projectId | project_id | text | NOT NULL, FK -> projects.id | -- | 대상 프로젝트 |
| memoryKey | memory_key | text | NOT NULL | -- | 잠금 대상 메모리 키 |
| lockedBy | locked_by | text | nullable | -- | 잠금 소유자 (세션/에이전트 ID) |
| expiresAt | expires_at | integer (timestamp) | NOT NULL | -- | 잠금 만료 시각 |
| createdAt | created_at | integer (timestamp) | NOT NULL | `new Date()` | 잠금 생성 시각 |

**UNIQUE 제약**: `(project_id, memory_key)` -- 하나의 메모리에 대해 동시에 하나의 잠금만 존재할 수 있다.

#### 2.3.5 `orgMemoryDefaults`

조직 수준의 기본 메모리를 정의한다. 새 프로젝트 생성 시 이 기본값이 복사된다.

| 컬럼 | DB 컬럼명 | 타입 | 제약 조건 | 기본값 | 설명 |
|---|---|---|---|---|---|
| id | id | text | PRIMARY KEY | -- | 레코드 ID |
| orgId | org_id | text | NOT NULL, FK -> organizations.id | -- | 소속 조직 |
| key | key | text | NOT NULL | -- | 메모리 키 |
| content | content | text | NOT NULL | -- | 메모리 본문 |
| metadata | metadata | text | nullable | -- | JSON 메타데이터 |
| priority | priority | integer | nullable | `0` | 우선순위 |
| tags | tags | text | nullable | -- | JSON 배열 (태그 목록) |
| createdBy | created_by | text | nullable, FK -> users.id | -- | 생성자 |
| createdAt | created_at | integer (timestamp) | NOT NULL | `new Date()` | 생성 시각 |
| updatedAt | updated_at | integer (timestamp) | NOT NULL | `new Date()` | 수정 시각 |

**UNIQUE 제약**: `(org_id, key)` -- 조직 내에서 기본 메모리 키는 고유하다.

---

### 2.4 활동 / 감사 (Activity & Audit)

#### 2.4.1 `sessionLogs`

AI 에이전트 세션의 활동 요약을 기록한다.

| 컬럼 | DB 컬럼명 | 타입 | 제약 조건 | 기본값 | 설명 |
|---|---|---|---|---|---|
| id | id | text | PRIMARY KEY | -- | 세션 로그 ID |
| projectId | project_id | text | NOT NULL, FK -> projects.id | -- | 대상 프로젝트 |
| sessionId | session_id | text | NOT NULL | -- | 에이전트 세션 식별자 |
| branch | branch | text | nullable | -- | Git 브랜치명 |
| summary | summary | text | nullable | -- | 세션 요약 |
| keysRead | keys_read | text | nullable | -- | JSON 배열: 읽은 메모리 키 목록 |
| keysWritten | keys_written | text | nullable | -- | JSON 배열: 쓴 메모리 키 목록 |
| toolsUsed | tools_used | text | nullable | -- | JSON 배열: 사용한 도구 이름 목록 |
| startedAt | started_at | integer (timestamp) | NOT NULL | `new Date()` | 세션 시작 시각 |
| endedAt | ended_at | integer (timestamp) | nullable | -- | 세션 종료 시각 |
| lastActivityAt | last_activity_at | integer (timestamp) | nullable | -- | 마지막 활동 시각 |
| createdBy | created_by | text | nullable, FK -> users.id | -- | 세션 사용자 |

#### 2.4.2 `activityLogs`

메모리 읽기/쓰기/삭제 등 개별 활동 이벤트를 기록한다.

| 컬럼 | DB 컬럼명 | 타입 | 제약 조건 | 기본값 | 설명 |
|---|---|---|---|---|---|
| id | id | text | PRIMARY KEY | -- | 활동 로그 ID |
| projectId | project_id | text | NOT NULL, FK -> projects.id | -- | 대상 프로젝트 |
| sessionId | session_id | text | nullable | -- | 연관 세션 ID |
| action | action | text | NOT NULL | -- | 동작 유형: `"tool_call"` / `"memory_read"` / `"memory_write"` / `"memory_delete"` |
| toolName | tool_name | text | nullable | -- | 사용된 도구 이름 |
| memoryKey | memory_key | text | nullable | -- | 대상 메모리 키 |
| details | details | text | nullable | -- | JSON: 추가 정보 |
| createdBy | created_by | text | nullable, FK -> users.id | -- | 활동 수행자 |
| createdAt | created_at | integer (timestamp) | NOT NULL | `new Date()` | 활동 시각 |

#### 2.4.3 `auditLogs`

조직/프로젝트 수준의 관리 활동(역할 변경, 멤버 관리 등)을 기록한다.

| 컬럼 | DB 컬럼명 | 타입 | 제약 조건 | 기본값 | 설명 |
|---|---|---|---|---|---|
| id | id | text | PRIMARY KEY | -- | 감사 로그 ID |
| orgId | org_id | text | NOT NULL, FK -> organizations.id | -- | 대상 조직 |
| projectId | project_id | text | nullable, FK -> projects.id | -- | 대상 프로젝트 (해당 시) |
| actorId | actor_id | text | NOT NULL, FK -> users.id | -- | 수행자 |
| action | action | text | NOT NULL | -- | 동작 유형 (아래 참조) |
| targetUserId | target_user_id | text | nullable, FK -> users.id | -- | 대상 사용자 |
| details | details | text | nullable | -- | JSON: 추가 컨텍스트 |
| createdAt | created_at | integer (timestamp) | NOT NULL | `new Date()` | 발생 시각 |

`action` 값 목록: `"role_changed"`, `"member_removed"`, `"member_assigned"`, `"member_unassigned"`, `"project_created"`, `"project_updated"`, `"project_deleted"`.

#### 2.4.4 `adminActions`

시스템 관리자의 조직 대상 관리 행위를 기록한다.

| 컬럼 | DB 컬럼명 | 타입 | 제약 조건 | 기본값 | 설명 |
|---|---|---|---|---|---|
| id | id | text | PRIMARY KEY | -- | 관리 행위 ID |
| orgId | org_id | text | NOT NULL, FK -> organizations.id | -- | 대상 조직 |
| adminId | admin_id | text | NOT NULL, FK -> users.id | -- | 수행 관리자 |
| action | action | text | NOT NULL | -- | 관리 동작명 |
| details | details | text | nullable | -- | JSON: 상세 정보 |
| createdAt | created_at | integer (timestamp) | NOT NULL | `new Date()` | 수행 시각 |

---

### 2.5 설정 (Configuration)

#### 2.5.1 `contextTypes`

조직별 커스텀 컨텍스트 타입을 정의한다. 메모리 메타데이터의 `contextType` 필드와 연동되어 내용 검증에 사용된다.

| 컬럼 | DB 컬럼명 | 타입 | 제약 조건 | 기본값 | 설명 |
|---|---|---|---|---|---|
| id | id | text | PRIMARY KEY | -- | 컨텍스트 타입 ID |
| orgId | org_id | text | NOT NULL, FK -> organizations.id | -- | 소속 조직 |
| slug | slug | text | NOT NULL | -- | 타입 슬러그 |
| label | label | text | NOT NULL | -- | 표시 레이블 |
| description | description | text | NOT NULL | -- | 설명 |
| schema | schema | text | nullable | -- | JSON Schema (내용 검증용) |
| icon | icon | text | nullable | -- | 아이콘 이름 |
| createdAt | created_at | integer (timestamp) | NOT NULL | `new Date()` | 생성 시각 |
| updatedAt | updated_at | integer (timestamp) | NOT NULL | `new Date()` | 수정 시각 |

**UNIQUE 제약**: `(org_id, slug)`.

#### 2.5.2 `projectTemplates`

프로젝트 초기화 시 적용할 수 있는 메모리 템플릿이다.

| 컬럼 | DB 컬럼명 | 타입 | 제약 조건 | 기본값 | 설명 |
|---|---|---|---|---|---|
| id | id | text | PRIMARY KEY | -- | 템플릿 ID |
| orgId | org_id | text | NOT NULL, FK -> organizations.id | -- | 소속 조직 |
| name | name | text | NOT NULL | -- | 템플릿 이름 |
| description | description | text | nullable | -- | 설명 |
| data | data | text | NOT NULL | -- | JSON 배열: `{ key, content, metadata, priority, tags }` 항목들 |
| isBuiltin | is_builtin | integer (boolean) | nullable | `false` | 시스템 기본 템플릿 여부 |
| createdBy | created_by | text | nullable, FK -> users.id | -- | 생성자 |
| createdAt | created_at | integer (timestamp) | NOT NULL | `new Date()` | 생성 시각 |

#### 2.5.3 `planTemplates`

관리자가 Enterprise 고객을 위해 미리 정의하는 플랜 템플릿이다.

| 컬럼 | DB 컬럼명 | 타입 | 제약 조건 | 기본값 | 설명 |
|---|---|---|---|---|---|
| id | id | text | PRIMARY KEY | -- | 템플릿 ID |
| name | name | text | NOT NULL | -- | 템플릿 이름 |
| description | description | text | nullable | -- | 설명 |
| basePlanId | base_plan_id | text | NOT NULL | `"enterprise"` | 기반 플랜 ID |
| projectLimit | project_limit | integer | NOT NULL | -- | 프로젝트 수 제한 |
| memberLimit | member_limit | integer | NOT NULL | -- | 멤버 수 제한 |
| memoryLimitPerProject | memory_limit_per_project | integer | NOT NULL | -- | 프로젝트당 메모리 제한 |
| apiRatePerMinute | api_rate_per_minute | integer | NOT NULL | -- | 분당 API 호출 제한 |
| stripePriceInCents | stripe_price_in_cents | integer | nullable | -- | Stripe 가격 (cents) |
| isArchived | is_archived | integer (boolean) | nullable | `false` | 보관 여부 |
| createdAt | created_at | integer (timestamp) | NOT NULL | `new Date()` | 생성 시각 |
| updatedAt | updated_at | integer (timestamp) | NOT NULL | `new Date()` | 수정 시각 |

---

### 2.6 과금 (Billing & Promotions)

#### 2.6.1 `promoCodes`

프로모션 코드를 관리한다. Stripe 쿠폰/프로모 코드와 연동된다.

| 컬럼 | DB 컬럼명 | 타입 | 제약 조건 | 기본값 | 설명 |
|---|---|---|---|---|---|
| id | id | text | PRIMARY KEY | -- | 프로모 코드 ID |
| code | code | text | NOT NULL, UNIQUE | -- | 프로모 코드 문자열 |
| description | description | text | nullable | -- | 설명 |
| campaign | campaign | text | nullable | -- | 캠페인 식별자 |
| stripeCouponId | stripe_coupon_id | text | NOT NULL | -- | Stripe 쿠폰 ID |
| stripePromoCodeId | stripe_promo_code_id | text | NOT NULL | -- | Stripe 프로모 코드 ID |
| discountType | discount_type | text | NOT NULL | -- | 할인 유형: `"percent"` / `"fixed"` |
| discountAmount | discount_amount | integer | NOT NULL | -- | 할인 금액 (퍼센트 또는 cents) |
| currency | currency | text | nullable | `"usd"` | 통화 코드 |
| duration | duration | text | NOT NULL | -- | 적용 기간: `"once"` / `"repeating"` / `"forever"` |
| durationInMonths | duration_in_months | integer | nullable | -- | 반복 기간 (월, duration="repeating" 시) |
| applicablePlans | applicable_plans | text | nullable | -- | JSON 배열: 적용 가능 플랜 ID (null=전체) |
| minimumPlanTier | minimum_plan_tier | text | nullable | -- | 최소 적용 플랜 티어 |
| restrictedToOrgs | restricted_to_orgs | text | nullable | -- | JSON 배열: 제한 대상 조직 ID (null=전체) |
| maxRedemptions | max_redemptions | integer | nullable | -- | 총 사용 횟수 제한 (null=무제한) |
| maxRedemptionsPerOrg | max_redemptions_per_org | integer | nullable | `1` | 조직당 사용 횟수 제한 |
| firstSubscriptionOnly | first_subscription_only | integer (boolean) | nullable | `false` | 첫 구독에만 적용 여부 |
| noPreviousPromo | no_previous_promo | integer (boolean) | nullable | `false` | 이전 프로모 사용 시 제외 여부 |
| startsAt | starts_at | integer (timestamp) | nullable | -- | 유효 시작 시각 |
| expiresAt | expires_at | integer (timestamp) | nullable | -- | 유효 종료 시각 |
| active | active | integer (boolean) | nullable | `true` | 활성 상태 |
| timesRedeemed | times_redeemed | integer | NOT NULL | `0` | 총 사용 횟수 |
| totalDiscountGiven | total_discount_given | integer | NOT NULL | `0` | 총 할인 금액 (cents) |
| createdBy | created_by | text | nullable, FK -> users.id | -- | 생성자 |
| createdAt | created_at | integer (timestamp) | NOT NULL | `new Date()` | 생성 시각 |
| updatedAt | updated_at | integer (timestamp) | NOT NULL | `new Date()` | 수정 시각 |

#### 2.6.2 `promoRedemptions`

프로모 코드 사용 이력을 기록한다.

| 컬럼 | DB 컬럼명 | 타입 | 제약 조건 | 기본값 | 설명 |
|---|---|---|---|---|---|
| id | id | text | PRIMARY KEY | -- | 사용 이력 ID |
| promoCodeId | promo_code_id | text | NOT NULL, FK -> promoCodes.id | -- | 사용된 프로모 코드 |
| orgId | org_id | text | NOT NULL, FK -> organizations.id | -- | 사용 조직 |
| userId | user_id | text | NOT NULL, FK -> users.id | -- | 사용자 |
| planId | plan_id | text | NOT NULL | -- | 적용 대상 플랜 |
| discountApplied | discount_applied | integer | NOT NULL | -- | 적용된 할인 금액 |
| stripeCheckoutSessionId | stripe_checkout_session_id | text | nullable | -- | Stripe Checkout 세션 ID |
| redeemedAt | redeemed_at | integer (timestamp) | NOT NULL | `new Date()` | 사용 시각 |

---

### 2.7 콘텐츠 (Content)

#### 2.7.1 `blogPosts`

블로그 글을 관리한다.

| 컬럼 | DB 컬럼명 | 타입 | 제약 조건 | 기본값 | 설명 |
|---|---|---|---|---|---|
| id | id | text | PRIMARY KEY | -- | 글 ID |
| slug | slug | text | NOT NULL, UNIQUE | -- | URL 슬러그 |
| title | title | text | NOT NULL | -- | 제목 |
| excerpt | excerpt | text | nullable | -- | 발췌문 |
| content | content | text | NOT NULL | -- | 본문 |
| coverImageUrl | cover_image_url | text | nullable | -- | 커버 이미지 URL |
| authorId | author_id | text | NOT NULL, FK -> users.id | -- | 저자 |
| status | status | text | NOT NULL | `"draft"` | 상태: `"draft"` / `"published"` 등 |
| publishedAt | published_at | integer (timestamp) | nullable | -- | 게시 시각 |
| createdAt | created_at | integer (timestamp) | NOT NULL | `new Date()` | 생성 시각 |
| updatedAt | updated_at | integer (timestamp) | NOT NULL | `new Date()` | 수정 시각 |

#### 2.7.2 `changelogEntries`

변경 로그의 버전 단위 항목이다.

| 컬럼 | DB 컬럼명 | 타입 | 제약 조건 | 기본값 | 설명 |
|---|---|---|---|---|---|
| id | id | text | PRIMARY KEY | -- | 항목 ID |
| version | version | text | NOT NULL, UNIQUE | -- | 버전 문자열 |
| title | title | text | NOT NULL | -- | 제목 |
| summary | summary | text | nullable | -- | 요약 |
| releaseDate | release_date | integer (timestamp) | NOT NULL | -- | 릴리즈 날짜 |
| status | status | text | NOT NULL | `"draft"` | 상태 |
| authorId | author_id | text | NOT NULL, FK -> users.id | -- | 저자 |
| createdAt | created_at | integer (timestamp) | NOT NULL | `new Date()` | 생성 시각 |
| updatedAt | updated_at | integer (timestamp) | NOT NULL | `new Date()` | 수정 시각 |

#### 2.7.3 `changelogItems`

변경 로그 항목 내의 개별 변경 사항이다.

| 컬럼 | DB 컬럼명 | 타입 | 제약 조건 | 기본값 | 설명 |
|---|---|---|---|---|---|
| id | id | text | PRIMARY KEY | -- | 항목 ID |
| entryId | entry_id | text | NOT NULL, FK -> changelogEntries.id (CASCADE) | -- | 상위 변경 로그 항목 |
| category | category | text | NOT NULL | -- | 변경 카테고리 |
| description | description | text | NOT NULL | -- | 변경 설명 |
| sortOrder | sort_order | integer | NOT NULL | `0` | 정렬 순서 |

**CASCADE**: changelogEntry 삭제 시 하위 item도 함께 삭제된다.

#### 2.7.4 `onboardingResponses`

사용자 온보딩 설문 응답을 저장한다.

| 컬럼 | DB 컬럼명 | 타입 | 제약 조건 | 기본값 | 설명 |
|---|---|---|---|---|---|
| id | id | text | PRIMARY KEY | -- | 응답 ID |
| userId | user_id | text | NOT NULL, FK -> users.id | -- | 응답 사용자 |
| heardFrom | heard_from | text | nullable | -- | 서비스 인지 경로 |
| role | role | text | nullable | -- | 직무 역할 |
| teamSize | team_size | text | nullable | -- | 팀 규모 |
| useCase | use_case | text | nullable | -- | 사용 목적 |
| createdAt | created_at | integer (timestamp) | NOT NULL | `new Date()` | 응답 시각 |

---

## 3. 인덱스

스키마에 명시적으로 정의된 모든 인덱스를 나열한다. UNIQUE 제약은 위 테이블 정의에서 다루었으므로, 여기서는 성능용 인덱스만 포함한다.

### 3.1 memories 테이블 인덱스

| 인덱스 이름 | 대상 컬럼 | 용도 |
|---|---|---|
| `memories_project_updated` | `(project_id, updated_at)` | 프로젝트 내 최근 수정순 조회 |
| `memories_project_archived` | `(project_id, archived_at)` | 프로젝트 내 아카이브 상태 필터링 |
| `memories_project_priority` | `(project_id, priority)` | 프로젝트 내 우선순위순 조회 |
| `memories_project_created` | `(project_id, created_at)` | 프로젝트 내 생성순 조회 |

### 3.2 memoryVersions 테이블 인덱스

| 인덱스 이름 | 대상 컬럼 | 용도 |
|---|---|---|
| `memory_versions_memory_version` | `(memory_id, version)` | 특정 메모리의 버전 이력 조회 |

### 3.3 activityLogs 테이블 인덱스

| 인덱스 이름 | 대상 컬럼 | 용도 |
|---|---|---|
| `activity_session_action` | `(session_id, action)` | 세션별 동작 유형 조회 |
| `activity_project_action_created` | `(project_id, action, created_at)` | 프로젝트별 시간순 활동 조회 |

### 3.4 auditLogs 테이블 인덱스

| 인덱스 이름 | 대상 컬럼 | 용도 |
|---|---|---|
| `audit_org_created` | `(org_id, created_at)` | 조직별 시간순 감사 로그 |
| `audit_project_created` | `(project_id, created_at)` | 프로젝트별 시간순 감사 로그 |

### 3.5 adminActions 테이블 인덱스

| 인덱스 이름 | 대상 컬럼 | 용도 |
|---|---|---|
| `admin_actions_org_created` | `(org_id, created_at)` | 조직별 시간순 관리 행위 조회 |

### 3.6 orgInvitations 테이블 인덱스

| 인덱스 이름 | 대상 컬럼 | 용도 |
|---|---|---|
| `org_invitations_email` | `(email)` | 이메일로 초대 조회 |

### 3.7 promoCodes 테이블 인덱스

| 인덱스 이름 | 대상 컬럼 | 용도 |
|---|---|---|
| `promo_codes_code` | `(code)` | 코드 문자열로 조회 |
| `promo_codes_active` | `(active)` | 활성 코드 필터링 |
| `promo_codes_campaign` | `(campaign)` | 캠페인별 조회 |
| `promo_codes_created_at` | `(created_at)` | 시간순 정렬 |

### 3.8 promoRedemptions 테이블 인덱스

| 인덱스 이름 | 대상 컬럼 | 용도 |
|---|---|---|
| `promo_redemptions_promo_code_id` | `(promo_code_id)` | 프로모 코드별 사용 이력 |
| `promo_redemptions_org_id` | `(org_id)` | 조직별 프로모 사용 이력 |
| `promo_redemptions_promo_org` | `(promo_code_id, org_id)` | 프로모-조직 복합 조회 (중복 사용 확인) |

---

## 4. 테이블 관계도

아래는 텍스트 기반의 Entity Relationship 다이어그램이다. `1--*`은 일대다, `*--*`은 다대다(조인 테이블 통해) 관계를 표현한다.

```
users
  |
  +--1--* sessions              (userId -> users.id)
  +--1--* accounts              (userId -> users.id)
  +--1--* apiTokens             (userId -> users.id)
  +--1--* onboardingResponses   (userId -> users.id)
  |
  +--1--* organizations         (ownerId -> users.id)
  |        |
  |        +--1--* organizationMembers  (orgId -> organizations.id)
  |        |        +-- userId -> users.id
  |        |
  |        +--1--* projects             (orgId -> organizations.id)
  |        |        |
  |        |        +--1--* memories           (projectId -> projects.id)
  |        |        |        |
  |        |        |        +--1--* memoryVersions  (memoryId -> memories.id, CASCADE)
  |        |        |
  |        |        +--1--* memorySnapshots    (projectId -> projects.id)
  |        |        +--1--* memoryLocks        (projectId -> projects.id)
  |        |        +--1--* sessionLogs        (projectId -> projects.id)
  |        |        +--1--* activityLogs       (projectId -> projects.id)
  |        |        +--1--* projectMembers     (projectId -> projects.id, CASCADE)
  |        |                 +-- userId -> users.id
  |        |
  |        +--1--* orgInvitations       (orgId -> organizations.id)
  |        |        +-- invitedBy -> users.id
  |        |
  |        +--1--* orgMemoryDefaults    (orgId -> organizations.id)
  |        +--1--* contextTypes         (orgId -> organizations.id)
  |        +--1--* projectTemplates     (orgId -> organizations.id)
  |        +--1--* auditLogs            (orgId -> organizations.id)
  |        +--1--* adminActions         (orgId -> organizations.id)
  |        +--1--* promoCodes (via promoRedemptions)
  |
  +--1--* blogPosts             (authorId -> users.id)
  +--1--* changelogEntries      (authorId -> users.id)
           |
           +--1--* changelogItems  (entryId -> changelogEntries.id, CASCADE)

verifications  -- 독립 테이블 (FK 없음)
planTemplates  -- 독립 테이블 (FK 없음, organizations.planTemplateId에서 참조)

promoCodes
  +--1--* promoRedemptions  (promoCodeId -> promoCodes.id)
           +-- orgId -> organizations.id
           +-- userId -> users.id
```

---

## 5. 벡터 임베딩 저장

소스: `apps/web/lib/embeddings.ts`

### 5.1 모델

- **모델**: `Xenova/all-MiniLM-L6-v2` (Hugging Face Transformers.js)
- **차원**: 384 (EMBEDDING_DIM = 384)
- **풀링**: mean pooling
- **정규화**: enabled
- **라이브러리**: `@xenova/transformers`

### 5.2 저장 형식

`memories.embedding` 컬럼에 JSON 문자열로 저장된다. 두 가지 형식이 공존한다:

**Int8 양자화 형식** (현재 기본):
```json
{
  "values": [-128, -45, 12, ...],   // Int8 값 배열 (384개)
  "min": -0.123,                     // 원본 Float32 최솟값
  "max": 0.456                       // 원본 Float32 최댓값
}
```

양자화 과정:
1. Float32 벡터에서 min/max 범위를 계산한다.
2. 각 값을 `((value - min) / range) * 255 - 128`로 변환하여 Int8 범위(-128~127)로 매핑한다.
3. `min`, `max` 값과 함께 저장하여 역양자화를 가능하게 한다.

이 방식은 원본 Float32 JSON(약 3~4KB)을 약 500바이트로 압축한다.

**레거시 Float32 형식** (하위 호환):
```json
[0.123, -0.456, 0.789, ...]         // Float32 값의 plain number 배열 (384개)
```

역직렬화(`deserializeEmbedding`) 시 `values` 속성의 존재 여부로 형식을 자동 판별한다.

### 5.3 생성 프로세스

1. **메모리 생성/수정 시**: 비동기 fire-and-forget으로 임베딩을 생성한다. 입력 텍스트는 `"${key} ${content} ${tags}"` 형태로 조합된다.
2. **배치 백필**: `apps/web/scripts/backfill-embeddings.ts` 스크립트로 임베딩이 없는 메모리를 일괄 처리한다. 50개 단위로 배치 처리하며, 배치 실패 시 개별 순차 처리로 폴백한다.
3. **검색 시**: 쿼리 텍스트에 대해 동일한 모델로 임베딩을 생성하고, 저장된 모든 임베딩과 코사인 유사도를 계산한다. 유사도 임계값은 0.3이다.

### 5.4 검색 통합 (Hybrid Search)

`apps/web/lib/fts.ts`에서 FTS5 결과와 벡터 검색 결과를 **Reciprocal Rank Fusion (RRF)** 알고리즘으로 병합한다:

```
score(id) = sum( 1 / (k + rank + 1) )   // k = 60
```

FTS 순위와 벡터 유사도 순위를 각각 계산한 후, RRF로 최종 순위를 결정한다.

---

## 6. FTS5 설정

소스: `apps/web/lib/fts.ts`

### 6.1 가상 테이블 정의

```sql
CREATE VIRTUAL TABLE IF NOT EXISTS memories_fts USING fts5(
  key,
  content,
  tags,
  content='memories',
  content_rowid='rowid'
)
```

- **인덱스 대상 컬럼**: `key`, `content`, `tags`
- **content table**: `memories` -- content-sync 모드로 실제 데이터는 `memories` 테이블에서 참조한다.
- **content_rowid**: `rowid` -- SQLite의 내부 rowid를 기준으로 연결한다.

### 6.2 동기화 트리거

FTS 인덱스를 `memories` 테이블과 자동 동기화하는 3개의 트리거가 생성된다:

| 트리거 이름 | 이벤트 | 동작 |
|---|---|---|
| `memories_ai` | AFTER INSERT ON memories | FTS에 새 레코드 삽입. `tags`가 NULL이면 빈 문자열로 처리 |
| `memories_ad` | AFTER DELETE ON memories | FTS에서 삭제 커맨드 실행 (`'delete'` 특수 명령) |
| `memories_au` | AFTER UPDATE ON memories | 기존 레코드 삭제 후 새 값으로 재삽입 (delete + insert) |

### 6.3 초기화 및 폴백

- `ensureFts()`는 프로세스당 한 번만 실행된다 (`ftsInitialized` 플래그).
- FTS5를 사용할 수 없는 환경(테스트 등)에서는 조용히 실패하며, `LIKE` 쿼리로 폴백한다.
- `rebuildFtsIndex()` 함수로 FTS 인덱스를 전체 재구축할 수 있다 (`INSERT INTO memories_fts(memories_fts) VALUES ('rebuild')`).

### 6.4 검색 쿼리

FTS5 특수 문자를 이스케이프 처리한 뒤, 각 단어를 큰따옴표로 감싸고 `OR`로 결합한다:

```
입력: "auth token refresh"
FTS 쿼리: "auth" OR "token" OR "refresh"
```

검색 결과는 `memories` 테이블과 `JOIN`하여 `project_id` 필터링 및 `archived_at IS NULL` 조건을 적용한 후, FTS5의 내장 `rank` 함수로 정렬한다.

### 6.5 Drizzle Kit 제외 설정

`drizzle.config.ts`의 `tablesFilter: ["!memories_fts*"]`로 FTS 가상 테이블을 마이그레이션 스캔 대상에서 제외한다. FTS 테이블은 어플리케이션 런타임에서 `ensureFts()`를 통해 동적으로 생성 및 관리된다.

---

## 7. 공유 타입 및 유효성 검증

### 7.1 상수 정의 (`packages/shared/src/constants.ts`)

#### 플랜 ID 및 제한

```typescript
export const PLAN_IDS = ["free", "lite", "pro", "business", "scale", "enterprise"] as const;
```

| 플랜 | 월 가격($) | 프로젝트 제한 | 멤버 제한 | 프로젝트당 메모리 제한 | 분당 API 호출 |
|---|---|---|---|---|---|
| free | 0 | 3 | 1 | 400 | 60 |
| lite | 5 | 10 | 3 | 1,200 | 100 |
| pro | 18 | 25 | 10 | 5,000 | 150 |
| business | 59 | 100 | 30 | 10,000 | 150 |
| scale | 149 | 150 | 100 | 25,000 | 150 |
| enterprise | -1 (커스텀) | 무제한 | 무제한 | 무제한 | 150 |

- `apiCallLimit`은 모든 플랜에서 `Infinity` (무제한)이다. 실질적 제한은 `apiRatePerMinute`(sliding window)가 담당한다.
- `EXTRA_SEAT_PRICE = 8` -- 플랜 포함 좌석 초과 시 인당 월 $8.

#### 조직 상태 및 역할

```typescript
export const ORG_STATUSES = ["active", "suspended", "banned"] as const;
export const ORG_ROLES = ["owner", "admin", "member"] as const;
```

#### 온보딩 옵션

| 항목 | 옵션 |
|---|---|
| `ONBOARDING_HEARD_FROM` | `"github"`, `"twitter"`, `"blog"`, `"friend"`, `"search"`, `"other"` |
| `ONBOARDING_ROLES` | `"developer"`, `"team_lead"`, `"engineering_manager"`, `"other"` |
| `ONBOARDING_TEAM_SIZES` | `"solo"`, `"2-5"`, `"6-20"`, `"20+"` |
| `ONBOARDING_USE_CASES` | `"personal"`, `"team"`, `"enterprise"`, `"open_source"` |

### 7.2 Zod 유효성 검증 스키마 (`packages/shared/src/validators.ts`)

#### 기본 스키마

| 스키마 | 설명 | 주요 규칙 |
|---|---|---|
| `slugSchema` | URL 슬러그 | 1~64자, `/^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?$/` |
| `orgCreateSchema` | 조직 생성 | `name`: 1~128자, `slug`: slugSchema |
| `projectCreateSchema` | 프로젝트 생성 | `name`: 1~128자, `slug`: slugSchema, `description`: 512자 이하 (선택) |
| `projectUpdateSchema` | 프로젝트 수정 | `name`: 1~128자 (선택), `description`: 512자 이하 (선택) |
| `planIdSchema` | 플랜 ID 열거 | `PLAN_IDS` 배열의 값 중 하나 |

#### 메모리 관련 스키마

**Hard/Soft 제한 상수**:
- `MEMORY_CONTENT_HARD_LIMIT = 16384` (16KB) -- 저장 최대 한도
- `MEMORY_CONTENT_SOFT_LIMIT = 4096` (4KB) -- 권장 한도

| 스키마 | 설명 | 주요 규칙 |
|---|---|---|
| `memoryStoreSchema` | 메모리 생성/upsert | `key`: 1~256자, `content`: 1~16384자, `metadata`: 레코드 (선택), `scope`: `"project"/"shared"` (기본 `"project"`), `priority`: 0~100 정수 (선택), `tags`: 최대 20개 문자열 배열, 각 1~64자 (선택), `expiresAt`: 정수 타임스탬프 (선택) |
| `memoryUpdateSchema` | 메모리 부분 수정 | `content`: 1~16384자 (선택), `metadata`: 레코드 (선택), `priority`: 0~100 (선택), `tags`: 20개 이하 배열 (선택), `expiresAt`: 정수 또는 null (선택) |
| `memorySearchSchema` | 메모리 검색 | `query`: 1~256자, `limit`: 1~100 정수 (기본 20), `intent`: SearchIntent 열거값 (선택) |
| `memoryBulkGetSchema` | 메모리 일괄 조회 | `keys`: 1~50개 문자열 배열, 각 1~256자 |

#### 컨텍스트 타입 스키마

| 스키마 | 설명 | 주요 규칙 |
|---|---|---|
| `contextTypeCreateSchema` | 컨텍스트 타입 생성 | `slug`: slugSchema, `label`: 1~128자, `description`: 1~512자, `schema`: 65536자 이하 (선택), `icon`: 64자 이하 (선택) |
| `contextTypeUpdateSchema` | 컨텍스트 타입 수정 | 위 필드 모두 선택 |

#### 인증/조직 관리 스키마

| 스키마 | 설명 | 주요 규칙 |
|---|---|---|
| `apiTokenCreateSchema` | API 토큰 생성 | `name`: 1~128자, `expiresAt`: 정수 (선택) |
| `onboardingSchema` | 온보딩 응답 | `heardFrom`/`role`/`teamSize`/`useCase`: 문자열 (선택), `orgName`: 1~128자, `orgSlug`: slugSchema |
| `memberInviteSchema` | 멤버 초대 | `email`: 이메일 형식, `role`: ORG_ROLES 열거 (기본 `"member"`) |
| `memberRoleUpdateSchema` | 멤버 역할 변경 | `role`: `"admin"` 또는 `"member"` |
| `projectAssignmentSchema` | 프로젝트 할당 | `projectIds`: 문자열 배열 (기본 빈 배열) |

#### 관리자 스키마

`adminOrgActionSchema`는 **discriminated union** 패턴을 사용하며, `action` 필드에 따라 16가지 동작을 지원한다:

| action 값 | 추가 필드 | 설명 |
|---|---|---|
| `"suspend"` | `reason`: 1~1024자 | 조직 정지 |
| `"ban"` | `reason`: 1~1024자 | 조직 차단 |
| `"reactivate"` | `reason`: 1024자 이하 (선택) | 조직 재활성화 |
| `"override_plan"` | `planId`: PlanId 또는 null | 플랜 수동 오버라이드 |
| `"override_limits"` | `projectLimit`/`memberLimit`/`memoryLimitPerProject`/`apiRatePerMinute`: 정수 (각 선택) | 제한 수동 오버라이드 |
| `"reset_limits"` | -- | 커스텀 제한 초기화 |
| `"transfer_ownership"` | `newOwnerId`: 문자열 | 소유권 이전 |
| `"update_notes"` | `notes`: 4096자 이하 | 관리자 메모 업데이트 |
| `"start_trial"` | `durationDays`: 1~365 정수 | 트라이얼 시작 |
| `"end_trial"` | -- | 트라이얼 종료 |
| `"set_expiry"` | `expiresAt`: 정수 타임스탬프 | 플랜 만료일 설정 |
| `"clear_expiry"` | -- | 플랜 만료일 제거 |
| `"create_subscription"` | `priceInCents`: 100 이상 정수, `interval`: `"month"`/`"year"` (기본 `"month"`) | Stripe 구독 생성 |
| `"cancel_subscription"` | -- | 구독 취소 |
| `"update_contract"` | `contractValue`/`contractNotes`/`contractStartDate`/`contractEndDate`: 각 선택/nullable | 계약 정보 업데이트 |
| `"apply_template"` | `templateId`: 문자열, `createSubscription`: boolean (선택), `subscriptionInterval`: `"month"`/`"year"` (선택) | 플랜 템플릿 적용 |

#### 플랜 템플릿 스키마

| 스키마 | 설명 | 주요 규칙 |
|---|---|---|
| `planTemplateCreateSchema` | 플랜 템플릿 생성 | `name`: 1~128자, `description`: 512자 이하 (선택), `basePlanId`: PlanId (기본 `"enterprise"`), `projectLimit`/`memberLimit`/`memoryLimitPerProject`/`apiRatePerMinute`: 각 1 이상 정수, `stripePriceInCents`: 100 이상 정수 또는 null (선택) |
| `planTemplateUpdateSchema` | 플랜 템플릿 수정 | 위 모든 필드가 선택적 (`partial()`) |

### 7.3 검색 의도 분류 (`packages/shared/src/intent.ts`)

검색 쿼리를 5가지 의도(intent)로 분류하여 가중치 기반 랭킹에 활용한다:

| 의도 | 설명 | ftsBoost | vectorBoost | recencyBoost | priorityBoost | graphBoost |
|---|---|---|---|---|---|---|
| `entity` | 특정 엔티티(파일, 식별자) 검색 | 2.0 | 0.5 | 0.3 | 1.0 | 0 |
| `temporal` | 최근 변경 항목 검색 | 0.7 | 0.5 | 3.0 | 0.5 | 0 |
| `relationship` | 관련 항목 탐색 | 0.5 | 1.5 | 1.0 | 1.0 | 2.0 |
| `aspect` | 규칙/패턴/관례 검색 | 1.0 | 1.5 | 0.5 | 1.5 | 0 |
| `exploratory` | 탐색적 검색 (기본 폴백) | 1.0 | 1.2 | 1.0 | 1.0 | 0 |

분류 우선순위: 경로 패턴/식별자(`entity`) > 시간 관련 키워드(`temporal`) > 관계 키워드(`relationship`) > 규칙/패턴 키워드(`aspect`) > 기본(`exploratory`).

### 7.4 공유 TypeScript 인터페이스 (`packages/shared/src/types.ts`)

`types.ts`는 스키마의 각 테이블에 대응하는 TypeScript 인터페이스를 정의한다. 주요 특징:

- 타임스탬프 필드는 `number` 타입 (Unix epoch)으로 표현된다.
- nullable 필드는 `| null` 유니온으로 명시된다.
- JSON 직렬화 필드(`metadata`, `tags` 등)는 `string | null`로 표현되며, 파싱은 사용처에서 수행한다.
- `JwtPayload` 인터페이스가 포함되어 있어 JWT 기반 인증 토큰의 구조를 정의한다: `userId`, `orgId`, `sessionId`, `jti`, `iat`, `exp`.
- `WebhookConfig` 인터페이스가 types.ts에 정의되어 있으나, 해당 테이블은 schema.ts에 존재하지 않는다 (미구현 또는 별도 저장소).

정의된 인터페이스 목록: `User`, `Organization`, `OrganizationMember`, `Project`, `Memory`, `MemorySnapshot`, `SessionLog`, `MemoryVersion`, `ActivityLog`, `ContextType`, `MemoryLock`, `ProjectTemplate`, `WebhookConfig`, `ApiToken`, `OnboardingResponse`, `OrgMemoryDefault`, `JwtPayload`.
