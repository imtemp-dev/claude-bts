# Context Sync — Extraction Prototype 준비

## 1. 트랜스크립트 현황

### 사용 가능한 세션 데이터

```
총 73개 세션, 다양한 프로젝트
```

| 프로젝트 | 세션 수 | 가장 큰 세션 | 특징 |
|----------|---------|-------------|------|
| mydream (Next.js 프론트엔드) | 31 | 728 prompts, 637 tools | UI 컴포넌트 구현, 디자인 개선 |
| mydream-backend (tRPC + Temporal) | 2 | 44 prompts, 417 tools | 백엔드 스캐폴딩, API 구현 |
| mydream-ai-ml | 다수 | 6,193줄 | AI/ML 파이프라인 |
| ssda-api | 1 | 532 prompts, 2,556 tools | 대규모 API 개발 |
| context-sync (현재 프로젝트) | 1 | 27 prompts, 216 tools | 리서치 + 문서 작성 |

### 트랜스크립트 형식 (JSONL)

각 줄은 하나의 이벤트. 주요 타입:

```jsonl
// 1. user 메시지
{"type":"user","message":{"role":"user","content":"구현을 시작하자"},"sessionId":"...","cwd":"..."}

// 2. assistant 메시지 (tool_use 포함)
{"type":"assistant","message":{"role":"assistant","content":[
  {"type":"text","text":"파일을 수정하겠습니다."},
  {"type":"tool_use","id":"toolu_xxx","name":"Edit","input":{"file_path":"...","old_string":"...","new_string":"..."}}
]}}

// 3. tool_result (user 메시지 안에 포함)
{"type":"user","message":{"content":[
  {"type":"tool_result","tool_use_id":"toolu_xxx","content":"File updated successfully"}
]}}

// 4. progress (다수, 무시 가능)
{"type":"progress","content":{"type":"tool_use","name":"Edit",...}}

// 5. file-history-snapshot (파일 상태 스냅샷)
{"type":"file-history-snapshot","snapshot":{...}}
```

### 통계 (context-sync 세션 기준)

| 타입 | 수 | 비율 |
|------|-----|------|
| progress | 4,328 | 87.7% ← 대부분 노이즈 |
| assistant | 305 | 6.2% |
| user | 232 | 4.7% |
| file-history-snapshot | 46 | 0.9% |
| system | 28 | 0.6% |

**핵심 관찰**: 전체의 88%가 progress(스트리밍 중간 상태)이며 무시해야 한다. 실제 의미 있는 데이터는 user + assistant의 약 12%.

---

## 2. Extraction 테스트 설계

### 2.1 입력: 대화 턴 (Conversation Turn)

하나의 대화 턴 = user prompt + assistant의 tool_use들 + tool_result들 + assistant의 최종 text response

```
Turn = {
  userPrompt: string,           // 사용자 요청
  toolCalls: [{                 // 도구 사용 목록
    name: string,               // Edit, Bash, Write, Read, ...
    input: object,              // 도구 입력
    result: string,             // 도구 결과
  }],
  assistantResponse: string,    // 최종 텍스트 응답
  timestamp: string,
  sessionId: string,
  project: string,
}
```

### 2.2 출력: 구조화된 Observation

```xml
<observation>
  <type>decision|constraint|exploration|discovery</type>
  <status>adopted|modified|abandoned</status>
  <title>간결한 제목 (한 줄)</title>
  <narrative>무엇이 일어났고 왜 중요한지 설명</narrative>
  <facts>
    <fact>구체적인 사실 1</fact>
    <fact>구체적인 사실 2</fact>
  </facts>
  <concepts>
    <concept>관련 키워드</concept>
  </concepts>
  <files>
    <file path="src/auth/middleware.ts" action="edit"/>
  </files>
  <!-- decision일 때 -->
  <rationale>이 선택의 이유</rationale>
  <alternatives>
    <alt option="다른 옵션" rejected="거부 이유"/>
  </alternatives>
  <!-- constraint일 때 -->
  <source>제약의 출처</source>
  <impact>제약의 영향</impact>
  <!-- exploration일 때 -->
  <approach>시도한 접근</approach>
  <outcome>결과</outcome>
  <abandonment_reason>포기 이유 (status=abandoned일 때)</abandonment_reason>
</observation>
```

### 2.3 테스트 케이스 선정

10개 대화 턴을 다음 기준으로 선정:

| # | 프로젝트 | 특성 | 기대 추출 타입 |
|---|----------|------|---------------|
| 1 | mydream | UI 컴포넌트 신규 생성 (Write) | discovery |
| 2 | mydream | 디자인 개선 요청 → 코드 수정 (Edit) | decision (디자인 선택) |
| 3 | mydream | 타이밍 버그 수정 (Edit) | discovery + constraint (타이밍 제약) |
| 4 | mydream | "디자인 스킬 사용해서 검토" → 리팩터 | exploration |
| 5 | mydream-backend | 스캐폴딩 구현 시작 (Write 대량) | decision (아키텍처 선택) |
| 6 | mydream-backend | git 브랜치 작업 (Bash) | low-signal (필터링 대상) |
| 7 | ssda-api | API 엔드포인트 구현 (Edit) | decision + constraint |
| 8 | context-sync | 경쟁 분석 문서 작성 (Agent) | discovery |
| 9 | mydream | "서버 재시작했어?" (짧은 질문) | low-signal (필터링 대상) |
| 10 | mydream | 선택지/버튼 UX 개선 (Edit 다수) | decision (UX 선택) |

### 2.4 평가 기준

각 추출 결과를 5점 척도로 평가:

| 기준 | 5 (우수) | 3 (보통) | 1 (실패) |
|------|---------|---------|---------|
| **타입 정확성** | 올바른 type 분류 | 근접하지만 부정확 | 완전히 잘못됨 |
| **제목 품질** | 핵심을 한 줄로 포착 | 모호하거나 너무 길음 | 무의미 |
| **facts 가치** | 나중에 검색할 만한 구체적 사실 | 맞지만 검색 가치 낮음 | 잘못되거나 없음 |
| **narrative 품질** | "왜"를 설명 | "무엇을"만 설명 | 파싱 실패 |
| **추가 필드** | rationale/alternatives/approach 등이 정확 | 있지만 피상적 | 없거나 잘못됨 |
| **Low-signal 감지** | 노이즈를 정확히 식별 | 일부 누락 | 유의미한 턴을 노이즈로 분류 |

**합격 기준**: 10개 중 7개 이상에서 평균 3.5점 이상이면 Phase 1 진행 가능.

---

## 3. 실행 방법

### 3.1 턴 추출 스크립트

트랜스크립트 JSONL에서 대화 턴을 추출하는 스크립트가 필요:

```
input: session.jsonl
output: turns.json (대화 턴 배열)

로직:
1. progress, file-history-snapshot 무시
2. user(text) → assistant(tool_use*) → user(tool_result*) → assistant(text) 체인 추적
3. 각 체인을 하나의 Turn으로 패킹
4. Read/Glob/Grep 단독 사용은 low-signal 후보로 마킹
```

### 3.2 Extraction 프롬프트

LLM에 전달할 프롬프트 템플릿:

```
시스템:
  "You are a code session observer. You analyze developer-AI interactions
   and extract structured observations. You identify decisions made,
   constraints discovered, approaches explored, and key discoveries."

사용자:
  "[Turn Data]
   User prompt: {userPrompt}
   Tools used: {toolCalls 요약}
   Assistant response: {assistantResponse}

   Analyze this interaction and produce ONE observation in XML format.
   If this is low-signal (routine file read, build command, simple query),
   respond with <low_signal reason="..."/>

   Otherwise, produce:
   <observation>
     <type>decision|constraint|exploration|discovery</type>
     ...
   </observation>"
```

### 3.3 평가 루프

```
for each turn in selected_10_turns:
  1. 턴 데이터 준비 (트랜스크립트에서 추출)
  2. Extraction 프롬프트에 턴 데이터 삽입
  3. LLM 호출 (Gemini Flash Lite → 무료)
  4. XML 파싱 시도
  5. 수동 평가 (5점 척도)
  6. 결과 기록
```

---

## 4. 즉시 해야 할 작업

| # | 작업 | 산출물 |
|---|------|--------|
| 1 | 턴 추출 스크립트 작성 | `scripts/extract-turns.ts` |
| 2 | 10개 세션에서 대표 턴 선정 | `data/test-turns.json` |
| 3 | Extraction 프롬프트 작성 | `src/extraction/prompts.ts` (초안) |
| 4 | XML 파서 작성 | `src/extraction/parser.ts` (초안) |
| 5 | 평가 실행 + 결과 기록 | `docs/research/07-extraction-results.md` |

이 5단계가 완료되면 go/no-go 결정을 내릴 수 있다.
