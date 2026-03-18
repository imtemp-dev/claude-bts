# ContextStream — 아키텍처 분석

> 소스 위치: `~/Workspace/context-sync-research/contextstream/`
> 패키지: `@contextstream/mcp-server` v0.4.64
> 저장소: `https://github.com/contextstream/mcp-server`
> 라이선스: MIT

---

## 1. 프로젝트 구조

단일 패키지(single-package) 구조로, monorepo가 아니다. `src/` 아래에 모든 소스가 위치하며, `src/hooks/` 하위 디렉터리에 에디터 hook 스크립트가 개별 파일로 분리되어 있다.

```
contextstream/
├── package.json                # 패키지 메타, 의존성, bin 진입점, 빌드 스크립트
├── tsconfig.json               # TypeScript 설정 (ES2020, ESNext, strict)
├── vitest.config.cjs           # Vitest 테스트 설정 (CJS 형식)
├── eslint.config.js            # ESLint flat config (typescript-eslint)
├── Dockerfile                  # Docker 이미지 (node:20-alpine, test-server)
├── server.json                 # MCP Server Manifest (JSON Schema 2025-12-11)
├── smithery.yaml               # Smithery 배포 설정
├── scripts/
│   └── postinstall.js          # npm postinstall — Claude Code hook 자동 설정
├── .github/
│   └── workflows/
│       └── release.yml         # CI/CD: 크로스 플랫폼 바이너리 빌드 + 체크섬
├── src/
│   ├── index.ts          (454L)   # 진입점 — CLI 명령 라우팅, MCP 서버 초기화
│   ├── tools.ts        (15460L)   # MCP 도구 등록 — 전체 tool handler 구현
│   ├── client.ts        (6673L)   # API 클라이언트 — ContextStream API 호출 래퍼
│   ├── session-manager.ts (865L)  # 세션 상태 관리 — auto-context, 토큰 추적, 체크포인트
│   ├── setup.ts         (2074L)   # 설정 마법사 — 에디터별 MCP/규칙 설치 CLI
│   ├── hooks-config.ts  (2169L)   # Hook 설정 빌더 — Claude Code/Cursor/Cline 등 hook 구성
│   ├── rules-templates.ts(1365L)  # 에디터별 규칙 템플릿 생성 (bootstrap/dynamic/full)
│   ├── files.ts          (902L)   # 파일 읽기/해싱 — 코드 인덱싱용 파일 수집
│   ├── prompts.ts        (742L)   # MCP Prompts 등록 — 22개 prompt 템플릿
│   ├── resources.ts      (119L)   # MCP Resources 등록 — OpenAPI, workspaces, projects
│   ├── config.ts          (76L)   # 설정 스키마 (Zod) 및 환경변수 로딩
│   ├── credentials.ts    (102L)   # 자격 증명 파일 관리 (~/.contextstream/credentials.json)
│   ├── http-gateway.ts          # Streamable HTTP transport — MCP HTTP 게이트웨이
│   ├── http.ts                  # HTTP 요청 유틸 — 재시도, 에러 처리, rate limiting
│   ├── workspace-config.ts(161L)  # 워크스페이스 설정 — .contextstream/config.json, 글로벌 매핑
│   ├── version.ts        (593L)  # 버전 관리 — npm 최신 버전 확인, 자동 업데이트
│   ├── cache.ts                 # 인메모리 TTL 캐시 — workspace/project 정보 캐싱
│   ├── auth-context.ts   (21L)   # AsyncLocalStorage 기반 인증 오버라이드 (HTTP 게이트웨이용)
│   ├── ignore.ts                # .contextstream/ignore — gitignore 스타일 패턴 매칭 (ignore 라이브러리)
│   ├── tool-catalog.ts          # 도구 카탈로그 — AI용 초경량 도구 레퍼런스 (~120 토큰)
│   ├── token-savings.ts         # 토큰 절약 추적 — 도구별 candidate/context 토큰 비율 측정
│   ├── microcopy.ts             # 교육적 마이크로카피 — 도구 응답에 포함되는 힌트 메시지
│   ├── educational-microcopy.ts # 세션 시작 팁, 캡처 힌트 등 회전 표시 메시지
│   ├── project-index-utils.ts   # 인덱스 상태 판별 유틸 (fresh/stale/missing)
│   ├── todo-utils.ts            # TODO 완료 상태 정규화 유틸
│   ├── verify-key.ts            # API 키 검증 명령 구현
│   ├── test-server.ts           # 테스트용 HTTP 서버 — MCP 도구를 HTTP로 래핑
│   ├── types/
│   │   └── mcp-sdk.d.ts         # MCP SDK 타입 확장
│   ├── hooks/                   # 에디터 Hook 스크립트 (27개 파일)
│   │   ├── runner.ts      (57L)   # Hook 라우터 — 단일 진입점에서 hook 이름으로 분기
│   │   ├── common.ts     (309L)   # 공통 유틸 — config 로딩, stdin/stdout, API 호출
│   │   ├── prompt-state.ts(207L)  # Hook 간 상태 공유 — context 요구 플래그 관리
│   │   ├── session-init.ts(370L)  # SessionStart — 세션 시작 시 전체 컨텍스트 주입
│   │   ├── session-end.ts (415L)  # SessionEnd/Stop — 세션 종료 시 트랜스크립트 저장
│   │   ├── stop.ts         (41L)  # Stop — 응답 완료 시 체크포인트 저장
│   │   ├── user-prompt-submit.ts(869L) # UserPromptSubmit — 매 메시지마다 규칙 리마인더 주입
│   │   ├── pre-tool-use.ts(570L)  # PreToolUse — Glob/Grep/Search 차단, ContextStream 리다이렉트
│   │   ├── post-write.ts  (482L)  # PostToolUse — Edit/Write 후 실시간 파일 인덱싱
│   │   ├── pre-compact.ts (451L)  # PreCompact — 컨텍스트 압축 전 세션 스냅샷 저장
│   │   ├── post-compact.ts(267L)  # PostCompact — 압축 후 컨텍스트 복원
│   │   ├── post-tool-use-failure.ts(98L) # PostToolUseFailure — 반복 도구 실패 캡처
│   │   ├── notification.ts (32L)  # Notification — 런타임 알림 캡처
│   │   ├── permission-request.ts(46L)  # PermissionRequest — 권한 에스컬레이션 캡처
│   │   ├── subagent-start.ts(73L) # SubagentStart — 하위 에이전트에 컨텍스트 주입
│   │   ├── subagent-stop.ts(168L) # SubagentStop — 하위 에이전트 결과 캡처
│   │   ├── task-completed.ts(108L)# TaskCompleted — 작업 완료 상태 업데이트
│   │   ├── teammate-idle.ts (59L) # TeammateIdle — 유휴 팀메이트를 대기 작업으로 리다이렉트
│   │   ├── on-save-intent.ts(178L)# UserPromptSubmit — 문서 저장 의도를 ContextStream으로 리다이렉트
│   │   ├── media-aware.ts (144L)  # Legacy — 미디어 관련 프롬프트 감지 (현재 noop)
│   │   ├── auto-rules.ts  (337L)  # Legacy — 자동 규칙 생성 (현재 noop)
│   │   ├── on-bash.ts     (275L)  # Legacy noop
│   │   ├── on-task.ts     (218L)  # Legacy noop
│   │   ├── on-read.ts     (245L)  # Legacy noop
│   │   ├── on-web.ts      (233L)  # Legacy noop
│   │   ├── noop.ts          (7L)  # noop 핸들러 — exit 0
│   │   └── prompt-state.test.ts(97L) # prompt-state 단위 테스트
│   ├── *.test.ts                # 단위 테스트 파일들 (cache, client, files, hooks-config, etc.)
```

전체 소스 행 수는 약 42,486행이다. 가장 큰 파일은 `tools.ts` (15,460행)로 모든 MCP 도구의 핸들러 로직이 단일 파일에 집중되어 있으며, 그 다음이 `client.ts` (6,673행)로 ContextStream API에 대한 HTTP 래퍼를 포함한다.

---

## 2. 테크 스택

### 2.1 런타임 요구사항

| 항목 | 버전 |
|------|------|
| Node.js | >= 18 (`engines.node`) |
| 패키지 타입 | ESM (`"type": "module"`) |

### 2.2 Production 의존성

```json
{
  "@modelcontextprotocol/sdk": "^1.25.1",
  "ignore": "^7.0.5",
  "zod": "^3.23.8"
}
```

- **@modelcontextprotocol/sdk** (^1.25.1): MCP 프로토콜 구현체. `McpServer`, `StdioServerTransport`, `StreamableHTTPServerTransport`, `ResourceTemplate` 등 핵심 클래스를 제공한다. 빌드 시 `--external` 처리되어 번들에 포함되지 않는다.
- **zod** (^3.23.8): 설정 스키마 검증(`configSchema`)에 사용된다. 또한 `tools.ts`에서 tool input schema 정의 시 MCP SDK와 함께 사용된다.
- **ignore** (^7.0.5): `.contextstream/ignore` 파일의 gitignore 스타일 패턴 매칭에 사용된다. 파일 인덱싱 시 제외 경로 판별에 핵심 역할을 한다.

의존성이 단 3개로 극도로 경량화되어 있다. 이는 의도적인 설계로, MCP 서버는 에디터 프로세스 내에서 실행되므로 설치 속도와 메모리 사용량이 중요하다.

### 2.3 Dev 의존성

| 패키지 | 버전 | 용도 |
|--------|------|------|
| `typescript` | ^5.6.3 | 타입 체크 (`tsc --noEmit`) |
| `esbuild` | ^0.27.0 | 번들링 (main + hooks + test-server) |
| `vitest` | ^4.0.16 | 테스트 러너 |
| `tsx` | ^4.15.4 | 개발 시 TypeScript 직접 실행 (`npm run dev`) |
| `eslint` | ^9.39.2 | Linting (flat config) |
| `@eslint/js` | ^9.39.2 | ESLint 기본 추천 설정 |
| `typescript-eslint` | ^8.52.0 | TypeScript ESLint 통합 |
| `@typescript-eslint/eslint-plugin` | ^8.52.0 | TS-specific lint 규칙 |
| `@typescript-eslint/parser` | ^8.52.0 | TypeScript 파서 |
| `prettier` | ^3.7.4 | 코드 포맷팅 |
| `@types/node` | ^20.10.0 | Node.js 타입 정의 |

### 2.4 Overrides

```json
{ "qs": "6.14.1" }
```

`qs` 라이브러리의 보안 취약점 대응을 위한 버전 고정이다.

---

## 3. 빌드 시스템

### 3.1 esbuild 번들링

빌드 스크립트(`npm run build`)는 단일 `&&` 체인으로 25개의 esbuild 명령을 순차 실행한다. 모든 번들은 동일한 옵션을 공유한다:

```
--bundle --platform=node --target=node18 --format=esm
--external:@modelcontextprotocol/sdk
--banner:js="#!/usr/bin/env node"
```

| 옵션 | 설명 |
|------|------|
| `--bundle` | 모든 로컬 import를 단일 파일로 번들링 |
| `--platform=node` | Node.js 내장 모듈 외부 처리 |
| `--target=node18` | Node 18 호환 JavaScript 생성 |
| `--format=esm` | ESM 출력 (`import`/`export` 구문 유지) |
| `--external:@modelcontextprotocol/sdk` | MCP SDK를 번들에서 제외, 런타임 resolve |
| `--banner:js="#!/usr/bin/env node"` | 실행 가능 스크립트 shebang 추가 |

### 3.2 번들 대상 파일

총 25개의 독립 번들이 생성된다:

**메인 번들:**
- `dist/index.js` -- MCP 서버 메인 진입점 (`src/index.ts`)
- `dist/test-server.js` -- HTTP 테스트 서버 (`src/test-server.ts`)

**Hook runner:**
- `dist/hooks/runner.js` -- Hook 통합 라우터 (`src/hooks/runner.ts`)

**개별 Hook 번들 (22개):**
- `dist/hooks/post-write.js`, `dist/hooks/pre-tool-use.js`, `dist/hooks/post-tool-use-failure.js`
- `dist/hooks/user-prompt-submit.js`, `dist/hooks/media-aware.js`
- `dist/hooks/pre-compact.js`, `dist/hooks/post-compact.js`
- `dist/hooks/notification.js`, `dist/hooks/permission-request.js`
- `dist/hooks/subagent-start.js`, `dist/hooks/subagent-stop.js`
- `dist/hooks/task-completed.js`, `dist/hooks/teammate-idle.js`
- `dist/hooks/stop.js`, `dist/hooks/auto-rules.js`
- `dist/hooks/on-bash.js`, `dist/hooks/on-task.js`, `dist/hooks/on-read.js`, `dist/hooks/on-web.js`
- `dist/hooks/session-init.js`, `dist/hooks/session-end.js`, `dist/hooks/on-save-intent.js`

각 hook이 개별 번들로 생성되는 이유는 hook이 에디터에 의해 독립적으로 실행되기 때문이다. 전체 MCP 서버를 로딩하는 오버헤드 없이 해당 hook의 코드만 빠르게 실행할 수 있다.

### 3.3 TypeScript 설정

```json
{
  "compilerOptions": {
    "target": "ES2020",
    "module": "ESNext",
    "moduleResolution": "Node",
    "outDir": "dist",
    "rootDir": "src",
    "esModuleInterop": true,
    "resolveJsonModule": true,
    "skipLibCheck": true,
    "strict": true
  },
  "include": ["src/**/*"]
}
```

`tsc`는 빌드에 사용되지 않는다. `npm run typecheck` (`tsc --noEmit`)으로 타입 체크만 수행하며, 실제 번들링은 esbuild가 담당한다.

### 3.4 npm 게시 설정

```json
{
  "main": "dist/index.js",
  "bin": {
    "mcp-server": "dist/index.js",
    "contextstream-mcp": "dist/index.js",
    "contextstream-hook": "dist/hooks/runner.js"
  },
  "files": ["dist", "dist/hooks", "scripts", "README.md"]
}
```

세 개의 CLI 바이너리가 등록된다:
- `mcp-server` / `contextstream-mcp`: MCP 서버 메인 실행
- `contextstream-hook`: Hook runner (에디터가 직접 호출)

`prepublishOnly` 스크립트가 게시 전 자동으로 `npm run build`를 실행한다.

### 3.5 postinstall 스크립트

`scripts/postinstall.js`는 npm 설치 후 자동 실행된다:

1. `~/.claude/.contextstream-version` 파일에 현재 버전을 기록한다.
2. `~/.claude/settings.json`이 존재하면, 기존 `npx @contextstream/mcp-server` 명령을 `node <설치경로>/dist/index.js`로 교체하여 hook 실행 속도를 최적화한다.

---

## 4. 진입점 (src/index.ts)

### 4.1 CLI 명령 라우팅

`src/index.ts`의 `main()` 함수는 `process.argv`를 파싱하여 다음과 같이 분기한다:

```
contextstream-mcp                     → MCP stdio 서버 (기본)
contextstream-mcp --help / -h         → 도움말 출력
contextstream-mcp --version / -v      → 버전 출력
contextstream-mcp setup               → 설정 마법사 (runSetupWizard)
contextstream-mcp http                → HTTP MCP 게이트웨이 (runHttpGateway)
contextstream-mcp hook <hook-name>    → Hook 실행 (switch-case 분기)
contextstream-mcp verify-key [--json] → API 키 검증
contextstream-mcp update-hooks [flags]→ 전체 에디터 Hook 업데이트
```

### 4.2 Hook 라우팅 상세

`hook` 서브커맨드는 hook 이름에 따라 `dynamic import()`로 해당 모듈을 로딩하고 실행한다. 여러 별칭(alias)이 존재한다:

| Hook 이름 | 실제 핸들러 | 설명 |
|-----------|------------|------|
| `post-tool-use` | `post-write.js` | 별칭 (PostToolUse -> PostWrite) |
| `post-write` | `post-write.js` | Edit/Write 후 실시간 파일 인덱싱 |
| `post-tool-use-failure` | `post-tool-use-failure.js` | 반복 도구 실패 캡처 |
| `notification` | `notification.js` | 런타임 알림 캡처 |
| `permission-request` | `permission-request.js` | 권한 에스컬레이션 캡처 |
| `subagent-start` | `subagent-start.js` | 하위 에이전트 컨텍스트 주입 |
| `subagent-stop` | `subagent-stop.js` | 하위 에이전트 결과 캡처 |
| `task-completed` | `task-completed.js` | 작업 완료 업데이트 |
| `teammate-idle` | `teammate-idle.js` | 유휴 팀메이트 리다이렉트 |
| `pre-tool-use` | `pre-tool-use.js` | 검색 도구 차단/리다이렉트 |
| `user-prompt-submit` | `user-prompt-submit.js` | 매 메시지 규칙 리마인더 주입 |
| `media-aware` | `noop.js` | Legacy noop |
| `pre-compact` | `pre-compact.js` | 컨텍스트 압축 전 스냅샷 |
| `auto-rules` | `noop.js` | Legacy noop |
| `post-compact` | `post-compact.js` | 압축 후 컨텍스트 복원 |
| `on-bash`, `on-task`, `on-read`, `on-web` | `noop.js` | Legacy noop |
| `session-start` / `session-init` | `session-init.js` | 세션 시작 시 컨텍스트 주입 |
| `stop` | `stop.js` | Stop 체크포인트 |
| `session-end` | `session-end.js` | 세션 종료, 트랜스크립트 저장 |
| `on-save-intent` | `on-save-intent.js` | 문서 저장 의도 리다이렉트 |
| (알 수 없는 hook) | | `process.exit(0)` — 오류 방지 |

알 수 없는 hook 이름에 대해서는 `process.exit(0)`으로 조용히 종료한다. 이는 구 버전 hook 설정이 남아있을 때 에디터에 "hook error"를 표시하지 않기 위한 의도적 설계이다.

### 4.3 MCP 서버 초기화 흐름

인자 없이 실행될 때의 초기화 순서:

1. **자격 증명 로딩**: 환경변수(`CONTEXTSTREAM_API_KEY`, `CONTEXTSTREAM_JWT`)가 없으면 `~/.contextstream/credentials.json`에서 저장된 자격 증명을 읽어 환경변수에 설정한다.

2. **설정 로딩** (`loadConfig()`): Zod 스키마로 환경변수를 검증하고 `Config` 객체를 생성한다. 자격 증명이 없으면 `isMissingCredentialsError`를 통해 감지하고 **제한 모드(limited mode)**로 전환한다.

3. **제한 모드**: 자격 증명 없이 실행 시 `registerLimitedTools()`만 등록하여 설정 안내 도구만 노출한다. `McpServer` + `StdioServerTransport`로 연결하되, 완전한 기능은 비활성화된다.

4. **정상 모드**:
   - `ContextStreamClient` 생성 (API 클라이언트)
   - `McpServer` 생성 (`name: "contextstream-mcp"`, `version: VERSION`)
   - `setupClientDetection(server)` -- 클라이언트 감지 콜백 설정 (MCP initialize 시 토큰 민감 클라이언트 감지)
   - `SessionManager` 생성 -- auto-context, 토큰 추적, 체크포인트 관리
   - `registerTools(server, client, sessionManager, { toolSurfaceProfile })` -- 도구 등록
   - `registerResources(server, client, config.apiUrl)` -- 리소스 등록
   - `registerPrompts(server)` -- 프롬프트 등록 (환경변수로 비활성화 가능)
   - `StdioServerTransport` 생성 및 `server.connect(transport)` 연결
   - 첫 실행 메시지 표시 (`showFirstRunMessage`) -- `~/.contextstream/.star-shown` 플래그
   - 백그라운드 업데이트 체크 (`checkForUpdates`)

---

## 5. 설정 시스템

### 5.1 Config 스키마 (src/config.ts)

Zod 기반 설정 스키마:

```typescript
const configSchema = z.object({
  apiUrl: z.string().url().default("https://api.contextstream.io"),
  apiKey: z.string().min(1).optional(),
  jwt: z.string().min(1).optional(),
  defaultWorkspaceId: z.string().uuid().optional(),
  defaultProjectId: z.string().uuid().optional(),
  userAgent: z.string().default(`contextstream-mcp/${VERSION}`),
  allowHeaderAuth: z.boolean().optional(),
  contextPackEnabled: z.boolean().default(true),
  showTiming: z.boolean().default(false),
  toolSurfaceProfile: z.enum(["default", "openai_agentic"]).default("default"),
});
```

인증 우선순위: `CONTEXTSTREAM_API_KEY` > `CONTEXTSTREAM_JWT` > `CONTEXTSTREAM_ALLOW_HEADER_AUTH`. 세 가지 모두 없으면 `MISSING_CREDENTIALS_ERROR`를 발생시킨다.

### 5.2 환경변수 전체 목록

**인증:**
| 환경변수 | 설명 |
|----------|------|
| `CONTEXTSTREAM_API_URL` | API 기본 URL (기본: `https://api.contextstream.io`) |
| `CONTEXTSTREAM_API_KEY` | API 키 인증 |
| `CONTEXTSTREAM_JWT` | JWT 인증 (API 키 대안) |
| `CONTEXTSTREAM_ALLOW_HEADER_AUTH` | Header 기반 인증 허용 (HTTP 게이트웨이용) |

**스코프:**
| 환경변수 | 설명 |
|----------|------|
| `CONTEXTSTREAM_WORKSPACE_ID` | 기본 워크스페이스 ID |
| `CONTEXTSTREAM_PROJECT_ID` | 기본 프로젝트 ID |

**도구 제어:**
| 환경변수 | 설명 | 기본값 |
|----------|------|--------|
| `CONTEXTSTREAM_TOOLSET` | 도구 모드: `light`/`standard`/`complete` | `standard` |
| `CONTEXTSTREAM_TOOL_SURFACE_PROFILE` | 도구 표면: `default`/`openai_agentic` | `default` |
| `CONTEXTSTREAM_TOOL_ALLOWLIST` | 쉼표 구분 도구 이름 (toolset 오버라이드) | - |
| `CONTEXTSTREAM_AUTO_TOOLSET` | 클라이언트 자동 감지 후 toolset 조정 | `false` |
| `CONTEXTSTREAM_AUTO_HIDE_INTEGRATIONS` | 미연결 통합 도구 자동 숨김 | `true` |
| `CONTEXTSTREAM_SCHEMA_MODE` | 스키마 상세도: `compact`/`full` | `full` |
| `CONTEXTSTREAM_PROGRESSIVE_MODE` | 점진적 공개: ~13개 핵심 도구로 시작 | `false` |
| `CONTEXTSTREAM_ROUTER_MODE` | 라우터 패턴: 2개 메타 도구만 노출 | `false` |
| `CONTEXTSTREAM_CONSOLIDATED` | 통합 도메인 도구 (v0.4.x, ~75% 토큰 절감) | `true` |
| `CONTEXTSTREAM_PRO_TOOLS` | PRO 도구 이름 (쉼표 구분) | AI 도구 |
| `CONTEXTSTREAM_UPGRADE_URL` | PRO 도구 업그레이드 URL | - |

**출력 제어:**
| 환경변수 | 설명 | 기본값 |
|----------|------|--------|
| `CONTEXTSTREAM_OUTPUT_FORMAT` | 출력 상세도: `compact`/`pretty` | `compact` |
| `CONTEXTSTREAM_INCLUDE_STRUCTURED_CONTENT` | 구조화된 JSON 페이로드 포함 | `true` |
| `CONTEXTSTREAM_SEARCH_LIMIT` | 기본 검색 결과 수 | `3` |
| `CONTEXTSTREAM_SEARCH_MAX_CHARS` | 검색 결과당 최대 문자 수 | `400` |
| `CONTEXTSTREAM_CONTEXT_PACK` | Context Pack 활성화 | `true` |
| `CONTEXTSTREAM_ENABLE_PROMPTS` | MCP Prompts 활성화 | `true` |
| `CONTEXTSTREAM_LOG_LEVEL` | 로깅 수준: `quiet`/`normal`/`verbose` | `normal` |
| `CONTEXTSTREAM_SHOW_TIMING` | API 호출 타이밍 표시 | `false` |
| `CONTEXTSTREAM_SEARCH_REMINDER` | 검색 규칙 리마인더 활성화 | `true` |

**HTTP 게이트웨이:**
| 환경변수 | 설명 | 기본값 |
|----------|------|--------|
| `MCP_HTTP_HOST` | 호스트 | `0.0.0.0` |
| `MCP_HTTP_PORT` | 포트 | `8787` |
| `MCP_HTTP_PATH` | 경로 | `/mcp` |
| `MCP_HTTP_REQUIRE_AUTH` | 인증 요구 | `true` |
| `MCP_HTTP_JSON_RESPONSE` | JSON 응답 활성화 | `false` |

**Hook 제어:**
| 환경변수 | 설명 |
|----------|------|
| `CONTEXTSTREAM_HOOK_ENABLED` | PreToolUse hook 활성화 |
| `CONTEXTSTREAM_REMINDER_ENABLED` | UserPromptSubmit hook 활성화 |
| `CONTEXTSTREAM_POSTWRITE_ENABLED` | PostWrite hook 활성화 |
| `CONTEXTSTREAM_PRECOMPACT_ENABLED` | PreCompact hook 활성화 |
| `CONTEXTSTREAM_POSTCOMPACT_ENABLED` | PostCompact hook 활성화 |
| `CONTEXTSTREAM_SESSION_INIT_ENABLED` | SessionInit hook 활성화 |
| `CONTEXTSTREAM_SESSION_END_ENABLED` | SessionEnd hook 활성화 |
| `CONTEXTSTREAM_STOP_ENABLED` | Stop hook 활성화 |
| `CONTEXTSTREAM_SUBAGENT_CONTEXT_ENABLED` | SubagentStart hook 활성화 |
| `CONTEXTSTREAM_AUTO_UPDATE` | 자동 업데이트 활성화 |
| `CONTEXTSTREAM_CHECKPOINT_ENABLED` | 연속 체크포인트 활성화 |

### 5.3 워크스페이스 설정 파일

세 가지 설정 파일이 계층적으로 사용된다:

**1. 로컬 프로젝트 설정: `.contextstream/config.json`**

프로젝트 루트의 `.contextstream/` 디렉터리에 위치한다. Hook이 실행될 때 cwd에서 상위 6단계까지 탐색하여 이 파일을 찾는다.

```typescript
interface WorkspaceConfig {
  workspace_id: string;
  workspace_name?: string;
  project_id?: string;
  project_name?: string;
  associated_at?: string;
  version?: string;
  configured_editors?: string[];
  context_pack?: boolean;
  api_url?: string;
  updated_at?: string;
  indexing_enabled?: boolean;
}
```

**2. 글로벌 자격 증명: `~/.contextstream/credentials.json`**

파일 권한은 `0o600`으로 설정된다 (소유자만 읽기/쓰기).

```typescript
interface SavedCredentialsV1 {
  version: 1;
  api_url: string;
  api_key: string;
  email?: string;
  created_at: string;
  updated_at: string;
}
```

**3. 글로벌 폴더 매핑: `~/.contextstream-mappings.json`**

상위 폴더와 워크스페이스 간 매핑을 저장한다. 새 프로젝트를 해당 상위 폴더 아래에 만들면 자동으로 올바른 워크스페이스에 연결된다.

```typescript
interface ParentMapping {
  pattern: string;      // e.g., "/home/user/dev/projects/*"
  workspace_id: string;
  workspace_name: string;
}
```

### 5.4 Hook 설정 로딩 (common.ts)

Hook은 자체적인 설정 로딩 로직을 가진다 (`loadHookConfig()`). MCP 서버 프로세스와 별개로 실행되므로, 다음 순서로 API 설정을 탐색한다:

1. 환경변수 (`CONTEXTSTREAM_API_KEY`, `CONTEXTSTREAM_API_URL` 등)
2. 프로젝트 `.mcp.json` (cwd에서 상위 6단계까지 탐색)
3. 프로젝트 `.contextstream/config.json` (workspace_id, project_id)
4. 홈 디렉터리 `~/.mcp.json`

### 5.5 기타 설정 파일

| 파일 | 위치 | 용도 |
|------|------|------|
| `~/.contextstream/.star-shown` | 홈 | 첫 실행 메시지 표시 여부 |
| `~/.contextstream/version-cache.json` | 홈 | npm 최신 버전 캐시 (TTL 12시간) |
| `~/.contextstream/indexed-projects.json` | 홈 | 인덱싱된 프로젝트 목록 (hook에서 참조) |
| `~/.contextstream/prompt-state.json` | 홈 | Hook 간 상태 공유 (context 요구 플래그) |
| `~/.claude/.contextstream-version` | 홈 | postinstall이 기록하는 설치 버전 |
| `.contextstream/ignore` | 프로젝트 | gitignore 스타일 인덱싱 제외 패턴 |

---

## 6. 배포 방식

### 6.1 npm 패키지 설치

가장 기본적인 설치 방식:

```bash
npx --prefer-online -y @contextstream/mcp-server@latest
```

또는 전역 설치:

```bash
npm install -g @contextstream/mcp-server
```

### 6.2 Docker

`Dockerfile`은 `node:20-alpine` 기반이며, 테스트 서버(`dist/test-server.js`)를 실행한다:

```dockerfile
FROM node:20-alpine
WORKDIR /app
COPY package*.json ./
RUN npm ci --production
COPY dist/ ./dist/
EXPOSE 3099
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:3099/health || exit 1
ENV NODE_ENV=production
ENV MCP_TEST_PORT=3099
CMD ["node", "dist/test-server.js"]
```

헬스 체크 엔드포인트: `GET /health` (포트 3099).

### 6.3 바이너리 빌드 (Bun compile)

CI/CD 파이프라인에서 Bun의 `--compile` 옵션으로 단일 실행 파일을 생성한다. Node.js 런타임이 필요하지 않은 자체 포함(self-contained) 바이너리이다.

**MCP 서버 바이너리:**

| 대상 | 아티팩트 이름 | 실행 환경 |
|------|-------------|----------|
| macOS ARM64 | `contextstream-mcp-darwin-arm64` | `macos-latest` |
| macOS x64 | `contextstream-mcp-darwin-x64` | `macos-latest` |
| Linux x64 | `contextstream-mcp-linux-x64` | `ubuntu-latest` |
| Linux ARM64 | `contextstream-mcp-linux-arm64` | `ubuntu-24.04-arm` |
| Windows x64 | `contextstream-mcp-win-x64.exe` | `windows-latest` |

**Hook runner 바이너리:**

| 대상 | 아티팩트 이름 |
|------|-------------|
| Linux x64 | `contextstream-hook-linux-x64` |
| macOS ARM64 | `contextstream-hook-darwin-arm64` |
| macOS x64 | `contextstream-hook-darwin-x64` |
| Windows x64 | `contextstream-hook-win-x64.exe` |

바이너리 빌드 시 버전이 빌드 타임 상수로 포함된다:
```bash
bun build src/index.ts --compile --outfile=<artifact> \
  --target=bun-<target> \
  --define __CONTEXTSTREAM_VERSION__=\"<version>\"
```

`version.ts`에서 `__CONTEXTSTREAM_VERSION__` 상수를 먼저 확인하고, 없으면 `package.json`에서 버전을 읽는다.

바이너리 설치 경로:
- Unix: `/usr/local/bin/contextstream-mcp`
- Windows: `%LOCALAPPDATA%\ContextStream\contextstream-mcp.exe`

### 6.4 Smithery 배포

`smithery.yaml`로 Smithery AI 레지스트리에 배포된다:

```yaml
startCommand:
  type: stdio
  configSchema:
    type: object
    properties:
      CONTEXTSTREAM_API_URL:
        type: string
        default: https://api.contextstream.io
      CONTEXTSTREAM_API_KEY:
        type: string
        description: Your ContextStream API key
    required:
      - CONTEXTSTREAM_API_KEY
  commandFunction: |-
    (config) => ({
      command: "npx",
      args: ["-y", "@contextstream/mcp-server"],
      env: { ... }
    })
```

### 6.5 MCP Server Manifest (server.json)

MCP 레지스트리용 서버 매니페스트. stdio 및 streamable HTTP 두 가지 transport를 지원한다:

- **stdio**: `@contextstream/mcp-server` npm 패키지 실행
- **streamable-http**: `https://mcp.contextstream.io/mcp` 엔드포인트 (Authorization, X-ContextStream-Workspace-Id, X-ContextStream-Project-Id 헤더)

### 6.6 설정 마법사 (setup)

`contextstream-mcp setup` 명령은 대화형 CLI 마법사를 실행한다. `src/setup.ts`에 구현되어 있으며, `readline/promises`를 사용한 stdin/stdout 인터페이스를 제공한다.

마법사는 8개 이상의 에디터에 대한 설치를 지원한다:

| 에디터 키 | 레이블 | 프로젝트 MCP 설정 지원 |
|-----------|--------|----------------------|
| `codex` | Codex CLI | - |
| `claude` | Claude Code | O |
| `cursor` | Cursor / VS Code | O |
| `cline` | Cline | - |
| `kilo` | Kilo Code | O |
| `roo` | Roo Code | O |
| `aider` | Aider | - |
| `antigravity` | Antigravity (Google) | O |

마법사 흐름:
1. API URL 및 API 키 입력/검증
2. 에디터 자동 감지 (파일 시스템 기반)
3. 설치할 에디터 선택
4. 에디터별 MCP 서버 설정 (`mcpServers.contextstream`) 생성
5. 규칙 파일 생성 (bootstrap 모드)
6. Claude Code hook 설치
7. 자격 증명 저장 (`~/.contextstream/credentials.json`)

---

## 7. 에디터 통합

### 7.1 Claude Code

**Hook 시스템**: Claude Code는 가장 풍부한 hook 지원을 제공한다. `installClaudeCodeHooks()`는 `~/.claude/settings.json` (글로벌) 또는 `<project>/.claude/settings.json` (프로젝트)에 hook 설정을 작성한다.

설치되는 hook 유형:

| Hook 유형 | Matcher | 명령 | Timeout |
|-----------|---------|------|---------|
| `PreToolUse` | `*` | `hook pre-tool-use` | 5s |
| `UserPromptSubmit` | `*` | `hook user-prompt-submit` | 5s |
| `UserPromptSubmit` | `*` | `hook on-save-intent` | 5s |
| `PostToolUse` | `Edit\|Write\|NotebookEdit` | `hook post-write` | 10s |
| `PostToolUseFailure` | `*` | `hook post-tool-use-failure` | 10s |
| `PreCompact` | `*` | `hook pre-compact` | 10s |
| `SessionStart` | `startup\|resume\|compact` | `hook session-start` | 10s |
| `Stop` | `*` | `hook stop` | 15s |
| `SessionEnd` | `*` | `hook session-end` | 10s |
| `SubagentStart` | `Explore\|Plan\|general-purpose\|custom` | `hook subagent-start` | 10s |
| `SubagentStop` | `Plan` | `hook subagent-stop` | 15s |
| `TaskCompleted` | `*` | `hook task-completed` | 10s |
| `TeammateIdle` | `*` | `hook teammate-idle` | 10s |
| `Notification` | `*` | `hook notification` | 10s |
| `PermissionRequest` | `*` | `hook permission-request` | 10s |

**규칙 파일**: `CLAUDE.md` (프로젝트) 또는 `~/.claude/CLAUDE.md` (글로벌)에 ContextStream 규칙을 생성한다.

**Hook 명령 우선순위** (`getHookCommand()`):
1. 바이너리 설치 경로 확인 (Unix: `/usr/local/bin/contextstream-mcp`, Windows: `%LOCALAPPDATA%\ContextStream\contextstream-mcp.exe`)
2. 설치된 패키지의 `dist/index.js` 직접 실행 (`node "<path>/index.js" hook <name>`)
3. `npx @contextstream/mcp-server hook <name>` (최후 수단)

### 7.2 Cursor / VS Code

**규칙 파일**: `.cursorrules` (프로젝트 루트)에 ContextStream 규칙을 생성한다. 글로벌 규칙은 Cursor 앱 UI를 통해 설정해야 하므로 프로그래밍 방식으로는 지원하지 않는다.

**MCP 설정**: `.cursor/mcp.json` 또는 프로젝트 `.mcp.json`에 `contextstream` 서버 설정을 추가한다.

### 7.3 Cline

**규칙 파일**: `~/Documents/Cline/Rules/contextstream.md` (글로벌) 또는 `.clinerules` (프로젝트)에 규칙을 생성한다.

**Hook 지원**: `installEditorHooks()` 함수가 Cline용 hook 설정을 생성한다. Hook 입력 형식이 Claude Code와 다르다 (`hookName`, `toolName`, `toolParameters`, `workspaceRoots`).

### 7.4 Kilo Code

**규칙 파일**: `~/.kilocode/rules/contextstream.md` (글로벌) 또는 `.kilocoderules` (프로젝트).

**MCP 설정 지원**: 프로젝트 MCP 설정을 지원한다.

### 7.5 Roo Code

**규칙 파일**: `~/.roo/rules/contextstream.md` (글로벌) 또는 `.roorules` (프로젝트).

**MCP 설정 지원**: 프로젝트 MCP 설정을 지원한다.

### 7.6 Aider

**규칙 파일**: `~/.aider.conf.yml` (글로벌)에 ContextStream 설정을 추가한다.

### 7.7 Antigravity (Google)

**규칙 파일**: `~/.gemini/GEMINI.md` (글로벌)에 규칙을 생성한다.

**MCP 설정 지원**: 프로젝트 MCP 설정을 지원한다.

### 7.8 Codex CLI

**규칙 파일**: `~/.codex/AGENTS.md` (글로벌).

### 7.9 규칙 생성 모드 (rules-templates.ts)

규칙 생성 시 세 가지 모드를 지원한다:

| 모드 | 설명 | 크기 |
|------|------|------|
| `bootstrap` | 최소 규칙 — `context()` 호출 보장에 집중 | ~40행 |
| `dynamic` | bootstrap과 동일 (하위 호환) | ~40행 |
| `full` | 상세 규칙 — hook 동작, 검색 프로토콜, 도구 카탈로그 포함 | 대형 |

**Bootstrap 규칙**의 핵심 철학: 정적 규칙은 모든 것을 담을 필요가 없다. `context()` 호출만 보장하면, `context()`가 나머지 모든 동적 규칙을 전달한다.

규칙 파일은 `<!-- BEGIN ContextStream -->` / `<!-- END ContextStream -->` 마커로 감싸진다. 기존 파일에 ContextStream 섹션이 있으면 해당 부분만 교체하고, 없으면 파일 끝에 추가한다. Legacy ContextStream 규칙도 감지하여 업데이트한다.

### 7.10 MCP Tool Prefix

에디터별로 MCP 도구 호출 시 사용되는 접두사가 다르다. `rules-templates.ts`의 `applyMcpToolPrefix()` 함수가 규칙 내 도구 이름에 적절한 접두사(예: `mcp__contextstream__`)를 적용한다.

---

## 8. CI/CD

### 8.1 GitHub Actions 워크플로우 (release.yml)

트리거:
- `release` 이벤트 (`created` 타입): GitHub Release 생성 시 자동 실행
- `workflow_dispatch`: 수동 실행 (version 입력 필수)

### 8.2 build-binaries 잡

5개 플랫폼에 대해 병렬로 바이너리를 빌드한다.

| OS | 타겟 | 아티팩트 |
|----|------|----------|
| `macos-latest` | `darwin-arm64` | `contextstream-mcp-darwin-arm64` |
| `macos-latest` | `darwin-x64` | `contextstream-mcp-darwin-x64` |
| `ubuntu-latest` | `linux-x64` | `contextstream-mcp-linux-x64` |
| `ubuntu-24.04-arm` | `linux-arm64` | `contextstream-mcp-linux-arm64` |
| `windows-latest` | `win-x64` | `contextstream-mcp-win-x64.exe` |

빌드 과정:
1. 코드 체크아웃 (`actions/checkout@v4`)
2. 버전 추출 (release tag에서 `v` 접두사 제거)
3. Bun 설정 (`oven-sh/setup-bun@v2`, `bun-version: latest`)
4. 의존성 설치 (`bun install`)
5. 바이너리 컴파일 (`bun build src/index.ts --compile ...`)
6. 아티팩트 업로드 (`actions/upload-artifact@v4`)
7. Release 자산 업로드 (`softprops/action-gh-release@v2`, release 이벤트 시에만)

### 8.3 build-hooks 잡

Hook runner 바이너리를 4개 플랫폼에 대해 빌드한다 (Ubuntu runner에서 크로스 컴파일).

```bash
bun build src/hooks/runner.ts --compile \
  --outfile=hooks-bin/contextstream-hook-<target> \
  --target=bun-<target> \
  --define __CONTEXTSTREAM_VERSION__=\"<version>\"
```

대상: `linux-x64`, `darwin-arm64`, `darwin-x64`, `windows-x64`.

### 8.4 update-checksums 잡

`build-binaries`와 `build-hooks`가 모두 완료된 후 실행된다 (release 이벤트 시에만):

1. 모든 아티팩트 다운로드
2. `sha256sum`으로 체크섬 생성 (`checksums.txt`)
3. Release에 체크섬 파일 업로드

### 8.5 npm 게시

CI에 별도 npm publish 잡은 없다. `package.json`의 `"prepublishOnly": "npm run build"` 스크립트가 `npm publish` 실행 시 자동으로 빌드를 수행한다. npm 게시는 수동으로 이루어지는 것으로 보인다.

---

## 9. 핵심 아키텍처 패턴

### 9.1 First-Tool Interceptor 패턴

`SessionManager`와 `withAutoContext()` 래퍼가 구현하는 핵심 패턴이다. 세션의 첫 번째 도구 호출 시 자동으로 컨텍스트를 초기화하고, 응답에 컨텍스트 요약을 prepend한다.

```
첫 번째 도구 호출 → autoInitialize() → API initSession() → 컨텍스트 요약 prepend
이후 도구 호출 → (이미 초기화됨, skip) → 원래 핸들러만 실행
```

이 패턴의 장점은 MCP Tools primitive만 사용하므로 모든 MCP 클라이언트에서 동작한다는 것이다.

### 9.2 통합 도메인 도구 (Consolidated Domain Tools)

v0.4.x의 기본 아키텍처. 개별 API 엔드포인트를 에디터에 도구 하나하나로 노출하는 대신, 도메인 단위로 통합된 ~11개 도구를 제공하여 도구 스키마 토큰을 ~75% 절감한다.

통합 도구 목록:
- `init` -- 세션 초기화
- `context` -- 매 메시지 컨텍스트 조회 (context_smart)
- `search` -- 검색 (모드: auto, semantic, hybrid, keyword, pattern)
- `session` -- 세션 작업 (capture, recall, remember, lessons, plans, etc.)
- `memory` -- 메모리 이벤트/노드 CRUD
- `graph` -- 코드 그래프 분석 (dependencies, impact, call_path, etc.)
- `project` -- 프로젝트 관리 (list, get, create, index, ingest_local, etc.)
- `workspace` -- 워크스페이스 관리 (list, get, associate, bootstrap)
- `reminder` -- 리마인더 관리
- `integration` -- 외부 통합 (slack, github, all)
- `help` -- 도움말 (tools, auth, version, editor_rules)

각 통합 도구는 `action` 파라미터로 세부 동작을 구분한다.

### 9.3 컨텍스트 압력(Context Pressure) 추적

`SessionManager`는 세션 내 토큰 사용량을 추정하여 컨텍스트 압력을 계산한다:

- 추적된 토큰 (도구 응답 문자수 기반, ~4문자/토큰) + 대화 턴 수 x 3,000 토큰/턴
- 기본 임계값: 70,000 토큰 (100k 컨텍스트 윈도우 가정)
- high/critical 압력 감지 시 체크포인트 자동 저장
- 압축 후 토큰 대폭 하락 감지 시 자동 복원 (`shouldRestorePostCompact()`)

### 9.4 HTTP 게이트웨이

`src/http-gateway.ts`는 `StreamableHTTPServerTransport`를 사용하여 stdio 없이 HTTP로 MCP 프로토콜을 제공한다. 세션 기반 라우팅으로 여러 클라이언트의 동시 접속을 지원하며, `AsyncLocalStorage` 기반 `auth-context.ts`로 요청별 인증 오버라이드를 처리한다.

지원 경로:
- `POST /mcp` -- MCP 메시지 처리
- `GET /.well-known/mcp-config` -- MCP 설정 디스커버리
- `GET /.well-known/mcp.json` -- MCP 서버 카드

### 9.5 인메모리 캐시

`src/cache.ts`의 `MemoryCache` 클래스는 TTL 기반 인메모리 캐시를 제공한다. workspace/project 정보 등 자주 접근하는 데이터의 HTTP 왕복을 줄인다. 60초마다 만료 항목을 정리하며, `unref()`로 프로세스 종료를 방해하지 않는다.

---

## 10. 테스트 인프라

### 10.1 Vitest 설정

```javascript
// vitest.config.cjs
module.exports = {
  cacheDir: ".vite",
  test: {
    globals: true,
    environment: "node",
    include: ["src/**/*.test.ts"],
    coverage: {
      provider: "v8",
      reporter: ["text", "json", "html"],
      exclude: ["node_modules", "dist", "**/*.test.ts"],
    },
  },
};
```

CJS 형식의 config를 사용한다 (`vitest.config.cjs`). 이는 `"type": "module"` 패키지에서 Vitest가 ESM config를 로딩할 때 발생하는 호환성 문제를 회피하기 위한 것으로 보인다.

### 10.2 테스트 파일

| 테스트 파일 | 대상 |
|------------|------|
| `src/cache.test.ts` | 인메모리 캐시 |
| `src/client.test.ts` | API 클라이언트 |
| `src/client.tags.test.ts` | 클라이언트 태그 기능 |
| `src/client.todos.test.ts` | 클라이언트 TODO 기능 |
| `src/files.test.ts` | 파일 읽기/해싱 |
| `src/hooks-config.test.ts` | Hook 설정 빌더 |
| `src/hooks-scenario.test.ts` | Hook 시나리오 통합 테스트 |
| `src/ignore.test.ts` | ignore 패턴 매칭 |
| `src/project-index-utils.test.ts` | 인덱스 상태 유틸 |
| `src/rules-templates.test.ts` | 규칙 템플릿 생성 |
| `src/todo-utils.test.ts` | TODO 유틸 |
| `src/token-savings.test.ts` | 토큰 절약 추적 |
| `src/version.test.ts` | 버전 유틸 |
| `src/hooks/prompt-state.test.ts` | Hook 상태 관리 |

---

## 11. 파일 인덱싱 시스템

`src/files.ts`는 코드 인덱싱을 위한 파일 수집 로직을 담당한다.

### 11.1 지원 확장자

약 60개 이상의 파일 확장자를 지원한다: `.ts`, `.tsx`, `.js`, `.jsx`, `.py`, `.rs`, `.go`, `.java`, `.kt`, `.c`, `.cpp`, `.cs`, `.rb`, `.php`, `.swift`, `.scala`, `.sh`, `.json`, `.yaml`, `.toml`, `.sql`, `.md`, `.html`, `.css` 등.

### 11.2 FileToIngest 인터페이스

```typescript
interface FileToIngest {
  path: string;
  content: string;
  language?: string;
  git_commit_sha?: string;
  git_commit_timestamp?: string;
  source_modified_at?: string;
  machine_id?: string;
  git_branch?: string;
  git_default_branch?: string;
  is_default_branch?: boolean;
}
```

Git 메타데이터(커밋 SHA, 브랜치, 타임스탬프)를 포함하여 다중 머신 동기화를 지원한다.

### 11.3 변경 감지

`sha256Hex` 해싱과 hash manifest (`readHashManifest`/`writeHashManifest`)를 사용하여 이전 인덱싱 이후 변경된 파일만 전송한다 (`readChangedFilesInBatches`). 자동 인덱싱 시 파일 수 상한은 10,000개 (`AUTO_INDEX_FILE_CAP`)이다.

---

## 12. MCP 프로토콜 구현

### 12.1 Tools

`src/tools.ts` (15,460행)에서 모든 도구를 `McpServer.tool()` API로 등록한다. 주요 함수:

- `registerTools()`: 정상 모드 도구 등록 (인증된 상태)
- `registerLimitedTools()`: 제한 모드 도구 등록 (설정 안내만)
- `setupClientDetection()`: MCP initialize 이벤트에서 클라이언트 감지

도구 응답 형식은 `ToolTextResult`로 통일된다:
```typescript
type ToolTextResult = {
  content: Array<{ type: "text"; text: string }>;
  structuredContent?: { [x: string]: unknown };
  isError?: boolean;
};
```

### 12.2 Resources

3개의 리소스가 등록된다:

| URI 패턴 | 이름 | 설명 |
|---------|------|------|
| `contextstream:openapi` | OpenAPI spec | API 엔드포인트의 OpenAPI JSON |
| `contextstream:workspaces` | Workspaces | 접근 가능한 워크스페이스 목록 |
| `contextstream:projects/{workspaceId}` | Projects | 워크스페이스별 프로젝트 목록 |

### 12.3 Prompts

22개의 프롬프트 템플릿이 등록된다:

| 프롬프트 | 접근 | 설명 |
|---------|------|------|
| `explore-codebase` | Free | 코드베이스 구조 탐색 |
| `capture-decision` | Free | 아키텍처 결정 문서화 |
| `review-context` | Free | 코드 리뷰 컨텍스트 구축 |
| `investigate-bug` | Free | 버그 디버깅 컨텍스트 |
| `explore-knowledge` | Free | 지식 그래프 탐색 |
| `onboard-to-project` | Free | 프로젝트 온보딩 가이드 |
| `analyze-refactoring` | Free | 리팩터링 분석 |
| `build-context` | PRO | LLM 작업용 종합 컨텍스트 |
| `smart-search` | Free | 메모리/결정/코드 통합 검색 |
| `recall-context` | Free | 과거 결정/노트 회상 |
| `session-summary` | Free | 세션 요약 |
| `capture-lesson` | Free | 학습 교훈 기록 |
| `capture-preference` | Free | 사용자 선호도 저장 |
| `capture-task` | Free | 작업 항목 캡처 |
| `capture-bug` | Free | 버그 리포트 캡처 |
| `capture-feature` | Free | 기능 요청 캡처 |
| `generate-plan` | PRO | 개발 계획 생성 |
| `generate-tasks` | PRO | 작업 목록 생성 |
| `token-budget-context` | PRO | 토큰 예산 내 컨텍스트 |
| `find-todos` | Free | 코드 내 TODO/FIXME 스캔 |
| `generate-editor-rules` | Free | 에디터 규칙 파일 생성 |
| `index-local-repo` | Free | 로컬 파일 인덱싱 |

PRO 프롬프트(`build-context`, `generate-plan`, `generate-tasks`, `token-budget-context`)는 유료 플랜이 필요하다.

---

## 13. 로깅 시스템

`tools.ts`에 정의된 3단계 로깅:

| 수준 | 환경변수 값 | 동작 |
|------|-----------|------|
| `quiet` | `CONTEXTSTREAM_LOG_LEVEL=quiet` | 에러만 출력 |
| `normal` | `CONTEXTSTREAM_LOG_LEVEL=normal` (기본) | 도구 시작/완료, 시스템 메시지 |
| `verbose` | `CONTEXTSTREAM_LOG_LEVEL=verbose` | 디버그 정보 포함 |

로그 아이콘 규칙:
- `◦` -- 도구 시작
- `✓` -- 도구 완료
- `✗` -- 에러
- `•` -- 시스템 정보
- `!` -- 경고

모든 로그는 `stderr`로 출력된다 (MCP 프로토콜이 `stdout`을 사용하므로).
