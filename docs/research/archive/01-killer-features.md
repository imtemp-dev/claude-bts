# Context Sync — 핵심 차별화

## 경쟁자와의 근본적 차이

경쟁자들은 **모든 것을 기억하려 한다.** 매 도구 실행마다 캡처하고, 벡터 DB에 넣고, 하이브리드 검색으로 찾는다.

우리는 **세 가지만 기억한다:**

### 1. Decisions (결정)

```
무엇을: OAuth2를 JWT 대신 선택
왜: SOC2 compliance requirement
거부된 대안: JWT (만료 처리 복잡), 커스텀 토큰 (유지보수 부담)
```

코드에는 OAuth2 구현만 남는다. "왜 JWT를 안 썼는가"는 사라진다.

### 2. Constraints (제약)

```
제약: Temporal Workflow 내에서 Date.now(), fetch(), db.query() 호출 금지
출처: Temporal determinism — replay 시 동일 결과 보장 필요
영향: 모든 외부 호출은 Activity로 분리해야 함
```

이것을 모르는 새 개발자가 Workflow 내에서 `Date.now()`를 호출하면, 테스트는 통과하지만 프로덕션에서 replay 충돌이 발생한다.

### 3. Explorations (탐색)

```
시도: framer-motion으로 페이지 전환 애니메이션
결과: 구현했으나 모바일에서 jank 발생
상태: abandoned
이유: requestAnimationFrame 타이밍 이슈, CSS transition으로 대체
```

"왜 framer-motion을 안 쓰지?"라는 질문에 대한 답이 코드 어디에도 없다.

---

## 왜 이 세 가지인가

```
개발 과정에서 생성되는 정보:

  탐색 → 실험 → 실패 → 방향 전환 → 결정 → 구현
  ─────────────────────────────────────────────
  ↑ 여기가 전부 사라짐              ↑ 여기만 코드에 남음
```

코드 리뷰, 버그 수정, 온보딩에서 필요한 정보는 **사라진 부분**이다:
- "왜 이 방식인가?" → decision의 rationale
- "다른 방법은 없었나?" → decision의 rejected alternatives
- "이거 바꿔도 되나?" → constraint의 source와 impact
- "이거 시도해 봤나?" → exploration의 approach와 outcome

---

## 접근: 가장 단순한 방법

### 매 도구마다 캡처하지 않는다 (경쟁자 방식)

claude-mem은 PostToolUse 훅으로 매 도구 실행마다 캡처한다 (120초 타임아웃). 세션 하나에 수백 개의 도구 실행이 발생하고, 대부분은 파일 읽기/검색이다. 결과적으로:
- 노이즈 비율 88% (progress 메시지 포함)
- 노이즈 필터링을 위해 content-hash dedup, low-signal filter 필요
- 벡터 검색(Chroma, 384-dim embedding)으로 관련 항목을 찾아야 함
- ProcessRegistry, claim-confirm 큐, 좀비 방지 등 인프라 필요
- 결과: 35,650줄

### 세션이 끝날 때 한 번만 추출한다 (우리 방식)

AI는 세션 동안 일어난 모든 것을 이미 알고 있다. 세션이 끝날 때 "이 세션에서 무엇을 결정했고, 무엇을 포기했고, 어떤 제약을 발견했나?"라고 물으면 된다.

```
Hook 3개:
  SessionStart  → 이전 observations 주입
  PreCompact    → 컨텍스트 압축 전 추출 (긴 세션 보호)
  Stop          → 세션 종료 시 추출

저장:
  ~/.context-sync/<project>/observations.jsonl
  한 줄 = 하나의 observation (JSON)

검색:
  MCP 도구 1개 — JSONL grep
```

노이즈가 없으므로 벡터 검색이 불필요하다. 저장되는 것은 전부 decisions/constraints/explorations뿐이다.

---

## 경쟁 비교: 복잡성 vs 가치

| | claude-mem | ContextStream | memctl | **Context Sync** |
|---|-----------|---------------|--------|-----------------|
| 캡처 시점 | 매 도구 (PostToolUse) | 매 메시지 (context) | 매 도구 (hook) | **세션 끝 (1회)** |
| 저장소 | SQLite + Chroma | 클라우드 API | 클라우드 Turso | **JSONL 파일** |
| 검색 | 3-strategy hybrid | 8-mode semantic | RRF hybrid | **grep** |
| 프로세스 | Worker + Agent + MCP | 클라우드 서버 | Next.js 앱 | **Hook만** |
| 소스 규모 | 35,650줄 | 42,486줄 | ~22,000줄 | **~800줄 (목표)** |
| 핵심 가치 | 전체 세션 기억 | per-message 컨텍스트 | 팀 메모리 | **why/why-not/constraints** |

---

## Phase 1에서 의도적으로 하지 않는 것

| 안 하는 것 | 이유 | Phase 2에서 |
|-----------|------|------------|
| 벡터 검색 | observations가 전부 고품질이므로 grep으로 충분 | 규모가 커지면 추가 |
| 팀 서버 | 개인 사용 먼저 검증 | PR push 시점에 동기화 |
| SQLite | JSONL이 더 단순하고 git-friendly | 1,000+ observations 시 전환 검토 |
| Worker 프로세스 | Hook에서 직접 처리 가능 | 실시간 캡처 필요 시 추가 |
| 다중 에디터 | Claude Code에 집중 | 검증 후 확장 |
| Stale detection | 개인 사용에서는 자기가 알고 있음 | 팀 공유 시 필수 |
| 다중 AI 프로바이더 | 하나면 충분 | 비용 최적화 시 추가 |

---

## 채택하는 검증된 패턴 (최소한만)

| 패턴 | 출처 | 채택 이유 |
|------|------|----------|
| Hook lifecycle (SessionStart/Stop) | claude-mem | 자동화의 기본. 개발자 마찰 제로 |
| XML extraction + 파서 | claude-mem sdk/parser.ts | 구조화 추출 검증됨 (우리 프로토타입) |
| Privacy stripping | claude-mem tag-stripping.ts | `<private>` 태그 제거. 보안 기본 |
| Content-hash dedup | claude-mem observations/store.ts | SHA256 + 30초 윈도우. 중복 방지 |
| Pre-compact 스냅샷 | ContextStream pre-compact.ts | 긴 세션에서 컨텍스트 손실 방지 |

**채택하지 않는 것**: Worker 프로세스, SQLite, Chroma, Express 서버, 프로세스 레지스트리, 좀비 방지, 토큰 압력 관리, 통합 도메인 도구, RRF 검색, intent 분류 — 전부 Phase 1에서 불필요.
