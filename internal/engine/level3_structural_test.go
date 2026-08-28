package engine

import (
	"strings"
	"testing"
)

// A blueprint that carries what only a document can carry: invariants
// with owners, the shape crossing a boundary, an order that cannot be
// undone, a falsifier per invariant, and what is still open.
const skeletonDoc = "# Blueprint: cover image\n\n" +
	"## 1. What ships\n" +
	"Book captures show a real cover. Scans stay on the gradient — that is P2.\n\n" +
	"## 2. Invariants and owners\n" +
	"| ID | Statement | Owner |\n" +
	"|---|---|---|\n" +
	"| INV-001 | a stored cover is `''` or an https URL on an allowed host | `backend/supabase/migrations/0026_cover.sql` |\n" +
	"| INV-002 | absence is `''` in storage and nil in the client | `ios/App/Services/CommunityModels.swift` |\n\n" +
	"## 3. Wire contract\n" +
	"| Layer | Name | Shape |\n" +
	"|---|---|---|\n" +
	"| column | `cover_url` | `text not null default ''` |\n" +
	"| wire | `coverUrl` | `string \\| undefined` |\n" +
	"| derived | `coverImageURL` | `URL?` |\n\n" +
	"## 4. Units and order\n" +
	"`wireframe.md` section 4 is authoritative. Not copied here.\n\n" +
	"## 5. Irreversible order and rollback\n" +
	"1. S-1 backend constant and migration, same commit.\n" +
	"2. S-2 apply locally and probe.\n" +
	"3. S-3 backend wiring, before the app ships.\n" +
	"Shipping the app first makes every publish a 400. rollback: drop the column.\n\n" +
	"## 6. Falsifiers\n" +
	"| Invariant | Falsifier |\n" +
	"|---|---|\n" +
	"| INV-001 | `backend/test/migration-guards.spec.ts` — a disallowed host dies |\n" +
	"| INV-002 | `ios/App-Tests/CoverTests.swift` — omitted and empty agree |\n\n" +
	"## Known Uncertainties\n\n" +
	"### U-001: whether the migration file applies atomically\n" +
	"Opens-with: `supabase db push --local` with a deliberate failure inserted.\n"

func TestSkeletonReachesLevel3(t *testing.T) {
	got := assessLevel(skeletonDoc)
	if got.Level < 3.0 {
		t.Errorf("skeleton level = %.3f, want 3.0; unmet: %v", got.Level, got.Missing)
	}
	if lines := strings.Count(skeletonDoc, "\n"); lines > 40 {
		t.Errorf("the skeleton is %d lines; this test is worthless if it is not short", lines)
	}
}

// The defect this whole change exists to remove: the score used to rise
// with length, because every criterion was a keyword threshold over an
// unbounded text. Repetition adds length and no structure, so it must
// not move the level.
func TestLengthAloneDoesNotRaiseTheLevel(t *testing.T) {
	short := assessLevel(skeletonDoc)
	inflated := assessLevel(strings.Repeat(skeletonDoc+"\n\n", 12))
	if inflated.Level > short.Level {
		t.Errorf("repeating the document raised the level %.3f -> %.3f",
			short.Level, inflated.Level)
	}
}

func TestInvariantsCarryNeedsEveryInvariantPaired(t *testing.T) {
	// INV-002 has no owner on its row.
	partial := "| INV-001 | x | `a/b.ts` |\n| INV-002 | y | TBD |\n"
	if invariantsCarry(partial, lineNamesOwner) {
		t.Error("one unowned invariant should fail the criterion")
	}
	full := partial + "| INV-002 | y | `c/d.ts` |\n"
	if !invariantsCarry(full, lineNamesOwner) {
		t.Error("every invariant owned should pass")
	}
	// No invariants at all is not a pass — there is nothing to have owned.
	if invariantsCarry("no invariants here", lineNamesOwner) {
		t.Error("a document with no invariants must not satisfy the criterion")
	}
}

// The old keyword list named one stack (typescript, python, go, react,
// node, express, django, postgresql, redis), so a Swift and Supabase
// project could not satisfy it at any length by describing itself
// accurately.
// A cross-reference line that lists every invariant and happens to
// contain the word "test" marked seven of nine covered on one measured
// draft. A falsifier has to name something that can go red.
func TestFalsifierNeedsANamedArtifact(t *testing.T) {
	index := "INV-001 and INV-002 are re-stated in their owning sections (tests too)\n"
	if invariantsCarry(index, lineNamesFalsifier) {
		t.Error("a falsifier word with no named artifact should not count")
	}
	named := "| INV-001 | `backend/test/guards.spec.ts` |\n| INV-002 | `ios/CoverTests.swift` — decodes |\n"
	if !invariantsCarry(named, lineNamesFalsifier) {
		t.Error("a falsifier word plus a named artifact should count")
	}
	// An owner row is not a falsifier row: it names a file, not a red light.
	owner := "| INV-001 | statement | `backend/src/community.service.ts` |\n"
	if invariantsCarry(owner, lineNamesFalsifier) {
		t.Error("naming the owning file must not double as naming a falsifier")
	}
}

// Leading zeros are formatting, not identity.
func TestFalsifierCoverageReportsIDsAsWritten(t *testing.T) {
	got := FalsifierCoverage("| INV-007 | statement | `a/b.ts` |\n")
	if len(got) != 1 {
		t.Fatalf("want 1 uncovered invariant, got %d", len(got))
	}
	if got[0].ID != "INV-007" {
		t.Errorf("ID = %q, want INV-007 as the document writes it", got[0].ID)
	}
	if FalsifierCoverage("a document declaring no invariants") != nil {
		t.Error("no invariants means nothing to cover, not a violation")
	}
}

func TestTechStackFromFileExtensions(t *testing.T) {
	swiftAndSQL := "we touch `ios/App/Cover.swift` and `db/0026_cover.sql`"
	if !hasTechStack(swiftAndSQL) {
		t.Error("two source extensions should pin the stack")
	}
	if hasTechStack("a document naming `only/one.swift` and nothing else") {
		t.Error("a single extension is a file, not a stack")
	}
	if !hasTechStack("built on Supabase") {
		t.Error("a named technology should still count")
	}
}

func TestDelegationSatisfiesUpstreamCriteria(t *testing.T) {
	doc := "The flow and the decomposition are in `wireframe.md` section 3."
	if !checkCriterion(doc, "data_flow_defined") {
		t.Error("naming the document that holds the flow should satisfy the criterion")
	}
	if checkCriterion("no siblings named, and no arrows", "data_flow_defined") {
		t.Error("neither a flow nor a pointer should fail")
	}
	// Level 3 never delegates: the blueprint's own job cannot be a pointer.
	if checkCriterion("see `domain.md`", "invariants_owned") {
		t.Error("invariants_owned must not be satisfiable by a pointer")
	}
	if checkCriterion("see `wireframe.md`", "falsifiers_assigned") {
		t.Error("falsifiers_assigned must not be satisfiable by a pointer")
	}
}

func TestDeclaredUncertainties(t *testing.T) {
	const heading = "## Known Uncertainties\n\n"
	if !hasDeclaredUncertainties(heading + "None open.\n") {
		t.Error("a section declaring nothing open should pass")
	}
	unanswered := heading + "### U-001: whether the cache reloads\nWe are not sure.\n"
	if hasDeclaredUncertainties(unanswered) {
		t.Error("an uncertainty with no way to settle it should fail")
	}
	answered := unanswered + "Opens-with: `curl` the RPC and look for PGRST202.\n"
	if !hasDeclaredUncertainties(answered) {
		t.Error("Opens-with should settle the criterion")
	}
	if hasDeclaredUncertainties("no section at all") {
		t.Error("silence is not a declaration")
	}
}

func TestIrreversibleOrderNeedsBothOrderAndUndo(t *testing.T) {
	ordered := "1. S-1 migrate\n2. S-2 wire the backend\n"
	if hasIrreversibleOrder(ordered) {
		t.Error("an order with no rollback should fail")
	}
	if !hasIrreversibleOrder(ordered + "rollback: drop the column.\n") {
		t.Error("order plus rollback should pass")
	}
}

func TestErrorStrategyNeedsADisposition(t *testing.T) {
	if hasErrorStrategy("we handle errors carefully") {
		t.Error("naming failure without saying what happens should fail")
	}
	if !hasErrorStrategy("a malformed publish is rejected with 400") {
		t.Error("failure plus disposition should pass")
	}
}

func TestUnknownCriterionIsNotMet(t *testing.T) {
	if checkCriterion("anything at all", "no_such_criterion") {
		t.Error("a criterion with no predicate must not report itself met")
	}
}

// bts runs on non-English projects, and every criterion here was written
// with Korean alternatives in its patterns. RE2's `\b` is defined against
// ASCII `\w`, so a Korean word never has a word boundary adjacent to it
// and `\b계약\b` cannot match at all — a bug no English fixture can show.
// This document is the shape a Korean skeleton actually takes.
func TestKoreanDocumentReachesLevel3(t *testing.T) {
	doc := "# Blueprint: 커뮤니티 번들 커버 이미지\n\n" +
		"## 1. 무엇을 출하하는가\n" +
		"책 캡처를 공유하면 실제 표지가 보인다. 스캔 사진은 P2다.\n\n" +
		"## 2. 불변식과 소유자\n" +
		"| ID | 진술 | 소유 파일 |\n|---|---|---|\n" +
		"| INV-001 | 저장된 표지 주소는 허용 호스트의 https URL이다 | `backend/db/0026_cover.sql` |\n" +
		"| INV-002 | 표지 없음은 하나의 표현만 갖는다 | `ios/App/CommunityModels.swift` |\n\n" +
		"## 3. 경계 계약\n" +
		"| 층 | 이름 | 모양 |\n|---|---|---|\n" +
		"| 컬럼 | `cover_url` | 빈 문자열 아니면 https |\n" +
		"| 와이어 | `coverUrl` | 옵셔널 문자열 |\n" +
		"| 파생 | `coverImageURL` | 옵셔널 URL |\n\n" +
		"## 4. 단위와 의존 순서\n" +
		"`wireframe.md` §4가 정본이다. 여기서 복제하지 않는다.\n\n" +
		"## 5. 되돌릴 수 없는 순서\n" +
		"1. S-1 마이그레이션을 먼저 적용한다.\n" +
		"2. S-2 백엔드를 배선한다. 앱은 그 뒤에 나간다.\n" +
		"순서를 어기면 모든 발행이 400으로 거부된다. 롤백은 컬럼 제거다.\n\n" +
		"## 6. 반증자\n" +
		"| 불변식 | 반증자 |\n|---|---|\n" +
		"| INV-001 | `backend/test/guards.spec.ts` — 허용목록 밖 호스트가 죽는다 |\n" +
		"| INV-002 | `ios/App-Tests/CoverTests.swift` — 부재의 두 표현이 같아진다 |\n\n" +
		"## Known Uncertainties\n\n" +
		"### U-001: 콜레이션이 범위를 고정하는지\n" +
		"Opens-with: `show lc_ctype` 후 비ASCII 문자로 프로브.\n"

	got := assessLevel(doc)
	if got.Level < 3.0 {
		t.Errorf("Korean skeleton level = %.3f, want 3.0; unmet: %v", got.Level, got.Missing)
	}
}
