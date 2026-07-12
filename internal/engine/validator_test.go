package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateVerificationMd_MissingBlock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "verification.md")
	if err := os.WriteFile(path, []byte("# Verify report\n\nno block here\n"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	errs := validateVerificationMd(path)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}
	if !strings.Contains(errs[0].Message, "missing structured findings block") {
		t.Errorf("unexpected error message: %s", errs[0].Message)
	}
}

func TestValidateVerificationMd_ValidBlock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "verification.md")
	content := `# Report

<bts-findings>
{
  "critical": 0,
  "major": 1,
  "minor_resolvable": 2,
  "minor_deferred": 0
}
</bts-findings>

findings list below…
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	errs := validateVerificationMd(path)
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}
}

func TestValidateVerificationMd_MissingField(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "verification.md")
	content := `<bts-findings>
{"critical": 0, "major": 0}
</bts-findings>`
	_ = os.WriteFile(path, []byte(content), 0644)
	errs := validateVerificationMd(path)
	// missing: minor_resolvable, minor_deferred
	if len(errs) != 2 {
		t.Fatalf("expected 2 errors, got %d: %v", len(errs), errs)
	}
}

func TestValidateAssessDecisionBlock_Valid(t *testing.T) {
	content := `## Assessment

<bts-decision>
{
  "level": 2.5,
  "action": "IMPROVE",
  "phase": "draft",
  "reason": "add signatures"
}
</bts-decision>
`
	action, errs := ValidateAssessDecisionBlock(content)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if action != "IMPROVE" {
		t.Errorf("want action=IMPROVE, got %s", action)
	}
}

func TestValidateAssessDecisionBlock_InvalidEnum(t *testing.T) {
	content := `<bts-decision>
{"level": 1.0, "action": "RAGEQUIT", "phase": "draft", "reason": "x"}
</bts-decision>`
	_, errs := ValidateAssessDecisionBlock(content)
	if len(errs) == 0 {
		t.Fatal("expected enum error")
	}
	found := false
	for _, e := range errs {
		if strings.Contains(e.Message, "invalid action") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'invalid action' message, got %v", errs)
	}
}

func TestValidateAssessDecisionBlock_Missing(t *testing.T) {
	_, errs := ValidateAssessDecisionBlock("no block here")
	if len(errs) == 0 {
		t.Fatal("expected missing-block error")
	}
}

// Sprint 10: ParseFindingsBlock backs `bts recipe log --from-verification`.
func TestParseFindingsBlock_Valid(t *testing.T) {
	doc := []byte("prose\n<bts-findings>\n{\"critical\": 1, \"major\": 2, \"minor_resolvable\": 3, \"minor_deferred\": 4, \"info\": 5}\n</bts-findings>\nfindings...")
	c, err := ParseFindingsBlock(doc)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if c.Critical != 1 || c.Major != 2 || c.MinorResolvable != 3 || c.MinorDeferred != 4 || c.Info != 5 {
		t.Errorf("counts mismatch: %+v", c)
	}
}

func TestParseFindingsBlock_Errors(t *testing.T) {
	cases := map[string][]byte{
		"missing block":   []byte("no block here"),
		"duplicate block": []byte("<bts-findings>{\"critical\":0,\"major\":0,\"minor_resolvable\":0,\"minor_deferred\":0}</bts-findings><bts-findings>{\"critical\":0,\"major\":0,\"minor_resolvable\":0,\"minor_deferred\":0}</bts-findings>"),
		"missing field":   []byte("<bts-findings>{\"critical\":0,\"major\":0,\"minor_resolvable\":0}</bts-findings>"),
		"malformed json":  []byte("<bts-findings>{critical: 0}</bts-findings>"),
	}
	for name, doc := range cases {
		if _, err := ParseFindingsBlock(doc); err == nil {
			t.Errorf("%s: expected error, got nil", name)
		}
	}
}
