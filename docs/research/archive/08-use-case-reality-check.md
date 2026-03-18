# Context Sync — Use Case 현실 점검

> 기능의 난이도와 별개로, **사용자가 얼마나 자주, 얼마나 절실하게** 이것을 필요로 하는가?
> 이 답에 따라 MVP 방향이 달라진다.

---

## 1. 빈도 vs 강도 매트릭스

우리가 지금까지 정의한 핵심 가치를 빈도와 강도로 분류한다:

| Use Case | 빈도 | 강도 | 현재 해결책 |
|----------|------|------|------------|
| "지난 세션에서 뭐 했더라?" | **매일** | 중 | 없음. 처음부터 다시 설명 |
| "이 프로젝트 맥락 다시 설명해야 해" | **매일** | 중 | 없음. 수동 재설명 5-10분 |
| "컴팩션 후 맥락 날아갔다" | **주 2-3회** | 상 | 없음. /compact 후 혼란 |
| "PR description 작성" | **주 2-5회** | 하 | 수동 (대부분 빈약) |
| "이 코드 왜 이렇게 했지?" | **주 1-2회** | 중 | git blame + Slack 질문 |
| "이거 다른 방법 없었나?" | **월 2-3회** | 상 | 없음 |
| "이 파일 바꿔도 되나?" | **월 1-2회** | **최상** | 없음. 잘못 바꾸면 프로덕션 장애 |
| "같은 실패 반복" | **월 1회** | **최상** | 없음. 시간 낭비 |

**문제 발견**: 우리가 집중한 "why not"(월 1-2회)은 강도는 최상이지만 **빈도가 너무 낮다.**

반면 "지난 세션에서 뭐 했더라?"(매일)는 빈도가 최상이지만 **우리가 이것을 명시적으로 다루지 않았다.**

---

## 2. 개발자의 실제 하루

AI 코딩 도구를 쓰는 개발자의 하루를 따라가 본다:

### 09:00 — 세션 시작 (매일)

```
어제 auth middleware 리팩터링하다 말았는데...
Claude Code 새 세션 열면 → 빈 상태.

"어제 OAuth2 Passport.js로 리팩터하고 있었어.
 worker/workflows/에서 Temporal 제약 때문에 Activity로 분리했고,
 episode.workflow.ts까지 수정했어. 타입 체크는 통과했는데
 integration test는 아직 안 돌렸어."

→ 이 5분의 재설명이 매일 반복된다.
```

**Context Sync가 있다면**: SessionStart에서 자동 주입.
```
## 이전 세션 요약 (어제 18:30)
- OAuth2 + Passport.js 리팩터링 진행 중 (adopted)
- Temporal Workflow 내 외부 호출 → Activity 분리 (constraint)
- episode.workflow.ts 까지 수정 완료
- 남은 작업: integration test
```

**평가**: 이건 **매일 5분 절약**이다. 연간 ~20시간. vitamin처럼 보이지만, **매일 반복**되므로 습관이 된다. 이것이 acquisition hook이 될 수 있다.

### 10:30 — 컴팩션 (주 2-3회)

```
긴 세션. 파일 20개 수정. 갑자기:
"Context window approaching limit. Compacting..."

→ 직전까지 무엇을 하고 있었는지, 어떤 파일을 수정했는지,
  어떤 접근을 시도하다 중단했는지 — 전부 날아감.

"뭐 하고 있었지? 어디까지 했지?"
→ 10-20분 혼란 후 겨우 복구
```

**Context Sync가 있다면**: PreCompact hook에서 추출.
```
## 컴팩션 전 상태 저장됨
- SceneCard 타이밍 이슈 수정 중 (exploration, 진행 중)
- 1.2s 딜레이 결정 (decision, adopted)
- cancelRef safety check 추가 완료
- 남은: EpisodeViewer 네비게이션 버튼
```

**평가**: 이건 **절실하다.** 컴팩션 후 맥락 손실은 Claude Code 사용자의 가장 흔한 불만 중 하나다. ContextStream이 이것을 위해 pre/post compact 훅에 719줄을 투자한 이유.

### 14:00 — 다른 사람 코드 수정 (주 1-2회)

```
SceneCard.tsx를 수정해야 함. setTimeout(1200)이 있음.
"이거 왜 1.2초지? 줄여도 되나?"
→ git blame → "Add delay" 커밋 메시지
→ Slack에서 원 작성자에게 물어봄 (또는 못 물어봄)
```

**Context Sync가 있다면**: 이 파일 관련 observations 자동 주입.
```
⚠ 이 파일에 대한 기존 결정:
[DECISION] 1.2s 딜레이 — 수동 넘기기는 플로우 방해로 거부됨
```

**평가**: 유용하지만 **주 1-2회**. 그리고 Slack으로 물어볼 수 있다.

### 16:00 — PR 작성 (주 2-5회)

```
기능 완료. PR 생성.
"description에 뭘 쓰지..."
→ "Refactor auth middleware" (한 줄로 끝냄)
→ 또는 PR 템플릿 채우는 데 10-15분
```

**Context Sync가 있다면**: 세션 중 축적된 observations에서 자동 생성.
```
## Summary
Auth middleware를 OAuth2로 리팩터링.

## Decisions
- OAuth2 over JWT: SOC2 compliance requirement
- Passport.js over custom: 유지보수성, 커뮤니티 지원

## Constraints Found
- Temporal Workflow 내 Date.now()/fetch() 금지

## Tried & Rejected
- framer-motion transitions: 모바일 jank → CSS transition으로 대체
```

**평가**: 이건 **보이는 결과물**이다. PR description이 갑자기 풍부해지면 리뷰어가 즉시 가치를 느낀다. **바이럴 효과** 가능 — "이 PR description 어떻게 만든 거야?"

---

## 3. Use Case 파괴력 순위

솔직하게 순위를 매긴다:

### Tier 1: 매일 쓰고, 없으면 불편한 것

**A. 세션 간 맥락 유지 ("어제 뭐 했더라?")**
- 빈도: **매일**
- 대상: AI 코딩 도구 사용하는 **모든** 개발자
- 현재 해결책: **없음** (매번 수동 재설명)
- 채택 장벽: **없음** (설치하면 자동)
- 파괴력: **높음** — 매일 5분 절약이 습관을 만든다

**B. 컴팩션 후 맥락 복구**
- 빈도: **주 2-3회** (긴 세션 사용자)
- 대상: Claude Code **파워 유저**
- 현재 해결책: **없음** (10-20분 혼란)
- 채택 장벽: **없음**
- 파괴력: **높음** — 기존 pain이 극심

### Tier 2: 자주 쓰고, 가시적인 결과물

**C. PR description 자동 생성**
- 빈도: **주 2-5회**
- 대상: PR 기반 워크플로우 팀
- 현재 해결책: 수동 (대부분 빈약)
- 채택 장벽: **낮음** (결과물이 바로 보임)
- 파괴력: **중-상** — 바이럴 효과. 리뷰어가 가치를 느끼면 팀 전체 도입

### Tier 3: 가끔 쓰지만 강력한 것

**D. 파일 관련 제약/결정 자동 표면화**
- 빈도: **주 1-2회**
- 대상: 팀에서 다른 사람 코드를 수정하는 개발자
- 현재 해결책: git blame + Slack (부분적)
- 채택 장벽: 축적된 데이터 필요 (cold start)
- 파괴력: **중** — 유용하지만 없어도 일은 됨

**E. "이거 시도해 봤나?" 검색 (why not)**
- 빈도: **월 1-2회**
- 대상: 팀 규모 5명 이상
- 현재 해결책: **없음**
- 채택 장벽: **높음** — 팀 전체 도입 + 데이터 축적 필요
- 파괴력: **낮-중** — 빈도가 너무 낮아서 도구 도입을 정당화하기 어려움

---

## 4. MVP 방향에 대한 시사점

### 현재 방향의 문제

```
현재 MVP: "decisions / constraints / explorations를 추출하여 보존"
          → Tier 3-4 use case에 집중
          → 매일 쓸 이유가 약함
          → "있으면 좋다" (vitamin)
```

### 제안하는 방향 전환

```
수정 MVP: "세션이 기억하는 것을 다음 세션에 전달"
          → Tier 1 use case가 핵심
          → 매일 자동으로 동작
          → "없으면 불편하다" (painkiller로 전이 가능)
```

**핵심 전환: "why not 보존" → "세션 연속성"으로 프레이밍 변경.**

하지만 구현은 거의 동일하다:
- 세션 끝에 AI가 추출하는 것: decisions, constraints, explorations + **현재 작업 상태**
- 다음 세션 시작 시 주입하는 것: 이전 세션의 추출 내용

차이는 **추출 항목에 "현재 진행 상황"을 추가하는 것**뿐이다:

```xml
<session_summary>
  <in_progress>현재 진행 중인 작업 (가장 중요!)</in_progress>
  <completed>완료된 작업</completed>
  <next_steps>다음에 해야 할 것</next_steps>
  <decisions>결정된 사항</decisions>
  <constraints>발견된 제약</constraints>
  <explorations>시도한 것들</explorations>
</session_summary>
```

**in_progress / completed / next_steps** = 매일 가치 (Tier 1)
**decisions / constraints / explorations** = 장기 가치 (Tier 3, 축적)

둘 다 같은 메커니즘으로 추출된다. 비용 차이 없음.

---

## 5. 파괴적 시나리오: "AI가 기억하는 팀"

가장 파괴적인 장기 비전은 이것일 수 있다:

```
팀원 5명이 각자 Claude Code로 개발.
각 세션에서 decisions/constraints/explorations가 자동 축적.
PR merge 시 팀 공유 저장소에 동기화.

3개월 후:
- 코드베이스의 모든 "왜"가 기록되어 있음
- 새 팀원이 코드를 열면 관련 결정/제약이 자동 표시
- "이거 시도해 봤나?" → 즉시 답변 가능
- PR description이 자동으로 풍부
- 아키텍처 결정 기록(ADR)이 자동 생성

= 팀의 두뇌가 코드 옆에 살아있는 상태
```

하지만 이것은 **3개월 뒤의 비전**이다. Day 1 가치가 아니다.

**Day 1 가치**: "어제 세션에서 뭐 하고 있었는지 기억해 준다."
**Week 1 가치**: "컴팩션 후에도 맥락을 복구해 준다."
**Month 1 가치**: "이 파일에 대한 과거 결정을 자동으로 보여준다."
**Month 3+ 가치**: "팀 전체의 경험이 축적된다."

---

## 6. 수정된 MVP 정의

### Hook: "Your AI remembers"

핵심 메시지:
```
"Claude Code는 세션이 끝나면 모든 것을 잊는다.
 Context Sync는 중요한 것을 기억하고 다음 세션에 전달한다."
```

이것은 **모든 Claude Code 사용자**가 공감할 수 있는 메시지다.

### 구현 변경 (최소)

현재 추출 프롬프트에 3개 필드 추가:

| 추가 필드 | 설명 | 매일 가치 |
|-----------|------|----------|
| `in_progress` | 현재 진행 중인 작업 | "어제 뭐 하고 있었더라?" 해결 |
| `completed` | 이번 세션에서 완료한 것 | "어디까지 했더라?" 해결 |
| `next_steps` | 다음에 해야 할 것 | "다음에 뭐 해야 하지?" 해결 |

기존 3개 필드는 유지:

| 기존 필드 | 설명 | 장기 가치 |
|-----------|------|----------|
| `decisions` | 결정 + 이유 + 거부된 대안 | "왜 이렇게 했는가?" |
| `constraints` | 제약 + 출처 + 영향 | "이거 바꿔도 되나?" |
| `explorations` | 시도 + 결과 + 포기 이유 | "이거 해봤나?" |

**아키텍처 변경: 없음.** 같은 3개 hook, 같은 JSONL, 같은 주입 메커니즘. 프롬프트만 6개 필드로 확장.

### 주입 변경

SessionStart 주입 내용의 우선순위:

```
## Context Sync — 이전 세션에서 이어서

### 진행 중이던 작업
- OAuth2 middleware 리팩터링 (src/auth/)
- episode.workflow.ts Temporal Activity 분리 중

### 완료된 작업
- tRPC 라우터 4개 설정 완료
- DB 스키마 + 시드 스크립트 완료

### 다음 할 일
- integration test 작성
- PR 리뷰 코멘트 반영

### 알아야 할 결정
- OAuth2 over JWT (SOC2 compliance)

### 알아야 할 제약
- Temporal Workflow 내 Date.now()/fetch() 금지
```

**"진행 중이던 작업"이 맨 위.** 이것이 매일 가치의 핵심.

---

## 7. 결론: Painkiller로 만드는 법

### 기존 접근
```
"코드가 말해주지 않는 것을 보존한다" (why not)
→ 월 1-2회 발생하는 고강도 pain
→ Vitamin (대부분의 시간에는 필요 없음)
```

### 수정 접근
```
"AI가 세션을 넘어 기억한다" (session continuity + why not)
→ 매일 발생하는 중강도 pain (세션 재시작)
→ 주 2-3회 발생하는 고강도 pain (컴팩션)
→ + 장기 축적 가치 (decisions/constraints/explorations)
→ Painkiller (매일 쓰고, 없으면 불편)
```

### 코드 변경
```
추출 프롬프트에 in_progress/completed/next_steps 추가: ~20줄
주입 포맷에 진행 상황 우선 배치: ~15줄
총 변경: ~35줄

아키텍처 변경: 없음
```

**35줄 변경으로 vitamin에서 painkiller로 전환할 수 있다.**
