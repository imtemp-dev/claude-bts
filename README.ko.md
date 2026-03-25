# claude-forge

코드 전에 검증하라 — forge는 스펙 오류가 디버깅 세션이 되기 전에 잡아냅니다.

[![CI](https://github.com/imtemp-dev/claude-forge/actions/workflows/ci.yml/badge.svg)](https://github.com/imtemp-dev/claude-forge/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/imtemp-dev/claude-forge)](https://github.com/imtemp-dev/claude-forge/releases)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.22+-00ADD8.svg)](https://go.dev)

[English](README.md) | [中文](README.zh.md) | [日本語](README.ja.md)

## Why

Claude Code를 진지하게 사용한다면, 아마 나만의 프로세스를 이미 만들었을 것입니다: AI에게 전체 아키텍처를 상기시키고, 출력을 직접 리뷰하게 하고, 엣지 케이스를 확인하기. 이건 올바른 접근입니다.

하지만 이걸 매번 수동으로 하면 현실적인 한계가 있습니다:

- **일관성이 없습니다.** 어떤 세션에서는 리뷰를 요청하고, 어떤 세션에서는 잊어버립니다. 품질이 그날의 꼼꼼함에 좌우됩니다.
- **에러는 일찍 잡을수록 쌉니다.** 계획의 실수가 바로 코드로 넘어가면 여러 파일에 퍼지고, 잡으려면 빌드와 디버깅이 필요합니다. 계획 단계에서 잡으면 텍스트 수정이면 끝입니다. 하지만 검증 단계가 없으면, 계획 수준의 오류는 코드 문제가 되기 전에 잡힐 기회가 없습니다.
- **구현이 목적지를 삼킵니다.** AI는 계획을 세울 수 있지만, 코드에 깊이 들어가면 — 타입 에러 수정, 테스트 실패 추적 — 완성된 시스템이 전체적으로 어떤 모습이어야 하는지를 놓칩니다. forge가 의도 파악, 범위 설정, 와이어프레임으로 시작하는 이유입니다: 세부사항이 컨텍스트를 잡아먹기 전에 큰 그림을 확립합니다.

패턴은 항상 같습니다: 대화로 품질 관리를 하고 있지만, 매 세션마다 처음부터 반복하며, 지난번에 잡았던 것이 이번에도 잡힌다는 보장이 없습니다.

## forge가 하는 일

forge는 Claude Code의 라이프사이클 훅에 연결되는 CLI 도구입니다. 이미 하고 있는 프로세스를 구조화하되 — 자동으로, 추적 가능하게, 별도 AI 컨텍스트에서 검증합니다.

**구조화된 큰 그림 우선.** 코드 전에, forge는 의도 탐색, 범위 정의, 와이어프레임 설계를 거칩니다. 이후 모든 단계 — 초안 작성, 검증, 구현 — 가 돌아볼 수 있는 목적지를 줌으로써, AI가 전체를 희생하고 당장의 문제에 최적화하는 것을 방지합니다.

**독립된 검증.** 같은 세션에서 AI가 자기 출력을 리뷰하면 같은 맹점을 공유합니다. forge는 별도 에이전트 컨텍스트에서 검증을 실행합니다 — 문서를 만든 대화 히스토리를 공유하지 않는 다른 AI 인스턴스입니다.

**세션 간 상태 추적.** forge는 검증에서 발견된 모든 이슈를 기록하고, 해결 여부를 추적하며, 세션과 컨텍스트 압축을 넘어 유지합니다. 세션이 재개되면 어디까지 진행했고 뭐가 미해결인지 정확히 알고 있습니다.

**완료 게이트.** 검증을 통과하지 않으면 스펙을 확정할 수 없습니다. 테스트 통과, 리뷰 완료, 스펙-코드 차이 문서화 전까지 구현을 완료할 수 없습니다. 이 게이트는 자동으로 적용됩니다 — 당신이 확인을 잊어도 상관없습니다.

기본 아이디어는 단순합니다: **에러를 코드가 아닌 문서에서 잡는다.** 스펙 수정은 텍스트 편집이고, 코드 수정은 빌드-테스트-디버그 사이클입니다. 구현 전에 걸러내는 에러가 많을수록, 구현 후 재작업이 줄어듭니다.

## 빠른 시작

[Claude Code](https://docs.anthropic.com/en/docs/claude-code)가 필요합니다.

```bash
# Homebrew (macOS / Linux)
brew tap imtemp-dev/tap
brew install forge

# 또는 원라인 설치
curl -fsSL https://raw.githubusercontent.com/imtemp-dev/claude-forge/main/install.sh | bash

# 또는 소스에서 빌드 (Go 1.22+)
git clone https://github.com/imtemp-dev/claude-forge.git && cd claude-forge && make install

# 프로젝트에서 초기화
cd your-project
forge init .

# Claude Code 시작
claude
```

Claude Code 내에서:

```bash
# 완벽한 스펙 생성 → 구현 → 테스트 → 완료
/forge-recipe-blueprint OAuth2 인증 추가

# 알려진 버그 수정
/forge-recipe-fix 로그인 bcrypt 해시 비교 실패

# 원인 모르는 이슈 디버그
/forge-recipe-debug 5분 후 세션 끊김
```

## 작동 방식

forge는 작업을 **스펙**과 **구현** 두 단계로 나눕니다. 각 레시피 타입은 고유한 스펙 단계를 갖지만, 모두 같은 구현 루프를 공유합니다.

스펙 단계에서 forge는 문서를 반복합니다 — 의도를 탐색하고, 코드베이스를 조사하고, 상세 설계를 작성하고, 별도 AI 컨텍스트에서 여러 라운드의 검증을 거칩니다. 여기서 잡힌 에러는 텍스트 수정 비용입니다.

구현 단계에서 forge는 확정된 스펙에서 코드를 생성하고, 테스트를 실행(실패 시 재시도)하고, 코드 경로를 시뮬레이션하고, 품질을 리뷰하고, 차이를 스펙에 동기화합니다. 각 단계에는 요구사항이 충족될 때까지 완료를 차단하는 자동 게이트가 있습니다.

각 레시피 타입의 상세 flow는 [레시피 라이프사이클](#레시피-라이프사이클)을 참조하세요.

## 레시피

| 레시피 | 용도 | 출력 |
|--------|------|------|
| `/forge-recipe-blueprint` | 전체 구현 스펙 | Level 3 스펙 → 코드 → 테스트 |
| `/forge-recipe-design` | 기능 설계 | Level 2 설계 문서 |
| `/forge-recipe-analyze` | 기존 시스템 이해 | Level 1 분석 문서 |
| `/forge-recipe-fix` | 알려진 버그 수정 | 수정 스펙 → 코드 → 테스트 |
| `/forge-recipe-debug` | 원인 모르는 버그 조사 | 6관점 분석 → 스펙 → 코드 |

다중 기능 프로젝트의 경우, forge는 작업을 **비전 + 로드맵**으로 분해합니다. 각 레시피는 로드맵 항목에 매핑되며 완료가 자동으로 추적됩니다.

## 기능

### 21개 스킬

| 카테고리 | 스킬 |
|----------|------|
| **레시피** | blueprint, design, analyze, fix, debug |
| **탐색** | discover, wireframe |
| **검증** | verify, cross-check, audit, assess, sync-check |
| **분석** | research, simulate, debate, adjudicate |
| **구현** | implement, test, sync, status |
| **품질** | review (basic / security / performance / patterns) |

### 라이프사이클 훅

| 훅 | 용도 |
|----|------|
| session-start | 컨텍스트 인식 재개 (레시피 상태 + 다음 단계 힌트 주입) |
| stop | 완료 게이트 (스펙, 테스트, 리뷰를 완료 전에 검증) |
| pre-compact | 컨텍스트 압축 전 작업 상태 스냅샷 |
| session-end | 세션 간 재개를 위한 작업 상태 영속화 |
| post-tool-use | 도구 사용 메트릭스 추적 (도구명, 파일, 성공/실패) |
| subagent-start/stop | 서브에이전트 라이프사이클 메트릭스 추적 |

### 메트릭스 & 비용 추정

```
forge stats
```

```
Project Overview
────────────────────────────────────────
  Recipes:     3 complete, 1 active, 4 total
  Sessions:    12 total, 5 compactions
  Models:      claude-opus-4-6, claude-sonnet-4-6

Estimated Cost
────────────────────────────────────────
  Total:       $4.52
  Input:       $1.23
  Output:      $2.89
```

세션별, 레시피별 토큰 추적 및 모델별 비용 추정. CSV(`--csv`) 또는 JSON(`--json`)으로 외부 분석 도구에 내보내기 가능.

### 상태 표시줄

```
forge v0.1.0 │ JWT auth │ implement 3/5 │ ctx 60%
```

Claude Code 상태 표시줄에서 레시피 진행 상황, 단계, 컨텍스트 사용량을 실시간으로 확인할 수 있습니다.

### 문서 시각화

```bash
forge graph              # 프로젝트 전체 문서 관계도
forge graph <recipe-id>  # 레시피별 문서 그래프
```

문서 의존성, 토론 결론, 검증 체인을 보여주는 mermaid 다이어그램을 생성합니다.

## 레시피 라이프사이클

각 레시피 타입은 고유한 스펙 단계를 가집니다. 코드를 생성하는 레시피는 모두 같은 구현 단계를 공유합니다.

### 스펙 단계 (레시피별)

**Blueprint** — 새 기능을 위한 전체 스펙:

```mermaid
flowchart LR
    DIS["탐색"] --> SC["범위"] --> R["조사"] --> W["와이어프레임"]
    W --> D["초안"] --> V["검증"]
    V -->|"이슈"| D
    V -->|"통과"| F["확정"]
```

**Fix** — 경량 진단:

```mermaid
flowchart LR
    DIAG["진단"] --> SPEC["수정 스펙"] --> F["확정"]
```

**Debug** — 다관점 근본 원인 분석:

```mermaid
flowchart LR
    BP["6개 블루프린트"] --> CROSS["교차 분석"] --> SPEC["수정 스펙"] --> F["확정"]
```

**Design** / **Analyze** — 스펙만, 구현 없음:

```mermaid
flowchart LR
    R["조사"] --> D["초안"] --> V["검증"]
    V -->|"이슈"| D
    V -->|"통과"| F["확정"]
```

### 구현 단계 (공통)

코드를 생성하는 모든 레시피는 `/forge-implement`를 통해 같은 구현 루프로 진입합니다:

```mermaid
flowchart LR
    F["확정된 스펙"] --> IMP["구현"] --> T["테스트"]
    T -->|"실패"| IMP
    T -->|"통과"| SIM["시뮬레이션"] --> RV["리뷰"] --> SY["동기화"] --> DONE["완료"]
```

## 아키텍처

**Go 바이너리** — 단일 정적 링크 바이너리 (~5ms 시작). 상태 관리, 완료 검증, 템플릿 배포, 메트릭스 추적. Go 외에 런타임 의존성이 없습니다.

**Claude Code 통합** — 21개 스킬이 레시피 프로토콜을, 8개 라이프사이클 훅이 세션 이벤트(재개, 완료 게이트, 메트릭스)를, 6개 규칙이 제약 조건을 처리합니다. 검증은 항상 별도 에이전트 컨텍스트에서 실행됩니다.

## 핵심 원칙

- **문서 먼저** — 코드가 아닌 스펙을 반복한다
- **자기 출력 검증 금지** — 검증은 별도 에이전트 컨텍스트에서
- **컨텍스트가 글루** — 스킬은 규칙 강제가 아닌 상황 인식 제공
- **Deviation = 후속 작업** — 스펙-코드 차이는 보고서이지 게이트가 아님
- **충돌 복원** — JSON을 통한 작업 상태 영속화; 세션 자동 재개
- **계층적 맵** — 가벼운 프로젝트 개요, 필요 시 상세 정보
- **빠름** — 단일 Go 바이너리, 런타임 의존성 제로, ~5ms 시작

## CLI

```
forge init [dir]              프로젝트 초기화 (스킬, 훅, 규칙 배포)
forge doctor [recipe-id]      건강 체크 (시스템, 레시피, 문서)
forge validate [recipe-id]    JSON 스키마 준수 확인
forge verify <file>           문서 일관성 검사, 레벨 평가
forge recipe status           활성 레시피 표시
forge recipe list             전체 레시피 목록
forge recipe create           새 레시피 생성
forge recipe log <id>         액션 / 단계 / 이터레이션 기록
forge recipe cancel           활성 레시피 취소
forge stats [recipe-id]       메트릭스 및 비용 추정 (--json, --csv)
forge graph [recipe-id]       문서 관계 시각화 (--all)
forge sync-check <id>         레시피 내 문서 동기화 확인
forge update                  바이너리 버전에 맞게 템플릿 업데이트
forge version                 바이너리 및 템플릿 버전 표시
```

## 요구 사항

- **Go** 1.22+ ([설치](https://go.dev/dl/))
- **Claude Code** ([설치](https://docs.anthropic.com/en/docs/claude-code))
- **OS**: macOS, Linux (Windows는 WSL 사용)

설치 후 `forge doctor`를 실행하여 환경을 확인하세요.

## 기여

기여를 환영합니다. 버그 보고나 기능 요청은 [이슈](https://github.com/imtemp-dev/claude-forge/issues)를 열어주세요.

```bash
# 개발 환경 설정
git clone https://github.com/imtemp-dev/claude-forge.git
cd claude-forge
make install          # 빌드 후 ~/.local/bin에 설치
go test -race ./...   # 테스트 실행
```

## 라이선스

MIT
