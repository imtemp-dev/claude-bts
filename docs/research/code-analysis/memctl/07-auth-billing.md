# memctl -- 인증 & 과금 분석

## 목차

1. [인증 시스템](#1-인증-시스템)
2. [API 인증 3경로](#2-api-인증-3경로)
3. [미들웨어 유틸리티 함수](#3-미들웨어-유틸리티-함수)
4. [RBAC](#4-rbac)
5. [Rate Limiting](#5-rate-limiting)
6. [플랜 시스템](#6-플랜-시스템)
7. [Stripe 연동](#7-stripe-연동)
8. [좌석 과금](#8-좌석-과금)
9. [프로모 코드](#9-프로모-코드)
10. [베타 게이트](#10-베타-게이트)

---

## 1. 인증 시스템

> 소스: `apps/web/lib/auth.ts`

memctl의 인증은 [Better Auth](https://github.com/better-auth/better-auth) 라이브러리를 기반으로 하며, GitHub OAuth와 Magic Link 두 가지 인증 수단을 제공한다. 개발/Self-hosted 환경에서는 인증을 우회할 수 있는 Dev Bypass 모드를 지원한다.

### Better Auth 구성

> 소스: `apps/web/lib/auth.ts:223-367`

```typescript
return betterAuth({
  baseURL,
  trustedOrigins,
  user: { fields: { image: "avatarUrl" } },
  database: drizzleAdapter(getDb(), {
    provider: "sqlite",
    usePlural: true,
    schema: { users, sessions, accounts, verifications },
  }),
  socialProviders: {
    github: {
      clientId: githubClientId,
      clientSecret: githubClientSecret,
      scope: ["user:email"],
    },
  },
  session: {
    cookieCache: { enabled: true, maxAge: 5 * 60 },  // 5분 쿠키 캐시
  },
  plugins: [magicLink({ expiresIn: 300, sendMagicLink: ... })],
  databaseHooks: { user: { create: { after: ... } } },
});
```

### 주요 구성 요소

| 구성 요소 | 설명 | 소스 위치 |
|-----------|------|-----------|
| Database adapter | Drizzle ORM + SQLite | `auth.ts:254-263` |
| Social provider | GitHub OAuth (`user:email` scope) | `auth.ts:233-244` |
| Magic Link | 5분 만료, Resend API로 이메일 발송 | `auth.ts:272-296` |
| Session cookie | 5분 캐시 활성화 | `auth.ts:265-269` |
| User image field | `avatarUrl` (Better Auth 기본 `image` 대신) | `auth.ts:250-252` |

### GitHub OAuth

> 소스: `apps/web/lib/auth.ts:233-244`

```typescript
const githubClientId = process.env.GITHUB_CLIENT_ID?.trim();
const githubClientSecret = process.env.GITHUB_CLIENT_SECRET?.trim();
const socialProviders =
  githubClientId && githubClientSecret
    ? { github: { clientId, clientSecret, scope: ["user:email"] } }
    : {};
```

`GITHUB_CLIENT_ID`와 `GITHUB_CLIENT_SECRET` 환경 변수가 모두 설정된 경우에만 GitHub OAuth가 활성화된다. 둘 중 하나라도 없으면 `socialProviders`는 빈 객체가 되어 GitHub 로그인이 비활성화된다.

### Magic Link

> 소스: `apps/web/lib/auth.ts:272-296`

| 설정 | 값 | 설명 |
|------|------|------|
| `expiresIn` | 300 (초) | 링크 유효 시간 5분 |
| 이메일 발송 | `sendEmail()` via Resend API | 프로덕션 환경 |
| 개발 환경 | 콘솔 출력 | `RESEND_API_KEY` 미설정 시 |
| 이메일 검증 | `isValidAdminEmail(email)` | 유효하지 않은 이메일 차단 |

개발 환경에서 `RESEND_API_KEY`가 없으면 magic link URL을 콘솔에 직접 출력한다:

```
[DEV MAGIC LINK] ------------------------------
   Email: admin@example.com
   URL:   https://localhost:3000/api/auth/magic-link/verify?token=xxx
-----------------------------------------------
```

### Database Hooks -- 사용자 생성 후 처리

> 소스: `apps/web/lib/auth.ts:298-365`

사용자가 최초 생성될 때 다음 작업이 자동으로 수행된다:

```
user.create.after(user)
    |
    +-- @memctl.com 이메일? -> isAdmin = true 자동 승격
    |
    +-- 미수락 조직 초대 검색 (미만료, 미수락)
    |       |
    |       +-- for each invite:
    |               |
    |               +-- ensureSeatForAdditionalMember(invite.orgId)
    |               |       좌석 확보 실패 시 해당 초대 건너뜀
    |               |
    |               +-- organizationMembers INSERT
    |               +-- orgInvitations.acceptedAt = now
    |               |
    |               +-- 실패 시 syncSeatQuantityToMemberCount()
    |                       좌석 수 보정
    |
    +-- sendEmail(WelcomeEmail) [fire-and-forget]
```

### Dev Auth Bypass

> 소스: `apps/web/lib/auth.ts:37-201`

개발 환경 또는 self-hosted 모드에서 인증을 우회할 수 있는 메커니즘이다.

#### 활성화 조건

```typescript
function isDevAuthBypassEnabled() {
  const bypassRequested =
    process.env.DEV_AUTH_BYPASS === "true" ||
    process.env.NEXT_PUBLIC_DEV_AUTH_BYPASS === "true";
  return bypassRequested &&
    (process.env.NODE_ENV === "development" || isSelfHosted());
}
```

두 조건 모두 충족되어야 한다:

1. `DEV_AUTH_BYPASS` 또는 `NEXT_PUBLIC_DEV_AUTH_BYPASS`가 `"true"`
2. `NODE_ENV === "development"` 또는 `SELF_HOSTED === "true"`

#### Bypass 구성 환경 변수

| 환경 변수 | 기본값 | 설명 |
|-----------|--------|------|
| `DEV_AUTH_BYPASS_USER_ID` | `"dev-auth-user"` | 바이패스 사용자 ID |
| `DEV_AUTH_BYPASS_USER_NAME` | `"Dev User"` | 사용자 표시명 |
| `DEV_AUTH_BYPASS_USER_EMAIL` | `"dev@local.memctl.test"` | 사용자 이메일 |
| `DEV_AUTH_BYPASS_ORG_ID` | `"dev-auth-org"` | 조직 ID |
| `DEV_AUTH_BYPASS_ORG_NAME` | `"Dev Organization"` | 조직 이름 |
| `DEV_AUTH_BYPASS_ORG_SLUG` | `"dev-org"` | 조직 URL slug |
| `DEV_AUTH_BYPASS_ADMIN` | `"false"` | 관리자 권한 여부 |

#### ensureDevBypassSession() 동작

> 소스: `apps/web/lib/auth.ts:65-201`

```
ensureDevBypassSession()
    |
    +-- isDevAuthBypassEnabled()? -> false면 return null
    |
    +-- DB에서 user 조회 (ID 기준)
    |       없으면 INSERT (실패 시 email로 재조회)
    |
    +-- isAdmin 상태 불일치? -> UPDATE
    |
    +-- DB에서 org 조회 (slug 기준)
    |       없으면 INSERT (getOrgCreationLimits()로 초기 제한값)
    |
    +-- planId/projectLimit/memberLimit 변경? -> UPDATE
    |       (DEV_PLAN 환경 변수 변경 반영)
    |
    +-- organizationMembers에서 멤버십 조회
    |       없으면 INSERT (role: "owner")
    |
    +-- 세션 객체 반환:
          {
            user: { id, name, email, image, ... },
            session: {
              id: "dev-auth-bypass-session",
              token: "dev-auth-bypass-token",
              expiresAt: now + 24h,
              ...
            }
          }
```

#### Proxy 패턴을 통한 투명한 주입

> 소스: `apps/web/lib/auth.ts:369-388`

```typescript
export const auth = new Proxy({} as ReturnType<typeof betterAuth>, {
  get(_, prop) {
    const authInstance = getAuthInstance();
    if (prop === "api") {
      _apiProxy = new Proxy(authInstance.api, {
        get(apiTarget, apiProp) {
          if (apiProp === "getSession") {
            return getSessionWithDevBypass;  // Dev bypass 주입
          }
          return Reflect.get(apiTarget, apiProp);
        },
      });
      return _apiProxy;
    }
    return authInstance[prop];
  },
});
```

`auth.api.getSession()` 호출을 가로채어:
1. 먼저 실제 Better Auth 세션을 확인
2. 세션이 없고 bypass가 활성화되어 있으면 `ensureDevBypassSession()` 호출
3. bypass 세션을 최초 한 번만 콘솔에 로깅 (`_devBypassLogged` 플래그)

---

## 2. API 인증 3경로

> 소스: `apps/web/lib/api-middleware.ts:51-208`

모든 API 요청은 `authenticateRequest()` 함수를 통해 인증된다. Authorization 헤더의 형식에 따라 세 가지 경로로 분기된다.

### 인증 흐름도

```
authenticateRequest(req)
    |
    +-- Authorization: Bearer xxx ?
    |       |
    |       +-- YES -> JWT 검증 시도
    |       |       |
    |       |       +-- 유효한 JWT? -> [경로 1: JWT 인증]
    |       |       |
    |       |       +-- JWT 실패 -> API Token 검증 시도
    |       |               |
    |       |               +-- 유효한 API token? -> [경로 2: API Token 인증]
    |       |               |
    |       |               +-- 실패 -> 401 "Invalid token"
    |       |
    |       +-- NO -> Cookie 세션 확인
    |               |
    |               +-- 유효한 세션? -> [경로 3: Cookie 인증]
    |               |
    |               +-- 실패 -> 401 "Missing authorization"
    |
    +-- return AuthContext { userId, orgId, sessionId }
```

### 경로 1: JWT 인증

> 소스: `apps/web/lib/api-middleware.ts:57-102`, `apps/web/lib/jwt.ts`

CLI 및 API 클라이언트가 사용하는 주 인증 방식이다.

#### JWT 구조

| 필드 | 타입 | 설명 |
|------|------|------|
| `userId` | `string` | 사용자 고유 ID |
| `orgId` | `string` | 조직 고유 ID |
| `sessionId` | `string` | Better Auth 세션 ID |
| `jti` | `string` | JWT ID (`crypto.randomUUID()`) |
| `iat` | `number` | 발급 시각 |
| `exp` | `number` | 만료 시각 (발급 후 1시간) |

#### JWT 생성

> 소스: `apps/web/lib/jwt.ts:19-30`

```typescript
export async function createJwt(payload: {
  userId: string;
  orgId: string;
  sessionId: string;
}) {
  const jti = crypto.randomUUID();
  return new SignJWT({ ...payload, jti })
    .setProtectedHeader({ alg: "HS256" })
    .setIssuedAt()
    .setExpirationTime("1h")
    .sign(secret);
}
```

| 속성 | 값 |
|------|------|
| 알고리즘 | HS256 (HMAC-SHA256) |
| 만료 시간 | 1시간 |
| 비밀 키 | `BETTER_AUTH_SECRET` 환경 변수 (기본값: `"development-secret-change-me"`) |
| 라이브러리 | `jose` (JavaScript Object Signing and Encryption) |

#### LRU 세션 캐시

> 소스: `apps/web/lib/jwt.ts:14-17`

```typescript
const sessionCache = new LRUCache<string, CachedSession>({
  max: 1000,
  ttl: 5 * 60 * 1000,  // 5분
});
```

| 설정 | 값 | 설명 |
|------|------|------|
| `max` | 1,000 | 최대 캐시 항목 수 |
| `ttl` | 300,000ms (5분) | 항목 생존 시간 |
| Key | JWT의 `jti` | JWT ID를 캐시 키로 사용 |
| Value | `{ valid, userId, orgId }` | 세션 유효성과 기본 정보 |

JWT 검증 흐름:

```
JWT 검증
    |
    +-- verifyJwt(token) -> payload
    |
    +-- getCachedSession(payload.jti)
    |       |
    |       +-- HIT: cached.valid === false? -> 401 "Session revoked"
    |       +-- HIT: cached.valid === true? -> AuthContext 반환
    |       +-- MISS: DB에서 session 조회
    |               |
    |               +-- 세션 없음 또는 만료? -> 캐시(valid:false), 401
    |               +-- 유효? -> 캐시(valid:true), AuthContext 반환
```

### 경로 2: API Token 인증

> 소스: `apps/web/lib/api-middleware.ts:104-163`

JWT 검증에 실패한 Bearer token은 API Token으로 처리된다. `mctl_` prefix는 토큰 생성 시 사용되는 명명 규칙이지만, 코드에서 이 prefix를 검증하지는 않는다. JWT 검증에 실패한 모든 Bearer token은 그대로 해시되어 API Token으로 조회된다.

#### 토큰 해싱

> 소스: `apps/web/lib/api-middleware.ts:42-49`

```typescript
async function hashApiToken(token: string): Promise<string> {
  const encoder = new TextEncoder();
  const data = encoder.encode(token);
  const hash = await crypto.subtle.digest("SHA-256", data);
  return Array.from(new Uint8Array(hash))
    .map((b) => b.toString(16).padStart(2, "0"))
    .join("");
}
```

원본 토큰은 DB에 저장되지 않는다. SHA-256 해시만 저장하고 비교한다.

#### API Token LRU 캐시

> 소스: `apps/web/lib/api-middleware.ts:37-40`

```typescript
const apiTokenCache = new LRUCache<string, CachedApiTokenAuth>({
  max: 1000,
  ttl: 60_000,  // 1분
});
```

| 설정 | 값 | 설명 |
|------|------|------|
| `max` | 1,000 | 최대 캐시 항목 수 |
| `ttl` | 60,000ms (1분) | JWT 캐시보다 짧은 TTL |
| Key | 토큰의 SHA-256 해시 | |
| Value | `{ valid, userId, orgId, tokenId, expiresAt }` 또는 `{ valid: false }` | |

API Token 검증 흐름:

```
API Token 검증
    |
    +-- hashApiToken(token) -> tokenHash
    |
    +-- apiTokenCache.get(tokenHash)
    |       |
    |       +-- HIT: valid === false? -> 401
    |       +-- HIT: valid === true?
    |       |       |
    |       |       +-- expiresAt < now? -> cache.delete, 401
    |       |       +-- 유효? -> AuthContext 반환
    |       |               sessionId: "api-token:{tokenId}"
    |       |
    |       +-- MISS: DB에서 apiTokens 조회 (tokenHash로)
    |               |
    |               +-- 없음/revoked/만료? -> 캐시(valid:false), 401
    |               +-- 유효? -> 캐시(valid:true)
    |                       +-- lastUsedAt 비동기 업데이트 (void, fire-and-forget)
    |                       +-- AuthContext 반환
```

`lastUsedAt` 업데이트는 `void db.update(...).catch(() => null)`로 fire-and-forget 방식으로 처리한다. 인증 응답 지연을 방지하기 위함이다.

### 경로 3: Cookie Session 인증

> 소스: `apps/web/lib/api-middleware.ts:166-208`

웹 대시보드에서 사용하는 인증 방식이다. `Authorization` 헤더가 없을 때 이 경로로 진입한다.

```
Cookie 인증
    |
    +-- auth.api.getSession({ headers: req.headers })
    |       Better Auth가 쿠키에서 세션 토큰 추출 및 검증
    |
    +-- 세션 없음? -> 401 "Missing authorization"
    |
    +-- X-Org-Slug 헤더 확인
    |       |
    |       +-- 없음? -> 400 "Missing X-Org-Slug header"
    |       +-- 있음? -> DB에서 organizations 조회
    |               |
    |               +-- 없음? -> 404 "Organization not found"
    |               +-- status === "suspended"? -> 403
    |               +-- status === "banned"? -> 403
    |               +-- 정상? -> AuthContext 반환
```

Cookie 인증의 경우 `orgId`는 `X-Org-Slug` 헤더로 전달되는 조직 slug에서 DB를 조회하여 결정한다. JWT/API Token은 토큰 자체에 `orgId`가 포함되어 있으므로 이 헤더가 불필요하다.

### 세 경로 비교

| 속성 | JWT | API Token | Cookie |
|------|-----|-----------|--------|
| 주 사용처 | CLI (mcp 서버) | 외부 API 통합 | 웹 대시보드 |
| 인증 헤더 | `Bearer {jwt}` | `Bearer {token}` (관례적으로 `mctl_` prefix) | 없음 (쿠키) |
| 만료 시간 | 1시간 | 설정 가능 (또는 무기한) | Better Auth 세션 TTL |
| orgId 출처 | JWT payload | DB (apiTokens 테이블) | `X-Org-Slug` 헤더 -> DB |
| 캐시 TTL | 5분 (LRU) | 1분 (LRU) | Better Auth cookieCache (5분) |
| sessionId 형식 | `payload.sessionId` | `"api-token:{tokenId}"` | `session.session.id` |
| 조직 상태 확인 | 없음 | 없음 | `suspended`/`banned` 확인 |

---

## 3. 미들웨어 유틸리티 함수

> 소스: `apps/web/lib/api-middleware.ts`

API 요청 처리에 사용되는 인증/인가 유틸리티 함수들이다. 이들은 자동으로 연결되는 미들웨어 체인이 아니라, 각 라우트 핸들러가 필요에 따라 개별적으로 호출하는 독립적인 함수들이다. 단, `withApiMiddleware`는 내부적으로 `authenticateRequest`를 호출한다.

### 유틸리티 함수 구조

```
클라이언트 요청
    |
    +-- [1] withApiMiddleware(handler)        ← 라우트 핸들러를 래핑
    |       apps/web/lib/api-middleware.ts:288-316
    |       - X-Request-Id 생성 및 헤더 설정
    |       - 요청/응답 로깅 (method, path, status, duration)
    |       - try/catch로 500 에러 래핑
    |       - 내부적으로 authenticateRequest() 호출
    |
    |   +-- [1-1] authenticateRequest(req)    ← withApiMiddleware 내부에서 호출
    |       |     apps/web/lib/api-middleware.ts:51-208
    |       |     - JWT / API Token / Cookie 인증
    |       |     - AuthContext { userId, orgId, sessionId } 반환
    |       |     - 실패 시 401 또는 400 응답
    |
    +-- 이하 함수들은 라우트 핸들러가 필요에 따라 개별 호출 --
    |
    +-- requireOrgMembership(userId, orgId)
    |       apps/web/lib/api-middleware.ts:210-226
    |       - organizationMembers 테이블에서 멤버십 확인
    |       - 역할(role) 반환: "owner" | "admin" | "member" | null
    |       - null이면 403 응답
    |
    +-- checkProjectAccess(userId, projectId, orgRole)
    |       apps/web/lib/api-middleware.ts:228-247
    |       - owner/admin: 무조건 접근 허용
    |       - member: projectMembers 테이블에서 할당 확인
    |
    +-- checkRateLimit(authContext)
    |       apps/web/lib/api-middleware.ts:322-355
    |       - 조직의 planId에서 apiRatePerMinute 조회
    |       - rateLimit(userId, limit) 호출
    |       - 초과 시 429 + Retry-After 헤더
    |
    +-- 실제 라우트 핸들러 로직 실행
    |
    +-- 응답 반환 (X-Request-Id 포함)
```

### withApiMiddleware()

> 소스: `apps/web/lib/api-middleware.ts:288-316`

```typescript
export function withApiMiddleware(
  handler: (req: NextRequest, ctx?: unknown) => Promise<NextResponse>,
) {
  return async (req: NextRequest, ctx?: unknown): Promise<NextResponse> => {
    const requestId = generateRequestId();
    const start = Date.now();
    const method = req.method;
    const path = new URL(req.url).pathname;

    try {
      const res = await handler(req, ctx);
      const duration = Date.now() - start;
      logger.info({ requestId, method, path, status: res.status, duration });
      res.headers.set("X-Request-Id", requestId);
      return res;
    } catch (err) {
      const duration = Date.now() - start;
      logger.error({ requestId, method, path, duration, error: String(err) });
      const res = NextResponse.json(
        { error: "Internal server error" },
        { status: 500 },
      );
      res.headers.set("X-Request-Id", requestId);
      return res;
    }
  };
}
```

| 기능 | 설명 |
|------|------|
| Request ID | `generateRequestId()`로 고유 ID 생성, `X-Request-Id` 헤더에 포함 |
| 로깅 | 성공: `logger.info`, 실패: `logger.error` |
| 에러 래핑 | 미처리 예외를 `500 Internal server error`로 변환 |
| 소요 시간 | `Date.now()` 차이로 밀리초 단위 측정 |

### checkRateLimit()

> 소스: `apps/web/lib/api-middleware.ts:322-355`

```typescript
export async function checkRateLimit(
  authContext: AuthContext,
): Promise<NextResponse | null> {
  const [org] = await db
    .select({ planId, planOverride, projectLimit, memberLimit,
              memoryLimitPerProject, apiRatePerMinute })
    .from(organizations)
    .where(eq(organizations.id, authContext.orgId))
    .limit(1);

  const limits = getOrgLimits(org);
  const result = rateLimit(authContext.userId, limits.apiRatePerMinute);

  if (!result.allowed) {
    const res = NextResponse.json({ error: "Rate limit exceeded" }, { status: 429 });
    res.headers.set("Retry-After", String(result.retryAfterSeconds));
    res.headers.set("X-RateLimit-Limit", String(result.limit));
    res.headers.set("X-RateLimit-Remaining", "0");
    return res;
  }
  return null;  // null = 허용
}
```

429 응답 시 포함되는 헤더:

| 헤더 | 설명 |
|------|------|
| `Retry-After` | 재시도까지 대기 시간 (초) |
| `X-RateLimit-Limit` | 분당 최대 요청 수 |
| `X-RateLimit-Remaining` | 남은 요청 수 (초과 시 `"0"`) |

---

## 4. RBAC

> 소스: `apps/web/lib/api-middleware.ts:210-277`

memctl은 조직 수준의 역할 기반 접근 제어(Role-Based Access Control)를 구현한다.

### 역할 정의

> 소스: `packages/shared/src/constants.ts:88-89`

```typescript
export const ORG_ROLES = ["owner", "admin", "member"] as const;
export type OrgRole = (typeof ORG_ROLES)[number];
```

### 역할별 권한

| 권한 | owner | admin | member |
|------|-------|-------|--------|
| 모든 프로젝트 접근 | O | O | X (할당된 프로젝트만) |
| 조직 설정 변경 | O | O | X |
| 멤버 초대/관리 | O | O | X |
| 프로젝트 생성 | O | O | X |
| 과금 관리 | O | X | X |

### requireOrgMembership()

> 소스: `apps/web/lib/api-middleware.ts:210-226`

```typescript
export async function requireOrgMembership(
  userId: string,
  orgId: string,
): Promise<string | null> {
  const [member] = await db
    .select()
    .from(organizationMembers)
    .where(
      and(
        eq(organizationMembers.orgId, orgId),
        eq(organizationMembers.userId, userId),
      ),
    )
    .limit(1);
  return member?.role ?? null;
}
```

`null` 반환 시 해당 사용자는 조직의 멤버가 아닌 것이다. 호출측에서 403 응답을 반환해야 한다.

### checkProjectAccess()

> 소스: `apps/web/lib/api-middleware.ts:228-247`

```typescript
export async function checkProjectAccess(
  userId: string,
  projectId: string,
  orgRole: string,
): Promise<boolean> {
  if (orgRole === "owner" || orgRole === "admin") return true;
  const [assignment] = await db
    .select()
    .from(projectMembers)
    .where(
      and(
        eq(projectMembers.projectId, projectId),
        eq(projectMembers.userId, userId),
      ),
    )
    .limit(1);
  return !!assignment;
}
```

접근 제어 흐름:

```
checkProjectAccess(userId, projectId, orgRole)
    |
    +-- orgRole === "owner" 또는 "admin"? -> true (모든 프로젝트 접근)
    |
    +-- orgRole === "member"?
            |
            +-- projectMembers 테이블에서 (projectId, userId) 검색
            +-- 할당 존재? -> true
            +-- 할당 없음? -> false
```

### getAccessibleProjectIds()

> 소스: `apps/web/lib/api-middleware.ts:249-277`

```typescript
export async function getAccessibleProjectIds(
  userId: string,
  orgId: string,
  orgRole: string,
): Promise<string[] | null> {
  if (orgRole === "owner" || orgRole === "admin") return null;  // null = 모든 프로젝트
  // member인 경우: 할당된 프로젝트 ID 목록 반환
  ...
}
```

| 반환 값 | 의미 |
|---------|------|
| `null` | owner/admin -- 모든 프로젝트에 접근 가능 |
| `string[]` | member -- 할당된 프로젝트 ID 목록만 접근 가능 |
| `[]` (빈 배열) | member이지만 할당된 프로젝트 없음 |

---

## 5. Rate Limiting

> 소스: `apps/web/lib/rate-limit.ts`

### Fixed Window 구현

memctl의 rate limiter는 단순한 고정 윈도우(fixed window) 방식으로 구현되어 있다. 분당 요청 수를 추적하며, 윈도우가 만료되면 카운트가 초기화된다.

```typescript
const cache = new Map<string, RateLimitEntry>();

interface RateLimitEntry {
  count: number;
  resetAt: number;  // Date.now() + 60_000
}
```

### rateLimit() 함수

> 소스: `apps/web/lib/rate-limit.ts:25-57`

```
rateLimit(identifier, apiRatePerMinute)
    |
    +-- limit >= UNLIMITED_SENTINEL (999999)?
    |       -> { allowed: true, remaining: limit }
    |
    +-- cache.get(identifier)
    |       |
    |       +-- MISS 또는 만료 (now >= resetAt)?
    |       |       -> cache.set({ count: 1, resetAt: now + 60s })
    |       |       -> { allowed: true, remaining: limit - 1 }
    |       |
    |       +-- HIT (윈도우 내)?
    |               -> entry.count++
    |               |
    |               +-- count > limit?
    |               |       -> { allowed: false, retryAfterSeconds }
    |               |
    |               +-- count <= limit?
    |                       -> { allowed: true, remaining: limit - count }
```

### RateLimitResult 인터페이스

```typescript
export interface RateLimitResult {
  allowed: boolean;
  limit: number;
  remaining: number;
  retryAfterSeconds: number;
}
```

### 캐시 정리

> 소스: `apps/web/lib/rate-limit.ts:11-16`

```typescript
setInterval(() => {
  const now = Date.now();
  for (const [key, entry] of cache) {
    if (now >= entry.resetAt) cache.delete(key);
  }
}, 5 * 60_000);
```

5분마다 만료된 엔트리를 정리한다. 이는 메모리 누수를 방지하기 위한 것으로, 활발하지 않은 사용자의 엔트리가 무한히 쌓이는 것을 막는다.

### Per-User, Per-Plan 제한

rate limit의 identifier는 `authContext.userId`이다. 조직 단위가 아닌 사용자 단위로 제한한다. 제한값은 조직의 플랜에서 결정된다.

| 플랜 | apiRatePerMinute |
|------|-----------------|
| free | 60 |
| lite | 100 |
| pro | 150 |
| business | 150 |
| scale | 150 |
| enterprise | 150 |

### UNLIMITED_SENTINEL

> 소스: `apps/web/lib/plans.ts:4`

```typescript
export const UNLIMITED_SENTINEL = 999999;
```

SQLite는 `Infinity`를 정수 컬럼에 저장할 수 없으므로, 999,999를 "무제한" 센티넬 값으로 사용한다. rate limiter에서 이 값 이상이면 항상 허용한다.

---

## 6. 플랜 시스템

> 소스: `apps/web/lib/plans.ts`, `packages/shared/src/constants.ts`

### 6개 플랜 티어

> 소스: `packages/shared/src/constants.ts:1-80`

```typescript
export const PLAN_IDS = [
  "free", "lite", "pro", "business", "scale", "enterprise",
] as const;
```

| 플랜 | 월 가격($) | 프로젝트 | 멤버 | Memory/프로젝트 | API Rate/분 |
|------|-----------|---------|------|----------------|-------------|
| free | 0 | 3 | 1 | 400 | 60 |
| lite | 5 | 10 | 3 | 1,200 | 100 |
| pro | 18 | 25 | 10 | 5,000 | 150 |
| business | 59 | 100 | 30 | 10,000 | 150 |
| scale | 149 | 150 | 100 | 25,000 | 150 |
| enterprise | -1 (별도) | Infinity | Infinity | Infinity | 150 |

Enterprise 플랜의 가격은 `-1`로 표시되며, 이는 별도 계약을 의미한다.

### Effective Plan ID 결정

> 소스: `apps/web/lib/plans.ts:96-131`

`getEffectivePlanId()` 함수는 여러 조건을 고려하여 조직의 실제 적용 플랜을 결정한다:

```
getEffectivePlanId(org)
    |
    +-- org.trialEndsAt <= now (트라이얼 만료)?
    |       |
    |       +-- org.planId가 유효한 유료 플랜? -> org.planId 반환
    |       +-- 아니면 -> "free" 반환
    |
    +-- org.planExpiresAt <= now (플랜 만료)?
    |       -> "free" 반환
    |
    +-- org.planOverride가 유효한 PLAN_ID?
    |       -> planOverride 반환
    |
    +-- org.planId가 유효한 PLAN_ID?
    |       -> planId 반환
    |
    +-- 기본값 -> "free" 반환
```

### 우선순위 체계

| 우선순위 | 조건 | 결과 |
|---------|------|------|
| 1 (최고) | 트라이얼 만료 + 유료 Stripe 구독 활성 | Stripe 구독 플랜 |
| 2 | 트라이얼 만료 + 구독 없음 | `free` |
| 3 | 플랜 만료 (`planExpiresAt <= now`) | `free` |
| 4 | `planOverride` 존재 | `planOverride` |
| 5 | `planId` 유효 | `planId` |
| 6 (최저) | 기본값 | `free` |

`planOverride`는 관리자가 수동으로 설정하는 값으로, Stripe 구독 상태와 무관하게 플랜을 강제 지정할 때 사용한다.

### 조직 제한값 (getOrgLimits)

> 소스: `apps/web/lib/plans.ts:58-75`

```typescript
export function getOrgLimits(org): OrgLimits {
  const planId = getEffectivePlanId(org);
  const plan = PLANS[planId] ?? PLANS.free;
  return {
    projectLimit: org.projectLimit,           // DB 값 사용
    memberLimit: org.memberLimit,              // DB 값 사용
    memoryLimitPerProject:
      org.memoryLimitPerProject ?? clampLimit(plan.memoryLimitPerProject),
    apiRatePerMinute:
      org.apiRatePerMinute ?? clampLimit(plan.apiRatePerMinute),
  };
}
```

`projectLimit`과 `memberLimit`은 DB에 저장된 값을 그대로 사용한다 (좌석 과금으로 인해 플랜 기본값과 다를 수 있으므로). `memoryLimitPerProject`와 `apiRatePerMinute`은 DB 값이 `null`이면 플랜 기본값을 사용한다.

### 조직 생성 시 초기 제한값

> 소스: `apps/web/lib/plans.ts:31-49`

```typescript
export function getOrgCreationLimits(planId?: PlanId) {
  const resolvedPlan = planId ?? getDefaultPlanId();
  const plan = PLANS[resolvedPlan] ?? PLANS.free;
  return {
    planId: resolvedPlan,
    projectLimit: clampLimit(plan.projectLimit),
    memberLimit: clampLimit(plan.memberLimit),
    memoryLimitPerProject: null,   // 플랜 기본값 사용
    apiRatePerMinute: null,        // 플랜 기본값 사용
    customLimits: false,
  };
}
```

### Self-Hosted 모드

> 소스: `apps/web/lib/plans.ts:6-8`

```typescript
export function isSelfHosted(): boolean {
  return process.env.SELF_HOSTED === "true";
}
```

Self-hosted 모드의 특징:

| 속성 | 값 | 설명 |
|------|------|------|
| 기본 플랜 | `enterprise` | 모든 제한 무제한 |
| 과금 | 비활성 | Stripe 연동 불가 |
| Dev Auth Bypass | 활성화 가능 | `DEV_AUTH_BYPASS=true` |
| 좌석 과금 | 비활성 | 멤버 제한만 적용 |

### 트라이얼 및 만료

> 소스: `apps/web/lib/plans.ts:133-156`

```typescript
export function isActiveTrial(org): boolean {
  if (!org.trialEndsAt) return false;
  return org.trialEndsAt > new Date();
}

export function daysUntilExpiry(org): number | null {
  const target = org.trialEndsAt ?? org.planExpiresAt;
  if (!target) return null;
  const diffMs = target.getTime() - Date.now();
  if (diffMs <= 0) return 0;
  return Math.ceil(diffMs / (1000 * 60 * 60 * 24));
}

export function planExpiresWithinDays(org, days): boolean {
  const remaining = daysUntilExpiry(org);
  if (remaining === null) return false;
  return remaining <= days && remaining > 0;
}
```

| 함수 | 용도 |
|------|------|
| `isActiveTrial()` | 트라이얼이 현재 활성인지 확인 |
| `daysUntilExpiry()` | 만료까지 남은 일수 (trialEndsAt 우선, 없으면 planExpiresAt) |
| `planExpiresWithinDays()` | N일 이내 만료 여부 (알림 표시용) |

### 상수

| 상수 | 값 | 소스 | 설명 |
|------|------|------|------|
| `FREE_ORG_LIMIT_PER_USER` | 3 | `plans.ts:88` | 사용자당 최대 무료 조직 수 |
| `INVITATIONS_PER_DAY` | 20 | `plans.ts:91` | 조직당 일일 초대 한도 |
| `MAX_PENDING_INVITATIONS` | 50 | `plans.ts:94` | 조직당 최대 미수락 초대 수 |
| `EXTRA_SEAT_PRICE` | $8/월 | `constants.ts:83` | 추가 좌석 월 가격 |

---

## 7. Stripe 연동

> 소스: `apps/web/lib/stripe.ts`, `apps/web/app/api/stripe/webhook/route.ts`

### Stripe 클라이언트

> 소스: `apps/web/lib/stripe.ts:1-19`

```typescript
let _stripe: Stripe | null = null;

export function getStripe(): Stripe {
  if (!_stripe) {
    _stripe = new Stripe(process.env.STRIPE_SECRET_KEY!, {
      apiVersion: "2025-02-24.acacia",
    });
  }
  return _stripe;
}

export const stripe = new Proxy({} as Stripe, {
  get(_, prop) {
    return (getStripe() as unknown as Record<string | symbol, unknown>)[prop];
  },
});
```

Lazy initialization 패턴으로, 첫 사용 시에만 Stripe 클라이언트를 생성한다. Proxy를 통해 `stripe.customers.create()` 같은 호출이 자동으로 초기화를 트리거한다.

### STRIPE_PLANS 매핑

> 소스: `apps/web/lib/stripe.ts:21-45`

```typescript
export const STRIPE_PLANS: Record<string, { priceId: string; name: string; price: number }> = {
  lite:     { priceId: process.env.STRIPE_LITE_PRICE_ID ?? "",     name: "Lite",     price: 500 },
  pro:      { priceId: process.env.STRIPE_PRO_PRICE_ID ?? "",      name: "Pro",      price: 1800 },
  business: { priceId: process.env.STRIPE_BUSINESS_PRICE_ID ?? "", name: "Business", price: 5900 },
  scale:    { priceId: process.env.STRIPE_SCALE_PRICE_ID ?? "",    name: "Scale",    price: 14900 },
};
```

| 플랜 | Stripe Price (센트) | 달러 환산 | 환경 변수 |
|------|---------------------|----------|-----------|
| lite | 500 | $5.00 | `STRIPE_LITE_PRICE_ID` |
| pro | 1,800 | $18.00 | `STRIPE_PRO_PRICE_ID` |
| business | 5,900 | $59.00 | `STRIPE_BUSINESS_PRICE_ID` |
| scale | 14,900 | $149.00 | `STRIPE_SCALE_PRICE_ID` |

`free`와 `enterprise` 플랜은 Stripe 매핑에 없다. free는 결제가 불필요하고, enterprise는 별도 계약이다.

### Checkout Session 생성

> 소스: `apps/web/lib/stripe.ts:50-99`

```typescript
export async function createCheckoutSession(params: {
  customerId: string;
  priceId: string;
  planId: PlanId;
  orgSlug: string;
  successUrl: string;
  cancelUrl: string;
  stripePromoCodeId?: string;
  extraSeatQuantity?: number;
}) { ... }
```

Checkout 흐름:

```
createCheckoutSession()
    |
    +-- Line Items 구성:
    |       [1] 기본 플랜 price (quantity: 1)
    |       [2] 추가 좌석 price (quantity: extraSeatQuantity) -- 있을 때만
    |
    +-- Stripe 옵션:
    |       mode: "subscription"
    |       automatic_tax: { enabled: true }
    |       tax_id_collection: { enabled: true }
    |       billing_address_collection: "required"
    |
    +-- Metadata:
    |       orgSlug, entitlementPlanId (세션 + 구독 모두에 설정)
    |
    +-- 프로모 코드 처리:
            stripePromoCodeId 있음? -> discounts: [{ promotion_code: id }]
            없음? -> allow_promotion_codes: true (사용자가 입력 가능)
```

### Customer Portal

> 소스: `apps/web/lib/stripe.ts:153-186`

```typescript
export async function createCustomerPortalSession(params: {
  customerId: string;
  returnUrl: string;
  switchToPlan?: {
    subscriptionId: string;
    subscriptionItemId: string;
    newPriceId: string;
  };
}) { ... }
```

| 모드 | 설명 |
|------|------|
| 기본 | `billingPortal.sessions.create({ customer, return_url })` -- 일반 포탈 |
| 플랜 변경 | `flow_data.type: "subscription_update_confirm"` -- 특정 플랜으로 전환 |

### Webhook 이벤트 처리

> 소스: `apps/web/app/api/stripe/webhook/route.ts`

```
Stripe Webhook POST /api/stripe/webhook
    |
    +-- 서명 검증: stripe.webhooks.constructEvent(body, sig, secret)
    |
    +-- 이벤트 분기:
           |
           +-- checkout.session.completed
           |       구매 완료 -> 플랜/제한 업데이트, 프로모 코드 추적
           |
           +-- customer.subscription.created
           |       구독 생성 -> orgSlug 또는 customerId로 조직 매칭, 플랜 설정
           |
           +-- customer.subscription.updated
           |       구독 변경 -> 플랜/제한 재계산
           |
           +-- customer.subscription.deleted
           |       구독 취소 -> free 플랜으로 다운그레이드
           |
           +-- invoice.payment_failed
           |       결제 실패 -> 로깅
           |
           +-- customer.updated
           |       고객 정보 변경 -> billing profile 동기화
           |
           +-- customer.tax_id.created/deleted
                   세금 ID 변경 -> billing profile 동기화
```

### checkout.session.completed 처리 상세

> 소스: `apps/web/app/api/stripe/webhook/route.ts:47-167`

```
checkout.session.completed
    |
    +-- orgSlug, subscriptionId, customerId 추출
    |
    +-- subscription에서 planId 결정:
    |       1. getPlanFromSubscription() -- price ID 매칭
    |       2. getPlanFromMetadata() -- metadata.entitlementPlanId
    |       3. adminCreated? -> "enterprise"
    |
    +-- extraSeatQuantity 계산
    |       STRIPE_EXTRA_SEAT_PRICE_ID와 일치하는 item의 quantity
    |
    +-- DB 업데이트:
    |       stripeSubscriptionId, planId, updatedAt
    |       customLimits가 아닌 경우: projectLimit, memberLimit 등 재계산
    |       entitlementManaged인 경우: planOverride, trialEndsAt=null, planExpiresAt=null
    |
    +-- 프로모 코드 추적:
    |       session.discounts에서 promotion_code 추출
    |       promoCodes 테이블에서 매칭
    |       promoRedemptions INSERT
    |       promoCodes.timesRedeemed++, totalDiscountGiven 누적
    |
    +-- syncOrgBillingProfile(customerId)
    |       customer 정보 -> org의 companyName, taxId, billingAddress 동기화
    |
    +-- enforceSeatComplianceStatus(orgId)
            멤버 수 > memberLimit? -> org.status = "suspended"
            멤버 수 <= memberLimit && 이전에 suspended? -> org.status = "active"
```

### customer.subscription.deleted 처리

> 소스: `apps/web/app/api/stripe/webhook/route.ts:308-348`

구독 취소 시:

```typescript
const freePlan = PLANS.free;
const updateValues = {
  planId: "free",
  stripeSubscriptionId: null,
  updatedAt: new Date(),
};
// entitlementManaged이거나 customLimits가 아닌 경우:
updateValues.planOverride = null;
updateValues.customLimits = false;
updateValues.projectLimit = freePlan.projectLimit;      // 3
updateValues.memberLimit = freePlan.memberLimit;         // 1
updateValues.memoryLimitPerProject = null;
updateValues.apiRatePerMinute = null;
updateValues.planTemplateId = null;
updateValues.trialEndsAt = null;
updateValues.planExpiresAt = null;
```

### 헬퍼 함수

| 함수 | 소스 위치 | 설명 |
|------|-----------|------|
| `getPlanFromPriceId()` | `webhook/route.ts:379-393` | Stripe price ID -> PlanId 매핑 |
| `getPlanFromSubscription()` | `webhook/route.ts:395-407` | subscription items에서 planId 추출 |
| `getPlanFromMetadata()` | `webhook/route.ts:409-420` | metadata.entitlementPlanId 추출 |
| `isEntitlementManaged()` | `webhook/route.ts:430-436` | 관리자 생성 구독인지 확인 |
| `getExtraSeatQuantityFromSubscription()` | `webhook/route.ts:438-449` | 추가 좌석 수량 추출 |
| `syncOrgBillingProfile()` | `webhook/route.ts:487-516` | Stripe customer -> org billing 정보 동기화 |
| `enforceSeatComplianceStatus()` | `webhook/route.ts:518-568` | 좌석 초과 시 org 정지/복구 |

### Admin 구독 관리

> 소스: `apps/web/lib/stripe.ts:188-231`

관리자가 직접 생성하는 구독:

```typescript
export async function createAdminSubscription(params: {
  customerId: string;
  priceId: string;
  orgSlug: string;
  entitlementPlanId?: PlanId;
}): Promise<{ subscriptionId: string }> {
  const subscription = await s.subscriptions.create({
    customer: params.customerId,
    items: [{ price: params.priceId }],
    metadata: {
      orgSlug: params.orgSlug,
      adminCreated: "true",
      entitlementManaged: "true",
      entitlementPlanId: params.entitlementPlanId ?? "enterprise",
    },
  });
  return { subscriptionId: subscription.id };
}
```

`adminCreated: "true"` 메타데이터는 webhook에서 이 구독이 관리자에 의해 생성되었음을 식별하는 데 사용된다. `entitlementManaged: "true"`는 플랜 변경 시 `planOverride`를 자동 설정하도록 한다.

### Custom Price 생성

> 소스: `apps/web/lib/stripe.ts:188-202`

```typescript
export async function createCustomPrice(params: {
  unitAmountCents: number;
  productName: string;
  interval: "month" | "year";
}): Promise<{ productId: string; priceId: string }> { ... }
```

Enterprise 고객을 위한 맞춤 가격 생성. Stripe product와 price를 동시에 생성한다.

---

## 8. 좌석 과금

> 소스: `apps/web/lib/seat-billing.ts`

플랜에 포함된 멤버 수를 초과하는 경우 추가 좌석 비용이 자동으로 Stripe 구독에 추가된다.

### 좌석 과금 대상 플랜

> 소스: `apps/web/lib/seat-billing.ts:8`

```typescript
const PAID_SEAT_PLANS: PlanId[] = ["lite", "pro", "business", "scale"];
```

`free`와 `enterprise`는 좌석 과금 대상이 아니다. free는 항상 1명이고, enterprise는 무제한이다.

### ensureSeatForAdditionalMember()

> 소스: `apps/web/lib/seat-billing.ts:63-136`

새 멤버 추가 시 좌석 확보를 보장하는 함수이다.

```
ensureSeatForAdditionalMember(orgId)
    |
    +-- org 조회: planId, memberLimit, stripeSubscriptionId, customLimits
    |
    +-- 현재 멤버 수 조회
    +-- requiredMemberCapacity = currentMembers + 1
    |
    +-- requiredMemberCapacity <= org.memberLimit?
    |       -> { ok: true }  (기존 한도 내)
    |
    +-- isSelfHosted()?
    |       -> { ok: false, error: "Member limit reached" }
    |
    +-- !isBillingEnabled()?
    |       -> { ok: false, error: "Billing is not enabled" }
    |
    +-- !STRIPE_EXTRA_SEAT_PRICE_ID?
    |       -> { ok: false, error: "Extra seat billing not configured" }
    |
    +-- org.customLimits?
    |       -> { ok: false, error: "Custom limits, add seats in admin" }
    |
    +-- !org.stripeSubscriptionId?
    |       -> { ok: false, error: "Start a paid subscription to add seats" }
    |
    +-- baseIncludedSeats = getBaseIncludedSeats(org.planId)
    |       null이면? -> { ok: false, error: "Plan does not support extra seats" }
    |
    +-- requiredExtraSeats = max(0, requiredMemberCapacity - baseIncludedSeats)
    |
    +-- upsertExtraSeatQuantity(subscriptionId, requiredExtraSeats)
    |       Stripe 구독에 추가 좌석 line item 추가/수정
    |
    +-- DB: memberLimit = baseIncludedSeats + requiredExtraSeats
    |
    +-- { ok: true }
```

### 실패 조건 정리

| 조건 | 에러 메시지 | 설명 |
|------|------------|------|
| Self-hosted | `"Member limit reached for this organization"` | 과금 불가 |
| Billing 미활성 | `"Billing is not enabled"` | `STRIPE_SECRET_KEY` 미설정 |
| Price ID 미설정 | `"Extra seat billing is not configured"` | `STRIPE_EXTRA_SEAT_PRICE_ID` 미설정 |
| Custom limits | `"Custom limits, add seats in admin"` | 관리자 직접 관리 |
| 구독 없음 | `"Start a paid subscription to add seats"` | free 플랜 |
| 지원 안 되는 플랜 | `"Current plan does not support extra seats"` | free/enterprise |

### syncSeatQuantityToMemberCount()

> 소스: `apps/web/lib/seat-billing.ts:138-173`

멤버가 제거된 후 좌석 수를 줄이기 위한 동기화 함수이다.

```
syncSeatQuantityToMemberCount(orgId)
    |
    +-- isSelfHosted() || !isBillingEnabled() || !STRIPE_EXTRA_SEAT_PRICE_ID?
    |       -> return (과금 비활성)
    |
    +-- org 조회: planId, stripeSubscriptionId, customLimits
    |
    +-- customLimits || !stripeSubscriptionId?
    |       -> return (조정 불필요)
    |
    +-- baseIncludedSeats = getBaseIncludedSeats(planId)
    |       null이면? -> return
    |
    +-- memberCount = 현재 멤버 수
    +-- requiredExtraSeats = max(0, memberCount - baseIncludedSeats)
    |
    +-- upsertExtraSeatQuantity(subscriptionId, requiredExtraSeats)
    |       0이면 line item 삭제, 양수면 수량 조정
    |
    +-- DB: memberLimit = baseIncludedSeats + requiredExtraSeats
```

### upsertExtraSeatQuantity()

> 소스: `apps/web/lib/seat-billing.ts:25-61`

Stripe 구독에서 추가 좌석 line item을 관리하는 내부 함수이다:

```
upsertExtraSeatQuantity(subscriptionId, quantity)
    |
    +-- subscription 조회
    +-- STRIPE_EXTRA_SEAT_PRICE_ID와 일치하는 item 검색
    |
    +-- quantity <= 0?
    |       |
    |       +-- existingItem 있음? -> subscriptionItems.del (proration)
    |       +-- 없음? -> 아무것도 안 함
    |
    +-- quantity > 0?
            |
            +-- existingItem 있음? -> subscriptionItems.update({ quantity })
            +-- 없음? -> subscriptionItems.create({ price, quantity })
            |
            +-- 모든 변경: proration_behavior: "create_prorations"
```

`proration_behavior: "create_prorations"`는 좌석 수 변경 시 일할 계산된 금액이 다음 청구서에 반영되도록 한다.

### 좌석 준수 상태 강제

> 소스: `apps/web/app/api/stripe/webhook/route.ts:518-568`

Stripe webhook에서 플랜/구독 변경 후 `enforceSeatComplianceStatus()`가 호출된다:

| 상태 | 조건 | 동작 |
|------|------|------|
| 정상 -> 정지 | `memberCount > memberLimit` | `status = "suspended"`, `statusReason = "seat_limit_exceeded_unpaid"` |
| 정지 -> 복구 | `memberCount <= memberLimit` && `statusReason === "seat_limit_exceeded_unpaid"` | `status = "active"`, `statusReason = null` |

정지는 좌석 초과 사유(`seat_limit_exceeded_unpaid`)인 경우에만 자동 복구된다. 다른 사유(관리자 수동 정지 등)로 정지된 조직은 자동 복구되지 않는다.

---

## 9. 프로모 코드

> 소스: `apps/web/app/api/v1/orgs/[slug]/validate-promo/route.ts`, `apps/web/lib/stripe.ts:101-151`

### 프로모 코드 검증 흐름

> 소스: `apps/web/app/api/v1/orgs/[slug]/validate-promo/route.ts`

```
POST /api/v1/orgs/{slug}/validate-promo
    body: { code: "LAUNCH50", planId: "pro" }
    |
    +-- [인증] auth.api.getSession()
    +-- [Rate Limit] LRU 캐시, 분당 10회 (사용자 ID 기준)
    +-- [멤버십] 조직 멤버 확인
    |
    +-- 검증 체크 (순서대로, 첫 실패 시 중단):
    |
    |   [1] code 존재 및 active 여부
    |       -> 실패: "Invalid promo code"
    |
    |   [2] startsAt 확인 (시작일 이전?)
    |       -> 실패: "This code is not active yet"
    |
    |   [3] expiresAt 확인 (만료?)
    |       -> 실패: "This code has expired"
    |
    |   [4] maxRedemptions 확인 (전체 사용 횟수 초과?)
    |       -> 실패: "This code has reached its usage limit"
    |
    |   [5] maxRedemptionsPerOrg 확인 (조직당 사용 횟수 초과?)
    |       -> 실패: "Already used by your organization"
    |
    |   [6] restrictedToOrgs 확인 (특정 조직 제한?)
    |       -> 실패: "This code is not available for your organization"
    |
    |   [7] applicablePlans 확인 (적용 가능 플랜?)
    |       -> 실패: "Not valid for this plan"
    |
    |   [8] minimumPlanTier 확인 (최소 플랜 티어?)
    |       -> 실패: "Requires a higher plan tier"
    |
    |   [9] firstSubscriptionOnly 확인 (첫 구독 전용?)
    |       -> 실패: "Only valid for first subscription"
    |
    |   [10] noPreviousPromo 확인 (이전 프로모 사용 이력?)
    |       -> 실패: "Only valid for organizations that haven't used a promo code before"
    |
    +-- 모든 검증 통과:
          {
            valid: true,
            discount: { type, amount, currency, duration, durationInMonths },
            stripePromoCodeId: "promo_xxx"
          }
```

### 검증 조건 상세

| # | 조건 | DB 필드 | 타입 | 설명 |
|---|------|---------|------|------|
| 1 | 코드 존재/활성 | `promoCodes.active` | `boolean` | 비활성 코드 차단 |
| 2 | 시작일 | `promoCodes.startsAt` | `Date \| null` | 예약 프로모 코드 |
| 3 | 만료일 | `promoCodes.expiresAt` | `Date \| null` | 기간 한정 프로모 |
| 4 | 전체 사용 한도 | `promoCodes.maxRedemptions` | `number \| null` | null = 무제한 |
| 5 | 조직당 사용 한도 | `promoCodes.maxRedemptionsPerOrg` | `number \| null` | 중복 사용 방지 |
| 6 | 조직 제한 | `promoCodes.restrictedToOrgs` | `JSON string \| null` | 특정 조직만 허용 |
| 7 | 적용 플랜 | `promoCodes.applicablePlans` | `JSON string \| null` | 특정 플랜만 허용 |
| 8 | 최소 플랜 티어 | `promoCodes.minimumPlanTier` | `string \| null` | 하위 플랜 차단 |
| 9 | 첫 구독 전용 | `promoCodes.firstSubscriptionOnly` | `boolean` | 기존 구독자 차단 |
| 10 | 이전 프로모 미사용 | `promoCodes.noPreviousPromo` | `boolean` | 프로모 중복 사용 차단 |

### 플랜 티어 순서

> 소스: `apps/web/app/api/v1/orgs/[slug]/validate-promo/route.ts:15-22`

```typescript
const PLAN_TIER_ORDER: PlanId[] = [
  "free", "lite", "pro", "business", "scale", "enterprise",
];
```

`minimumPlanTier` 검증 시 이 순서를 사용하여 선택 플랜이 최소 요구 티어 이상인지 확인한다.

### 프로모 Rate Limiting

> 소스: `apps/web/app/api/v1/orgs/[slug]/validate-promo/route.ts:24-30`

```typescript
const PROMO_RATE_LIMIT = 10;
const promoRateCache = new LRUCache<string, { count: number; resetAt: number }>({
  max: 5_000,
  ttl: 60_000,
});
```

| 설정 | 값 | 설명 |
|------|------|------|
| 최대 시도 | 10회/분 | 사용자 ID 기준 |
| LRU 캐시 크기 | 5,000 | 동시 사용자 처리 |
| TTL | 60초 | 1분 윈도우 |

브루트 포스 공격을 방지하기 위한 별도의 rate limiter이다.

### Stripe 프로모 코드 생성

> 소스: `apps/web/lib/stripe.ts:101-143`

```typescript
export async function createStripeCouponAndPromoCode(params: {
  code: string;
  discountType: "percent" | "fixed";
  discountAmount: number;
  currency?: string;
  duration: "once" | "repeating" | "forever";
  durationInMonths?: number;
  maxRedemptions?: number;
  expiresAt?: Date;
  firstSubscriptionOnly?: boolean;
}) { ... }
```

Stripe에서의 프로모 코드 생성은 2단계이다:
1. **Coupon 생성**: 할인 유형(percent/fixed), 기간, 최대 사용 횟수
2. **Promotion Code 생성**: coupon에 연결된 코드 문자열, 활성 상태

### Redemption 추적

> 소스: `apps/web/app/api/stripe/webhook/route.ts:107-161`

프로모 코드 사용은 `checkout.session.completed` webhook에서 추적된다:

```
checkout.session.completed
    |
    +-- session.discounts에서 promotion_code ID 추출
    |
    +-- promoCodes 테이블에서 stripePromoCodeId로 검색
    |
    +-- promoRedemptions INSERT:
    |       { promoCodeId, orgId, userId, planId, discountApplied, stripeCheckoutSessionId }
    |
    +-- promoCodes UPDATE:
            timesRedeemed += 1
            totalDiscountGiven += totalDiscount
```

### 프로모 코드 비활성화/재활성화

> 소스: `apps/web/lib/stripe.ts:145-151`

```typescript
export async function deactivateStripePromoCode(promoCodeId: string) {
  return getStripe().promotionCodes.update(promoCodeId, { active: false });
}

export async function reactivateStripePromoCode(promoCodeId: string) {
  return getStripe().promotionCodes.update(promoCodeId, { active: true });
}
```

---

## 10. 베타 게이트

> 소스: `apps/web/middleware.ts`

웹 애플리케이션의 페이지 라우트에 HTTP Basic 인증을 적용하여 베타 접근을 제한하는 미들웨어이다.

### 미들웨어 구조

```typescript
export function middleware(request: NextRequest) {
  // API 경로는 통과
  if (request.nextUrl.pathname.startsWith("/api")) {
    return NextResponse.next();
  }

  if (isHostProtected(request.headers.get("host") ?? "") && !isAuthorized(request)) {
    return new NextResponse("Authentication required", {
      status: 401,
      headers: { "WWW-Authenticate": 'Basic realm="Private Beta"' },
    });
  }

  return NextResponse.next();
}
```

### 미들웨어 matcher

> 소스: `apps/web/middleware.ts:79-84`

```typescript
export const config = {
  matcher: [
    "/((?!api|_next/static|_next/image|favicon.ico|fonts|.*\\..*$).*)",
  ],
};
```

제외 경로:
- `/api/*` -- API 라우트 (미들웨어 내부에서도 추가 확인)
- `/_next/static/*` -- Next.js 정적 파일
- `/_next/image/*` -- Next.js 이미지 최적화
- `/favicon.ico`, `/fonts/*` -- 정적 리소스
- `*.xxx` -- 확장자가 있는 모든 파일

### isHostProtected()

> 소스: `apps/web/middleware.ts:18-31`

```
isHostProtected(requestHost)
    |
    +-- BETA_GATE_ENABLED !== "true"? -> false (비활성)
    |
    +-- BETA_GATE_HOSTS 파싱 (쉼표로 분리, 정규화)
    |       빈 목록 또는 "*" 포함? -> true (모든 호스트 보호)
    |
    +-- requestHost를 정규화하여 configuredHosts에 포함되는지 확인
```

### 환경 변수

| 환경 변수 | 기본값 | 설명 |
|-----------|--------|------|
| `BETA_GATE_ENABLED` | (미설정 = 비활성) | `"true"`이면 베타 게이트 활성화 |
| `BETA_GATE_HOSTS` | (미설정 = 모든 호스트) | 쉼표로 구분된 호스트 목록. `"*"` 또는 빈 값이면 전체 |
| `BETA_GATE_USERNAME` | `"beta"` | Basic auth 사용자명 |
| `BETA_GATE_PASSWORD` | (미설정 = 인증 불가) | Basic auth 비밀번호. 미설정 시 모든 인증 실패 |

### isAuthorized()

> 소스: `apps/web/middleware.ts:33-57`

```
isAuthorized(request)
    |
    +-- BETA_GATE_PASSWORD 미설정? -> false (인증 불가)
    |
    +-- Authorization 헤더 확인
    |       없음 또는 "Basic "으로 시작하지 않음? -> false
    |
    +-- Base64 디코딩
    |       실패? -> false
    |
    +-- ":" 구분자로 username:password 분리
    |       구분자 없음? -> false
    |
    +-- username === BETA_GATE_USERNAME && password === BETA_GATE_PASSWORD?
            -> true/false
```

### normalizeHost()

> 소스: `apps/web/middleware.ts:3-16`

호스트 문자열을 정규화하여 비교한다:

| 입력 | 출력 | 설명 |
|------|------|------|
| `"example.com"` | `"example.com"` | 그대로 |
| `"Example.Com:3000"` | `"example.com"` | 소문자 변환, 포트 제거 |
| `"https://example.com/path"` | `"example.com"` | URL인 경우 hostname 추출 |
| `""` | `""` | 빈 문자열 |

### 보호 흐름 요약

```
사용자 요청 (브라우저)
    |
    +-- /api/* 경로? -> 통과 (API는 자체 인증 사용)
    |
    +-- 페이지 경로 && BETA_GATE_ENABLED === "true"?
    |       |
    |       +-- 요청 호스트가 BETA_GATE_HOSTS에 포함?
    |       |       |
    |       |       +-- Authorization: Basic 헤더 유효?
    |       |       |       -> 통과
    |       |       |
    |       |       +-- 미인증?
    |       |               -> 401 + WWW-Authenticate: Basic realm="Private Beta"
    |       |               -> 브라우저가 로그인 다이얼로그 표시
    |       |
    |       +-- 호스트 미보호?
    |               -> 통과
    |
    +-- BETA_GATE_ENABLED !== "true"?
            -> 통과 (게이트 비활성)
```

### API 경로 제외의 중요성

베타 게이트는 페이지 경로에만 적용되고 `/api/*`는 제외한다. 이는 다음을 보장한다:
- CLI/MCP 클라이언트의 API 호출이 베타 게이트에 영향받지 않음
- Stripe webhook이 정상 작동
- Health check 엔드포인트 접근 가능

API 인증은 별도의 `authenticateRequest()` 미들웨어(섹션 2, 3 참조)에서 처리한다.

---

## 부록: 인증 & 과금 아키텍처 전체도

```
                         +------------------+
                         |  Next.js 미들웨어  |
                         |  (베타 게이트)      |
                         +--------+---------+
                                  |
                    +-------------+-------------+
                    |                           |
               /api/* 경로                  페이지 경로
                    |                           |
          +---------+---------+         +-------+--------+
          | withApiMiddleware |         |  Basic Auth    |
          |  (Request ID,    |         |  (BETA_GATE)   |
          |   로깅, 에러 래핑,  |         +----------------+
          |   authenticateRequest
          |   내부 호출)       |
          +---------+---------+
                    |
          +---------+---------------------+
          | 라우트 핸들러                    |
          | (필요에 따라 유틸리티 함수 호출)   |
          |                               |
          |  requireOrgMembership()       |
          |  checkProjectAccess()         |
          |  checkRateLimit()             |
          +-------------------------------+


  +------- Stripe 과금 흐름 -------+
  |                                |
  |  Checkout -> Webhook ->        |
  |  Plan Update -> Seat Billing   |
  |  -> Compliance Enforcement     |
  |                                |
  +--------------------------------+
```
