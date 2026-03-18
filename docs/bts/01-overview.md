# bts — Bulletproof Technical Specification

구현 문서(spec)의 완성도를 극도로 높여서, AI가 한 번에 높은 정확도의 코드를 생성할 수 있게 만드는 도구.

## 문제

AI 코딩 도구(Claude Code, Cursor 등)로 개발할 때 가장 비싼 루프:

```
대충 계획 → AI에게 구현 시키기 → 버그 → 수정 → 버그 → 수정 → ... (N회)
```

코드 수준에서 반복하면 빌드/테스트/사이드이펙트가 매번 따라온다. 비싸다.

## 해결

**반복의 위치를 코드에서 문서로 옮긴다.**

```
spec 작성 → 검증 → 수정 → 검증 → ... → 완성된 spec → AI → 높은 정확도의 코드
```

문서에서 반복하면 텍스트 수정뿐이다. 빌드도 테스트도 사이드이펙트도 없다. 코드 반복보다 상당히 저렴하다.

핵심: Opus 같은 모델은 정확하고 구체적인 구현 문서가 있으면 경험적으로 매우 높은 완성도로 코드를 생성한다. 문서의 품질이 곧 코드의 품질이다.

## 문서 수준 정의

| Level | 이름 | 내용 | 코드 생성 정확도 |
|-------|------|------|----------------|
| 1 | 이해 | 현재 시스템 구조, 파일 위치, 의존성 | 불가 |
| 2 | 설계 | 무엇을 만들 것인지, 컴포넌트, 데이터 흐름 | ~60-70% |
| 3 | 구현 직전 | 파일 경로, 함수 시그니처, 타입, 연결점, edge case, 스캐폴딩 | **매우 높음** |

bts의 최종 목표는 spec을 **Level 3**까지 끌어올리는 것이다. Level 1(분석)과 Level 2(설계)는 Level 3에 도달하기 위한 전 단계이며, 각 단계에 대응하는 레시피가 있다: `/recipe analyze` (L1), `/recipe design` (L2), `/recipe blueprint` (L3).

전체 스킬 8개, 레시피 9개, 에이전트 3개로 구성된다. (상세: 03-skills-and-recipes.md)

Level 3 문서의 예:
```
src/auth/oauth.ts 생성:
  - export function configureOAuth(app: Express): void
  - 파라미터: app (src/server/app.ts:14의 Express instance)
  - passport.use(new OAuth2Strategy({...}))
  - 콜백: /auth/callback → src/auth/routes.ts:handleCallback()
  - 에러: InvalidGrantError → 401 + redirect to /login
  - 세션: express-session (기존 src/server/session.ts 재사용)
  - 테스트 시나리오:
    - happy: valid code → token → session
    - error: expired code → 401
    - error: invalid state → 403
```

이 문서를 Opus에 넣으면 추측 없이 구현한다.

## 4단계 검증 체계

문서를 Level 3까지 올리기 위해 4가지 검증을 반복한다:

**1. 정적 검증 (바이너리, 결정론적)**
- 파일이 존재하는가? (fs.Stat)
- 함수/타입명이 소스에 있는가? (grep)
- 라인 수가 맞는가? (wc -l)
- import 경로가 유효한가?
- 의존성이 충돌하지 않는가? (package.json 파싱)

**2. 흐름 검증 (바이너리 + 서브에이전트)**
- A→B→C 호출 체인이 코드에 실제 존재하는가?
- 데이터 흐름이 연결되는가? (입력→변환→출력)
- 에러 경로가 처리되는가?
- 상태 전이가 빠짐없는가?

**3. 논리적 검증 (서브에이전트)**
- 결론이 근거에서 도출되는가?
- 문서 내 모순이 없는가?
- 빠뜨린 시나리오/edge case가 없는가?
- 숨겨진 가정이 없는가?

**4. 시각적 검증 (AI가 다이어그램 생성 → AI가 검증)**
- 흐름을 mermaid 다이어그램으로 변환
- 다이어그램에서 구조적 빈틈 감지
- 텍스트로 보면 놓치는 것을 시각적으로 포착

## 자동 실행, 예외적 사람 개입

기본: 모든 검증과 수정을 AI가 자동 반복한다. 사람은 관여하지 않는다.

사람이 개입하는 순간 (3가지만):
1. **방향 결정** — "OAuth2로 갈까 JWT로 갈까?" 기술/비즈니스 선택
2. **수렴 불가** — 검증 루프 N회 반복했으나 동일 오류 반복
3. **토론 교착** — 전문가 토론에서 합의 불가

## moai-adk와의 관계

```
bts:   요구사항 → 완성도 높은 구현 spec (Level 3)
moai:  구현 spec → 코드 (TRUST 5, TDD/DDD)
```

bts가 만든 spec이 moai의 SPEC 입력이 될 수 있다. 각자 영역이 명확하다:
- bts = spec 품질
- moai = 코드 품질

## 아키텍처 요약

Go 싱글 바이너리 (moai-adk 패턴):
- `bts init` → .claude/에 스킬/에이전트/훅/규칙 배포, .bts/에 설정/상태
- `bts hook <event>` → Claude Code lifecycle 이벤트 처리
- `bts verify <file>` → 결정론적 팩트 체크
- 스킬/레시피/에이전트는 마크다운 파일 (Claude Code가 읽음)
- 검증 엔진은 Go 코드 (결정론적 확인)
- 상태는 JSON/JSONL 파일 (세션 경계 초월, resume 가능)
