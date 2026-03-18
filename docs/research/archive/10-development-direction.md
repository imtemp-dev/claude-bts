# 개발 방향 검토

## 핵심 아이디어

moai-adk의 아키텍처(바이너리 + 템플릿 배포 + Hook + 상태 관리)를 차용하되, moai가 **안 다루는 영역** — AI 출력물의 검증 — 에 집중한다.

```
moai-adk:  "AI가 일을 잘 하게" (Plan → Run → Sync + TRUST 5 코드 품질)
우리:      "AI가 한 일이 맞는지 확인" (Verify → Audit → Cross-check + 레시피)
```

보완 관계. 충돌하지 않음. moai 사용자도 우리 도구를 같이 쓸 수 있음.

---

## moai에서 차용하는 것 vs 우리가 만드는 것

### 차용: 아키텍처 패턴

| moai 패턴 | 우리 적용 |
|-----------|----------|
| Go 싱글 바이너리 + embed 템플릿 | 동일. `init` 한 번이면 모든 파일 배포 |
| `moai hook <event>` Hook 핸들러 | 동일. 결정론적 검증을 Hook에서 수행 |
| `.moai/state/` 상태 파일 관리 | 동일. 레시피/토론 상태 영속화 |
| `.claude/skills/` 스킬 배포 | 동일. 검증 스킬 배포 |
| `.claude/agents/` 에이전트 배포 | 동일. 검증 전용 에이전트 배포 |
| `.claude/commands/` 슬래시 커맨드 | 동일. `/verify`, `/debate` 등 |
| Ralph Engine 수렴 루프 | 차용. 검증이 통과할 때까지 반복 |
| 매니페스트로 배포 파일 추적 | 차용. 업데이트 시 변경 파일 감지 |

### 우리가 만드는 것: 검증 기능

**스킬 (Skills)**:

| 스킬 | 목적 | 작동 방식 |
|------|------|----------|
| `/verify` | 논리적 오류 검증 | 문서/코드를 읽고 논리적 일관성 확인. 서브에이전트가 수행 |
| `/audit` | 누락 시나리오 검토 | "빠뜨린 것이 없는가?" 다각도 검토. 체크리스트 기반 |
| `/cross-check` | 코드-문서 교차검증 | 문서의 사실적 주장을 소스 코드와 대조. **결정론적 + LLM 혼합** |
| `/debate` | 전문가 페르소나 토론 | 3명 전문가 라운드 토론. 상태 저장으로 이어서 토론 가능 |
| `/research` | 체계적 조사 | 코드/문서/공식 문서 병렬 조사. 결과 구조화 |
| `/e2e` | E2E 테스트 시나리오 | 기능에 대한 테스트 시나리오 생성 + 실행 |

**레시피 (Recipes)** — 스킬을 조합하는 상위 스킬:

| 레시피 | 타겟 | 스킬 조합 |
|--------|------|----------|
| `/recipe debug` | 버그 수정 | research → verify → (debate if 복잡) → 코드 수정 → e2e |
| `/recipe feature` | 새 기능 | research → design 문서 → verify → audit → 구현 → e2e |
| `/recipe analyze` | 코드 분석 | research → 문서 작성 → cross-check → verify → 최종 문서 |
| `/recipe refactor` | 리팩터링 | research → audit (영향 범위) → design → verify → 구현 → e2e |

**에이전트 (Agents)**:

| 에이전트 | 역할 | 도구 제한 |
|---------|------|----------|
| `verifier` | 논리적 오류 전문 검증 | Read, Grep, Glob (읽기 전용) |
| `auditor` | 누락/커버리지 검토 | Read, Grep, Glob (읽기 전용) |
| `cross-checker` | 사실 확인 전문 | Read, Grep, Bash(wc, grep) |
| `debater-{1,2,3}` | 전문가 페르소나 | Read (읽기 전용) |

**Hook 핸들러 (결정론적 검증)**:

| Hook | 시점 | 하는 일 |
|------|------|--------|
| `post-tool-use` | Edit/Write 후 | 수정된 파일이 레시피 범위 내인지 확인 |
| `stop` | 응답 완료 | 레시피 진행 중이면 다음 단계 안내 |
| `pre-compact` | 컴팩션 전 | 레시피 상태 스냅샷 |

**CLI 명령어 (바이너리)**:

| 명령어 | 목적 |
|--------|------|
| `init` | 프로젝트 초기화. 스킬/에이전트/훅/규칙 배포 |
| `hook <event>` | Hook 핸들러 실행 |
| `verify <file>` | 결정론적 교차검증 (파일 존재, 함수명, 라인 수 등) |
| `recipe status` | 현재 레시피 진행 상태 |
| `recipe resume` | 중단된 레시피 재개 |
| `debate list` | 저장된 토론 목록 |
| `doctor` | 시스템 진단 |
| `update` | 자동 업데이트 |

---

## 레시피 실행 플로우 예시: `/recipe feature "OAuth2 인증 추가"`

```
Phase 1: Research
  │ Claude가 /research 스킬 호출
  │ → 서브에이전트(Explore)가 코드베이스 분석
  │ → 공식 문서 조사 (WebSearch/WebFetch)
  │ → 결과 → .veri/state/{id}/01-research.md 저장
  │
Phase 2: Design
  │ Claude가 설계 문서 작성
  │ → .veri/state/{id}/02-design.md 저장
  │
Phase 3: Verify (수렴 루프)
  │ ┌─ 서브에이전트(verifier) → 논리적 오류 검증
  │ │  결과: errors[] 반환
  │ │
  │ ├─ 바이너리 `verify` → 결정론적 확인
  │ │  - 문서에서 언급한 파일이 존재하는가?
  │ │  - 함수명/타입명이 실제 코드와 일치하는가?
  │ │  결과: facts_checked, mismatches[] 반환
  │ │
  │ ├─ 서브에이전트(auditor) → 누락 검토
  │ │  결과: missing_scenarios[] 반환
  │ │
  │ └─ 오류/불일치 있으면?
  │    → Claude가 문서 수정
  │    → 다시 Phase 3 (최대 3회)
  │    통과하면 → Phase 4
  │
Phase 4: Debate (필요시)
  │ 복잡한 결정이 있으면:
  │ → /debate "OAuth2 vs JWT vs 커스텀 토큰"
  │ → 3명 전문가 토론 (라운드 1, 2, 3)
  │ → 결론 → .veri/state/{id}/03-debate.md 저장
  │ → 결론에 대해 다시 /verify
  │
Phase 5: Final Document
  │ 모든 결과물 통합
  │ → .veri/state/{id}/04-final.md
  │ → 이 문서가 구현의 기반이 됨
  │
Phase 6: Implement
  │ Plan mode → 구현 계획
  │ 코드 작성
  │ /e2e → 테스트 시나리오 생성 + 실행
  │ 테스트 통과까지 반복
```

---

## 상태 관리

```
.veri/
├── config/
│   ├── settings.yaml          # 사용자 설정 (기본 모델, 언어 등)
│   └── quality.yaml           # 검증 기준 (최대 반복 횟수, 엄격도 등)
├── state/
│   ├── recipes/
│   │   └── {recipe-id}/
│   │       ├── recipe.json    # 레시피 메타 (타입, 현재 Phase, 시작 시각)
│   │       ├── 01-research.md
│   │       ├── 02-design.md
│   │       ├── 03-debate.md
│   │       ├── 04-final.md
│   │       └── verify-log.jsonl  # 검증 이력 (각 라운드의 errors/mismatches)
│   └── debates/
│       └── {debate-id}/
│           ├── debate.json    # 토론 메타 (주제, 라운드 수, 결론)
│           ├── round-1.md     # 각 라운드 기록
│           ├── round-2.md
│           └── round-3.md
└── templates/                  # 내장 (embed)
    ├── skills/
    ├── agents/
    ├── rules/
    ├── hooks/
    └── commands/
```

세션이 끊겨도 `recipe resume`으로 재개. 이전 토론도 `debate list`로 찾아서 이어서 진행 가능.

---

## Go vs TypeScript 결정

| 기준 | Go | TypeScript |
|------|-----|-----------|
| 배포 | **7MB 싱글 바이너리, 의존성 0** | npm install 필요, Node.js 18+ 필요 |
| 설치 | `curl \| bash` 끝 | `npm i -g` 또는 `npx` |
| Hook 속도 | ~5ms 시작 | ~100-500ms 시작 |
| 템플릿 embed | `//go:embed` 네이티브 | pkg/bun compile 가능하나 복잡 |
| Claude Code 사용자 | Go 모를 수 있음 | **이미 Node.js 있음** |
| MCP SDK | Go SDK 없음 | **공식 지원** |
| Agent SDK | Go SDK 없음 | **공식 지원** |
| 기존 코드 | 없음 | **프로토타입 412줄 있음** |
| moai와 일관성 | **동일 패턴** | 다른 빌드 체인 |

**권장: Go.**

이유:
1. `curl | bash` 설치가 `npm install`보다 마찰이 근본적으로 낮다
2. Claude Code 사용자에게 Node.js가 있더라도, npm 패키지의 의존성 트리/버전 충돌은 현실적 문제
3. Hook 실행 빈도가 높으면 (post-tool-use) 5ms vs 500ms 차이가 누적
4. MCP/Agent SDK가 필요한 부분은 우리 도구에 없음 — 우리는 Hook + CLI + 상태 관리만
5. moai 사용자가 이미 Go 바이너리 패턴에 익숙

단, TypeScript 프로토타입(extraction 코드 412줄)은 검증용으로 유지. 프롬프트 튜닝과 파싱 로직을 TypeScript에서 검증 후 Go로 포팅.

---

## MVP 범위

### Phase 1: 핵심 스킬 + CLI (2-3주)

| 산출물 | 내용 |
|--------|------|
| `init` CLI | .claude/ 에 스킬/에이전트/훅/규칙 배포 |
| `/verify` 스킬 | 논리적 오류 검증 (서브에이전트) |
| `/cross-check` 스킬 | 교차검증 (서브에이전트 + CLI 결정론적 확인) |
| `/debate` 스킬 | 3명 전문가 토론 + 상태 저장 |
| `verify` CLI | 결정론적 팩트 체크 (파일 존재, 함수명, 라인 수) |
| 상태 관리 | .veri/state/ 파일 기반 |

### Phase 2: 레시피 + 수렴 루프 (2-3주)

| 산출물 | 내용 |
|--------|------|
| `/recipe` 스킬 | 레시피 오케스트레이션 |
| 수렴 루프 | verify 통과까지 반복 (최대 N회) |
| `/audit` 스킬 | 누락 시나리오 검토 |
| `/research` 스킬 | 체계적 조사 |
| `recipe status/resume` CLI | 레시피 상태 관리 |

### Phase 3: 확장 (이후)

| 산출물 | 내용 |
|--------|------|
| `/e2e` 스킬 | E2E 테스트 시나리오 |
| Hook 강화 | post-tool-use 품질 게이트 |
| moai 연동 | moai의 Run phase 후 자동 verify |
| 자동 업데이트 | 새 버전 감지 + 자가 교체 |

---

## 프로젝트명 검토

| 후보 | 의미 |
|------|------|
| `veri` | verify의 약자. 짧고 기억하기 쉬움 |
| `check` | 직관적이지만 일반적 |
| `probe` | 탐침. 검증/조사 느낌 |
| `audit` | 감사. 너무 기업적 |
| `review` | 리뷰. GitHub review와 혼동 |

`.veri/` 디렉토리, `veri init`, `veri verify`, `/veri-debug` — 자연스럽다.

---

## 검증: 이 방향이 맞는가?

**moai가 증명한 것**: Go 바이너리 + 스킬/에이전트 배포 패턴이 Claude Code 생태계에서 동작한다 (500+ stars).

**우리가 추가하는 것**: moai가 안 다루는 영역 (AI 출력물 검증)을 동일한 아키텍처로.

**리스크**:
1. Go 학습 곡선 — Go를 모르면 개발 속도 저하
2. 검증 스킬의 품질 — "서브에이전트에게 검증 시키면 진짜 잘 하는가?" 는 프롬프트 품질에 의존
3. 레시피의 실용성 — "스킬 조합"이 실제로 사용자에게 가치가 있는지 검증 필요

**완화**:
1. Go 기본기만 있으면 moai 코드를 참고하여 빠르게 개발 가능. 패턴이 동일하므로.
2. 프로토타입에서 추출 품질은 이미 검증됨 (Round 2: 10/10 파싱, 타입 분류 개선 확인)
3. Phase 1에서 `/verify`와 `/debate`만 먼저 만들어서 직접 사용해보고 검증
