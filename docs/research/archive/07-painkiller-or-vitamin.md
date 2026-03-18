# Context Sync — Painkiller인가, Vitamin인가?

> 실제 추출 결과를 가지고 구체적 시나리오별로 검토한다.
> 각 시나리오에서 "이것이 없으면 어떤 일이 벌어지는가?"를 직시한다.

---

## 실제 추출된 데이터 (Round 2, 8개 observation)

우리 프로토타입이 실제 세션에서 추출한 것들:

```
[DECISION] P0 구현 완료 — tRPC 4개 라우터, Temporal Workflow, DB 스키마, 시드 스크립트
  Rejected: "구현 연기" — 이미 요구사항 충족

[DECISION] PR 리뷰 6개 critical + 11개 major 이슈 수정
  Rejected: "미해결 방치" — 런타임 에러 유발

[DECISION] 타이핑 완료 후 1.2초 딜레이 추가
  Rejected: "딜레이 없음" — 읽을 시간 부족
  Rejected: "수동 넘기기" — 플로우 방해

[DECISION] 씬 네비게이션 UX 개선 (선택지 마지막 페이지, 뒤로가기, 텍스트 고정)
  Rejected: "현재 UX 유지" — 사용자 요구 미충족

[DECISION] 경쟁 포지셔닝 전략 수정 — PR 연결 + 팀 공유 교차점
  Rejected: "Stale detection 집중" — 쉽게 복제 가능
  Rejected: "AI-to-AI review 집중" — 기술 장벽 없음

[EXPLORATION] PR 리뷰 코멘트 검토 → 수정 파일 식별
[EXPLORATION] memctl 코드 다관점 분석
[DISCOVERY] 백엔드 팀 책임 범위 + Temporal Workflow 제약 식별
```

---

## 시나리오별 Painkiller 검증

### 시나리오 1: 개발자 B가 A의 작업을 이어받는다

**상황**: A가 mydream-backend P0을 구현하고 퇴근. B가 다음 날 이어서 P1을 구현.

**Context Sync가 있으면**:
B가 세션을 시작하면 자동으로 주입:
```
## PR #3 Context (team/backend branch)
### Decisions
- tRPC 4 routers + Temporal Workflows 아키텍처 채택
- USE_MOCK_GENERATION=true로 GPU 없이 전체 플로우 구현
- Temporal Workflow 내 db.query/fetch/Date.now 금지 (replay 충돌 방지)

### Resolved Issues
- 6 critical + 11 major issues from PR review fixed
- workflowsPath 설정 오류 → workflows/index.ts 분리로 해결
- Temporal client singleton 미복구 → 재시작 시 recovery 로직 추가
```

**Context Sync가 없으면**:
- B가 코드를 읽고 구조를 파악 (30-60분)
- Temporal Workflow 내 db.query 호출 → replay 충돌 발생 → 디버깅 (1-2시간)
- PR #3 리뷰에서 이미 지적된 singleton 문제를 모르고 다시 만남

**평가**: **Painkiller — 단, 조건부.**
- Temporal Workflow 제약처럼 **코드만 봐서는 알 수 없는** 정보가 있을 때 → 확실한 painkiller
- 단순 구조 파악이라면 코드 읽기로 해결 가능 → vitamin
- **핵심**: "코드가 말해주지 않는 것"이 있는가? → 제약, 폐기된 접근, 대안 비교가 있다면 painkiller

### 시나리오 2: PR 리뷰

**상황**: 리뷰어가 `SceneCard.tsx`의 1.2초 딜레이를 보고 "왜 수동 넘기기 안 했어?"라고 물음.

**Context Sync가 있으면**:
리뷰어가 PR 컨텍스트를 보면:
```
[DECISION] 타이핑 후 1.2초 딜레이
  Rejected: "수동 넘기기" — 플로우 방해로 거부됨
```
→ 질문할 필요 없음. 이미 고려되었음을 알 수 있음.

**Context Sync가 없으면**:
- 리뷰어: "수동 넘기기가 더 낫지 않나?" (코멘트)
- 작성자: "그건 플로우를 방해해서 딜레이로 갔어" (답변)
- 리뷰어: "아 그렇구나" (acknowledge)
- 왕복 시간: ~30분-수시간 (비동기 대화 기준)

**평가**: **Vitamin.**
- 왕복이 불편하지만 **업무가 차단되지는 않는다**
- 리뷰어는 승인을 기다리지 않고 다른 일을 한다
- 시간 절약은 있지만, "이것 때문에 제품을 산다"는 수준은 아님
- **예외**: 하루 10개+ PR을 리뷰하는 대규모 팀에서는 누적 효과가 커질 수 있음

### 시나리오 3: 6개월 후 버그 수정

**상황**: 6개월 후 `SceneCard.tsx`의 1.2초 딜레이가 느리다는 버그 리포트. 새 개발자 C가 수정.

**Context Sync가 있으면**:
```
[DECISION] 타이핑 후 1.2초 딜레이 (2026-03)
  Why: 읽을 시간 확보
  Rejected: "딜레이 없음" — 읽을 시간 부족
  Rejected: "수동 넘기기" — 플로우 방해

⚠ Stale: SceneCard.tsx changed 15 times since this decision
```
→ C는 딜레이의 원래 의도와 고려된 대안을 알고 수정 방향을 결정

**Context Sync가 없으면**:
- `git blame` → 커밋 메시지: "Add 1.2s delay after typing" (what, not why)
- 원 작성자에게 질문? → 퇴사했거나 기억 못함
- C의 선택: 딜레이 줄이기? 수동 넘기기? → **원래 왜 수동을 안 했는지 모름**
- 최악: 수동 넘기기로 바꿈 → 사용자 불만 ("플로우 끊긴다")

**평가**: **Painkiller — 가장 강한 시나리오.**
- 원 작성자가 없을 때 컨텍스트 손실은 **되돌릴 수 없다**
- `git blame`은 "what"만 보여주고 "why not"은 보여주지 못함
- 잘못된 수정으로 인한 regression > 컨텍스트 조회 비용
- **단, 빈도 문제**: 이 시나리오가 주당 몇 번 발생하는가?

### 시나리오 4: 같은 실패 반복

**상황**: A가 Temporal Workflow 내에서 `Date.now()` 호출 → replay 충돌 → 수정. 2달 후 D가 같은 실수.

**Context Sync가 있으면**:
```
[CONSTRAINT] Temporal Workflow 내 db.query/fetch/Date.now 금지
  Source: Temporal determinism requirement
  Impact: replay 충돌 → workflow 실패
```
→ D가 Workflow 파일 작업 시작 시 자동 주입 → 실수 방지

**Context Sync가 없으면**:
- D가 `Date.now()` 호출 → 테스트 통과 (단일 실행) → 프로덕션에서 replay 충돌
- 디버깅 2-4시간
- 원인 발견 후 "아 이거 A도 겪었었네"

**평가**: **Painkiller — 단, 빈도가 핵심.**
- 같은 실수의 반복 비용은 높다 (디버깅 + 프로덕션 리스크)
- 하지만 이 시나리오의 **발생 빈도가 낮을 수 있다**
- 팀이 작으면 구두로 전달 가능. 팀이 크면 빈도 상승.

### 시나리오 5: 포지셔닝/전략 결정 회고

**상황**: 3개월 후 "왜 stale detection 대신 PR 연결에 집중했지?" 논의 필요.

**Context Sync가 있으면**:
```
[DECISION] PR 연결 + 팀 공유 교차점 포지셔닝
  Rejected: "Stale detection 집중" — 2주면 복제 가능
  Rejected: "AI-to-AI review 집중" — 기술 장벽 없음
```
→ 당시 근거가 명확하게 기록되어 있음

**Context Sync가 없으면**:
- "왜 이걸로 갔더라?" → 문서를 뒤짐 → 기억에 의존
- 같은 논의 반복 가능

**평가**: **Vitamin.**
- 전략 결정은 월 1-2회. 빈도가 낮다
- 문서(우리의 03-objective-assessment.md)가 이미 이 역할을 함
- 자동 추출의 부가 가치 낮음

---

## 정직한 결론

### 분류 매트릭스

| 시나리오 | 판정 | 빈도 | 대안 존재? |
|----------|------|------|-----------|
| 작업 이어받기 (코드가 말 안 하는 제약) | **Painkiller** | 팀 규모 비례 | 구두 전달 (확장 불가) |
| PR 리뷰 왕복 절감 | Vitamin | 높음 | PR description (대부분 빈약하지만) |
| 6개월 후 버그 수정 (원저자 부재) | **Painkiller** | 낮-중 | git blame (why 없음) |
| 같은 실패 반복 방지 | **Painkiller** | 낮 | 팀 위키/규칙 (유지 안 됨) |
| 전략 결정 회고 | Vitamin | 매우 낮 | 문서 |

### Painkiller인 경우의 공통점

1. **코드가 말해주지 않는 정보**가 필요할 때 (why, why not, constraints)
2. **원 작성자에게 물어볼 수 없을 때** (퇴사, 팀 이동, 기억 손실)
3. **잘못된 결정의 비용이 높을 때** (프로덕션 장애, 리팩터링 낭비)

### Vitamin인 경우의 공통점

1. **대면/비동기 대화로 해결 가능** (PR 코멘트, Slack)
2. **빈도가 너무 낮아** 도구 도입 비용을 정당화 못함
3. **기존 도구(문서, git)로 충분**한 경우

---

## 그래서 우리는 무엇인가?

### 솔직한 답: **상황에 따라 다르다**

| 팀 상황 | 판정 | 이유 |
|---------|------|------|
| 솔로 개발자 | **Vitamin** | 자기 컨텍스트를 자기가 기억함 |
| 2-3명 안정 팀 | **Vitamin** | 물어보면 됨 |
| 5-10명 팀, 코드 영역 분리 | **Vitamin→Painkiller 전이 구간** | 핸드오프 빈도 증가, 구두 전달 한계 |
| 10명+ 팀, 이직 있음 | **Painkiller** | 지식 손실 비가역적, 반복 비용 누적 |
| 오픈소스/분산 팀 | **Painkiller** | 원저자에게 물어볼 수 없음이 기본 상태 |

### Painkiller로 만들려면 집중해야 할 것

**"코드가 말해주지 않는 것"에 집중:**

1. **Constraints** (제약) — Temporal에서 Date.now() 금지 같은 것. 코드에 주석이 있을 수도 있지만 대부분 없다.
2. **Rejected alternatives** (거부된 대안) — "수동 넘기기를 왜 안 했는가". 코드에는 선택된 것만 남는다.
3. **Cross-cutting decisions** (교차 결정) — "이 아키텍처를 왜 이렇게 했는가". 여러 파일에 걸쳐 있어 git blame으로 추적 불가.

**"남아있지 않는 정보"가 핵심:**

```
코드에 남는 것:    what (구현)
커밋에 남는 것:    what changed (변경)
PR에 남는 것:      what + sometimes why (설명)
어디에도 안 남는 것: why not, what was tried and failed, what constraints existed
                   ↑ 여기가 우리의 영역
```

### 가격 시사점

- Vitamin 영역(솔로/소규모): **무료**여야 함. 유료화하면 안 씀
- Painkiller 영역(10명+/이직): **기꺼이 지불**. 단, 팀 규모에 비례하는 가격
- **Free tier에서 개인 사용 풀 기능** → 습관 형성 → 팀 성장 시 자연스럽게 유료 전환

이것은 Slack과 같은 모델이다: 소규모에서 무료로 쓰다가, 팀이 커지면 검색 제한 때문에 유료로 간다.

### 즉시 검증해야 할 것

**"이 경험이 주당 몇 번 발생하는가?"**

우리의 실제 세션(mydream-backend)에서:
- 44턴 중 의미 있는 observations: 8개
- 그중 "나중에 누군가에게 가치가 있을" 것: **3-4개** (Temporal 제약, PR 리뷰 이슈, UX 딜레이 결정)
- 하루 세션 1-2개 × 유용한 observations 3-4개 = **하루 3-8개**

**질문**: 이 3-8개의 observation이, 2-4주 후에, 다른 팀원에게, 실제로 시간을 절약해 주는가?

이것은 **사용자 인터뷰로만 검증 가능**하다. 코드로는 답할 수 없다.
