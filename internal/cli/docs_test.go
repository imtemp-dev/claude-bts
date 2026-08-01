package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// docsFixture builds a miniature source repo: a template tree defining
// what "ships", plus whatever prose surfaces the test needs.
func docsFixture(t *testing.T, files map[string]string, skills []string, hooks, rules int) string {
	t.Helper()
	repo := t.TempDir()
	tmpl := filepath.Join(repo, "internal", "template", "templates", ".claude")
	for _, s := range skills {
		if err := os.MkdirAll(filepath.Join(tmpl, "skills", s), 0755); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < hooks; i++ {
		writeProjectFile(t, repo, filepath.Join("internal/template/templates/.claude/hooks",
			"h"+string(rune('a'+i))+".sh"), "#!/bin/sh\n")
	}
	for i := 0; i < rules; i++ {
		writeProjectFile(t, repo, filepath.Join("internal/template/templates/.claude/rules",
			"r"+string(rune('a'+i))+".md"), "rule\n")
	}
	for name, content := range files {
		writeProjectFile(t, repo, name, content)
	}
	return repo
}

// The counted-claim check must catch a stale number in EVERY translation,
// not just the English one — that is exactly how all four READMEs came to
// claim 21 skills while 24 shipped.
func TestCheckCountedClaims_CatchesEveryTranslation(t *testing.T) {
	repo := docsFixture(t, map[string]string{
		"README.md":    "24 skills, 8 lifecycle hooks, 7 rules",
		"README.ko.md": "21개 스킬 / 8개 라이프사이클 훅 / 6개 규칙",
		"README.zh.md": "21 个技能, 8 个生命周期钩子, 7 个规则",
		"README.ja.md": "24スキル 8ライフサイクルフック 7ルール",
	}, []string{"bts-verify", "bts-audit"}, 8, 7)

	inv := &inventory{skills: make([]string, 24), hooks: 8, rules: 7}
	problems := checkCountedClaims(repo,
		[]string{"README.md", "README.ko.md", "README.zh.md", "README.ja.md"}, inv)

	joined := strings.Join(problems, "\n")
	for _, want := range []string{"README.ko.md", "README.zh.md"} {
		if !strings.Contains(joined, want) {
			t.Errorf("expected a stale-count problem for %s, got:\n%s", want, joined)
		}
	}
	for _, unwanted := range []string{"README.md say", "README.ja.md"} {
		if strings.Contains(joined, unwanted) {
			t.Errorf("%s is correct and must not be reported, got:\n%s", unwanted, joined)
		}
	}
	if len(problems) != 3 { // ko: skills + rules, zh: skills
		t.Errorf("expected 3 problems (ko skills, ko rules, zh skills), got %d:\n%s", len(problems), joined)
	}
}

// A path segment must not read as a slash-command reference: the doc
// mentioning scripts/bts-monitor.ts is naming a file, not a skill.
func TestCheckSkillNames_PathSegmentIsNotAReference(t *testing.T) {
	repo := docsFixture(t, map[string]string{
		"llms.txt": "run `scripts/bts-monitor.ts` and see docs/bts-baseline.md\n",
	}, []string{"bts-verify"}, 1, 1)

	inv := &inventory{skills: []string{"bts-verify"}}
	if problems := checkSkillNames(repo, []string{"llms.txt"}, inv); len(problems) != 0 {
		t.Fatalf("file paths are not skill references, got: %v", problems)
	}
}

// A genuine reference to a skill that no longer ships must be caught.
func TestCheckSkillNames_CatchesRemovedSkill(t *testing.T) {
	repo := docsFixture(t, map[string]string{
		"README.md": "Run /bts-verify then /bts-ghost to finish.\n",
	}, []string{"bts-verify"}, 1, 1)

	inv := &inventory{skills: []string{"bts-verify"}}
	problems := checkSkillNames(repo, []string{"README.md"}, inv)
	if len(problems) != 1 || !strings.Contains(problems[0], "bts-ghost") {
		t.Fatalf("expected one problem naming bts-ghost, got: %v", problems)
	}
}

// Only backtick-quoted `bts <word>` is a command reference, so ordinary
// prose is not mistaken for one.
func TestCheckCommandNames_CatchesUnknownAndIgnoresProse(t *testing.T) {
	repo := docsFixture(t, map[string]string{
		"README.md": "bts then verifies the spec. Run `bts doctor` and `bts nonesuch`.\n",
	}, []string{"bts-verify"}, 1, 1)

	problems := checkCommandNames(repo, []string{"README.md"})
	if len(problems) != 1 || !strings.Contains(problems[0], "bts nonesuch") {
		t.Fatalf("expected exactly the unknown command, got: %v", problems)
	}
}

func TestCheckRelativeLinks_CatchesMissingTargetAndSkipsExternal(t *testing.T) {
	repo := docsFixture(t, map[string]string{
		"README.md":    "[ok](docs/real.md) [gone](docs/gone.md) [web](https://example.com) [anchor](#x)\n",
		"docs/real.md": "hi\n",
	}, []string{"bts-verify"}, 1, 1)

	problems := checkRelativeLinks(repo, []string{"README.md"})
	if len(problems) != 1 || !strings.Contains(problems[0], "docs/gone.md") {
		t.Fatalf("expected exactly the missing local target, got: %v", problems)
	}
}

// A new document nobody classified would otherwise be silently exempt
// from every check.
func TestCheckAudienceCoverage_BothDirections(t *testing.T) {
	audiences := map[string]string{
		"README.md":    "user-reference",
		"docs/gone.md": "maintainer-design",
	}
	problems := checkAudienceCoverage([]string{"README.md", "docs/new.md"}, audiences)

	joined := strings.Join(problems, "\n")
	if !strings.Contains(joined, "docs/new.md is not classified") {
		t.Errorf("an unclassified surface must be reported, got:\n%s", joined)
	}
	if !strings.Contains(joined, "docs/gone.md, which no longer exists") {
		t.Errorf("a classification for a deleted file must be reported, got:\n%s", joined)
	}
	if len(problems) != 2 {
		t.Errorf("expected 2 problems, got %d:\n%s", len(problems), joined)
	}
}

// The repo's own documentation must pass. This is the check gating CI, so
// a failure here is a real drift, not a test-fixture problem.
func TestDocsCheck_RepoIsClean(t *testing.T) {
	repo, err := findSourceRepo()
	if err != nil {
		t.Skipf("not in the source repo: %v", err)
	}
	surfaces, err := maintainedSurfaces(repo)
	if err != nil {
		t.Fatal(err)
	}
	inv, err := shippedInventory(repo)
	if err != nil {
		t.Fatal(err)
	}
	audiences, err := loadAudiences(repo)
	if err != nil {
		t.Fatal(err)
	}

	var problems []string
	problems = append(problems, checkAudienceCoverage(surfaces, audiences)...)
	current := surfacesWithAudience(surfaces, audiences, audienceUserReference)
	problems = append(problems, checkSkillNames(repo, current, inv)...)
	problems = append(problems, checkCommandNames(repo, current)...)
	problems = append(problems, checkCountedClaims(repo, current, inv)...)
	problems = append(problems, checkRelativeLinks(repo, surfaces)...)

	if len(problems) > 0 {
		t.Fatalf("documentation drift:\n  %s", strings.Join(problems, "\n  "))
	}
}
