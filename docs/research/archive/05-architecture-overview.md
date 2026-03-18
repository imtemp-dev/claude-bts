# Context Sync — Phase 1 아키텍처

> 세션에서 decisions / constraints / explorations를 추출하여 보존한다.
> 3개 hook, 1개 MCP 도구, 1개 JSONL 파일. ~800줄.

---

## 시스템 전체

```
Claude Code
  │
  ├─ [SessionStart hook]     ← 이전 observations 주입
  ├─ ... 평소대로 개발 ...
  ├─ [PreCompact hook]       ← 컨텍스트 압축 전 추출 (긴 세션 보호)
  ├─ [Stop hook]             ← 세션 종료 시 추출
  │
  └─ [MCP search tool]      ← 수동 검색 (선택적)

데이터:
  ~/.context-sync/
    └── <project-hash>/
        └── observations.jsonl    ← 한 줄 = 하나의 observation
```

**프로세스 1개 (Hook), 파일 1개 (JSONL), 서버 없음, DB 없음.**

---

## Hook 설계

### hooks.json

```json
{
  "hooks": {
    "SessionStart": [{
      "matcher": "startup",
      "command": "node dist/hooks/inject.js",
      "timeout": 10
    }],
    "PreCompact": [{
      "matcher": "*",
      "command": "node dist/hooks/extract.js",
      "timeout": 30
    }],
    "Stop": [{
      "matcher": "*",
      "command": "node dist/hooks/extract.js",
      "timeout": 30
    }]
  }
}
```

3개 hook. SessionStart는 주입만 (빠름, 10초). PreCompact/Stop은 추출 (LLM 호출, 30초).

### SessionStart: inject.js

```
stdin (JSON: session_id, cwd, ...)
  → project 식별 (cwd → hash)
  → observations.jsonl 읽기
  → 현재 작업 파일과 관련된 observations 필터링
    - git diff --name-only HEAD~5 로 최근 수정 파일 목록
    - observations의 files 필드와 교차
  → 관련 observations을 마크다운으로 포맷
  → stdout: { hookSpecificOutput: { additionalContext: "..." } }
```

주입 예시:
```markdown
## Context Sync — 이 코드에 대해 알아야 할 것

### Decisions
- **타이핑 후 1.2초 딜레이** (SceneCard.tsx, 3일 전)
  수동 넘기기는 플로우를 방해하므로 거부됨

### Constraints
- **Temporal Workflow 내 Date.now()/fetch() 금지** (worker/, 1주 전)
  replay 충돌 방지. 외부 호출은 Activity로 분리해야 함
```

### PreCompact / Stop: extract.js

```
stdin (JSON: session_id, cwd, transcript_path?, ...)
  → project 식별
  → 이전 추출 시점 이후의 새 대화 내용 수집
    - transcript JSONL에서 최근 user/assistant 메시지 읽기
    - 또는 stdin에 포함된 대화 요약 사용
  → LLM에 추출 프롬프트 전송 (검증된 프롬프트, Round 2)
  → XML 응답 파싱
  → content-hash dedup 체크 (SHA256 + 30초 윈도우)
  → observations.jsonl에 append
  → stdout: { hookSpecificOutput: {} }
```

---

## 데이터 형식

### observations.jsonl

한 줄 = 하나의 JSON 객체:

```json
{
  "id": "a1b2c3d4",
  "type": "decision",
  "status": "adopted",
  "title": "Added 1.2s delay after typing completion",
  "narrative": "Manual page advancement was rejected because it disrupts reading flow. A timed delay provides a natural pause.",
  "facts": ["1.2s sleep after onTypingComplete", "cancelRef check after sleep for safety"],
  "concepts": ["UX", "readability", "auto-transition"],
  "files": [{"path": "src/components/SceneCard.tsx", "action": "edit"}],
  "rationale": "Enhance UX by giving readers time to absorb text before scene change",
  "alternatives": [
    {"option": "No delay", "rejected": "Not enough reading time"},
    {"option": "Manual button", "rejected": "Disrupts flow"}
  ],
  "sessionId": "e85214a0-...",
  "branch": "feature/scene-navigation",
  "commitSha": "abc1234",
  "timestamp": "2026-03-18T09:00:00Z",
  "contentHash": "f7a3b2c1d9e8f6a5"
}
```

### 타입별 필드

| 필드 | decision | constraint | exploration | 공통 |
|------|----------|-----------|-------------|------|
| type | O | O | O | O |
| title | O | O | O | O |
| narrative | O | O | O | O |
| facts[] | O | O | O | O |
| files[] | O | O | O | O |
| concepts[] | O | O | O | O |
| rationale | **O** | | | |
| alternatives[] | **O** | | | |
| source | | **O** | | |
| impact | | **O** | | |
| approach | | | **O** | |
| outcome | | | **O** | |
| status | | | **O** (adopted/modified/abandoned) | O |

---

## MCP 도구

### search

```
도구 이름: context_search
설명: "Search preserved decisions, constraints, and explorations"
파라미터: { query: string, type?: "decision"|"constraint"|"exploration", limit?: number }
```

구현: observations.jsonl을 읽고, title + narrative + facts에서 query 매칭. type 필터 적용. 결과를 마크다운 테이블로 반환.

Phase 1에서는 **substring match**로 충분하다. 저장되는 것이 전부 고품질(노이즈 없음)이므로.

---

## 디렉토리 구조

```
context-sync/
├── package.json                    # tsx, typescript, esbuild
├── tsconfig.json
├── plugin/
│   ├── hooks/hooks.json            # 3 hooks
│   └── .claude-plugin/plugin.json
├── src/
│   ├── hooks/
│   │   ├── inject.ts               # SessionStart — observations 주입
│   │   └── extract.ts              # PreCompact/Stop — observations 추출
│   ├── extraction/
│   │   ├── types.ts                # Observation, Turn 등 타입 (기존)
│   │   ├── prompts.ts              # LLM 추출 프롬프트 (기존, 검증됨)
│   │   ├── parser.ts               # XML → Observation 파서 (기존, 검증됨)
│   │   └── llm-client.ts           # Gemini/OpenAI 클라이언트 (기존)
│   ├── storage/
│   │   ├── observations.ts         # JSONL read/append/search
│   │   └── dedup.ts                # content-hash 중복 방지
│   ├── git/
│   │   └── git-info.ts             # branch, commit SHA, recent files
│   ├── inject/
│   │   └── context-builder.ts      # observations → 주입 마크다운 생성
│   └── shared/
│       ├── paths.ts                # ~/.context-sync/ 경로
│       └── privacy.ts              # <private> 태그 제거
├── scripts/                        # 프로토타입 스크립트 (기존)
└── data/                           # 테스트 데이터 (기존)
```

**파일 12개. 예상 ~800줄.** (extraction/ 4개 파일은 이미 작성 완료)

---

## 기존 코드 재사용

프로토타입에서 이미 검증된 코드:

| 파일 | 줄 수 | 상태 |
|------|-------|------|
| `src/extraction/types.ts` | 96 | 완료, 재사용 |
| `src/extraction/prompts.ts` | 130 | 완료, Round 2 튜닝 적용 |
| `src/extraction/parser.ts` | 108 | 완료, 재사용 |
| `src/extraction/llm-client.ts` | 78 | 완료, Gemini+OpenAI 지원 |
| **소계** | **412** | **이미 50% 완료** |

새로 작성 필요:

| 파일 | 예상 줄 수 | 역할 |
|------|-----------|------|
| `src/hooks/inject.ts` | ~80 | SessionStart 주입 |
| `src/hooks/extract.ts` | ~100 | PreCompact/Stop 추출 |
| `src/storage/observations.ts` | ~60 | JSONL read/append/search |
| `src/storage/dedup.ts` | ~30 | SHA256 dedup |
| `src/git/git-info.ts` | ~40 | branch, commit, recent files |
| `src/inject/context-builder.ts` | ~60 | 마크다운 포맷 |
| `src/shared/paths.ts` | ~15 | 경로 상수 |
| `src/shared/privacy.ts` | ~20 | 태그 스트리핑 |
| `plugin/hooks/hooks.json` | ~20 | Hook 설정 |
| **소계** | **~425** | |
| **총계** | **~837** | |

---

## 검증 방법

```bash
# 1. 빌드
npm run build

# 2. Claude Code 플러그인으로 설치
# plugin/ 디렉터리를 ~/.claude/plugins/에 링크

# 3. 새 세션 시작 → SessionStart hook 실행 확인
# (처음에는 observations가 없으므로 빈 주입)

# 4. 코드 작업 수행 (Edit, Write 등)

# 5. /compact 또는 세션 종료 → extract hook 실행 확인
# observations.jsonl에 새 항목 추가 확인

# 6. 새 세션 시작 → 이전 observations 주입 확인
# "이 코드에 대해 알아야 할 것:" 메시지 표시

# 7. MCP search 도구로 검색 테스트
```

---

## 이전 아키텍처(05-architecture-overview-v1)와의 차이

| 항목 | v1 (이전) | v2 (현재) |
|------|----------|----------|
| 프로세스 | Hook + Worker + MCP Server (3개) | **Hook만 (1개)** |
| 저장소 | SQLite WAL + FTS5 + Vector | **JSONL 파일** |
| 검색 | RRF + Intent + 3-layer | **grep** |
| 추출 시점 | 매 도구 실행 (PostToolUse) | **세션 끝 (1회)** |
| 파일 수 | ~40 | **~12** |
| 코드 규모 | 5,000-7,000줄 | **~800줄** |
| 의존성 | better-sqlite3, express, @xenova/transformers | **tsx만** |
| 캡처 대상 | 모든 도구 실행 | **decisions/constraints/explorations만** |
