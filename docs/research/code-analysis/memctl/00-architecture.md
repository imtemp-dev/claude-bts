# memctl -- 아키텍처 분석

> AI 코딩 에이전트를 위한 공유 메모리 시스템. MCP(Model Context Protocol) 기반의 서버와 CLI를 제공하며, 팀 단위로 프로젝트 컨텍스트를 동기화한다.
>
> 소스 경로: `~/Workspace/context-sync-research/memctl/`

---

## 목차

1. [프로젝트 구조](#1-프로젝트-구조)
2. [테크 스택](#2-테크-스택)
3. [빌드 시스템](#3-빌드-시스템)
4. [진입점](#4-진입점)
5. [패키지 의존 관계](#5-패키지-의존-관계)
6. [환경 구성](#6-환경-구성)
7. [CI/CD](#7-cicd)

---

## 1. 프로젝트 구조

### 1.1 Monorepo 레이아웃

```
memctl/
├── apps/
│   └── web/                    # Next.js 웹 애플리케이션 (대시보드 + API)
├── packages/
│   ├── cli/                    # CLI + MCP 서버 (npm 패키지: memctl)
│   ├── db/                     # Drizzle ORM 스키마 + 클라이언트
│   └── shared/                 # 공유 타입, 상수, 유효성 검증, 알고리즘
├── plugins/
│   └── memctl/                 # Claude Code 플러그인 (hook 기반 자동 캡처)
├── docs/                       # 문서
├── .github/
│   └── workflows/              # CI/CD (ci.yml, publish.yml)
├── turbo.json                  # Turborepo 태스크 설정
├── pnpm-workspace.yaml         # pnpm 워크스페이스 정의
├── tsconfig.json               # 루트 TypeScript 설정
├── Dockerfile                  # 프로덕션 멀티 스테이지 빌드
├── Dockerfile.dev              # 개발용 Docker 이미지
├── docker-compose.yml          # 개발 환경 (libsql + web + stripe-cli)
├── docker-compose.prod.yml     # 프로덕션 환경 (libsql + web)
├── vitest.config.ts            # 테스트 설정
├── eslint.config.mjs           # ESLint flat config
├── .prettierrc                 # Prettier 설정
└── .mcp.json                   # 개발용 MCP 서버 설정 (shadcn)
```

### 1.2 각 패키지의 역할

| 패키지 | npm 이름 | 유형 | 역할 |
|--------|----------|------|------|
| `apps/web` | `@memctl/web` | Next.js App | 웹 대시보드 UI, REST API (`/api/v1/*`), 인증, 결제, 관리자 패널 |
| `packages/cli` | `memctl` | npm 공개 패키지 | CLI 도구 + MCP 서버 (stdio transport). `npx memctl`로 실행 |
| `packages/db` | `@memctl/db` | 내부 패키지 | Drizzle ORM 스키마 정의, libSQL/Turso 클라이언트 생성 |
| `packages/shared` | `@memctl/shared` | 내부 패키지 | 타입 인터페이스, 요금제 상수, Zod 검증 스키마, 관련도 스코어링, 검색 의도 분류 |
| `plugins/memctl` | -- | Claude Code 플러그인 | hook 기반으로 세션 시작/종료, 턴 데이터를 자동 캡처하여 memctl API로 전송 |

### 1.3 `apps/web` 세부 구조

Next.js 15 App Router 기반이다. 주요 라우트 그룹은 다음과 같다.

```
apps/web/
├── app/
│   ├── (auth)/login/                   # 로그인 페이지
│   ├── (dashboard)/
│   │   ├── onboarding/                 # 온보딩 플로우
│   │   └── org/[orgSlug]/             # 조직 대시보드
│   │       ├── activity/               # 활동 피드
│   │       ├── billing/                # 결제/구독 관리
│   │       ├── health/                 # 메모리 건강 대시보드
│   │       ├── hygiene/                # 메모리 위생 대시보드
│   │       ├── members/                # 팀원 관리
│   │       ├── projects/[projectSlug]/ # 프로젝트 상세
│   │       ├── settings/               # 조직 설정
│   │       ├── tokens/                 # API 토큰 관리
│   │       └── usage/                  # 사용량 차트
│   ├── (marketing)/pricing/            # 가격 페이지
│   ├── admin/(protected)/              # 관리자 패널 (blog, changelog, orgs, users, promo-codes, plan-templates)
│   ├── api/
│   │   ├── auth/[...all]/              # better-auth 핸들러
│   │   ├── search/                     # 문서 검색
│   │   ├── stripe/webhook/             # Stripe webhook 핸들러
│   │   └── v1/                         # REST API (아래 상세)
│   ├── blog/                           # 블로그
│   ├── changelog/                      # 변경 로그
│   └── docs/                           # Fumadocs 기반 문서
├── components/                          # UI 컴포넌트 (shadcn/ui 기반)
├── content/                             # MDX 문서 콘텐츠
├── emails/                              # React Email 템플릿
├── hooks/                               # React hooks
├── lib/                                 # 서버 유틸리티 (아래 상세)
├── public/                              # 정적 파일
└── scripts/                             # 유틸리티 스크립트
```

**`apps/web/lib/` 모듈 목록:**

| 파일 | 역할 |
|------|------|
| `auth.ts` | better-auth 인스턴스 생성 (GitHub OAuth, Magic Link, 개발용 bypass) |
| `auth-client.ts` | 클라이언트 사이드 auth 유틸리티 |
| `api-middleware.ts` | API 인증 미들웨어 (JWT Bearer, API 토큰, 세션 쿠키) |
| `jwt.ts` | JWT 생성/검증 (jose), LRU 캐시 기반 세션 캐싱 |
| `db/index.ts` | 싱글턴 DB 인스턴스 |
| `embeddings.ts` | `@xenova/transformers`로 `all-MiniLM-L6-v2` 모델 기반 벡터 임베딩 생성 |
| `fts.ts` | SQLite FTS5 가상 테이블 생성 및 전문 검색 (hybrid: FTS + 벡터 유사도) |
| `rate-limit.ts` | 인메모리 슬라이딩 윈도우 rate limiter (분당 요청 제한) |
| `stripe.ts` | Stripe 결제 통합 |
| `seat-billing.ts` | 좌석 기반 과금 로직 |
| `plans.ts` | 요금제 한도 해석, self-hosted 모드 처리 |
| `email.ts` | Resend API 기반 이메일 발송 |
| `scheduler.ts` | `node-cron` 기반 백그라운드 스케줄러 (Next.js instrumentation에서 초기화) |
| `logger.ts` | pino 기반 구조화 로깅 |
| `etag.ts` | HTTP ETag 지원 |
| `schema-validator.ts` | AJV 기반 JSON 스키마 검증 |
| `audit.ts` | 감사 로그 기록 |
| `admin.ts` | 관리자 권한 검증 |
| `source.ts` | Fumadocs MDX 소스 설정 |

**`apps/web/app/api/v1/` 주요 엔드포인트:**

| 경로 | 기능 |
|------|------|
| `memories/` | CRUD, 검색, 일괄 처리, 버전 관리, 스냅샷, 잠금, 피드백, 라이프사이클 |
| `memories/search-org/` | 조직 전체 메모리 검색 |
| `memories/export/` | agents\_md, cursorrules, json 포맷 내보내기 |
| `memories/similar/` | 벡터 유사도 기반 유사 메모리 검색 |
| `memories/traverse/` | 그래프 탐색 (관련 메모리 체인) |
| `memories/health/` | 메모리 건강 점수 |
| `memories/lifecycle/` | 자동 정리/보관/승격/강등 정책 실행 |
| `orgs/[slug]/` | 조직 관리, 멤버, 초대, 프로젝트, 활동, 결제 |
| `tokens/` | API 토큰 생성/해지 |
| `context-types/` | 사용자 정의 컨텍스트 타입 CRUD |
| `session-logs/` | 에이전트 세션 로그 |
| `auth/token/` | JWT 토큰 발급 |
| `batch/` | 일괄 API 호출 |
| `admin/*` | 관리자 전용 API (블로그, 변경로그, 사용자, 조직, 프로모 코드, 요금제 템플릿) |

### 1.4 `packages/cli` 세부 구조

```
packages/cli/src/
├── index.ts                # 진입점: CLI vs MCP 서버 라우팅
├── cli.ts                  # CLI 명령어 파서 및 핸들러
├── server.ts               # MCP 서버 생성 (McpServer 인스턴스)
├── api-client.ts           # REST API 클라이언트 (fetch 기반, 캐싱, 오프라인 모드)
├── config.ts               # ~/.memctl/config.json 관리, MCP env 파일 탐색
├── config-command.ts       # `memctl config` 명령어
├── auth.ts                 # `memctl auth` 브라우저 기반 인증
├── init.ts                 # `memctl init` 설정 마법사 (IDE별 MCP 설정 생성)
├── doctor.ts               # `memctl doctor` 진단
├── cache.ts                # 인메모리 ETag/TTL 캐시 (stale-while-revalidate)
├── local-cache.ts          # better-sqlite3 기반 로컬 영구 캐시 (오프라인 지원)
├── session-tracker.ts      # 세션 추적 (읽기/쓰기 키, 도구 호출 기록)
├── agent-context.ts        # 에이전트 컨텍스트 빌드 (부트스트랩, 기능별 항목 추출)
├── agents-template.ts      # AGENTS.md 템플릿 생성
├── hooks.ts                # `memctl hook` 명령어 (start/turn/end)
├── hook-adapter.ts         # 에이전트별 hook 어댑터 템플릿 생성
├── intent.ts               # (추가 의도 분류 로직)
├── ui.ts                   # 터미널 색상 출력 유틸리티
├── tools/
│   ├── index.ts            # 모든 MCP 도구 등록
│   ├── rate-limit.ts       # 클라이언트 사이드 rate limiter
│   ├── response.ts         # 응답 포맷팅 유틸리티
│   └── handlers/
│       ├── memory.ts           # memory 도구 (store, get, search, delete, update, list)
│       ├── memory-advanced.ts  # 고급 메모리 도구 (link, similar, diff, feedback, lock 등)
│       ├── memory-lifecycle.ts # 라이프사이클 도구 (cleanup, archive, snapshot, rollback 등)
│       ├── context.ts          # context 도구 (bootstrap, smart_retrieve, context_for 등)
│       ├── context-config.ts   # context-config 도구 (타입 관리)
│       ├── branch.ts           # branch 도구 (get, plan, diff)
│       ├── session.ts          # session 도구 (start, end, handoff)
│       ├── import-export.ts    # import-export 도구 (export, import)
│       ├── repo.ts             # repo 도구 (프로젝트/조직 관리)
│       ├── org.ts              # org 도구 (조직 레벨 메모리)
│       └── activity.ts         # activity 도구 (memo_read, memo_leave)
└── resources/
    └── index.ts            # MCP 리소스 등록 (memory://, agent://, session://)
```

### 1.5 `plugins/memctl` 세부 구조

Claude Code 플러그인으로, hook 시스템을 통해 세션 라이프사이클 이벤트를 자동 캡처한다.

```
plugins/memctl/
├── .claude-plugin/
│   └── plugin.json          # 플러그인 매니페스트 (name, version, hooks 경로)
├── hooks/
│   ├── hooks.json           # hook 이벤트 매핑 (SessionStart, UserPromptSubmit, Stop, SessionEnd)
│   └── memctl-hook-dispatch.sh  # Bash 기반 hook 디스패처
└── README.md
```

**hook 이벤트 매핑:**

| 이벤트 | 단계 | 동작 |
|--------|------|------|
| `SessionStart` | `start` | 세션 ID 생성, 부트스트랩 컨텍스트 주입 |
| `SessionStart` (compact matcher) | `compact` | 컨텍스트 압축 후 리마인더 재주입 |
| `UserPromptSubmit` | `user` | `hookSpecificOutput`으로 memctl 리마인더 주입, 턴 데이터 비동기 전송 |
| `Stop` | `assistant` | 어시스턴트 응답 데이터 비동기 전송 |
| `SessionEnd` | `end` | 세션 종료 + 요약 전송, 세션 파일 삭제 |

---

## 2. 테크 스택

### 2.1 런타임 및 언어

| 기술 | 버전 | 용도 |
|------|------|------|
| Node.js | >= 20 | 런타임 (engines 제약) |
| TypeScript | ^5.7.3 | 전체 코드베이스 |
| pnpm | 9.15.4 | 패키지 매니저 (corepack) |

### 2.2 프레임워크

| 기술 | 버전 | 패키지 | 용도 |
|------|------|--------|------|
| Next.js | ^15.1.6 | `apps/web` | 웹 프레임워크 (App Router, Turbopack dev, standalone output) |
| React | ^19.0.0 | `apps/web` | UI 라이브러리 |
| React DOM | ^19.0.0 | `apps/web` | 브라우저 렌더링 |
| `@modelcontextprotocol/sdk` | ^1.0.0 | `packages/cli` | MCP 서버 SDK (stdio transport) |

### 2.3 데이터베이스

| 기술 | 버전 | 용도 |
|------|------|------|
| Turso (libSQL) | -- (서비스) | 프로덕션 데이터베이스 (SQLite 호환, edge 배포) |
| `@libsql/client` | ^0.14.0 | Turso/libSQL HTTP 클라이언트 |
| `ghcr.io/tursodatabase/libsql-server` | latest | 로컬 개발용 libSQL 서버 (Docker) |
| Drizzle ORM | ^0.41.0 | ORM (SQLite dialect) |
| Drizzle Kit | ^0.30.4 | 마이그레이션 생성/실행 도구 |
| better-sqlite3 | ^12.6.2 | CLI 로컬 캐시 (오프라인 모드 지원) |

### 2.4 인증 및 보안

| 기술 | 버전 | 용도 |
|------|------|------|
| better-auth | ^1.1.14 | 인증 프레임워크 (GitHub OAuth, Magic Link) |
| jose | ^6.1.0 | JWT 생성/검증 (HS256) |
| LRU Cache | ^11.0.2 | 세션/API 토큰 캐시 (인메모리) |

### 2.5 결제

| 기술 | 버전 | 용도 |
|------|------|------|
| Stripe | ^17.5.0 | 구독 결제, webhook, 프로모 코드 |
| stripe-cli (Docker) | latest | 로컬 webhook 포워딩 |

### 2.6 검색 및 AI

| 기술 | 버전 | 용도 |
|------|------|------|
| `@xenova/transformers` | ^2.17.2 | 서버 사이드 임베딩 생성 (all-MiniLM-L6-v2 모델) |
| SQLite FTS5 | (내장) | 전문 검색 (가상 테이블 + 트리거 동기화) |
| AJV | ^8.18.0 | JSON 스키마 유효성 검증 |
| Zod | ^3.24.1 | 런타임 타입 검증 (API 입력, 설정) |

### 2.7 UI 컴포넌트

| 기술 | 버전 | 용도 |
|------|------|------|
| Radix UI | 다수 | headless UI 프리미티브 (dialog, dropdown, tabs, toast 등) |
| shadcn/ui | -- | Radix + Tailwind 기반 UI 컴포넌트 시스템 |
| Tailwind CSS | ^4.0.0 | 유틸리티 CSS |
| class-variance-authority | ^0.7.1 | 컴포넌트 variant 관리 |
| tailwind-merge | ^2.6.0 | Tailwind 클래스 충돌 해결 |
| clsx | ^2.1.1 | 조건부 클래스 결합 |
| Lucide React | ^0.468.0 | 아이콘 |
| Geist | ^1.7.0 | 폰트 (Sans + Mono) |
| cmdk | ^1.1.1 | 커맨드 팔레트 |
| Sonner | ^2.0.7 | 토스트 알림 |
| Recharts | 2.15.4 | 차트 (사용량 대시보드) |
| d3-force | ^3.0.0 | 그래프 시각화 (메모리 관계 그래프) |
| GSAP | ^3.14.2 | 애니메이션 (랜딩 페이지) |
| Motion | ^12.34.0 | 애니메이션 |

### 2.8 문서 및 콘텐츠

| 기술 | 버전 | 용도 |
|------|------|------|
| Fumadocs (core, ui, mdx) | ^15.8.5 / ^11.10.1 | 문서 사이트 프레임워크 |
| react-markdown | ^10.1.0 | 마크다운 렌더링 |
| remark-gfm | ^4.0.1 | GFM 지원 |
| rehype-slug | ^6.0.0 | 헤딩 앵커 생성 |
| React Email | ^1.0.7 | 이메일 템플릿 |

### 2.9 이메일 및 로깅

| 기술 | 버전 | 용도 |
|------|------|------|
| Resend | ^6.9.2 | 이메일 발송 서비스 |
| pino | ^10.3.1 | 구조화 로깅 |
| node-cron | ^4.2.1 | 백그라운드 cron 스케줄링 |

### 2.10 개발 도구

| 기술 | 버전 | 용도 |
|------|------|------|
| Turborepo | ^2.3.3 | 모노레포 빌드 오케스트레이션 |
| Vitest | ^4.0.18 | 테스트 프레임워크 |
| ESLint | ^9.19.0+ | 린팅 (flat config) |
| typescript-eslint | ^8.55.0 | TypeScript ESLint 플러그인 |
| Prettier | ^3.4.2 | 코드 포매팅 |
| prettier-plugin-tailwindcss | ^0.6.11 | Tailwind 클래스 정렬 |

---

## 3. 빌드 시스템

### 3.1 pnpm 워크스페이스

`pnpm-workspace.yaml`:

```yaml
packages:
  - "apps/*"
  - "packages/*"
```

`plugins/` 디렉터리는 워크스페이스에 포함되지 않는다. 이는 Claude Code 플러그인이 독립적으로 동작하며, Node.js 의존성 없이 Bash 스크립트와 `memctl` CLI만 사용하기 때문이다.

패키지 매니저 버전은 `package.json`의 `packageManager` 필드로 고정된다:

```json
"packageManager": "pnpm@9.15.4"
```

### 3.2 Turborepo 설정

`turbo.json`:

```json
{
  "$schema": "https://turbo.build/schema.json",
  "globalEnv": ["TURSO_DATABASE_URL", "TURSO_AUTH_TOKEN", "DATABASE_URL"],
  "globalPassThroughEnv": [
    "TURSO_DATABASE_URL",
    "TURSO_AUTH_TOKEN",
    "DATABASE_URL"
  ],
  "tasks": {
    "build": {
      "dependsOn": ["^build"],
      "outputs": ["dist/**", ".next/**", "!.next/cache/**"]
    },
    "dev": {
      "cache": false,
      "persistent": true
    },
    "lint": {
      "dependsOn": ["^build"]
    },
    "lint:fix": {
      "dependsOn": ["^build"]
    },
    "typecheck": {
      "dependsOn": ["^build"]
    },
    "test": {
      "dependsOn": ["^build"]
    }
  }
}
```

**핵심 포인트:**

- `build` 태스크는 의존 패키지를 먼저 빌드한다 (`^build`). 빌드 출력물은 `dist/**`(CLI, DB, shared)와 `.next/**`(web)이다.
- `dev` 태스크는 캐시하지 않으며, 영구 실행(`persistent: true`)된다.
- `lint`, `typecheck`, `test`는 모두 `^build`에 의존한다. 즉 의존 패키지의 타입 선언이 빌드되어야 실행 가능하다.
- 데이터베이스 관련 환경변수(`TURSO_DATABASE_URL`, `TURSO_AUTH_TOKEN`, `DATABASE_URL`)는 `globalEnv`로 선언되어 모든 태스크에서 캐시 키에 반영된다.

### 3.3 TypeScript 설정

**루트 `tsconfig.json` (기본 설정):**

```json
{
  "compilerOptions": {
    "target": "ES2022",
    "lib": ["ES2022"],
    "module": "ESNext",
    "moduleResolution": "bundler",
    "strict": true,
    "noEmit": true,
    "esModuleInterop": true,
    "isolatedModules": true,
    "incremental": true,
    "declaration": true,
    "declarationMap": true,
    "sourceMap": true
  }
}
```

**패키지별 TypeScript 설정 차이:**

| 패키지 | 확장 | module | moduleResolution | outDir | 특이사항 |
|--------|------|--------|------------------|--------|----------|
| `packages/cli` | 루트 확장 | `NodeNext` | `NodeNext` | `./dist` | `noEmit: false`, `declaration: true`, 테스트 파일 제외 |
| `packages/db` | 루트 확장 | (상속) | (상속) | `./dist` | -- |
| `packages/shared` | 루트 확장 | (상속) | (상속) | `./dist` | -- |
| `apps/web` | 독립 설정 | `esnext` | `bundler` | -- | `jsx: preserve`, `@/*` path alias, Next.js 플러그인 |

`packages/cli`가 `NodeNext` 모듈 시스템을 사용하는 이유는 npm으로 배포되어 Node.js에서 직접 실행되기 때문이다. 나머지 내부 패키지는 source-level import(`.ts` 직접 참조)를 사용하므로 빌드가 필요 없다.

### 3.4 빌드 출력물

| 패키지 | 빌드 명령 | 출력물 | 설명 |
|--------|----------|--------|------|
| `packages/cli` | `tsc` | `dist/` (JS + `.d.ts`) | npm publish 대상. `bin.memctl = ./dist/index.js` |
| `packages/db` | -- | 없음 (source export) | `exports`가 `.ts` 파일을 직접 가리킴 |
| `packages/shared` | -- | 없음 (source export) | `exports`가 `.ts` 파일을 직접 가리킴 |
| `apps/web` | `next build` | `.next/` (standalone) | `output: "standalone"` 모드, Docker 배포용 |

### 3.5 Drizzle Kit 설정

`packages/db/drizzle.config.ts`:

```typescript
export default defineConfig({
  schema: "./src/schema.ts",
  out: "./src/migrations",
  dialect: "turso",
  dbCredentials: { url, authToken },
  tablesFilter: ["!memories_fts*"],  // FTS5 가상 테이블 제외
});
```

- dialect: `turso` (libSQL 호환)
- FTS5 테이블(`memories_fts*`)은 런타임에 동적 생성되므로 마이그레이션에서 제외한다.
- 로컬 libSQL 서버에 연결 시 더미 auth token(`local-dev-token`)을 사용한다.

### 3.6 테스트 설정

`vitest.config.ts`:

```typescript
export default defineConfig({
  test: {
    globals: true,
    environment: "node",
    include: ["packages/*/src/**/*.test.ts", "apps/web/**/*.test.ts"],
    exclude: ["**/node_modules/**", "**/dist/**"],
  },
});
```

### 3.7 린팅 및 포매팅

**ESLint** (`eslint.config.mjs`): flat config 형식, `@eslint/js` + `typescript-eslint` recommended 규칙 적용. `no-unused-vars`는 `_` 프리픽스 무시, `no-explicit-any`는 경고.

**Prettier** (`.prettierrc`):

```json
{
  "semi": true,
  "singleQuote": false,
  "tabWidth": 2,
  "trailingComma": "all",
  "plugins": ["prettier-plugin-tailwindcss"]
}
```

---

## 4. 진입점

### 4.1 `packages/cli` -- 이중 진입점 (CLI vs MCP)

`packages/cli/src/index.ts`는 memctl의 핵심 진입점이다. 하나의 바이너리가 CLI와 MCP 서버 두 가지 모드로 동작한다.

```
memctl <command>
  │
  ├─ command in cliCommands[] ──> runCli(args)  [cli.ts]
  │     list, get, search, export, generate, hook,
  │     init, auth, config, doctor, whoami, status,
  │     delete, pin, unpin, archive, unarchive, ...
  │
  └─ command 없음 또는 "serve" ──> createServer() + StdioServerTransport  [server.ts]
        MCP 서버 모드 (stdin/stdout 통신)
```

**CLI 모드:** `memctl list`, `memctl auth`, `memctl init` 등의 명령을 실행한다. 설정은 `loadConfigForCwd()`로 해석된다.

**MCP 서버 모드:** 인자 없이 실행하면 MCP 서버가 시작된다. 이 모드에서는 다음을 요구한다:
- `MEMCTL_TOKEN` (또는 `~/.memctl/config.json`의 default 프로파일)
- `MEMCTL_ORG`
- `MEMCTL_PROJECT`

MCP 서버는 다음을 등록한다:
- **11개 도구** (memory, memory-advanced, memory-lifecycle, context, context-config, branch, session, import-export, repo, org, activity)
- **7개 리소스** (`memory://project/{slug}`, `memory://project/{slug}/{key}`, `memory://capacity`, `agent://functionalities`, `agent://functionalities/{type}`, `agent://branch/current`, `agent://bootstrap`, `session://handoff`, `memctl://connection-status`)
- **3개 프롬프트** (`agent-startup`, `context-for-files`, `session-handoff`)

### 4.2 `apps/web` -- Next.js App Router

| 파일 | 역할 |
|------|------|
| `app/layout.tsx` | 루트 레이아웃 (GeistSans/Mono 폰트, ThemeProvider, Toaster) |
| `app/page.tsx` | 랜딩 페이지 |
| `middleware.ts` | Beta gate (HTTP Basic Auth), API 경로 제외 |
| `instrumentation.ts` | Node.js 런타임에서 cron 스케줄러 초기화 |
| `next.config.ts` | standalone output, Fumadocs MDX 통합, GSAP transpile |
| `app/api/v1/*/route.ts` | REST API 엔드포인트 (Route Handlers) |

`next.config.ts`의 주요 설정:

```typescript
const nextConfig: NextConfig = {
  reactStrictMode: true,
  output: "standalone",     // Docker 배포를 위한 독립 실행형 출력
  compress: true,
  transpilePackages: ["gsap", "@gsap/react"],
};
```

### 4.3 `packages/db` -- 다중 export 진입점

```json
{
  "exports": {
    ".": "./src/index.ts",
    "./schema": "./src/schema.ts",
    "./client": "./src/client.ts"
  }
}
```

- `@memctl/db` -- 스키마 + `createDb` 함수 re-export
- `@memctl/db/schema` -- 스키마 테이블 정의만 직접 import
- `@memctl/db/client` -- `createDb()` 함수 + `Database` 타입

### 4.4 `packages/shared` -- 다중 export 진입점

```json
{
  "exports": {
    ".": "./src/index.ts",
    "./types": "./src/types.ts",
    "./constants": "./src/constants.ts",
    "./validators": "./src/validators.ts",
    "./relevance": "./src/relevance.ts",
    "./intent": "./src/intent.ts"
  }
}
```

각 서브 경로로 세분화된 import가 가능하다. `index.ts`는 모든 모듈을 re-export한다.

### 4.5 `plugins/memctl` -- Claude Code 플러그인 진입점

`plugin.json`:

```json
{
  "name": "memctl",
  "version": "0.1.0",
  "description": "Auto-capture high-signal coding context into memctl memory",
  "hooks": "./hooks/hooks.json"
}
```

플러그인은 `memctl-hook-dispatch.sh`를 통해 `memctl hook` CLI 명령을 호출한다. 세션 ID는 `.memctl/hooks/session_id` 파일에 저장되어 턴 간에 유지된다.

---

## 5. 패키지 의존 관계

### 5.1 의존 관계 다이어그램

```
+--------------------+
|    apps/web        |
| (@memctl/web)      |
+--------+-----------+
         |
         | workspace:*          workspace:*
         v                      v
+----------------+     +------------------+
| packages/db    |     | packages/shared  |
| (@memctl/db)   |     | (@memctl/shared) |
+----------------+     +------------------+

+--------------------+
| packages/cli       |         (npm 독립 패키지 -- 워크스페이스 의존 없음)
| (memctl)           |
+--------------------+

+--------------------+
| plugins/memctl     |         (워크스페이스 외부 -- memctl CLI에만 런타임 의존)
+--------------------+
```

### 5.2 상세 의존 관계 표

| 소비자 | 의존 대상 | 의존 방식 | 참조 내용 |
|--------|----------|----------|----------|
| `apps/web` | `@memctl/db` | `workspace:*` | 스키마 테이블 정의, `createDb()` 함수 |
| `apps/web` | `@memctl/shared` | `workspace:*` | 타입 인터페이스, 요금제 상수, Zod 검증 스키마 |
| `packages/cli` | -- | 없음 | 독립 패키지. API를 통해 `apps/web`과 통신 |
| `packages/db` | -- | 없음 | `@libsql/client`, `drizzle-orm`만 사용 |
| `packages/shared` | -- | 없음 | `zod`만 사용 |
| `plugins/memctl` | `packages/cli` | 런타임 (CLI 바이너리) | `memctl hook` 명령 호출 |

**핵심 설계 결정:** `packages/cli`는 `@memctl/db`나 `@memctl/shared`에 의존하지 않는다. CLI/MCP 서버는 REST API(`apps/web`의 `/api/v1/*`)를 통해서만 서버와 통신한다. 이로써 CLI는 독립적으로 npm에 배포할 수 있고, 서버 코드 변경에 영향받지 않는다.

### 5.3 런타임 통신 흐름

```
AI 에이전트 (Claude, Cursor 등)
    │
    │ MCP protocol (stdio)
    v
packages/cli (MCP 서버)
    │
    │ HTTP REST API (Bearer JWT / API Token)
    v
apps/web (/api/v1/*)
    │
    │ Drizzle ORM
    v
Turso / libSQL (SQLite)
```

```
Claude Code Plugin (hooks)
    │
    │ Shell exec (memctl hook ...)
    v
packages/cli (CLI 모드)
    │
    │ HTTP REST API
    v
apps/web (/api/v1/*)
```

---

## 6. 환경 구성

### 6.1 환경 변수 전체 목록

`.env.example` 기반으로 정리한다.

#### 데이터베이스

| 변수 | 필수 | 기본값 | 설명 |
|------|------|--------|------|
| `TURSO_DATABASE_URL` | Y | -- | Turso/libSQL 서버 URL. 로컬: `http://localhost:8080` |
| `TURSO_AUTH_TOKEN` | N | -- | Turso auth 토큰. 로컬 libSQL에서는 불필요 |
| `DATABASE_URL` | N | -- | 대체 DB URL (`TURSO_DATABASE_URL` 우선) |

#### 인증

| 변수 | 필수 | 기본값 | 설명 |
|------|------|--------|------|
| `GITHUB_CLIENT_ID` | Y (프로덕션) | -- | GitHub OAuth App ID |
| `GITHUB_CLIENT_SECRET` | Y (프로덕션) | -- | GitHub OAuth App Secret |
| `BETTER_AUTH_SECRET` | Y | -- | JWT 서명 시크릿 |
| `BETTER_AUTH_URL` | Y | `http://localhost:3000` | Auth 콜백 URL |

#### 개발 인증 바이패스

| 변수 | 필수 | 기본값 | 설명 |
|------|------|--------|------|
| `DEV_AUTH_BYPASS` | N | `false` | 개발/self-hosted에서 OAuth 없이 인증 우회 |
| `NEXT_PUBLIC_DEV_AUTH_BYPASS` | N | `false` | 클라이언트 사이드 동일 설정 |
| `DEV_AUTH_BYPASS_ORG_SLUG` | N | `dev-org` | 바이패스 시 사용할 조직 slug |
| `DEV_AUTH_BYPASS_USER_EMAIL` | N | `dev@local.memctl.test` | 바이패스 사용자 이메일 |
| `DEV_AUTH_BYPASS_USER_NAME` | N | `Dev User` | 바이패스 사용자 이름 |
| `DEV_AUTH_BYPASS_ADMIN` | N | `false` | 바이패스 사용자 관리자 권한 |

#### Self-hosted 모드

| 변수 | 필수 | 기본값 | 설명 |
|------|------|--------|------|
| `SELF_HOSTED` | N | `false` | 결제 비활성화, 모든 제한 해제 |
| `NEXT_PUBLIC_SELF_HOSTED` | N | `false` | 클라이언트 사이드 동일 설정 |

#### 결제 (Stripe)

| 변수 | 필수 | 기본값 | 설명 |
|------|------|--------|------|
| `STRIPE_SECRET_KEY` | N | -- | Stripe 비밀 키 |
| `STRIPE_PUBLISHABLE_KEY` | N | -- | Stripe 공개 키 |
| `STRIPE_WEBHOOK_SECRET` | N | -- | Webhook 서명 검증 시크릿 |
| `STRIPE_LITE_PRICE_ID` | N | -- | Lite 플랜 Stripe Price ID |
| `STRIPE_PRO_PRICE_ID` | N | -- | Pro 플랜 Stripe Price ID |
| `STRIPE_BUSINESS_PRICE_ID` | N | -- | Business 플랜 Stripe Price ID |
| `STRIPE_SCALE_PRICE_ID` | N | -- | Scale 플랜 Stripe Price ID |
| `STRIPE_EXTRA_SEAT_PRICE_ID` | N | -- | 추가 좌석 Stripe Price ID |

#### MCP / CLI

| 변수 | 필수 | 기본값 | 설명 |
|------|------|--------|------|
| `MEMCTL_API_URL` | N | `https://memctl.com/api/v1` | API 기본 URL |
| `MEMCTL_TOKEN` | N | -- | API 토큰 (또는 `memctl auth`로 저장) |
| `MEMCTL_ORG` | Y (MCP) | -- | 조직 slug |
| `MEMCTL_PROJECT` | Y (MCP) | -- | 프로젝트 slug |

#### 기타

| 변수 | 필수 | 기본값 | 설명 |
|------|------|--------|------|
| `CRON_SECRET` | N | -- | `/api/cron/*` 엔드포인트 인증 |
| `GITHUB_TOKEN` | N | -- | 랜딩 페이지 GitHub 통계용 (선택) |
| `RESEND_API_KEY` | N | -- | 이메일 발송 (없으면 콘솔 로깅) |
| `NEXT_PUBLIC_APP_URL` | N | `http://localhost:3000` | 앱 공개 URL |
| `DEV_PLAN` | N | -- | 개발 환경 요금제 오버라이드 (예: `enterprise`) |
| `BETA_GATE_ENABLED` | N | `false` | 비공개 베타 HTTP Basic Auth |
| `BETA_GATE_HOSTS` | N | -- | 보호할 호스트 (쉼표 구분) |
| `BETA_GATE_USERNAME` | N | `beta` | Basic Auth 사용자명 |
| `BETA_GATE_PASSWORD` | N | -- | Basic Auth 비밀번호 |

### 6.2 CLI 설정 파일 구조

`memctl auth`와 `memctl init`로 생성되는 설정 파일:

**`~/.memctl/config.json`** (전역 프로파일):

```json
{
  "profiles": {
    "default": {
      "token": "eyJ...",
      "apiUrl": "https://memctl.com/api/v1"
    }
  }
}
```

**프로젝트별 MCP 설정** (`.mcp.json` 또는 `.claude/mcp.json` 또는 `.cursor/mcp.json`):

```json
{
  "mcpServers": {
    "memctl": {
      "command": "npx",
      "args": ["-y", "memctl@latest"],
      "env": {
        "MEMCTL_ORG": "my-org",
        "MEMCTL_PROJECT": "my-project"
      }
    }
  }
}
```

**설정 해석 우선순위** (`loadConfigForCwd()`):

1. 환경변수 (`MEMCTL_TOKEN` + `MEMCTL_ORG` + `MEMCTL_PROJECT`) -- 모두 있으면 직접 사용
2. MCP 설정 파일 (`.mcp.json` 등)의 `env` + 전역 프로파일 토큰 조합
3. 환경변수 `MEMCTL_ORG`/`MEMCTL_PROJECT` + 전역 프로파일 토큰

### 6.3 Docker 개발 환경

`docker-compose.yml`은 세 개의 서비스를 정의한다:

```
┌─────────────────────────────────────────────────┐
│              docker-compose.yml                  │
│                                                  │
│  ┌──────────────┐    ┌─────────────────────┐    │
│  │   libsql     │    │       web            │    │
│  │  :8080 :5001 │<───│  :3000               │    │
│  │  (healthcheck│    │  (Dockerfile.dev)     │    │
│  │   포함)       │    │  bind-mount: .:/app   │    │
│  └──────────────┘    │  hot reload           │    │
│                      └─────────────────────┘    │
│                                                  │
│  ┌──────────────────────────────┐  [tools 프로파일]│
│  │   stripe-cli                 │               │
│  │  -> http://web:3000/         │               │
│  │     api/stripe/webhook       │               │
│  └──────────────────────────────┘               │
└─────────────────────────────────────────────────┘
```

**서비스 상세:**

| 서비스 | 이미지 | 포트 | 볼륨 | 설명 |
|--------|--------|------|------|------|
| `libsql` | `ghcr.io/tursodatabase/libsql-server:latest` | `8080`, `5001` | `libsql-data` (named) | libSQL 서버 (healthcheck 포함) |
| `web` | `Dockerfile.dev` (node:20-alpine) | `3000` | bind mount (소스) + named (node\_modules, pnpm\_store, .next) | Next.js dev 서버, 파일 시스템 폴링 |
| `stripe-cli` | `stripe/stripe-cli:latest` | -- | -- | Stripe webhook 포워딩 (`tools` 프로파일) |

**개발 환경 Docker 특이사항:**
- `web` 서비스는 `ulimits.nofile`을 65536으로 설정 (파일 감시용)
- `WATCHPACK_POLLING=true`, `CHOKIDAR_USEPOLLING=true` -- Docker 볼륨에서 파일 변경 감지
- `stripe-cli`는 `profiles: [tools]`로 선택적 실행: `docker compose --profile tools up`

### 6.4 Docker 프로덕션 환경

`docker-compose.prod.yml`은 두 개의 서비스만 사용한다:

| 서비스 | 이미지 | 설명 |
|--------|--------|------|
| `libsql` | `ghcr.io/tursodatabase/libsql-server:latest` | 프로덕션 DB |
| `web` | `Dockerfile` (멀티 스테이지 빌드) | Next.js standalone 서버 |

**프로덕션 Dockerfile 스테이지:**

```
Stage 1: base     -- node:20-bookworm-slim + pnpm 활성화
Stage 2: deps     -- pnpm install --frozen-lockfile (package.json만 복사)
Stage 3: builder  -- 소스 복사 + pnpm turbo build --filter=@memctl/web
Stage 4: runner   -- node:20-bookworm-slim + standalone 출력물만 복사
                     nextjs 사용자로 실행, 포트 3000
```

빌드 시 `NEXT_PUBLIC_APP_URL`을 `ARG`로 받아 Next.js에 인라인한다. 나머지 환경변수는 placeholder 값을 사용한다 (빌드 타임에만 필요).

### 6.5 요금제 구조

`packages/shared/src/constants.ts`에 정의된 요금제:

| 플랜 | 월 가격($) | 프로젝트 | 멤버 | 프로젝트당 메모리 | 분당 API |
|------|-----------|---------|------|------------------|---------|
| free | 0 | 3 | 1 | 400 | 60 |
| lite | 5 | 10 | 3 | 1,200 | 100 |
| pro | 18 | 25 | 10 | 5,000 | 150 |
| business | 59 | 100 | 30 | 10,000 | 150 |
| scale | 149 | 150 | 100 | 25,000 | 150 |
| enterprise | 커스텀 | 무제한 | 무제한 | 무제한 | 150 |

추가 좌석은 월 $8이다. Self-hosted 모드에서는 모든 제한이 해제된다.

---

## 7. CI/CD

### 7.1 GitHub Actions 워크플로우

두 개의 워크플로우 파일이 존재한다.

#### 7.1.1 CI (`ci.yml`)

**트리거:** `push`/`pull_request` on `main`

```
                          ┌────────┐
                     ┌───>│  Lint  │───┐
                     │    └────────┘   │
push/PR to main ────>┤                  ├──> Build
                     │  ┌────────────┐ │
                     ├─>│ Typecheck  │─┤
                     │  └────────────┘ │
                     │    ┌──────┐     │
                     └───>│ Test │─────┘
                          └──────┘
```

| 작업 | 내용 | 의존성 |
|------|------|--------|
| `lint` | `pnpm lint` (Turborepo 경유, 모든 패키지) | -- |
| `typecheck` | `pnpm typecheck` | -- |
| `test` | `npx vitest run` | -- |
| `build` | `pnpm build` (Turborepo 경유, 모든 패키지) | lint, typecheck, test 성공 후 |

모든 작업은 `ubuntu-latest`, Node.js 20, pnpm 캐시를 사용한다.

#### 7.1.2 Publish (`publish.yml`)

**트리거:**
- `workflow_dispatch` (수동)
- `push` on `main` (경로: `packages/cli/**` 또는 `.github/workflows/publish.yml`)

```
push to main (packages/cli/**)
    │
    v
┌──────────────────┐     ┌───────────────────────┐
│  check-version   │────>│  publish               │
│  (npm 버전 비교)  │     │  (npm publish + GitHub │
│                  │     │   Release 생성)         │
└──────────────────┘     └───────────────────────┘
```

| 작업 | 내용 | 조건 |
|------|------|------|
| `check-version` | `packages/cli/package.json`의 버전을 npm 레지스트리와 비교 | -- |
| `publish` | `pnpm --filter memctl build` -> `npm publish --access public --provenance` -> `gh release create` | 버전이 변경된 경우에만 실행 |

**npm 배포 특이사항:**
- **OIDC Trusted Publishing** 사용 (npm 토큰 없이 GitHub Actions에서 직접 인증)
- `--provenance` 플래그로 npm provenance 메타데이터 포함
- npm >= 11.5.1이 필요하며, 낮은 버전이면 `npm@11.6.2`를 설치한다
- TOKEN 기반 인증이 감지되면 빌드를 중단한다 (trusted publishing과 충돌 방지)
- 배포 후 `gh release create "v$VERSION"` 으로 GitHub Release 자동 생성
- Node.js 22 사용 (빌드/배포 작업만)

---

## 부록

### A. 데이터베이스 스키마 요약

`packages/db/src/schema.ts`에 정의된 총 20개 테이블:

| 테이블 | 용도 | 주요 관계 |
|--------|------|----------|
| `users` | 사용자 계정 | -- |
| `sessions` | better-auth 세션 | -> `users` |
| `accounts` | OAuth 계정 (GitHub 등) | -> `users` |
| `verifications` | 인증 코드 (Magic Link 등) | -- |
| `organizations` | 조직 | -> `users` (owner), -> `planTemplates` |
| `organizationMembers` | 조직 멤버십 | -> `organizations`, -> `users` |
| `orgInvitations` | 조직 초대 | -> `organizations`, -> `users` |
| `orgMemoryDefaults` | 조직 기본 메모리 (새 프로젝트에 자동 적용) | -> `organizations` |
| `projects` | 프로젝트 | -> `organizations` |
| `projectMembers` | 프로젝트 멤버 할당 | -> `projects`, -> `users` |
| `projectTemplates` | 프로젝트 템플릿 (메모리 프리셋) | -> `organizations` |
| `memories` | 메모리 (핵심 엔티티) | -> `projects`, -> `users` |
| `memoryVersions` | 메모리 버전 이력 | -> `memories` (cascade delete) |
| `memorySnapshots` | 프로젝트 메모리 스냅샷 | -> `projects` |
| `memoryLocks` | 메모리 동시 편집 잠금 | -> `projects` |
| `contextTypes` | 사용자 정의 컨텍스트 타입 | -> `organizations` |
| `sessionLogs` | 에이전트 세션 기록 | -> `projects` |
| `activityLogs` | 활동 로그 (도구 호출, 읽기/쓰기) | -> `projects` |
| `apiTokens` | API 토큰 (SHA-256 해시 저장) | -> `users`, -> `organizations` |
| `onboardingResponses` | 온보딩 설문 응답 | -> `users` |
| `planTemplates` | 관리자용 요금제 템플릿 | -- |
| `promoCodes` | 프로모션 코드 | -> `users` |
| `promoRedemptions` | 프로모 코드 사용 기록 | -> `promoCodes`, -> `organizations`, -> `users` |
| `auditLogs` | 감사 로그 | -> `organizations`, -> `projects`, -> `users` |
| `adminActions` | 관리자 행동 기록 | -> `organizations`, -> `users` |
| `changelogEntries` | 변경 로그 항목 | -> `users` |
| `changelogItems` | 변경 로그 세부 항목 | -> `changelogEntries` (cascade delete) |
| `blogPosts` | 블로그 게시물 | -> `users` |

**`memories` 테이블 인덱스:**
- `UNIQUE(project_id, key)` -- 프로젝트 내 키 고유성
- `memories_project_updated(project_id, updated_at)` -- 최근 변경 조회
- `memories_project_archived(project_id, archived_at)` -- 아카이브 필터
- `memories_project_priority(project_id, priority)` -- 우선순위 정렬
- `memories_project_created(project_id, created_at)` -- 생성 순 정렬

### B. MCP 서버 도구 목록

`packages/cli/src/tools/` 디렉터리에 11개의 도구 핸들러가 등록된다:

| 도구 파일 | 도구 이름 | 주요 action |
|----------|----------|------------|
| `memory.ts` | `memory` | `store`, `get`, `search`, `delete`, `update`, `list` |
| `memory-advanced.ts` | `memory_advanced` | `link`, `unlink`, `similar`, `diff`, `org_diff`, `feedback`, `lock`, `unlock`, `co_accessed`, `watch`, `validate` |
| `memory-lifecycle.ts` | `memory_lifecycle` | `pin`, `unpin`, `archive`, `unarchive`, `set_expiry`, `snapshot`, `list_snapshots`, `rollback`, `cleanup_expired`, `suggest_cleanup`, `run_lifecycle`, `capacity` |
| `context.ts` | `context` | `bootstrap`, `bootstrap_compact`, `smart_retrieve`, `context_for`, `functionality_get`, `functionality_set`, `functionality_list`, `functionality_delete` |
| `context-config.ts` | `context_config` | `list_types`, `create_type`, `update_type`, `delete_type` |
| `branch.ts` | `branch` | `get`, `plan_get`, `plan_set`, `diff` |
| `session.ts` | `session` | `start`, `end`, `handoff_get`, `handoff_set`, `list` |
| `import-export.ts` | `import_export` | `export_agents_md`, `export_cursorrules`, `export_json`, `import` |
| `repo.ts` | `repo` | `get_project`, `update_project`, `list_org_projects` |
| `org.ts` | `org` | `defaults_get`, `defaults_set`, `defaults_delete`, `defaults_list`, `defaults_apply`, `search_org` |
| `activity.ts` | `activity` | `memo_read`, `memo_leave`, `log`, `recent`, `session_activity` |

### C. 관련도 스코어링 알고리즘

`packages/shared/src/relevance.ts`에 구현된 관련도 점수 계산:

```
score = basePriority * usageFactor * timeFactor * feedbackFactor * pinBoost * 100

basePriority   = max(priority, 1) / 100
usageFactor    = 1 + log(1 + accessCount)
timeFactor     = exp(-0.03 * daysSinceAccess)     -- 반감기 약 23일
feedbackFactor = 0.5 + (helpfulCount / totalFeedback)  -- [0.5, 1.5]
pinBoost       = 1.5 (고정 시) / 1.0 (일반)
```

점수 구간: excellent (>= 60), good (>= 30), fair (>= 10), poor (< 10).

### D. 검색 의도 분류

`packages/shared/src/intent.ts`에 구현된 5가지 검색 의도:

| 의도 | 신뢰도 | 패턴 | FTS 부스트 | 벡터 부스트 | 최근성 부스트 |
|------|--------|------|-----------|-----------|-------------|
| `entity` | 0.6~0.9 | 파일 경로, 식별자, 짧은 비질문 쿼리 | 2.0 | 0.5 | 0.3 |
| `temporal` | 0.85 | recent, latest, changed, updated 등 | 0.7 | 0.5 | 3.0 |
| `relationship` | 0.8 | related to, depends on, linked 등 | 0.5 | 1.5 | 1.0 |
| `aspect` | 0.75 | conventions, rules, patterns, how to 등 | 1.0 | 1.5 | 0.5 |
| `exploratory` | 0.5 | (기본 fallback) | 1.0 | 1.2 | 1.0 |
