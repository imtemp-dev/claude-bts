# bts — 아키텍처

## 시스템 구성

```
Go 싱글 바이너리 (bts)
  ├── CLI 명령어 (init, verify, recipe, debate, doctor, ...)
  ├── Hook 핸들러 (Claude Code lifecycle 이벤트 처리)
  ├── 검증 엔진 (Fact Checker[P1], Flow Checker[P2], Convergence Loop)
  ├── 상태 관리 (레시피, 토론, 세션 상태)
  ├── 템플릿 엔진 (go:embed로 스킬/에이전트/훅/규칙 내장)
  └── Privacy 엔진 (민감 정보 감지/제거)

Claude Code 설정 파일 (마크다운, init으로 배포)
  ├── .claude/skills/bts/     스킬 (검증, 조사, 토론 등)
  ├── .claude/agents/bts/     에이전트 (verifier, auditor 등)
  ├── .claude/commands/bts/   슬래시 커맨드 (/verify, /recipe 등)
  ├── .claude/rules/bts/      규칙 (검증 프로토콜, 레시피 프로토콜)
  └── .claude/hooks/bts/      Hook 셸 스크립트 → bts 바이너리 호출

프로젝트 상태
  └── .bts/
      ├── config/                   설정 (settings.yaml, quality.yaml)
      ├── state/                    레시피/토론/세션 상태
      ├── lessons/                  학습된 패턴 (Phase 2)
      └── manifest.json             배포 파일 추적
```

## 역할 분리

| 마크다운 (Claude가 읽음) | Go 바이너리 (Claude 밖에서 실행) |
|-------------------------|-------------------------------|
| "어떻게 검증하라" 지시 | 사실을 직접 확인 (결정론적) |
| "어떤 순서로 진행하라" 안내 | 상태를 파일에 저장/로딩 |
| 에이전트 역할 정의 | Hook 이벤트 처리 |
| 레시피 프로토콜 | 수렴 판단 (반복/종료/사람 요청) |

핵심 원칙: **같은 컨텍스트의 Claude에게 자기 결과를 검증하라고 시키지 않는다.** 사실 확인은 바이너리가 결정론적으로, 논리적 판단은 **컨텍스트가 격리된 별도 서브에이전트**가 독립적으로 수행한다. 서브에이전트도 Claude이지만, 메인 세션과 컨텍스트를 공유하지 않으므로 검증의 독립성이 보장된다.

## 오케스트레이션 모델

### 누가 전체 흐름을 제어하는가

```
오케스트레이터 = Claude (Recipe SKILL.md의 프로토콜을 따름)
진단 도구     = Go 바이너리 (bts verify — 결정론적 팩트체크)
검증 위원     = 서브에이전트 (verifier, auditor — 컨텍스트 격리된 판단)
관문          = Stop Hook (종료 전 마지막 검증)
기록          = 상태 파일 (세션 경계 초월)
```

Claude가 능동적으로 레시피를 진행한다. 바이너리와 서브에이전트는 Claude가 호출할 때만 동작한다. Stop Hook은 Claude가 "끝났다"고 할 때 진짜 끝인지 확인하는 유일한 자동 관문이다.

### 실행 흐름

```
사용자: /recipe blueprint "OAuth2 인증"
  │
  Claude: recipe-blueprint SKILL.md 로딩 → 프로토콜 읽음
  │
  ├─ Step 1: Research
  │   └─ /research 스킬 호출 (내부적으로 Agent(Explore) spawn)
  │   └─ .bts/state/{id}/01-research.md 저장
  │
  ├─ Step 2: Draft
  │   └─ 메인 Claude가 Level 3 문서 초안 작성
  │   └─ .bts/state/{id}/02-draft.md 저장
  │
  ├─ Step 3: Verify Loop (Claude가 SKILL.md 프로토콜에 따라 루프)
  │   │
  │   │  Iteration 1:
  │   │  ├─ /cross-check 스킬 호출
  │   │  │   → 내부: Bash `bts verify draft.md` (결정론적)
  │   │  │   → 내부: Agent(cross-checker) spawn (의미 확인)
  │   │  │   → 결과: mismatches[] 반환
  │   │  ├─ /verify 스킬 호출
  │   │  │   → 내부: Agent(verifier) spawn → errors[] 반환
  │   │  ├─ /audit 스킬 호출
  │   │  │   → 내부: Agent(auditor) spawn → missing[] 반환
  │   │  ├─ 결과 집계 → Bash: `bts recipe log {id} --iteration 1 ...`
  │   │  ├─ critical 1, major 2 → 문서 수정 → 다음 이터레이션
  │   │
  │   │  Iteration 2:
  │   │  ├─ 동일 스킬 호출
  │   │  ├─ 결과: critical 0, major 0, minor 1
  │   │  └─ 수렴 조건 충족 (소프트 판단) → Step 4로
  │   │
  │   └─ 최대 N회 도달 시: [CONVERGENCE FAILED] → 사람에게 보고
  │
  ├─ Step 4: Decision (필요 시)
  │   └─ /debate 스킬 호출 → 토론 → 결론 → 문서 반영
  │   └─ iteration 카운터 리셋 → Step 3 재실행 (verify-log는 유지)
  │
  └─ Step 5: Finalize
      └─ 최종 문서 → .bts/state/{id}/final.md
      └─ <bts>DONE</bts> 출력
```

### 스킬 호출 vs 직접 호출

레시피 SKILL.md 안에서 Claude가 하는 것:
- **스킬 호출**: `/cross-check`, `/verify`, `/audit`, `/research`, `/debate`
- 각 스킬 내부에서 바이너리 실행(Bash)이나 서브에이전트 spawn이 일어남
- 레시피 SKILL.md가 Bash나 Agent를 직접 호출하지 않음 — 항상 스킬을 통함

```
레시피 SKILL.md → /cross-check 스킬 호출
                    → 스킬 내부: Bash(`bts verify`) + Agent(cross-checker)
               → /verify 스킬 호출
                    → 스킬 내부: Agent(verifier)
               → /audit 스킬 호출
                    → 스킬 내부: Agent(auditor)
```

### 이중 수렴 판단 (소프트 + 하드)

| 판단 | 주체 | 시점 | 강제력 |
|------|------|------|--------|
| **소프트** | Claude (SKILL.md 프로토콜) | 매 iteration 후 | Claude가 무시할 수 있음 |
| **하드** | Stop Hook (바이너리) | Claude가 종료하려 할 때 | 무시 불가 (exit 2 = 차단) |

소프트 판단: Claude가 프로토콜에 따라 "critical 0, major 0이면 다음 단계로" 결정.
하드 판단: Claude가 프로토콜을 무시하고 조기 종료해도, Stop Hook이 verify-log를 확인하여 차단.

```
Stop Hook 로직 (Go 바이너리):
  1. stdin에서 세션 정보 읽기
  2. 활성 레시피가 있는가? → 없으면 exit 0 (통과)
  3. <bts>DONE</bts> 마커가 있는가? → 없으면 exit 0 (통과)
  4. verify-log.jsonl 마지막 이터레이션 확인
     → critical > 0 OR major > 0 → exit 2 (차단 + 피드백 주입)
     → critical = 0 AND major = 0 → exit 0 (허용)
```

### 수렴 조건

| 조건 | 기준 | 확인 주체 |
|------|------|----------|
| critical 오류 | 0 (필수) | 바이너리 (팩트체크) |
| major 오류 | 0 (필수) | 서브에이전트 (논리/완결성) |
| minor 오류 | 허용 (문서에 주석으로 포함) | 서브에이전트 |
| 최대 반복 | N회 (설정, 기본 3) | SKILL.md 프로토콜 (소프트) |
| 정체 감지 | 2회 연속 동일 오류 → 전략 전환 | 바이너리 (이력 비교, Phase 2) |

### 상태 기반 재개

세션이 끊겨도 상태 파일로 재개 가능:

```
세션 A: /recipe blueprint → Step 3 Iteration 2에서 세션 종료
  → pre-compact hook이 상태 저장
  → .bts/state/{id}/recipe.json: { phase: "verify", iteration: 2 }

세션 B: 새 세션 시작
  → session-start hook이 상태 감지
  → Claude에게 주입: "진행 중인 레시피가 있습니다: OAuth2 인증 (Step 3, Iteration 2)"
  → 사용자 또는 Claude가 /recipe resume 호출
  → recipe-blueprint SKILL.md 재로딩 + 상태 파일 읽기
  → SKILL.md에 resume 프로토콜 포함:
    "recipe.json의 phase/iteration을 확인하고 해당 Step부터 시작하라"
  → Step 3 Iteration 2부터 재개
```

---

## Hook 시스템

```
Claude Code lifecycle 이벤트
  → .claude/hooks/bts/handle-{event}.sh
    → bts hook {event} < stdin(JSON) > stdout(JSON)
```

| Hook | 시점 | 동작 |
|------|------|------|
| session-start | 세션 시작 (matcher: `startup\|resume`) | 진행 중 레시피 있으면 안내 주입 |
| pre-compact | 컨텍스트 압축 전 (matcher: `*`) | 레시피/토론 상태 스냅샷 |
| stop | 응답 완료 (matcher: `*`) | 레시피 진행 중이면 다음 단계 안내 |
| session-end | 세션 종료 (matcher: `*`) | 최종 상태 저장 |

## 검증 엔진

### Fact Checker (정적 검증, 결정론적) — Phase 1

```go
FileExists(path)           // 파일 존재 확인
SymbolExists(file, name)   // grep으로 함수/타입명 확인
LineCount(file)            // wc -l
ImportExists(file, pkg)    // import문 확인
DependencyCheck(pkg.json)  // 의존성 충돌 확인
```

### Flow Checker (흐름 검증, 결정론적 + 서브에이전트) — Phase 2

```go
TraceCallChain(from, to)   // A→B→C 호출 경로 추적
CheckDataFlow(input, output) // 데이터 변환 연결 확인
DetectCircular(entry)      // 순환 의존성 감지
```

### Convergence Loop (수렴 판단)

```
검증 실행 → 오류 목록 생성 → Claude에 전달 → 문서 수정 → 재검증
  → 오류 0? → 완료
  → 오류 남음? → 반복 (최대 N회)
  → N회 후에도 오류? → 사람에게 물어봄
  → 이전 라운드와 동일한 오류? → 전략 전환 또는 사람에게 물어봄
```

### Severity Classifier

| 등급 | 기준 | 예시 |
|------|------|------|
| critical | 존재하지 않는 것을 참조 | 없는 파일, 없는 함수 |
| major | 논리적 불일치 | 타입 불일치, 호출 불가능한 경로 |
| minor | 부정확하지만 치명적이지 않음 | 라인 수 ±10% |
| info | 개선 제안 | 더 나은 네이밍, 구조 개선 |

## 상태 관리

모든 상태는 JSON/JSONL 파일. 세션이 끊겨도 재개 가능.

```
.bts/state/
  ├── recipes/{id}/
  │   ├── recipe.json        { id, type, phase, started, updated }
  │   ├── 01-research.md     Phase 1 산출물
  │   ├── 02-design.md       Phase 2 산출물
  │   ├── 03-blueprint.md    Phase 3 산출물 (Level 3)
  │   └── verify-log.jsonl   검증 라운드 이력
  │
  ├── debates/{id}/
  │   ├── debate.json        { id, topic, rounds, conclusion }
  │   ├── round-1.md
  │   ├── round-2.md
  │   └── round-3.md
  │
  └── session.json           { active_recipe, active_debate }
```

Atomic write: temp file → os.Rename (크래시 안전)

## Privacy 엔진 (Phase 1)

검증 과정에서 민감 정보가 상태 파일에 남지 않도록:
- `<private>` 태그 스트리핑 (문서에서 제거 후 검증)
- 시크릿 패턴 감지 (API key, password, token 등 → 마스킹)
- 검증 로그(.bts/state/)에서 민감 정보 제외

## CLI 명령어

| 명령어 | 역할 |
|--------|------|
| `bts init` | 프로젝트 초기화 + 파일 배포 |
| `bts hook <event>` | Hook 핸들러 |
| `bts verify <file>` | 결정론적 팩트 체크 |
| `bts recipe status` | 레시피 상태 |
| `bts recipe resume` | 레시피 재개 |
| `bts recipe list` | 레시피 이력 |
| `bts recipe log <id>` | 검증 이터레이션 결과 기록 (스킬이 Bash로 호출) |
| `bts recipe cancel` | 진행 중 레시피 취소 |
| `bts debate list` | 토론 목록 |
| `bts debate resume <id>` | 토론 재개 |
| `bts debate export <id>` | 토론 내보내기 |
| `bts doctor` | 시스템 진단 |
| `bts config set/get` | 설정 관리 |
| `bts update` | 자동 업데이트 (Phase 2) |

## 기술 스택

| 기술 | 이유 |
|------|------|
| Go 1.22+ | 싱글 바이너리, 크로스 플랫폼, moai와 동일 패턴 |
| cobra | CLI 프레임워크 |
| bubbletea + huh | TUI (init 마법사) |
| glamour | 마크다운 렌더링 |
| go:embed | 템플릿 내장 |
| gopkg.in/yaml.v3 | 설정 파싱 |
| goreleaser | 크로스 플랫폼 릴리스 |

## 소스 구조

```
bts/
├── cmd/bts/main.go
├── internal/
│   ├── cli/          init, hook, verify, recipe, debate, config, doctor
│   ├── hook/         Hook 이벤트 핸들러 + 레지스트리
│   ├── engine/       Fact Checker (Phase 1), Flow Checker (Phase 2), Convergence Loop, Severity
│   ├── state/        레시피/토론/세션 상태 관리
│   ├── template/     go:embed 배포 엔진 + 매니페스트
│   ├── privacy/      태그 스트리핑, 시크릿 감지
│   ├── config/       설정 로딩/저장
│   └── ui/           TUI 마법사
├── pkg/models/       공유 타입
├── go.mod
├── Makefile
├── .goreleaser.yml
└── install.sh
```
