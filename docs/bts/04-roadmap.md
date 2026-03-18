# bts — 로드맵

## Phase 1: 핵심 (4-6주)

**목표**: `/recipe blueprint`가 동작하여 Level 3 문서를 생성할 수 있는 최소 셋.

### Go 바이너리

| 컴포넌트 | 내용 |
|---------|------|
| `bts init` | TUI 마법사. .claude/ + .bts/ 배포 |
| `bts hook <event>` | session-start, pre-compact, stop, session-end |
| `bts verify <file>` | Fact Checker (파일/함수/타입 존재, 라인 수) |
| `bts recipe status/resume/list` | 레시피 상태 관리 |
| `bts debate list/resume/export` | 토론 관리 |
| `bts doctor` | 시스템 진단 |
| `bts config set/get` | 설정 관리 |

### 검증 엔진

| 컴포넌트 | Phase 1 범위 |
|---------|-------------|
| Fact Checker | FileExists, SymbolExists, LineCount, ImportExists |
| Convergence Loop | 최대 N회 반복, 수렴/실패 판단 |
| Severity | critical / major / minor / info 분류 |
| Privacy | 태그 스트리핑, 시크릿 패턴 감지 |

### 스킬 + 에이전트 + 레시피

| 종류 | Phase 1 범위 |
|------|-------------|
| 스킬 | /verify, /cross-check, /audit, /debate, /research (5개) |
| 에이전트 | verifier, auditor, cross-checker (3개) |
| 레시피 | /recipe analyze, /recipe design, /recipe blueprint (3개) |

### Hook

| Hook | 동작 |
|------|------|
| session-start | 진행 중 레시피 안내 |
| pre-compact | 상태 스냅샷 |
| stop | 다음 단계 안내 |
| session-end | 최종 상태 저장 |

### 상태 관리

- .bts/state/ (recipes, debates, session)
- Atomic write (temp + rename)
- Resume 지원

### 배포

- goreleaser (macOS/Linux/Windows, amd64/arm64)
- install.sh (`curl | bash`)

---

## Phase 2: 확장 (4-6주)

**목표**: 흐름 검증, 학습, 추가 레시피.

### 추가 스킬

| 스킬 | 내용 |
|------|------|
| /flowcheck | 코드 기반 호출 체인 검증 |
| /visualize | mermaid 다이어그램 생성 (AI 자기 검증용) |
| /trace | 진입점에서 호출 체인 전체 추적 |

### 추가 레시피

| 레시피 | 내용 |
|--------|------|
| /recipe debug | 근본 원인 분석 문서 |
| /recipe decide | 의사결정 문서 (토론 포함) |
| /recipe flow | 흐름 문서 + 다이어그램 |
| /recipe api | API 설계 문서 |

### 검증 엔진 확장

| 컴포넌트 | 내용 |
|---------|------|
| Flow Checker | TraceCallChain, CheckDataFlow, DetectCircular |
| Stagnation Detector | 이전 라운드와 동일 오류 → 전략 전환 |
| Stale Detection | git diff 기반 문서 신선도 |

### Lesson Engine

| 기능 | 내용 |
|------|------|
| 패턴 감지 | 검증 이력에서 반복 오류 패턴 추출 |
| Proactive injection | 관련 파일 작업 시 과거 패턴 자동 경고 |
| lesson CLI | list, add |

### 기타

| 기능 | 내용 |
|------|------|
| 자동 업데이트 | `bts update` |
| 매니페스트 추적 | 배포 파일 버전 관리 |
| Content-hash 캐싱 | 동일 검증 중복 방지 |
| 검증 보고서 | `bts report` |

---

## Phase 3: 고급 (4-6주)

**목표**: 팀 공유, 외부 연동, 고급 검증.

### 추가 레시피

| 레시피 | 내용 |
|--------|------|
| /recipe dataflow | 데이터 파이프라인 문서 |
| /recipe integration | 시스템 통합 문서 |

### 팀 기능

| 기능 | 내용 |
|------|------|
| export/import | 검증 결과, lesson 내보내기/가져오기 |
| 팀 lesson 공유 | 공통 패턴 배포 |

### 외부 연동

| 연동 | 내용 |
|------|------|
| moai | bts 문서 → moai SPEC 변환 |
| GitHub Actions | CI에서 bts verify 실행 |
| git pre-push | 검증 안 된 문서 push 차단 |

### 고급 검증

| 기능 | 내용 |
|------|------|
| AST 기반 시그니처 확인 | tree-sitter로 함수 파라미터/리턴 타입 정확 확인 |
| 의존성 그래프 | import 관계 시각화 + 충돌 감지 |

---

## 타임라인 요약

```
Phase 1 (4-6주):
  init + 5 스킬 + 3 레시피 + 3 에이전트
  Fact Checker + Convergence Loop
  상태 관리 + 4 Hook
  → "/recipe blueprint" 동작

Phase 2 (4-6주):
  + 3 스킬 + 4 레시피
  Flow Checker + Lesson Engine
  → 흐름 검증, 다이어그램, 학습

Phase 3 (4-6주):
  + 2 레시피
  팀 공유, moai 연동, CI/CD
  → 팀 단위 사용
```

## 성공 기준

| 기준 | Phase 1 | 전체 완성 |
|------|---------|----------|
| 설치 | `curl \| bash` 30초 | 동일 |
| 초기화 | `bts init` 1분 | 동일 |
| 첫 레시피 | `/recipe blueprint` → Level 3 문서 생성 | 9개 레시피 모두 동작 |
| 문서 품질 | 사실 오류 0 (critical/major), 논리 오류 0 | + 흐름 오류 0, 완결성 점수 달성 |
| 문서→코드 (참고지표) | Level 3 문서로 Opus가 높은 완성도의 코드 생성 가능 | 동일 (bts 통제 밖, Opus 성능 의존) |
| 토론 | 3라운드 토론 + 저장 + resume | + 교착 감지 + 사람 개입 |
| 바이너리 | ≤15MB | ≤20MB |
