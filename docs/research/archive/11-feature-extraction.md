# 프로젝트 기능 추출

> 가칭: **veri** (verify의 약자)
> 포지션: Claude Code 출력물 검증 프레임워크
> 아키텍처: Go 싱글 바이너리 (moai-adk 패턴)

---

## 이름 후보

| 이름 | 장점 | 단점 |
|------|------|------|
| **veri** | 짧음, `veri init`/`/verify` 자연스러움 | verify 축약이 직관적이지 않을 수 있음 |
| **prism** | "다각도 검토" 의미 전달 | prism.js 등 기존 프로젝트 혼동 |
| **vigil** | "감시/경계" 느낌 | 발음이 익숙하지 않을 수 있음 |
| **klarity** | clarity + k, 명확성 강조 | 스펠링 인위적 |
| **sieve** | 체로 걸러내기, 검증 느낌 | 부정적 뉘앙스 가능 |

일단 **veri**로 진행. 나중에 변경 가능.

---

## 기존 문서별 기능 추출

### 01-killer-features.md에서

| 기능 | 원래 맥락 | veri 적용 | 우선순위 |
|------|----------|----------|---------|
| 구조화 추출 (decision/constraint/exploration) | "코드가 말해주지 않는 것" | `/verify` 스킬이 문서에서 이 구조를 검증. "이 결정에 rationale이 있는가? 대안이 검토되었는가?" | **Phase 1** |
| Stale detection (`git diff`) | 문서-코드 신선도 확인 | `/cross-check`에서 "이 문서가 참조하는 코드가 변경되었는가?" 결정론적 확인 | **Phase 2** |
| Content-hash dedup | 중복 방지 | 검증 결과 중복 실행 방지. 같은 문서를 2번 검증하면 이전 결과 재사용 | **Phase 2** |

### 03-objective-assessment.md에서

| 기능 | 원래 맥락 | veri 적용 | 우선순위 |
|------|----------|----------|---------|
| "코드가 말해주지 않는 3가지" 추출 | decisions, constraints, explorations | `/verify` 검증 항목으로 활용: "이 설계 문서에 결정의 근거가 있는가? 제약이 명시되었는가?" | **Phase 1** |
| 방어 가능한 차별화: PR 단위 | PR 경계에서 컨텍스트 연결 | 레시피 `/recipe feature`의 마지막 단계에서 PR description 검증 포함 가능 | **Phase 3** |

### 04-synthesis-feasibility.md에서 (경쟁자 패턴)

**claude-mem 패턴:**

| 패턴 | veri 적용 | 우선순위 |
|------|----------|---------|
| Privacy stripping (`<private>` 태그) | Hook에서 민감 정보 제거 후 검증. 검증 로그에 비밀 정보 남지 않게 | **Phase 1** |
| Content-hash dedup (SHA256 + 30s) | 검증 결과 캐싱. 동일 입력에 대한 중복 검증 방지 | **Phase 2** |
| Claim-confirm 큐 | 레시피 Phase 전환 시 atomic 상태 저장. 크래시 안전성 | **Phase 2** |
| Edge privacy stripping | 검증 중 발견된 코드/문서에서 시크릿/키 감지 및 경고 | **Phase 2** |

**memctl 패턴:**

| 패턴 | veri 적용 | 우선순위 |
|------|----------|---------|
| Intent classification (5 타입) | 검증 요청의 의도 분류: 사실 확인 / 논리 검증 / 커버리지 검토 / 비교 분석 / 탐색적 검토 | **Phase 2** |
| Relevance scoring | 검증 결과의 심각도 스코어링: critical / major / minor / info | **Phase 1** |
| Low-signal filter | 검증할 가치가 없는 사소한 항목 자동 건너뜀 | **Phase 1** |
| Health-based eviction | 오래된 검증 결과 자동 정리 | **Phase 3** |

**ContextStream 패턴:**

| 패턴 | veri 적용 | 우선순위 |
|------|----------|---------|
| Lesson 시스템 (proactive injection) | 이전 검증에서 발견된 반복 패턴을 자동 표면화. "이 프로젝트에서 자주 발생하는 오류: 라인 수 off-by-one" | **Phase 2** |
| Pre/Post compaction | 컴팩션 전에 레시피 상태 저장 + 컴팩션 후 복원 | **Phase 1** |
| Token pressure management | 긴 검증 세션에서 토큰 관리. 검증 결과 요약본으로 교체 | **Phase 3** |
| Consolidated domain tools | 검증 도구를 하나의 통합 인터페이스로 (`/verify` 하나에 action 파라미터) | **Phase 2** |

### 07-painkiller-or-vitamin.md에서

| 인사이트 | veri 적용 | 우선순위 |
|---------|----------|---------|
| "코드에 남는 것/안 남는 것" 분류 | `/verify` 체크리스트에 포함: "이 문서가 코드만으로는 알 수 없는 정보를 담고 있는가?" | **Phase 1** |
| 6개월 후 버그 수정 시나리오 | `/recipe debug`에서 관련 과거 검증 결과 자동 표시 | **Phase 2** |
| 같은 실패 반복 방지 | 검증 이력에서 패턴 감지. "이전에 같은 파일에서 같은 종류의 오류 발견됨" | **Phase 3** |

### 08-use-case-reality-check.md에서

| 인사이트 | veri 적용 | 우선순위 |
|---------|----------|---------|
| 세션 연속성 (매일 가치) | `/recipe resume` — 중단된 레시피를 이전 상태에서 이어서. **매일 쓰는 기능** | **Phase 1** |
| 컴팩션 후 맥락 복구 | PreCompact hook에서 레시피/토론 상태 자동 저장 | **Phase 1** |
| in_progress/completed/next_steps | 레시피 상태에 자연스럽게 포함. `recipe status`로 확인 | **Phase 1** |

### 10-development-direction.md에서 (이미 정의된 것)

이미 정의된 핵심 기능은 그대로 유지. 위의 추출 결과를 Phase에 배치.

### moai-adk 분석에서

| 패턴 | veri 적용 | 우선순위 |
|------|----------|---------|
| Ralph Engine 수렴 루프 | `/verify` 루프: 검증 → 오류 수정 → 재검증 → 수렴까지 반복 (최대 N회) | **Phase 1** |
| 상태 영속화 (atomic write) | 모든 상태 변경 시 temp file + rename | **Phase 1** |
| Phase pipeline with quality gates | 레시피의 각 Phase 사이에 검증 게이트 | **Phase 1** |
| Worktree isolation | 검증을 격리된 환경에서 실행 (읽기 전용 서브에이전트) | **Phase 2** |
| Progressive disclosure | 스킬 3단계 로딩: Phase 1에서는 핵심만, 필요시 확장 | **Phase 1** |
| 매니페스트 추적 | 배포된 파일 버전 관리. `veri update` 시 변경분만 교체 | **Phase 2** |
| 프로파일 시스템 | 사용자별 검증 선호도 (엄격도, 기본 언어, 모델 정책) | **Phase 3** |
| TUI 마법사 (Bubbletea) | `veri init` 대화형 설정 | **Phase 1** |

### 프로토타입(extraction)에서 검증된 것

| 검증 결과 | veri 적용 | 우선순위 |
|---------|----------|---------|
| XML 추출 + 파싱 100% 성공 | 검증 결과를 구조화된 XML로 출력. 파싱 로직 재사용 | **Phase 1** |
| Low-signal 필터링 동작 확인 | 사소한 검증 항목 자동 건너뜀 | **Phase 1** |
| 프롬프트 튜닝으로 분류 정확도 개선 | 검증 스킬의 프롬프트도 동일한 튜닝 방법론 적용 | **Phase 1** |

---

## Phase별 기능 정리

### Phase 1: 핵심 (직접 사용 가능한 최소 셋)

```
CLI:
  veri init              프로젝트 초기화 (TUI 마법사)
  veri hook <event>      Hook 핸들러
  veri verify <file>     결정론적 팩트 체크
  veri recipe status     레시피 상태
  veri recipe resume     레시피 재개
  veri debate list       토론 목록
  veri doctor            시스템 진단

Skills:
  /verify                논리적 오류 검증 (서브에이전트)
  /cross-check           코드-문서 교차검증 (결정론적 + 서브에이전트)
  /debate                전문가 3명 토론 (상태 저장, 이어서 가능)
  /audit                 누락 시나리오 검토
  /research              체계적 조사

Recipes:
  /recipe debug          버그 수정 레시피
  /recipe feature        새 기능 레시피
  /recipe analyze        코드/시스템 분석 레시피

Agents:
  verifier               논리 검증 전문 (읽기 전용)
  auditor                커버리지 검토 전문 (읽기 전용)
  cross-checker          사실 대조 전문 (읽기 전용 + wc/grep)
  debater                토론 페르소나 (읽기 전용)

Hooks:
  pre-compact            레시피/토론 상태 저장
  session-start          이전 레시피 상태 복원 안내
  stop                   레시피 진행 중이면 다음 단계 안내

검증 게이트:
  심각도 분류             critical / major / minor / info
  수렴 루프               최대 N회 반복 (설정 가능)
  Low-signal 필터         사소한 항목 건너뜀

상태 관리:
  .veri/state/            레시피, 토론, 검증 이력
  atomic write            temp + rename
  resume 지원             세션 끊겨도 재개

Privacy:
  태그 스트리핑            <private>, 시크릿 패턴 감지
```

### Phase 2: 확장

```
CLI:
  veri update            자동 업데이트
  veri verify --stale    stale detection (git diff)

Skills:
  /e2e                   E2E 테스트 시나리오 생성/실행
  /recipe refactor       리팩터링 레시피

기능:
  Lesson 시스템           반복 오류 패턴 자동 표면화
  Intent 분류             검증 요청 의도 자동 판별
  Content-hash 캐싱       동일 입력 중복 검증 방지
  매니페스트 추적          배포 파일 버전 관리
  Stale detection         git diff 기반 문서 신선도 확인
  Worktree isolation      검증을 격리 환경에서 실행
  Consolidated tools      /verify 하나에 action 파라미터
```

### Phase 3: 고급

```
프로파일 시스템           사용자별 검증 선호도
토큰 압력 관리           긴 세션에서 검증 결과 요약 교체
Health-based eviction    오래된 검증 결과 자동 정리
반복 실패 감지           이전 검증 이력에서 패턴 추출
moai 연동               moai Run phase 후 자동 verify
팀 공유                  검증 결과 팀 동기화
```

---

## Phase 1 산출물 요약

```
배포:
  Go 싱글 바이너리 (~10-15MB)
  curl -fsSL .../install.sh | bash → 끝

내장 템플릿:
  Skills: 5개 (verify, cross-check, debate, audit, research)
  Recipes: 3개 (debug, feature, analyze)
  Agents: 4개 (verifier, auditor, cross-checker, debater)
  Hooks: 3개 (pre-compact, session-start, stop)
  Rules: 검증 기준, 레시피 프로토콜
  Commands: /verify, /debate, /audit, /recipe 등

예상 규모:
  Go 코드: ~3,000-5,000줄 (CLI + Hook + 결정론적 검증 + 상태 관리 + TUI)
  마크다운 템플릿: ~2,000-3,000줄 (스킬 + 에이전트 + 규칙 + 커맨드)
```
