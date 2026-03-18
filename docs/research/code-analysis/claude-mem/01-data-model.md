# claude-mem -- 데이터 모델 분석

> **분석 대상**: `src/services/sqlite/` 디렉토리 전체 및 관련 타입 정의
> **데이터베이스**: SQLite 3 (`bun:sqlite`), WAL 모드
> **경로**: `~/.claude-mem/claude-mem.db`

---

## 1. SQLite 데이터베이스

### Database.ts (360L)

`src/services/sqlite/Database.ts`는 두 개의 데이터베이스 관리 클래스를 포함한다.

#### ClaudeMemDatabase (신규 권장)

```typescript
export class ClaudeMemDatabase {
  public db: Database;
  constructor(dbPath: string = DB_PATH) { ... }
  close(): void { ... }
}
```

생성자에서 다음을 순차적으로 수행한다:

1. **데이터 디렉토리 보장** -- `ensureDir(DATA_DIR)` (`:memory:`가 아닌 경우)
2. **연결 생성** -- `new Database(dbPath, { create: true, readwrite: true })`
3. **스키마 복구** -- `repairMalformedSchemaWithReopen()` (손상된 스키마 자동 복구)
4. **PRAGMA 최적화 적용**
5. **마이그레이션 실행** -- `MigrationRunner.runAllMigrations()`

#### PRAGMA 설정

| PRAGMA | 값 | 의미 |
|--------|-----|------|
| `journal_mode` | `WAL` | Write-Ahead Logging -- 읽기/쓰기 동시성 향상 |
| `synchronous` | `NORMAL` | WAL 모드에서 안전한 수준의 동기화 (성능 최적화) |
| `foreign_keys` | `ON` | 외래 키 제약 조건 강제 |
| `temp_store` | `memory` | 임시 테이블을 메모리에 저장 |
| `mmap_size` | `268,435,456` (256MB) | 메모리 매핑 I/O 크기 |
| `cache_size` | `10,000` pages | 페이지 캐시 크기 |

#### 스키마 복구 메커니즘

`repairMalformedSchema()`는 머신 간 데이터베이스 동기화 시 발생하는 "malformed database schema" 오류를 자동 복구한다. `bun:sqlite`는 `writable_schema`를 지원하지 않으므로 Python의 `sqlite3` 모듈을 사용하여 복구한다:

1. `sqlite_master`에서 문제가 되는 객체 이름을 추출
2. 임시 Python 스크립트를 생성하여 `writable_schema = ON`으로 문제 객체 삭제
3. `schema_versions` 테이블을 초기화하여 모든 마이그레이션이 재실행되도록 함

`repairMalformedSchemaWithReopen()`은 재귀적으로 호출되어 다중 손상 객체를 처리한다.

#### DatabaseManager (레거시, deprecated)

싱글턴 패턴의 레거시 데이터베이스 관리자. `ClaudeMemDatabase`로 대체 중이다. 비동기 `initialize()`, `withTransaction()`, 마이그레이션 버전 추적을 제공한다.

#### 전역 함수

- `getDatabase()` -- 전역 DB 인스턴스 반환 (레거시 호환)
- `initializeDatabase()` -- DatabaseManager 초기화 (레거시 호환)

---

## 2. 스키마 (마이그레이션)

두 개의 마이그레이션 시스템이 공존한다.

### 레거시 마이그레이션 시스템: migrations.ts (522L)

`DatabaseManager`가 사용하는 7개의 Migration 객체로 구성된다. 각 마이그레이션은 `version` 번호, `up()` 함수, 선택적 `down()` 함수를 가진다.

#### migration001 (version 1) -- 초기 스키마

5개의 코어 테이블을 생성한다:

**sessions** -- 세션 추적 (레거시)

| 컬럼 | 타입 | 제약 | 설명 |
|------|------|------|------|
| `id` | INTEGER | PRIMARY KEY AUTOINCREMENT | |
| `session_id` | TEXT | UNIQUE NOT NULL | 세션 ID |
| `project` | TEXT | NOT NULL | 프로젝트명 |
| `created_at` | TEXT | NOT NULL | ISO 타임스탬프 |
| `created_at_epoch` | INTEGER | NOT NULL | 에포크 밀리초 |
| `source` | TEXT | NOT NULL DEFAULT 'compress' | 소스 유형 |
| `archive_path` | TEXT | | 아카이브 경로 |
| `archive_bytes` | INTEGER | | 아카이브 크기 |
| `archive_checksum` | TEXT | | 체크섬 |
| `archived_at` | TEXT | | 아카이브 시각 |
| `metadata_json` | TEXT | | JSON 메타데이터 |

인덱스: `idx_sessions_project`, `idx_sessions_created_at`, `idx_sessions_project_created`

**memories** -- 압축된 메모리 청크 (레거시)

| 컬럼 | 타입 | 제약 | 설명 |
|------|------|------|------|
| `id` | INTEGER | PRIMARY KEY AUTOINCREMENT | |
| `session_id` | TEXT | NOT NULL, FK(sessions) | 세션 참조 |
| `text` | TEXT | NOT NULL | 메모리 텍스트 |
| `document_id` | TEXT | UNIQUE | Chroma 문서 ID |
| `keywords` | TEXT | | 키워드 |
| `created_at` | TEXT | NOT NULL | ISO 타임스탬프 |
| `created_at_epoch` | INTEGER | NOT NULL | 에포크 밀리초 |
| `project` | TEXT | NOT NULL | 프로젝트명 |
| `archive_basename` | TEXT | | 아카이브 파일명 |
| `origin` | TEXT | NOT NULL DEFAULT 'transcript' | 출처 |

인덱스: `idx_memories_session`, `idx_memories_project`, `idx_memories_created_at`, `idx_memories_project_created`, `idx_memories_document_id`, `idx_memories_origin`

**overviews** -- 프로젝트별 세션 요약 (레거시)

| 컬럼 | 타입 | 제약 | 설명 |
|------|------|------|------|
| `id` | INTEGER | PRIMARY KEY AUTOINCREMENT | |
| `session_id` | TEXT | NOT NULL, FK(sessions) | 세션 참조 |
| `content` | TEXT | NOT NULL | 요약 내용 |
| `created_at` | TEXT | NOT NULL | |
| `created_at_epoch` | INTEGER | NOT NULL | |
| `project` | TEXT | NOT NULL | |
| `origin` | TEXT | NOT NULL DEFAULT 'claude' | |

인덱스: `idx_overviews_session`, `idx_overviews_project`, `idx_overviews_created_at`, `idx_overviews_project_created`, `idx_overviews_project_latest`

**diagnostics** -- 시스템 진단 정보

| 컬럼 | 타입 | 제약 | 설명 |
|------|------|------|------|
| `id` | INTEGER | PRIMARY KEY AUTOINCREMENT | |
| `session_id` | TEXT | FK(sessions) ON DELETE SET NULL | |
| `message` | TEXT | NOT NULL | 진단 메시지 |
| `severity` | TEXT | NOT NULL DEFAULT 'info' | info/warn/error |
| `created_at` | TEXT | NOT NULL | |
| `created_at_epoch` | INTEGER | NOT NULL | |
| `project` | TEXT | NOT NULL | |
| `origin` | TEXT | NOT NULL DEFAULT 'system' | |

**transcript_events** -- 원시 대화 이벤트

| 컬럼 | 타입 | 제약 | 설명 |
|------|------|------|------|
| `id` | INTEGER | PRIMARY KEY AUTOINCREMENT | |
| `session_id` | TEXT | NOT NULL, FK(sessions) | 세션 참조 |
| `project` | TEXT | | |
| `event_index` | INTEGER | NOT NULL | 이벤트 순서 |
| `event_type` | TEXT | | 이벤트 유형 |
| `raw_json` | TEXT | NOT NULL | 원시 JSON |
| `captured_at` | TEXT | NOT NULL | |
| `captured_at_epoch` | INTEGER | NOT NULL | |

UNIQUE 제약: `(session_id, event_index)`

#### migration002 (version 2) -- 계층적 메모리 필드

`memories` 테이블에 `title`, `subtitle`, `facts`, `concepts`, `files_touched` 컬럼을 추가한다.

#### migration003 (version 3) -- streaming_sessions 테이블

실시간 SDK 세션 추적을 위한 `streaming_sessions` 테이블을 생성한다.

#### migration004 (version 4) -- SDK 에이전트 아키텍처

현재 활성 스키마의 핵심 테이블을 생성한다:

**sdk_sessions** -- SDK 세션 추적

| 컬럼 | 타입 | 제약 | 설명 |
|------|------|------|------|
| `id` | INTEGER | PRIMARY KEY AUTOINCREMENT | |
| `content_session_id` | TEXT | UNIQUE NOT NULL | 사용자 관찰 세션 ID |
| `memory_session_id` | TEXT | UNIQUE | 메모리 에이전트 세션 ID |
| `project` | TEXT | NOT NULL | 프로젝트명 |
| `user_prompt` | TEXT | | 첫 사용자 프롬프트 |
| `started_at` | TEXT | NOT NULL | |
| `started_at_epoch` | INTEGER | NOT NULL | |
| `completed_at` | TEXT | | |
| `completed_at_epoch` | INTEGER | | |
| `status` | TEXT | CHECK(IN ('active','completed','failed')), DEFAULT 'active' | 세션 상태 |

**observations** -- 추출된 관찰

| 컬럼 | 타입 | 제약 | 설명 |
|------|------|------|------|
| `id` | INTEGER | PRIMARY KEY AUTOINCREMENT | |
| `memory_session_id` | TEXT | NOT NULL, FK(sdk_sessions.memory_session_id) ON DELETE CASCADE ON UPDATE CASCADE | |
| `project` | TEXT | NOT NULL | |
| `text` | TEXT | nullable (migration009) | 레거시 텍스트 |
| `type` | TEXT | NOT NULL | 관찰 유형 |
| `created_at` | TEXT | NOT NULL | |
| `created_at_epoch` | INTEGER | NOT NULL | |

**session_summaries** -- 세션 요약

| 컬럼 | 타입 | 제약 | 설명 |
|------|------|------|------|
| `id` | INTEGER | PRIMARY KEY AUTOINCREMENT | |
| `memory_session_id` | TEXT | NOT NULL, FK(sdk_sessions.memory_session_id) ON DELETE CASCADE ON UPDATE CASCADE | |
| `project` | TEXT | NOT NULL | |
| `request` | TEXT | | 사용자 요청 |
| `investigated` | TEXT | | 조사한 내용 |
| `learned` | TEXT | | 학습한 내용 |
| `completed` | TEXT | | 완료한 내용 |
| `next_steps` | TEXT | | 다음 단계 |
| `files_read` | TEXT | | JSON 배열: 읽은 파일 |
| `files_edited` | TEXT | | JSON 배열: 편집한 파일 |
| `notes` | TEXT | | 추가 노트 |
| `created_at` | TEXT | NOT NULL | |
| `created_at_epoch` | INTEGER | NOT NULL | |

#### migration005 (version 5) -- 테이블 정리

`streaming_sessions`, `observation_queue` 테이블을 삭제한다 (sdk_sessions, Unix 소켓으로 대체됨).

#### migration006 (version 6) -- FTS5 전문 검색

`observations_fts`, `session_summaries_fts` FTS5 가상 테이블을 생성하고, AFTER INSERT/DELETE/UPDATE 트리거로 동기화를 유지한다. 플랫폼에서 FTS5를 사용할 수 없으면 건너뛴다 (Bun on Windows).

#### migration007 (version 7) -- ROI 메트릭

`observations`와 `session_summaries`에 `discovery_tokens INTEGER DEFAULT 0` 컬럼을 추가한다.

### 현재 마이그레이션 시스템: migrations/runner.ts (866L)

`MigrationRunner` 클래스는 `SessionStore` 및 `ClaudeMemDatabase`에서 사용되는 현재 마이그레이션 시스템이다. 레거시 시스템과 버전 번호가 충돌하는 경우를 처리하기 위해 실제 컬럼/테이블 상태를 확인한 후 마이그레이션을 적용한다.

`runAllMigrations()`가 호출하는 마이그레이션 순서:

| 메서드 | 버전 | 설명 |
|--------|------|------|
| `initializeSchema()` | 4 | 코어 테이블 (sdk_sessions, observations, session_summaries) |
| `ensureWorkerPortColumn()` | 5 | sdk_sessions에 `worker_port INTEGER` 추가 |
| `ensurePromptTrackingColumns()` | 6 | `prompt_counter`, `prompt_number` 추가 |
| `removeSessionSummariesUniqueConstraint()` | 7 | session_summaries.memory_session_id의 UNIQUE 제거 |
| `addObservationHierarchicalFields()` | 8 | observations에 title, subtitle, facts, narrative, concepts, files_read, files_modified 추가 |
| `makeObservationsTextNullable()` | 9 | observations.text를 nullable로 변경 (테이블 재생성) |
| `createUserPromptsTable()` | 10 | user_prompts 테이블 + FTS5 |
| `ensureDiscoveryTokensColumn()` | 11 | discovery_tokens 컬럼 추가 |
| `createPendingMessagesTable()` | 16 | pending_messages 테이블 |
| `renameSessionIdColumns()` | 17 | claude_session_id -> content_session_id, sdk_session_id -> memory_session_id |
| `repairSessionIdColumnRename()` | 19 | No-op (migration 17이 멱등으로 변경됨) |
| `addFailedAtEpochColumn()` | 20 | pending_messages에 `failed_at_epoch` 추가 |
| `addOnUpdateCascadeToForeignKeys()` | 21 | observations, session_summaries FK에 ON UPDATE CASCADE 추가 (테이블 재생성) |
| `addObservationContentHashColumn()` | 22 | observations에 `content_hash TEXT` + 인덱스 + 기존 행 백필 |
| `addSessionCustomTitleColumn()` | 23 | sdk_sessions에 `custom_title TEXT` 추가 |

### schema_versions 테이블

```sql
CREATE TABLE IF NOT EXISTS schema_versions (
  id INTEGER PRIMARY KEY,
  version INTEGER UNIQUE NOT NULL,
  applied_at TEXT NOT NULL
)
```

모든 마이그레이션은 `INSERT OR IGNORE INTO schema_versions (version, applied_at) VALUES (?, ?)`로 적용 기록을 남긴다. `OR IGNORE`를 사용하여 재실행에 안전하다.

### 최종 활성 스키마 요약

마이그레이션 23까지 적용된 최종 스키마:

**sdk_sessions**

| 컬럼 | 타입 | 제약 |
|------|------|------|
| `id` | INTEGER | PK AUTOINCREMENT |
| `content_session_id` | TEXT | UNIQUE NOT NULL |
| `memory_session_id` | TEXT | UNIQUE |
| `project` | TEXT | NOT NULL |
| `user_prompt` | TEXT | |
| `started_at` | TEXT | NOT NULL |
| `started_at_epoch` | INTEGER | NOT NULL |
| `completed_at` | TEXT | |
| `completed_at_epoch` | INTEGER | |
| `status` | TEXT | CHECK(IN ('active','completed','failed')) DEFAULT 'active' |
| `worker_port` | INTEGER | (migration 5) |
| `prompt_counter` | INTEGER | DEFAULT 0 (migration 6) |
| `custom_title` | TEXT | (migration 23) |

인덱스: content_session_id, memory_session_id, project, status, started_at_epoch DESC

**observations**

| 컬럼 | 타입 | 제약 |
|------|------|------|
| `id` | INTEGER | PK AUTOINCREMENT |
| `memory_session_id` | TEXT | NOT NULL, FK -> sdk_sessions ON DELETE/UPDATE CASCADE |
| `project` | TEXT | NOT NULL |
| `text` | TEXT | nullable |
| `type` | TEXT | NOT NULL |
| `title` | TEXT | |
| `subtitle` | TEXT | |
| `facts` | TEXT | JSON 배열 |
| `narrative` | TEXT | |
| `concepts` | TEXT | JSON 배열 |
| `files_read` | TEXT | JSON 배열 |
| `files_modified` | TEXT | JSON 배열 |
| `prompt_number` | INTEGER | |
| `discovery_tokens` | INTEGER | DEFAULT 0 |
| `content_hash` | TEXT | 중복 제거용 |
| `created_at` | TEXT | NOT NULL |
| `created_at_epoch` | INTEGER | NOT NULL |

인덱스: memory_session_id, project, type, created_at_epoch DESC, content_hash+created_at_epoch

**session_summaries**

| 컬럼 | 타입 | 제약 |
|------|------|------|
| `id` | INTEGER | PK AUTOINCREMENT |
| `memory_session_id` | TEXT | NOT NULL, FK -> sdk_sessions ON DELETE/UPDATE CASCADE |
| `project` | TEXT | NOT NULL |
| `request` | TEXT | |
| `investigated` | TEXT | |
| `learned` | TEXT | |
| `completed` | TEXT | |
| `next_steps` | TEXT | |
| `files_read` | TEXT | JSON 배열 |
| `files_edited` | TEXT | JSON 배열 |
| `notes` | TEXT | |
| `prompt_number` | INTEGER | |
| `discovery_tokens` | INTEGER | DEFAULT 0 |
| `created_at` | TEXT | NOT NULL |
| `created_at_epoch` | INTEGER | NOT NULL |

인덱스: memory_session_id, project, created_at_epoch DESC

**user_prompts**

| 컬럼 | 타입 | 제약 |
|------|------|------|
| `id` | INTEGER | PK AUTOINCREMENT |
| `content_session_id` | TEXT | NOT NULL, FK -> sdk_sessions.content_session_id ON DELETE CASCADE |
| `prompt_number` | INTEGER | NOT NULL |
| `prompt_text` | TEXT | NOT NULL |
| `created_at` | TEXT | NOT NULL |
| `created_at_epoch` | INTEGER | NOT NULL |

인덱스: content_session_id, created_at_epoch DESC, prompt_number, (content_session_id, prompt_number) composite

**pending_messages**

| 컬럼 | 타입 | 제약 |
|------|------|------|
| `id` | INTEGER | PK AUTOINCREMENT |
| `session_db_id` | INTEGER | NOT NULL, FK -> sdk_sessions.id ON DELETE CASCADE |
| `content_session_id` | TEXT | NOT NULL |
| `message_type` | TEXT | NOT NULL, CHECK(IN ('observation', 'summarize')) |
| `tool_name` | TEXT | |
| `tool_input` | TEXT | JSON |
| `tool_response` | TEXT | JSON |
| `cwd` | TEXT | |
| `last_user_message` | TEXT | |
| `last_assistant_message` | TEXT | |
| `prompt_number` | INTEGER | |
| `status` | TEXT | NOT NULL DEFAULT 'pending', CHECK(IN ('pending','processing','processed','failed')) |
| `retry_count` | INTEGER | NOT NULL DEFAULT 0 |
| `created_at_epoch` | INTEGER | NOT NULL |
| `started_processing_at_epoch` | INTEGER | |
| `completed_at_epoch` | INTEGER | |
| `failed_at_epoch` | INTEGER | |

인덱스: session_db_id, status, content_session_id

**FTS5 가상 테이블** (플랫폼 지원 시):

- `observations_fts` -- title, subtitle, narrative, text, facts, concepts
- `session_summaries_fts` -- request, investigated, learned, completed, next_steps, notes
- `user_prompts_fts` -- prompt_text

각 FTS5 테이블에 AFTER INSERT/DELETE/UPDATE 트리거가 설정되어 원본 테이블과 자동 동기화된다.

---

## 3. 세션 스토어

### SessionStore.ts (2,459L)

`SessionStore`는 claude-mem 데이터 계층의 핵심 클래스이다. `bun:sqlite`를 직접 사용하며, 생성자에서 스키마 초기화와 모든 마이그레이션을 실행한다.

#### 생성자

```typescript
constructor(dbPath: string = DB_PATH) {
  // 1. 디렉토리 보장
  // 2. Database 연결 생성
  // 3. PRAGMA 설정 (WAL, NORMAL sync, foreign_keys ON)
  // 4. 스키마 초기화 (initializeSchema)
  // 5. 15개 마이그레이션 순차 실행
}
```

#### 주요 메서드 카테고리

**세션 관리:**

| 메서드 | 설명 |
|--------|------|
| `createSession(contentSessionId, project, userPrompt?)` | 세션 생성, DB ID 반환 |
| `getSessionByContentId(contentSessionId)` | content_session_id로 세션 조회 |
| `getSessionByMemoryId(memorySessionId)` | memory_session_id로 세션 조회 |
| `getSessionById(id)` | DB ID로 세션 조회 |
| `updateMemorySessionId(contentSessionId, memorySessionId)` | memory_session_id 업데이트 |
| `updateSessionStatus(memorySessionId, status)` | 상태 변경 (active/completed/failed) |
| `completeSession(memorySessionId)` | 세션 완료 표시 |
| `failSession(memorySessionId)` | 세션 실패 표시 |
| `getActiveSessionByProject(project)` | 프로젝트의 활성 세션 조회 |
| `getRecentSessions(project, limit?)` | 최근 세션 목록 |
| `getAllSessions(limit?)` | 전체 세션 목록 |
| `deleteSession(id)` | 세션 및 연관 데이터 삭제 |
| `updateSessionCustomTitle(id, title)` | custom_title 설정 |

**관찰(Observation) 관리:**

| 메서드 | 설명 |
|--------|------|
| `storeObservation(memorySessionId, project, observation, promptNumber?, discoveryTokens?)` | 관찰 저장 (content-hash 중복 제거) |
| `getObservationsForSession(memorySessionId)` | 세션의 관찰 목록 |
| `getRecentObservations(project, limit?)` | 프로젝트 최근 관찰 |
| `getAllRecentObservations(limit?)` | 전체 최근 관찰 (UI용) |
| `getObservationById(id)` | ID로 관찰 조회 |
| `getObservationsByIds(ids, options?)` | ID 배열로 조회 (필터, 정렬 지원) |
| `getFilesForSession(memorySessionId)` | 세션의 파일 목록 집계 |

**요약(Summary) 관리:**

| 메서드 | 설명 |
|--------|------|
| `storeSummary(memorySessionId, project, summary, promptNumber?, discoveryTokens?)` | 요약 저장 |
| `getSummaryForSession(memorySessionId)` | 세션 최신 요약 |
| `getRecentSummaries(project, limit?)` | 프로젝트 최근 요약 |
| `getAllRecentSummaries(limit?)` | 전체 최근 요약 (UI용) |
| `getSummaryById(id)` | ID로 요약 조회 |
| `getSummariesByIds(ids, options?)` | ID 배열로 조회 |
| `getRecentSummariesWithSessionInfo(project, limit?)` | 세션 정보 포함 요약 |

**프롬프트(Prompt) 관리:**

| 메서드 | 설명 |
|--------|------|
| `saveUserPrompt(contentSessionId, promptNumber, promptText)` | 프롬프트 저장 |
| `getUserPrompt(contentSessionId, promptNumber)` | 프롬프트 텍스트 조회 |
| `getPromptNumberFromUserPrompts(contentSessionId)` | 프롬프트 번호 (COUNT 기반) |
| `getLatestUserPrompt(contentSessionId)` | 최신 프롬프트 (세션 정보 JOIN) |
| `getAllRecentUserPrompts(limit?)` | 전체 최근 프롬프트 (UI용) |
| `getPromptById(id)` | ID로 프롬프트 조회 |
| `getPromptsByIds(ids)` | ID 배열로 조회 |
| `getUserPromptsByIds(ids, options?)` | Chroma 하이브리드 검색용 |

**타임라인 관리:**

| 메서드 | 설명 |
|--------|------|
| `getTimelineAroundTimestamp(anchorEpoch, depthBefore, depthAfter, project?)` | 타임스탬프 중심 타임라인 |
| `getTimelineAroundObservation(obsId, epoch, before, after, project?)` | 관찰 ID 중심 타임라인 |
| `getAllProjects()` | 고유 프로젝트 목록 |

**통계 및 유틸리티:**

| 메서드 | 설명 |
|--------|------|
| `getStats()` | 전체 통계 (sessions, observations, summaries, user_prompts 카운트) |
| `getProjectStats(project)` | 프로젝트별 통계 |
| `close()` | 데이터베이스 연결 종료 |

**임포트:**

| 메서드 | 설명 |
|--------|------|
| `importSdkSession(session)` | 세션 임포트 (중복 체크) |
| `importObservation(obs)` | 관찰 임포트 (중복 체크) |
| `importSessionSummary(summary)` | 요약 임포트 (중복 체크) |
| `importUserPrompt(prompt)` | 프롬프트 임포트 (중복 체크) |

SessionStore는 `src/services/sqlite/` 하위 모듈의 함수들을 래핑하여 통합 인터페이스를 제공한다. 내부적으로 동일한 마이그레이션 로직이 `MigrationRunner`에도 복제되어 있는데, 이는 `SessionStore`와 `ClaudeMemDatabase` 두 진입점 모두에서 독립적으로 마이그레이션을 실행할 수 있도록 하기 위함이다.

---

## 4. 세션 검색

### SessionSearch.ts (607L)

`SessionSearch` 클래스는 필터 기반 구조화된 쿼리를 제공한다. 텍스트 검색은 ChromaDB가 담당하고, 이 클래스는 필터 전용 SQLite 쿼리만 지원한다.

#### 생성자

WAL 모드를 설정하고, FTS5 테이블 존재를 보장한다 (하위 호환성용, 실제 검색에는 사용하지 않음).

#### 핵심 검색 메서드

**searchObservations(query, options)**

- `query`가 `undefined`이면 필터 전용 경로를 사용하여 `observations` 테이블을 직접 쿼리한다.
- `query`가 있으면 ChromaDB로 위임해야 하므로 빈 배열을 반환하고 경고를 로깅한다.

**searchSessions(query, options)**

- 동일한 패턴으로 `session_summaries` 테이블을 필터 기반 쿼리한다.

**searchUserPrompts(query, options)**

- `user_prompts`를 `sdk_sessions`와 JOIN하여 프로젝트 필터를 지원한다.

#### SearchFilters 인터페이스

```typescript
interface SearchFilters {
  project?: string;                         // 프로젝트 필터
  type?: ObservationRow['type'] | array;    // 관찰 유형 필터
  concepts?: string | string[];             // JSON 배열 검색 (json_each)
  files?: string | string[];                // 파일 경로 LIKE 검색
  dateRange?: { start?; end? };             // 에포크/ISO 기반 날짜 범위
}
```

#### SearchOptions 인터페이스

```typescript
interface SearchOptions extends SearchFilters {
  limit?: number;      // 기본값 50
  offset?: number;     // 기본값 0
  orderBy?: 'relevance' | 'date_desc' | 'date_asc';
  isFolder?: boolean;  // 폴더 직접 자식만 매칭
}
```

#### 특수 검색 메서드

- **findByConcept(concept, options)** -- concepts JSON 배열에서 `json_each()`로 검색
- **findByFile(filePath, options)** -- files_read, files_modified JSON 배열에서 LIKE 검색. `isFolder=true`일 때 직접 자식 파일만 반환하기 위해 후처리 필터링 수행
- **findByType(type, options)** -- 관찰 유형별 필터
- **getUserPromptsBySession(contentSessionId)** -- 세션의 전체 프롬프트 (prompt_number 순)

#### buildFilterClause 내부 메서드

필터 조건들을 SQL WHERE 절로 변환한다:
- `project` -- `= ?`
- `type` -- `= ?` 또는 `IN (?, ?, ...)`
- `dateRange` -- `>= ?` / `<= ?` (epoch 변환)
- `concepts` -- `EXISTS (SELECT 1 FROM json_each(concepts) WHERE value = ?)`
- `files` -- `EXISTS (SELECT 1 FROM json_each(files_read) WHERE value LIKE ?) OR EXISTS (SELECT 1 FROM json_each(files_modified) WHERE value LIKE ?)`

---

## 5. 관찰(Observation) 모델

### Observations.ts (12L)

모듈 진입점으로, 5개 하위 모듈을 re-export한다:
- `observations/types.ts` -- 타입 정의
- `observations/store.ts` -- 저장
- `observations/get.ts` -- 조회
- `observations/recent.ts` -- 최근 관찰
- `observations/files.ts` -- 파일 집계

### observations/types.ts (83L)

주요 타입 정의:

```typescript
interface ObservationInput {
  type: string;
  title: string | null;
  subtitle: string | null;
  facts: string[];
  narrative: string | null;
  concepts: string[];
  files_read: string[];
  files_modified: string[];
}

interface StoreObservationResult { id: number; createdAtEpoch: number; }
interface GetObservationsByIdsOptions { orderBy?; limit?; project?; type?; concepts?; files?; }
interface SessionFilesResult { filesRead: string[]; filesModified: string[]; }
interface ObservationSessionRow { title; subtitle; type; prompt_number; }
interface RecentObservationRow { type; text; prompt_number; created_at; }
interface AllRecentObservationRow { id; type; title; subtitle; text; project; prompt_number; created_at; created_at_epoch; }
```

**관찰 유형** (ObservationRow.type): `decision`, `bugfix`, `feature`, `refactor`, `discovery`, `change`

### observations/store.ts (105L)

**content-hash 중복 제거 메커니즘:**

`computeObservationContentHash(memorySessionId, title, narrative)`는 SHA-256 해시의 처음 16자를 반환한다. `findDuplicateObservation(db, contentHash, timestampEpoch)`는 30초 윈도우(DEDUP_WINDOW_MS) 내에서 동일한 content_hash를 가진 관찰이 있는지 확인한다.

`storeObservation()` 함수의 처리 과정:
1. 타임스탬프 결정 (override 또는 현재 시각)
2. 프로젝트명 해소 (빈 문자열이면 git root에서 추출)
3. content-hash 계산
4. 중복 확인 (30초 윈도우)
5. INSERT 실행 (15개 컬럼: facts, concepts, files_read, files_modified는 JSON.stringify)

### observations/get.ts (113L)

세 가지 조회 함수를 제공한다:

- `getObservationById(db, id)` -- 단일 관찰 조회
- `getObservationsByIds(db, ids, options)` -- ID 배열 조회, 프로젝트/유형/개념/파일 필터 지원. IN 절에 플레이스홀더를 동적 생성하고, 추가 필터 조건을 AND로 결합한다.
- `getObservationsForSession(db, memorySessionId)` -- 세션의 관찰 목록 (title, subtitle, type, prompt_number)

### observations/recent.ts (45L)

- `getRecentObservations(db, project, limit=20)` -- 프로젝트 최근 관찰
- `getAllRecentObservations(db, limit=100)` -- 전체 최근 관찰 (Web UI용)

### observations/files.ts (54L)

`getFilesForSession(db, memorySessionId)` -- 세션의 모든 관찰에서 `files_read`와 `files_modified` JSON 배열을 파싱하여 `Set<string>`으로 병합한 후 배열로 반환한다. 파일 목록의 중복을 제거한다.

---

## 6. 대기 메시지 큐

### PendingMessageStore.ts (489L)

`PendingMessageStore`는 SDK 메시지의 영속적 작업 큐를 구현한다. Claim-Confirm 패턴을 사용하여 중복 처리를 방지하고, 크래시 복구를 가능하게 한다.

#### 메시지 생명주기

```
enqueue() -> [pending]
  ↓
claimNextMessage() -> [processing]
  ↓ (성공)
confirmProcessed() -> [삭제됨]
  ↓ (실패)
markFailed() -> retry_count < 3 ? [pending] : [failed]
```

#### 자기 치유(Self-Healing) 메커니즘

`claimNextMessage()` 내부에서 트랜잭션 시작 전에 60초(STALE_PROCESSING_THRESHOLD_MS) 이상 `processing` 상태인 메시지를 자동으로 `pending`으로 리셋한다. 이를 통해 제너레이터 크래시 후 외부 타이머 없이 복구가 가능하다.

#### 주요 메서드

**큐 조작:**

| 메서드 | 설명 |
|--------|------|
| `enqueue(sessionDbId, contentSessionId, message)` | 메시지 인큐. DB ID 반환 |
| `claimNextMessage(sessionDbId)` | 원자적으로 다음 pending 메시지를 processing으로 변경. 트랜잭션 내부에서 stale 메시지 자동 리셋 |
| `confirmProcessed(messageId)` | 처리 완료 메시지 삭제 (큐에서 제거) |
| `markFailed(messageId)` | retry_count < maxRetries이면 pending으로 복귀, 아니면 failed로 영구 표시 |

**상태 조회:**

| 메서드 | 설명 |
|--------|------|
| `getAllPending(sessionDbId)` | 세션의 대기 메시지 목록 |
| `getPendingCount(sessionDbId)` | 대기 메시지 수 (pending + processing) |
| `getQueueMessages()` | UI용 전체 큐 (pending/processing/failed, sdk_sessions JOIN) |
| `getStuckCount(thresholdMs)` | 고착 메시지 수 |
| `hasAnyPendingWork()` | 작업 존재 여부 (5분 이상 processing 자동 리셋) |
| `getSessionsWithPendingMessages()` | 대기 작업 있는 세션 ID 목록 (시작 시 복구용) |
| `getRecentlyProcessed(limit, withinMinutes)` | 최근 처리 완료 메시지 (UI 피드백용) |

**복구 및 정리:**

| 메서드 | 설명 |
|--------|------|
| `resetStaleProcessingMessages(thresholdMs, sessionDbId?)` | stale processing 메시지를 pending으로 리셋 |
| `resetProcessingToPending(sessionDbId)` | 세션의 모든 processing을 pending으로 |
| `markSessionMessagesFailed(sessionDbId)` | 세션의 모든 processing을 failed로 (세션 레벨 오류) |
| `markAllSessionMessagesAbandoned(sessionDbId)` | pending + processing 모두 failed로 |
| `retryMessage(messageId)` | pending/processing/failed를 pending으로 |
| `retryAllStuck(thresholdMs)` | 모든 고착 메시지 pending으로 |
| `abortMessage(messageId)` | 큐에서 메시지 삭제 |
| `clearFailed()` | 모든 failed 메시지 삭제 |
| `clearAll()` | pending/processing/failed 모두 삭제 |

**직렬화:**

`toPendingMessage(persistent)` -- DB 레코드를 `PendingMessage` 인터페이스로 변환. `tool_input`과 `tool_response`는 `JSON.parse()`로 역직렬화한다.

#### PersistentPendingMessage 인터페이스

```typescript
interface PersistentPendingMessage {
  id: number;
  session_db_id: number;
  content_session_id: string;
  message_type: 'observation' | 'summarize';
  tool_name: string | null;
  tool_input: string | null;       // JSON 직렬화
  tool_response: string | null;    // JSON 직렬화
  cwd: string | null;
  last_assistant_message: string | null;
  prompt_number: number | null;
  status: 'pending' | 'processing' | 'processed' | 'failed';
  retry_count: number;
  created_at_epoch: number;
  started_processing_at_epoch: number | null;
  completed_at_epoch: number | null;
}
```

---

## 7. 트랜잭션 관리

### transactions.ts (254L)

크로스 도메인 원자적 트랜잭션을 제공한다. 관찰 저장, 요약 저장, 대기 메시지 완료를 단일 트랜잭션으로 묶어 데이터 일관성을 보장한다.

#### storeObservationsAndMarkComplete()

7개 매개변수를 받아 3단계 원자적 트랜잭션을 실행한다:

1. **관찰 저장** -- 각 관찰에 대해 content-hash 중복 확인 후 INSERT. `computeObservationContentHash()`와 `findDuplicateObservation()`을 사용하여 30초 윈도우 내 중복을 건너뛴다.
2. **요약 저장** -- 제공된 경우 `session_summaries`에 INSERT
3. **메시지 완료 표시** -- `pending_messages`의 해당 메시지를 `processed` 상태로 업데이트하고, `tool_input`과 `tool_response`를 NULL로 설정 (공간 절약)

전체 과정이 `db.transaction()` 내에서 실행되므로, 어느 단계에서든 실패하면 모든 변경이 롤백된다. 이 설계는 "관찰은 저장되었으나 메시지는 미완료" 상태를 방지하여 크래시 복구 시 재처리로 인한 중복 관찰 버그를 해결한다.

#### storeObservations()

`storeObservationsAndMarkComplete()`의 간소화 버전. claim-and-delete 큐 패턴에서 사용되며, 메시지 완료 표시 단계가 없다. 관찰 저장과 요약 저장만 원자적으로 수행한다.

#### StoreObservationsResult 인터페이스

```typescript
interface StoreObservationsResult {
  observationIds: number[];
  summaryId: number | null;
  createdAtEpoch: number;
}
```

---

## 8. 프롬프트 스토어

### 모듈 구조

```
src/services/sqlite/prompts/
├── types.ts  (42L) -- 타입 정의
├── store.ts  (30L) -- 저장
└── get.ts    (169L) -- 조회
```

`Prompts.ts`가 이들을 re-export한다.

### prompts/types.ts (42L)

```typescript
interface RecentUserPromptResult {
  id; content_session_id; project; prompt_number; prompt_text; created_at; created_at_epoch;
}

interface PromptWithProject {
  id; content_session_id; prompt_number; prompt_text; project; created_at; created_at_epoch;
}

interface GetPromptsByIdsOptions {
  orderBy?: 'date_desc' | 'date_asc';
  limit?: number;
  project?: string;
}
```

### prompts/store.ts (30L)

`saveUserPrompt(db, contentSessionId, promptNumber, promptText)` -- `user_prompts` 테이블에 INSERT. `created_at`과 `created_at_epoch`를 자동 생성한다.

### prompts/get.ts (169L)

7개의 조회 함수를 제공한다:

| 함수 | 설명 |
|------|------|
| `getUserPrompt(db, contentSessionId, promptNumber)` | 특정 프롬프트 텍스트 |
| `getPromptNumberFromUserPrompts(db, contentSessionId)` | COUNT(*) 기반 프롬프트 번호 (prompt_counter 컬럼 대체) |
| `getLatestUserPrompt(db, contentSessionId)` | 최신 프롬프트 + sdk_sessions JOIN (memory_session_id, project 포함) |
| `getAllRecentUserPrompts(db, limit)` | 전체 최근 프롬프트 (sdk_sessions LEFT JOIN) |
| `getPromptById(db, id)` | 단일 프롬프트 + sdk_sessions LEFT JOIN |
| `getPromptsByIds(db, ids)` | ID 배열 조회 |
| `getUserPromptsByIds(db, ids, options)` | Chroma 하이브리드 검색용 (orderBy, limit, project 필터) |

---

## 9. 타임라인

### timeline/queries.ts (218L)

시간 기반 컨텍스트 쿼리를 제공한다. 특정 시점을 중심으로 전후 N개의 관찰, 세션, 프롬프트를 조회한다.

#### TimelineResult 인터페이스

```typescript
interface TimelineResult {
  observations: ObservationRecord[];
  sessions: Array<{
    id; memory_session_id; project; request; completed; next_steps; created_at; created_at_epoch;
  }>;
  prompts: Array<{
    id; content_session_id; prompt_number; prompt_text; project; created_at; created_at_epoch;
  }>;
}
```

#### getTimelineAroundTimestamp()

`getTimelineAroundObservation()`의 편의 래퍼. `anchorObservationId`를 null로 전달하여 타임스탬프 기반 쿼리를 수행한다.

#### getTimelineAroundObservation()

양방향 시간 윈도우를 결정한 후 세 종류의 레코드를 모두 조회한다:

1. **앵커가 observation ID인 경우**: ID 오프셋으로 경계 관찰의 타임스탬프를 구한다
2. **앵커가 timestamp인 경우**: `created_at_epoch` 기준으로 전후 N개의 관찰 타임스탬프를 구한다
3. 결정된 `startEpoch`~`endEpoch` 범위에서 observations, session_summaries, user_prompts 세 테이블을 모두 쿼리한다
4. user_prompts는 sdk_sessions와 JOIN하여 project 정보를 가져온다

#### getAllProjects()

`sdk_sessions`에서 `DISTINCT project`를 조회한다. Web UI의 프로젝트 필터에 사용된다.

---

## 10. 요약

### 모듈 구조

```
src/services/sqlite/summaries/
├── types.ts  (99L) -- 타입 정의
├── store.ts  (60L) -- 저장
├── get.ts    (88L) -- 조회
└── recent.ts (79L) -- 최근 요약
```

`Summaries.ts`가 이들을 re-export한다.

### summaries/types.ts (99L)

6개의 인터페이스를 정의한다:

```typescript
interface SummaryInput {
  request: string;
  investigated: string;
  learned: string;
  completed: string;
  next_steps: string;
  notes: string | null;
}

interface StoreSummaryResult { id: number; createdAtEpoch: number; }
interface SessionSummary { request; investigated; learned; completed; next_steps; files_read; files_edited; notes; prompt_number; created_at; created_at_epoch; }
interface SummaryWithSessionInfo { memory_session_id; request; learned; completed; next_steps; prompt_number; created_at; }
interface RecentSummary { /* SessionSummary와 동일한 필드 */ }
interface FullSummary { id; request; investigated; learned; completed; next_steps; files_read; files_edited; notes; project; prompt_number; created_at; created_at_epoch; }
interface GetByIdsOptions { orderBy?; limit?; project?; }
```

### summaries/store.ts (60L)

`storeSummary(db, memorySessionId, project, summary, promptNumber?, discoveryTokens?, overrideTimestampEpoch?)` -- `session_summaries`에 INSERT. 12개 컬럼을 채운다. `overrideTimestampEpoch`는 백로그 처리 시 원본 타임스탬프를 사용하기 위한 것이다.

### summaries/get.ts (88L)

| 함수 | 설명 |
|------|------|
| `getSummaryForSession(db, memorySessionId)` | 세션의 최신 요약 (created_at_epoch DESC LIMIT 1) |
| `getSummaryById(db, id)` | ID로 전체 레코드 조회 |
| `getSummariesByIds(db, ids, options)` | ID 배열 조회, 정렬/제한/프로젝트 필터 |

### summaries/recent.ts (79L)

| 함수 | 설명 |
|------|------|
| `getRecentSummaries(db, project, limit=10)` | 프로젝트 최근 요약 (10개 기본) |
| `getRecentSummariesWithSessionInfo(db, project, limit=3)` | memory_session_id 포함 (컨텍스트 표시용, 3개 기본) |
| `getAllRecentSummaries(db, limit=50)` | 전체 프로젝트 최근 요약 (Web UI용, 50개 기본) |

---

## 11. 임포트

### Import.ts (7L)

모듈 진입점. `import/bulk.ts`를 re-export한다.

### import/bulk.ts (237L)

4개의 벌크 임포트 함수를 제공한다. 각 함수는 중복 체크 후 INSERT를 수행하며, `ImportResult { imported: boolean; id: number }` 형식의 결과를 반환한다.

#### importSdkSession()

중복 기준: `content_session_id`
- 기존 세션이 있으면 `{ imported: false, id }` 반환
- 없으면 INSERT 후 `{ imported: true, id }` 반환

INSERT 컬럼: content_session_id, memory_session_id, project, user_prompt, started_at, started_at_epoch, completed_at, completed_at_epoch, status

#### importSessionSummary()

중복 기준: `memory_session_id`

INSERT 컬럼: memory_session_id, project, request, investigated, learned, completed, next_steps, files_read, files_edited, notes, prompt_number, discovery_tokens, created_at, created_at_epoch

#### importObservation()

중복 기준: `memory_session_id + title + created_at_epoch` 복합 조건

INSERT 컬럼: memory_session_id, project, text, type, title, subtitle, facts, narrative, concepts, files_read, files_modified, prompt_number, discovery_tokens, created_at, created_at_epoch

#### importUserPrompt()

중복 기준: `content_session_id + prompt_number` 복합 조건

INSERT 컬럼: content_session_id, prompt_number, prompt_text, created_at, created_at_epoch

임포트 함수들은 `src/bin/import-xml-observations.ts` (389L)에서 XML 데이터를 파싱하여 호출된다.

---

## 12. 타입 정의

### database.ts (139L)

`src/types/database.ts`는 데이터베이스 쿼리 결과에 대한 타입 안전성을 제공한다.

**스키마 정보 타입:**

```typescript
interface TableColumnInfo { cid; name; type; notnull; dflt_value; pk; }  // PRAGMA table_info
interface IndexInfo { seq; name; unique; origin; partial; }               // PRAGMA index_list
interface TableNameRow { name: string; }                                  // sqlite_master
interface SchemaVersion { version: number; }                              // schema_versions
```

**레코드 타입:**

```typescript
interface SdkSessionRecord {
  id: number;
  content_session_id: string;
  memory_session_id: string | null;
  project: string;
  user_prompt: string | null;
  started_at: string;
  started_at_epoch: number;
  completed_at: string | null;
  completed_at_epoch: number | null;
  status: 'active' | 'completed' | 'failed';
  worker_port?: number;
  prompt_counter?: number;
}

interface ObservationRecord {
  id: number;
  memory_session_id: string;
  project: string;
  text: string | null;
  type: 'decision' | 'bugfix' | 'feature' | 'refactor' | 'discovery' | 'change';
  created_at: string;
  created_at_epoch: number;
  title?: string;
  concept?: string;
  source_files?: string;
  prompt_number?: number;
  discovery_tokens?: number;
}

interface SessionSummaryRecord {
  id: number;
  memory_session_id: string;
  project: string;
  request: string | null;
  investigated: string | null;
  learned: string | null;
  completed: string | null;
  next_steps: string | null;
  created_at: string;
  created_at_epoch: number;
  prompt_number?: number;
  discovery_tokens?: number;
}

interface UserPromptRecord {
  id: number;
  content_session_id: string;
  prompt_number: number;
  prompt_text: string;
  project?: string;            // JOIN 결과
  created_at: string;
  created_at_epoch: number;
}

interface LatestPromptResult {
  id: number;
  content_session_id: string;
  memory_session_id: string;
  project: string;
  prompt_number: number;
  prompt_text: string;
  created_at_epoch: number;
}

interface ObservationWithContext {
  id: number;
  memory_session_id: string;
  project: string;
  text: string | null;
  type: string;
  created_at: string;
  created_at_epoch: number;
  title?: string;
  concept?: string;
  source_files?: string;
  prompt_number?: number;
  discovery_tokens?: number;
}
```

### transcript.ts (174L)

`src/types/transcript.ts`는 Claude Code 트랜스크립트 JSONL 구조를 모델링한다.

**콘텐츠 아이템:**

| 타입 | 필드 | 설명 |
|------|------|------|
| `TextContent` | type='text', text | 텍스트 콘텐츠 |
| `ToolUseContent` | type='tool_use', id, name, input | 도구 사용 |
| `ToolResultContent` | type='tool_result', tool_use_id, content, is_error? | 도구 결과 |
| `ThinkingContent` | type='thinking', thinking, signature? | 사고 과정 |
| `ImageContent` | type='image', source | 이미지 |

`ContentItem` = TextContent | ToolUseContent | ToolResultContent | ThinkingContent | ImageContent

**메시지 타입:**

| 타입 | 필드 |
|------|------|
| `UserMessage` | role='user', content (string 또는 ContentItem[]) |
| `AssistantMessage` | id, type='message', role='assistant', model, content, stop_reason?, usage? |

**도구 결과 유형:**

`ToolUseResult` = string | TodoItem[] | FileReadResult | CommandResult | TodoResult | EditResult | ContentItem[]

**트랜스크립트 엔트리:**

| 타입 | 공통 필드 | 고유 필드 |
|------|-----------|-----------|
| `UserTranscriptEntry` | parentUuid, isSidechain, userType, cwd, sessionId, version, uuid, timestamp | message: UserMessage, toolUseResult? |
| `AssistantTranscriptEntry` | (동일) | message: AssistantMessage, requestId? |
| `SummaryTranscriptEntry` | (없음) | type='summary', summary, leafUuid, cwd? |
| `SystemTranscriptEntry` | (동일) | type='system', content, level? |
| `QueueOperationTranscriptEntry` | (없음) | type='queue-operation', operation, timestamp, sessionId, content? |

`TranscriptEntry` = UserTranscriptEntry | AssistantTranscriptEntry | SummaryTranscriptEntry | SystemTranscriptEntry | QueueOperationTranscriptEntry

### sqlite/types.ts (288L)

`src/services/sqlite/types.ts`는 SQLite 계층 전용 타입을 정의한다.

**레거시 테이블 행 타입:**

- `SessionRow` -- sessions 테이블 (source: 'compress' | 'save' | 'legacy-jsonl')
- `OverviewRow` -- overviews 테이블
- `MemoryRow` -- memories 테이블 (계층적 필드 포함)
- `DiagnosticRow` -- diagnostics 테이블
- `TranscriptEventRow` -- transcript_events 테이블
- `ArchiveRow` -- 아카이브 행
- `TitleRow` -- 제목 행

**입력 타입 (Input):**

- `SessionInput`, `OverviewInput`, `MemoryInput`, `DiagnosticInput`, `TranscriptEventInput`

**SDK 테이블 행 타입:**

- `SDKSessionRow` -- sdk_sessions (worker_port, prompt_counter 포함)
- `ObservationRow` -- observations (전체 계층적 필드 + discovery_tokens)
- `SessionSummaryRow` -- session_summaries (discovery_tokens 포함)
- `UserPromptRow` -- user_prompts

**검색/필터 타입:**

- `DateRange` -- start/end (ISO string 또는 epoch)
- `SearchFilters` -- project, type, concepts, files, dateRange
- `SearchOptions` -- SearchFilters + limit, offset, orderBy, isFolder
- `ObservationSearchResult` -- ObservationRow + rank?, score?
- `SessionSummarySearchResult` -- SessionSummaryRow + rank?, score?
- `UserPromptSearchResult` -- UserPromptRow + rank?, score?

**유틸리티 함수:**

`normalizeTimestamp(timestamp)` -- string, Date, number, undefined를 `{ isoString, epoch }` 쌍으로 정규화한다. 빈 문자열, 잘못된 형식도 안전하게 처리하여 현재 시각으로 폴백한다.

### sessions/types.ts (62L)

세션 쿼리 결과를 위한 독립 타입:

- `SessionBasic` -- id, content_session_id, memory_session_id, project, user_prompt, custom_title
- `SessionFull` -- SessionBasic + 타임스탬프, status
- `SessionWithStatus` -- memory_session_id, status, started_at, user_prompt, has_summary
- `SessionSummaryDetail` -- 세션 + 요약 조인 결과 (request_summary, learned_summary 포함)

---

## 부록: 엔티티 관계도

```
sdk_sessions (PK: id)
  ├──< observations        (FK: memory_session_id -> memory_session_id, CASCADE)
  ├──< session_summaries   (FK: memory_session_id -> memory_session_id, CASCADE)
  ├──< user_prompts        (FK: content_session_id -> content_session_id, CASCADE)
  └──< pending_messages    (FK: session_db_id -> id, CASCADE)

observations_fts          (FTS5 content='observations')
session_summaries_fts     (FTS5 content='session_summaries')
user_prompts_fts          (FTS5 content='user_prompts')

schema_versions           (마이그레이션 추적, 독립)

[레거시 테이블]
sessions -> memories, overviews, diagnostics, transcript_events
```

핵심 관계는 `sdk_sessions`를 중심으로 한 1:N 관계이다. `content_session_id`는 사용자가 관찰하는 Claude Code 세션 ID이고, `memory_session_id`는 메모리 에이전트가 사용하는 SDK 세션 ID이다. 두 ID는 비동기적으로 연결되며, `user_prompts`는 `content_session_id`로 참조하고 `observations`와 `session_summaries`는 `memory_session_id`로 참조한다. 모든 FK에 ON DELETE CASCADE와 ON UPDATE CASCADE가 설정되어 있어, 세션 삭제 시 모든 관련 데이터가 자동으로 함께 삭제된다.
