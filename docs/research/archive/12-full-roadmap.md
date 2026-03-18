# veri — 전체 로드맵

> Claude Code 출력물 검증 프레임워크
> "AI가 한 일이 맞는지 확인하는 체계적 방법"

---

## 완성된 그림

### 개발자의 하루 (veri가 완성된 세계)

```
09:00 — 버그 리포트 도착
  $ /recipe debug "로그인 시 500 에러"
  → veri가 자동으로:
    1. 관련 코드 조사 (research)
    2. 에러 재현 경로 분석 (analyze)
    3. 근본 원인 문서 작성 (document)
    4. 문서의 사실 관계를 코드와 대조 (cross-check) ← 결정론적
    5. 논리적 오류 검증 (verify) ← 서브에이전트
    6. 수정 계획 수립 (plan mode)
    7. 수정 구현
    8. E2E 테스트 생성 + 실행 (e2e)
    9. 통과까지 반복
  → 전체 과정이 .veri/state/에 기록
  → 다음 날 이어서 가능 (recipe resume)

11:00 — 새 기능 요청
  $ /recipe feature "OAuth2 인증 추가"
  → veri가 자동으로:
    1. 기존 인증 코드 조사
    2. 설계 문서 초안 작성
    3. 검증 루프 (verify → fix → verify, 최대 3회)
    4. 불확실한 결정 → 전문가 토론 (/debate)
    5. 토론 결론 검증
    6. 최종 설계 문서 확정
    7. 구현 (plan mode → code)
    8. E2E 테스트
  → 설계 문서에 모든 결정 근거가 남음

14:00 — PR 리뷰
  $ /recipe review PR#42
  → veri가 자동으로:
    1. PR diff 분석
    2. 변경된 파일의 기존 검증 이력 조회
    3. "이전에 이 파일에서 발견된 패턴" 표시
    4. 논리적 일관성 검증
    5. 누락 테스트 케이스 식별
    6. 리뷰 코멘트 초안 생성 + 검증

16:00 — 이전 토론 이어서
  $ veri debate resume auth-strategy
  → 2일 전 "OAuth2 vs JWT" 토론 3라운드 결과 로딩
  → 새로운 정보 추가하여 4라운드 진행
  → 결론 업데이트

17:00 — 하루 마무리
  → pre-compact hook이 자동으로 진행 중 레시피 상태 저장
  → 내일 세션 시작 시 "진행 중인 레시피 2개" 안내
```

---

## 시스템 전체 구조 (완성 시)

```
┌─ 사용자 인터페이스 ──────────────────────────────────────────┐
│                                                               │
│  Claude Code 내:                                              │
│    /verify, /audit, /cross-check, /debate                     │
│    /research, /e2e, /recipe                                   │
│    /review (PR 리뷰)                                          │
│                                                               │
│  터미널:                                                       │
│    veri init, veri doctor, veri update                         │
│    veri recipe status/resume/list/cancel                      │
│    veri debate list/resume/export                              │
│    veri verify <file>, veri report                             │
│    veri lesson list/add                                        │
│    veri config set/get                                         │
│                                                               │
└───────────────────────────┬───────────────────────────────────┘
                            │
┌─ Claude Code 통합 ────────┴──────────────────────────────────┐
│                                                               │
│  .claude/                                                     │
│    ├── skills/veri/          스킬 (검증, 조사, 토론, 테스트)    │
│    ├── agents/veri/          에이전트 (verifier, auditor 등)    │
│    ├── commands/veri/        슬래시 커맨드                      │
│    ├── rules/veri/           검증 규칙, 레시피 프로토콜          │
│    └── hooks/veri/           Hook 핸들러 셸 스크립트            │
│                                                               │
└───────────────────────────┬───────────────────────────────────┘
                            │
┌─ Go 바이너리 (veri) ──────┴──────────────────────────────────┐
│                                                               │
│  ┌─ CLI Layer ────────────────────────────────────────────┐  │
│  │  init, doctor, update, config                           │  │
│  │  recipe (status/resume/list/cancel)                     │  │
│  │  debate (list/resume/export)                            │  │
│  │  verify (결정론적 팩트 체크)                               │  │
│  │  report (검증 보고서 생성)                                 │  │
│  │  lesson (패턴 관리)                                       │  │
│  └────────────────────────────────────────────────────────┘  │
│                                                               │
│  ┌─ Hook Engine ──────────────────────────────────────────┐  │
│  │  session-start: 레시피 상태 복원 안내                     │  │
│  │  pre-compact: 상태 스냅샷                                │  │
│  │  post-tool-use: 레시피 범위 내 파일 변경 감지             │  │
│  │  stop: 레시피 다음 단계 안내                              │  │
│  │  session-end: 최종 상태 저장                              │  │
│  └────────────────────────────────────────────────────────┘  │
│                                                               │
│  ┌─ Verification Engine ──────────────────────────────────┐  │
│  │  Fact Checker:                                          │  │
│  │    파일 존재 확인, 함수/타입명 검증, 라인 수 확인          │  │
│  │    git diff (stale detection), AST 기반 시그니처 확인     │  │
│  │  Convergence Loop:                                      │  │
│  │    검증 → 오류 발견 → 수정 지시 → 재검증 (최대 N회)       │  │
│  │  Severity Classifier:                                   │  │
│  │    critical / major / minor / info                      │  │
│  └────────────────────────────────────────────────────────┘  │
│                                                               │
│  ┌─ State Manager ────────────────────────────────────────┐  │
│  │  Recipe state: phase, progress, artifacts               │  │
│  │  Debate state: rounds, conclusions, participants        │  │
│  │  Verification log: history, patterns, lessons           │  │
│  │  Atomic writes: temp + rename                           │  │
│  │  Resume: 세션 경계 초월                                   │  │
│  └────────────────────────────────────────────────────────┘  │
│                                                               │
│  ┌─ Lesson Engine ────────────────────────────────────────┐  │
│  │  패턴 감지: "이 프로젝트에서 자주 발생하는 오류"            │  │
│  │  Proactive injection: 관련 파일 작업 시 자동 경고          │  │
│  │  학습: 검증 이력에서 반복 패턴 추출                        │  │
│  └────────────────────────────────────────────────────────┘  │
│                                                               │
│  ┌─ Template Engine ──────────────────────────────────────┐  │
│  │  go:embed로 내장된 스킬/에이전트/규칙/훅/커맨드 템플릿     │  │
│  │  init 시 프로젝트에 배포                                  │  │
│  │  update 시 변경분만 교체 (매니페스트 추적)                  │  │
│  └────────────────────────────────────────────────────────┘  │
│                                                               │
│  ┌─ Privacy Engine ───────────────────────────────────────┐  │
│  │  <private> 태그 스트리핑                                  │  │
│  │  시크릿 패턴 감지 (API key, password, token)              │  │
│  │  검증 로그에서 민감 정보 마스킹                             │  │
│  └────────────────────────────────────────────────────────┘  │
│                                                               │
└───────────────────────────┬───────────────────────────────────┘
                            │
┌─ 데이터 저장소 ───────────┴──────────────────────────────────┐
│                                                               │
│  .veri/                                                       │
│    ├── config/                                                │
│    │   ├── settings.yaml        사용자 설정                    │
│    │   ├── quality.yaml         검증 기준 (엄격도, 반복 횟수)   │
│    │   └── profile.yaml         프로파일 (이름, 언어, 모델)     │
│    ├── state/                                                 │
│    │   ├── recipes/             레시피 실행 상태               │
│    │   ├── debates/             토론 기록                      │
│    │   ├── verifications/       검증 이력                      │
│    │   └── session.json         현재 세션 상태                 │
│    ├── lessons/                                               │
│    │   ├── patterns.jsonl       발견된 반복 패턴               │
│    │   └── project-rules.md     프로젝트 학습 규칙             │
│    ├── reports/                                               │
│    │   └── {date}-{recipe}.md   검증 보고서                    │
│    ├── manifest.json            배포 파일 추적                  │
│    └── templates/               (embed, init 시 배포)          │
│                                                               │
└───────────────────────────────────────────────────────────────┘
```

---

## 전체 기능 목록

### 스킬 (Skills) — Claude Code 내에서 호출

| 스킬 | 설명 | Phase |
|------|------|-------|
| `/verify` | 논리적 오류 검증. 서브에이전트(verifier)가 문서/코드의 논리적 일관성 확인 | 1 |
| `/cross-check` | 사실 대조. 바이너리가 결정론적 확인(파일/함수/라인) + 서브에이전트가 의미 확인 | 1 |
| `/audit` | 누락 검토. 서브에이전트(auditor)가 "빠뜨린 시나리오, 고려하지 않은 edge case" 검토 | 1 |
| `/debate` | 전문가 토론. 3명 페르소나가 라운드별 토론. 상태 저장, 이어서 가능 | 1 |
| `/research` | 체계적 조사. 코드/문서/웹을 병렬 조사하여 구조화된 결과 생성 | 1 |
| `/e2e` | E2E 테스트. 기능에 대한 테스트 시나리오 생성 → 실행 → 결과 검증 | 2 |
| `/review` | PR 리뷰. diff 분석 + 이전 검증 이력 조회 + 리뷰 코멘트 생성 | 2 |
| `/recap` | 세션 요약. 현재 세션에서 한 일, 결정, 남은 것 정리 | 2 |
| `/lesson` | 교훈 기록. 발견한 패턴/실수를 lesson으로 저장 | 2 |

### 레시피 (Recipes) — 스킬의 조합

| 레시피 | 스킬 조합 | 검증 게이트 | Phase |
|--------|----------|------------|-------|
| `/recipe debug` | research → analyze → (debate?) → plan → implement → e2e | cross-check 후 verify 통과까지 | 1 |
| `/recipe feature` | research → design → verify-loop → (debate?) → plan → implement → e2e | design 문서 verify 3회 이내 통과 | 1 |
| `/recipe analyze` | research → document → cross-check → verify → audit → final | 교차검증 + 논리검증 둘 다 통과 | 1 |
| `/recipe refactor` | research → audit(영향) → design → verify → implement → e2e | audit 커버리지 + e2e 통과 | 2 |
| `/recipe review` | diff-analyze → history → verify → comment-draft → verify | 리뷰 코멘트의 사실 관계 검증 | 2 |
| `/recipe migrate` | research → risk-audit → plan → implement → e2e → rollback-plan | 마이그레이션 리스크 검증 | 3 |
| `/recipe security` | scan → classify → verify → remediate → re-scan | 보안 취약점 0까지 반복 | 3 |

### 에이전트 (Agents) — 검증 전문 서브에이전트

| 에이전트 | 역할 | 도구 제한 | Phase |
|---------|------|----------|-------|
| `verifier` | 논리적 일관성 전문 검증 | Read, Grep, Glob | 1 |
| `auditor` | 커버리지/누락 전문 검토 | Read, Grep, Glob | 1 |
| `cross-checker` | 사실 대조 전문 | Read, Grep, Bash(wc, grep, find) | 1 |
| `debater` | 토론 페르소나 (설정별 전문 분야 변경) | Read | 1 |
| `researcher` | 체계적 조사 전문 | Read, Grep, Glob, WebSearch, WebFetch | 2 |
| `test-designer` | 테스트 시나리오 설계 전문 | Read, Grep, Glob | 2 |
| `reviewer` | 코드 리뷰 전문 | Read, Grep, Glob, Bash(git diff) | 2 |

### CLI 명령어

| 명령어 | 설명 | Phase |
|--------|------|-------|
| `veri init [project]` | 프로젝트 초기화. TUI 마법사. 스킬/에이전트/훅/규칙 배포 | 1 |
| `veri hook <event>` | Hook 핸들러 실행 (Claude Code가 호출) | 1 |
| `veri verify <file>` | 결정론적 팩트 체크 (파일 존재, 함수명, 라인 수, git diff) | 1 |
| `veri recipe status` | 현재 레시피 진행 상태 표시 | 1 |
| `veri recipe resume [id]` | 중단된 레시피 재개 | 1 |
| `veri recipe list` | 모든 레시피 이력 | 1 |
| `veri recipe cancel` | 진행 중 레시피 취소 | 1 |
| `veri debate list` | 저장된 토론 목록 | 1 |
| `veri debate resume <id>` | 중단된 토론 재개 | 1 |
| `veri debate export <id>` | 토론 결과를 마크다운으로 내보내기 | 1 |
| `veri doctor` | 시스템 진단 (Go, Claude Code, 설정 유효성) | 1 |
| `veri config set <key> <value>` | 설정 변경 | 1 |
| `veri config get [key]` | 설정 조회 | 1 |
| `veri update` | 자동 업데이트 | 2 |
| `veri report [recipe-id]` | 검증 보고서 생성 (마크다운) | 2 |
| `veri lesson list` | 학습된 패턴 목록 | 2 |
| `veri lesson add <pattern>` | 수동 패턴 추가 | 2 |
| `veri status` | 프로젝트 전체 상태 (레시피, 토론, lesson 수, 검증 이력) | 2 |
| `veri export` | 전체 상태를 이동 가능한 형태로 내보내기 | 3 |

### Hook 핸들러

| Hook | 시점 | 동작 | Phase |
|------|------|------|-------|
| `session-start` | 세션 시작 | 진행 중 레시피 있으면 안내 메시지 주입 | 1 |
| `pre-compact` | 컨텍스트 압축 전 | 레시피/토론 상태 스냅샷 저장 | 1 |
| `stop` | 응답 완료 | 레시피 진행 중이면 다음 단계 안내 | 1 |
| `session-end` | 세션 종료 | 최종 상태 저장, 미완료 레시피 기록 | 1 |
| `post-tool-use` | Edit/Write 후 | 레시피 범위 내 파일 변경 감지. 검증 필요 여부 판단 | 2 |
| `user-prompt-submit` | 사용자 입력 | 관련 lesson 있으면 경고 주입 | 2 |
| `subagent-stop` | 서브에이전트 완료 | 검증 결과 수집, 수렴 판단 | 2 |

### 규칙 (Rules)

| 규칙 | 내용 | Phase |
|------|------|-------|
| `core/verification-protocol.md` | 검증의 기본 원칙. "사실 확인은 바이너리, 판단은 에이전트" | 1 |
| `core/recipe-protocol.md` | 레시피 실행 규칙. Phase 전환 조건, 상태 저장 의무 | 1 |
| `core/debate-protocol.md` | 토론 규칙. 라운드 구조, 결론 도출 방법 | 1 |
| `core/severity-classification.md` | 심각도 분류 기준. critical/major/minor/info 정의 | 1 |
| `core/convergence-rules.md` | 수렴 루프 규칙. 최대 반복, 정체 감지, 전략 전환 | 1 |
| `workflow/debug-recipe.md` | 디버그 레시피 상세 프로토콜 | 1 |
| `workflow/feature-recipe.md` | 기능 구현 레시피 상세 프로토콜 | 1 |
| `workflow/analyze-recipe.md` | 분석 레시피 상세 프로토콜 | 1 |
| `quality/fact-check-rules.md` | 결정론적 검증 규칙. 어떤 사실을 어떻게 확인하는가 | 1 |
| `quality/low-signal-rules.md` | 검증 가치 없는 항목 필터 규칙 | 1 |
| `privacy/stripping-rules.md` | 민감 정보 제거 규칙 | 1 |

### 검증 엔진 (Go 바이너리 내부)

| 컴포넌트 | 역할 | Phase |
|---------|------|-------|
| **Fact Checker** | 결정론적 사실 확인 | 1 |
| ├ FileExistence | `os.Stat()` — 파일/디렉토리 존재 확인 | 1 |
| ├ SymbolGrep | `grep -rn` — 함수명/타입명/변수명이 소스에 존재하는지 | 1 |
| ├ LineCounter | `wc -l` — 라인 수 일치 확인 | 1 |
| ├ GitDiff | `git diff` — 문서가 참조하는 코드가 변경되었는지 (stale) | 2 |
| └ ASTChecker | AST 파싱 — 함수 시그니처, 파라미터 타입 확인 | 3 |
| **Convergence Loop** | 수렴 판단 | 1 |
| ├ MaxIterations | 설정 가능한 최대 반복 횟수 (기본 3) | 1 |
| ├ StagnationDetector | 이전 라운드와 비교하여 개선 없으면 전략 전환 | 2 |
| └ HumanBreakpoint | 사람 개입 요청 (자동 수렴 불가 시) | 2 |
| **Severity Classifier** | 발견된 문제 분류 | 1 |
| ├ Critical | 사실 오류, 존재하지 않는 파일/함수 참조 | 1 |
| ├ Major | 논리적 불일치, 누락된 시나리오 | 1 |
| ├ Minor | 표현 모호, 부정확한 수치 (±10% 이내) | 1 |
| └ Info | 개선 제안, 스타일 | 1 |

### 상태 관리

| 상태 | 구조 | Phase |
|------|------|-------|
| **Recipe State** | | 1 |
| ├ recipe.json | `{ id, type, phase, started, updated, artifacts[] }` | 1 |
| ├ {phase}.md | 각 Phase의 산출물 (research.md, design.md, final.md) | 1 |
| └ verify-log.jsonl | 각 검증 라운드의 결과 (errors, severity, iteration) | 1 |
| **Debate State** | | 1 |
| ├ debate.json | `{ id, topic, rounds, conclusion, participants[] }` | 1 |
| └ round-{n}.md | 각 라운드 기록 (참가자별 의견, 반론, 합의) | 1 |
| **Session State** | | 1 |
| └ session.json | `{ active_recipe, active_debate, last_hook_at }` | 1 |
| **Lesson State** | | 2 |
| ├ patterns.jsonl | `{ pattern, file_pattern, frequency, severity, first_seen }` | 2 |
| └ project-rules.md | 축적된 프로젝트별 규칙 (CLAUDE.md와 별도) | 2 |
| **Verification History** | | 2 |
| └ history.jsonl | `{ timestamp, file, type, result, issues[] }` | 2 |

---

## Phase 계획 상세

### Phase 1: 핵심 (4-6주)

**목표**: 직접 매일 사용할 수 있는 최소 셋. `/verify`, `/debate`, `/recipe`가 동작.

```
Week 1-2: 뼈대
  ├─ Go 프로젝트 초기화 (cobra CLI, go:embed)
  ├─ init 명령어 + TUI 마법사 (bubbletea)
  ├─ 템플릿 배포 엔진 (moai 패턴)
  ├─ 상태 관리 기본 (atomic read/write, recipe state)
  └─ 스킬 5개 + 에이전트 4개 + 규칙 마크다운 작성

Week 3-4: 검증 엔진
  ├─ Fact Checker (파일 존재, 함수명, 라인 수)
  ├─ Severity Classifier (critical/major/minor/info)
  ├─ Hook 핸들러 4개 (session-start, pre-compact, stop, session-end)
  ├─ Convergence Loop (최대 N회 반복)
  └─ 결정론적 verify CLI

Week 5-6: 레시피 + 토론
  ├─ 레시피 3개 (debug, feature, analyze) 프로토콜 작성
  ├─ recipe status/resume/list CLI
  ├─ Debate 상태 관리 (라운드별 저장, resume)
  ├─ debate list/resume/export CLI
  ├─ Privacy stripping (태그 제거, 시크릿 감지)
  └─ doctor, config CLI

산출물:
  - Go 바이너리 (~3,000-5,000줄)
  - 마크다운 템플릿 (~2,000-3,000줄)
  - install.sh
  - goreleaser 설정 (macOS/Linux/Windows)
```

### Phase 2: 확장 (4-6주)

**목표**: 실사용에서 발견된 부족분 보강. 학습 기능, 추가 레시피, 자동화 강화.

```
Lesson Engine:
  ├─ 검증 이력에서 반복 패턴 자동 감지
  ├─ 관련 파일 작업 시 lesson 자동 주입 (user-prompt-submit hook)
  ├─ lesson CLI (list, add)
  └─ project-rules.md 자동 생성

추가 스킬/레시피:
  ├─ /e2e 스킬 (테스트 시나리오 생성 + 실행)
  ├─ /review 스킬 (PR 리뷰)
  ├─ /recap 스킬 (세션 요약)
  ├─ /recipe refactor
  └─ /recipe review

검증 엔진 강화:
  ├─ Git diff stale detection
  ├─ Stagnation detector (수렴 루프 정체 감지)
  ├─ Human breakpoint (사람 개입 요청)
  └─ Intent 분류 (검증 요청의 의도 자동 판별)

자동화 강화:
  ├─ post-tool-use hook (파일 변경 감지)
  ├─ subagent-stop hook (검증 결과 수집)
  ├─ user-prompt-submit hook (lesson 주입)
  └─ 자동 업데이트 (veri update)

인프라:
  ├─ Verification history (검증 이력 저장)
  ├─ Report 생성 (검증 보고서 마크다운)
  ├─ 매니페스트 추적 (배포 파일 버전 관리)
  ├─ Content-hash 캐싱 (동일 입력 중복 검증 방지)
  └─ veri status (프로젝트 전체 상태)
```

### Phase 3: 고급 (4-6주)

**목표**: 팀 기능, 고급 검증, 다른 도구 연동.

```
고급 검증:
  ├─ AST 기반 시그니처 확인 (tree-sitter)
  ├─ 의존성 그래프 검증 (import/require 추적)
  ├─ /recipe migrate (마이그레이션 리스크 검증)
  └─ /recipe security (보안 취약점 스캔 + 반복 수정)

팀 기능:
  ├─ 검증 결과 내보내기/가져오기 (veri export/import)
  ├─ lesson 공유 (팀 공통 패턴)
  └─ 프로파일 시스템 (팀원별 설정)

연동:
  ├─ moai 연동 (moai Run 후 자동 veri verify)
  ├─ GitHub Actions (CI/CD에서 veri verify 실행)
  └─ Git pre-push hook (검증 안 된 변경 push 차단)

기타:
  ├─ Token pressure 관리 (긴 검증 세션 최적화)
  ├─ Health-based eviction (오래된 상태 자동 정리)
  └─ 다국어 지원 (i18n)
```

---

## 기술 스택

| 계층 | 기술 | 이유 |
|------|------|------|
| 언어 | Go 1.22+ | 싱글 바이너리, 크로스 플랫폼, moai와 동일 패턴 |
| CLI | cobra | Go 표준 CLI 프레임워크, moai와 동일 |
| TUI | bubbletea + huh + glamour | 대화형 마법사, 마크다운 렌더링, moai와 동일 |
| 템플릿 | go:embed | 바이너리 내 파일 내장 |
| 설정 | YAML (gopkg.in/yaml.v3) | moai와 동일 |
| 상태 | JSON/JSONL 파일 | 단순, 사람이 읽을 수 있음, 도구 없이 편집 가능 |
| 빌드 | goreleaser | 크로스 플랫폼 바이너리, GitHub Releases |
| 테스트 | Go 표준 testing | |
| CI | GitHub Actions | 빌드/테스트/릴리스 자동화 |

---

## 디렉토리 구조 (프로젝트 소스)

```
veri/
├── cmd/veri/
│   └── main.go                    진입점
├── internal/
│   ├── cli/                       CLI 명령어
│   │   ├── root.go                루트 명령
│   │   ├── init.go                프로젝트 초기화
│   │   ├── hook.go                Hook 핸들러 디스패치
│   │   ├── verify.go              결정론적 검증
│   │   ├── recipe.go              레시피 관리
│   │   ├── debate.go              토론 관리
│   │   ├── config.go              설정 관리
│   │   ├── doctor.go              시스템 진단
│   │   ├── update.go              자동 업데이트 (Phase 2)
│   │   ├── report.go              보고서 생성 (Phase 2)
│   │   └── lesson.go              교훈 관리 (Phase 2)
│   ├── hook/                      Hook 이벤트 핸들러
│   │   ├── registry.go            이벤트→핸들러 매핑
│   │   ├── session_start.go
│   │   ├── pre_compact.go
│   │   ├── stop.go
│   │   └── session_end.go
│   ├── engine/                    검증 엔진
│   │   ├── fact_checker.go        결정론적 사실 확인
│   │   ├── convergence.go         수렴 루프
│   │   ├── severity.go            심각도 분류
│   │   └── stale.go               stale detection (Phase 2)
│   ├── state/                     상태 관리
│   │   ├── manager.go             읽기/쓰기/atomic
│   │   ├── recipe.go              레시피 상태
│   │   ├── debate.go              토론 상태
│   │   └── session.go             세션 상태
│   ├── template/                  템플릿 배포
│   │   ├── deployer.go            파일 배포 엔진
│   │   ├── manifest.go            배포 추적
│   │   └── templates/             go:embed 대상
│   │       ├── .claude/skills/
│   │       ├── .claude/agents/
│   │       ├── .claude/commands/
│   │       ├── .claude/rules/
│   │       └── .claude/hooks/
│   ├── privacy/                   민감 정보 처리
│   │   ├── stripper.go            태그 제거
│   │   └── detector.go            시크릿 패턴 감지
│   ├── lesson/                    학습 엔진 (Phase 2)
│   │   ├── engine.go              패턴 감지
│   │   └── store.go               패턴 저장
│   ├── config/                    설정 관리
│   │   ├── manager.go             로딩/저장/검증
│   │   └── defaults.go            기본값
│   ├── ui/                        TUI
│   │   ├── wizard.go              init 마법사
│   │   └── progress.go            진행 표시
│   └── update/                    자동 업데이트 (Phase 2)
│       └── updater.go
├── pkg/
│   ├── models/                    공유 타입
│   │   ├── recipe.go
│   │   ├── debate.go
│   │   ├── verification.go
│   │   └── config.go
│   └── version/
│       └── version.go
├── go.mod
├── go.sum
├── Makefile
├── .goreleaser.yml
├── install.sh
├── install.ps1
└── .github/workflows/
    └── release.yml
```

---

## 성공 기준

### Phase 1 완료 시

| 기준 | 목표 |
|------|------|
| 설치 | `curl \| bash` → 30초 이내 완료 |
| 초기화 | `veri init` → 1분 이내 완료 |
| 첫 검증 | `/verify` 실행 → 서브에이전트가 오류 발견 → 수정 → 재검증 통과 |
| 첫 토론 | `/debate` 실행 → 3라운드 → 결론 → 저장 → 다음 세션에서 resume |
| 첫 레시피 | `/recipe analyze` 실행 → 조사→문서→검증→최종문서 생성 |
| 상태 관리 | 세션 끊김 후 `recipe resume`으로 복구 |
| 결정론적 검증 | `veri verify doc.md` → 파일 존재/함수명/라인 수 불일치 보고 |

### 전체 완료 시

| 기준 | 목표 |
|------|------|
| 일일 사용 | 레시피로 모든 개발 작업 진행 (debug/feature/analyze/review) |
| 검증 품질 | 문서 오류 70%+ 자동 감지 (현재 수동으로 발견하는 것 대비) |
| 학습 | 반복 오류 패턴 자동 표면화, 프로젝트별 검증 규칙 축적 |
| 팀 공유 | lesson과 검증 규칙을 팀에 공유 |
| 바이너리 크기 | ≤15MB (설치 ≤30초) |
