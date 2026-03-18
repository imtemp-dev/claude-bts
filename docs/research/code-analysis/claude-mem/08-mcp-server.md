# claude-mem -- MCP 서버 및 인프라 분석

## 1. MCP 서버 (mcp-server.ts, 455줄)

mcp-server.ts는 Claude Code, Cursor 등의 호스트 애플리케이션에 MCP(Model Context Protocol) 도구를 제공하는 서버이다. "Thin HTTP Wrapper" 설계로, 비즈니스 로직을 Worker HTTP API에 위임하고 MCP 프로토콜 처리와 도구 스키마 정의에 집중한다.

### 1.1 아키텍처

```
[Claude Code/Cursor] <--stdio JSON-RPC--> [MCP Server] <--HTTP--> [Worker API :37777]
```

MCP 서버는 stdio 전송을 사용한다. stdout은 JSON-RPC 프로토콜 메시지 전용이므로, `console.log`를 가로채어 `logger.error`로 리다이렉트한다:

```typescript
console['log'] = (...args: any[]) => {
  logger.error('CONSOLE', 'Intercepted console output (MCP protocol protection)', undefined, { args });
};
```

이 가로채기는 다른 import보다 먼저 실행되어, 어떤 라이브러리가 `console.log`를 호출하더라도 stdout이 오염되지 않도록 보장한다. Claude Desktop이 `[2025...` 같은 로그 출력을 JSON 배열로 파싱하려 시도하는 문제를 방지한다.

### 1.2 등록된 도구 (Tools)

총 **7개 도구**가 등록된다:

#### 1.2.1 `__IMPORTANT` -- 워크플로우 안내

```typescript
{
  name: '__IMPORTANT',
  description: `3-LAYER WORKFLOW (ALWAYS FOLLOW):
1. search(query) -> Get index with IDs (~50-100 tokens/result)
2. timeline(anchor=ID) -> Get context around interesting results
3. get_observations([IDs]) -> Fetch full details ONLY for filtered IDs
NEVER fetch full details without filtering first. 10x token savings.`
}
```

입력 스키마: 빈 객체 `{}`. 호출하면 3-Layer 워크플로우의 상세 가이드를 반환한다. 도구 이름이 `__IMPORTANT`인 이유는 LLM이 도구 목록을 스캔할 때 가장 먼저 이 지침을 인식하도록 하기 위함이다.

#### 1.2.2 `search` -- 메모리 검색

```
Step 1: Search memory. Returns index with IDs.
Params: query, limit, project, type, obs_type, dateStart, dateEnd, offset, orderBy
```

Worker API의 `/api/search`에 GET으로 위임한다. 파라미터를 쿼리 스트링으로 변환하여 전달. 입력 스키마는 `additionalProperties: true`로 열려 있어 LLM이 자유롭게 파라미터를 전달할 수 있다.

#### 1.2.3 `timeline` -- 타임라인 컨텍스트

```
Step 2: Get context around results.
Params: anchor (observation ID) OR query (finds anchor automatically), depth_before, depth_after, project
```

Worker API의 `/api/timeline`에 GET으로 위임한다.

#### 1.2.4 `get_observations` -- 상세 조회

```
Step 3: Fetch full details for filtered IDs.
Params: ids (array of observation IDs, required), orderBy, limit, project
```

Worker API의 `/api/observations/batch`에 **POST**로 위임한다. `ids`가 필수 파라미터이며, 배열 타입이다:

```typescript
inputSchema: {
  type: 'object',
  properties: {
    ids: {
      type: 'array',
      items: { type: 'number' },
      description: 'Array of observation IDs to fetch (required)'
    }
  },
  required: ['ids'],
  additionalProperties: true
}
```

#### 1.2.5 `smart_search` -- 코드베이스 구조 검색

```
Search codebase for symbols, functions, classes using tree-sitter AST parsing.
Returns folded structural views with token counts.
```

Worker API를 거치지 않고 **직접** `searchCodebase()`를 호출한다:

```typescript
handler: async (args: any) => {
  const rootDir = resolve(args.path || process.cwd());
  const result = await searchCodebase(rootDir, args.query, {
    maxResults: args.max_results || 20,
    filePattern: args.file_pattern
  });
  const formatted = formatSearchResults(result, args.query);
  return { content: [{ type: 'text', text: formatted }] };
}
```

입력 파라미터:
- `query` (string, required) -- 검색어
- `path` (string) -- 루트 디렉토리 (기본: cwd)
- `max_results` (number) -- 최대 결과 수 (기본: 20)
- `file_pattern` (string) -- 파일 경로 필터

#### 1.2.6 `smart_unfold` -- 심볼 소스 코드 펼치기

```
Expand a specific symbol (function, class, method) from a file.
Returns the full source code of just that symbol.
```

파일을 읽고 `unfoldSymbol()`로 특정 심볼을 추출한다. 심볼을 찾지 못하면 `parseFile()`로 사용 가능한 심볼 목록을 반환한다.

입력 파라미터:
- `file_path` (string, required) -- 소스 파일 경로
- `symbol_name` (string, required) -- 펼칠 심볼 이름

#### 1.2.7 `smart_outline` -- 파일 구조 개요

```
Get structural outline of a file -- shows all symbols with signatures but bodies folded.
Much cheaper than reading the full file.
```

`parseFile()` 후 `formatFoldedView()`로 접힌 뷰를 생성한다.

입력 파라미터:
- `file_path` (string, required) -- 소스 파일 경로

### 1.3 도구-엔드포인트 매핑

```typescript
const TOOL_ENDPOINT_MAP: Record<string, string> = {
  'search': '/api/search',
  'timeline': '/api/timeline'
};
```

`search`와 `timeline`만 Worker API에 위임되고, `get_observations`는 POST 전용 헬퍼 `callWorkerAPIPost()`를 사용하며, `smart_*` 3종은 MCP 서버 내에서 직접 실행된다.

### 1.4 Worker API 통신

두 가지 호출 패턴:

**GET (callWorkerAPI)**: 파라미터를 URLSearchParams로 변환하여 쿼리 스트링 전달. `workerHttpRequest()`가 Unix 소켓 또는 TCP를 자동 선택.

**POST (callWorkerAPIPost)**: JSON body를 직접 전달. 응답을 `JSON.stringify(data, null, 2)`로 포매팅하여 MCP 텍스트 콘텐츠로 래핑.

에러 시 `isError: true`와 에러 메시지를 포함한 MCP 응답을 반환하여, MCP 프로토콜 수준의 에러가 아닌 도구 수준의 에러로 처리한다.

### 1.5 MCP 서버 설정

```typescript
const server = new Server(
  { name: 'claude-mem', version: packageVersion },
  { capabilities: { tools: {} } }
);
```

`packageVersion`은 빌드 시 esbuild define으로 주입되는 `__DEFAULT_PACKAGE_VERSION__`이다.

두 개의 핸들러가 등록된다:
- `ListToolsRequestSchema` -- 7개 도구의 이름, 설명, 스키마 반환
- `CallToolRequestSchema` -- 도구 이름으로 해당 핸들러 조회 후 실행, 에러 시 `isError: true` 반환

### 1.6 부모 프로세스 하트비트

```typescript
const HEARTBEAT_INTERVAL_MS = 30_000;
```

Unix 전용 (Windows 제외): 30초마다 `process.ppid`를 확인한다. ppid가 1 (orphaned) 이거나 초기값과 달라지면 부모가 죽은 것으로 판단하여 `cleanup()` -> `process.exit(0)`을 호출한다. 이를 통해 Claude Code가 비정상 종료될 때 MCP 서버가 좀비로 남는 것을 방지한다.

타이머에 `unref()`를 호출하여 하트비트 자체가 프로세스를 살려두지 않도록 한다.

### 1.7 시작 흐름

```typescript
async function main() {
  const transport = new StdioServerTransport();
  await server.connect(transport);
  startParentHeartbeat();

  // 비동기 Worker 연결 확인 (비차단)
  setTimeout(async () => {
    const workerAvailable = await verifyWorkerConnection();
    if (!workerAvailable) {
      // 경고 로그만 출력, 서버는 계속 실행
    }
  }, 0);
}
```

Worker가 사용 불가해도 MCP 서버는 시작된다. 도구 호출 시점에 에러가 반환될 뿐이다.

---

## 2. HTTP 서버 (Server.ts, 363줄)

Server.ts는 Express 기반의 HTTP 서버 래퍼로, 미들웨어 등록과 코어 시스템 엔드포인트를 제공한다.

### 2.1 ServerOptions 인터페이스

```typescript
export interface ServerOptions {
  getInitializationComplete: () => boolean;
  getMcpReady: () => boolean;
  onShutdown: () => Promise<void>;
  onRestart: () => Promise<void>;
  workerPath: string;
  getAiStatus: () => AiStatus;
}
```

콜백 패턴으로 워커 서비스의 상태를 주입받는다. 서버 자체는 비즈니스 로직을 모르며, 상태 확인을 콜백으로 위임한다.

### 2.2 코어 엔드포인트

#### `GET /api/health` -- 헬스 체크

항상 200 OK를 반환한다 (초기화 중이라도). 응답 본문:

```json
{
  "status": "ok",
  "version": "1.2.3",
  "workerPath": "/path/to/worker-service.cjs",
  "uptime": 123456,
  "managed": false,
  "hasIpc": false,
  "platform": "darwin",
  "pid": 12345,
  "initialized": true,
  "mcpReady": true,
  "ai": {
    "provider": "claude",
    "authMethod": "cli-subscription",
    "lastInteraction": { "timestamp": 1234, "success": true }
  }
}
```

#### `GET /api/readiness` -- 준비 상태

초기화 완료 전: 503 `{ status: 'initializing', message: '...' }`
초기화 완료 후: 200 `{ status: 'ready', mcpReady: true }`

#### `GET /api/version` -- 버전

```json
{ "version": "1.2.3" }
```

#### `GET /api/instructions` -- SKILL.md 로딩

`topic` 파라미터로 특정 섹션을 추출하거나 전체 내용을 반환한다:
- `workflow`, `search_params`, `examples`, `all`

`operation` 파라미터가 있으면 `mem-search/operations/{operation}.md` 파일을 로드한다. 경로 순회(path traversal) 방지를 위해 `path.resolve()` + `startsWith()` 검증을 수행한다.

#### `POST /api/admin/restart` -- 재시작 (localhost 전용)

`requireLocalhost` 미들웨어가 `req.ip`를 검증한다. Windows managed 모드에서는 IPC로 래퍼에 restart 메시지를 보내고, 그 외에는 직접 `onRestart()` 호출 후 `process.exit(0)`.

#### `POST /api/admin/shutdown` -- 종료 (localhost 전용)

`onShutdown()` 호출 후 반드시 `process.exit(0)`. 코드 주석에서 강조: "Without this, the daemon stays alive as a zombie -- background tasks (backfill, reconnects) keep running and respawn chroma-mcp subprocesses."

#### `GET /api/admin/doctor` -- 진단 (localhost 전용)

Supervisor의 프로세스 레지스트리를 조회하여 각 프로세스의 생존 상태를 확인한다:

```json
{
  "supervisor": { "running": true, "pid": 12345, "uptime": "2h 30m" },
  "processes": [
    { "id": "chroma-mcp", "pid": 54321, "type": "chroma", "status": "alive", "startedAt": "..." }
  ],
  "health": {
    "deadProcessPids": [],
    "envClean": true
  }
}
```

`envClean`은 `CLAUDECODE_*` 환경 변수가 현재 프로세스에 누출되지 않았는지 확인한다.

### 2.3 라우트 등록 패턴

```typescript
registerRoutes(handler: RouteHandler): void {
  handler.setupRoutes(this.app);
}
```

`RouteHandler` 인터페이스를 구현하는 외부 모듈이 `setupRoutes(app)`으로 Express 앱에 라우트를 추가한다. 모든 라우트 등록 후 `finalizeRoutes()`를 호출하여 404 핸들러와 글로벌 에러 핸들러를 마지막에 등록한다.

### 2.4 서버 생명주기

```typescript
async listen(port: number, host: string): Promise<void>
async close(): Promise<void>
```

`close()`에서 Windows 호환성을 위한 추가 지연이 있다:
1. `closeAllConnections()` -- 활성 연결 모두 종료
2. Windows 전용: 500ms 대기
3. `server.close()` -- 서버 종료
4. Windows 전용: 500ms 추가 대기 (포트 완전 해제 보장)

---

## 3. 에러 처리 (ErrorHandler.ts)

### 3.1 AppError 클래스

```typescript
export class AppError extends Error {
  constructor(
    message: string,
    public statusCode: number = 500,
    public code?: string,
    public details?: unknown
  ) {
    super(message);
    this.name = 'AppError';
  }
}
```

표준 Error를 확장하여 HTTP 상태 코드, 에러 코드, 상세 정보를 포함한다.

### 3.2 에러 응답 형식

```typescript
export interface ErrorResponse {
  error: string;    // 에러 이름 (e.g., 'AppError')
  message: string;  // 사람이 읽을 수 있는 메시지
  code?: string;    // 기계 판독 가능한 코드
  details?: unknown; // 추가 상세 정보
}
```

### 3.3 글로벌 에러 핸들러 미들웨어

```typescript
export const errorHandler: ErrorRequestHandler = (err, req, res, _next) => {
  const statusCode = err instanceof AppError ? err.statusCode : 500;
  // ... 로깅 및 응답
  res.status(statusCode).json(response);
};
```

`AppError`이면 지정된 상태 코드를 사용하고, 일반 Error이면 500을 반환한다. `_next`를 호출하지 않아 에러 처리가 여기서 종료된다.

### 3.4 404 핸들러

```typescript
export function notFoundHandler(req: Request, res: Response): void {
  res.status(404).json(createErrorResponse('NotFound', `Cannot ${req.method} ${req.path}`));
}
```

### 3.5 비동기 래퍼

```typescript
export function asyncHandler<T>(
  fn: (req: Request, res: Response, next: NextFunction) => Promise<T>
): (req: Request, res: Response, next: NextFunction) => void
```

async 라우트 핸들러의 거부(rejection)를 Express 에러 핸들러로 자동 전달한다.

---

## 4. 미들웨어 (Middleware.ts + worker/http/middleware.ts)

`Middleware.ts`는 `worker/http/middleware.ts`의 re-export 모듈이다. 실제 구현은 후자에 있다.

### 4.1 미들웨어 체인

`createMiddleware(summarizeRequestBody)` 함수가 4개의 미들웨어를 순서대로 반환한다:

**1. JSON 파싱**
```typescript
express.json({ limit: '50mb' })
```
50MB 제한의 JSON body 파싱.

**2. CORS**
```typescript
cors({
  origin: (origin, callback) => {
    if (!origin || origin.startsWith('http://localhost:') || origin.startsWith('http://127.0.0.1:')) {
      callback(null, true);
    } else {
      callback(new Error('CORS not allowed'));
    }
  },
  methods: ['GET', 'HEAD', 'POST', 'PUT', 'PATCH', 'DELETE'],
  allowedHeaders: ['Content-Type', 'Authorization', 'X-Requested-With'],
  credentials: false
})
```

localhost와 127.0.0.1 오리진만 허용한다. Origin 헤더가 없는 요청(hooks, curl, CLI)도 허용된다.

**3. HTTP 요청/응답 로깅**

정적 자산(`.html`, `.js`, `.css`, `.svg`, `.webp`, `.woff2` 등), 헬스 체크(`/health`), 폴링 엔드포인트(`/api/logs`)는 로깅에서 제외한다. 나머지 요청은:
- 요청 시: `-> GET /api/search` + body 요약
- 응답 시: `<- 200 /api/search` + 소요 시간

`res.send`를 프록시하여 응답 시점을 캡처한다.

**4. 정적 파일 서빙**
```typescript
express.static(path.join(packageRoot, 'plugin', 'ui'))
```
웹 UI(viewer.html, viewer-bundle.js, 로고, 폰트 등)를 서빙한다.

### 4.2 localhost 전용 미들웨어

```typescript
export function requireLocalhost(req: Request, res: Response, next: NextFunction): void
```

`req.ip`가 `127.0.0.1`, `::1`, `::ffff:127.0.0.1`, `localhost` 중 하나인지 확인한다. 아니면 403 Forbidden을 반환한다. admin 엔드포인트에 사용된다.

### 4.3 요청 본문 요약

```typescript
export function summarizeRequestBody(method: string, path: string, body: any): string
```

민감 데이터나 대용량 페이로드의 로깅을 방지한다:
- `/init` 경로: 빈 문자열
- `/observations` 경로: `tool={toolSummary}` 형태로 요약
- `/summarize` 경로: `'requesting summary'`
- 기타: 빈 문자열

---

## 5. 허용 상수 (allowed-constants.ts)

`/api/instructions` 엔드포인트의 보안을 위한 화이트리스트:

```typescript
export const ALLOWED_OPERATIONS = ['search', 'context', 'summarize', 'import', 'export'];
export const ALLOWED_TOPICS = ['workflow', 'search_params', 'examples', 'all'];
```

이 상수들은 Server.ts의 instructions 핸들러에서 입력 검증에 사용된다. 허용되지 않은 operation이나 topic이 요청되면 400 에러를 반환한다.

operations는 `mem-search/operations/` 디렉토리의 마크다운 파일에 매핑되며, path traversal을 추가로 검증한다.

---

## 6. 스마트 파일 읽기 -- 파서 (parser.ts, 666줄)

parser.ts는 tree-sitter CLI를 셸아웃(shell out)하여 AST 기반 코드 구조를 추출하는 파서이다. 네이티브 바인딩이나 WASM 없이 CLI 바이너리와 쿼리 패턴만 사용한다.

### 6.1 지원 언어

```typescript
const LANG_MAP: Record<string, string> = {
  ".js": "javascript", ".mjs": "javascript", ".cjs": "javascript",
  ".jsx": "tsx", ".ts": "typescript", ".tsx": "tsx",
  ".py": "python", ".pyw": "python",
  ".go": "go", ".rs": "rust", ".rb": "ruby",
  ".java": "java", ".c": "c", ".h": "c",
  ".cpp": "cpp", ".cc": "cpp", ".cxx": "cpp", ".hpp": "cpp", ".hh": "cpp"
};
```

10개 언어를 지원하며, `detectLanguage()` 함수가 파일 확장자로 언어를 판별한다.

### 6.2 문법(Grammar) 패키지 해결

```typescript
const GRAMMAR_PACKAGES: Record<string, string> = {
  javascript: "tree-sitter-javascript",
  typescript: "tree-sitter-typescript/typescript",
  tsx: "tree-sitter-typescript/tsx",
  python: "tree-sitter-python",
  // ... go, rust, ruby, java, c, cpp
};
```

`resolveGrammarPath()`가 `_require.resolve()`로 문법 패키지의 경로를 찾는다. ESM과 CJS 번들 모두에서 동작하도록 `typeof __filename` 체크로 적절한 `createRequire`를 사용한다.

### 6.3 쿼리 패턴 (Query Patterns)

언어별 tree-sitter 쿼리가 정의된다. 예를 들어 JS/TS:

```scheme
(function_declaration name: (identifier) @name) @func
(lexical_declaration (variable_declarator name: (identifier) @name value: [(arrow_function) (function_expression)])) @const_func
(class_declaration name: (type_identifier) @name) @cls
(method_definition name: (property_identifier) @name) @method
(interface_declaration name: (type_identifier) @name) @iface
(type_alias_declaration name: (type_identifier) @name) @tdef
(enum_declaration name: (identifier) @name) @enm
(import_statement) @imp
(export_statement) @exp
```

지원 언어 그룹:
- **jsts**: JavaScript, TypeScript, TSX
- **python**: Python
- **go**: Go
- **rust**: Rust (struct, enum, trait, impl 포함)
- **ruby**: Ruby
- **java**: Java
- **generic**: 범용 폴백

### 6.4 CLI 실행

`getTreeSitterBin()`이 tree-sitter CLI 바이너리를 찾는다:
1. `tree-sitter-cli/package.json` 해결 -> `{dir}/tree-sitter`
2. 폴백: PATH에서 `tree-sitter`

쿼리 파일은 임시 디렉토리(`smart-read-queries-*`)에 캐시된다:

```typescript
function getQueryFile(queryKey: string): string {
  if (queryFileCache.has(queryKey)) return queryFileCache.get(queryKey)!;
  // ... 임시 파일 생성, 캐시
}
```

`runBatchQuery()`가 여러 파일을 한 번의 CLI 호출로 처리한다:

```bash
tree-sitter query -p {grammarPath} {queryFile} {file1} {file2} ...
```

타임아웃 30초가 설정되어 있다.

### 6.5 쿼리 출력 파싱

`parseMultiFileQueryOutput()`이 tree-sitter CLI의 텍스트 출력을 파싱한다:

```
/path/to/file.ts
  pattern: 0
    capture: name, start: (5, 16), end: (5, 24), text: `myFunction`
    capture: func, start: (5, 0), end: (10, 1)
```

파일별로 그룹화된 `Map<string, RawMatch[]>`를 반환한다.

### 6.6 심볼 구축

`buildSymbols()`가 RawMatch 배열을 CodeSymbol 트리로 변환한다:

```typescript
export interface CodeSymbol {
  name: string;
  kind: "function" | "class" | "method" | "interface" | "type" | "const" | "variable" |
        "export" | "struct" | "enum" | "trait" | "impl" | "property" | "getter" | "setter";
  signature: string;
  jsdoc?: string;
  lineStart: number;
  lineEnd: number;
  parent?: string;
  exported: boolean;
  children?: CodeSymbol[];
}
```

처리 단계:
1. export 범위와 import 문을 먼저 수집
2. 각 매치에서 kind와 name 캡처를 추출
3. `extractSignatureFromLines()` -- 첫 줄 또는 `{` 직전까지의 시그니처 추출 (최대 200자)
4. `findCommentAbove()` -- 선행 JSDoc/주석 수집
5. `findPythonDocstringFromLines()` -- Python docstring 추출
6. `isExported()` -- 언어별 export 판단:
   - JS/TS: export 범위 내에 있는지
   - Python: `_`로 시작하지 않는지
   - Go: 대문자로 시작하는지
   - Rust: `pub`으로 시작하는지
7. 컨테이너(class, struct, impl, trait) 내의 심볼을 children으로 중첩

### 6.7 parseFile -- 단일 파일 파싱

```typescript
export function parseFile(content: string, filePath: string): FoldedFile
```

1. 언어 감지
2. 문법 경로 해결 (없으면 빈 결과 반환)
3. 임시 파일에 소스 쓰기 (올바른 확장자 포함)
4. tree-sitter 쿼리 실행
5. 심볼 구축
6. 접힌 뷰 포매팅 -> 토큰 추정 (`folded.length / 4`)
7. 임시 디렉토리 정리 (`rmSync`)

### 6.8 parseFilesBatch -- 배치 파싱

```typescript
export function parseFilesBatch(
  files: Array<{ absolutePath: string; relativePath: string; content: string }>
): Map<string, FoldedFile>
```

**핵심 최적화**: 언어별로 파일을 그룹화하여 언어당 1회의 CLI 호출만 수행한다. 파일당 1회 호출 대비 큰 성능 향상이다.

### 6.9 접힌 뷰 포매팅

`formatFoldedView()`가 파일의 구조적 개요를 생성한다:

```
[파일 아이콘] src/example.ts (typescript, 150 lines)

  [패키지 아이콘] Imports: 5 statements
    import { foo } from './bar'
    ...

  f myFunction [exported] (L10-25)
    export function myFunction(arg: string): boolean
    [주석 아이콘] Checks if the argument is valid

  [클래스 아이콘] MyClass [exported] (L30-100)
    export class MyClass
    f constructor (L31-35)
    f doSomething (L37-50)
```

심볼 아이콘 매핑: function/method -> `f`, class/struct -> `[다이아몬드]`, interface/type/trait -> `[빈 다이아몬드]`, enum -> `[채워진 사각]`, impl -> `[보석]`

### 6.10 unfoldSymbol -- 심볼 펼치기

```typescript
export function unfoldSymbol(content: string, filePath: string, symbolName: string): string | null
```

1. `parseFile()`로 파일 파싱
2. 재귀적으로 심볼 트리를 탐색하여 이름 매칭
3. 선행 주석/데코레이터까지 포함하여 시작 행 조정
4. 슬라이스된 소스 코드를 `// [위치 아이콘] {path} L{start}-{end}` 헤더와 함께 반환

---

## 7. 스마트 파일 읽기 -- 검색 (search.ts, 316줄)

search.ts는 코드베이스에서 심볼을 찾는 검색 모듈이다. grep 스타일과 구조적 검색을 결합한다.

### 7.1 파일 수집

`walkDir()`가 디렉토리를 재귀적으로 순회하며 코드 파일을 yield한다:

**지원 확장자**: `.js`, `.jsx`, `.ts`, `.tsx`, `.mjs`, `.cjs`, `.py`, `.pyw`, `.go`, `.rs`, `.rb`, `.java`, `.cs`, `.cpp`, `.c`, `.h`, `.hpp`, `.swift`, `.kt`, `.php`, `.vue`, `.svelte`

**무시 디렉토리**: `node_modules`, `.git`, `dist`, `build`, `.next`, `__pycache__`, `.venv`, `venv`, `env`, `.env`, `target`, `vendor`, `.cache`, `.turbo`, `coverage`, `.nyc_output`, `.claude`, `.smart-file-read`

**제한 사항**:
- 최대 깊이: 20
- 최대 파일 크기: 512KB
- 바이너리 파일 건너뛰기 (처음 1000자에 null 바이트 포함 시)
- `.`으로 시작하는 항목 건너뛰기

### 7.2 검색 알고리즘 (searchCodebase)

3단계로 진행된다:

**Phase 1: 파일 수집**
- `walkDir()`로 파일 경로 수집
- `filePattern` 옵션이 있으면 상대 경로에 대한 부분 문자열 필터링
- `safeReadFile()`로 내용 읽기

**Phase 2: 배치 파싱**
- `parseFilesBatch()`로 한 번에 파싱 (언어당 1회 CLI 호출)

**Phase 3: 쿼리 매칭**

쿼리를 `[\s_\-./]+`로 분할하여 파트별로 매칭한다:

```typescript
const queryParts = queryLower.split(/[\s_\-./]+/).filter(p => p.length > 0);
```

매칭 점수 체계:
- **파일 경로**: `matchScore(relativePath, queryParts)` > 0이면 파일 포함
- **심볼 이름**: `nameScore * 3` (이름 매칭에 가중치)
- **시그니처**: 쿼리 문자열 포함 시 +2
- **JSDoc**: 쿼리 문자열 포함 시 +1

`matchScore()` 함수의 세 가지 매칭 수준:
- 정확 매칭: +10
- 부분 문자열 매칭: +5
- 퍼지 매칭 (모든 문자가 순서대로 등장): +1

결과는 관련성 점수로 정렬 후 `maxResults`로 잘라낸다.

### 7.3 결과 포맷

`formatSearchResults()`가 LLM 소비에 최적화된 텍스트를 생성한다:

```
[검색 아이콘] Smart Search: "shutdown"
   Scanned 150 files, found 1200 symbols
   14 matches across 7 files (~3500 tokens for folded view)

-- Matching Symbols --

  function performGracefulShutdown (services/GracefulShutdown.ts:56)
    export async function performGracefulShutdown(services: Service[]): Promise<void>
    [주석 아이콘] Gracefully stops all services in reverse order

-- Folded File Views --

  [파일 아이콘] services/GracefulShutdown.ts (typescript, 120 lines)
    ...

-- Actions --
  To see full implementation: use smart_unfold with file path and symbol name
```

---

## 8. 세션 큐 프로세서 (SessionQueueProcessor.ts, 123줄)

SessionQueueProcessor는 세션별 메시지 큐를 처리하는 비동기 이터레이터를 생성한다.

### 8.1 핵심 메커니즘

```typescript
async *createIterator(options: CreateIteratorOptions): AsyncIterableIterator<PendingMessageWithId>
```

이벤트 기반 소비 루프:

1. `store.claimNextMessage(sessionDbId)` -- 원자적으로 다음 메시지를 claim (DB에서 `processing` 상태로 마킹)
2. 메시지가 있으면 yield 후 루프 계속
3. 메시지가 없으면 `waitForMessage()` -- EventEmitter의 `'message'` 이벤트 대기
4. 타임아웃(`IDLE_TIMEOUT_MS = 3분`) 후 idle 감지

### 8.2 Idle 타임아웃

3분간 메시지가 없으면 `onIdleTimeout` 콜백을 호출한다. 이 콜백은 `abortController.abort()`를 트리거하여 SDK 서브프로세스를 종료해야 한다.

코드 주석에서 강조: "Just returning from the iterator is NOT enough -- the subprocess stays alive!" 이터레이터 종료만으로는 서브프로세스가 죽지 않으므로, 반드시 abort를 통해 명시적으로 종료해야 한다.

### 8.3 Claim-Confirm 패턴

`claimNextMessage()`가 메시지를 `processing` 상태로 전환하고, ResponseProcessor에서 저장 성공 후 `confirmProcessed()`로 최종 삭제한다. 이 2단계 패턴은:
- 중복 처리 방지 (다른 워커가 같은 메시지를 claim하지 않음)
- 크래시 복구 (processing 상태 메시지는 stale 복구 대상)

### 8.4 waitForMessage

```typescript
private waitForMessage(signal: AbortSignal, timeoutMs: number): Promise<boolean>
```

세 가지 이벤트를 경쟁시킨다:
1. `events.once('message', onMessage)` -- 새 메시지 도착 -> `true` 반환
2. `signal.addEventListener('abort', onAbort)` -- 세션 중단 -> `false` 반환
3. `setTimeout(onTimeout, timeoutMs)` -- 타임아웃 -> `false` 반환

`cleanup()` 함수가 모든 리스너를 정리하여 메모리 누수를 방지한다.

---

## 9. UI 뷰어 (plugin/ui/)

### 9.1 viewer.html

웹 기반 세션 뷰어의 HTML 엔트리 포인트이다. 주요 특성:

- **테마 지원**: CSS 변수로 Light/Dark 모드를 정의한다. `:root` 아래에 50개 이상의 CSS 변수가 정의되어 색상, 배경, 테두리, 타이포그래피를 제어한다.
- **커스텀 폰트**: `Monaspace Radon` 가변 폰트 (woff2-variations) 사용
- **카드 분류**: 관찰(observation), 요약(summary), 프롬프트(prompt) 각각에 대해 별도의 배경색과 테두리색이 정의된다:
  - 관찰: `--color-bg-observation: #f0f6fb`, `--color-border-observation: #0969da`
  - 요약: `--color-bg-summary: #fffbf0`, `--color-border-summary: #d4a72c`
  - 프롬프트: `--color-bg-prompt: #f6f3fb`, `--color-border-prompt: #8250df`

### 9.2 viewer-bundle.js

번들된 JavaScript 파일로, 실제 뷰어 로직을 포함한다. Worker API에서 세션 데이터를 가져와 타임라인 형태로 표시하며, SSE를 통해 실시간 업데이트를 수신한다.

### 9.3 정적 자산

```
plugin/ui/
  viewer.html                          -- 메인 HTML
  viewer-bundle.js                     -- 번들된 JS
  claude-mem-logo-for-dark-mode.webp   -- 다크 모드 로고
  claude-mem-logomark.webp             -- 로고마크 (파비콘용)
  icon-thick-completed.svg             -- 완료 아이콘
  icon-thick-investigated.svg          -- 조사 아이콘
  icon-thick-learned.svg               -- 학습 아이콘
  icon-thick-next-steps.svg            -- 다음 단계 아이콘
  assets/                              -- 폰트 등 추가 자산
```

미들웨어의 `express.static(uiDir)`이 이 디렉토리를 서빙하므로, Worker HTTP 서버의 루트 경로(`/`)에서 뷰어에 접근할 수 있다.
