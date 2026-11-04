package engine

import (
	"errors"
	"strings"
	"testing"
)

func reviewDoc(block string) []byte {
	return []byte("# Code Review: auth\n\n## Summary\n- Critical: 1\n\n" + block + "\n")
}

func TestParseReviewFindings_ValidRunAndScenario(t *testing.T) {
	doc := reviewDoc(`<bts-review-findings>
{"findings": [
  {"id": "CRT-001", "severity": "critical", "perspective": "quality", "title": "token never expires",
   "anchor": "src/auth/session.go:42",
   "evidence": {"kind": "run", "command": "go test ./src/auth -run TestExpiry",
                "observed": "--- FAIL: TestExpiry: token valid after 2h", "required": "PASS"}},
  {"id": "CRT-002", "severity": "critical", "perspective": "security", "title": "authz bypass on reset",
   "evidence": {"kind": "scenario", "input": "POST /reset with another user's id",
                "path": "handlers/reset.go:30 -> store/user.go:88", "outcome": "password of another user changed"}},
  {"id": "MAJ-001", "severity": "major", "perspective": "architecture", "title": "import cycle risk"}
]}
</bts-review-findings>`)
	block, err := ParseReviewFindingsBlock(doc)
	if err != nil {
		t.Fatalf("valid block rejected: %v", err)
	}
	if len(block.Findings) != 3 {
		t.Fatalf("want 3 findings, got %d", len(block.Findings))
	}
}

func TestParseReviewFindings_NoBlock(t *testing.T) {
	_, err := ParseReviewFindingsBlock(reviewDoc("no block here"))
	if !errors.Is(err, ErrNoReviewFindingsBlock) {
		t.Fatalf("want ErrNoReviewFindingsBlock, got %v", err)
	}
}

func TestParseReviewFindings_CriticalNeedsEvidence(t *testing.T) {
	cases := map[string]string{
		"no evidence":               `{"id":"CRT-001","severity":"critical","perspective":"quality","title":"x"}`,
		"run without observed":      `{"id":"CRT-001","severity":"critical","perspective":"quality","title":"x","evidence":{"kind":"run","command":"go test ./...","required":"PASS"}}`,
		"scenario outside security": `{"id":"CRT-001","severity":"critical","perspective":"quality","title":"x","evidence":{"kind":"scenario","input":"a","path":"b","outcome":"c"}}`,
		"scenario missing outcome":  `{"id":"CRT-001","severity":"critical","perspective":"security","title":"x","evidence":{"kind":"scenario","input":"a","path":"b"}}`,
		"unknown evidence kind":     `{"id":"CRT-001","severity":"critical","perspective":"quality","title":"x","evidence":{"kind":"vibes"}}`,
		"unknown severity":          `{"id":"MAJ-001","severity":"blocker","perspective":"quality","title":"x"}`,
		"missing title":             `{"id":"MAJ-001","severity":"major","perspective":"quality","title":""}`,
	}
	for name, finding := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := ParseReviewFindingsBlock(reviewDoc(`<bts-review-findings>{"findings":[` + finding + `]}</bts-review-findings>`))
			if err == nil || errors.Is(err, ErrNoReviewFindingsBlock) {
				t.Fatalf("want a validation error, got %v", err)
			}
		})
	}
}

func TestParseReviewFindings_NonCriticalNeedsNoEvidence(t *testing.T) {
	doc := reviewDoc(`<bts-review-findings>{"findings":[
	  {"id":"MAJ-001","severity":"major","perspective":"quality","title":"unproven, downgraded"},
	  {"id":"MIN-001","severity":"minor","perspective":"architecture","title":"naming"}]}</bts-review-findings>`)
	if _, err := ParseReviewFindingsBlock(doc); err != nil {
		t.Fatalf("majors and minors need no evidence: %v", err)
	}
}

func TestParseReviewFindings_ReportsEveryProblem(t *testing.T) {
	doc := reviewDoc(`<bts-review-findings>{"findings":[
	  {"id":"CRT-001","severity":"critical","perspective":"quality","title":"a"},
	  {"id":"CRT-002","severity":"critical","perspective":"quality","title":"b"}]}</bts-review-findings>`)
	_, err := ParseReviewFindingsBlock(doc)
	if err == nil || !strings.Contains(err.Error(), "CRT-001") || !strings.Contains(err.Error(), "CRT-002") {
		t.Fatalf("error must name every unproven critical, got %v", err)
	}
}

func TestParseReviewFindings_TwoBlocksRejected(t *testing.T) {
	one := `<bts-review-findings>{"findings":[]}</bts-review-findings>`
	if _, err := ParseReviewFindingsBlock(reviewDoc(one + "\n" + one)); err == nil || errors.Is(err, ErrNoReviewFindingsBlock) {
		t.Fatalf("two blocks must be rejected, got %v", err)
	}
}
