# claude-mem -- 아키텍처 분석

> **분석 대상**: claude-mem v10.5.6 (AGPL-3.0)
> **저장소**: https://github.com/thedotmack/claude-mem
> **설명**: Claude Code 플러그인 -- 세션 간 persistent memory 시스템. 도구 사용을 캡처하고, Claude Agent SDK를 통해 observation을 압축하며, 이후 세션에 관련 컨텍스트를 주입한다.

---

## 1. 프로젝트 구조

claude-mem은 단일 저장소 내에 여러 하위 프로젝트가 공존하는 구조를 취한다. 최상위 디렉토리는 다음과 같다.

```
claude-mem/
├── src/                    # 핵심 TypeScript 소스 (37,595 lines)
│   ├── bin/                # 독립 실행 스크립트 (cleanup-duplicates, import-xml-observations)
│   ├── cli/                # CLI 핸들러 및 어댑터
│   │   ├── adapters/       # claude-code, cursor, raw 어댑터
│   │   ├── handlers/       # context, file-edit, observation, session-init, summarize 등
│   │   ├── hook-command.ts # Hook 명령 라우터
│   │   ├── stdin-reader.ts # stdin 파이프 읽기
│   │   └── types.ts
│   ├── hooks/              # Hook response 처리 (hook-response.ts)
│   ├── sdk/                # SDK 인터페이스 (index.ts, parser.ts, prompts.ts)
│   ├── servers/            # MCP 서버 (mcp-server.ts, 455L)
│   ├── services/           # 핵심 서비스 계층
│   │   ├── context/        # ContextBuilder, ObservationCompiler, TokenCalculator, formatters, sections
│   │   ├── domain/         # ModeManager, domain types
│   │   ├── infrastructure/ # ProcessManager (802L), HealthMonitor (190L), GracefulShutdown (112L)
│   │   ├── integrations/   # CursorHooksInstaller (675L)
│   │   ├── queue/          # SessionQueueProcessor
│   │   ├── server/         # Express 서버 (Server.ts, Middleware.ts, ErrorHandler.ts)
│   │   ├── smart-file-read/# tree-sitter 기반 파일 파싱 (parser.ts 666L, search.ts)
│   │   ├── sqlite/         # SQLite 데이터 계층 (아래 별도 분석)
│   │   ├── sync/           # ChromaSync (812L), ChromaMcpManager (478L)
│   │   ├── transcripts/    # 트랜스크립트 watcher, processor, config
│   │   ├── worker/         # Worker 서비스 핵심
│   │   │   ├── agents/     # FallbackErrorHandler, ObservationBroadcaster, ResponseProcessor, SessionCleanupHelper
│   │   │   ├── events/     # SessionEventBroadcaster (SSE)
│   │   │   ├── http/       # BaseRouteHandler, middleware, routes/ (7개 라우트 모듈)
│   │   │   ├── search/     # SearchOrchestrator, strategies/ (Chroma, Hybrid, SQLite), filters/
│   │   │   ├── session/    # SessionCompletionHandler
│   │   │   ├── validation/ # PrivacyCheckValidator
│   │   │   ├── SDKAgent.ts, GeminiAgent.ts, OpenRouterAgent.ts
│   │   │   ├── SessionManager.ts (503L), SearchManager.ts (1,884L)
│   │   │   ├── DatabaseManager.ts, BranchManager.ts, FormattingService.ts
│   │   │   ├── ProcessRegistry.ts (463L), SSEBroadcaster.ts, TimelineService.ts
│   │   │   └── SettingsManager.ts, PaginationHelper.ts
│   │   └── worker-service.ts (1,251L) # Worker 진입점
│   ├── shared/             # 공유 유틸리티
│   │   ├── EnvManager.ts (274L), SettingsDefaultsManager.ts (243L)
│   │   ├── paths.ts (184L), path-utils.ts, hook-constants.ts
│   │   ├── plugin-state.ts, timeline-formatting.ts, transcript-parser.ts, worker-utils.ts
│   ├── supervisor/         # 프로세스 감독 시스템
│   │   ├── index.ts (189L), shutdown.ts (158L)
│   │   ├── health-checker.ts (41L), env-sanitizer.ts (21L)
│   │   └── process-registry.ts (254L)
│   ├── types/              # TypeScript 타입 정의
│   │   ├── database.ts (139L), transcript.ts (174L), tree-kill.d.ts
│   ├── ui/                 # React Viewer UI
│   │   ├── viewer/         # App.tsx, index.tsx, components/ (12개), hooks/ (8개), constants/, utils/
│   │   ├── viewer-template.html
│   │   └── SVG 아이콘 및 webp 로고 파일
│   └── utils/              # 범용 유틸리티
│       ├── logger.ts (409L), claude-md-utils.ts (462L)
│       ├── agents-md-utils.ts, bun-path.ts, cursor-utils.ts
│       ├── error-messages.ts, project-filter.ts, project-name.ts
│       ├── tag-stripping.ts, transcript-parser.ts, worktree.ts
├── plugin/                 # 빌드된 플러그인 배포 디렉토리
│   ├── .claude-plugin/     # plugin.json, CLAUDE.md
│   ├── hooks/              # hooks.json (84L) -- Claude Code hook 정의
│   ├── modes/              # 34개 JSON 모드 파일 + 1개 markdown
│   ├── scripts/            # 빌드된 CJS 번들 및 런처 스크립트
│   ├── skills/             # 4개 스킬 (do, make-plan, mem-search, smart-explore)
│   ├── ui/                 # viewer.html, viewer-bundle.js, SVG 아이콘, 폰트
│   └── package.json
├── installer/              # 대화형 설치기 (5 files, TypeScript + esbuild)
├── cursor-hooks/           # Cursor IDE 통합 (9 files)
├── openclaw/               # OpenClaw 법률 플러그인 (13 files)
├── ragtime/                # Ragtime RAG 도구 (4 files)
├── scripts/                # 빌드/유틸리티 스크립트 (37 files)
├── tests/                  # 테스트 스위트 (70 files, 18,834 lines)
├── docs/                   # Mintlify 기반 문서
├── .github/workflows/      # CI/CD (6 workflows)
├── package.json            # NPM 패키지 정의
├── tsconfig.json           # TypeScript 설정
├── CLAUDE.md               # AI 개발 지침
├── conductor.json          # Conductor 설정
└── .mcp.json               # MCP 서버 설정 (비어있음)
```

**총 소스 라인**: TypeScript/TSX 37,595 lines (src/ 디렉토리)
**테스트 라인**: 18,834 lines (70 files)

### 주요 파일 라인 수 (상위 15개)

| 파일 | 라인 수 | 역할 |
|------|---------|------|
| `src/services/sqlite/SessionStore.ts` | 2,459 | 세션/관찰/요약 CRUD |
| `src/services/worker/SearchManager.ts` | 1,884 | 검색 오케스트레이션 |
| `src/services/worker-service.ts` | 1,251 | Worker 진입점/라우팅 |
| `src/services/sqlite/migrations/runner.ts` | 866 | 마이그레이션 실행기 |
| `src/services/sync/ChromaSync.ts` | 812 | ChromaDB 벡터 동기화 |
| `src/services/infrastructure/ProcessManager.ts` | 802 | PID/프로세스 관리 |
| `src/services/worker/http/routes/SessionRoutes.ts` | 780 | 세션 HTTP 라우트 |
| `src/services/integrations/CursorHooksInstaller.ts` | 675 | Cursor 통합 |
| `src/services/smart-file-read/parser.ts` | 666 | Tree-sitter 파서 |
| `src/services/sqlite/SessionSearch.ts` | 607 | SQLite 검색 |
| `src/cli/claude-md-commands.ts` | 545 | CLAUDE.md 명령 |
| `src/services/sqlite/migrations.ts` | 522 | 레거시 마이그레이션 |
| `src/services/worker/SessionManager.ts` | 503 | 세션 생명주기 |
| `src/services/worker/SDKAgent.ts` | 489 | Claude SDK 에이전트 |
| `src/services/sqlite/PendingMessageStore.ts` | 489 | 대기 메시지 큐 |

---

## 2. 테크 스택

### 런타임

- **Node.js** >= 18.0.0 (package.json `engines`)
- **Bun** >= 1.0.0 (bun:sqlite 의존, Worker 데몬용 필수)
- **Python 3.13** (uv를 통해 자동 설치, ChromaDB용)

### TypeScript 설정

`tsconfig.json` 핵심 옵션:

```json
{
  "target": "ES2022",
  "module": "ESNext",
  "moduleResolution": "node",
  "jsx": "react",
  "strict": true,
  "declaration": true,
  "declarationMap": true,
  "sourceMap": true,
  "outDir": "./dist",
  "rootDir": "./src"
}
```

`package.json`의 `"type": "module"`로 ESM을 기본 모듈 시스템으로 사용한다.

### Production Dependencies

| 패키지 | 버전 | 용도 |
|--------|------|------|
| `@anthropic-ai/claude-agent-sdk` | ^0.1.76 | Claude Agent SDK -- 메모리 압축/관찰 생성 |
| `@modelcontextprotocol/sdk` | ^1.25.1 | MCP 프로토콜 구현 |
| `ansi-to-html` | ^0.7.2 | 터미널 ANSI 코드를 HTML로 변환 (Viewer UI) |
| `dompurify` | ^3.3.1 | HTML 새니타이징 |
| `express` | ^4.18.2 | HTTP 서버 (Worker API) |
| `glob` | ^11.0.3 | 파일 glob 패턴 매칭 |
| `handlebars` | ^4.7.8 | 템플릿 엔진 (컨텍스트 생성) |
| `react` | ^18.3.1 | Viewer UI 프레임워크 |
| `react-dom` | ^18.3.1 | React DOM 렌더러 |
| `yaml` | ^2.8.2 | YAML 파싱 |
| `zod-to-json-schema` | ^3.24.6 | Zod 스키마를 JSON Schema로 변환 |

### Dev Dependencies

| 패키지 | 버전 | 용도 |
|--------|------|------|
| `@types/cors` | ^2.8.19 | CORS 타입 |
| `@types/dompurify` | ^3.0.5 | DOMPurify 타입 |
| `@types/express` | ^4.17.21 | Express 타입 |
| `@types/node` | ^20.0.0 | Node.js 타입 |
| `@types/react` | ^18.3.5 | React 타입 |
| `@types/react-dom` | ^18.3.0 | React DOM 타입 |
| `esbuild` | ^0.27.2 | 번들러/빌드 도구 |
| `np` | ^11.0.2 | NPM 퍼블리싱 자동화 |
| `tree-sitter-cli` | ^0.26.5 | Tree-sitter CLI |
| `tree-sitter-c` | ^0.24.1 | C 파서 |
| `tree-sitter-cpp` | ^0.23.4 | C++ 파서 |
| `tree-sitter-go` | ^0.25.0 | Go 파서 |
| `tree-sitter-java` | ^0.23.5 | Java 파서 |
| `tree-sitter-javascript` | ^0.25.0 | JavaScript 파서 |
| `tree-sitter-python` | ^0.25.0 | Python 파서 |
| `tree-sitter-ruby` | ^0.23.1 | Ruby 파서 |
| `tree-sitter-rust` | ^0.24.0 | Rust 파서 |
| `tree-sitter-typescript` | ^0.23.2 | TypeScript 파서 |
| `tsx` | ^4.20.6 | TypeScript 실행기 |
| `typescript` | ^5.3.0 | TypeScript 컴파일러 |

### Optional Dependencies

| 패키지 | 버전 | 용도 |
|--------|------|------|
| `tree-kill` | ^1.2.2 | 프로세스 트리 종료 (Windows) |

---

## 3. 빌드 시스템

### 빌드 스크립트 (`scripts/build-hooks.js`, 217L)

esbuild를 사용하여 TypeScript 소스를 독립적인 CJS 번들로 빌드한다. 세 개의 주요 진입점이 있다:

1. **worker-service.cjs** -- `src/services/worker-service.ts`에서 번들링
   - `platform: 'node'`, `target: 'node18'`, `format: 'cjs'`
   - `minify: true`
   - `bun:sqlite`를 external로 처리 (Bun 내장 모듈)
   - Chromadb 임베딩 관련 패키지 external 처리 (`cohere-ai`, `ollama`, `@chroma-core/default-embed`, `onnxruntime-node`)
   - `#!/usr/bin/env bun` 배너 주입
   - `__DEFAULT_PACKAGE_VERSION__`을 package.json 버전으로 define

2. **mcp-server.cjs** -- `src/servers/mcp-server.ts`에서 번들링
   - 동일한 빌드 설정
   - tree-sitter 관련 10개 패키지를 external로 처리 (네이티브 바이너리)
   - `#!/usr/bin/env node` 배너 주입

3. **context-generator.cjs** -- `src/services/context-generator.ts`에서 번들링
   - `bun:sqlite`만 external

### React Viewer 빌드 (`scripts/build-viewer.js`)

빌드 스크립트가 별도로 실행되어 `plugin/ui/viewer.html`과 `plugin/ui/viewer-bundle.js`를 생성한다.

### Tree-sitter 파서 번들링

빌드 시 `plugin/package.json`이 자동 생성되며, 10개 언어의 tree-sitter 파서를 런타임 의존성으로 포함한다:

- C, C++, Go, Java, JavaScript, Python, Ruby, Rust, TypeScript (+ tree-sitter-cli)

이 파서들은 `src/services/smart-file-read/parser.ts`에서 코드 구조 분석에 사용된다.

### 출력 구조

```
plugin/
├── scripts/
│   ├── worker-service.cjs    # Bun 데몬 (메인 워커)
│   ├── mcp-server.cjs        # MCP 서버
│   ├── context-generator.cjs # 컨텍스트 생성기
│   ├── worker-wrapper.cjs    # 워커 래퍼
│   ├── smart-install.js      # 의존성 설치
│   ├── bun-runner.js         # Bun 런타임 탐색/실행
│   ├── worker-cli.js         # 워커 CLI
│   └── statusline-counts.js  # 상태 표시줄 카운트
├── ui/
│   ├── viewer.html           # React Viewer HTML
│   └── viewer-bundle.js      # 번들된 React 앱
└── package.json              # 런타임 의존성 (tree-sitter)
```

### 주요 NPM 스크립트

| 스크립트 | 설명 |
|----------|------|
| `npm run build` | `node scripts/build-hooks.js` -- 전체 빌드 |
| `npm run build-and-sync` | 빌드 후 마켓플레이스 동기화 및 워커 재시작 |
| `npm run sync-marketplace` | 빌드 결과를 `~/.claude/plugins/marketplaces/thedotmack/`에 동기화 |
| `npm run worker:start` | `bun plugin/scripts/worker-service.cjs start` |
| `npm run worker:stop` | `bun plugin/scripts/worker-service.cjs stop` |
| `npm run worker:restart` | `bun plugin/scripts/worker-service.cjs restart` |
| `npm run test` | `bun test` |
| `npm run release:patch` | `np patch --no-cleanup` |

---

## 4. 진입점

### CLI 진입점

`package.json`에 별도의 `bin` 필드는 없으나, 실행은 다음 경로를 통해 이루어진다:

- **worker-service.cjs** -- 주요 진입점. `start`, `stop`, `restart`, `status`, `hook`, `cursor` 서브커맨드를 처리한다.
- **mcp-server.cjs** -- MCP 프로토콜 서버. Claude Code의 `.mcp.json`을 통해 실행된다.
- **context-generator.cjs** -- 세션 시작 시 컨텍스트를 생성하여 stdout으로 출력한다.

### Hook 진입점

Claude Code 플러그인 시스템을 통해 6개의 생명주기 훅이 등록된다 (`plugin/hooks/hooks.json`):

1. **Setup** -- `setup.sh` 실행 (초기 환경 설정)
2. **SessionStart** -- smart-install.js 실행 -> worker start -> context 생성
3. **UserPromptSubmit** -- `session-init` 핸들러 호출
4. **PostToolUse** -- `observation` 핸들러 호출 (도구 사용 관찰)
5. **Stop** -- `summarize` 핸들러 호출 (세션 요약 생성)
6. **SessionEnd** -- `session-complete` 핸들러 호출

각 훅은 `node bun-runner.js worker-service.cjs hook claude-code <handler>` 형태로 실행된다. `bun-runner.js`는 시스템에서 Bun 런타임을 탐색하여 `worker-service.cjs`를 Bun으로 실행하는 래퍼이다.

### CLI 어댑터 시스템

`src/cli/adapters/`에 세 종류의 어댑터가 있다:

- **claude-code** -- Claude Code 전용 어댑터 (기본)
- **cursor** -- Cursor IDE 어댑터
- **raw** -- 범용 어댑터

어댑터는 각 IDE의 입력 형식을 통일된 내부 형식으로 변환한다.

### CLI 핸들러

`src/cli/handlers/` 디렉토리에 7개의 핸들러가 있다:

- `context.ts` -- 세션 시작 시 과거 기억 기반 컨텍스트 주입
- `session-init.ts` -- 세션 초기화 및 Worker 등록
- `observation.ts` -- 도구 사용 관찰 → Worker로 전송
- `summarize.ts` -- 세션 종료 시 요약 생성 요청
- `session-complete.ts` -- 세션 완료 처리
- `user-message.ts` -- 사용자 메시지 처리
- `file-edit.ts` -- 파일 편집 추적

### MCP 서버 진입점

`src/servers/mcp-server.ts` (455L)는 Model Context Protocol 서버로, tree-sitter 기반 smart file read 기능을 제공한다.

---

## 5. 플러그인 구조

### plugin/ 디렉토리 전체 구조

```
plugin/
├── .claude-plugin/
│   ├── plugin.json         # Claude Code 플러그인 메타데이터
│   └── CLAUDE.md
├── .mcp.json               # MCP 서버 설정
├── CLAUDE.md               # 플러그인 AI 지침
├── package.json            # 런타임 의존성
├── hooks/
│   ├── hooks.json          # 6개 생명주기 훅 정의 (84L)
│   ├── bugfixes-2026-01-10.md
│   └── CLAUDE.md
├── modes/                  # 35개 모드 파일
│   ├── code.json           # 기본 코드 모드
│   ├── code--chill.json    # 캐주얼 코드 모드
│   ├── code--ar.json       # 아랍어
│   ├── code--bn.json       # 벵골어
│   ├── code--cs.json       # 체코어
│   ├── code--da.json       # 덴마크어
│   ├── code--de.json       # 독일어
│   ├── code--el.json       # 그리스어
│   ├── code--es.json       # 스페인어
│   ├── code--fi.json       # 핀란드어
│   ├── code--fr.json       # 프랑스어
│   ├── code--he.json       # 히브리어
│   ├── code--hi.json       # 힌디어
│   ├── code--hu.json       # 헝가리어
│   ├── code--id.json       # 인도네시아어
│   ├── code--it.json       # 이탈리아어
│   ├── code--ja.json       # 일본어
│   ├── code--ko.json       # 한국어
│   ├── code--nl.json       # 네덜란드어
│   ├── code--no.json       # 노르웨이어
│   ├── code--pl.json       # 폴란드어
│   ├── code--pt-br.json    # 브라질 포르투갈어
│   ├── code--ro.json       # 루마니아어
│   ├── code--ru.json       # 러시아어
│   ├── code--sv.json       # 스웨덴어
│   ├── code--th.json       # 태국어
│   ├── code--tr.json       # 터키어
│   ├── code--uk.json       # 우크라이나어
│   ├── code--ur.json       # 우르두어
│   ├── code--vi.json       # 베트남어
│   ├── code--zh.json       # 중국어
│   ├── email-investigation.json  # 이메일 조사 모드
│   ├── law-study.json      # 법률 학습 모드
│   ├── law-study--chill.json # 캐주얼 법률 학습 모드
│   └── law-study-CLAUDE.md
├── scripts/                # 런타임 스크립트
│   ├── worker-service.cjs  # 빌드된 Worker 데몬
│   ├── mcp-server.cjs      # 빌드된 MCP 서버
│   ├── context-generator.cjs # 빌드된 컨텍스트 생성기
│   ├── worker-wrapper.cjs  # Worker 래퍼
│   ├── bun-runner.js       # Bun 탐색/실행 래퍼
│   ├── smart-install.js    # 의존성 자동 설치
│   ├── worker-cli.js       # Worker CLI 인터페이스
│   ├── statusline-counts.js # 상태 표시줄 카운트
│   └── CLAUDE.md
├── skills/                 # 4개 스킬
│   ├── do/SKILL.md         # 실행 스킬 (phased plan 실행)
│   ├── make-plan/SKILL.md  # 계획 수립 스킬
│   ├── mem-search/SKILL.md # 메모리 검색 스킬 (HTTP API)
│   └── smart-explore/SKILL.md # 스마트 탐색 스킬
└── ui/                     # Viewer UI
    ├── viewer.html         # React SPA HTML
    ├── viewer-bundle.js    # 번들된 React 앱
    ├── claude-mem-logo-for-dark-mode.webp
    ├── claude-mem-logomark.webp
    ├── icon-thick-completed.svg
    ├── icon-thick-investigated.svg
    ├── icon-thick-learned.svg
    ├── icon-thick-next-steps.svg
    └── assets/fonts/       # Monaspace Radon 폰트
```

### 모드 시스템

35개의 모드 JSON 파일이 `plugin/modes/`에 있다. 모드는 크게 세 카테고리로 나뉜다:

- **code** -- 기본 코드 모드 (1개 기본 + 1개 chill 변형 + 29개 언어 변형)
- **email-investigation** -- 이메일 조사 전문 모드
- **law-study** -- 법률 학습 모드 (1개 기본 + 1개 chill 변형)

언어 변형 모드는 `code--{locale}.json` 형식이며, 29개 언어를 지원한다 (ar, bn, cs, da, de, el, es, fi, fr, he, hi, hu, id, it, ja, ko, nl, no, pl, pt-br, ro, ru, sv, th, tr, uk, ur, vi, zh).

### 스킬 시스템

4개의 스킬이 `plugin/skills/`에 SKILL.md 형식으로 정의된다:

- **do** -- 단계별 계획을 서브에이전트를 사용하여 실행
- **make-plan** -- 문서 탐색을 포함한 단계별 구현 계획 수립
- **mem-search** -- Worker HTTP API (port 37777)를 통한 과거 작업 검색
- **smart-explore** -- 프로젝트 구조 탐색

---

## 6. 설정 시스템

### SettingsDefaultsManager (`src/shared/SettingsDefaultsManager.ts`, 243L)

모든 설정의 단일 진실 소스(single source of truth)를 제공한다. 설정 우선순위:

1. **환경 변수** (최우선)
2. **설정 파일** (`~/.claude-mem/settings.json`)
3. **하드코딩된 기본값** (최하위)

주요 설정 키:

| 키 | 기본값 | 설명 |
|----|--------|------|
| `CLAUDE_MEM_MODEL` | `claude-sonnet-4-5` | 메모리 에이전트 모델 |
| `CLAUDE_MEM_WORKER_PORT` | `37777` | Worker HTTP 포트 |
| `CLAUDE_MEM_WORKER_HOST` | `127.0.0.1` | Worker 호스트 |
| `CLAUDE_MEM_CONTEXT_OBSERVATIONS` | `50` | 컨텍스트에 포함할 관찰 수 |
| `CLAUDE_MEM_PROVIDER` | `claude` | AI 프로바이더 (claude/gemini/openrouter) |
| `CLAUDE_MEM_CLAUDE_AUTH_METHOD` | `cli` | 인증 방식 (cli/api) |
| `CLAUDE_MEM_GEMINI_MODEL` | `gemini-2.5-flash-lite` | Gemini 모델 |
| `CLAUDE_MEM_OPENROUTER_MODEL` | `xiaomi/mimo-v2-flash:free` | OpenRouter 모델 |
| `CLAUDE_MEM_DATA_DIR` | `~/.claude-mem` | 데이터 디렉토리 |
| `CLAUDE_MEM_LOG_LEVEL` | `INFO` | 로그 레벨 |
| `CLAUDE_MEM_MAX_CONCURRENT_AGENTS` | `2` | 최대 동시 에이전트 수 |
| `CLAUDE_MEM_CHROMA_ENABLED` | `true` | Chroma 벡터 DB 사용 여부 |
| `CLAUDE_MEM_CHROMA_PORT` | `8000` | Chroma 포트 |
| `CLAUDE_MEM_CONTEXT_SESSION_COUNT` | `10` | 컨텍스트에 포함할 세션 수 |
| `CLAUDE_MEM_SKIP_TOOLS` | (5개 도구) | 관찰에서 제외할 도구 |

### EnvManager (`src/shared/EnvManager.ts`, 274L)

`~/.claude-mem/.env` 파일에 API 키를 격리 저장한다. Issue #733을 해결하기 위해 도입되었다 -- 프로젝트의 `.env` 파일에 있는 `ANTHROPIC_API_KEY`가 SDK에 의해 자동 검색되어 개인 API 계정에 과금되는 문제를 방지한다.

관리 대상 자격 증명:
- `ANTHROPIC_API_KEY`
- `GEMINI_API_KEY`
- `OPENROUTER_API_KEY`

`buildIsolatedEnv()` 함수는 하위 프로세스 생성 시 차단 목록 방식으로 환경을 격리한다:
- `ANTHROPIC_API_KEY` -- 프로젝트 .env 자동 검색 방지
- `CLAUDECODE` -- Claude Code 내부 세션 충돌 방지

### 경로 시스템 (`src/shared/paths.ts`, 184L)

```
~/.claude-mem/                  # DATA_DIR (기본값, 설정으로 변경 가능)
├── claude-mem.db               # DB_PATH (SQLite 데이터베이스)
├── settings.json               # USER_SETTINGS_PATH
├── .env                        # 자격 증명
├── worker.pid                  # PID 파일
├── supervisor.json             # 프로세스 레지스트리
├── worker-{sessionId}.sock     # 세션별 Unix 소켓
├── archives/                   # 세션 아카이브
├── logs/                       # 로그 파일
├── trash/                      # 삭제된 데이터
├── backups/                    # 백업
├── modes/                      # 사용자 커스텀 모드
├── vector-db/                  # Chroma 벡터 DB
└── observer-sessions/          # SDK 쿼리용 cwd
```

### conductor.json

```json
{
  "scripts": {
    "setup": "cp ../settings.local.json .claude/settings.local.json && npm install",
    "run": "npm run build-and-sync"
  }
}
```

### .mcp.json

현재 비어있음 (`{ "mcpServers": {} }`). 별도의 MCP 서버 설정이 필요할 때 사용된다.

---

## 7. Supervisor 시스템

### 개요

Supervisor는 Worker 프로세스와 하위 프로세스의 생명주기를 관리하는 싱글턴 시스템이다. 5개의 파일로 구성된다.

### supervisor/index.ts (189L)

`Supervisor` 클래스가 핵심이다:

- **start()** -- ProcessRegistry 초기화, PID 파일 검증 (이미 실행 중이면 에러), HealthChecker 시작
- **stop()** -- HealthChecker 중지, `runShutdownCascade()` 실행
- **configureSignalHandlers()** -- SIGTERM, SIGINT, SIGHUP 핸들러 등록 (daemon 모드에서 SIGHUP 무시)
- **assertCanSpawn()** -- 종료 중일 때 새 프로세스 생성 거부
- **registerProcess() / unregisterProcess()** -- 프로세스 레지스트리 관리

`validateWorkerPidFile()` 함수는 PID 파일 상태를 검증한다:
- `missing` -- PID 파일 없음
- `alive` -- 프로세스가 살아있음
- `stale` -- 프로세스가 죽었으나 PID 파일 남아있음 (자동 정리)
- `invalid` -- PID 파일 파싱 실패 (자동 삭제)

### supervisor/shutdown.ts (158L)

`runShutdownCascade()`는 다단계 종료 과정을 구현한다:

1. 모든 자식 프로세스를 최신 시작 순으로 정렬
2. 각 살아있는 프로세스에 **SIGTERM** 전송
3. **5초** 대기 (100ms 간격 폴링)
4. 생존 프로세스에 **SIGKILL** 전송
5. **1초** 추가 대기
6. 레지스트리에서 모든 항목 해제
7. **PID 파일** 삭제
8. 레지스트리에서 죽은 항목 정리 (`pruneDeadEntries`)

Windows 환경에서는 `tree-kill` 모듈을 사용하여 프로세스 트리를 종료하고, 사용 불가 시 `taskkill /PID /T /F`로 폴백한다.

### supervisor/health-checker.ts (41L)

30초 간격으로 프로세스 레지스트리를 검사하여 죽은 프로세스를 자동 정리한다. `setInterval`에 `unref()`를 호출하여 프로세스 종료를 방해하지 않는다.

### supervisor/env-sanitizer.ts (21L)

하위 프로세스 생성 시 환경 변수에서 `CLAUDECODE_*`, `CLAUDE_CODE_*` 접두사와 `CLAUDECODE`, `CLAUDE_CODE_SESSION`, `CLAUDE_CODE_ENTRYPOINT`, `MCP_SESSION_ID`를 제거한다.

### supervisor/process-registry.ts (254L)

`ProcessRegistry` 클래스는 관리되는 프로세스를 `~/.claude-mem/supervisor.json`에 영속화한다:

- **initialize()** -- 파일 로드, 죽은 프로세스 정리
- **register(id, info, processRef?)** -- 프로세스 등록 (ChildProcess 참조 선택적 저장)
- **unregister(id)** -- 프로세스 해제
- **getAll()** -- 시작 시간 순 정렬된 전체 목록
- **getBySession(sessionId)** -- 세션별 프로세스 조회
- **getByPid(pid)** -- PID별 조회
- **pruneDeadEntries()** -- 죽은 프로세스 자동 정리 (반환: 제거 수)
- **reapSession(sessionId)** -- 세션의 모든 프로세스를 SIGTERM -> 5초 대기 -> SIGKILL 순서로 종료 (Issue #1351 대응)

`isPidAlive(pid)` 유틸리티는 `process.kill(pid, 0)`으로 프로세스 존재를 확인한다. `EPERM`은 살아있는 것으로 간주한다.

---

## 8. 인프라

### ProcessManager.ts (802L)

`src/services/infrastructure/ProcessManager.ts`는 다음을 담당한다:

- **PID 파일 관리** -- `~/.claude-mem/worker.pid`에 JSON 형식 (`{pid, port, startedAt}`)으로 기록
- **런타임 탐색** -- `resolveWorkerRuntimePath()`가 Bun 실행 파일을 탐색. Windows에서는 Bun 필수 (bun:sqlite 의존성). 탐색 순서: 현재 실행 경로 -> `BUN` 환경 변수 -> PATH 검색 -> 알려진 설치 경로
- **고아 프로세스 정리** -- `mcp-server.cjs`, `worker-service.cjs`, `chroma-mcp` 패턴의 30분 이상 된 프로세스를 자동 정리
- **시그널 핸들러 등록** -- SIGTERM, SIGINT, SIGHUP (daemon 모드 제외)
- **자식 프로세스 열거 및 정리** -- Windows 좀비 포트 문제 대응

### HealthMonitor.ts (190L)

Worker 상태 모니터링을 제공한다:

- **isPortInUse(port)** -- `/api/health` 엔드포인트로 포트 사용 확인
- **waitForHealth(port, timeout)** -- Worker HTTP 서버 응답 대기 (liveness check)
- **waitForReadiness(port, timeout)** -- DB 및 검색 초기화 완료 대기 (readiness check)
- **waitForPortFree(port, timeout)** -- 포트 해제 대기 (재시작용)
- **httpShutdown(port)** -- HTTP POST `/api/admin/shutdown`으로 종료 요청
- **checkVersionMatch(port)** -- 설치된 플러그인 버전과 실행 중인 Worker 버전 비교 (업데이트 감지)

### GracefulShutdown.ts (112L)

6단계 정상 종료 과정을 구현한다:

1. **HTTP 서버 종료** -- `closeAllConnections()` + `server.close()`. Windows에서는 소켓 해제를 위해 500ms 추가 대기
2. **세션 매니저 종료** -- `shutdownAll()`
3. **MCP 클라이언트 종료** -- 자식 프로세스에 정상 종료 시그널
4. **Chroma MCP 연결 종료**
5. **데이터베이스 연결 종료** (ChromaSync 정리 포함)
6. **Supervisor 종료** -- 추적된 자식 프로세스 종료, PID 정리, 소켓 정리

---

## 9. CI/CD

`.github/workflows/` 디렉토리에 6개의 GitHub Actions 워크플로우가 있다:

| 파일 | 설명 |
|------|------|
| `claude.yml` (1,898 bytes) | Claude Code 자동화 |
| `claude-code-review.yml` (1,964 bytes) | Claude Code를 활용한 PR 리뷰 |
| `convert-feature-requests.yml` (4,588 bytes) | Feature request 이슈 변환 (가장 큰 워크플로우) |
| `deploy-install-scripts.yml` (726 bytes) | 설치 스크립트 배포 |
| `npm-publish.yml` (438 bytes) | NPM 패키지 퍼블리싱 |
| `summary.yml` (879 bytes) | 요약 생성 |

릴리스 프로세스는 `np` 패키지를 사용한다 (`npm run release:patch`, `release:minor`, `release:major`). `prepublishOnly` 스크립트로 빌드가 자동 실행된다.

---

## 10. 테스트

### 구성

- **테스트 프레임워크**: Bun의 내장 테스트 러너 (`bun test`)
- **총 파일**: 70개
- **총 라인**: 18,834

### 테스트 카테고리

`package.json`의 테스트 스크립트를 통해 카테고리별 실행이 가능하다:

| 카테고리 | 명령 | 경로 |
|----------|------|------|
| SQLite | `npm run test:sqlite` | `tests/sqlite/` |
| Agents | `npm run test:agents` | `tests/worker/agents/` |
| Search | `npm run test:search` | `tests/worker/search/` |
| Context | `npm run test:context` | `tests/context/` |
| Infrastructure | `npm run test:infra` | `tests/infrastructure/` |
| Server | `npm run test:server` | `tests/server/` |

### 테스트 특징

- SQLite 테스트는 `:memory:` 데이터베이스를 사용하여 파일 시스템 의존성을 제거한다
- 마이그레이션 테스트는 실제 마이그레이션 로직의 멱등성(idempotency)을 검증한다
- Worker/Agent 테스트는 SDK 호출을 모킹하여 네트워크 의존성을 제거한다
