# Context Sync — 경쟁자 장점 합성 가능성 검토

> 불확실한 가정("why not" 자동 추출, AI-to-AI review 가치)은 Phase 2로 미룬다.
> 검증된 패턴만으로 Phase 1을 구성할 수 있는지 검토한다.

---

## 1. Phase 2로 미루는 항목

| 항목 | 미루는 이유 |
|------|------------|
| "Why not" 자동 추출 | LLM extraction 품질 미검증. 세션 noise 속에서 "폐기된 접근" 식별 가능 여부 불확실 |
| AI-to-AI review handoff | refinement 품질에 100% 의존. 기반 없이 프레이밍만으로는 무가치 |
| CX/PM/Onboarding 뷰 | Phase 1에서 개발자 워크플로우 먼저 검증해야 함 |
| Cross-team/cross-repo 그래프 | 단일 팀 내 동작 검증 후 확장 |
| 다중 에디터 지원 (Cursor, Cline 등) | Claude Code 단일 타겟으로 집중. 에디터당 어댑터 비용이 큼 (ContextStream은 8개 에디터에 2,169줄의 hooks-config.ts) |
| 모드 시스템 (law-study 등) | claude-mem의 37개 모드 파일은 매력적이지만 핵심이 아님 |

**Phase 1 목표**: PR 연결 + 자동 캡처 + 구조화 추출 + 저장 + 검색 + 주입. Claude Code 단일 에디터.

---

## 2. 각 프로젝트의 검증된 장점

코드 분석에서 확인된, 실제 동작하는 패턴만 정리한다.

### claude-mem에서 가져올 것

| 패턴 | 구현 | 규모 | 채택 이유 |
|------|------|------|----------|
| **6-hook 자동 캡처** | hooks.json → bun-runner.js → worker-service.cjs | ~84줄 설정 + 112줄 hook-command | 개발자 마찰 제로. "설치하면 끝" |
| **Agent 기반 구조화** | SDKAgent (489줄) — XML prompt → parse → observation | 파서 212줄, 프롬프트 237줄 | 키워드 매칭(memctl)보다 정확. type/title/facts/narrative/concepts |
| **Claim-confirm 큐** | PendingMessageStore (489줄) — enqueue→claim→confirm | STALE 60s, retry 3회 | 크래시 안전성. 메시지 유실 방지 |
| **Content-hash 중복 방지** | SHA256(sessionId+title+narrative).slice(0,16) | 30초 윈도우 | 동일 관찰 재저장 차단 |
| **SQLite WAL + 마이그레이션** | Database.ts (359줄), MigrationRunner (866줄) | 15개 마이그레이션, 5개 테이블 | 로컬 우선 + 스키마 진화 |
| **3-Layer 검색 UI** | index→timeline→full fetch | SearchManager 1,884줄 | 토큰 효율 (~10x 절감) |
| **Edge privacy stripping** | hook layer에서 `<private>` 태그 제거 | 수십 줄 | 스토리지 전에 민감 데이터 제거 |

### memctl에서 가져올 것

| 패턴 | 구현 | 규모 | 채택 이유 |
|------|------|------|----------|
| **Org/Project/Role 모델** | users→organizations→projects, organization_members (role enum) | 28 테이블 (핵심 ~8개) | 팀 기능의 기초. 접근 제어 |
| **RRF 하이브리드 검색** | `score += 1/(k + rank + 1)`, k=60 | FTS5 + vector merge | FTS와 vector의 장점 결합 |
| **Intent 분류** | entity/temporal/relationship/aspect/exploratory | 5 타입 + 가중치 | 쿼리 의도에 맞는 검색 전략 자동 선택 |
| **Relevance 스코링** | priority * log(access) * exp(-0.03*days) * feedback * pin | 공식 1줄, 적용 범위 넓음 | 시간 감쇠 + 피드백 학습 |
| **Low-signal 필터** | 일반 역량 노이즈, 비맥락 셸 명령, 설명 10단어 미만 | 분류 로직 ~100줄 | 저품질 캡처 사전 차단 |
| **Session claims** | TTL 기반 비관적 잠금, `agent/claims/{sessionId}` | 메모리 엔트리 기반 | 동시 편집 충돌 감지 |
| **Health-based 퇴출** | health score = priority * access * decay * feedback | 용량 초과 시 자동 아카이브 | 스토리지 관리 자동화 |

### ContextStream에서 가져올 것

| 패턴 | 구현 | 규모 | 채택 이유 |
|------|------|------|----------|
| **First-tool interceptor** | withAutoContext() — 첫 도구 호출 시 자동 초기화 | SessionManager 865줄 | MCP 도구만으로 모든 클라이언트에서 동작 |
| **토큰 압력 관리** | 4단계 (low/medium/high/critical), 자동 체크포인트 | 임계값 70K, 턴당 3K 추정 | 긴 세션에서 컨텍스트 보존 |
| **Lesson 시스템** | capture_lesson → dedup(2분) → proactive injection | RISKY_ACTION_KEYWORDS + severity | 과거 실수 자동 표면화 |
| **Pre/Post compaction** | 압축 전 스냅샷 → 압축 후 50%+ 토큰 하락 감지 → 복원 | pre-compact 451줄, post-compact 267줄 | 컨텍스트 압축 시 맥락 보존 |
| **Lagging transcript capture** | UserPromptSubmit에서 이전 교환(user+assistant) 캡처 | /transcripts/exchange POST | 완전한 대화 쌍 보장 |
| **Consolidated domain tools** | 19개 도메인 도구 + action 파라미터 | ~75% 토큰 절감 | 도구 스키마 오버헤드 최소화 |

---

## 3. 충돌 지점과 해결

### 3.1 스토리지 아키텍처: 로컬 vs 클라우드

| 프로젝트 | 방식 | 장점 | 단점 |
|----------|------|------|------|
| claude-mem | 로컬 SQLite WAL + Chroma | 오프라인, 프라이버시, 빠름 | 팀 공유 불가 |
| memctl | 클라우드 Turso (libSQL) | 팀 공유, 중앙 관리 | 클라우드 의존, 비용 |
| ContextStream | 클라우드 전용 API | 기기 간 동기화 | 오프라인 불가, vendor lock |

**결정: 로컬 우선 + 클라우드 동기화 (hybrid)**

```
[Local Layer]                    [Cloud Layer]
SQLite WAL (즉시 저장)           Team DB (PostgreSQL or Turso)
  ├── observations               ├── shared observations
  ├── sessions                   ├── team search index
  ├── summaries                  ├── PR-linked context
  └── pending_queue              └── org/project/role
         │                              ▲
         └──── sync on PR push ─────────┘
```

이유:
- 캡처/추출은 로컬에서 즉시 발생 (claude-mem 패턴). 네트워크 지연 없음
- 팀 공유는 PR push/merge 시점에 동기화 (자연스러운 경계)
- 오프라인 동작 보장. 네트워크 없어도 개인 사용 가능
- PR 연결은 클라우드에서만 의미 (PR = 팀 공유 단위)

**충돌 해결**: claude-mem의 로컬 스토리지를 기반으로 하되, memctl의 팀 모델을 클라우드 레이어에 배치. ContextStream의 순수 클라우드 모델은 채택하지 않는다.

### 3.2 캡처 트리거: 매 도구 vs 세션 경계

| 프로젝트 | 트리거 | 결과 |
|----------|--------|------|
| claude-mem | PostToolUse (매 도구 실행) | 세밀하지만 대량. 120초 타임아웃 필요 |
| ContextStream | per-message context() | 매 메시지마다 API 호출 |
| memctl | Hook + 명시적 tool call | 반자동 |

**결정: claude-mem 방식 + 지능적 필터링**

```
PostToolUse (매 도구 실행)
  → low-signal filter (memctl 패턴)
    → 통과: PendingMessageStore에 큐잉
    → 차단: 조용히 버림
  → Agent 추출 (claude-mem 패턴)
  → 로컬 SQLite 저장
```

이유:
- 매 도구 캡처(claude-mem)가 가장 완전한 데이터를 수집
- 하지만 claude-mem의 약점은 모든 도구 실행을 캡처하여 노이즈가 많다는 것
- memctl의 low-signal filter를 앞단에 추가하여 품질 향상
- ContextStream의 per-message 방식은 클라우드 의존이므로 채택하지 않음

### 3.3 Refinement: 키워드 vs Agent vs 클라우드

| 프로젝트 | 방식 | 품질 | 비용 |
|----------|------|------|------|
| memctl | 키워드 분류 | 낮음 | 무료 |
| claude-mem | 로컬 Agent (Claude/Gemini/OpenRouter) | 높음 | API 비용 발생 |
| ContextStream | 클라우드 서버 측 | 불명 | 서버 비용 |

**결정: claude-mem의 multi-agent + 우리의 taxonomy**

```
원시 도구 출력
  → Agent 프롬프트 (우리의 taxonomy)
    → decision: { choice, rationale, alternatives[] }
    → constraint: { description, source, impact }
    → exploration: { approach, outcome, status: "adopted"|"modified"|"abandoned" }
    → discovery: { finding, implications }
  → XML 파싱 → 로컬 SQLite
```

이유:
- claude-mem의 Agent 파이프라인이 검증된 가장 높은 품질
- 하지만 claude-mem의 6 타입(decision/bugfix/feature/refactor/discovery/change)은 "왜"를 충분히 포착하지 못함
- Deciduous의 taxonomy (goal→decision→action→observation→revisit)에서 영감을 받되, 자동 추출
- 불확실한 "abandoned approach 자동 추출"은 Phase 1에서는 `status` 필드로 단순화 — agent가 판별하되 실패하면 "adopted"를 기본값으로

**Phase 1 타협**: "exploration with abandonment reason"을 완벽하게 추출하는 대신, observation에 `status` 필드를 추가하고 agent가 best-effort로 분류하게 한다. 품질 데이터를 수집하여 Phase 2에서 개선.

### 3.4 검색: 단일 전략 vs 다중 전략

| 프로젝트 | 검색 | 특징 |
|----------|------|------|
| memctl | RRF (FTS5 + vector) | Intent 분류, 5 타입 |
| claude-mem | 3-strategy (SQLite/Chroma/Hybrid) | SearchOrchestrator, fallback |
| ContextStream | 8-mode (semantic/hybrid/keyword/pattern/exhaustive/refactor/crawl/team) | recommendSearchMode(), 20+ 조건 |

**결정: memctl의 RRF + claude-mem의 3-layer UI + 단순화**

```
검색 쿼리
  → Intent 분류 (memctl: entity/temporal/relationship/aspect/exploratory)
  → RRF 하이브리드 (FTS5 + vector, k=60)
  → 3-layer 결과 반환 (claude-mem):
      Layer 1: 인덱스 (ID, 시간, 타입, 제목) → ~50 토큰/결과
      Layer 2: 타임라인 (앵커 주변 컨텍스트) → ~200 토큰/결과
      Layer 3: 전체 (선택된 항목만) → ~500 토큰/결과
```

이유:
- ContextStream의 8 모드는 과도하다. 사용자가 모드를 선택하는 것 자체가 마찰
- memctl의 intent 분류가 자동으로 최적 전략을 선택하므로 사용자는 쿼리만 입력
- claude-mem의 3-layer가 토큰 효율에서 검증됨 (10x 절감)
- RRF의 k=60은 memctl에서 검증된 값

### 3.5 주입: 언제, 무엇을

| 프로젝트 | 시점 | 내용 |
|----------|------|------|
| claude-mem | SessionStart | 최근 observations + summaries + timeline |
| ContextStream | 매 메시지 (context tool) | 관련 컨텍스트 + lessons + pressure |
| memctl | MCP auto (context_for) | 요청된 컨텍스트 타입 |

**결정: SessionStart 주입 + PR 연관 컨텍스트 + stale check**

```
SessionStart
  → 현재 작업 디렉토리의 git 상태 확인
  → 최근 PR에서 관련 컨텍스트 조회 (클라우드)
  → git diff로 stale 체크
  → 로컬 최근 observations (claude-mem 패턴)
  → 합산하여 주입
```

이유:
- ContextStream의 per-message 주입은 클라우드 의존 + 매 메시지마다 API 호출
- claude-mem의 SessionStart 주입이 더 실용적 (1회 호출)
- 하지만 claude-mem에 없는 것: **PR 기반 관련 컨텍스트**와 **stale check**
- 이 두 가지가 우리의 차별화

### 3.6 팀 모델

| 프로젝트 | 팀 기능 |
|----------|---------|
| memctl | org/project/role, RBAC, session claims, API tokens |
| ContextStream | workspace/project, Team plan, cross-workspace search |
| claude-mem | 없음 |

**결정: memctl의 핵심 모델을 간소화하여 채택**

```
Phase 1 팀 모델 (최소):
  Team → Project (1:N)
  TeamMember (role: owner/admin/member)
  SharedContext (PR 연결, 검색 가능)
```

이유:
- memctl의 28 테이블은 과도 (blog_posts, promo_codes 등 포함)
- 핵심만 추출: team, member, project, 권한
- ContextStream의 workspace 모델보다 memctl의 org 모델이 더 명확한 계층
- session claims(동시 편집 충돌 감지)는 Phase 1에서는 불필요 — PR 단위이므로 자연스러운 격리

---

## 4. 합성 아키텍처

```
┌─────────────────────────────────────────────────────────┐
│                    Claude Code Plugin                     │
│  hooks.json (6 hooks: Setup, SessionStart, UserPrompt,   │
│              PostToolUse, Stop, SessionEnd)               │
└──────────────┬──────────────────────────────┬────────────┘
               │                              │
        ┌──────▼──────┐              ┌────────▼────────┐
        │ Hook Engine  │              │  MCP Server     │
        │ (claude-mem  │              │  (도구 노출)      │
        │  패턴)        │              │                 │
        └──────┬──────┘              └────────┬────────┘
               │                              │
        ┌──────▼──────────────────────────────▼────────┐
        │              Worker Service                    │
        │  ┌─────────────────────────────────────────┐  │
        │  │  Capture Pipeline                        │  │
        │  │  raw input → low-signal filter (memctl)  │  │
        │  │  → Agent extraction (claude-mem)          │  │
        │  │  → content-hash dedup (claude-mem)        │  │
        │  │  → privacy strip (claude-mem)             │  │
        │  └─────────────────┬───────────────────────┘  │
        │                    │                           │
        │  ┌─────────────────▼───────────────────────┐  │
        │  │  Local Storage (SQLite WAL)              │  │
        │  │  observations, sessions, summaries       │  │
        │  │  pending_queue (claim-confirm)            │  │
        │  │  FTS5 index                              │  │
        │  └─────────────────┬───────────────────────┘  │
        │                    │                           │
        │  ┌─────────────────▼───────────────────────┐  │
        │  │  Search Engine                           │  │
        │  │  Intent classification (memctl)          │  │
        │  │  RRF hybrid (memctl FTS5 + vector)       │  │
        │  │  3-layer UI (claude-mem)                 │  │
        │  └─────────────────────────────────────────┘  │
        │                                                │
        │  ┌─────────────────────────────────────────┐  │
        │  │  Injection Engine                        │  │
        │  │  SessionStart context (claude-mem)       │  │
        │  │  PR-linked context (new)                 │  │
        │  │  Stale check: git diff (new)             │  │
        │  │  Token pressure (ContextStream)          │  │
        │  │  Lesson injection (ContextStream)        │  │
        │  └─────────────────────────────────────────┘  │
        └────────────────────┬───────────────────────────┘
                             │
                    ┌────────▼────────┐
                    │  Cloud Sync     │
                    │  (PR push/merge │
                    │   시점에 동기화)  │
                    └────────┬────────┘
                             │
                    ┌────────▼────────┐
                    │  Team Server    │
                    │  team/project   │
                    │  /role (memctl) │
                    │  PR-linked      │
                    │  context store  │
                    │  team search    │
                    └─────────────────┘
```

---

## 5. 출처 매핑 — 어떤 코드에서 무엇을 배우는가

| 합성 컴포넌트 | 주 출처 | 참고 소스 (읽을 파일) | 예상 규모 |
|---------------|---------|----------------------|----------|
| Hook 설정 | claude-mem | `plugin/hooks/hooks.json`, `src/cli/hook-command.ts` | ~200줄 |
| Capture pipeline | claude-mem + memctl | `src/services/worker/agents/ResponseProcessor.ts`, `src/sdk/parser.ts`, `src/sdk/prompts.ts` | ~500줄 |
| Low-signal filter | memctl | `packages/shared/src/hooks.ts` (extractHookCandidates) | ~150줄 |
| PendingMessageStore | claude-mem | `src/services/sqlite/PendingMessageStore.ts` | ~300줄 |
| SQLite schema + migrations | claude-mem | `src/services/sqlite/Database.ts`, `migrations/runner.ts` | ~500줄 |
| Content-hash dedup | claude-mem | `src/services/sqlite/observations/store.ts` | ~50줄 |
| FTS5 + vector search | memctl + claude-mem | `packages/cli/src/tools/search.ts`, `src/services/worker/search/` | ~800줄 |
| Intent classification | memctl | `packages/shared/src/intent.ts` | ~200줄 |
| 3-layer search UI | claude-mem | `src/services/worker/SearchManager.ts` | ~400줄 |
| RRF merge | memctl | `packages/cli/src/tools/search.ts` | ~100줄 |
| SessionStart injection | claude-mem | `src/services/context/ContextBuilder.ts`, `ObservationCompiler.ts` | ~400줄 |
| Token pressure | ContextStream | `src/session-manager.ts` (getSessionTokens, pressure levels) | ~100줄 |
| Lesson system | ContextStream | `src/tools.ts` (capture_lesson, getHighPriorityLessons) | ~200줄 |
| Stale check | 신규 | git diff + threshold | ~100줄 |
| PR linking | 신규 | GitHub API + PR metadata | ~500줄 |
| Team model | memctl (간소화) | `packages/db/src/schema/` (org, members, projects) | ~300줄 |
| Cloud sync | 신규 | PR push/merge 시 local→cloud | ~500줄 |
| Privacy stripping | claude-mem | `src/utils/tag-stripping.ts` | ~50줄 |
| **총 예상** | | | **~5,000줄** |

---

## 6. 충돌 없이 합쳐지는가?

### 합쳐지는 것 (시너지)

1. **claude-mem 캡처 + memctl 필터**: claude-mem이 모든 도구 실행을 캡처하고, memctl의 low-signal filter가 노이즈를 사전 차단. 파이프라인 앞단에 필터를 배치하면 자연스럽게 결합.

2. **claude-mem 로컬 저장 + memctl 팀 모델**: 로컬에서 즉시 저장(빠름, 오프라인), PR 시점에 팀 서버로 동기화(팀 공유). 두 레이어가 역할 분리.

3. **memctl RRF 검색 + claude-mem 3-layer UI**: RRF가 결과 품질을, 3-layer가 토큰 효율을 담당. 독립적인 관심사.

4. **ContextStream 토큰 압력 + claude-mem SessionStart 주입**: 토큰 압력은 "언제 저장할지"를 결정하고, SessionStart 주입은 "무엇을 주입할지"를 결정. 독립적.

5. **ContextStream lesson + 우리의 PR 연결**: lesson을 PR 단위로 연결하면 "이 코드 영역에서 과거에 어떤 실수가 있었는지" 자동 표면화. 기존 lesson 시스템의 자연스러운 확장.

### 충돌하는 것 (해결 필요)

| 충돌 | 내용 | 해결 |
|------|------|------|
| **Agent 비용** | claude-mem의 Agent 추출은 API 호출당 비용 발생 | Gemini Flash Lite(무료)를 기본값, Claude를 Pro 전용 옵션으로 |
| **벡터 DB** | claude-mem은 Chroma(MCP 경유), memctl은 인라인 384-dim | Phase 1에서는 인라인 vector(memctl 방식). Chroma는 Phase 2 |
| **Hook 타임아웃** | claude-mem은 PostToolUse 120초 | 동일. Claude Code가 지원하는 최대값 |
| **검색 API** | memctl은 서버 측, claude-mem은 로컬 | 로컬 검색 기본 + 팀 검색은 클라우드 API |

---

## 7. Phase 1 MVP 범위

### 포함

| 기능 | 출처 | 설명 |
|------|------|------|
| Claude Code 6-hook 자동 캡처 | claude-mem | 설치만 하면 자동 동작 |
| Low-signal 필터 | memctl | 노이즈 사전 차단 |
| Agent 구조화 추출 | claude-mem + 자체 taxonomy | decision/constraint/exploration/discovery |
| 로컬 SQLite + FTS5 | claude-mem | 즉시 저장, 오프라인 가능 |
| Content-hash 중복 방지 | claude-mem | SHA256 + 30초 윈도우 |
| Claim-confirm 큐 | claude-mem | 크래시 안전성 |
| RRF 하이브리드 검색 | memctl | FTS5 + vector merge |
| 3-layer 검색 결과 | claude-mem | 토큰 효율적 |
| SessionStart 컨텍스트 주입 | claude-mem | 이전 세션 맥락 복원 |
| PR 연결 (GitHub) | **신규** | commit SHA + file paths + PR metadata |
| Stale detection | **신규** | git diff 기반 유효성 검증 |
| Privacy stripping | claude-mem | `<private>` 태그 edge 제거 |
| MCP 도구 노출 | 공통 | search, context, capture 등 |

### 미포함 (Phase 2)

| 기능 | 이유 |
|------|------|
| 팀 서버 + RBAC | 개인 사용 먼저 검증 |
| 클라우드 동기화 | PR 연결 로직 안정화 후 |
| AI-to-AI review | refinement 품질 검증 후 |
| "Why not" 자동 추출 | LLM 능력 검증 후 |
| Cursor/Cline 지원 | Claude Code에서 검증 후 |
| 토큰 압력 관리 | 기본 주입으로 시작 |
| Lesson 시스템 | 관찰 데이터 축적 후 |
| 모드 시스템 | 코드 모드만으로 시작 |

---

## 8. 결론

### 합칠 수 있는가?

**가능하다.** 세 프로젝트의 장점은 대부분 다른 레이어를 담당하므로 직접 충돌이 적다:

```
claude-mem  → 캡처 + 추출 + 로컬 저장 + 검색 UI
memctl      → 필터링 + 검색 품질 + 팀 모델
ContextStream → 주입 전략 + 토큰 관리
신규         → PR 연결 + stale check
```

### 가장 큰 리스크는?

**Agent 추출 품질**이다. claude-mem의 SDKAgent가 6 타입으로 분류하는 것은 검증되었지만, 우리의 확장된 taxonomy (decision with alternatives, exploration with status)가 실제로 동작하는지는 아직 모른다.

**즉시 해야 할 일**: 실제 Claude Code 세션 트랜스크립트 10개를 수집하고, 우리의 extraction prompt로 처리하여 품질을 평가한다. 이것이 전체 제품의 feasibility를 결정한다.
