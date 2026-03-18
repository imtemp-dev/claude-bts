# claude-mem -- 동기화 및 통합 분석

## 1. Chroma 벡터 동기화 (ChromaSync.ts, 812줄)

ChromaSync는 SQLite에 저장된 관찰(observation)과 세션 요약(summary)을 ChromaDB 벡터 데이터베이스에 실시간으로 동기화하는 서비스이다. 시맨틱 검색 기능의 기반이 된다.

### 1.1 설계 원칙

- **MCP 기반 통신**: ChromaMcpManager를 통해 chroma-mcp 서버와 stdio MCP 프로토콜로 통신. chromadb npm 패키지나 ONNX/WASM 의존성이 불필요하다.
- **Fail-fast**: Chroma가 사용 불가하면 동기화가 실패한다. 폴백이 없으며, 호출자(ResponseProcessor)가 fire-and-forget 패턴으로 실패를 흡수한다.
- **세밀한 문서 분할(Granular Approach)**: 하나의 관찰을 여러 Chroma 문서로 분할하여 벡터 검색 정밀도를 높인다.

### 1.2 컬렉션 관리

생성자에서 프로젝트명을 정규화하여 컬렉션명을 결정한다:

```typescript
constructor(project: string) {
  const sanitized = project
    .replace(/[^a-zA-Z0-9._-]/g, '_')
    .replace(/[^a-zA-Z0-9]+$/, '');
  this.collectionName = `cm__${sanitized || 'unknown'}`;
}
```

Chroma 컬렉션명은 `[a-zA-Z0-9._-]` 문자만 허용하며, 3-512자 길이, 알파뉴메릭으로 시작/종료해야 한다. 접두사 `cm__`로 다른 Chroma 사용자와의 충돌을 방지한다.

`ensureCollectionExists()`는 `chroma_create_collection` MCP 도구를 호출하며, 멱등성(idempotent)을 보장한다. 한 번 생성되면 `collectionCreated` 플래그로 중복 호출을 방지한다.

### 1.3 문서 포맷 -- 관찰

`formatObservationDocs()`는 하나의 관찰을 여러 Chroma 문서로 분할한다:

| 문서 ID 패턴 | 소스 필드 | 설명 |
|-------------|-----------|------|
| `obs_{id}_narrative` | narrative | 전체 내러티브 텍스트 |
| `obs_{id}_text` | text | 레거시 텍스트 필드 |
| `obs_{id}_fact_{index}` | facts 배열 | 각 fact가 별도 문서 |

공통 메타데이터:
```typescript
{
  sqlite_id: obs.id,
  doc_type: 'observation',
  memory_session_id: obs.memory_session_id,
  project: obs.project,
  created_at_epoch: obs.created_at_epoch,
  type: obs.type || 'discovery',
  title: obs.title || 'Untitled',
  subtitle?: obs.subtitle,
  concepts?: concepts.join(','),
  files_read?: files_read.join(','),
  files_modified?: files_modified.join(',')
}
```

각 fact가 별도 벡터 문서가 되므로, "SQLite가 WAL 모드에서 동시 쓰기를 지원한다"와 같은 개별 사실에 대한 시맨틱 검색이 가능하다.

### 1.4 문서 포맷 -- 요약

`formatSummaryDocs()`는 요약의 각 필드를 별도 문서로 분할한다:

| 문서 ID 패턴 | 소스 필드 |
|-------------|-----------|
| `summary_{id}_request` | request |
| `summary_{id}_investigated` | investigated |
| `summary_{id}_learned` | learned |
| `summary_{id}_completed` | completed |
| `summary_{id}_next_steps` | next_steps |
| `summary_{id}_notes` | notes |

공통 메타데이터에 `doc_type: 'session_summary'`가 포함된다.

### 1.5 문서 포맷 -- 사용자 프롬프트

`formatUserPromptDoc()`는 사용자 프롬프트를 단일 문서로 저장한다:

```typescript
{
  id: `prompt_${prompt.id}`,
  document: prompt.prompt_text,
  metadata: {
    sqlite_id: prompt.id,
    doc_type: 'user_prompt',
    memory_session_id, project, created_at_epoch, prompt_number
  }
}
```

### 1.6 배치 추가

`addDocuments()`는 `BATCH_SIZE = 100`으로 문서를 분할하여 `chroma_add_documents` MCP 도구를 호출한다. 메타데이터 정제 단계에서 null, undefined, 빈 문자열 값을 필터링한다:

```typescript
const cleanMetadatas = batch.map(d =>
  Object.fromEntries(
    Object.entries(d.metadata).filter(([_, v]) => v !== null && v !== undefined && v !== '')
  )
);
```

하나의 배치가 실패해도 나머지 배치는 계속 처리된다.

### 1.7 실시간 동기화

두 메서드가 ResponseProcessor에서 호출된다:

- `syncObservation()`: ParsedObservation을 StoredObservation 형식으로 변환 후 문서 포맷팅, 추가
- `syncSummary()`: ParsedSummary를 StoredSummary 형식으로 변환 후 문서 포맷팅, 추가

두 메서드 모두 동기적으로 완료를 기다리며(`await`), 호출자가 fire-and-forget 패턴으로 에러를 흡수한다.

### 1.8 시맨틱 검색

`queryChroma()`는 MCP의 `chroma_query_documents` 도구를 호출한다:

```typescript
async queryChroma(query: string, limit: number, whereFilter?: Record<string, any>)
```

결과 처리에서 중요한 중복 제거 로직이 있다:

```
obs_123_narrative, obs_123_fact_0, obs_123_fact_1
```

동일한 SQLite ID(123)에 매핑되는 여러 Chroma 문서 중 가장 높은 랭크(첫 번째 등장)의 것만 유지한다. 세 가지 ID 패턴을 지원한다:
- `obs_{id}_*` -- 관찰
- `summary_{id}_*` -- 세션 요약
- `prompt_{id}` -- 사용자 프롬프트

연결 에러(ECONNREFUSED, ENOTFOUND, fetch failed, subprocess closed, timed out) 감지 시 `collectionCreated` 플래그를 리셋하여 다음 호출에서 재연결을 시도한다.

### 1.9 스마트 백필 (Smart Backfill)

`ensureBackfilled()`는 SQLite에는 존재하지만 Chroma에는 없는 데이터를 찾아 동기화한다:

1. `getExistingChromaIds()`로 Chroma에 이미 존재하는 SQLite ID 세트를 수집 (관찰, 요약, 프롬프트 별도)
2. SQLite에서 `NOT IN (기존ID목록)` 조건으로 누락된 레코드만 조회
3. 배치로 Chroma에 추가

SQL 쿼리에서 `existingObsIds`를 직접 보간하기 전에 `Number.isInteger(id) && id > 0` 검증을 수행하여 SQL 인젝션을 방지한다.

### 1.10 전체 프로젝트 백필

`backfillAllProjects()` 정적 메서드는 워커 시작 시 fire-and-forget으로 호출된다:

```typescript
static async backfillAllProjects(): Promise<void>
```

모든 프로젝트를 순회하며, 하나의 프로젝트 실패가 다른 프로젝트의 처리를 중단하지 않는다. 공유 ChromaSync 인스턴스(`new ChromaSync('claude-mem')`)와 단일 Chroma 연결을 사용한다.

---

## 2. Chroma MCP 관리자 (ChromaMcpManager.ts, 478줄)

ChromaMcpManager는 chroma-mcp 서버와의 MCP stdio 연결을 관리하는 싱글톤이다.

### 2.1 연결 관리

**지연 연결(Lazy Connection)**: 첫 `callTool()` 호출 시 연결을 시작한다. 이후 연결은 워커 수명 동안 유지된다.

**연결 잠금(Connection Lock)**: `this.connecting` Promise를 통해 동시 연결 시도를 방지한다:

```typescript
if (this.connecting) {
  await this.connecting;  // 기존 연결 시도 대기
  return;
}
this.connecting = this.connectInternal();
```

**백오프(Backoff)**: 연결 실패 후 `RECONNECT_BACKOFF_MS = 10_000`ms 동안 재연결을 차단한다.

### 2.2 서브프로세스 생성

`connectInternal()`에서 `uvx chroma-mcp`를 stdio 서브프로세스로 생성한다:

- **로컬 모드**: `uvx --python {version} chroma-mcp --client-type persistent --data-dir ~/.claude-mem/chroma`
- **원격 모드**: `uvx --python {version} chroma-mcp --client-type http --host {host} --port {port}` + SSL, tenant, database, API key 옵션

**Windows 호환성**: Windows에서 `.cmd` 파일은 shell 해결이 필요하므로 `cmd.exe /c uvx ...`로 라우팅한다. Git Bash 호환성(Issue #1062)도 이 방식으로 해결된다.

**연결 타임아웃**: `MCP_CONNECTION_TIMEOUT_MS = 30_000`ms. `Promise.race()`로 연결과 타임아웃을 경쟁시킨다. 타임아웃 시 서브프로세스를 즉시 종료하여 좀비 방지.

### 2.3 자동 재연결

`transport.onclose` 핸들러가 서브프로세스 종료를 감지하면:
1. `this.connected = false`
2. Supervisor에서 프로세스 등록 해제
3. client와 transport를 null로 설정
4. `lastConnectionFailureTimestamp` 갱신 (백오프 적용)

**참조 가드(Reference Guard)**: 이전 transport의 stale onclose 핸들러가 현재 연결을 덮어쓰는 레이스 컨디션을 방지하기 위해, 핸들러 등록 시 `currentTransport` 참조를 캡처하여 비교한다.

### 2.4 도구 호출 (`callTool`)

```typescript
async callTool(toolName: string, toolArguments: Record<string, unknown>): Promise<unknown>
```

1. `ensureConnected()`로 연결 보장
2. `client.callTool()` 호출
3. **Transport 에러 시 1회 재시도**: 서브프로세스가 죽었을 수 있으므로 (orphan reaper, HNSW 인덱스 손상 등), 연결을 리셋하고 1회 재시도 (Issue #1131)
4. `result.isError` 확인 -- MCP 도구는 에러를 isError 플래그로 시그널링
5. 응답 텍스트를 JSON 파싱 시도. 실패하면 void 성공 메시지로 간주하고 null 반환

### 2.5 헬스 체크

```typescript
async isHealthy(): Promise<boolean>
```

`chroma_list_collections({ limit: 1 })`을 호출하여 연결 상태를 확인한다.

### 2.6 기업 프록시 지원 (Zscaler)

`getCombinedCertPath()`는 macOS에서 Zscaler 인증서를 Python certifi CA 번들과 결합한다:

1. `uvx --with certifi python -c "import certifi; print(certifi.where())"` -- certifi 경로 획득
2. `security find-certificate -a -c "Zscaler" -p /Library/Keychains/System.keychain` -- Zscaler 인증서 추출
3. 두 파일을 결합하여 `~/.claude-mem/combined_certs.pem`에 저장 (24시간 캐시)

결합된 인증서는 서브프로세스 환경 변수로 주입된다:
- `SSL_CERT_FILE`
- `REQUESTS_CA_BUNDLE`
- `CURL_CA_BUNDLE`
- `NODE_EXTRA_CA_CERTS`

### 2.7 프로세스 등록

`registerManagedProcess()`가 Supervisor에 chroma-mcp 프로세스를 등록한다:

```typescript
getSupervisor().registerProcess('chroma-mcp', {
  pid: chromaProcess.pid,
  type: 'chroma',
  startedAt: new Date().toISOString()
}, chromaProcess);
```

프로세스 종료 시 자동으로 등록 해제된다. 이를 통해 Doctor 엔드포인트에서 chroma-mcp 상태를 모니터링할 수 있다.

### 2.8 환경 정제

`getSpawnEnv()`에서 `sanitizeEnv(process.env)`를 호출하여 CLAUDECODE_* 등의 민감한 환경 변수를 제거한 후 서브프로세스에 전달한다.

---

## 3. Cursor IDE 통합 (CursorHooksInstaller.ts, 675줄)

CursorHooksInstaller는 Cursor IDE와의 통합을 관리하는 모듈이다. 훅 설치/제거, MCP 서버 설정, 컨텍스트 파일 생성을 담당한다.

### 3.1 설치 대상 (Install Target)

세 가지 설치 수준을 지원한다:

| 대상 | 경로 | 범위 |
|------|------|------|
| `project` | `{cwd}/.cursor` | 현재 프로젝트만 |
| `user` | `~/.cursor` | 사용자 전역 |
| `enterprise` | OS별 시스템 경로 | 전사적 적용 |

Enterprise 경로:
- macOS: `/Library/Application Support/Cursor`
- Linux: `/etc/cursor`
- Windows: `%ProgramData%\Cursor`

### 3.2 hooks.json 생성

`installCursorHooks()`가 통합 CLI 모드의 hooks.json을 생성한다:

```json
{
  "version": 1,
  "hooks": {
    "beforeSubmitPrompt": [
      { "command": "\"bun\" \"worker-service.cjs\" hook cursor session-init" },
      { "command": "\"bun\" \"worker-service.cjs\" hook cursor context" }
    ],
    "afterMCPExecution": [
      { "command": "\"bun\" \"worker-service.cjs\" hook cursor observation" }
    ],
    "afterShellExecution": [
      { "command": "\"bun\" \"worker-service.cjs\" hook cursor observation" }
    ],
    "afterFileEdit": [
      { "command": "\"bun\" \"worker-service.cjs\" hook cursor file-edit" }
    ],
    "stop": [
      { "command": "\"bun\" \"worker-service.cjs\" hook cursor summarize" }
    ]
  }
}
```

Bun 런타임이 필요한 이유는 `worker-service.cjs`가 `bun:sqlite`를 사용하기 때문이다. `findBunPath()`가 다음 순서로 Bun을 탐색한다:
1. `~/.bun/bin/bun` (표준 사용자 설치)
2. `/usr/local/bin/bun`
3. `/usr/bin/bun`
4. Windows: `~/.bun/bin/bun.exe`, `%LOCALAPPDATA%\bun\bun.exe`
5. 폴백: `'bun'` (PATH에서 해결)

### 3.3 프로젝트 레지스트리

`cursor-projects.json` 파일로 Cursor 프로젝트를 추적한다:

```typescript
export interface CursorProjectRegistry {
  [projectName: string]: {
    workspacePath: string;
    installedAt: string;
  };
}
```

`registerCursorProject()`와 `unregisterCursorProject()`로 프로젝트를 등록/해제한다. 이 레지스트리는 요약 생성 후 자동 컨텍스트 업데이트의 대상을 결정한다.

### 3.4 컨텍스트 자동 업데이트

`updateCursorContextForProject()`가 ResponseProcessor의 요약 브로드캐스트 후에 호출된다:

1. 프로젝트 레지스트리에서 해당 프로젝트 조회
2. Worker HTTP API의 `/api/context/inject?project={name}`에서 최신 컨텍스트 가져오기
3. `writeContextFile()`로 `.cursor/rules/claude-mem-context.mdc`에 기록

이 파일은 Cursor의 Rules 시스템을 통해 매 채팅에 자동 포함된다. `alwaysApply: true` 프론트매터로 항상 적용됨을 보장한다.

### 3.5 MCP 서버 설정

`configureCursorMcp()`가 `.cursor/mcp.json`에 claude-mem MCP 서버를 등록한다:

```json
{
  "mcpServers": {
    "claude-mem": {
      "command": "node",
      "args": ["/path/to/mcp-server.cjs"]
    }
  }
}
```

기존 `mcp.json`이 있으면 파싱하여 `claude-mem` 항목만 추가/갱신한다. 파싱 실패 시 새로운 설정으로 대체한다.

### 3.6 스크립트 경로 탐색

`findMcpServerPath()`와 `findWorkerServicePath()`가 다음 위치를 순서대로 확인한다:
1. 마켓플레이스 설치: `~/.claude/plugins/marketplaces/thedotmack/plugin/scripts/`
2. 개발/소스 위치: `__filename` 기준 상대 경로
3. 대안 개발 위치: `process.cwd()/plugin/scripts/`

### 3.7 제거

`uninstallCursorHooks()`는:
1. 레거시 셸 스크립트 제거 (`.sh`, `.ps1` 파일들)
2. `hooks.json` 제거
3. 프로젝트 수준이면 컨텍스트 파일(`.cursor/rules/claude-mem-context.mdc`) 제거, 레지스트리에서 해제

### 3.8 상태 확인

`checkCursorHooksStatus()`가 project, user, enterprise 수준을 순회하며 설치 상태를 확인한다. 통합 CLI 모드(`worker-service.cjs hook cursor`)인지 레거시 셸 스크립트 모드인지도 판별한다.

### 3.9 Claude Code 감지

`detectClaudeCode()`가 Claude Code CLI 존재 여부를 확인한다:
1. `which claude || where claude` 실행
2. `$CLAUDE_CONFIG_DIR/plugins` 디렉토리 존재 확인

---

## 4. 통합 인터페이스 (integrations/types.ts)

통합 모듈의 타입 정의:

```typescript
export interface CursorMcpConfig {
  mcpServers: {
    [name: string]: {
      command: string;
      args?: string[];
      env?: Record<string, string>;
    };
  };
}

export type CursorInstallTarget = 'project' | 'user' | 'enterprise';
export type Platform = 'windows' | 'unix';

export interface CursorHooksJson {
  version: number;
  hooks: {
    beforeSubmitPrompt?: Array<{ command: string }>;
    afterMCPExecution?: Array<{ command: string }>;
    afterShellExecution?: Array<{ command: string }>;
    afterFileEdit?: Array<{ command: string }>;
    stop?: Array<{ command: string }>;
  };
}
```

`CursorHooksJson`은 Cursor IDE의 hooks.json 스키마를 정의한다. 5개의 훅 포인트가 있으며, 각 훅은 실행할 커맨드의 배열이다.

`integrations/index.ts`는 `types.ts`와 `CursorHooksInstaller.ts`를 단순 re-export한다.

---

## 5. 모드 관리자 (ModeManager.ts, 254줄)

ModeManager는 모드 프로파일을 로드하고 관리하는 싱글톤이다. 모드는 관찰 타입, 컨셉, 프롬프트 템플릿을 정의하여 claude-mem을 다양한 도메인에 적용할 수 있게 한다.

### 5.1 싱글톤 패턴

```typescript
static getInstance(): ModeManager {
  if (!ModeManager.instance) {
    ModeManager.instance = new ModeManager();
  }
  return ModeManager.instance;
}
```

생성자에서 `plugin/modes/` 디렉토리를 탐색한다. 프로덕션과 개발 환경 모두를 지원하기 위해 두 경로를 확인한다:
- `{packageRoot}/modes` (프로덕션)
- `{packageRoot}/../plugin/modes` (개발)

### 5.2 상속 패턴 (Inheritance)

모드 ID에 `--` 구분자가 있으면 상속으로 처리한다:

```
code--ko  -->  parent: code,  override: code--ko
```

1단계 상속만 지원하며 (`code--ko--verbose`는 에러), 처리 순서는:

1. `parseInheritance(modeId)` -- `--`로 분할하여 부모/오버라이드 ID 추출
2. `loadMode(parentId)` -- 부모 모드를 재귀적으로 로드
3. `loadModeFile(overrideId)` -- 오버라이드 파일 로드
4. `deepMerge(parentMode, overrideConfig)` -- 깊은 병합

### 5.3 깊은 병합 (Deep Merge)

`deepMerge()` 메서드의 규칙:
- **중첩 객체**: 재귀적으로 병합
- **배열**: 완전 교체 (병합하지 않음)
- **프리미티브**: 오버라이드 값으로 교체

이 규칙 덕분에 한국어 오버라이드(`code--ko`)는 `prompts` 객체의 특정 필드만 교체하면서 나머지(observation_types, observation_concepts 등)는 부모로부터 상속한다.

### 5.4 폴백 체인

모드 로딩 실패 시:
1. 요청된 모드 파일이 없으면 `'code'`로 폴백
2. 부모 모드가 없으면 `'code'`로 폴백
3. 오버라이드 파일이 없으면 부모 모드만 사용
4. `'code'` 자체가 없으면 critical 에러 throw

### 5.5 유틸리티 메서드

```typescript
getActiveMode(): ModeConfig          // 현재 활성 모드 반환 (미로드 시 에러)
getObservationTypes(): ObservationType[]   // 활성 모드의 관찰 타입 목록
getObservationConcepts(): ObservationConcept[]  // 활성 모드의 컨셉 목록
getTypeIcon(typeId: string): string    // 타입의 이모지 아이콘
getWorkEmoji(typeId: string): string   // 타입의 작업 이모지
validateType(typeId: string): boolean  // 타입 ID 유효성 검증
getTypeLabel(typeId: string): string   // 타입의 표시 라벨
```

---

## 6. 모드 정의 (plugin/modes/ JSON 구조)

### 6.1 ModeConfig 타입

```typescript
export interface ModeConfig {
  name: string;
  description: string;
  version: string;
  observation_types: ObservationType[];
  observation_concepts: ObservationConcept[];
  prompts: ModePrompts;
}
```

### 6.2 code.json -- 소프트웨어 개발 모드 (기본)

**관찰 타입 6종**:

| ID | 라벨 | 설명 | 아이콘 |
|----|------|------|--------|
| `bugfix` | Bug Fix | 수정된 버그 | 빨강 |
| `feature` | Feature | 새로운 기능 | 보라 |
| `refactor` | Refactor | 구조 개선 | 순환 |
| `change` | Change | 기타 변경 | 초록 |
| `discovery` | Discovery | 시스템 학습 | 파랑 |
| `decision` | Decision | 설계 결정 | 저울 |

**관찰 컨셉 7종**: `how-it-works`, `why-it-exists`, `what-changed`, `problem-solution`, `gotcha`, `pattern`, + (추가 있음)

### 6.3 law-study.json -- 법학 공부 모드

**관찰 타입 6종**:

| ID | 라벨 | 설명 |
|----|------|------|
| `case-holding` | Case Holding | 판례 요약 (사실 + 판시) + 법적 규칙 추출 |
| `issue-pattern` | Issue Pattern | 시험 트리거 또는 법적 쟁점 식별 패턴 |
| `prof-framework` | Prof Framework | 교수의 분석 렌즈, 강조점 |
| `doctrine-rule` | Doctrine / Rule | 법적 테스트, 기준, 법리 |
| `argument-structure` | Argument Structure | 법적 논증 구조 |
| `cross-case-connection` | Cross-Case Connection | 다수 판례/법리를 연결하는 통찰 |

**관찰 컨셉 6종**: `exam-relevant`, `minority-position`, `gotcha`, `unsettled-law`, `policy-rationale`, `course-theme`

법학 모드는 코드 모드와 완전히 다른 관찰 분류 체계를 가진다. 동일한 XML 구조(`<observation>`, `<summary>`)를 사용하지만, 타입과 컨셉이 법학 도메인에 특화되어 있다.

### 6.4 email-investigation.json -- 이메일 조사 모드

RAGTIME 스타일의 이메일 사기 조사를 위한 모드이다.

**관찰 타입 6종**: `entity` (인물/조직 발견), `relationship` (관계 발견), `timeline-event` (시간순 이벤트), `evidence` (증거), `anomaly` (이상 패턴), `conclusion` (조사 결론)

**관찰 컨셉 6종**: `who`, `when`, `what-happened`, `motive`, `red-flag`, `corroboration`

### 6.5 언어 변형 (27+ 파일)

`code--{locale}.json` 패턴으로 30개 가까운 언어 변형이 존재한다:

`ar`, `bn`, `cs`, `da`, `de`, `el`, `es`, `fi`, `fr`, `he`, `hi`, `hu`, `id`, `it`, `ja`, `ko`, `nl`, `no`, `pl`, `pt-br`, `ro`, `ru`, `sv`, `th`, `tr`, `uk`, `ur`, `vi`, `zh`

각 변형 파일은 `prompts` 객체의 일부 필드만 오버라이드한다. 예를 들어 `code--ko.json`의 구조:

```json
{
  "name": "Code Development (Korean)",
  "prompts": {
    "footer": "... LANGUAGE REQUIREMENTS: Please write the observation data in 한국어",
    "xml_title_placeholder": "[**title**: 핵심 작업이나 주제를 포착하는 짧은 제목]",
    "xml_subtitle_placeholder": "[**subtitle**: 한 문장 설명 (최대 24단어)]",
    ...
    "continuation_instruction": "... LANGUAGE REQUIREMENTS: Please write the observation data in 한국어",
    "summary_footer": "... LANGUAGE REQUIREMENTS: Please write ALL summary content ... in 한국어"
  }
}
```

XML 플레이스홀더와 언어 지시(LANGUAGE REQUIREMENTS)만 해당 언어로 번역되고, 나머지 시스템 지시, 관찰 타입, 컨셉은 부모 `code.json`에서 상속된다.

### 6.6 성격 변형 -- code--chill.json

`code--chill.json`은 언어가 아닌 **관찰 밀도**를 변경하는 변형이다:

```json
{
  "name": "Code Development (Chill)",
  "prompts": {
    "recording_focus": "WHAT TO RECORD (SELECTIVE MODE)\n... Only record work that would be painful to rediscover...",
    "skip_guidance": "WHEN TO SKIP (BE LIBERAL)\n... When in doubt, skip it. Less is more..."
  }
}
```

`recording_focus`와 `skip_guidance`만 오버라이드하여 "재발견하기 고통스러운 것만 기록"하는 관대한 모드를 제공한다. `law-study--chill.json`도 동일한 패턴이다.

### 6.7 ModePrompts 인터페이스 (domain/types.ts)

45줄에 걸친 `ModePrompts` 인터페이스(types.ts:19-63)는 프롬프트의 모든 텍스트 조각을 정의한다:

- **시스템 정의**: `system_identity`, `language_instruction?`, `observer_role`
- **관찰 지침**: `recording_focus`, `skip_guidance`, `type_guidance`, `concept_guidance`, `field_guidance`
- **출력 형식**: `output_format_header`, `format_examples`, `footer`
- **XML 플레이스홀더** (관찰 6개 + 요약 6개): `xml_title_placeholder`, `xml_summary_request_placeholder` 등
- **섹션 헤더** 3개: `header_memory_start`, `header_memory_continued`, `header_summary_checkpoint`
- **컨티뉴에이션** 2개: `continuation_greeting`, `continuation_instruction`
- **요약** 4개: `summary_instruction`, `summary_context_label`, `summary_format_instruction`, `summary_footer`
- **공간 인식**: `spatial_awareness` (작업 디렉토리 컨텍스트)

---

## 7. 스킬 시스템 (plugin/skills/)

claude-mem은 4개의 SKILL.md 파일을 제공하여 Claude Code의 slash command 시스템과 통합된다.

### 7.1 do (실행)

```yaml
name: do
description: Execute a phased implementation plan using subagents.
```

ORCHESTRATOR 패턴으로 서브에이전트를 배치하여 구현 계획을 실행한다. 각 단계(phase)마다:
1. **Implementation** 서브에이전트 -- 구현 실행
2. **Verification** 서브에이전트 -- 검증 체크리스트 실행
3. **Anti-pattern** 서브에이전트 -- 안티패턴 검색
4. **Code Quality** 서브에이전트 -- 코드 리뷰
5. **Commit** 서브에이전트 -- 검증 통과 시에만 커밋
6. **Branch/Sync** 서브에이전트 -- 브랜치 푸시, 다음 단계 핸드오프

핵심 원칙: "Don't invent APIs that 'should' exist -- verify against docs"

### 7.2 make-plan (계획 수립)

```yaml
name: make-plan
description: Create a detailed, phased implementation plan with documentation discovery.
```

서브에이전트를 사실 수집(fact-gathering)에 활용하고, 합성(synthesis)과 계획 작성은 오케스트레이터가 수행한다. Phase 0(Documentation Discovery)을 항상 먼저 실행하여 사용 가능한 API를 확인한다.

서브에이전트 보고 계약(Reporting Contract): 소스 파일/URL, 구체적 발견물, 복사 가능한 스니펫 위치, 신뢰도 메모가 필수이다.

### 7.3 mem-search (메모리 검색)

```yaml
name: mem-search
description: Search claude-mem's persistent cross-session memory database.
```

3-Layer Workflow를 정의한다:
1. **search** -- 인덱스 조회 (ID, 제목, 날짜 반환, ~50-100 토큰/결과)
2. **timeline** -- 앵커 주변 컨텍스트 (시간순 문맥)
3. **get_observations** -- 필터링된 ID만 상세 조회 (~500-1000 토큰/결과)

"NEVER fetch full details without filtering first. 10x token savings."

### 7.4 smart-explore (스마트 탐색)

```yaml
name: smart-explore
description: Token-optimized structural code search using tree-sitter AST parsing.
```

기본 탐색 동작(Read, Grep, Glob)을 대체하는 3계층 도구:
1. **smart_search** -- 디렉토리 순회 + AST 파싱으로 심볼 검색 (~2-6k 토큰)
2. **smart_outline** -- 파일의 구조적 스켈레톤 (~1-2k 토큰)
3. **smart_unfold** -- 특정 심볼의 전체 소스 코드 (~400-2,100 토큰)

토큰 절약 효과:
- outline + unfold vs Read: **4-8x 절약**
- smart_search vs Explore agent: **11-18x 절약**
