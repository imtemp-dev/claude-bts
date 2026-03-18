# bts — 스킬, 레시피, 에이전트

## 스킬 (개별 기능)

### Phase 1 스킬 (5개)

| 스킬 | 목적 | 수행 주체 |
|------|------|----------|
| `/verify` | 문서의 논리적 오류 검증. 모순, 비약, 잘못된 인과 관계 | 서브에이전트 (verifier) |
| `/cross-check` | 문서의 사실적 주장을 소스 코드와 대조. 파일/함수/타입 존재 여부 | 바이너리 (결정론적) + 서브에이전트 |
| `/audit` | 문서의 완결성 검토. 빠뜨린 시나리오, 미고려 edge case, 숨겨진 가정 | 서브에이전트 (auditor) |
| `/research` | 코드/문서/웹을 체계적으로 조사하여 구조화된 결과 생성 | 서브에이전트 (Explore) |
| `/debate` | 전문가 3명 페르소나로 라운드 토론. 상태 저장, 이어서 가능 | 메인 Claude (3 페르소나 순차 연기) |

### Phase 2 추가 스킬 (3개)

| 스킬 | 목적 | 수행 주체 |
|------|------|----------|
| `/flowcheck` | 문서에 기술된 호출 흐름을 실제 코드에서 추적하여 연결성 확인 | 바이너리 (호출 추적) + 서브에이전트 |
| `/visualize` | 문서의 흐름을 mermaid 다이어그램으로 변환. AI가 시각적으로 재검증 | Claude 직접 실행 |
| `/trace` | 특정 진입점에서 호출 체인을 **발견** (조사 목적. /flowcheck은 문서의 흐름이 맞는지 **검증**) | 바이너리 (grep/import 추적) |

---

## 에이전트

| 에이전트 | 역할 | 도구 제한 | 모델 |
|---------|------|----------|------|
| `verifier` | 논리적 일관성 전문 검증. 모순, 비약, 잘못된 인과 | Read, Grep, Glob | sonnet |
| `auditor` | 완결성/커버리지 전문 검토. 빠뜨린 것 찾기 | Read, Grep, Glob | sonnet |
| `cross-checker` | 사실 대조 전문. 문서 주장 vs 실제 코드 | Read, Grep, Bash(wc, grep, find) | sonnet |

모든 검증 에이전트는 **읽기 전용**. 문서를 수정하는 것은 메인 세션의 Claude만 한다.

**토론 구현**: `/debate`는 별도 서브에이전트가 아닌 **메인 Claude가 3개 페르소나를 순차적으로 연기**한다. 서브에이전트 간에는 컨텍스트가 공유되지 않아 "서로의 의견에 반응하는 토론"이 불가능하기 때문이다. 스킬 프롬프트가 페르소나 전환과 라운드 구조를 지시한다.

---

## 레시피 (스킬 조합)

레시피 = 문서를 특정 목적의 Level까지 끌어올리는 자동화된 워크플로우.

### Phase 1 레시피 (3개)

| 레시피 | 목적 | 최종 산출물 | 산출물 Level |
|--------|------|-----------|-------------|
| `/recipe analyze` | 기존 시스템/코드 분석 | 검증된 분석 문서 | Level 1 (이해) |
| `/recipe design` | 기능/시스템 설계 | 검증된 설계 문서 | Level 2 (설계) |
| `/recipe blueprint` | 설계를 구현 직전까지 구체화 | 구현 문서 (스캐폴딩 포함) | Level 3 (구현 직전) |

### Phase 2 레시피 (4개)

| 레시피 | 목적 | 최종 산출물 |
|--------|------|-----------|
| `/recipe debug` | 버그 근본 원인 분석 | 검증된 근본 원인 + 수정 방안 문서 |
| `/recipe decide` | 기술 선택 의사결정 | 검증된 의사결정 문서 (대안 비교, 근거) |
| `/recipe flow` | 특정 흐름 문서화 + 검증 | 검증된 흐름 문서 + mermaid 다이어그램 |
| `/recipe api` | API 설계/문서화 | 검증된 API 문서 (경로, 파라미터, 응답, 에러) |

### Phase 3 레시피 (2개)

| 레시피 | 목적 | 최종 산출물 |
|--------|------|-----------|
| `/recipe dataflow` | 데이터 파이프라인 문서화 | 검증된 데이터 흐름 문서 + 다이어그램 |
| `/recipe integration` | 시스템 간 연결점 문서화 | 검증된 통합 문서 |

### 레시피 실행 구조

모든 레시피는 동일한 기본 패턴을 따른다:

```
[조사] → [초안 작성] → [검증 루프] → [확정]

Phase 1 검증 루프 (정적 + 논리):
  cross-check (사실) → verify (논리) → audit (완결성)
  → 오류 있으면 → 수정 → 재검증 (최대 N회)
  → 모두 통과 → 확정
  → 수렴 불가 → 사람에게 물어봄

Phase 2 검증 루프 (+ 흐름):
  cross-check → verify → audit → flowcheck (흐름) → visualize (다이어그램)
  → Phase 1 루프에 흐름/시각적 검증이 추가됨
```

Phase 1 레시피는 flowcheck/visualize 없이 동작한다. Phase 2에서 이 스킬들이 추가되면 검증 루프가 강화된다.

### 킬러 레시피: `/recipe blueprint`

Level 2 설계 문서를 Level 3 구현 문서로 변환하는 레시피.

**입력**: Level 2 설계 문서 (있으면 사용, 없으면 Step 1에서 자동 생성)
- 있을 때: "OAuth2 인증. Passport.js 사용. 세션 기반." → Step 1에서 이를 기반으로 조사
- 없을 때: 자연어 요구사항만 → Step 1에서 조사 후 Step 2에서 Level 2 + Level 3 초안 동시 생성

```
Step 1: Research (조사)
  ├─ /research 스킬 호출 (내부: Agent(Explore) spawn)
  ├─ 기존 코드 구조, 연결점, 의존성, 패턴 조사
  └─ .bts/state/{id}/01-research.md 저장

Step 2: Draft (Level 3 초안 작성)
  ├─ 메인 Claude가 작성:
  │   파일 경로, 함수 시그니처, 타입, 연결점, edge case, 스캐폴딩
  └─ .bts/state/{id}/02-draft.md 저장

Step 3: Verify Loop (검증 루프, 자동 반복)
  ├─ /cross-check → 사실 대조 (바이너리 + 서브에이전트)
  ├─ /verify → 논리적 일관성 (서브에이전트)
  ├─ /audit → 완결성 검토 (서브에이전트)
  ├─ (Phase 2+) /flowcheck → 호출 체인 연결성 확인
  ├─ (Phase 2+) /visualize → 다이어그램으로 누락 경로 감지
  ├─ 결과 → Bash: `bts recipe log {id} --iteration N ...`
  ├─ critical/major > 0 → 문서 수정 → Step 3 재실행 (최대 N회)
  └─ critical=0, major=0 → Step 4로

Step 4: Decision (필요 시만)
  ├─ 불확실한 선택이 있으면 → /debate 호출
  ├─ 방향 결정 필요 → [DECISION REQUIRED] → 사람 개입
  ├─ 확정 후 문서 반영 → iteration 카운터 리셋 → Step 3 재실행
  └─ verify-log는 유지 (이전 이터레이션 이력 보존)

Step 5: Finalize (완료)
  ├─ .bts/state/{id}/final.md로 최종 문서 확정
  └─ <bts>DONE</bts> 출력 → Stop Hook이 verify-log 최종 확인

산출물 예시:
  src/auth/oauth.ts:
    export function configureOAuth(app: Express): void
    - passport.use(new OAuth2Strategy({
        clientID: process.env.OAUTH_CLIENT_ID,
        clientSecret: process.env.OAUTH_CLIENT_SECRET,
        callbackURL: "/auth/callback"
      }))
    - app.get("/auth/callback", handleCallback)
    - handleCallback: src/auth/routes.ts:28
    - 에러: InvalidGrantError → 401, redirect /login
    - 에러: TokenExpiredError → refresh attempt → 실패 시 401
    - 세션: src/server/session.ts:configureSession() 재사용
    - 테스트:
      - POST /auth/login → 302 redirect to OAuth provider
      - GET /auth/callback?code=valid → 200, session created
      - GET /auth/callback?code=expired → 401
      - GET /auth/callback?state=invalid → 403
```

이 문서를 Opus에 넣으면, Opus는 추측 없이 구현한다.

---

## 수렴 루프 상세

```
Iteration 1:
  cross-check: "src/auth/routes.ts:28에 handleCallback이 없음" (critical)
  verify: "세션 만료 처리 로직이 에러 케이스에 빠져있음" (major)
  audit: "CSRF 보호가 누락됨" (major)
  → Claude가 문서 수정

Iteration 2:
  cross-check: 통과
  verify: "CSRF state 파라미터의 생성/검증 로직이 모호함" (minor)
  audit: 통과
  → Claude가 문서 수정

Iteration 3:
  cross-check: 통과
  verify: 통과
  audit: 통과
  → 수렴. 최종 문서 확정.

상태 저장 (verify-log.jsonl):
  {"iteration":1,"critical":1,"major":2,"minor":0,"status":"continue"}
  {"iteration":2,"critical":0,"major":0,"minor":1,"status":"continue"}
  {"iteration":3,"critical":0,"major":0,"minor":0,"status":"converged"}
```

수렴 기준:
- critical: 0 (필수)
- major: 0 (필수)
- minor: 허용 (info와 함께 최종 문서에 주석으로 포함)

---

## 토론 시스템

### 실행

```
/debate "OAuth2 vs JWT vs 커스텀 토큰"

Round 1: 각 전문가 입장 표명
  보안 전문가: "OAuth2가 표준이고 SOC2 호환"
  성능 전문가: "JWT가 stateless라 확장성 우수"
  운영 전문가: "커스텀은 유지보수 부담, OAuth2 권장"

Round 2: 반론
  성능: "OAuth2는 매 요청 세션 조회 필요"
  보안: "JWT는 토큰 무효화가 어려움"
  운영: "둘 다 일리 있으나 팀 역량 고려하면 OAuth2"

Round 3: 합의 도출
  결론: "OAuth2 채택. 성능 우려는 Redis 세션 캐시로 완화"
  조건: "초당 1000 요청 초과 시 JWT 재검토"
```

### 상태 관리

```
.bts/state/debates/{id}/
  ├── debate.json       { topic, rounds: 3, conclusion, decided: true }
  ├── round-1.md
  ├── round-2.md
  └── round-3.md
```

- `bts debate list` → 저장된 토론 목록
- `bts debate resume <id>` → 이전 토론 이어서 (새 정보 추가 후 추가 라운드)
- `bts debate export <id>` → 마크다운으로 내보내기

### 교착 시 사람 개입

```
Round 3 후에도 합의 불가:
  [DEBATE DEADLOCK]
  보안: OAuth2 고수
  성능: JWT 고수
  → 사람에게 최종 결정 요청
  → 사람 결정 후 → 결론 반영 → 레시피 계속 진행
```
