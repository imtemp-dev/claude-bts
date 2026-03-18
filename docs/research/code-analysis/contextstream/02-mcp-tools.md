# ContextStream -- MCP 도구 분석

> 분석 대상: `contextstream/src/tools.ts` (15,460 lines)
> 분석 일자: 2026-03-17

---

## 1. 도구 등록 패턴 (Consolidated Domain Tool Pattern)

### 1.1 핵심 설계 철학

ContextStream v0.4.x의 기본 동작 모드는 **Consolidated Domain Tool** 패턴이다. 기존에 60개 이상의 개별 MCP 도구를 등록하던 방식에서, **약 11개의 도메인 도구**에 `action` 파라미터를 두어 내부적으로 switch/case 디스패치하는 구조로 전환했다. 이 전략은 소스 코드에서 "Strategy 8"로 지칭된다.

```
// Strategy 8: Consolidated Domain Tools (v0.4.x default)
// Environment variable to control consolidated mode
// CONTEXTSTREAM_CONSOLIDATED=true | false (default: true in v0.4.x)
// When enabled, registers ~11 domain tools instead of ~58 individual tools
const CONSOLIDATED_MODE = process.env.CONTEXTSTREAM_CONSOLIDATED !== "false";
```

토큰 절감 효과는 약 **75%**이다. 기존 60+ 개별 도구의 스키마가 각각 MCP initialize 응답에 포함되던 것을, 11개 도메인 도구의 스키마로 압축했기 때문이다.

### 1.2 CONSOLIDATED_TOOLS Set

`CONSOLIDATED_TOOLS`는 도메인 도구의 이름 집합이다. 이 Set에 포함된 도구만 CONSOLIDATED_MODE에서 실제 MCP 서버에 등록된다:

```typescript
const CONSOLIDATED_TOOLS = new Set<string>([
  "init",              // 독립 도구 - 세션 초기화 (session_init에서 renamed)
  "context",           // 독립 도구 - 매 메시지 호출 (context_smart에서 renamed)
  "generate_rules",    // 독립 도구 - 규칙 파일 생성
  "generate_editor_rules", // 독립 도구 - 에디터별 규칙 생성
  "flash",             // instruct의 호환 별칭
  "instruct",          // 세션 범위 지시 캐시
  "ram",               // instruct의 호환 별칭
  "mem",               // instruct의 호환 별칭
  "search",            // search_semantic, search_hybrid, search_keyword 등 통합
  "session",           // session_capture, session_recall 등 통합
  "memory",            // memory_create_event, memory_get_event 등 통합
  "graph",             // graph_dependencies, graph_impact 등 통합
  "project",           // projects_list, projects_create 등 통합
  "workspace",         // workspaces_list, workspace_associate 등 통합
  "reminder",          // reminders_list, reminders_create 등 통합
  "integration",       // slack_*, github_*, notion_*, integrations_* 통합
  "media",             // media indexing, search, clip retrieval 통합
  "help",              // session_tools, auth_me, mcp_server_version 등 통합
]);
```

### 1.3 도메인 매핑 함수

`mapToolToConsolidatedDomain()` 함수가 기존 개별 도구 이름을 통합 도메인으로 매핑한다:

```typescript
function mapToolToConsolidatedDomain(toolName: string): string | null {
  if (CONSOLIDATED_TOOLS.has(toolName)) return toolName;
  if (toolName === "session_init") return "init";
  if (toolName === "context_smart") return "context";
  if (toolName.startsWith("search_")) return "search";
  if (toolName.startsWith("session_") || toolName === "context_feedback") return "session";
  if (toolName.startsWith("memory_") || toolName === "decision_trace") return "memory";
  if (toolName.startsWith("graph_")) return "graph";
  if (toolName.startsWith("projects_")) return "project";
  if (toolName.startsWith("workspaces_") || toolName.startsWith("workspace_")) return "workspace";
  if (toolName.startsWith("reminders_")) return "reminder";
  if (toolName.startsWith("slack_") || toolName.startsWith("github_") ||
      toolName.startsWith("integrations_") || toolName.startsWith("notion_")) return "integration";
  if (toolName.startsWith("media_")) return "media";
  if (toolName === "session_tools" || toolName === "auth_me" ||
      toolName === "mcp_server_version" || toolName === "tools_enable_bundle") return "help";
  return null;
}
```

### 1.4 등록 흐름

`registerTool()` 래퍼 함수가 모든 도구의 등록 시점에 다중 필터링을 적용한다. 순서가 중요하다:

1. **operationsRegistry에 항상 등록** -- Router Mode에서 디스패치용으로 사용
2. **CONSOLIDATED_MODE 필터** -- `CONSOLIDATED_TOOLS`에 없으면 MCP 등록 건너뜀
3. **toolAllowlist 필터** -- LIGHT/STANDARD 등 프로파일의 allowlist 확인
4. **Integration auto-hide 필터** -- 연결되지 않은 통합 도구 건너뜀
5. **ROUTER_MODE 필터** -- Router 직접 도구만 등록
6. **PROGRESSIVE_MODE 필터** -- 활성 번들에 속하지 않으면 지연 등록

```typescript
function registerTool<T extends z.ZodType>(name, config, handler) {
  operationsRegistry.set(name, { ... });

  if (CONSOLIDATED_MODE && !CONSOLIDATED_TOOLS.has(name) && !AGENTIC_DIRECT_TOOLS.has(name))
    return;
  if (toolAllowlist && !toolAllowlist.has(name) && !AGENTIC_DIRECT_TOOLS.has(name))
    return;
  if (!CONSOLIDATED_MODE && !shouldRegisterIntegrationTool(name))
    return;
  if (ROUTER_MODE && !ROUTER_DIRECT_TOOLS.has(name))
    return;
  if (PROGRESSIVE_MODE && !isToolInEnabledBundles(name))
    return;

  actuallyRegisterTool(name, config, handler);
}
```

`actuallyRegisterTool()`은 실제로 `serverRef.registerTool()`을 호출하며, 이 과정에서:
- COMPACT_SCHEMA 모드이면 `compactifyDescription()`과 `applyCompactParamDescriptions()` 적용
- 모든 핸들러를 `gateIfProTool()` 및 `gateIfIntegrationTool()` 게이트로 래핑
- `inferToolAnnotations()`으로 MCP ToolAnnotations 자동 추론
- `wrapWithAutoContext()`로 자동 컨텍스트 캡처 래핑

---

## 2. 도구 프로파일 (Tool Profiles)

### 2.1 LIGHT_TOOLSET (~43 tools)

토큰에 민감한 클라이언트(Claude Code, Claude Desktop)를 위한 최소 도구 집합이다. 환경 변수 `CONTEXTSTREAM_TOOLSET=light` 또는 자동 감지(`CONTEXTSTREAM_AUTO_TOOLSET=true` + Claude Code 환경)로 활성화된다.

포함 범위:
- Core session tools (15): `session_init`, `session_tools`, `context_smart`, `context_feedback`, `session_summary`, `session_capture`, `session_capture_smart`, `session_restore_context`, `session_capture_lesson`, `session_get_lessons`, `session_recall`, `session_remember`, `session_get_user_context`, `session_decision_trace`, `session_smart_search`, `session_compress`, `session_delta`
- Plans (4): `capture_plan`, `get_plan`, `update_plan`, `list_plans`
- Setup (4): `generate_editor_rules`, `generate_rules`, `workspace_associate`, `workspace_bootstrap`
- Project (5): `projects_create`, `projects_list`, `projects_get`, `projects_overview`, `projects_statistics`
- Indexing (4): `projects_ingest_local`, `projects_index`, `projects_index_status`, `projects_files`
- Memory (3): `memory_search`, `memory_decisions`, `memory_get_event`
- Graph (2): `graph_related`, `graph_decisions`
- Reminders (2): `reminders_list`, `reminders_active`
- Utility (2): `auth_me`, `mcp_server_version`

### 2.2 STANDARD_TOOLSET (~76 tools)

기본(default) 프로파일이다. `CONTEXTSTREAM_TOOLSET=standard`이거나 명시적 설정이 없을 때 적용된다.

LIGHT_TOOLSET의 모든 도구에 추가로:
- Workspace CRUD: `workspaces_list`, `workspaces_get`, `workspaces_create`, `workspaces_delete`
- Project CRUD 확장: `projects_update`, `projects_delete`
- Memory events 전체: `memory_create_event`, `memory_list_events`, `memory_update_event`, `memory_delete_event`, `memory_timeline`, `memory_summary`, `decision_trace`
- Memory nodes 전체: `memory_create_node`, `memory_list_nodes`, `memory_get_node`, `memory_update_node`, `memory_delete_node`, `memory_supersede_node`
- Memory distillation: `memory_distill_event`
- Graph 확장: `graph_path`, `graph_dependencies`, `graph_call_path`, `graph_impact`, `graph_circular_dependencies`, `graph_unused_code`, `graph_ingest`
- Search: `search_semantic`, `search_hybrid`, `search_keyword`
- Aliases: `flash`, `instruct`, `ram`, `mem`
- Reminders 전체: `reminders_create`, `reminders_snooze`, `reminders_complete`, `reminders_dismiss`

### 2.3 OPENAI_AGENTIC_CORE_TOOLSET (~9 tools)

OpenAI 기반 에이전틱 클라이언트를 위한 최소 도구 집합이다. `CONTEXTSTREAM_TOOL_SURFACE_PROFILE=openai_agentic`으로 활성화된다. 통합 도메인 도구 이름을 직접 사용한다:

```typescript
const OPENAI_AGENTIC_CORE_TOOLSET = new Set<string>([
  "init", "context", "session", "instruct",
  "search", "memory", "project", "workspace", "help",
]);
```

이 프로파일에서는 추가로 `tool_search`, `execute_operation`, `batch_operations`라는 AGENTIC_DIRECT_TOOLS가 등록되어, 9개 핵심 도구 밖의 나머지 도구를 동적으로 탐색 및 실행할 수 있다.

### 2.4 Complete 모드

`CONTEXTSTREAM_TOOLSET=complete` (또는 `full`, `all`)로 설정하면 allowlist가 null이 되어 모든 도구가 등록된다. 통합 도구 포함.

### 2.5 TOOLSET_ALIASES

```typescript
const TOOLSET_ALIASES: Record<string, Set<string> | null> = {
  light: LIGHT_TOOLSET,
  minimal: LIGHT_TOOLSET,
  standard: STANDARD_TOOLSET,
  core: STANDARD_TOOLSET,
  essential: STANDARD_TOOLSET,
  complete: null,
  full: null,
  all: null,
};
```

### 2.6 Progressive Disclosure (번들 모드)

`CONTEXTSTREAM_PROGRESSIVE_MODE=true`이면 core 번들만 초기 등록되고, AI가 `tools_enable_bundle`을 호출하여 동적으로 번들을 활성화할 수 있다.

번들 정의:

| 번들 이름 | 도구 수 | 설명 |
|-----------|---------|------|
| core | ~13 | 필수 세션 도구 (항상 활성) |
| session | ~7 | 확장 세션 관리 |
| memory | ~16 | Memory CRUD 전체 |
| search | ~3 | semantic, hybrid, keyword |
| graph | ~9 | 코드 그래프 분석 |
| workspace | ~4 | 워크스페이스 관리 |
| project | ~10 | 프로젝트 관리 및 인덱싱 |
| reminders | ~6 | 리마인더 관리 |
| integrations | ~32 | Slack/GitHub/Notion 통합 |

번들 활성화 시 `deferredTools`에 저장된 도구를 `actuallyRegisterTool()`로 등록하고, `sendToolsListChanged()` 알림을 발송한다.

---

## 3. 전체 도구 목록 (Complete Action Reference)

### 3.1 `session` -- 세션 관리 (22 actions)

통합 대상: `session_capture`, `session_recall`, `session_remember`, `session_capture_lesson`, `session_get_lessons`, `session_get_user_context`, `session_summary`, `session_compress`, `session_delta`, `session_smart_search`, `session_decision_trace`, `session_restore_context`, `capture_plan`, `get_plan`, `update_plan`, `list_plans`, `context_feedback`

| # | action | 설명 | 필수 파라미터 |
|---|--------|------|--------------|
| 1 | `capture` | 결정/인사이트/노트 등을 이벤트로 저장 | event_type, title, content |
| 2 | `capture_lesson` | 실수에서 배운 교훈을 저장 (자동 중복 방지) | title, trigger, impact, prevention |
| 3 | `get_lessons` | 컨텍스트 관련 교훈 조회 | workspace_id (세션에서 자동 해석) |
| 4 | `recall` | 자연어 기반 메모리 회상 | query |
| 5 | `remember` | 사용자 선호/규칙 빠른 저장 | content |
| 6 | `user_context` | 사용자 환경설정 및 선호 조회 | -- |
| 7 | `summary` | 워크스페이스 컨텍스트 요약 | -- |
| 8 | `compress` | 채팅 히스토리 압축 | content |
| 9 | `delta` | 특정 시점 이후 변경사항 조회 | since (ISO timestamp) |
| 10 | `smart_search` | 컨텍스트 강화 검색 | query |
| 11 | `decision_trace` | 결정의 출처와 영향 추적 | query |
| 12 | `restore_context` | 압축 후 상태 복원 (스냅샷 기반) | -- (snapshot_id 선택) |
| 13 | `capture_plan` | 구현 계획 저장 | title |
| 14 | `get_plan` | 계획 상세 조회 (태스크 포함) | plan_id |
| 15 | `update_plan` | 계획 수정 | plan_id |
| 16 | `list_plans` | 모든 계획 목록 조회 | -- |
| 17 | `team_decisions` | 팀 전체 결정 조회 (Team plan 전용) | -- |
| 18 | `team_lessons` | 팀 전체 교훈 조회 (Team plan 전용) | -- |
| 19 | `team_plans` | 팀 워크스페이스 전체 계획 조회 (Team plan 전용) | -- |
| 20 | `list_suggested_rules` | ML이 생성한 규칙 제안 목록 조회 | -- |
| 21 | `suggested_rule_action` | 제안 규칙 수락/거부/수정 | rule_id, rule_action |
| 22 | `suggested_rules_stats` | ML 규칙 정확도 통계 조회 | -- |

주요 공유 파라미터: `workspace_id`, `project_id`, `query`, `content`, `title`, `event_type`, `importance`, `tags`, `plan_id`, `snapshot_id`, `rule_id`, `rule_action`, `code_refs`, `provenance`

### 3.2 `search` -- 검색 (9 modes)

통합 대상: `search_semantic`, `search_hybrid`, `search_keyword`, `search_pattern`

| # | mode | 설명 |
|---|------|------|
| 1 | `auto` | 자동 모드 선택 (기본값, 권장) |
| 2 | `semantic` | 의미 기반 검색 (임베딩) |
| 3 | `hybrid` | auto의 하위 호환 별칭 (semantic + keyword 결합) |
| 4 | `keyword` | 정확 일치 검색 |
| 5 | `pattern` | 정규식 기반 검색 |
| 6 | `exhaustive` | grep과 유사한 전체 탐색 |
| 7 | `refactor` | 단어 경계 기반 심볼 이름 변경용 검색 |
| 8 | `team` | 팀 워크스페이스 전체 교차 검색 (Team plan 전용) |
| 9 | `crawl` | 딥 멀티모달 검색 |

출력 형식 (`output_format`): `full` (기본), `paths` (80% 토큰 절감), `minimal` (60% 절감), `count` (90% 절감)

검색 파라미터: `query` (필수), `workspace_id`, `project_id`, `limit`, `offset`, `content_max_chars`, `context_lines`, `exact_match_boost`, `output_format`

자동 폴백 체인: keyword 실패시 refactor -> exhaustive -> semantic -> hybrid 순으로 자동 재시도. hybrid 결과가 낮은 신뢰도이면 semantic으로 자동 전환.

### 3.3 `memory` -- 메모리 관리 (48 actions)

통합 대상: `memory_create_event`, `memory_get_event`, `memory_update_event`, `memory_delete_event`, `memory_list_events`, `memory_distill_event`, `memory_create_node`, `memory_get_node`, `memory_update_node`, `memory_delete_node`, `memory_list_nodes`, `memory_supersede_node`, `memory_search`, `memory_decisions`, `memory_timeline`, `memory_summary`, `decision_trace`

**Event 관련 (7 actions):**

| # | action | 설명 | 필수 파라미터 |
|---|--------|------|--------------|
| 1 | `create_event` | 메모리 이벤트 생성 | event_type, title, content |
| 2 | `get_event` | 이벤트 상세 조회 | event_id |
| 3 | `update_event` | 이벤트 수정 | event_id |
| 4 | `delete_event` | 이벤트 삭제 | event_id |
| 5 | `list_events` | 이벤트 목록 조회 | -- |
| 6 | `distill_event` | 이벤트 증류 (요약 추출) | event_id |
| 7 | `import_batch` | 이벤트 대량 가져오기 | events (배열) |

**Node 관련 (6 actions):**

| # | action | 설명 | 필수 파라미터 |
|---|--------|------|--------------|
| 8 | `create_node` | 지식 노드 생성 | title, content, node_type |
| 9 | `get_node` | 노드 상세 조회 | node_id |
| 10 | `update_node` | 노드 수정 | node_id |
| 11 | `delete_node` | 노드 삭제 | node_id |
| 12 | `list_nodes` | 노드 목록 조회 | -- |
| 13 | `supersede_node` | 노드를 새 내용으로 대체 | node_id, new_content |

**Query 관련 (4 actions):**

| # | action | 설명 |
|---|--------|------|
| 14 | `search` | 메모리 이벤트 의미 검색 |
| 15 | `decisions` | 결정 이벤트 필터 조회 |
| 16 | `timeline` | 시간순 이벤트 타임라인 |
| 17 | `summary` | 메모리 요약 |

**Task 관련 (6 actions):**

| # | action | 설명 | 필수 파라미터 |
|---|--------|------|--------------|
| 18 | `create_task` | 태스크 생성 (계획 연결 선택) | title |
| 19 | `get_task` | 태스크 상세 조회 | task_id |
| 20 | `update_task` | 태스크 수정 (plan_id로 연결/해제) | task_id |
| 21 | `delete_task` | 태스크 삭제 | task_id |
| 22 | `list_tasks` | 태스크 목록 (plan_id로 필터) | -- |
| 23 | `reorder_tasks` | 태스크 순서 변경 | task_ids (배열) |

**Todo 관련 (6 actions):**

| # | action | 설명 | 필수 파라미터 |
|---|--------|------|--------------|
| 24 | `create_todo` | 할일 생성 | title |
| 25 | `list_todos` | 할일 목록 조회 | -- |
| 26 | `get_todo` | 할일 상세 조회 | todo_id |
| 27 | `update_todo` | 할일 수정 | todo_id |
| 28 | `delete_todo` | 할일 삭제 | todo_id |
| 29 | `complete_todo` | 할일 완료 처리 | todo_id |

**Diagram 관련 (5 actions):**

| # | action | 설명 | 필수 파라미터 |
|---|--------|------|--------------|
| 30 | `create_diagram` | Mermaid 다이어그램 생성 | title, content |
| 31 | `list_diagrams` | 다이어그램 목록 조회 | -- |
| 32 | `get_diagram` | 다이어그램 상세 조회 | diagram_id |
| 33 | `update_diagram` | 다이어그램 수정 | diagram_id |
| 34 | `delete_diagram` | 다이어그램 삭제 | diagram_id |

**Doc 관련 (6 actions):**

| # | action | 설명 | 필수 파라미터 |
|---|--------|------|--------------|
| 35 | `create_doc` | 문서 생성 | title, content |
| 36 | `list_docs` | 문서 목록 조회 | -- |
| 37 | `get_doc` | 문서 상세 조회 | doc_id |
| 38 | `update_doc` | 문서 수정 | doc_id |
| 39 | `delete_doc` | 문서 삭제 | doc_id |
| 40 | `create_roadmap` | 로드맵 문서 생성 (milestones 포함) | title, milestones |

**Transcript 관련 (4 actions):**

| # | action | 설명 | 필수 파라미터 |
|---|--------|------|--------------|
| 41 | `list_transcripts` | 저장된 대화 목록 조회 | -- |
| 42 | `get_transcript` | 대화 전체 내용 조회 | transcript_id |
| 43 | `search_transcripts` | 대화 의미 검색 | query |
| 44 | `delete_transcript` | 대화 삭제 | transcript_id |

**Team 관련 (4 actions, Team plan 전용):**

| # | action | 설명 |
|---|--------|------|
| 45 | `team_tasks` | 팀 전체 태스크 조회 |
| 46 | `team_todos` | 팀 전체 할일 조회 |
| 47 | `team_diagrams` | 팀 전체 다이어그램 조회 |
| 48 | `team_docs` | 팀 전체 문서 조회 |

총 action 수: **48** (스키마의 enum에 정의된 action은 45개이며 일부 복합 동작 포함)

### 3.4 `graph` -- 코드 그래프 분석 (11 actions)

통합 대상: `graph_related`, `graph_path`, `graph_decisions`, `graph_dependencies`, `graph_impact`, `graph_call_path`, `graph_ingest`, `graph_circular_dependencies`, `graph_unused_code`, `graph_contradictions`

| # | action | 설명 | 필수 파라미터 | Graph 티어 |
|---|--------|------|--------------|-----------|
| 1 | `dependencies` | 모듈 의존성 분석 | target { type, id } | lite |
| 2 | `impact` | 변경 영향 분석 | target { type, id } | lite |
| 3 | `call_path` | 함수 호출 경로 추적 | source + target | full |
| 4 | `related` | 관련 노드 조회 | node_id | full |
| 5 | `path` | 두 노드 간 경로 탐색 | source_id, target_id | full |
| 6 | `decisions` | 결정 히스토리 조회 | -- | full |
| 7 | `ingest` | 코드 그래프 구축 | project_id | lite |
| 8 | `circular_dependencies` | 순환 의존성 탐지 | project_id | full |
| 9 | `unused_code` | 미사용 코드 탐지 | project_id | full |
| 10 | `contradictions` | 모순 탐지 | node_id | full |
| 11 | `usages` | 역방향 의존성 조회 | target_id, project_id | -- |

파라미터: `workspace_id`, `project_id`, `node_id`, `source_id`, `target_id`, `target_type`, `target { type, id }`, `source { type, id }`, `max_depth`, `include_transitive`, `limit`, `wait`

### 3.5 `project` -- 프로젝트 관리 (14 actions)

통합 대상: `projects_list`, `projects_get`, `projects_create`, `projects_update`, `projects_delete`, `projects_index`, `projects_overview`, `projects_statistics`, `projects_files`, `projects_index_status`, `projects_ingest_local`

| # | action | 설명 | 필수 파라미터 |
|---|--------|------|--------------|
| 1 | `list` | 프로젝트 목록 | -- |
| 2 | `get` | 프로젝트 상세 조회 | project_id |
| 3 | `create` | 프로젝트 생성 | name |
| 4 | `update` | 프로젝트 수정 | project_id |
| 5 | `delete` | 프로젝트 삭제 | project_id |
| 6 | `index` | 인덱싱 트리거 | project_id |
| 7 | `overview` | 프로젝트 개요 | project_id |
| 8 | `statistics` | 프로젝트 통계 | project_id |
| 9 | `files` | 프로젝트 파일 목록 | project_id |
| 10 | `index_status` | 인덱스 상태 확인 (freshness, confidence 포함) | project_id (자동 해석) |
| 11 | `index_history` | 인덱스 감사 추적 | project_id |
| 12 | `ingest_local` | 로컬 폴더 인덱싱 (백그라운드) | path, project_id |
| 13 | `team_projects` | 팀 전체 프로젝트 조회 (Team plan 전용) | -- |
| 14 | `recent_changes` | git log/diff 기반 최근 변경 조회 | folder_path |

추가 파라미터: `folder_path`, `path`, `overwrite`, `write_to_disk`, `force`, `machine_id`, `branch`, `since`, `until`, `path_pattern`, `sort_by`, `sort_order`, `page`, `page_size`, `generate_editor_rules`

### 3.6 `workspace` -- 워크스페이스 관리 (8 actions)

통합 대상: `workspaces_list`, `workspaces_get`, `workspaces_create`, `workspaces_delete`, `workspace_associate`, `workspace_bootstrap`

| # | action | 설명 | 필수 파라미터 |
|---|--------|------|--------------|
| 1 | `list` | 워크스페이스 목록 | -- |
| 2 | `get` | 워크스페이스 상세 조회 | workspace_id |
| 3 | `create` | 워크스페이스 생성 | name |
| 4 | `delete` | 워크스페이스 삭제 | workspace_id |
| 5 | `associate` | 폴더와 워크스페이스 연결 | folder_path, workspace_id |
| 6 | `bootstrap` | 워크스페이스 생성 + 초기화 | workspace_name |
| 7 | `team_members` | 팀 멤버 목록 (Team plan 전용) | -- |
| 8 | `index_settings` | 멀티머신 인덱스 동기화 설정 조회/수정 (admin 전용) | workspace_id |

index_settings 파라미터: `branch_policy` (default_branch_wins / newest_wins / feature_branch_wins), `conflict_resolution` (newest_timestamp / default_branch / manual), `allowed_machines`, `auto_sync_enabled`, `max_machines`

### 3.7 `reminder` -- 리마인더 관리 (6 actions)

통합 대상: `reminders_list`, `reminders_active`, `reminders_create`, `reminders_snooze`, `reminders_complete`, `reminders_dismiss`

| # | action | 설명 | 필수 파라미터 |
|---|--------|------|--------------|
| 1 | `list` | 리마인더 목록 | -- |
| 2 | `active` | 대기/연체 리마인더 조회 | -- |
| 3 | `create` | 리마인더 생성 | title, content, remind_at |
| 4 | `snooze` | 리마인더 연기 | reminder_id, until |
| 5 | `complete` | 리마인더 완료 | reminder_id |
| 6 | `dismiss` | 리마인더 무시 | reminder_id |

### 3.8 `integration` -- 통합 관리 (18 actions + provider 파라미터)

통합 대상: `slack_*` (9), `github_*` (8), `notion_*` (10), `integrations_*` (4)

`provider` 파라미터: `slack`, `github`, `notion`, `all`

**공통 actions (모든 provider):**

| # | action | 설명 |
|---|--------|------|
| 1 | `status` | 통합 연결 상태 확인 |
| 2 | `search` | 통합 소스 검색 |
| 3 | `stats` | 통합 통계 |
| 4 | `activity` | 최근 활동 조회 |
| 5 | `contributors` | 기여자 목록 (slack, github만) |
| 6 | `knowledge` | 지식 그래프 노드 조회 |
| 7 | `summary` | 통합 소스 요약 |

**Slack 전용 actions:**

| # | action | 설명 |
|---|--------|------|
| 8 | `channels` | Slack 채널 목록 |
| 9 | `discussions` | Slack 토론 스레드 |
| 10 | `sync_users` | Slack 사용자 동기화 |

**GitHub 전용 actions:**

| # | action | 설명 |
|---|--------|------|
| 11 | `repos` | GitHub 리포지토리 목록 |
| 12 | `issues` | GitHub 이슈/PR 목록 |

**Notion 전용 actions:**

| # | action | 설명 | 필수 파라미터 |
|---|--------|------|--------------|
| 13 | `create_page` | Notion 페이지 생성 | title |
| 14 | `create_database` | Notion 데이터베이스 생성 | title, parent_page_id |
| 15 | `list_databases` | Notion 데이터베이스 목록 | -- |
| 16 | `search_pages` | Notion 페이지 검색 (스마트 타입 감지) | -- |
| 17 | `get_page` | Notion 페이지 조회 | page_id |
| 18 | `query_database` | Notion 데이터베이스 쿼리 | database_id |
| 19 | `update_page` | Notion 페이지 수정 | page_id |

**Team action:**

| # | action | 설명 |
|---|--------|------|
| 20 | `team_activity` | 팀 전체 통합 활동 집계 (Team plan 전용) |

Notion search_pages 스마트 필터: `event_type` (NotionTask, NotionMeeting, NotionWiki 등), `status`, `priority`, `has_due_date`, `tags`

### 3.9 `media` -- 미디어 관리 (6 actions)

비디오/오디오/이미지 에셋의 인덱싱, 검색, 클립 추출을 통합한다. Remotion/FFmpeg 워크플로우를 위해 설계되었다.

| # | action | 설명 | 필수 파라미터 |
|---|--------|------|--------------|
| 1 | `index` | 로컬 파일 또는 외부 URL 인덱싱 (Whisper/CLIP/keyframe) | file_path 또는 external_url |
| 2 | `status` | 인덱싱 진행 상태 확인 | content_id |
| 3 | `search` | 인덱스된 미디어 의미 검색 | query |
| 4 | `get_clip` | 시간 범위 클립 상세 (remotion/ffmpeg/raw 형식) | content_id, start, end |
| 5 | `list` | 인덱스된 미디어 자산 목록 | -- |
| 6 | `delete` | 미디어 자산 인덱스 삭제 | content_id |

`output_format`: `remotion` (프레임 기반 props), `ffmpeg` (타임코드), `raw` (초 단위)

`content_type`: `video`, `audio`, `image`, `document` (자동 감지 가능)

`index` action은 presigned URL 업로드 방식을 사용한다: `mediaInitUpload` -> S3 PUT -> `mediaCompleteUpload`

### 3.10 `help` -- 유틸리티 (6 actions)

통합 대상: `session_tools`, `auth_me`, `mcp_server_version`, `generate_editor_rules`, `tools_enable_bundle`

| # | action | 설명 |
|---|--------|------|
| 1 | `tools` | 사용 가능한 도구 카탈로그 조회 (format: grouped/minimal/full) |
| 2 | `auth` | 현재 인증 사용자 정보 |
| 3 | `version` | MCP 서버 버전 정보 |
| 4 | `editor_rules` | AI 에디터 규칙 생성 + Claude Code 훅 설치 |
| 5 | `enable_bundle` | 도구 번들 활성화 (Progressive Mode 전용) |
| 6 | `team_status` | 팀 구독 정보 조회 (Team plan 전용) |

### 3.11 독립 도구 및 별칭

**독립 도구 (Standalone):**

| 도구 이름 | 설명 |
|-----------|------|
| `init` | 세션 초기화 (session_init에서 renamed). 워크스페이스/프로젝트 자동 해석, 인덱싱 상태 확인, 규칙 업데이트 알림, 컨텍스트 복원 포함 |
| `context` | 매 메시지 필수 호출 (context_smart에서 renamed). user_message 기반 관련 컨텍스트 반환, 교훈/리마인더 자동 주입 |
| `generate_rules` | 에디터별 규칙 파일 생성 (CLAUDE.md, .cursorrules 등) |
| `generate_editor_rules` | generate_rules와 동일 (별칭) |

**별칭 도구 (Aliases):**

| 별칭 | 원본 | 설명 |
|------|------|------|
| `flash` | `instruct` | 세션 범위 지시 캐시 (호환 별칭) |
| `ram` | `instruct` | 세션 범위 지시 캐시 (호환 별칭) |
| `mem` | `instruct` | 세션 범위 지시 캐시 (호환 별칭) |

`instruct` 도구는 세션 수명 동안 유지되는 지시사항을 캐시한다. 네 개의 이름 모두 동일한 `instructionToolSchema`와 `executeInstructionTool` 핸들러를 공유한다.

---

## 4. 스키마 및 디스패치 (Schema & Dispatch)

### 4.1 Zod 스키마 검증

모든 도구의 `inputSchema`는 Zod로 정의된다. 통합 도메인 도구의 경우 `z.object()`에 `action` 필드를 `z.enum([...])`으로 정의하고, 나머지 파라미터는 전체 action에서 사용할 수 있는 합집합(union)으로 선언한다.

```typescript
inputSchema: z.object({
  action: z.enum(["capture", "capture_lesson", "get_lessons", ...]).describe("Action to perform"),
  workspace_id: z.string().uuid().optional(),
  project_id: z.string().uuid().optional(),
  query: z.string().optional(),
  content: z.string().optional(),
  // ... 모든 action에서 사용 가능한 파라미터의 합집합
})
```

이 "flat union" 접근법은 discriminated union보다 스키마가 단순하지만, 각 action에서 실제 어떤 파라미터가 필요한지는 핸들러의 런타임 검증에 의존한다:

```typescript
case "capture": {
  if (!input.event_type || !input.title || !input.content) {
    return errorResult("capture requires: event_type, title, content");
  }
  // ...
}
```

### 4.2 switch/case 디스패치

각 통합 도메인 도구 핸들러 내부에서 `switch (input.action)` 문으로 해당 action의 로직을 디스패치한다:

```typescript
async (input) => {
  let workspaceId = resolveWorkspaceId(input.workspace_id);
  const projectId = resolveProjectId(input.project_id);

  switch (input.action) {
    case "capture": { /* ... */ }
    case "recall": { /* ... */ }
    // ...
    default:
      return errorResult(`Unknown action: ${input.action}`);
  }
}
```

### 4.3 공유 파라미터 (Shared Parameters)

거의 모든 도구에서 공유하는 파라미터:

- `workspace_id` -- `resolveWorkspaceId()`로 세션 컨텍스트에서 자동 해석
- `project_id` -- `resolveProjectId()`로 세션 컨텍스트에서 자동 해석

해석 우선순위:
1. 명시적으로 전달된 값
2. SessionManager에 저장된 세션 컨텍스트
3. `resolveWorkspace(folderPath)`로 폴더 매핑에서 추출
4. 로컬 인덱스 매핑에서 추출

### 4.4 스키마 압축 (Compact Schema Mode)

`CONTEXTSTREAM_SCHEMA_MODE=compact`일 때:

- `compactifyDescription()`: 도구 설명을 첫 문장으로 자르고 120자 이내로 축소
- `applyCompactParamDescriptions()`: 파라미터 설명을 첫 절(clause)로 줄이고 40자 이내로 축소
- 자동 생성 설명 생략 (명시적으로 제공된 것만 유지)

### 4.5 결과 포맷

`formatContent()` 함수가 `CONTEXTSTREAM_OUTPUT_FORMAT` 환경 변수에 따라 compact(minified) 또는 pretty(indented) JSON을 출력한다. 기본값은 compact으로, 약 30%의 토큰 절감 효과가 있다.

`maybeStripStructuredContent()`는 `CONTEXTSTREAM_INCLUDE_STRUCTURED_CONTENT=false`일 때 structuredContent 필드를 제거한다.

---

## 5. 도구 접근 제어 (Tool Access Control)

### 5.1 proTools Set (Pro 도구 게이트)

`defaultProTools`는 유료 플랜이 필요한 도구를 정의한다:

```typescript
const defaultProTools = new Set<string>([
  // AI endpoints
  "ai_context", "ai_enhanced_context", "ai_context_budget",
  "ai_embeddings", "ai_plan", "ai_tasks",
  // Slack integration
  "slack_stats", "slack_channels", "slack_contributors",
  "slack_activity", "slack_discussions", "slack_search", "slack_sync_users",
  // GitHub integration
  "github_stats", "github_repos", "github_contributors",
  "github_activity", "github_issues", "github_search",
  // Notion integration
  "notion_create_page", "notion_list_databases", "notion_search_pages",
  "notion_get_page", "notion_query_database", "notion_update_page",
  "notion_stats", "notion_activity", "notion_knowledge", "notion_summary",
  // Media operations
  "media_index", "media_search",
]);
```

`CONTEXTSTREAM_PRO_TOOLS` 환경 변수로 커스텀 오버라이드 가능하다.

### 5.2 gateIfProTool()

모든 도구 핸들러 실행 전에 `safeHandler` 래퍼에서 호출된다:

```typescript
async function gateIfProTool(toolName: string): Promise<ToolTextResult | null> {
  if (getToolAccessTier(toolName) !== "pro") return null;
  const planName = await client.getPlanName();
  if (planName !== "free") return null;
  return errorResult(`Access denied: \`${toolName}\` requires ContextStream PRO.`);
}
```

### 5.3 gateIfGraphTool()

Graph 도구는 2단계 티어(lite/full)로 세분화된다:

| 티어 | 도구 | 제한 |
|------|------|------|
| lite | `graph_dependencies`, `graph_impact`, `graph_ingest` | 모듈 수준, max_depth=1, transitive 불가 |
| full | `graph_related`, `graph_decisions`, `graph_path`, `graph_call_path`, `graph_circular_dependencies`, `graph_unused_code`, `graph_contradictions` | Elite/Team 플랜 필요 |

```typescript
async function gateIfGraphTool(toolName: string, input?: any): Promise<ToolTextResult | null> {
  const requiredTier = graphToolTiers.get(toolName);
  if (!requiredTier) return null;
  const graphTier = await client.getGraphTier();
  if (graphTier === "full") return null;
  if (graphTier === "lite") {
    if (requiredTier === "full") return errorResult("Elite/Team required");
    // lite 도구에 대해 target.type=module, max_depth=1 등 제약 검증
  }
  // graphTier === "none" -> Pro 업그레이드 안내
}
```

### 5.4 gateIfIntegrationTool()

통합 도구의 연결 상태를 확인하여, 연결되지 않은 통합의 도구 호출을 차단한다:

- Slack 도구: `integrationStatus.slack` 확인
- GitHub 도구: `integrationStatus.github` 확인
- Notion 도구: `integrationStatus.notion` 확인
- Cross-integration 도구: 하나 이상의 통합이 연결되어 있는지 확인

`AUTO_HIDE_INTEGRATIONS=false`이면 이 게이트가 비활성화된다.

### 5.5 통합 도구 동적 노출

`shouldRegisterIntegrationTool()`이 startup 시점에 호출되어, 통합이 확인되지 않은 상태에서는 통합 도구를 MCP에 등록하지 않는다. 이후 `session_init` 등에서 통합 상태가 확인되면 `updateIntegrationStatus()`가 호출되고, 새로 감지된 통합이 있으면 `sendToolsListChanged()` 알림을 발송한다.

---

## 6. 환경 제어 (Environment Controls)

### 6.1 CONTEXTSTREAM_CONSOLIDATED

```
CONTEXTSTREAM_CONSOLIDATED=true | false (default: true)
```

v0.4.x의 기본 모드. true이면 약 11개 통합 도메인 도구만 MCP에 등록되고, 개별 도구는 내부 디스패치로만 접근 가능하다. false이면 기존처럼 60+ 개별 도구를 등록한다.

### 6.2 CONTEXTSTREAM_PROGRESSIVE_MODE

```
CONTEXTSTREAM_PROGRESSIVE_MODE=true | false (default: false)
```

true이면 core 번들만 초기 등록하고, `tools_enable_bundle` 또는 `help(action="enable_bundle")`로 동적 활성화한다. CONSOLIDATED_MODE와 동시 사용 시 CONSOLIDATED가 우선한다.

### 6.3 CONTEXTSTREAM_ROUTER_MODE

```
CONTEXTSTREAM_ROUTER_MODE=true | false (default: false)
```

true이면 4개의 메타 도구(`contextstream`, `contextstream_help`, `operations`, `execute_operation`)만 MCP에 등록하고, 나머지는 `operationsRegistry`를 통해 동적 디스패치한다. Strategy 6.

### 6.4 CONTEXTSTREAM_SCHEMA_MODE

```
CONTEXTSTREAM_SCHEMA_MODE=compact | full (default: full)
```

compact이면 도구 설명과 파라미터 설명을 축소하여 토큰 오버헤드를 줄인다. Strategy 4.

### 6.5 CONTEXTSTREAM_AUTO_HIDE_INTEGRATIONS

```
CONTEXTSTREAM_AUTO_HIDE_INTEGRATIONS=true | false (default: true)
```

true이면 연결되지 않은 통합(Slack/GitHub/Notion)의 도구를 MCP 도구 목록에서 숨긴다. 연결 확인 후 동적으로 노출된다. 5분 TTL 캐시 사용.

### 6.6 CONTEXTSTREAM_TOOL_SURFACE_PROFILE

```
CONTEXTSTREAM_TOOL_SURFACE_PROFILE=default | openai_agentic
```

`openai_agentic`이면 OPENAI_AGENTIC_CORE_TOOLSET (9개 통합 도구)을 사용하고, 나머지는 `tool_search`/`execute_operation`을 통한 동적 접근으로 전환한다.

### 6.7 기타 환경 변수

| 환경 변수 | 기본값 | 설명 |
|-----------|--------|------|
| `CONTEXTSTREAM_TOOLSET` | (없음 = standard) | light / standard / complete / auto |
| `CONTEXTSTREAM_TOOL_ALLOWLIST` | (없음) | 쉼표 구분 도구 이름 허용 목록 |
| `CONTEXTSTREAM_AUTO_TOOLSET` | false | Claude Code 자동 감지 시 light 도구셋 적용 |
| `CONTEXTSTREAM_OUTPUT_FORMAT` | compact | compact (minified) / pretty (indented) |
| `CONTEXTSTREAM_SHOW_TIMING` | false | 도구 응답에 실행 시간 표시 |
| `CONTEXTSTREAM_INCLUDE_STRUCTURED_CONTENT` | true | structuredContent 필드 포함 여부 |
| `CONTEXTSTREAM_SEARCH_LIMIT` | 3 | 검색 기본 결과 수 |
| `CONTEXTSTREAM_SEARCH_MAX_CHARS` | 400 | 검색 결과당 최대 문자 수 |
| `CONTEXTSTREAM_LOG_LEVEL` | normal | quiet / normal / verbose |
| `CONTEXTSTREAM_RESTORE_CONTEXT` | true | session_init 시 자동 컨텍스트 복원 |
| `CONTEXTSTREAM_SEARCH_REMINDER` | true | 검색 규칙 리마인더 주입 여부 |
| `CONTEXTSTREAM_PRO_TOOLS` | (내장 목록) | Pro 게이트 대상 도구 커스텀 지정 |
| `CONTEXTSTREAM_UPGRADE_URL` | https://contextstream.io/pricing | 업그레이드 안내 URL |

---

## 7. 아키텍처 요약

### 7.1 전략 목록

소스 코드에서 명시적으로 번호가 부여된 전략들:

| # | 전략 | 환경 변수 | 기본값 |
|---|------|-----------|--------|
| 2 | Integration Auto-Hide | `AUTO_HIDE_INTEGRATIONS` | true |
| 3 | Client Detection | `AUTO_TOOLSET` | false |
| 4 | Schema Minimization | `SCHEMA_MODE` | full |
| 5 | Progressive Disclosure | `PROGRESSIVE_MODE` | false |
| 6 | Router Tool Pattern | `ROUTER_MODE` | false |
| 7 | Output Verbosity Reduction | `OUTPUT_FORMAT` | compact |
| 8 | Consolidated Domain Tools | `CONSOLIDATED` | true |

### 7.2 도구 수 비교

| 모드 | 등록 도구 수 | 토큰 비용 (상대적) |
|------|-------------|-------------------|
| 개별 도구 (v0.3) | ~60+ | 100% |
| STANDARD_TOOLSET | ~76 | ~95% |
| LIGHT_TOOLSET | ~43 | ~60% |
| CONSOLIDATED (기본) | ~20 | ~25% |
| OPENAI_AGENTIC | ~9 | ~15% |
| ROUTER_MODE | 4 | ~5% |

### 7.3 데이터 흐름

```
MCP Client
  -> MCP Server (registerTool filters)
    -> registerTool() wrapper
      -> CONSOLIDATED_MODE filter
      -> toolAllowlist filter
      -> integration auto-hide filter
      -> ROUTER_MODE filter
      -> PROGRESSIVE_MODE filter
      -> actuallyRegisterTool()
        -> COMPACT_SCHEMA compactification
        -> safeHandler wrapper
          -> gateIfProTool()
          -> gateIfIntegrationTool()
          -> handler(input)
            -> switch (input.action)
              -> client.apiMethod()
              -> formatContent(result)
```
