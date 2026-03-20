# bts — 방탄 기술 사양

[English](README.md) | [中文](README.zh.md) | [日本語](README.ja.md)

AI가 첫 번째 시도에서 동작하는 코드를 생성할 수 있도록 구현 스펙을 충분히 상세하게 만듭니다.

## 문제

```
대충 계획 → AI 코딩 → 버그 → 수정 → 버그 → 수정 → ... (N번 반복)
```

AI가 생성한 코드를 디버깅하는 데 대부분의 시간이 소모됩니다. 근본 원인: 스펙이 모호해서 AI가 추측했기 때문입니다.

## 해결책

```
스펙 → 검증 → 수정 → 검증 → ... → 완벽한 스펙 → AI 코딩 → 완료
```

코드가 아닌 **문서**를 반복합니다. 문서 변경은 비용이 없습니다 — 빌드, 테스트, 부작용 없음. 스펙이 완벽하면 AI가 최소한의 반복으로 코드를 생성합니다.

## 전체 라이프사이클

```mermaid
flowchart LR
    subgraph Blueprint
        S["범위 설정"] --> R["조사"] --> D["초안"] --> V["검증 루프"]
        V --> SIM["시뮬레이션"] --> DB["토론"] --> F["확정"]
    end
    subgraph Implement
        IMP["구현"] --> T["테스트"] --> SY["동기화"] --> ST["상태"]
    end
    F --> IMP
    ST --> DONE["완료"]
```

```mermaid
flowchart LR
    subgraph Fix
        DG["진단"] --> FS["수정 스펙"] --> SM["시뮬레이션"]
        SM --> ER["전문가 리뷰"] --> VR["검증"] --> IM["구현"] --> TE["테스트"]
    end
    TE --> FD["완료"]
```

```mermaid
flowchart LR
    subgraph Debug
        P["6개 관점"] --> CR["교차 분석"] --> HY["가설"]
        HY --> S2["시뮬레이션"] --> D2["토론"] --> V2["검증"] --> FM["final.md"]
    end
    FM --> IMP2["/implement"] --> DONE2["완료"]
```

bts는 **기획 → 구현 → 검증**을 하나의 자동화된 파이프라인으로 다룹니다.

## 설치

```bash
# 원라인 설치 (macOS / Linux)
curl -fsSL https://raw.githubusercontent.com/jlim/bts/main/install.sh | bash

# 또는 소스에서 빌드 (Go 1.22+)
git clone https://github.com/jlim/bts.git
cd bts
make install    # ~/.local/bin/bts에 설치
```

`~/.local/bin`이 PATH에 없으면 `.zshrc` 또는 `.bashrc`에 추가:
```bash
export PATH="$HOME/.local/bin:$PATH"
```

업데이트:
```bash
git pull && make install
```

## 빠른 시작

```bash
# 프로젝트 초기화
bts init .

# Claude Code 시작
claude

# 완벽한 스펙 생성
/recipe blueprint "OAuth2 인증 추가"

# 알려진 버그 수정
/recipe fix "로그인 bcrypt 해시 비교 실패"

# 원인 모르는 이슈 디버그
/recipe debug "5분 후 세션 끊김"

# 코드 품질 리뷰
/bts-review
/bts-review security src/auth/

# 프로젝트 건강 체크
bts doctor
```

## 레시피

| 레시피 | 용도 | 출력 |
|--------|------|------|
| `/recipe analyze` | 기존 시스템 이해 | Level 1 분석 문서 |
| `/recipe design` | 기능 설계 | Level 2 설계 문서 |
| `/recipe blueprint` | 전체 구현 스펙 | Level 3 스펙 → 코드 → 테스트 |
| `/recipe fix` | 알려진 버그 수정 (경량) | 수정 스펙 → 코드 → 테스트 |
| `/recipe debug` | 원인 모르는 버그 조사 | 6관점 분석 → 스펙 → 코드 |

## 스킬 (19개)

| 카테고리 | 스킬 |
|----------|------|
| **레시피** | blueprint, design, analyze, fix, debug |
| **검증** | verify, cross-check, audit, assess, sync-check |
| **분석** | research, simulate, debate, adjudicate |
| **구현** | implement, test, sync, status |
| **품질** | review (basic / security / performance / patterns) |

## 핵심 원칙

- **문서 먼저**: 코드가 아닌 스펙을 반복한다
- **자기 출력 검증 금지**: 검증은 별도 에이전트 컨텍스트에서
- **컨텍스트가 글루**: 스킬은 규칙 강제가 아닌 상황 인식 제공
- **Deviation = 후속 작업**: 스펙-코드 차이는 보고서이지 게이트가 아님
- **충돌 복원**: tasks.json + work-state.json으로 작업 상태 유지

## CLI

```
bts init [dir]              프로젝트 초기화
bts doctor [recipe-id]      레시피 건강 체크 (문서, 매니페스트, 플로우)
bts validate [recipe-id]    JSON 스키마 준수 확인
bts recipe status           활성 레시피 표시
bts recipe list             전체 레시피 목록
bts recipe log <id>         액션/단계/이터레이션 기록
bts recipe cancel           활성 레시피 취소
```

## 라이선스

MIT
