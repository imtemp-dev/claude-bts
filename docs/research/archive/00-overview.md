# Context Sync

## 한 줄 요약

코드가 말해주지 않는 것 — **왜 이렇게 했는가, 무엇을 시도했다가 포기했는가, 어떤 제약이 있었는가** — 을 AI 세션에서 자동으로 추출하여 코드 옆에 보존한다.

## 문제

```
코드에 남는 것:     what        (구현)
커밋에 남는 것:     what changed (변경)
PR에 남는 것:       sometimes why (설명, 대부분 빈약)
어디에도 안 남는 것: why not, what failed, what constrained
```

AI 세션에서 개발자는 탐색하고, 실험하고, 결정하고, 막다른 길을 만나고, 방향을 바꾼다. PR이 머지되면 코드만 남고 이 모든 맥락이 사라진다.

**6개월 후** 다른 개발자가 그 코드를 수정할 때:
- `git blame` → "Add 1.2s delay after typing" (what, not why)
- 원 작성자 → 퇴사했거나 기억 못함
- 결과: "수동 넘기기"로 바꿈 → 사용자 불만 (원래 플로우 방해로 거부된 대안이었음)

## 해결

세션이 끝나거나 컨텍스트가 압축될 때, AI에게 세 가지를 묻는다:

1. **무엇을 결정했는가?** (decision) — 선택, 이유, 거부된 대안
2. **어떤 제약을 발견했는가?** (constraint) — 출처, 영향
3. **무엇을 시도했는가?** (exploration) — 접근, 결과, 포기 이유

추출된 내용은 프로젝트 로컬에 저장하고, 다음 세션 시작 시 관련 파일에 대해 자동 주입한다.

## 동작 방식

```
세션 중: 평소대로 개발
     ↓
세션 끝 (또는 컴팩션 전):
     AI가 세션을 회고하여 decisions / constraints / explorations 추출
     ↓
~/.context-sync/<project>/observations.jsonl 에 추가
     ↓
다음 세션 시작:
     현재 작업 파일과 관련된 observations 자동 주입
     "이 파일에 대해 알아야 할 것: ..."
```

**3개 hook, 1개 MCP 도구, 1개 JSONL 파일. 그게 전부다.**

## 왜 이렇게 단순한가

경쟁자 3개의 소스 코드를 23,622줄에 걸쳐 분석했다:

| 프로젝트 | 규모 | 핵심 외 비중 |
|----------|------|-------------|
| ContextStream | 42,486줄 | 7개 토큰 전략, 8개 에디터, 통합(Slack/GitHub/Notion) |
| claude-mem | 35,650줄 | 3개 AI 프로바이더, 37개 모드, UI 뷰어, 프로세스 관리 |
| memctl | ~22,000줄 | 80+ API 웹앱, Stripe 빌링, 블로그, 프로모 코드 |

이들이 복잡한 이유: **모든 것을 캡처하려 하기 때문이다.** 파일 읽기, 빌드 명령, 검색 결과, 모든 도구 실행을 기록한다. 결과적으로 노이즈 속에서 신호를 찾기 위해 벡터 검색, 하이브리드 랭킹, 3-layer UI가 필요해진다.

우리는 **신호만 캡처한다.** AI가 세션을 회고하여 decisions/constraints/explorations만 추출하므로, 저장소에는 노이즈가 없고 단순 grep으로 충분하다.

## 실제 추출 예시 (검증 완료)

실제 세션 트랜스크립트에서 프로토타입으로 추출한 결과:

```
[DECISION] 타이핑 후 1.2초 딜레이 추가
  Why: 읽을 시간 확보를 위한 UX 결정
  Rejected: "딜레이 없음" — 읽을 시간 부족
  Rejected: "수동 넘기기" — 플로우 방해
  Files: src/components/SceneCard.tsx

[CONSTRAINT] Temporal Workflow 내 Date.now()/fetch()/db.query 금지
  Source: Temporal determinism requirement
  Impact: replay 충돌 → workflow 실패

[DECISION] PR 리뷰 6 critical + 11 major 이슈 수정
  Why: 런타임 안정성 확보
  Rejected: "미해결 방치" — 런타임 에러 유발
  Files: worker/index.ts, temporal/client.ts 등 13개 파일
```

10개 테스트 턴, 100% 파싱 성공, decision 5건 / exploration 2건 / low-signal 정확 감지 2건.

## Painkiller 조건

| 팀 상황 | 가치 |
|---------|------|
| 솔로 개발자 | 낮음 (자기가 기억) |
| 2-3명 안정 팀 | 낮음 (물어보면 됨) |
| 5-10명, 코드 영역 분리 | 중간 (핸드오프 마찰 시작) |
| **10명+, 이직 있음** | **높음 (지식 손실 비가역적)** |
| **오픈소스/분산 팀** | **높음 (원저자에게 물어볼 수 없음이 기본)** |

## Phase 계획

### Phase 1: 개인 사용 (현재)
- 3 hooks (SessionStart, PreCompact, Stop)
- JSONL 저장 (프로젝트별)
- 세션 시작 시 자동 주입
- MCP search 도구 1개
- Claude Code 전용

### Phase 2: 팀 공유
- PR 연결 (commit SHA + files + branch → PR 매핑)
- 팀 서버 동기화 (PR push 시점)
- 팀 검색
- Stale detection (git diff 기반)

### Phase 3: 확장
- 다중 에디터 (Cursor, Cline)
- AI-to-AI review
- Cross-repo 검색
